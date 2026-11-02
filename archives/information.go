package archives

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
)

// informationBody uses the same offline DOCX/PDF renderer as documents. It
// contains only the pinned archive's business fields and a material register.
func informationBody(snapshot Snapshot, versions map[int64]int64) (models.ArchiveJSON, error) {
	var m Metadata
	if err := json.Unmarshal([]byte(snapshot.Record.Metadata), &m); err != nil {
		return "", err
	}
	a := snapshot.Record
	category := map[string]string{"training": "培训服务", "inspection": "巡检报告", "incident": "故障报告", "other": "其他文档"}[a.Category]
	form := map[string]string{"training": "培训服务档案表", "inspection": "设备巡检档案表", "incident": "故障处理档案表", "other": "业务文档归档表"}[a.Category]
	state := map[string]string{"draft": "草稿", "archived": "已归档"}[a.State]
	value := func(s string) string {
		if strings.TrimSpace(s) == "" {
			return "未填写"
		}
		return s
	}
	date := func(s string) string {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.In(time.Local).Format("2006-01-02 15:04")
		}
		return s
	}
	translated := func(s string) string {
		if label, ok := map[string]string{"normal": "正常", "abnormal": "异常", "I": "Ⅰ级", "II": "Ⅱ级", "III": "Ⅲ级", "IV": "Ⅳ级"}[s]; ok {
			return label
		}
		return s
	}
	type field struct{ label, value string }
	var fields, longFields []field
	switch a.Category {
	case "training":
		duration := ""
		if m.Duration != nil {
			duration = fmt.Sprintf("%g 小时", *m.Duration)
		}
		fields = []field{{"培训部门", m.Department}, {"培训状态", translated(m.TrainingStatus)}, {"培训开始时间", date(m.StartTime)}, {"培训结束时间", date(m.EndTime)}, {"培训地点", m.Location}, {"培训时长", duration}, {"培训讲师", m.Lecturer}}
		longFields = []field{{"参加人员", m.Participants}, {"培训内容", m.TrainingContent}, {"培训效果", m.TrainingEffect}}
	case "inspection":
		fields = []field{{"巡检结果", translated(m.InspectionResult)}, {"处理人", m.Handler}}
		longFields = []field{{"巡检设备范围", strings.Join(m.Scope, "、")}, {"巡检总结", m.Summary}}
	case "incident":
		fields = []field{{"故障等级", translated(m.Level)}, {"影响业务", m.AffectedBusiness}, {"故障发生时间", date(m.OccurredAt)}, {"故障解决时间", date(m.ResolvedAt)}}
		longFields = []field{{"主要处理工程师", m.MainEngineer}, {"辅助处理工程师", m.AuxiliaryEngineers}}
	case "other":
		fields = []field{{"文档类型", m.CustomType}}
		longFields = []field{{"说明", m.Summary}}
	}
	type element = map[string]interface{}
	text := func(s string, size int, bold bool, align string) element {
		return element{"value": s, "font": "Noto Sans CJK SC", "size": size, "bold": bold, "rowFlex": align, "rowMargin": 0.5}
	}
	cell := func(s string, span int, label bool) element {
		align, background := "left", "#FFFFFF"
		if label {
			align, background = "center", "#F4F4F4"
		}
		return element{"colspan": span, "rowspan": 1, "verticalAlign": "middle", "backgroundColor": background, "value": []element{text(value(s), 14, false, align)}}
	}
	row := func(cells ...element) element { return element{"height": 40, "tdList": cells} }
	table := func(widths []int, rows []element) element {
		columns := make([]element, len(widths))
		for i, width := range widths {
			columns[i] = element{"width": width}
		}
		return element{"type": "table", "value": "", "borderType": "all", "colgroup": columns, "trList": rows}
	}
	rows := []element{row(cell("档案主题", 1, true), cell(a.Title, 3, false))}
	for i := 0; i < len(fields); i += 2 {
		left := fields[i]
		if i+1 == len(fields) {
			rows = append(rows, row(cell(left.label, 1, true), cell(left.value, 3, false)))
		} else {
			right := fields[i+1]
			rows = append(rows, row(cell(left.label, 1, true), cell(left.value, 1, false), cell(right.label, 1, true), cell(right.value, 1, false)))
		}
	}
	for _, f := range longFields {
		content := cell(f.value, 3, false)
		content["verticalAlign"] = "top"
		rows = append(rows, row(cell(f.label, 1, true), content))
	}
	main := []element{
		text("运维服务档案 / "+category, 12, false, "left"),
		text("\n"+form, 30, true, "center"),
		text(fmt.Sprintf("\n档案编号：#%d　　状态：%s　　档案版本 v%d", a.ID, state, a.Revision), 12, false, "left"),
		table([]int{118, 211, 118, 211}, rows),
		text(" ", 8, false, "left"),
	}
	materials := []element{
		row(cell("留档资料清单", 4, true)),
		row(cell("序号", 1, true), cell("资料名称", 1, true), cell("格式", 1, true), cell("版本", 1, true)),
	}
	for i, ref := range snapshot.Documents {
		kind := map[string]string{"online": "在线文档", "docx": "Word", "pdf": "PDF", "image": "图片"}[ref.Document.Kind]
		materials = append(materials, row(cell(fmt.Sprint(i+1), 1, false), cell(ref.Document.Title, 1, false), cell(kind, 1, false), cell(fmt.Sprintf("v%d", versions[ref.VersionID]), 1, false)))
	}
	if len(snapshot.Documents) == 0 {
		materials = append(materials, row(cell("暂无留档资料", 4, false)))
	}
	timestamp := func(ms int64) string {
		if ms <= 0 {
			return "未记录"
		}
		return time.UnixMilli(ms).In(time.Local).Format("2006-01-02 15:04")
	}
	main = append(main,
		table([]int{59, 375, 132, 92}, materials),
		text("\n创建账号："+value(a.CreateName)+"　　创建时间："+timestamp(a.CreateAt), 12, false, "left"),
		text("\n最后更新："+timestamp(a.UpdateAt), 12, false, "left"),
		text("\n本表为档案基本信息。文档正文与附件内容不在本表内。", 11, false, "left"),
	)
	return encode(element{
		"data": element{"main": main},
		"options": element{"defaultFont": "Noto Sans CJK SC", "defaultSize": 14, "width": 794, "height": 1123, "margins": []int{68, 68, 68, 68},
			"pageNumber": element{"format": "第 {pageNo} 页 / 共 {pageCount} 页", "rowFlex": "center", "font": "Noto Sans CJK SC", "size": 12}},
	}), nil
}
