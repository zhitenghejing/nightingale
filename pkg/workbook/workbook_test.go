package workbook

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"github.com/xuri/excelize/v2"
	"io"
	"strings"
	"testing"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	x := excelize.NewFile()
	defer x.Close()
	x.SetSheetName("Sheet1", "巡检报告")
	x.SetCellValue("巡检报告", "A1", "接口")
	x.SetCellValue("巡检报告", "B1", "耗时")
	x.SetCellValue("巡检报告", "A2", "0007")
	x.SetCellValue("巡检报告", "B2", 0.1)
	x.SetCellValue("巡检报告", "A3", "超时")
	x.SetCellValue("巡检报告", "B3", 0.2)
	x.SetCellFormula("巡检报告", "C3", "SUM(B2:B3)")
	x.MergeCell("巡检报告", "A5", "C5")
	x.SetCellValue("巡检报告", "A5", "合并说明")
	x.SetRowVisible("巡检报告", 3, false)
	x.NewSheet("实施记录")
	x.SetCellValue("实施记录", "A1", "另一张工作表内容")
	b, e := x.WriteToBuffer()
	if e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestReadAndStatistics(t *testing.T) {
	data := fixture(t)
	b, e := Read(context.Background(), bytes.NewReader(data), int64(len(data)))
	if e != nil {
		t.Fatal(e)
	}
	if len(b.Sheets) != 2 || b.Sheets[0].Cells[2].Value != "0007" || len(b.Sheets[0].HiddenRows) != 1 {
		t.Fatalf("%+v", b)
	}
	r, e := b.Query(Query{Sheet: "巡检报告", Range: "A2:B3", Operation: "sum", Column: "B"})
	if e != nil || r.Sum != "0.300000000000" || r.MatchedRows != 2 || !r.Complete {
		t.Fatalf("%+v %v", r, e)
	}
	r, e = b.Query(Query{Sheet: "巡检报告", Range: "A2:C3", Operation: "sum", Column: "C"})
	if e != nil || r.Complete {
		t.Fatalf("missing cache accepted %+v %v", r, e)
	}
	md, parts, e := b.Markdown()
	if e != nil || !strings.Contains(md, "实施记录") || len(parts) < 4 {
		t.Fatal(md, e)
	}
	r, e = b.Query(Query{Sheet: "巡检报告", Range: "A2:B3", Operation: "count", Filter: &Filter{Column: "A", Operator: "eq", Value: "超时"}})
	if e != nil || r.MatchedRows != 1 {
		t.Fatal(r, e)
	}
}
func TestRejectsMacroAndSparseOversize(t *testing.T) {
	for _, tc := range []struct{ name, xml string }{{"xl/vbaProject.bin", "macro"}, {"xl/worksheets/sheet1.xml", `<worksheet><sheetData><row r="1"><c r="A1"><v>1</v></c></row><row r="1048576"><c r="XFD1048576"><v>1</v></c></row></sheetData></worksheet>`}} {
		src := fixture(t)
		zr, _ := zip.NewReader(bytes.NewReader(src), int64(len(src)))
		var out bytes.Buffer
		zw := zip.NewWriter(&out)
		for _, f := range zr.File {
			if f.Name == tc.name {
				continue
			}
			w, _ := zw.Create(f.Name)
			r, _ := f.Open()
			io.Copy(w, r)
			r.Close()
		}
		w, _ := zw.Create(tc.name)
		w.Write([]byte(tc.xml))
		zw.Close()
		if _, e := Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len())); e == nil {
			t.Fatalf("accepted %s", tc.name)
		}
	}
}

func TestFormatsCachedFormulaAndCoverage(t *testing.T) {
	x := excelize.NewFile()
	defer x.Close()
	x.SetColWidth("Sheet1", "A", "XFD", 11)
	x.SetCellValue("Sheet1", "A1", 7)
	custom := "0000"
	style, e := x.NewStyle(&excelize.Style{CustomNumFmt: &custom})
	if e != nil {
		t.Fatal(e)
	}
	x.SetCellStyle("Sheet1", "A1", "A1", style)
	x.SetCellValue("Sheet1", "B1", 0.125)
	style, _ = x.NewStyle(&excelize.Style{NumFmt: 10})
	x.SetCellStyle("Sheet1", "B1", "B1", style)
	x.SetCellFormula("Sheet1", "C1", "SUM(1,2)")
	x.SetCellValue("Sheet1", "D1", 45292)
	style, _ = x.NewStyle(&excelize.Style{NumFmt: 14})
	x.SetCellStyle("Sheet1", "D1", "D1", style)
	x.NewSheet("隐藏资料")
	x.SetSheetVisible("隐藏资料", false)
	x.SetCellValue("隐藏资料", "A1", "保留隐藏文字")
	x.SetColVisible("Sheet1", "B", false)
	buf, e := x.WriteToBuffer()
	if e != nil {
		t.Fatal(e)
	}
	zr, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		if f.Name == "xl/worksheets/sheet1.xml" {
			b = bytes.Replace(b, []byte(`<c r="C1" t="str">`), []byte(`<c r="C1">`), 1)
			b = bytes.Replace(b, []byte("<f>SUM(1,2)</f>"), []byte("<f>SUM(1,2)</f><v>3</v>"), 1)
		}
		w, _ := zw.Create(f.Name)
		w.Write(b)
	}
	zw.Close()
	book, e := Read(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()))
	if e != nil {
		t.Fatal(e)
	}
	c := book.Sheets[0].Cells
	if c[0].Value != "0007" || c[1].Value != "12.50%" || c[2].Raw != "3" || c[2].MissingCache || c[3].Value == c[3].Raw || !book.Sheets[1].Hidden || len(book.Sheets[0].HiddenColumns) != 1 {
		t.Fatalf("%+v", book)
	}
	r, e := book.Query(Query{Operation: "sum", Sheet: "Sheet1", Range: "C1:C1", Column: "C"})
	if e != nil || r.Sum != "3.000000000000" || !r.Complete {
		t.Fatal(r, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Read(ctx, bytes.NewReader(out.Bytes()), int64(out.Len())); e == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestIncompleteFiltersAndOutputBudget(t *testing.T) {
	b := &Book{Sheets: []Sheet{{Name: "数据", Rows: 3, Columns: 2, Cells: []Cell{
		{Address: "A2", Row: 2, Column: 1, Formula: "UNKNOWN()", MissingCache: true},
		{Address: "B2", Row: 2, Column: 2, Type: "n", Raw: "1", Value: "1"},
		{Address: "A3", Row: 3, Column: 1, Type: "s", Raw: "正常", Value: "正常"},
		{Address: "B3", Row: 3, Column: 2, Type: "e", Raw: "#DIV/0!", Value: "#DIV/0!"},
	}}}}
	r, e := b.Query(Query{Sheet: "数据", Range: "A2:B3", Operation: "count", Filter: &Filter{Column: "A", Operator: "eq", Value: "正常"}})
	if e != nil || r.Complete || r.MatchedRows != 1 {
		t.Fatal(r, e)
	}
	r, e = b.Query(Query{Sheet: "数据", Range: "A2:B3", Operation: "sum", Column: "B"})
	if e != nil || r.Complete || r.NumericCells != 1 || r.Sum != "1.000000000000" {
		t.Fatal(r, e)
	}
	b.Sheets[0].Cells = []Cell{{Address: "A1", Row: 1, Column: 1, Type: "s", Value: strings.Repeat("字", 10000)}, {Address: "B1", Row: 1, Column: 2, Type: "s", Value: strings.Repeat("字", 10000)}}
	if _, e := b.Query(Query{Sheet: "数据", Range: "A1:B1", Operation: "read"}); e == nil {
		t.Fatal("silently truncated oversized row")
	}
	if _, e := b.Query(Query{Sheet: "数据", Range: "A1:A1", Operation: "read"}); e != nil {
		t.Fatal(e)
	}
}
func TestRejectsExcessWorksheets(t *testing.T) {
	x := excelize.NewFile()
	defer x.Close()
	for i := 1; i <= 32; i++ {
		x.NewSheet(fmt.Sprintf("Sheet%d-extra", i))
	}
	buf, e := x.WriteToBuffer()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Read(context.Background(), bytes.NewReader(buf.Bytes()), int64(buf.Len())); e == nil || !strings.Contains(e.Error(), "32") {
		t.Fatal("accepted >32 sheets", e)
	}
}

func TestSharedStringExpansionIsBounded(t *testing.T) {
	x := excelize.NewFile()
	defer x.Close()
	for i := 1; i <= 150; i++ {
		x.SetCellStr("Sheet1", fmt.Sprintf("A%d", i), strings.Repeat("x", 30000))
	}
	buf, e := x.WriteToBuffer()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Read(context.Background(), bytes.NewReader(buf.Bytes()), int64(buf.Len())); e == nil || !strings.Contains(e.Error(), "8 MiB") {
		t.Fatal("accepted amplified shared strings", e)
	}
}

func TestByteBudgetPaginationNeverSkipsRows(t *testing.T) {
	b := &Book{Sheets: []Sheet{{Name: "数据", Rows: 3, Columns: 1, Cells: []Cell{
		{Address: "A1", Row: 1, Column: 1, Value: strings.Repeat("x", 30000)},
		{Address: "A2", Row: 2, Column: 1, Value: strings.Repeat("y", 30000)},
		{Address: "A3", Row: 3, Column: 1, Value: "last"},
	}}}}
	first, e := b.Query(Query{Sheet: "数据", Range: "A1:A3", Operation: "read"})
	if e != nil || len(first.Rows) != 1 || !first.HasMore {
		t.Fatal(first, e)
	}
	second, e := b.Query(Query{Sheet: "数据", Range: "A1:A3", Operation: "read", Offset: 1})
	if e != nil || len(second.Rows) != 2 || second.Rows[0][0].Row != 2 || second.HasMore {
		t.Fatal(second, e)
	}
}
