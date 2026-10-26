package archives

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
)

func TestArchiveRendererAndAnnotationExports(t *testing.T) {
	if os.Getenv("N9E_TEST_ARCHIVE_RENDERER") != "1" {
		t.Skip("set N9E_TEST_ARCHIVE_RENDERER=1 with Node 24, LibreOffice and Poppler")
	}
	s := newTestService(t)
	s.ConfigureExports(os.Getenv("N9E_ARCHIVE_NODE"), "", 120)
	a, id := other(t, s)
	work := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	original := body("归档中文测试 Text integrity 123")
	var snapshot map[string]interface{}
	json.Unmarshal([]byte(original), &snapshot)
	snapshot["options"].(map[string]interface{})["defaultFont"] = "Noto Sans CJK SC"
	original = encode(snapshot)
	v := models.ArchiveDocumentVersion{Body: original}
	docxPath := filepath.Join(work, "result.docx")
	if err := s.renderVersion(ctx, work, docxPath, v, "docx", s.exportRuntime()); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(docxPath)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	found := false
	for _, f := range z.File {
		if f.Name == "word/document.xml" {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			found = strings.Contains(string(b), "归档中文测试 Text integrity 123")
		}
	}
	if !found {
		t.Fatal("DOCX dropped Chinese/text")
	}
	pdfPath := filepath.Join(work, "result.pdf")
	if err := s.renderVersion(ctx, work, pdfPath, v, "pdf", s.exportRuntime()); err != nil {
		t.Fatal(err)
	}
	text, err := exec.Command("pdftotext", pdfPath, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(text, []byte("归档中文测试")) || !bytes.Contains(text, []byte("Text integrity")) {
		t.Fatalf("PDF text missing: %s", text)
	}
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	a, err = s.Upload(alice, a.ID, 0, "report.pdf", "other", "", 0, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	pdfDoc := a.Documents[len(a.Documents)-1]
	l, err := s.Acquire(alice, pdfDoc.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := edit(l.Lease, encode(AnnotationBody{Annotations: []Annotation{{ID: "test-note", Type: "note", Page: 1, Rects: []Rect{{X: 0.1, Y: 0.1, Width: 0.1, Height: 0.1}}, Text: "中文批注"}}}))
	saved, err := s.Save(alice, pdfDoc.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	annotated := filepath.Join(work, "annotated.pdf")
	if err := s.renderVersion(ctx, work, annotated, saved.Version, "annotated_pdf", s.exportRuntime()); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(data, mustRead(t, annotated)) {
		t.Fatal("annotations did not change export")
	}
	if !bytes.Equal(data, mustRead(t, filepath.Join(s.Root, "objects", mustBlob(t, s, pdfDoc.File.ID).StorageKey))) {
		t.Fatal("PDF original changed")
	}
	// A selected document produces exactly the requested format, including PDF
	// original versus annotated output. No second rendition or internal files.
	currentSelection, err := s.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, format string
		documentID   int64
		extensions   []string
	}{
		{"online PDF only", "pdf", id, []string{".pdf"}},
		{"online Word only", "docx", id, []string{".docx"}},
		{"online both", "pdf_and_docx", id, []string{".docx", ".pdf"}},
		{"PDF original only", "original", pdfDoc.ID, []string{".pdf"}},
		{"PDF annotated only", "annotated_pdf", pdfDoc.ID, []string{".pdf"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job, err := s.CreateFormattedDownload(alice, a.ID, currentSelection.Revision, []DocumentDownload{{DocumentID: tc.documentID, Format: tc.format}})
			if err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(40 * time.Second); ; {
				job, path, err := s.Export(job.ID)
				if err != nil || job.State == "error" || time.Now().After(deadline) {
					t.Fatal("formatted download failed", err, job)
				}
				if job.State != "done" {
					time.Sleep(20 * time.Millisecond)
					continue
				}
				bundle, err := zip.OpenReader(path)
				if err != nil {
					t.Fatal(err)
				}
				defer bundle.Close()
				if len(bundle.File) != len(tc.extensions) {
					t.Fatalf("unexpected files: %v", bundle.File)
				}
				for i, file := range bundle.File {
					if filepath.Ext(file.Name) != tc.extensions[i] {
						t.Fatalf("wrong format: %s", file.Name)
					}
					reader, err := file.Open()
					if err != nil {
						t.Fatal(err)
					}
					got, err := io.ReadAll(reader)
					reader.Close()
					if err != nil {
						t.Fatal(err)
					}
					if tc.documentID == pdfDoc.ID && (bytes.Equal(got, data) != (tc.format == "original")) {
						t.Fatal("PDF original/annotated selection was not respected")
					}
					if tc.extensions[i] == ".pdf" && !bytes.HasPrefix(got, []byte("%PDF-")) {
						t.Fatal("download is not PDF")
					}
					if tc.extensions[i] == ".docx" && !bytes.HasPrefix(got, []byte("PK")) {
						t.Fatal("download is not DOCX")
					}
				}
				break
			}
		})
	}
	input.BaseVersionID = saved.CurrentVersionID
	input.Body = encode(AnnotationBody{Annotations: []Annotation{{Type: "note", Page: 999, Rects: []Rect{{X: 0.1, Y: 0.1, Width: 0.1, Height: 0.1}}}}})
	_, err = s.Save(alice, pdfDoc.ID, input)
	problemStatus(t, err, 422)
	s.Release(alice, pdfDoc.ID, l.Lease.SessionID, l.Lease.Epoch)
	// The selected historical version determines the export kind even when its
	// current attachment has subsequently been replaced by a different format.
	l, err = s.Acquire(alice, pdfDoc.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Upload(alice, a.ID, pdfDoc.ID, "replacement.png", "other", l.Lease.SessionID, l.Lease.Epoch, bytes.NewReader(pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	s.Release(alice, pdfDoc.ID, l.Lease.SessionID, l.Lease.Epoch)
	historyJob, err := s.CreateExport(alice, a.ID, pdfDoc.ID, saved.Version.ID, "annotated_pdf")
	if err != nil {
		t.Fatal("historical PDF export used the current file kind", err)
	}
	for deadline := time.Now().Add(20 * time.Second); ; {
		job, path, err := s.Export(historyJob.ID)
		if err != nil || job.State == "error" || time.Now().After(deadline) {
			t.Fatal("historical PDF export failed", err, job)
		}
		if job.State == "done" {
			if !bytes.HasPrefix(mustRead(t, path), []byte("%PDF-")) {
				t.Fatal("historical export did not produce PDF")
			}
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Full archive ZIP pins a manifest and includes originals + true online outputs.
	current, _ := s.Get(a.ID)
	infoPath := filepath.Join(work, "information.pdf")
	if err := s.renderInformation(ctx, work, infoPath, Snapshot{Record: current.ArchiveRecord}, s.exportRuntime()); err != nil {
		t.Fatal(err)
	}
	info, err := exec.Command("pdftotext", infoPath, "-").Output()
	if err != nil || !bytes.Contains(info, []byte("档案版本")) {
		t.Fatalf("information PDF: %v, %s", err, info)
	}
	job, err := s.CreateExport(alice, a.ID, 0, 0, "zip")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(80 * time.Second)
	for time.Now().Before(deadline) {
		current, p, err := s.Export(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.State == "error" {
			t.Fatal(current.Error)
		}
		if current.State == "done" {
			z, err := zip.OpenReader(p)
			if err != nil {
				t.Fatal(err)
			}
			defer z.Close()
			names := map[string]bool{}
			for _, f := range z.File {
				names[f.Name] = true
			}
			if !names["archive-information.pdf"] || names["archive-information.json"] {
				t.Fatal(names)
			}
			if len(names) < 4 {
				t.Fatalf("incomplete archive: %v", names)
			}
			_ = id
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("export timed out")
}

func TestArchiveDownloadSelectsOnlyRequestedSavedDocuments(t *testing.T) {
	s := newTestService(t)
	a, _ := other(t, s)
	a, err := s.Upload(alice, a.ID, 0, "selected.png", "other", "", 0, bytes.NewReader(pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	doc := a.Documents[len(a.Documents)-1]
	a, err = s.Upload(alice, a.ID, 0, "also-selected.png", "other", "", 0, bytes.NewReader(pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	second := a.Documents[len(a.Documents)-1]
	_, foreignID := other(t, s)
	for _, tc := range []struct {
		name     string
		revision int64
		ids      []int64
		status   int
	}{
		{"empty", a.Revision, nil, 422},
		{"missing revision", 0, []int64{doc.ID}, 422},
		{"too many", a.Revision, make([]int64, MaxDocuments+1), 422},
		{"stale", a.Revision - 1, []int64{doc.ID}, 409},
		{"duplicate", a.Revision, []int64{doc.ID, doc.ID}, 422},
		{"foreign", a.Revision, []int64{foreignID}, 422},
		{"missing", a.Revision, []int64{999999}, 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.CreateDownload(alice, a.ID, tc.revision, tc.ids)
			problemStatus(t, err, tc.status)
		})
	}
	var count int64
	s.DB.Model(&models.ArchiveExport{}).Count(&count)
	if count != 0 {
		t.Fatal("invalid selection queued an export")
	}
	job, err := s.CreateDownload(alice, a.ID, a.Revision, []int64{doc.ID, second.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Removing the current document must not alter the export's pinned snapshot.
	if err := s.DeleteDocument(alice, a.ID, doc.ID); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		job, path, err := s.Export(job.ID)
		if err != nil || job.State == "error" || time.Now().After(deadline) {
			t.Fatal("download failed", err, job)
		}
		if job.State == "done" {
			zip, err := zip.OpenReader(path)
			if err != nil {
				t.Fatal(err)
			}
			defer zip.Close()
			if len(zip.File) != 2 || !strings.HasSuffix(zip.File[0].Name, "-selected.png") || !strings.HasSuffix(zip.File[1].Name, "-also-selected.png") {
				t.Fatalf("unselected or internal files leaked: %v", zip.File)
			}
			r, err := zip.File[0].Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			got, _ := io.ReadAll(r)
			if !bytes.Equal(got, pngBytes(t)) {
				t.Fatal("original bytes changed")
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	logs, err := s.Logs(a.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var audited bool
	for _, log := range logs {
		if log.Action == "export_requested" && strings.Contains(string(log.Detail), "document_ids") {
			audited = true
		}
	}
	if !audited {
		t.Fatal("selection was not audited")
	}
}

func TestArchiveDownloadRejectsInvalidFormatSelections(t *testing.T) {
	s := newTestService(t)
	a, onlineID := other(t, s)
	a, err := s.Upload(alice, a.ID, 0, "image.png", "other", "", 0, bytes.NewReader(pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	imageID := a.Documents[len(a.Documents)-1].ID
	for _, documents := range [][]DocumentDownload{
		nil,
		{{DocumentID: onlineID}},
		{{DocumentID: onlineID, Format: "original"}},
		{{DocumentID: imageID, Format: "pdf"}},
		{{DocumentID: imageID, Format: "annotated_pdf"}},
		{{DocumentID: onlineID, Format: "pdf"}, {DocumentID: onlineID, Format: "docx"}},
		{{DocumentID: 99999, Format: "pdf"}},
	} {
		_, err := s.CreateFormattedDownload(alice, a.ID, a.Revision, documents)
		problemStatus(t, err, 422)
	}
	_, err = s.CreateFormattedDownload(alice, a.ID, a.Revision-1, []DocumentDownload{{DocumentID: onlineID, Format: "pdf"}})
	problemStatus(t, err, 409)
	var count int64
	s.DB.Model(&models.ArchiveExport{}).Count(&count)
	if count != 0 {
		t.Fatal("invalid format selection queued an export")
	}
}
func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func mustBlob(t *testing.T, s *Service, id int64) *models.ArchiveBlob {
	t.Helper()
	b, _, e := s.Blob(id)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
