// Package workbook reads bounded, local OOXML reports. It never evaluates
// formulas, follows external relationships, or treats report rows as assets.
package workbook

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"
)

const MaxCells = 50000
const MaxRows = 100000
const MaxCols = 256

var readers = make(chan struct{}, 2)

type Cell struct {
	Address      string `json:"address"`
	Row          int    `json:"row"`
	Column       int    `json:"column"`
	Value        string `json:"value"`
	Raw          string `json:"raw"`
	Type         string `json:"type"`
	Formula      string `json:"formula,omitempty"`
	MissingCache bool   `json:"missing_cache,omitempty"`
}
type Sheet struct {
	Name          string   `json:"name"`
	Hidden        bool     `json:"hidden"`
	Rows          int      `json:"rows"`
	Columns       int      `json:"columns"`
	HiddenRows    []int    `json:"hidden_rows"`
	HiddenColumns []int    `json:"hidden_columns"`
	Merges        []string `json:"merges"`
	Cells         []Cell   `json:"cells,omitempty"`
}
type Book struct {
	Sheets   []Sheet  `json:"sheets"`
	Warnings []string `json:"warnings"`
}

func Parse(ctx context.Context, filename string) (*Book, error) {
	f, e := os.Open(filename)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return nil, e
	}
	return Read(ctx, f, st.Size())
}
func Read(ctx context.Context, r io.ReaderAt, size int64) (book *Book, err error) {
	// A malformed third-party workbook must not panic the persistent index worker.
	defer func() {
		if recover() != nil {
			book = nil
			err = fmt.Errorf("无法解析 Excel，请检查文件是否损坏或包含不支持的格式")
		}
	}()
	select {
	case readers <- struct{}{}:
		defer func() { <-readers }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if size < 1 || size > 50<<20 {
		return nil, fmt.Errorf("Excel 原件超过 50 MiB 或为空")
	}
	z, e := zip.NewReader(r, size)
	if e != nil {
		return nil, fmt.Errorf("文件不是有效的 XLSX；旧 XLS 或加密文件请另存为未加密的 .xlsx")
	}
	if len(z.File) > 3000 {
		return nil, fmt.Errorf("Excel ZIP 条目超过 3000")
	}
	var total uint64
	found := false
	seen := map[string]bool{}
	sheetXML := map[string][]byte{}
	var workbookXML, relsXML []byte
	warnings := []string{"仅提取单元格及合并范围；图片、图表、批注和打印版式不参与知识检索。公式只读取已保存的缓存，不重新计算；外部链接不访问。隐藏数据保留并明确标记。"}
	for _, entry := range z.File {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		name := strings.ReplaceAll(entry.Name, "\\", "/")
		clean := path.Clean(name)
		if clean != strings.TrimSuffix(name, "/") || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") {
			return nil, fmt.Errorf("Excel 包含非法路径")
		}
		if seen[clean] {
			return nil, fmt.Errorf("Excel ZIP 包含重复条目")
		}
		seen[clean] = true
		if entry.FileInfo().IsDir() {
			continue
		}
		lower := strings.ToLower(clean)
		if strings.Contains(lower, "vbaproject") || strings.Contains(lower, "/embeddings/") || strings.Contains(lower, "activex") {
			return nil, fmt.Errorf("不支持包含宏、ActiveX 或嵌入对象的 Excel")
		}
		if entry.UncompressedSize64 > 32<<20 || entry.UncompressedSize64 > (128<<20)-total {
			return nil, fmt.Errorf("Excel 解压内容超过限制（总计 128 MiB、单项 32 MiB）")
		}
		total += entry.UncompressedSize64
		rc, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("Excel ZIP 损坏")
		}
		b, err := io.ReadAll(io.LimitReader(rc, 32<<20+1))
		rc.Close()
		if err != nil || len(b) > 32<<20 {
			return nil, fmt.Errorf("Excel ZIP 内容损坏或超过限制")
		}
		if clean == "xl/workbook.xml" {
			workbookXML = b
		}
		if clean == "xl/_rels/workbook.xml.rels" {
			relsXML = b
		}
		if clean == "[Content_Types].xml" {
			found = strings.Contains(string(b), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml")
			if strings.Contains(strings.ToLower(string(b)), "macroenabled") {
				return nil, fmt.Errorf("不支持启用宏的 Excel")
			}
		}
		if strings.HasPrefix(clean, "xl/worksheets/") && strings.HasSuffix(clean, ".xml") {
			sheetXML[clean] = b
		}
	}
	if !found {
		return nil, fmt.Errorf("文件内容不是普通 XLSX 工作簿")
	}
	if e := preflight(ctx, sheetXML); e != nil {
		return nil, e
	}
	// Excelize also enforces its own decompression limits. ReaderAt is wrapped in
	// a section reader; no network resolver or formula calculator is installed.
	x, e := excelize.OpenReader(io.NewSectionReader(r, 0, size), excelize.Options{UnzipSizeLimit: 128 << 20, UnzipXMLSizeLimit: 32 << 20})
	if e != nil {
		return nil, fmt.Errorf("无法读取 Excel：%w", e)
	}
	defer x.Close()
	names := x.GetSheetList()
	if len(names) > 32 {
		return nil, fmt.Errorf("Excel 工作表超过 32 张")
	}
	var wb struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if xml.Unmarshal(workbookXML, &wb) != nil || xml.Unmarshal(relsXML, &rels) != nil {
		return nil, fmt.Errorf("Excel 关系目录损坏")
	}
	paths := map[string]string{}
	byID := map[string]string{}
	for _, rel := range rels.Items {
		if rel.Mode != "External" {
			target := path.Clean(path.Join("xl", rel.Target))
			if strings.HasPrefix(rel.Target, "/") {
				target = strings.TrimPrefix(path.Clean(rel.Target), "/")
			}
			byID[rel.ID] = target
		}
	}
	for _, sheet := range wb.Sheets {
		paths[sheet.Name] = byID[sheet.RID]
	}
	out := &Book{Warnings: warnings, Sheets: []Sheet{}}
	count := 0
	textBytes, metadataCount := 0, 0
	// Obtain addresses from the actual sheet XML, avoiding dense allocation from
	// an attacker-controlled dimension such as A1:XFD1048576.
	for _, name := range names {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Excelize's sheet index is presentation order, not OOXML file number.
		// GetSheetPath resolves workbook relationships including reordered sheets.
		sheetPath := paths[name]
		b, ok := sheetXML[strings.TrimPrefix(sheetPath, "/")]
		if !ok {
			return nil, fmt.Errorf("工作表数据缺失：%s", name)
		}
		s := Sheet{Name: name, Cells: []Cell{}, Merges: []string{}, HiddenRows: []int{}, HiddenColumns: []int{}}
		visible, err := x.GetSheetVisible(name)
		if err != nil {
			return nil, err
		}
		s.Hidden = !visible
		dec := xml.NewDecoder(strings.NewReader(string(b)))
		for {
			tok, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("工作表 XML 损坏：%s", name)
			}
			start, ok := tok.(xml.StartElement)
			if !ok {
				continue
			}
			if start.Name.Local == "row" {
				var row int
				hidden := false
				for _, a := range start.Attr {
					if a.Name.Local == "r" {
						fmt.Sscan(a.Value, &row)
					}
					if a.Name.Local == "hidden" {
						hidden = a.Value == "1" || a.Value == "true"
					}
				}
				if row > MaxRows {
					return nil, fmt.Errorf("Excel 行元数据超过 %d 行限制", MaxRows)
				}
				if hidden && row > 0 {
					if metadataCount+len(s.HiddenRows)+len(s.HiddenColumns)+len(s.Merges) >= 1024 {
						return nil, fmt.Errorf("Excel 隐藏/合并元数据超过 1024 项")
					}
					s.HiddenRows = append(s.HiddenRows, row)
				}
			}
			if start.Name.Local == "col" {
				var min, max int
				hidden := false
				for _, a := range start.Attr {
					switch a.Name.Local {
					case "min":
						fmt.Sscan(a.Value, &min)
					case "max":
						fmt.Sscan(a.Value, &max)
					case "hidden":
						hidden = a.Value == "1" || a.Value == "true"
					}
				}
				if hidden {
					if max > MaxCols {
						max = MaxCols
					}
					for c := min; c > 0 && c <= max; c++ {
						if metadataCount+len(s.HiddenRows)+len(s.HiddenColumns)+len(s.Merges) >= 1024 {
							return nil, fmt.Errorf("Excel 隐藏/合并元数据超过 1024 项")
						}
						s.HiddenColumns = append(s.HiddenColumns, c)
					}
				}
			}
			if start.Name.Local != "c" {
				continue
			}
			var v struct {
				Address string  `xml:"r,attr"`
				Type    string  `xml:"t,attr"`
				Value   *string `xml:"v"`
				Formula *string `xml:"f"`
				Inline  *struct {
					Text string `xml:",innerxml"`
				} `xml:"is"`
			}
			if err := dec.DecodeElement(&v, &start); err != nil {
				return nil, err
			}
			if v.Value == nil && v.Formula == nil && v.Inline == nil {
				continue
			}
			col, row, err := excelize.CellNameToCoordinates(v.Address)
			if err != nil || col > MaxCols || row > MaxRows {
				return nil, fmt.Errorf("Excel 实际数据范围超过 %d 行 / %d 列", MaxRows, MaxCols)
			}
			count++
			if count > MaxCells {
				return nil, fmt.Errorf("Excel 非空单元格超过 %d；请拆分资料后上传", MaxCells)
			}
			if count%100 == 0 && ctx.Err() != nil {
				return nil, ctx.Err()
			}
			value, err := x.GetCellValue(name, v.Address)
			if err != nil {
				return nil, err
			}
			raw, err := x.GetCellValue(name, v.Address, excelize.Options{RawCellValue: true})
			if err != nil {
				return nil, err
			}
			c := Cell{Address: v.Address, Row: row, Column: col, Value: value, Raw: raw, Type: v.Type}
			if c.Type == "" {
				c.Type = "n"
			}
			if v.Formula != nil {
				c.Formula = *v.Formula
				c.MissingCache = v.Value == nil || *v.Value == ""
				if c.MissingCache {
					c.Value = "〔公式缺少缓存值〕"
				}
			}
			textBytes += len(c.Value) + len(c.Raw) + len(c.Formula) + len(c.Address) + 80
			if textBytes > 8<<20 {
				return nil, fmt.Errorf("Excel 展开后的单元格文本超过 8 MiB；请拆分资料")
			}
			if len(c.Value) > 32768 || len(c.Formula) > 32768 {
				return nil, fmt.Errorf("Excel 单元格内容超过限制")
			}
			s.Cells = append(s.Cells, c)
			if row > s.Rows {
				s.Rows = row
			}
			if col > s.Columns {
				s.Columns = col
			}
		}
		merges, err := x.GetMergeCells(name)
		if err != nil {
			return nil, err
		}
		if len(merges) > MaxCells {
			return nil, fmt.Errorf("Excel 合并范围过多")
		}
		for _, m := range merges {
			a, b := m.GetStartAxis(), m.GetEndAxis()
			c, row, err := excelize.CellNameToCoordinates(b)
			if err != nil || c > MaxCols || row > MaxRows {
				return nil, fmt.Errorf("Excel 合并范围超过限制")
			}
			if c > s.Columns {
				s.Columns = c
			}
			if row > s.Rows {
				s.Rows = row
			}
			if metadataCount+len(s.HiddenRows)+len(s.HiddenColumns)+len(s.Merges) >= 1024 {
				return nil, fmt.Errorf("Excel 隐藏/合并元数据超过 1024 项")
			}
			s.Merges = append(s.Merges, a+":"+b)
		}
		metadataCount += len(s.HiddenRows) + len(s.HiddenColumns) + len(s.Merges)
		if metadataCount > 1024 {
			return nil, fmt.Errorf("Excel 隐藏行列及合并范围元数据超过 1024 项；请拆分资料")
		}
		sort.Slice(s.Cells, func(i, j int) bool {
			a, b := s.Cells[i], s.Cells[j]
			if a.Row == b.Row {
				return a.Column < b.Column
			}
			return a.Row < b.Row
		})
		out.Sheets = append(out.Sheets, s)
	}
	return out, nil
}
