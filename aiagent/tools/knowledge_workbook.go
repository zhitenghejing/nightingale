package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/pkg/workbook"
	"time"
)

func init() {
	version := aiagent.ToolParameter{Name: "version_id", Type: "integer", Required: true, Description: "documents[].version_id, not source_version_id"}
	sheet := aiagent.ToolParameter{Name: "sheet", Type: "string", Required: true, Description: "Exact worksheet name from list_workbook_sheets"}
	rangeParam := aiagent.ToolParameter{Name: "range", Type: "string", Required: true, Description: "Explicit complete A1 range, e.g. D2:D72; exclude headers deliberately"}
	filter := []aiagent.ToolParameter{{Name: "filter_column", Type: "string"}, {Name: "filter_operator", Type: "string", Enum: []string{"eq", "contains", "gt", "lt"}}, {Name: "filter_value", Type: "string"}}
	for _, spec := range []struct {
		name, op, description string
		params                []aiagent.ToolParameter
	}{
		{"list_workbook_sheets", "sheets", "Discover exact worksheet names, dimensions, merged and hidden ranges of an authorized Excel report.", []aiagent.ToolParameter{version}},
		{"read_workbook_range", "read", "Read exact Excel coordinates. To find a serial/IP row, set filter_column/filter_operator/filter_value; filtering happens before pagination (filter column must lie in range). Otherwise has_more=true means more rows exist; use offset or a filter. Merged values remain at anchors; blank cells remain blank.", append([]aiagent.ToolParameter{version, sheet, rangeParam, {Name: "offset", Type: "integer"}, {Name: "limit", Type: "integer"}}, filter...)},
		{"count_workbook_rows", "count", "Count ALL nonempty matching rows over the explicit range, regardless of pagination. Set filter_column/operator/value for a filtered count. Report matched_rows and complete; a report is historical evidence.", append([]aiagent.ToolParameter{version, sheet, rangeParam}, filter...)},
		{"sum_workbook_column", "sum", "Sum saved numeric cells over the complete explicit range. Missing/error/formula caches make complete=false. Report the available sum with its limitations; do not manually add paged rows.", append([]aiagent.ToolParameter{version, sheet, rangeParam, {Name: "column", Type: "string", Required: true, Description: "Exact sum column, e.g. M"}}, filter...)},
	} {
		op := spec.op
		register(opsDef(spec.name, spec.description, spec.params...), func(ctx context.Context, d *aiagent.ToolDeps, a map[string]interface{}, p map[string]string) (string, error) {
			args := make(map[string]interface{}, len(a)+1)
			for k, v := range a {
				args[k] = v
			}
			args["operation"] = op
			return queryKnowledgeWorkbook(ctx, d, args, p)
		})
	}
	register(opsDef("query_knowledge_workbook", "Read an authorized current Excel report returned by search_knowledge. version_id is documents[].version_id (NOT source_version_id or sequence). First operation=sheets to discover exact sheet names, dimensions and hidden/merged ranges, then read explicit A1:D100 range. count/sum scan ALL nonempty rows in that explicit range, including hidden rows, independently of offset/limit; exclude headers explicitly. sum uses raw numbers (percent 12% means 0.12); missing/error/formula-without-cache means incomplete. Never infer whole-report totals from search snippets or paged reads. Does not calculate formulas or access external links. Report evidence is not realtime monitoring.",
		aiagent.ToolParameter{Name: "version_id", Type: "integer", Required: true}, aiagent.ToolParameter{Name: "operation", Type: "string", Required: true, Enum: []string{"sheets", "read", "count", "sum"}}, aiagent.ToolParameter{Name: "sheet", Type: "string"}, aiagent.ToolParameter{Name: "range", Type: "string"}, aiagent.ToolParameter{Name: "column", Type: "string", Description: "Sum column, e.g. D"}, aiagent.ToolParameter{Name: "filter_column", Type: "string"}, aiagent.ToolParameter{Name: "filter_operator", Type: "string", Enum: []string{"eq", "contains", "gt", "lt"}}, aiagent.ToolParameter{Name: "filter_value", Type: "string"}, aiagent.ToolParameter{Name: "offset", Type: "integer"}, aiagent.ToolParameter{Name: "limit", Type: "integer"}), queryKnowledgeWorkbook)
}
func queryKnowledgeWorkbook(ctx context.Context, d *aiagent.ToolDeps, a map[string]interface{}, p map[string]string) (string, error) {
	u, e := getUser(d, p)
	if e != nil {
		return "", e
	}
	if d.Library == nil {
		return "", fmt.Errorf("资料知识库未启用")
	}
	id := getArgInt64(a, "version_id")
	q := workbook.Query{Operation: getArgString(a, "operation"), Sheet: getArgString(a, "sheet"), Range: getArgString(a, "range"), Column: getArgString(a, "column"), Offset: int(getArgInt64(a, "offset")), Limit: int(getArgInt64(a, "limit"))}
	if col := getArgString(a, "filter_column"); col != "" {
		q.Filter = &workbook.Filter{Column: col, Operator: getArgString(a, "filter_operator"), Value: getArgString(a, "filter_value")}
	}
	if q.Operation != "sheets" && (q.Sheet == "" || q.Range == "") {
		return "", fmt.Errorf("缺少 sheet 或 range；先 list_workbook_sheets，再按实际名称传入，例如 sheet=Sheet1, range=A2:D20。不要重复未修正的参数")
	}
	_, filterValueGiven := a["filter_value"]
	if q.Filter != nil || getArgString(a, "filter_operator") != "" || filterValueGiven {
		if q.Filter == nil || q.Filter.Operator == "" || !filterValueGiven {
			return "", fmt.Errorf("筛选必须同时给出 filter_column、filter_operator 和 filter_value；缺少时不会退化为无筛选统计")
		}
	}
	if len(q.Sheet) > 255 || len(q.Range) > 50 || (q.Filter != nil && len(q.Filter.Value) > 1000) {
		return "", fmt.Errorf("查询参数超限")
	}
	store := d.Library.WithContext(ctx)
	if err := checkKnowledgeSourceScope(ctx, d, u.Id, id, p); err != nil {
		return "", err
	}
	r, label, link, e := store.Workbook(u.Id, id, q)
	if e != nil {
		return "", e
	}
	k, e := store.Version(u.Id, id)
	if e != nil {
		return "", e
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return "", e
	}
	if len(raw) > 64000 {
		return "", fmt.Errorf("查询结果超过 64 KB，请缩小工作表或列范围")
	}
	return opsJSON(aiagent.KnowledgeDataEvidence{Operation: "workbook_" + q.Operation, WorkbookVersion: id, WorkbookDigest: k.Digest, SourceLabel: label, SourceURL: link, QueriedAt: time.Now().Unix(), Query: q.Sheet + "!" + q.Range, Result: raw, Truncated: r.HasMore}, nil)
}
