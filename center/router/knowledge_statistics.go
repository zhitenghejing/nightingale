package router

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/pkg/knowledgebase"
	"github.com/ccfos/nightingale/v6/pkg/workbook"
)

var explicitWorkbookRange = regexp.MustCompile(`\b[A-Z]{1,3}[1-9][0-9]*:[A-Z]{1,3}[1-9][0-9]*\b`)
var workbookTitle = regexp.MustCompile(`《([^》]+)》`)
var quotedWorkbookValue = regexp.MustCompile(`[“"]([^”"]+)[”"]`)
var positiveWorkbookPredicate = regexp.MustCompile(`(?:为|填(?:写)?|等于)\s*[“"]([^”"]+)[”"]`)
var workbookComputationRequest = regexp.MustCompile(`[。；]请(?:实际)?(?:统计|计算)(?:完整)?(?:指定)?范围(?:并)?(?:给出|说明)(?:计算)?依据[。]?\s*$`)
var workbookBlankCountNote = regexp.MustCompile(`，不把空白当成[“"]否[”"][。]?$`)
var directWorkbookCount = regexp.MustCompile(`统计\s*[“"]([^”"]+)[”"]`)

// Only a narrow, explicit one-range statistic is published without generative
// review. Recompute it from the current authorized workbook: neither a model
// draft nor a stored tool-result number is authoritative.
func (rt *Router) deterministicWorkbookAnswer(ctx context.Context, user int64, question string, turns []aiagent.ChatMessage) (string, bool, error) {
	ranges := explicitWorkbookRange.FindAllString(question, -1)
	titles := workbookTitle.FindAllStringSubmatch(question, -1)
	if rt.Library == nil || len(ranges) != 1 || len(titles) != 1 || !strings.HasSuffix(strings.ToLower(titles[0][1]), ".xlsx") {
		return "", false, nil
	}
	// Multi-column or multi-condition interpretation still needs semantic review.
	ends := strings.Split(ranges[0], ":")
	columnName := regexp.MustCompile(`[0-9]+`)
	if columnName.ReplaceAllString(ends[0], "") != columnName.ReplaceAllString(ends[1], "") {
		return "", false, nil
	}
	outside := quotedWorkbookValue.ReplaceAllString(workbookTitle.ReplaceAllString(question, ""), "")
	for _, term := range []string{"能否", "是否可以", "当前", "现在", "目前", "为什么", "原因", "分别", "或者", "并且", "同时满足", "排除", "不是", "不等于", "不为", "不含", "除了", "除外", "以外", "之外", "非", ">", "<", "≥", "≤", "!="} {
		if strings.Contains(outside, term) {
			return "", false, nil
		}
	}
	op := "count"
	if strings.Contains(question, "求和") || strings.Contains(question, "合计") {
		op = "sum"
	} else if !strings.Contains(question, "统计") {
		return "", false, nil
	}
	predicates := positiveWorkbookPredicate.FindAllStringSubmatch(question, -1)
	if len(predicates) == 0 {
		predicates = directWorkbookCount.FindAllStringSubmatch(question, -1)
	}
	for i := len(turns) - 1; i >= 0; i-- {
		m := turns[i]
		if m.Role != "tool" || !aiagent.IsKnowledgeWorkbookTool(m.ToolName) {
			continue
		}
		var receipt aiagent.KnowledgeDataEvidence
		if json.Unmarshal([]byte(m.Content), &receipt) != nil || receipt.Operation != "workbook_"+op || receipt.WorkbookVersion <= 0 {
			continue
		}
		var result workbook.Result
		if json.Unmarshal(receipt.Result, &result) != nil || result.Operation != op || result.Range != ranges[0] || result.Sheet == "" {
			continue
		}
		sheetScope := regexp.MustCompile(`(?:^|[\s》“"'（(])` + regexp.QuoteMeta(result.Sheet) + `(?:$|[\s的!：:）》”"'])`)
		if !sheetScope.MatchString(workbookTitle.ReplaceAllString(question, " ")) {
			continue
		}
		field, eligible := workbookStatisticField(question, titles[0][1], result.Sheet, result.Range, op)
		if !eligible {
			continue
		}
		if (op == "count" && result.Filter != nil && result.Filter.Column != columnName.ReplaceAllString(ends[0], "")) || (op == "sum" && result.Column != columnName.ReplaceAllString(ends[0], "")) {
			continue
		}
		if op == "count" && (result.Filter == nil || result.Filter.Operator != "eq" || len(predicates) != 1 || predicates[0][1] != result.Filter.Value) {
			continue
		}
		if op == "sum" && result.Filter != nil {
			continue
		}
		k, err := rt.Library.WithContext(ctx).Version(user, receipt.WorkbookVersion)
		if err != nil {
			return "", true, err
		}
		if !knowledgebase.DocumentTitleMatches(k.Title, titles[0][1]) || k.Digest != receipt.WorkbookDigest {
			return "", false, nil
		}
		unique, err := rt.Library.WithContext(ctx).UniqueTitleVersion(user, titles[0][1])
		if err != nil {
			return "", true, err
		}
		if unique != k.ID {
			return "", false, nil
		}
		if field != "" && field != columnName.ReplaceAllString(ends[0], "") {
			// A named field must be the actual single-column header. Other header
			// layouts are deliberately left to semantic review.
			if !strings.HasSuffix(ends[0], "2") || columnName.ReplaceAllString(ends[0], "")+"2" != ends[0] {
				continue
			}
			header, _, _, err := rt.Library.WithContext(ctx).Workbook(user, k.ID, workbook.Query{Operation: "read", Sheet: result.Sheet, Range: columnName.ReplaceAllString(ends[0], "") + "1", Limit: 1})
			if err != nil {
				return "", true, err
			}
			if len(header.Rows) != 1 || len(header.Rows[0]) != 1 || strings.TrimSpace(header.Rows[0][0].Value) != field {
				continue
			}
		}
		q := workbook.Query{Operation: op, Sheet: result.Sheet, Range: result.Range, Column: result.Column, Filter: result.Filter}
		fresh, label, url, err := rt.Library.WithContext(ctx).Workbook(user, k.ID, q)
		if err != nil {
			return "", true, err
		}
		text := fmt.Sprintf("《%s》%s：", k.Title, result.Sheet)
		if op == "count" {
			if fresh.Complete {
				text += fmt.Sprintf("在 %s 中，%s 列明确填“%s”的记录为 **%d 条**。", q.Range, q.Filter.Column, q.Filter.Value, fresh.MatchedRows)
			} else {
				text += fmt.Sprintf("在 %s 中，目前可确认 %s 列明确填“%s”的记录有 **%d 条**；因数据缺失或错误，不能确认为完整条数。", q.Range, q.Filter.Column, q.Filter.Value, fresh.MatchedRows)
			}
			text += "空白不作为该值，也不自动解释为“否”。"
		} else {
			text += fmt.Sprintf("%s 的 %s 列已填写且可计算数值之和为 **%s**（%d 个数值单元格）。空白不代表已核实的零。", q.Range, q.Column, strings.TrimRight(strings.TrimRight(fresh.Sum, "0"), "."), fresh.NumericCells)
		}
		text += fmt.Sprintf("\n\n实际扫描 %d 行；只覆盖指定范围，隐藏行也按该范围计入，表头和合计行如在范围内不会自动排除。", fresh.ScannedRows)
		if !fresh.Complete {
			text += "\n存在缺值、错误或无缓存公式，统计完整性未确认；已填写数值合计不能作为全部记录的完整总数。"
		}
		text += "\n来源：[" + safeCitationLabel(label) + "](" + url + ")。这是保存的资料记录，不代表当前设备状态。"
		return text, true, nil
	}
	return "", false, nil
}

// This is a complete allow-grammar, not a best-effort intent classifier. Every
// extra condition or subquestion falls back to semantic review.
func workbookStatisticField(question, title, sheet, cellRange, operation string) (string, bool) {
	prefix := regexp.MustCompile(`^(?:请)?(?:仅)?(?:对|按)?《` + regexp.QuoteMeta(title) + `》\s*[“"]?` + regexp.QuoteMeta(sheet) + `[”"]?(?:的|!)?` + regexp.QuoteMeta(cellRange) + `\s*[：:,，]?\s*`)
	if !prefix.MatchString(question) {
		return "", false
	}
	body := strings.TrimSpace(prefix.ReplaceAllString(question, ""))
	body = workbookComputationRequest.ReplaceAllString(body, "")
	if operation == "count" {
		body = workbookBlankCountNote.ReplaceAllString(body, "")
		direct := regexp.MustCompile(`^统计[“"]([^”"]+)[”"]的?(?:设备|记录)?(?:条数|数量)[。]?$`)
		if direct.MatchString(body) {
			return "", true
		}
		named := regexp.MustCompile(`^统计[“"]?([^“”"，。；：:]+?)[”"]?(?:列)?(?:明确)?(?:为|填(?:写)?|等于)[“"]([^”"]+)[”"]的?(?:设备|记录)?(?:条数|数量)[。]?$`)
		m := named.FindStringSubmatch(body)
		if len(m) != 3 {
			return "", false
		}
		field := strings.TrimSuffix(m[1], "列")
		return field, field != ""
	}
	sum := regexp.MustCompile(`^对[“"]?([^“”"，。；：:]+?)[”"]?列的已填写数值求和(?:，并说明空白值不能自动当成已核实的零)?[。]?$`)
	m := sum.FindStringSubmatch(body)
	if len(m) != 2 {
		return "", false
	}
	return m[1], true
}
