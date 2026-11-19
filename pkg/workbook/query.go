package workbook

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/xuri/excelize/v2"
)

type Filter struct {
	Column   string `json:"column"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}
type Query struct {
	Sheet     string  `json:"sheet"`
	Range     string  `json:"range"`
	Operation string  `json:"operation"`
	Column    string  `json:"column"`
	Filter    *Filter `json:"filter,omitempty"`
	Offset    int     `json:"offset"`
	Limit     int     `json:"limit"`
}
type Result struct {
	Sheets       []Sheet  `json:"sheets,omitempty"`
	Sheet        string   `json:"sheet,omitempty"`
	Range        string   `json:"range,omitempty"`
	Operation    string   `json:"operation"`
	Column       string   `json:"column,omitempty"`
	Filter       *Filter  `json:"filter,omitempty"`
	Offset       int      `json:"offset"`
	Limit        int      `json:"limit"`
	Rows         [][]Cell `json:"rows"`
	MatchedRows  int      `json:"matched_rows"`
	ScannedRows  int      `json:"scanned_rows"`
	NumericCells int      `json:"numeric_cells"`
	Sum          string   `json:"sum,omitempty"`
	Complete     bool     `json:"complete"`
	Issues       []string `json:"issues"`
	HasMore      bool     `json:"has_more"`
	Warnings     []string `json:"warnings"`
}

func (b *Book) Query(q Query) (*Result, error) {
	r := &Result{Column: q.Column, Filter: q.Filter, Offset: q.Offset, Limit: q.Limit, Operation: q.Operation, Complete: true, Rows: [][]Cell{}, Issues: []string{}, Warnings: b.Warnings}
	if q.Operation == "sheets" {
		for _, s := range b.Sheets {
			s.Cells = nil
			r.Sheets = append(r.Sheets, s)
		}
		return r, nil
	}
	if q.Operation != "read" && q.Operation != "count" && q.Operation != "sum" {
		return nil, fmt.Errorf("operation 必须为 sheets/read/count/sum")
	}
	var s *Sheet
	for i := range b.Sheets {
		if b.Sheets[i].Name == q.Sheet {
			s = &b.Sheets[i]
			break
		}
	}
	if s == nil {
		return nil, fmt.Errorf("工作表不存在")
	}
	if q.Range == "" {
		return nil, fmt.Errorf("必须明确指定单元格范围，包含表头也会计入统计")
	}
	ends := strings.Split(q.Range, ":")
	if len(ends) == 1 {
		ends = append(ends, ends[0])
	}
	if len(ends) != 2 {
		return nil, fmt.Errorf("范围格式应为 A1:D100")
	}
	c1, r1, e := excelize.CellNameToCoordinates(ends[0])
	if e != nil {
		return nil, e
	}
	c2, r2, e := excelize.CellNameToCoordinates(ends[1])
	if e != nil || c2 < c1 || r2 < r1 || r2 > MaxRows || c2 > MaxCols {
		return nil, fmt.Errorf("范围无效或超限")
	}
	if q.Offset < 0 || q.Offset > MaxRows {
		return nil, fmt.Errorf("offset 超限")
	}
	if q.Limit == 0 {
		q.Limit = 50
	}
	r.Limit = q.Limit
	if q.Limit < 1 || q.Limit > 100 {
		return nil, fmt.Errorf("limit 必须为 1-100")
	}
	sumCol := 0
	if q.Operation == "sum" {
		sumCol, e = excelize.ColumnNameToNumber(q.Column)
		if e != nil || sumCol < c1 || sumCol > c2 {
			return nil, fmt.Errorf("求和列必须在范围内")
		}
	}
	fc := 0
	if q.Filter != nil {
		fc, e = excelize.ColumnNameToNumber(q.Filter.Column)
		if e != nil || fc < c1 || fc > c2 {
			return nil, fmt.Errorf("筛选列必须在范围内")
		}
		switch q.Filter.Operator {
		case "eq", "contains", "gt", "lt":
		default:
			return nil, fmt.Errorf("筛选操作为 eq/contains/gt/lt")
		}
	}
	byRow := map[int][]Cell{}
	for _, c := range s.Cells {
		if c.Row >= r1 && c.Row <= r2 && c.Column >= c1 && c.Column <= c2 {
			byRow[c.Row] = append(byRow[c.Row], c)
		}
	}
	sum := new(big.Rat)
	outBytes := 0
	outputFull := false
	for row := r1; row <= r2; row++ {
		r.ScannedRows++
		cells := byRow[row]
		if len(cells) == 0 {
			continue
		}
		if q.Filter != nil {
			var selected *Cell
			for i := range cells {
				if cells[i].Column == fc {
					selected = &cells[i]
					break
				}
			}
			if selected != nil && (selected.MissingCache || selected.Type == "e") {
				r.Complete = false
				if len(r.Issues) < 20 {
					r.Issues = append(r.Issues, selected.Address+" 筛选值缺失或错误")
				}
				continue
			}
			val := ""
			if selected != nil {
				val = selected.Value
			}
			match := false
			switch q.Filter.Operator {
			case "eq":
				match = val == q.Filter.Value
			case "contains":
				match = strings.Contains(val, q.Filter.Value)
			case "gt", "lt":
				a, ok := new(big.Rat).SetString(val)
				if selected != nil && selected.Type == "n" {
					a, ok = new(big.Rat).SetString(selected.Raw)
				}
				v, vok := new(big.Rat).SetString(q.Filter.Value)
				if !ok || !vok {
					r.Complete = false
					if len(r.Issues) < 20 {
						r.Issues = append(r.Issues, fmt.Sprintf("第 %d 行数值筛选无法比较", row))
					}
					continue
				}
				cmp := a.Cmp(v)
				match = (q.Filter.Operator == "gt" && cmp > 0) || (q.Filter.Operator == "lt" && cmp < 0)
			}
			if !match {
				continue
			}
		}
		r.MatchedRows++
		if q.Operation == "sum" {
			var found bool
			for _, c := range cells {
				if c.Column != sumCol {
					continue
				}
				found = true
				v, ok := new(big.Rat).SetString(c.Raw)
				if c.MissingCache || c.Type != "n" || !ok {
					r.Complete = false
					if len(r.Issues) < 20 {
						r.Issues = append(r.Issues, c.Address+" 非数值或公式缓存缺失/错误")
					}
					continue
				}
				sum.Add(sum, v)
				r.NumericCells++
			}
			if !found {
				r.Complete = false
				if len(r.Issues) < 20 {
					r.Issues = append(r.Issues, fmt.Sprintf("第 %d 行求和值为空", row))
				}
			}
		}
		if q.Operation == "read" && !outputFull && r.MatchedRows > q.Offset && len(r.Rows) < q.Limit {
			bytes := 0
			for _, c := range cells {
				bytes += len(c.Value) + len(c.Raw) + len(c.Formula) + 100
			}
			if bytes > 48000 {
				return nil, fmt.Errorf("单行内容超过 48 KB，请缩小列范围后读取")
			}
			if outBytes+bytes > 48000 {
				outputFull = true
				r.HasMore = true
				continue
			}
			outBytes += bytes
			r.Rows = append(r.Rows, cells)
		}
	}
	r.Sheet = s.Name
	r.Range = fmt.Sprintf("%s:%s", strings.ToUpper(ends[0]), strings.ToUpper(ends[1]))
	if q.Operation == "sum" {
		r.Sum = sum.FloatString(12)
	}
	if q.Operation == "read" && q.Offset+len(r.Rows) < r.MatchedRows {
		r.HasMore = true
	}
	r.Warnings = append(append([]string{}, r.Warnings...), "统计覆盖指定范围的非空行（含隐藏行）；不自动识别表头，不从筛选分页推断总数。sum 使用原始数值，不按显示百分比换算。")
	return r, nil
}

type Section struct{ Heading, Body string }

func (b *Book) Markdown() (string, []Section, error) {
	var all strings.Builder
	sections := []Section{}
	for _, s := range b.Sheets {
		flag := ""
		if s.Hidden {
			flag = "（隐藏工作表）"
		}
		header := fmt.Sprintf("工作表：%s%s；实际范围 %d 行、%d 列。合并：%s。隐藏行：%v；隐藏列：%v。\n", s.Name, flag, s.Rows, s.Columns, strings.Join(s.Merges, ", "), s.HiddenRows, s.HiddenColumns)
		if len([]rune(header)) > 2000 {
			header = string([]rune(header)[:2000]) + "〔元数据较长，请查询工作表目录取得完整范围〕\n"
		}
		var context strings.Builder
		context.WriteString("原表前 3 行上下文（不自动认定表头）：\n")
		for _, c := range s.Cells {
			if c.Row > 3 {
				break
			}
			if context.Len()+len(c.Value) > 4000 {
				context.WriteString("〔上下文较长，请读取实际范围〕\n")
				break
			}
			fmt.Fprintf(&context, "%s: %s\n", c.Address, c.Value)
		}
		header += context.String()
		intro := Section{Heading: s.Name + " · 目录", Body: header + strings.Join(b.Warnings, "\n")}
		sections = append(sections, intro)
		start := 0
		for start < len(s.Cells) {
			end := start
			size := 0
			for end < len(s.Cells) {
				nextSize := len(s.Cells[end].Value) + len(s.Cells[end].Formula) + 80
				if end > start && size+nextSize > 12000 {
					break
				}
				size += nextSize
				end++
			}
			first, last := s.Cells[start].Row, s.Cells[end-1].Row
			heading := fmt.Sprintf("%s · A%d:%s%d", s.Name, first, column(s.Columns), last)
			var body strings.Builder
			body.WriteString(header)
			for _, c := range s.Cells[start:end] {
				value := strings.NewReplacer("\n", " ", "\r", " ").Replace(c.Value)
				fmt.Fprintf(&body, "%s: %s", c.Address, value)
				if c.Formula != "" {
					fmt.Fprintf(&body, " 〔公式缓存；%s〕", c.Formula)
				}
				body.WriteByte('\n')
			}
			sections = append(sections, Section{heading, body.String()})
			start = end
		}
	}
	for _, s := range sections {
		if all.Len()+len(s.Heading)+len(s.Body)+10 > 2<<20 {
			return "", nil, fmt.Errorf("Excel Markdown 超过 2 MiB，请拆分资料")
		}
		fmt.Fprintf(&all, "## %s\n%s\n", s.Heading, s.Body)
	}
	if all.Len() > 2<<20 {
		return "", nil, fmt.Errorf("Excel Markdown 超过 2 MiB，请拆分资料")
	}
	return all.String(), sections, nil
}
func column(n int) string {
	if n < 1 {
		return "A"
	}
	s, _ := excelize.ColumnNumberToName(n)
	return s
}
