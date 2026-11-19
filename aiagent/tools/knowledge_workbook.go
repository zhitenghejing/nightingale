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
	if len(q.Sheet) > 255 || len(q.Range) > 50 || (q.Filter != nil && len(q.Filter.Value) > 1000) {
		return "", fmt.Errorf("查询参数超限")
	}
	store := d.Library.WithContext(ctx)
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
