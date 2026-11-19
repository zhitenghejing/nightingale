package workbook

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"github.com/xuri/excelize/v2"
	"io"
	"strconv"
	"strings"
)

// Check every declared coordinate before any Excelize worksheet read. A valid
// A1 followed by a malicious XFD1048576 must not reach its worksheet allocator.
func preflight(ctx context.Context, sheets map[string][]byte) error {
	cells, metadata, tokens := 0, 0, 0
	for _, data := range sheets {
		decoder := xml.NewDecoder(bytes.NewReader(data))
		for {
			token, e := decoder.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				return fmt.Errorf("工作表 XML 损坏")
			}
			tokens++
			if tokens%256 == 0 && ctx.Err() != nil {
				return ctx.Err()
			}
			start, ok := token.(xml.StartElement)
			if !ok {
				continue
			}
			attr := func(name string) string {
				for _, a := range start.Attr {
					if a.Name.Local == name {
						return a.Value
					}
				}
				return ""
			}
			switch start.Name.Local {
			case "c":
				cells++
				if cells > 100000 {
					return fmt.Errorf("Excel 单元格 XML 条目超过 100000，请拆分资料")
				}
				c, r, e := excelize.CellNameToCoordinates(attr("r"))
				if e != nil || c > MaxCols || r > MaxRows {
					return fmt.Errorf("Excel 实际数据范围超过 %d 行 / %d 列", MaxRows, MaxCols)
				}
			case "row":
				if value := attr("r"); value != "" {
					r, e := strconv.Atoi(value)
					if e != nil || r < 1 || r > MaxRows {
						return fmt.Errorf("Excel 行元数据超过 %d 行限制", MaxRows)
					}
				}
				if attr("hidden") == "1" || attr("hidden") == "true" {
					metadata++
				}
			case "col":
				min, e1 := strconv.Atoi(attr("min"))
				max, e2 := strconv.Atoi(attr("max"))
				if e1 != nil || e2 != nil || min < 1 || max < min || max > 16384 {
					return fmt.Errorf("Excel 列元数据超过 %d 列限制", MaxCols)
				}
				if attr("hidden") == "1" || attr("hidden") == "true" {
					if max > MaxCols {
						max = MaxCols
					}
					if min <= max {
						metadata += max - min + 1
					}
				}
			case "mergeCell":
				metadata++
				for _, axis := range strings.Split(attr("ref"), ":") {
					c, r, e := excelize.CellNameToCoordinates(axis)
					if e != nil || c > MaxCols || r > MaxRows {
						return fmt.Errorf("Excel 合并范围超过限制")
					}
				}
			}
			if metadata > 1024 {
				return fmt.Errorf("Excel 隐藏/合并元数据超过 1024 项")
			}
		}
	}
	return ctx.Err()
}
