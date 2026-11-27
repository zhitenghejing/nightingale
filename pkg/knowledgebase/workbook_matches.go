package knowledgebase

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/workbook"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

// WorkbookRecord binds exact identifier cells to their full original row.
// Context is the original first three rows, not an inferred header schema.
type WorkbookRecord struct {
	VersionID     int64                  `json:"version_id"`
	SectionID     int64                  `json:"section_id"`
	Title         string                 `json:"title"`
	Sequence      int64                  `json:"sequence"`
	Sheet         string                 `json:"sheet"`
	Row           int                    `json:"row"`
	MatchingCells []string               `json:"matching_cells"`
	ContextRows   [][]workbook.Cell      `json:"context_rows"`
	Result        *workbook.Result       `json:"result"`
	Citation      string                 `json:"citation"`
	URL           string                 `json:"url"`
	ReadParams    map[string]interface{} `json:"read_params"`
}

func (s *Store) WorkbookMatches(user, version, section int64, identifiers []string) ([]WorkbookRecord, error) {
	out := []WorkbookRecord{}
	if len(identifiers) == 0 {
		return out, nil
	}
	k, err := s.Version(user, version)
	if err != nil {
		return nil, err
	}
	var stored models.LibraryWorkbook
	if err = s.DB.First(&stored, "version_id=?", version).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return out, nil
		}
		return nil, err
	}
	var book workbook.Book
	if err = json.Unmarshal([]byte(stored.Data), &book); err != nil {
		return nil, err
	}
	scope := SourceScope{Identifiers: identifiers}
	for _, sheet := range book.Sheets {
		rows := map[int][]string{}
		context := map[int][]workbook.Cell{}
		for _, cell := range sheet.Cells {
			if cell.Row <= 3 {
				context[cell.Row] = append(context[cell.Row], cell)
			}
			if scope.Matches("", cell.Value) {
				rows[cell.Row] = append(rows[cell.Row], cell.Address)
			}
		}
		numbers := []int{}
		for row := range rows {
			numbers = append(numbers, row)
		}
		sort.Ints(numbers)
		col, _ := excelize.ColumnNumberToName(sheet.Columns)
		for _, row := range numbers {
			if len(out) >= 8 {
				return out, nil
			}
			cellRange := fmt.Sprintf("A%d:%s%d", row, col, row)
			result, err := book.Query(workbook.Query{Operation: "read", Sheet: sheet.Name, Range: cellRange, Limit: 1})
			if err != nil {
				return nil, err
			}
			headers := [][]workbook.Cell{}
			for n := 1; n <= 3; n++ {
				if cells := context[n]; len(cells) > 0 {
					headers = append(headers, cells)
				}
			}
			link := fmt.Sprintf("/archives/%d/documents/%d?version=%d&sheet=%s&range=%s", k.ArchiveID, k.DocumentID, k.SourceVersionID, url.QueryEscape(sheet.Name), url.QueryEscape(cellRange))
			record := WorkbookRecord{VersionID: version, SectionID: section, Title: k.Title, Sequence: k.Sequence, Sheet: sheet.Name, Row: row, MatchingCells: rows[row], ContextRows: headers, Result: result, URL: link, Citation: fmt.Sprintf("[%s · V%d · %s %s](%s)", k.Title, k.Sequence, sheet.Name, cellRange, link), ReadParams: map[string]interface{}{"version_id": version, "sheet": sheet.Name, "range": cellRange, "limit": 1}}
			raw, _ := json.Marshal(record)
			if len(raw) > 48<<10 {
				record.Result = &workbook.Result{Operation: "read", Sheet: sheet.Name, Range: cellRange, Complete: false, Issues: []string{"匹配行较长，须用read_params缩小列范围后读取；未返回完整行"}}
				record.ContextRows = nil
			}
			out = append(out, record)
		}
	}
	return out, nil
}
