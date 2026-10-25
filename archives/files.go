package archives

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func assetReferences(body models.ArchiveJSON) (map[int64]bool, error) {
	refs := map[int64]bool{}
	if body == "" || body == "null" {
		return refs, nil
	}
	var v interface{}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return nil, invalid("Invalid document snapshot")
	}
	count := 0
	var walk func(interface{}, int) error
	walk = func(v interface{}, depth int) error {
		count++
		if count > 200000 || depth > 64 {
			return invalid("Document structure is too complex")
		}
		switch n := v.(type) {
		case map[string]interface{}:
			if n["type"] == "block" || n["type"] == "latex" {
				return invalid("Embedded browser frames are not supported")
			}
			if raw, ok := n["url"].(string); ok && raw != "" {
				u, err := url.Parse(raw)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
					return invalid("Unsupported document hyperlink")
				}
			}
			if n["type"] == "image" {
				for _, key := range []string{"width", "height"} {
					raw, exists := n[key]
					if exists {
						number, ok := raw.(float64)
						if !ok || number <= 0 || number > 10000 {
							return invalid("Invalid document image dimensions")
						}
					}
				}

				value, _ := n["value"].(string)
				if !strings.HasPrefix(value, "archive-asset:") {
					return invalid("Document images must be uploaded before saving")
				}
				id, err := strconv.ParseInt(strings.TrimPrefix(value, "archive-asset:"), 10, 64)
				if err != nil || id <= 0 || value != "archive-asset:"+strconv.FormatInt(id, 10) {
					return invalid("Invalid document image")
				}
				refs[id] = true
			}
			for _, child := range n {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		case []interface{}:
			for _, child := range n {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(v, 0); err != nil {
		return nil, err
	}
	return refs, nil
}
func validateBody(tx *gorm.DB, archiveID int64, body models.ArchiveJSON, actorID int64) (models.ArchiveJSON, error) {
	if len(body) > MaxBodyBytes {
		return "", invalid("Document body exceeds 5 MiB")
	}
	var v map[string]interface{}
	if json.Unmarshal([]byte(body), &v) != nil {
		return "", invalid("Invalid document snapshot")
	}
	data, ok := v["data"].(map[string]interface{})
	if !ok {
		return "", invalid("Document data is missing")
	}
	if _, ok := data["main"].([]interface{}); !ok {
		return "", invalid("Document body is missing")
	}
	options, ok := v["options"].(map[string]interface{})
	if !ok {
		options = map[string]interface{}{}
		v["options"] = options
	}
	for k, value := range map[string]interface{}{"width": float64(794), "height": float64(1123), "defaultFont": "Noto Sans CJK SC", "defaultSize": float64(16), "margins": []interface{}{float64(72), float64(72), float64(72), float64(72)}} {
		if _, exists := options[k]; !exists {
			options[k] = value
		}
	}
	for _, key := range []string{"header", "footer"} {
		if raw, exists := data[key]; !exists || raw == nil {
			data[key] = []interface{}{}
		} else if _, ok := raw.([]interface{}); !ok {
			return "", invalid("Invalid document snapshot")
		}
	}
	if options, ok := v["options"].(map[string]interface{}); ok {
		for _, key := range []string{"width", "height"} {
			if raw, exists := options[key]; exists {
				number, ok := raw.(float64)
				if !ok || number < 100 || number > 3000 {
					return "", invalid("Invalid document page dimensions")
				}
			}
		}
		if raw, exists := options["margins"]; exists {
			margins, ok := raw.([]interface{})
			if !ok || len(margins) != 4 {
				return "", invalid("Invalid document margins")
			}
			for _, raw := range margins {
				n, ok := raw.(float64)
				if !ok || n < 0 || n > 500 {
					return "", invalid("Invalid document margins")
				}
			}
		}
		if background, ok := options["background"].(map[string]interface{}); ok && background["image"] != nil && background["image"] != "" {
			return "", invalid("Background images must be inserted as managed document images")
		}
		if watermark, ok := options["watermark"].(map[string]interface{}); ok && watermark["type"] == "image" && watermark["data"] != "" {
			return "", invalid("Background images must be inserted as managed document images")
		}
	}
	refs, err := assetReferences(body)
	if err != nil {
		return "", err
	}
	for _, key := range []string{"main", "header", "footer"} {
		if err := validateElements(data[key], 0); err != nil {
			return "", err
		}
	}
	for id := range refs {
		var n int64
		if err := tx.Model(&models.ArchiveBlob{}).Where("id = ? AND archive_id = ? AND asset = ? AND (staged = ? OR create_by = ?)", id, archiveID, true, false, actorID).Count(&n).Error; err != nil {
			return "", err
		}
		if n != 1 {
			return "", invalid("Document references an unavailable image")
		}
	}
	v["schema_version"] = 1
	normalized := encode(v)
	if len(normalized) > MaxBodyBytes {
		return "", invalid("Document body exceeds 5 MiB")
	}
	return normalized, nil
}

// Validate the pinned editor's element containers before a snapshot can become
// a draft or version. Malformed nodes must not make a saved document unreadable.
func validateElements(raw interface{}, depth int) error {
	items, ok := raw.([]interface{})
	if !ok || depth > 64 {
		return invalid("Invalid document snapshot")
	}
	for _, raw := range items {
		node, ok := raw.(map[string]interface{})
		if !ok {
			return invalid("Invalid document snapshot")
		}
		if value, exists := node["value"]; exists {
			if _, ok := value.(string); !ok {
				return invalid("Invalid document snapshot")
			}
		}
		kind := ""
		if raw, exists := node["type"]; exists {
			kind, ok = raw.(string)
			if !ok {
				return invalid("Invalid document snapshot")
			}
		}
		switch kind {
		case "", "text", "image", "table", "hyperlink", "superscript", "subscript", "separator", "pageBreak", "control", "area", "checkbox", "radio", "tab", "date", "title", "list", "label":
		default:
			return invalid("Invalid document snapshot")
		}
		if children, exists := node["valueList"]; exists {
			if err := validateElements(children, depth+1); err != nil {
				return err
			}
		}
		if kind == "table" {
			rows, ok := node["trList"].([]interface{})
			if !ok || len(rows) == 0 {
				return invalid("Invalid document snapshot")
			}
			for _, raw := range rows {
				row, ok := raw.(map[string]interface{})
				if !ok {
					return invalid("Invalid document snapshot")
				}
				cells, ok := row["tdList"].([]interface{})
				if !ok {
					return invalid("Invalid document snapshot")
				}
				for _, raw := range cells {
					cell, ok := raw.(map[string]interface{})
					if !ok {
						return invalid("Invalid document snapshot")
					}
					if err := validateElements(cell["value"], depth+1); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
func currentBytes(tx *gorm.DB, archiveID int64, docs []Document) (int64, error) {
	var size int64
	refs := map[int64]bool{}
	for _, d := range docs {
		if d.Version.BlobID > 0 {
			refs[d.Version.BlobID] = true
		}
		if d.Kind == "pdf" && d.File != nil && d.Version.Size > d.File.Size {
			size += d.Version.Size - d.File.Size
		}
		if d.Kind == "online" {
			size += d.Version.Size
			var images []int64
			if d.Version.AssetIDs != "" && d.Version.AssetIDs != "null" {
				if err := json.Unmarshal([]byte(d.Version.AssetIDs), &images); err != nil {
					return 0, err
				}
			}
			for _, id := range images {
				refs[id] = true
			}
		}
	}
	for id := range refs {
		var blob models.ArchiveBlob
		if err := tx.First(&blob, id).Error; err != nil {
			return 0, err
		}
		if blob.ArchiveID != archiveID {
			return 0, invalid("File belongs to another archive")
		}
		size += blob.Size
	}
	return size, nil
}
func validateFile(file *os.File, name string, asset bool) (kind, contentType string, err error) {
	head := make([]byte, 512)
	n, e := file.ReadAt(head, 0)
	if e != nil && e != io.EOF {
		return "", "", e
	}
	head = head[:n]
	ext := strings.ToLower(filepath.Ext(name))
	if asset && ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
		return "", "", invalid("Inline images must use PNG or JPEG")
	}
	switch ext {
	case ".png", ".jpg", ".jpeg":
		cfg, format, e := image.DecodeConfig(io.NewSectionReader(file, 0, MaxFileBytes))
		if e != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
			return "", "", invalid("Image is damaged or exceeds 40 million pixels")
		}
		if (ext == ".png" && format != "png") || (ext != ".png" && format != "jpeg") {
			return "", "", invalid("Image extension does not match its content")
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return "", "", err
		}
		if _, _, err := image.Decode(file); err != nil {
			return "", "", invalid("Image is damaged or exceeds 40 million pixels")
		}
		if format == "png" {
			return "image", "image/png", nil
		}
		return "image", "image/jpeg", nil
	case ".pdf":
		if !bytes.HasPrefix(head, []byte("%PDF-")) {
			return "", "", invalid("Invalid PDF header")
		}
		stat, e := file.Stat()
		if e != nil {
			return "", "", e
		}
		start := stat.Size() - 2048
		if start < 0 {
			start = 0
		}
		tail, e := io.ReadAll(io.NewSectionReader(file, start, stat.Size()-start))
		if e != nil {
			return "", "", e
		}
		if !bytes.Contains(tail, []byte("%%EOF")) {
			return "", "", invalid("PDF appears incomplete")
		}
		if bytes.Contains(tail, []byte("/Encrypt")) {
			return "", "", invalid("Encrypted PDFs are not supported")
		}
		return "pdf", "application/pdf", nil
	case ".docx":
		stat, e := file.Stat()
		if e != nil {
			return "", "", e
		}
		z, e := zip.NewReader(file, stat.Size())
		if e != nil {
			return "", "", invalid("Word file is not a valid DOCX archive")
		}
		var total uint64
		found := false
		for _, f := range z.File {
			total += f.UncompressedSize64
			if total > 200*1024*1024 || f.UncompressedSize64 > 50*1024*1024 {
				return "", "", invalid("DOCX decompressed content is too large")
			}
			if len(z.File) > 5000 {
				return "", "", invalid("DOCX contains too many entries")
			}
			clean := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
			if strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
				return "", "", invalid("Invalid path inside DOCX")
			}
			if strings.HasSuffix(strings.ToLower(f.Name), "vbaproject.bin") {
				return "", "", invalid("Document macros are not supported")
			}
			reader, err := f.Open()
			if err != nil {
				return "", "", invalid("Word file is not a valid DOCX archive")
			}
			if strings.HasSuffix(strings.ToLower(f.Name), ".xml") || strings.HasSuffix(f.Name, ".rels") {
				decoder := xml.NewDecoder(io.LimitReader(reader, 50*1024*1024+1))
				for {
					token, err := decoder.Token()
					if err == io.EOF {
						break
					}
					if err != nil {
						reader.Close()
						return "", "", invalid("Word XML content is invalid")
					}
					if element, ok := token.(xml.StartElement); ok && element.Name.Local == "Relationship" {
						typ, mode := "", ""
						for _, a := range element.Attr {
							if a.Name.Local == "Type" {
								typ = a.Value
							}
							if a.Name.Local == "TargetMode" {
								mode = a.Value
							}
						}
						if strings.HasSuffix(typ, "/image") && mode == "External" {
							reader.Close()
							return "", "", invalid("Embed external Word images before uploading")
						}
					}
				}
			} else if _, err := io.Copy(io.Discard, io.LimitReader(reader, 50*1024*1024+1)); err != nil {
				reader.Close()
				return "", "", invalid("Word file is not a valid DOCX archive")
			}
			reader.Close()
			if f.Name == "word/document.xml" {
				found = true
			}
		}
		if !found {
			return "", "", invalid("DOCX document content is missing")
		}
		return "docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", nil
	default:
		return "", "", invalid("Supported uploads are DOCX, PDF, PNG and JPEG")
	}
}
func (s *Service) storeFile(actor Actor, archiveID, documentID int64, name string, reader io.Reader, asset bool) (*models.ArchiveBlob, error) {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || strings.TrimSpace(name) == "" || len([]rune(name)) > 255 {
		return nil, invalid("Invalid file name")
	}
	if err := s.Prepare(); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(filepath.Join(s.Root, "temporary"), "upload-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	maximum := MaxFileBytes
	if asset {
		maximum = MaxImageBytes
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if size == 0 || size > maximum {
		return nil, invalid("File is empty or exceeds the upload limit")
	}
	kind, mime, err := validateFile(f, name, asset)
	if err != nil {
		return nil, err
	}
	if kind == "pdf" {
		if _, err := s.inspectPDF(f.Name(), hex.EncodeToString(h.Sum(nil))); err != nil {
			return nil, err
		}
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	key := uuid.NewString()
	if err = os.Rename(f.Name(), filepath.Join(s.Root, "objects", key)); err != nil {
		return nil, err
	}
	return &models.ArchiveBlob{ArchiveID: archiveID, DocumentID: documentID, StorageKey: key, Name: name, ContentType: mime, Kind: kind, Size: size, SHA256: hex.EncodeToString(h.Sum(nil)), CreateAt: s.now(), CreateBy: actor.ID, Asset: asset, Staged: asset}, nil
}
func (s *Service) Blob(id int64) (*models.ArchiveBlob, string, error) {
	var b models.ArchiveBlob
	if err := s.DB.First(&b, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, "", missing()
		}
		return nil, "", err
	}
	if _, err := uuid.Parse(b.StorageKey); err != nil {
		return nil, "", fmt.Errorf("invalid archive storage identity")
	}
	return &b, filepath.Join(s.Root, "objects", b.StorageKey), nil
}

type CreateDocument struct {
	Title            string             `json:"title"`
	Purpose          string             `json:"purpose"`
	SourceVersionID  int64              `json:"source_version_id"`
	SourceDocumentID int64              `json:"source_document_id"`
	Body             models.ArchiveJSON `json:"body"`
}

func (s *Service) CreateOnline(actor Actor, archiveID int64, in CreateDocument) (*Detail, error) {
	err := s.mutate(actor, archiveID, 0, "create_document", func(tx *gorm.DB, a *models.ArchiveRecord) error {
		if strings.TrimSpace(in.Title) == "" || len([]rune(in.Title)) > 255 {
			return invalid("Document title is required and limited to 255 characters")
		}
		if !purposeValid(a.Category, in.Purpose) {
			return invalid("Invalid document purpose")
		}
		if in.Purpose == "attendance" {
			return invalid("Training requires teaching materials and an uploaded attendance file")
		}
		body, err := validateBody(tx, a.ID, in.Body, actor.ID)
		if err != nil {
			return err
		}
		if in.SourceDocumentID > 0 {
			var source models.ArchiveDocument
			if err := tx.Where("id = ? AND archive_id = ? AND deleted = ?", in.SourceDocumentID, a.ID, false).First(&source).Error; err != nil {
				return invalid("Source Word document is unavailable")
			}
			if in.SourceVersionID == 0 {
				in.SourceVersionID = source.CurrentVersionID
			}
			var version models.ArchiveDocumentVersion
			if err := tx.Where("id = ? AND document_id = ?", in.SourceVersionID, source.ID).First(&version).Error; err != nil {
				return invalid("Source Word version is unavailable")
			}
			var blob models.ArchiveBlob
			if err := tx.Where("id = ? AND kind = ?", version.BlobID, "docx").First(&blob).Error; err != nil {
				return invalid("Source version is not a Word file")
			}
		}
		d := models.ArchiveDocument{ArchiveID: a.ID, Title: strings.TrimSpace(in.Title), Purpose: in.Purpose, Kind: "online", SourceDocumentID: in.SourceDocumentID, SourceVersionID: in.SourceVersionID, CreateAt: s.now(), UpdateAt: s.now(), CreateBy: actor.ID}
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		if err := s.newDocumentVersion(tx, &d, actor, 0, body, "Created online document"); err != nil {
			return err
		}
		if a.PrimaryDocumentID == 0 {
			a.PrimaryDocumentID = d.ID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(archiveID)
}
func (s *Service) newDocumentVersion(tx *gorm.DB, d *models.ArchiveDocument, actor Actor, blobID int64, body models.ArchiveJSON, description string) error {
	var seq int64
	if err := tx.Model(&models.ArchiveDocumentVersion{}).Where("document_id = ?", d.ID).Select("COALESCE(MAX(sequence), 0)").Scan(&seq).Error; err != nil {
		return err
	}
	v := models.ArchiveDocumentVersion{DocumentID: d.ID, Sequence: seq + 1, BlobID: blobID, Body: body, Size: int64(len(body)), Description: description, CreateAt: s.now(), CreateBy: actor.ID, CreateName: actor.Name}
	if blobID > 0 {
		var b models.ArchiveBlob
		if err := tx.First(&b, blobID).Error; err != nil {
			return err
		}
		v.Size += b.Size
	}
	v.HasContent = blobID > 0
	assets := []int64{}
	if d.Kind == "online" {
		v.HasContent = hasContent(body)
		refs, err := assetReferences(body)
		if err != nil {
			return err
		}
		for id := range refs {
			assets = append(assets, id)
		}
	}
	v.AssetIDs = encode(assets)
	if err := tx.Create(&v).Error; err != nil {
		return err
	}
	d.CurrentVersionID = v.ID
	d.UpdateAt = s.now()
	if err := tx.Save(d).Error; err != nil {
		return err
	}
	if d.Kind == "online" {
		refs, err := assetReferences(body)
		if err != nil {
			return err
		}
		for id := range refs {
			if err := tx.Model(&models.ArchiveBlob{}).Where("id = ? AND archive_id = ?", id, d.ArchiveID).Update("staged", false).Error; err != nil {
				return err
			}
		}
	}
	return s.log(tx, actor, d.ArchiveID, d.ID, "document_version", map[string]interface{}{"version": v.Sequence, "version_id": v.ID, "description": description})
}
func (s *Service) Upload(actor Actor, archiveID, documentID int64, name, purpose, session string, epoch int64, r io.Reader) (*Detail, error) {
	blob, err := s.storeFile(actor, archiveID, documentID, name, r, false)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			os.Remove(filepath.Join(s.Root, "objects", blob.StorageKey))
		}
	}()
	err = s.mutate(actor, archiveID, 0, "upload", func(tx *gorm.DB, a *models.ArchiveRecord) error {
		if !purposeValid(a.Category, purpose) {
			return invalid("Invalid document purpose")
		}
		d := models.ArchiveDocument{}
		if documentID > 0 {
			if err := tx.Where("id = ? AND archive_id = ? AND deleted = ?", documentID, archiveID, false).First(&d).Error; err != nil {
				return missing()
			}
			if d.Kind == "online" {
				return invalid("Online content cannot be replaced by a file")
			}
			if _, err := s.checkLease(tx, actor, documentID, session, epoch); err != nil {
				return err
			}
			d.Kind = blob.Kind
			d.Purpose = purpose
		} else {
			d = models.ArchiveDocument{ArchiveID: archiveID, Title: blob.Name, Purpose: purpose, Kind: blob.Kind, CreateAt: s.now(), UpdateAt: s.now(), CreateBy: actor.ID}
			if err := tx.Create(&d).Error; err != nil {
				return err
			}
		}
		blob.DocumentID = d.ID
		if err := tx.Create(blob).Error; err != nil {
			return err
		}
		if err := s.newDocumentVersion(tx, &d, actor, blob.ID, encode(map[string]interface{}{"annotations": []interface{}{}}), "Uploaded "+blob.Name); err != nil {
			return err
		}
		if documentID > 0 {
			if err := tx.Model(&models.ArchiveEditLease{}).Where("document_id = ?", d.ID).Update("base_version_id", d.CurrentVersionID).Error; err != nil {
				return err
			}
		}
		if a.PrimaryDocumentID == 0 {
			a.PrimaryDocumentID = d.ID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	committed = true
	return s.Get(archiveID)
}
func (s *Service) UploadAsset(actor Actor, archiveID, documentID int64, name string, r io.Reader, lease ...EditInput) (*models.ArchiveBlob, error) {
	var a models.ArchiveRecord
	if err := s.DB.Where("id = ? AND deleted = ?", archiveID, false).First(&a).Error; err != nil {
		return nil, missing()
	}
	blob, err := s.storeFile(actor, archiveID, documentID, name, r, true)
	if err != nil {
		return nil, err
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var record models.ArchiveRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, archiveID).Error; err != nil {
			return err
		}
		if record.Deleted {
			return missing()
		}
		if documentID > 0 {
			d, err := findDocument(tx, documentID)
			if err != nil || d.ArchiveID != archiveID {
				return missing()
			}
			if len(lease) != 1 {
				return conflict("An active editing session is required", nil)
			}
			if _, err := s.checkLease(tx, actor, documentID, lease[0].SessionID, lease[0].Epoch); err != nil {
				return err
			}
		}
		docs, err := documents(tx, archiveID)
		if err != nil {
			return err
		}
		used, err := currentBytes(tx, archiveID, docs)
		if err != nil {
			return err
		}
		var reserved int64
		if err := tx.Model(&models.ArchiveBlob{}).Where("archive_id = ? AND staged = ?", archiveID, true).Select("COALESCE(SUM(size), 0)").Scan(&reserved).Error; err != nil {
			return err
		}
		if used+reserved+blob.Size > MaxArchiveBytes {
			return invalid("Archive image quota exceeded")
		}
		return tx.Create(blob).Error
	})
	if err != nil {
		os.Remove(filepath.Join(s.Root, "objects", blob.StorageKey))
		return nil, err
	}
	return blob, nil
}
func (s *Service) DeleteDocument(actor Actor, archiveID, documentID int64) error {
	return s.mutate(actor, archiveID, 0, "delete_document", func(tx *gorm.DB, a *models.ArchiveRecord) error {
		if err := s.unoccupied(tx, archiveID, documentID); err != nil {
			return err
		}
		res := tx.Model(&models.ArchiveDocument{}).Where("id = ? AND archive_id = ? AND deleted = ?", documentID, archiveID, false).Update("deleted", true)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return missing()
		}
		if a.PrimaryDocumentID == documentID {
			a.PrimaryDocumentID = 0
			var d models.ArchiveDocument
			if err := tx.Where("archive_id = ? AND deleted = ?", archiveID, false).Order("id").First(&d).Error; err == nil {
				a.PrimaryDocumentID = d.ID
			} else if err != gorm.ErrRecordNotFound {
				return err
			}
		}
		return s.log(tx, actor, archiveID, documentID, "delete_document", nil)
	})
}

func (s *Service) UpdateDocument(actor Actor, archiveID, documentID, revision int64, title, purpose string) (*Detail, error) {
	err := s.mutate(actor, archiveID, revision, "update_document", func(tx *gorm.DB, a *models.ArchiveRecord) error {
		if strings.TrimSpace(title) == "" || len([]rune(title)) > 255 {
			return invalid("Document title is required and limited to 255 characters")
		}
		if !purposeValid(a.Category, purpose) {
			return invalid("Invalid document purpose")
		}
		if err := s.unoccupied(tx, archiveID, documentID); err != nil {
			return err
		}
		var d models.ArchiveDocument
		if err := tx.Where("id = ? AND archive_id = ? AND deleted = ?", documentID, archiveID, false).First(&d).Error; err != nil {
			return missing()
		}
		if d.Kind == "online" && purpose == "attendance" {
			return invalid("Training requires teaching materials and an uploaded attendance file")
		}
		before := d
		d.Title = strings.TrimSpace(title)
		d.Purpose = purpose
		d.UpdateAt = s.now()
		if err := tx.Save(&d).Error; err != nil {
			return err
		}
		return s.log(tx, actor, archiveID, documentID, "update_document", map[string]interface{}{"before": before, "after": d})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(archiveID)
}
