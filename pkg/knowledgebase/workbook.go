package knowledgebase

import (
	"encoding/json"
	"fmt"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/workbook"
	"net/url"
)

func (s *Store) Workbook(user, id int64, q workbook.Query) (*workbook.Result, string, string, error) {
	k, e := s.Version(user, id)
	if e != nil {
		return nil, "", "", e
	}
	if k.State != "ready" {
		return nil, "", "", fmt.Errorf("资料尚未处理完成")
	}
	var stored models.LibraryWorkbook
	if e = s.DB.First(&stored, "version_id = ?", id).Error; e != nil {
		return nil, "", "", fmt.Errorf("此资料没有可查询的 Excel 工作簿")
	}
	var b workbook.Book
	if e = json.Unmarshal([]byte(stored.Data), &b); e != nil {
		return nil, "", "", e
	}
	r, e := b.Query(q)
	if e != nil {
		return nil, "", "", e
	}
	link := fmt.Sprintf("/archives/%d/documents/%d?version=%d&sheet=%s&range=%s", k.ArchiveID, k.DocumentID, k.SourceVersionID, url.QueryEscape(q.Sheet), url.QueryEscape(q.Range))
	location := q.Sheet + " " + q.Range
	if q.Operation == "sheets" {
		location = "工作簿目录"
	}
	return r, fmt.Sprintf("%s · V%d · %s", k.Title, k.Sequence, location), link, nil
}
