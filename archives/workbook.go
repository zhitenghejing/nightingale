package archives

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/ccfos/nightingale/v6/pkg/workbook"
	"io"
	"os"
)

// Workbook reads the exact original, including historical versions. It uses the
// same archive read policy as Document; Agent reads use stricter current-index checks.
func (s *Service) Workbook(ctx context.Context, id, version int64, q workbook.Query) (*workbook.Result, error) {
	d, e := findDocument(s.DB, id)
	if e != nil {
		return nil, e
	}
	doc, e := s.Document(d.ID, version)
	if e != nil {
		return nil, e
	}
	if doc.Kind != "xlsx" || doc.File == nil {
		return nil, invalid("此文档不是 Excel")
	}
	blob, p, e := s.Blob(doc.File.ID)
	if e != nil {
		return nil, e
	}
	if blob.DocumentID != d.ID || blob.ArchiveID != d.ArchiveID {
		return nil, missing()
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, blob.Size+1))
	f.Close()
	if e != nil || n != blob.Size || hex.EncodeToString(h.Sum(nil)) != blob.SHA256 {
		return nil, invalid("Excel 原件校验失败")
	}
	b, e := workbook.Parse(ctx, p)
	if e != nil {
		return nil, invalid(e.Error())
	}
	r, e := b.Query(q)
	if e != nil {
		return nil, invalid(e.Error())
	}
	return r, nil
}
