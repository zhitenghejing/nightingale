package knowledge

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"github.com/google/uuid"
)

const MaxSourceBytes = 16 << 20

func (s *Store) AddSource(user, group int64, name, note string, r io.Reader) (*models.KnowledgeSource, error) {
	if _, err := opsauth.Check(s.DB, user, group, true); err != nil {
		return nil, err
	}
	if len(note) > 1000 {
		return nil, opsauth.Fail(422, "source_note", "Source description exceeds 1000 bytes")
	}
	name = filepath.Base(name)
	if name == "." || name == "" || len(name) > 255 {
		return nil, opsauth.Fail(422, "source_name", "Invalid source filename")
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > MaxSourceBytes {
		return nil, opsauth.Fail(422, "source_size", "Source must contain between 1 byte and 16 MiB")
	}
	media := ""
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".txt":
		if !utf8.Valid(data) {
			return nil, opsauth.Fail(422, "source_encoding", "Text source must use UTF-8")
		}
		media = "text/plain; charset=utf-8"
	case ".pdf":
		if !bytes.HasPrefix(data, []byte("%PDF-")) {
			return nil, opsauth.Fail(422, "source_format", "Invalid PDF source")
		}
		media = "application/pdf"
	case ".docx":
		z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if e != nil {
			return nil, opsauth.Fail(422, "source_format", "Invalid DOCX source")
		}
		found := false
		var total uint64
		for _, f := range z.File {
			if f.UncompressedSize64 > (64<<20)-total {
				return nil, opsauth.Fail(422, "source_format", "Unsafe or oversized DOCX source")
			}
			total += f.UncompressedSize64
			if total > 64<<20 || strings.Contains(strings.ToLower(f.Name), "vbaproject") || strings.Contains(f.Name, "..") {
				return nil, opsauth.Fail(422, "source_format", "Unsafe or oversized DOCX source")
			}
			if f.Name == "word/document.xml" {
				found = true
			}
		}
		if !found {
			return nil, opsauth.Fail(422, "source_format", "DOCX document content is missing")
		}
		media = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return nil, opsauth.Fail(422, "source_format", "Supported sources are Markdown, TXT, PDF and DOCX")
	}
	if s.DataDir == "" {
		return nil, opsauth.Fail(503, "source_storage", "Private source storage is not configured")
	}
	dir := filepath.Join(s.DataDir, "sources")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	key := uuid.NewString()
	path := filepath.Join(dir, key)
	if err = os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}
	row := models.KnowledgeSource{GroupID: group, Name: name, MediaType: media, StorageKey: key, SHA256: Digest(data), Size: int64(len(data)), Description: note, CreatedBy: user, CreatedAt: s.now()}
	if err = s.DB.Create(&row).Error; err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return &row, nil
}
func (s *Store) Sources(user, group int64) ([]models.KnowledgeSource, error) {
	if _, err := opsauth.Check(s.DB, user, group, false); err != nil {
		return nil, err
	}
	rows := []models.KnowledgeSource{}
	err := s.DB.Where("group_id = ?", group).Order("id DESC").Limit(200).Find(&rows).Error
	return rows, err
}
func (s *Store) Source(user, id int64) (*models.KnowledgeSource, []byte, error) {
	var row models.KnowledgeSource
	if err := s.DB.First(&row, id).Error; err != nil {
		return nil, nil, err
	}
	if _, err := opsauth.Check(s.DB, user, row.GroupID, false); err != nil {
		return nil, nil, err
	}
	if _, err := uuid.Parse(row.StorageKey); err != nil {
		return nil, nil, opsauth.Fail(500, "source_storage", "Invalid private source identity")
	}
	file, err := os.Open(filepath.Join(s.DataDir, "sources", row.StorageKey))
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxSourceBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if Digest(data) != row.SHA256 || int64(len(data)) != row.Size {
		return nil, nil, opsauth.Fail(http.StatusConflict, "source_integrity", "Source integrity check failed")
	}
	return &row, data, nil
}
