package knowledgebase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/ccfos/nightingale/v6/archives"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/workbook"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
	"testing"
)

func excelReport(t *testing.T, last int) []byte {
	t.Helper()
	x := excelize.NewFile()
	defer x.Close()
	x.SetSheetName("Sheet1", "巡检结果")
	x.SetCellValue("巡检结果", "A1", "检查项")
	x.SetCellValue("巡检结果", "B1", "数量")
	for i := 2; i <= last; i++ {
		x.SetCellValue("巡检结果", fmt.Sprintf("A%d", i), "模拟接口")
		x.SetCellValue("巡检结果", fmt.Sprintf("B%d", i), i)
	}
	x.NewSheet("验收记录")
	x.SetCellValue("验收记录", "A1", "排障说明：先核查接口日志")
	b, e := x.WriteToBuffer()
	require.NoError(t, e)
	return b.Bytes()
}
func TestExcelArchiveKnowledgeLifecycle(t *testing.T) {
	s := fixture(t)
	actor := archives.Actor{ID: 1, Name: "reader"}
	a, e := s.Archives.Create(actor, "other", archives.Metadata{Title: "Excel隔离模拟报告", CustomType: "测试资料"})
	require.NoError(t, e)
	data := excelReport(t, 122)
	a, e = s.Archives.Upload(actor, a.ID, 0, "巡检报告.xlsx", "other", "", 0, bytes.NewReader(data))
	require.NoError(t, e)
	require.Len(t, a.Documents, 1)
	d := a.Documents[0]
	require.Equal(t, "xlsx", d.Kind)
	r, e := s.Archives.Workbook(context.Background(), d.ID, d.CurrentVersionID, workbook.Query{Operation: "sheets"})
	require.NoError(t, e)
	require.Len(t, r.Sheets, 2)
	s.Summarize = func(context.Context, string) (string, error) { return "", errors.New("model temporarily unavailable") }
	require.NoError(t, s.Scan())
	require.NoError(t, s.Scan())
	worked, e := s.ProcessOne(context.Background())
	require.True(t, worked)
	require.NoError(t, e)
	var k models.LibraryVersion
	require.NoError(t, s.DB.First(&k).Error)
	require.Equal(t, "ready", k.State)
	require.Equal(t, "retry", k.SummaryState)
	hits, e := s.Search(1, "排障说明")
	require.NoError(t, e)
	require.NotEmpty(t, hits)
	require.Contains(t, hits[0].Heading, "验收记录")
	r, label, link, e := s.Workbook(1, k.ID, workbook.Query{Sheet: "巡检结果", Range: "A2:B122", Operation: "sum", Column: "B", Limit: 2})
	require.NoError(t, e)
	require.Equal(t, 121, r.MatchedRows)
	require.Equal(t, "7502.000000000000", r.Sum)
	require.Contains(t, label, "V1")
	require.Contains(t, link, "sheet=")
	r, _, _, e = s.Workbook(1, k.ID, workbook.Query{Sheet: "巡检结果", Range: "A2:B122", Operation: "read", Limit: 2, Offset: 120})
	require.NoError(t, e)
	require.Len(t, r.Rows, 1)
	require.Equal(t, 122, r.Rows[0][0].Row)
	_, _, _, e = s.Workbook(0, k.ID, workbook.Query{Operation: "sheets"})
	require.Error(t, e)
	_, e = s.Archives.Upload(actor, a.ID, d.ID, "新版.xlsx", "other", "", 0, bytes.NewReader(data))
	require.Error(t, e, "replacement requires lease")
	lease, e := s.Archives.Acquire(actor, d.ID)
	require.NoError(t, e)
	a, e = s.Archives.Upload(actor, a.ID, d.ID, "新版.xlsx", "other", lease.Lease.SessionID, lease.Lease.Epoch, bytes.NewReader(excelReport(t, 3)))
	require.NoError(t, e)
	_, _, _, e = s.Workbook(1, k.ID, workbook.Query{Operation: "sheets"})
	require.Error(t, e, "old index invalidates immediately")
	r, e = s.Archives.Workbook(context.Background(), d.ID, d.CurrentVersionID, workbook.Query{Operation: "sheets"})
	require.NoError(t, e)
	require.Equal(t, 122, r.Sheets[0].Rows, "historical original remains available")
	require.NoError(t, s.Scan())
	_, e = s.ProcessOne(context.Background())
	require.NoError(t, e)
	var count int64
	require.NoError(t, s.DB.Model(&models.LibraryWorkbook{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.NoError(t, s.DB.Model(&models.ArchiveRecord{}).Where("id=?", a.ID).Update("deleted", true).Error)
	_, e = s.Archives.Workbook(context.Background(), d.ID, 0, workbook.Query{Operation: "sheets"})
	require.Error(t, e)
}
