package archives

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"github.com/toolkits/pkg/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed renderer/worker.cjs
var renderWorker []byte

//go:embed renderer/NOTICE
var RendererNotices []byte

type ExportRuntime struct {
	NodePath, LibreOfficePath string
	TimeoutSeconds            int
	mu                        sync.Mutex
	once                      sync.Once
	pdfPages                  sync.Map
}

func (s *Service) ConfigureExports(node, office string, timeout int) {
	s.ExportRuntime = &ExportRuntime{NodePath: node, LibreOfficePath: office, TimeoutSeconds: timeout}
}
func (s *Service) exportRuntime() *ExportRuntime {
	if s.ExportRuntime == nil {
		return &ExportRuntime{TimeoutSeconds: 120}
	}
	return s.ExportRuntime
}
func (s *Service) StartExports() {
	r := s.exportRuntime()
	r.once.Do(func() {
		s.DB.Model(&models.ArchiveExport{}).Where("state IN ?", []string{"queued", "running"}).Updates(map[string]interface{}{"state": "error", "error": "Export interrupted; retry", "update_at": s.now()})
	})
}
func (s *Service) CreateExport(actor Actor, archiveID, documentID, versionID int64, format string, forReading ...bool) (*models.ArchiveExport, error) {
	return s.createExport(actor, archiveID, documentID, versionID, format, len(forReading) > 0 && forReading[0], nil)
}

// CreateDownload pins the chosen documents to one saved archive revision. The
// worker owns a copy of the selection; interrupted jobs are never resumed.
func (s *Service) CreateDownload(actor Actor, archiveID, revision int64, documentIDs []int64) (*models.ArchiveExport, error) {
	if revision <= 0 || len(documentIDs) == 0 || len(documentIDs) > MaxDocuments {
		return nil, invalid("Choose documents to download and reload the archive if it has changed")
	}
	return s.createExport(actor, archiveID, 0, 0, "zip", false, &downloadSelection{revision: revision, documentIDs: append([]int64(nil), documentIDs...)})
}

type DocumentDownload struct {
	DocumentID int64  `json:"document_id"`
	Format     string `json:"format"`
}

// CreateFormattedDownload preserves the requested output format per document.
func (s *Service) CreateFormattedDownload(actor Actor, archiveID, revision int64, documents []DocumentDownload) (*models.ArchiveExport, error) {
	if revision <= 0 || len(documents) == 0 || len(documents) > MaxDocuments {
		return nil, invalid("Choose documents to download and reload the archive if it has changed")
	}
	selection := &downloadSelection{revision: revision, formats: make(map[int64]string)}
	for _, document := range documents {
		selection.documentIDs = append(selection.documentIDs, document.DocumentID)
		selection.formats[document.DocumentID] = document.Format
	}
	return s.createExport(actor, archiveID, 0, 0, "zip", false, selection)
}

type downloadSelection struct {
	revision    int64
	documentIDs []int64
	formats     map[int64]string
}

func (s *Service) createExport(actor Actor, archiveID, documentID, versionID int64, format string, forReading bool, selection *downloadSelection) (*models.ArchiveExport, error) {
	if format != "pdf" && format != "docx" && format != "annotated_pdf" && format != "zip" && format != "information_pdf" {
		return nil, invalid("Unsupported export format")
	}
	if forReading && format != "pdf" {
		return nil, invalid("Unsupported export format")
	}
	s.StartExports()
	job := &models.ArchiveExport{ID: uuid.NewString(), ArchiveID: archiveID, DocumentID: documentID, Format: format, State: "queued", CreateAt: s.now(), UpdateAt: s.now(), CreateBy: actor.ID}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var a models.ArchiveRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&a, archiveID).Error; err != nil {
			return missing()
		}
		if selection != nil && a.Revision != selection.revision {
			return conflict("Archive changed; reload before downloading", a.Revision)
		}
		var active int64
		if err := tx.Model(&models.ArchiveExport{}).Where("create_by = ? AND state IN ?", actor.ID, []string{"queued", "running"}).Count(&active).Error; err != nil {
			return err
		}
		if active >= 5 {
			return conflict("Too many pending exports; wait for completion", nil)
		}
		if format == "zip" || format == "information_pdf" {
			var v models.ArchiveVersion
			q := tx.Where("archive_id = ?", archiveID)
			if versionID > 0 {
				q = q.Where("id = ?", versionID)
			}
			if err := q.Order("sequence DESC").First(&v).Error; err != nil {
				return missing()
			}
			job.ArchiveVersionID = v.ID
			if selection != nil {
				var snapshot Snapshot
				if err := json.Unmarshal([]byte(v.Snapshot), &snapshot); err != nil {
					return err
				}
				available := map[int64]string{}
				for _, ref := range snapshot.Documents {
					available[ref.Document.ID] = ref.Document.Kind
				}
				for _, id := range selection.documentIDs {
					kind, exists := available[id]
					if !exists {
						return invalid("Selected document is unavailable or duplicated")
					}
					if selection.formats != nil && !validDownloadFormat(kind, selection.formats[id]) {
						return invalid("Unsupported download format for selected document")
					}
					delete(available, id)
				}
			}
		} else {
			var d models.ArchiveDocument
			if err := tx.Where("id = ? AND archive_id = ?", documentID, archiveID).First(&d).Error; err != nil {
				return missing()
			}
			if versionID == 0 {
				versionID = d.CurrentVersionID
			}
			var v models.ArchiveDocumentVersion
			if err := tx.Where("id = ? AND document_id = ?", versionID, documentID).First(&v).Error; err != nil {
				return missing()
			}
			kind := "online"
			if v.BlobID > 0 {
				var blob models.ArchiveBlob
				if err := tx.First(&blob, v.BlobID).Error; err != nil {
					return err
				}
				kind = blob.Kind
			}
			if format == "annotated_pdf" && kind != "pdf" {
				return invalid("Annotations can only be exported for PDF files")
			}
			if (format == "pdf" || format == "docx") && kind != "online" {
				return invalid("Convert Word to an online document before exporting")
			}
			job.DocumentVersionID = v.ID
		}
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		if forReading {
			return nil
		}
		detail := map[string]interface{}{"job": job.ID, "format": format, "version_id": versionID, "archive_version_id": job.ArchiveVersionID}
		if selection != nil {
			detail["document_ids"] = selection.documentIDs
			if selection.formats != nil {
				detail["download_formats"] = selection.formats
			}
		}
		return s.log(tx, actor, archiveID, documentID, "export_requested", detail)
	})
	if err != nil {
		return nil, err
	}
	go s.runExport(*job, selection)
	return job, nil
}

func validDownloadFormat(kind, format string) bool {
	switch kind {
	case "online":
		return format == "pdf" || format == "docx" || format == "pdf_and_docx"
	case "pdf":
		return format == "original" || format == "annotated_pdf"
	default:
		return format == "original"
	}
}
func (s *Service) Export(id string) (*models.ArchiveExport, string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, "", missing()
	}
	var j models.ArchiveExport
	if err := s.DB.First(&j, "id = ?", id).Error; err != nil {
		return nil, "", missing()
	}
	return &j, filepath.Join(s.Root, "exports", j.StorageKey), nil
}
func (s *Service) runExport(job models.ArchiveExport, selection *downloadSelection) {
	runtime := s.exportRuntime()
	runtime.mu.Lock()
	defer runtime.mu.Unlock() // Bound memory and LibreOffice concurrency.
	res := s.DB.Model(&models.ArchiveExport{}).Where("id = ? AND state = ?", job.ID, "queued").Updates(map[string]interface{}{"state": "running", "update_at": s.now()})
	if res.Error != nil || res.RowsAffected != 1 {
		return
	}
	seconds := runtime.TimeoutSeconds
	if seconds <= 0 {
		seconds = 180
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	defer cancel()
	var work string
	err := s.Prepare()
	if err == nil {
		work, err = os.MkdirTemp(filepath.Join(s.Root, "temporary"), "export-")
	}
	if work != "" {
		defer os.RemoveAll(work)
	}
	filename := "archive-" + fmt.Sprint(job.ArchiveID) + "." + job.Format
	if job.Format == "information_pdf" {
		filename = "archive-" + fmt.Sprint(job.ArchiveID) + "-information.pdf"
	}
	if job.Format == "annotated_pdf" {
		filename = "document-" + fmt.Sprint(job.DocumentID) + "-annotations.pdf"
	}
	key := job.ID + filepath.Ext(filename)
	output := filepath.Join(work, filename)
	if err == nil {
		if job.Format == "information_pdf" {
			var v models.ArchiveVersion
			err = s.DB.First(&v, job.ArchiveVersionID).Error
			if err == nil {
				var snap Snapshot
				err = json.Unmarshal([]byte(v.Snapshot), &snap)
				if err == nil {
					err = s.renderInformation(ctx, work, output, snap, runtime)
				}
			}
		} else if job.Format == "zip" {
			err = s.renderArchive(ctx, work, output, job, runtime, selection)
		} else {
			var v models.ArchiveDocumentVersion
			err = s.DB.First(&v, job.DocumentVersionID).Error
			if err == nil {
				err = s.renderVersion(ctx, work, output, v, job.Format, runtime)
			}
		}
	}
	if err == nil {
		err = os.Rename(output, filepath.Join(s.Root, "exports", key))
	}
	state, msg := "done", ""
	if err != nil {
		logger.Errorf("archive export %s: %v", job.ID, err)
		state, msg = "error", "Document export failed; check the document service configuration and retry"
		if ctx.Err() != nil {
			msg = "Document export timed out; retry with a smaller document"
		}
	}
	s.DB.Model(&models.ArchiveExport{}).Where("id = ?", job.ID).Updates(map[string]interface{}{"state": state, "error": msg, "storage_key": key, "file_name": filename, "update_at": s.now()})
}
func (s *Service) runRenderer(ctx context.Context, work, output string, input interface{}, runtime *ExportRuntime) error {
	worker := filepath.Join(work, "worker.cjs")
	if err := os.WriteFile(worker, renderWorker, 0600); err != nil {
		return err
	}
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	inputPath := filepath.Join(work, "input-"+uuid.NewString()+".json")
	if err := os.WriteFile(inputPath, data, 0600); err != nil {
		return err
	}
	node := runtime.NodePath
	if node == "" {
		node = "node"
	}
	cmd := exec.CommandContext(ctx, node, "--max-old-space-size=768", worker, inputPath, output)
	prepareCommand(cmd)
	cmd.Dir = work
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("archive renderer: %w (%s)", err, stderr.String())
	}
	return nil
}
func (s *Service) renderVersion(ctx context.Context, work, output string, v models.ArchiveDocumentVersion, format string, runtime *ExportRuntime) error {
	if format == "annotated_pdf" {
		_, source, err := s.Blob(v.BlobID)
		if err != nil {
			return err
		}
		var annotations struct {
			Annotations []Annotation `json:"annotations"`
		}
		if v.Body != "" && v.Body != "null" {
			if err := json.Unmarshal([]byte(v.Body), &annotations); err != nil {
				return err
			}
		}
		source, _ = filepath.Abs(source)
		return s.runRenderer(ctx, work, output, map[string]interface{}{"operation": "annotate", "source": source, "annotations": annotations.Annotations}, runtime)
	}
	var body interface{}
	if err := json.Unmarshal([]byte(v.Body), &body); err != nil {
		return err
	}
	refs, err := assetReferences(v.Body)
	if err != nil {
		return err
	}
	assets := map[string]interface{}{}
	for id := range refs {
		b, p, err := s.Blob(id)
		if err != nil {
			return err
		}
		p, _ = filepath.Abs(p)
		assets[fmt.Sprint(id)] = map[string]interface{}{"path": p, "content_type": b.ContentType}
	}
	docxPath := output
	if format == "pdf" {
		docxPath = filepath.Join(work, "render-"+uuid.NewString()+".docx")
	}
	if err := s.runRenderer(ctx, work, docxPath, map[string]interface{}{"operation": "docx", "body": body, "assets": assets}, runtime); err != nil {
		return err
	}
	if format != "pdf" {
		return nil
	}
	office := runtime.LibreOfficePath
	if office == "" {
		office = "soffice"
	}
	profile := filepath.Join(work, "lo-profile-"+uuid.NewString())
	profile, _ = filepath.Abs(profile)
	if err := os.MkdirAll(filepath.Join(profile, "user"), 0700); err != nil {
		return err
	}
	// Highest macro security; only our generated DOCX enters this conversion.
	config := `<oor:items xmlns:oor="http://openoffice.org/2001/registry"><item oor:path="/org.openoffice.Office.Common/Security/Scripting"><prop oor:name="MacroSecurityLevel" oor:op="fuse"><value>3</value></prop></item><item oor:path="/org.openoffice.Office.Common/Misc"><prop oor:name="UseSystemFileDialog" oor:op="fuse"><value>false</value></prop></item></oor:items>`
	if err := os.WriteFile(filepath.Join(profile, "user", "registrymodifications.xcu"), []byte(config), 0600); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, office, "-env:UserInstallation=file://"+profile, "--headless", "--nologo", "--nodefault", "--nolockcheck", "--convert-to", "pdf:writer_pdf_Export", "--outdir", work, docxPath)
	prepareCommand(cmd)
	cmd.Dir = work
	if err := cmd.Run(); err != nil {
		return err
	}
	return os.Rename(strings.TrimSuffix(docxPath, ".docx")+".pdf", output)
}
func (s *Service) renderArchive(ctx context.Context, work, output string, job models.ArchiveExport, runtime *ExportRuntime, selection *downloadSelection) error {
	var v models.ArchiveVersion
	if err := s.DB.First(&v, job.ArchiveVersionID).Error; err != nil {
		return err
	}
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(v.Snapshot), &snapshot); err != nil {
		return err
	}
	file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	z := zip.NewWriter(file)
	add := func(name, p string) error {
		w, err := z.Create(name)
		if err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	}
	if selection == nil {
		p := filepath.Join(work, "archive-information.pdf")
		if err := s.renderInformation(ctx, work, p, snapshot, runtime); err != nil {
			return err
		}
		if err := add("archive-information.pdf", p); err != nil {
			return err
		}
	}
	selected := map[int64]bool{}
	if selection != nil {
		for _, id := range selection.documentIDs {
			selected[id] = true
		}
	}
	for _, ref := range snapshot.Documents {
		if selection != nil && !selected[ref.Document.ID] {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var dv models.ArchiveDocumentVersion
		if err := s.DB.First(&dv, ref.VersionID).Error; err != nil {
			return err
		}
		prefix := fmt.Sprint(ref.Document.ID) + "-" + safeExportName(ref.Document.Title)
		format := ""
		if selection != nil {
			format = selection.formats[ref.Document.ID]
		}
		if ref.Document.Kind == "online" {
			formats := []string{"docx", "pdf"}
			if format == "pdf" || format == "docx" {
				formats = []string{format}
			}
			for _, format := range formats {
				p := filepath.Join(work, uuid.NewString()+"."+format)
				if err := s.renderVersion(ctx, work, p, dv, format, runtime); err != nil {
					return err
				}
				if err := add(prefix+"."+format, p); err != nil {
					return err
				}
			}
		} else {
			blob, p, err := s.Blob(dv.BlobID)
			if err != nil {
				return err
			}
			if format != "annotated_pdf" {
				if err := add("originals/"+fmt.Sprint(ref.Document.ID)+"-"+safeExportName(blob.Name), p); err != nil {
					return err
				}
			}
			if blob.Kind == "pdf" && (format == "annotated_pdf" || (format == "" && dv.Body != "" && dv.Body != "null")) {
				p = filepath.Join(work, uuid.NewString()+".pdf")
				if err := s.renderVersion(ctx, work, p, dv, "annotated_pdf", runtime); err != nil {
					return err
				}
				if err := add(prefix+"-annotations.pdf", p); err != nil {
					return err
				}
			}
		}
	}
	if err := z.Close(); err != nil {
		return err
	}
	return file.Sync()
}
func safeExportName(s string) string {
	s = filepath.Base(strings.ReplaceAll(s, "\\", "/"))
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(":*?\"<>|", r) {
			return '_'
		}
		return r
	}, s)
	if s == "" || s == "." {
		return "document"
	}
	return s
}

func (s *Service) inspectPDF(source, key string) (int, error) {
	runtime := s.exportRuntime()
	if key != "" {
		if n, ok := runtime.pdfPages.Load(key); ok {
			return n.(int), nil
		}
	}
	if err := s.Prepare(); err != nil {
		return 0, err
	}
	work, err := os.MkdirTemp(filepath.Join(s.Root, "temporary"), "inspect-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(work)
	source, err = filepath.Abs(source)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output := filepath.Join(work, "pdf.json")
	if err := s.runRenderer(ctx, work, output, map[string]interface{}{"operation": "inspect_pdf", "source": source}, runtime); err != nil {
		return 0, invalid("PDF cannot be parsed; check the file and document runtime")
	}
	data, err := os.ReadFile(output)
	if err != nil {
		return 0, err
	}
	var result struct {
		Pages int `json:"pages"`
	}
	if json.Unmarshal(data, &result) != nil || result.Pages < 1 {
		return 0, invalid("Invalid PDF header")
	}
	if key != "" {
		runtime.pdfPages.Store(key, result.Pages)
	}
	return result.Pages, nil
}
func (s *Service) annotationsForVersion(tx *gorm.DB, actor Actor, v models.ArchiveDocumentVersion, body models.ArchiveJSON) (models.ArchiveJSON, error) {
	normalized, err := s.annotations(actor, v.Body, body)
	if err != nil {
		return "", err
	}
	var input AnnotationBody
	if err := json.Unmarshal([]byte(normalized), &input); err != nil {
		return "", err
	}
	if len(input.Annotations) == 0 {
		return normalized, nil
	}
	var blob models.ArchiveBlob
	if err := tx.First(&blob, v.BlobID).Error; err != nil {
		return "", err
	}
	pages, err := s.inspectPDF(filepath.Join(s.Root, "objects", blob.StorageKey), blob.SHA256)
	if err != nil {
		return "", err
	}
	for _, a := range input.Annotations {
		if a.Page > pages {
			return "", invalid("Annotation page exceeds PDF page count")
		}
	}
	return normalized, nil
}

func (s *Service) renderInformation(ctx context.Context, work, output string, snapshot Snapshot, runtime *ExportRuntime) error {
	ids := make([]int64, 0, len(snapshot.Documents))
	for _, ref := range snapshot.Documents {
		ids = append(ids, ref.VersionID)
	}
	versions := map[int64]int64{}
	if len(ids) > 0 {
		var rows []models.ArchiveDocumentVersion
		if err := s.DB.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			versions[row.ID] = row.Sequence
		}
		for _, id := range ids {
			if _, ok := versions[id]; !ok {
				return missing()
			}
		}
	}
	body, err := informationBody(snapshot, versions)
	if err != nil {
		return err
	}
	return s.renderVersion(ctx, work, output, models.ArchiveDocumentVersion{Body: body}, "pdf", runtime)
}
