package router

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/aiagent/llm"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledgebase"
	"github.com/ccfos/nightingale/v6/pkg/workbook"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type unavailableReviewModel struct{ calls int }

func (m *unavailableReviewModel) Name() string { return "unavailable" }
func (m *unavailableReviewModel) Generate(context.Context, *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	m.calls++
	return nil, errors.New("provider unavailable")
}
func (m *unavailableReviewModel) GenerateStream(context.Context, *llm.GenerateRequest) (<-chan llm.StreamChunk, error) {
	panic("unexpected stream")
}

func optimizationWorkbook(t *testing.T) (*Router, knowledgeAnswerEvidence, []aiagent.ChatMessage) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/review.db"), &gorm.Config{})
	require.NoError(t, err)
	sql, _ := db.DB()
	t.Cleanup(func() { sql.Close() })
	tables := append(models.ArchiveTables(), models.LibraryTables()...)
	tables = append(tables, &models.User{})
	require.NoError(t, db.AutoMigrate(tables...))
	require.NoError(t, db.Create(&models.User{Id: 1, Username: "reader", Roles: "Admin"}).Error)
	a := models.ArchiveRecord{ID: 1}
	require.NoError(t, db.Create(&a).Error)
	d := models.ArchiveDocument{ID: 2, ArchiveID: 1, Title: "合成设备表.xlsx", CurrentVersionID: 3}
	require.NoError(t, db.Create(&d).Error)
	k := models.LibraryVersion{ID: 4, SourceVersionID: 3, DocumentID: 2, ArchiveID: 1, Title: d.Title, State: "ready", Digest: "fixture", Sequence: 1}
	require.NoError(t, db.Create(&k).Error)
	b := workbook.Book{Sheets: []workbook.Sheet{{Name: "清单", Rows: 4, Columns: 1, Cells: []workbook.Cell{{Address: "A1", Row: 1, Column: 1, Type: "s", Value: "设备类型"}, {Address: "A2", Row: 2, Column: 1, Type: "s", Value: "服务器"}, {Address: "A3", Row: 3, Column: 1, Type: "s", Value: "交换机"}, {Address: "A4", Row: 4, Column: 1, Type: "s", Value: "服务器"}}}}}
	raw, _ := json.Marshal(b)
	require.NoError(t, db.Create(&models.LibraryWorkbook{VersionID: 4, Data: models.LibraryText(raw)}).Error)
	store := &knowledgebase.Store{DB: db}
	q := workbook.Query{Operation: "count", Sheet: "清单", Range: "A2:A4", Filter: &workbook.Filter{Column: "A", Operator: "eq", Value: "服务器"}}
	result, label, url, err := store.Workbook(1, 4, q)
	require.NoError(t, err)
	body, _ := json.Marshal(result)
	receipt := aiagent.KnowledgeDataEvidence{Operation: "workbook_count", WorkbookVersion: 4, WorkbookDigest: "fixture", SourceLabel: label, SourceURL: url, QueriedAt: 123, Result: body}
	text, _ := json.Marshal(receipt)
	return &Router{Library: store}, knowledgeAnswerEvidence{Data: []aiagent.KnowledgeDataEvidence{receipt}}, []aiagent.ChatMessage{{Role: "tool", ToolName: "query_knowledge_workbook", Content: string(text)}}
}

func TestWorkbookCountPublishesRecomputedEvidenceWithoutModelReview(t *testing.T) {
	rt, e, turns := optimizationWorkbook(t)
	m := &unavailableReviewModel{}
	answer, err := rt.auditKnowledgeAnswer(context.Background(), m, 1, "请对《合成设备表.xlsx》清单的A2:A4统计“服务器”的记录条数。", e, "模型猜测999条", turns)
	require.NoError(t, err)
	require.Contains(t, answer, "2")
	require.Contains(t, answer, "A2:A4")
	require.NotContains(t, answer, "999")
	require.Zero(t, m.calls)
}

func TestWorkbookCountCannotUseWrongRangeOrRevokedSource(t *testing.T) {
	rt, e, turns := optimizationWorkbook(t)
	m := &unavailableReviewModel{}
	_, err := rt.auditKnowledgeAnswer(context.Background(), m, 1, "请对《合成设备表.xlsx》清单的A2:A99统计“服务器”的记录条数。", e, "999", turns)
	require.Error(t, err)
	require.Equal(t, 1, m.calls)
	require.NoError(t, rt.Library.DB.Model(&models.ArchiveDocument{}).Where("id=2").Update("deleted", true).Error)
	_, err = rt.auditKnowledgeAnswer(context.Background(), m, 1, "请对《合成设备表.xlsx》清单的A2:A4统计“服务器”的记录条数。", e, "999", turns)
	require.Error(t, err)
}

func TestWorkbookEvidenceRejectsUnrequestedFilterAndTamperedTotals(t *testing.T) {
	rt, e, turns := optimizationWorkbook(t)
	m := &unavailableReviewModel{}
	_, err := rt.auditKnowledgeAnswer(context.Background(), m, 1, "请对《合成设备表.xlsx》清单的A2:A4统计“交换机”的记录条数。", e, "999", turns)
	require.Error(t, err)
	require.Equal(t, 1, m.calls)
	turns[0].Content = string([]byte(turns[0].Content))
	var receipt aiagent.KnowledgeDataEvidence
	require.NoError(t, json.Unmarshal([]byte(turns[0].Content), &receipt))
	var v workbook.Result
	require.NoError(t, json.Unmarshal(receipt.Result, &v))
	v.MatchedRows = 999
	receipt.Result, _ = json.Marshal(v)
	raw, _ := json.Marshal(receipt)
	turns[0].Content = string(raw)
	answer, err := rt.auditKnowledgeAnswer(context.Background(), m, 1, "请对《合成设备表.xlsx》清单的A2:A4统计“服务器”的记录条数。", e, "999", turns)
	require.NoError(t, err)
	require.Contains(t, answer, "**2 条**")
	require.NotContains(t, answer, "999")
}

func TestSearchReadReceiptIsEvidenceButCandidateAloneIsNot(t *testing.T) {
	var e knowledgeAnswerEvidence
	e.observe("search_knowledge", `{"documents":[{"id":15,"section_id":15}],"read_documents":[{"id":16,"section_id":16}]}`)
	require.Equal(t, []int64{16}, e.Sections)
	for _, name := range []string{"read_workbook_range", "sum_workbook_column", "count_workbook_rows"} {
		e.observe(name, `{"operation":"workbook_count","workbook_version":4,"queried_at":123,"result":{"matched_rows":2}}`)
	}
	require.Len(t, e.Data, 3)
}

type diagnosticReviewModel struct {
	unavailableReviewModel
	result *llm.GenerateResponse
	err    error
}

func (m *diagnosticReviewModel) Generate(context.Context, *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	return m.result, m.err
}

func TestReviewTelemetryDistinguishesFailuresWithoutSavingReasoning(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		result       *llm.GenerateResponse
		err          error
	}{
		{"length", "incomplete_output", &llm.GenerateResponse{Content: "partial", ReasoningContent: "PRIVATE_REASONING", FinishReason: "length", Usage: &llm.Usage{CompletionTokens: 4096}}, nil},
		{"empty", "empty_output", &llm.GenerateResponse{FinishReason: "stop"}, nil},
		{"timeout", "timeout", nil, context.DeadlineExceeded},
		{"quota", "rate_limited", nil, errors.New("API error status 429 private-provider-body")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var trace knowledgeReviewDiagnostic
			ctx := context.WithValue(context.Background(), knowledgeReviewTraceKey{}, &trace)
			rt := &Router{}
			e := knowledgeAnswerEvidence{Data: []aiagent.KnowledgeDataEvidence{{Operation: "environment_trace"}}}
			_, err := rt.auditKnowledgeAnswer(ctx, &diagnosticReviewModel{result: tc.result, err: tc.err}, 1, "当前依赖有什么", e, "草稿", nil)
			require.Error(t, err)
			require.Equal(t, tc.status, trace.Status)
			encoded, _ := json.Marshal(trace)
			require.NotContains(t, string(encoded), "PRIVATE_REASONING")
			require.NotContains(t, string(encoded), "private-provider-body")
		})
	}
}

func TestVerifiedCitationNormalizationHandlesPlainAndChinesePunctuation(t *testing.T) {
	source := "/archives/9/documents/12?version=14&sheet=inventory&range=A2%3AD5"
	text := "来源：（https:/" + source + "）。另一个 https:/" + source + "extra 不可改写。"
	out := normalizeKnowledgeLinks(text, []string{source})
	require.Contains(t, out, "（"+source+"）")
	require.Contains(t, out, "https:/"+source+"extra")
}

func TestIdentityAndMergeConclusionsRequireReview(t *testing.T) {
	e := knowledgeAnswerEvidence{Data: []aiagent.KnowledgeDataEvidence{{Operation: "workbook_read"}}}
	for _, q := range []string{"两台同IP设备为什么不能直接合并？", "是否可以仅按用途自动打所属业务标签？", "合并字段和真实空白应如何处理？"} {
		require.True(t, knowledgeNeedsAudit(q, e), q)
	}
	require.False(t, knowledgeNeedsAudit("设备型号和机柜位置是什么？", e))
}

func TestDeterministicStatisticsRejectNegationAndApproximateScope(t *testing.T) {
	rt, e, turns := optimizationWorkbook(t)
	for _, q := range []string{
		"请对《合成设备表.xlsx》清单的A2:A4统计不等于“服务器”的记录条数。",
		"请对《合成设备表.xlsx》清单的A2:A4统计不是“服务器”的记录条数。",
		"请对《设备表.xlsx》清单的A2:A4统计“服务器”的记录条数。",
		"请对《合成设备表.xlsx》清单备份的A2:A4统计“服务器”的记录条数。",
	} {
		m := &unavailableReviewModel{}
		_, err := rt.auditKnowledgeAnswer(context.Background(), m, 1, q, e, "草稿", turns)
		require.Error(t, err, q)
		require.Equal(t, 1, m.calls, q)
	}
}

func TestProtocolMarkersCannotGainCredibilityFromSourceFooter(t *testing.T) {
	rt, _, _ := optimizationWorkbook(t)
	section := models.LibrarySection{VersionID: 4, Body: "合成事实"}
	require.NoError(t, rt.Library.DB.Create(&section).Error)
	text := rt.groundKnowledgeAnswer(t.Context(), 1, knowledgeAnswerEvidence{Sections: []int64{section.ID}}, "\u200b</function>\n</tool_call>")
	require.NotContains(t, text, "</function>")
	require.NotContains(t, text, "依据来源")
	require.Contains(t, text, "未形成")
}

func TestReviewClientBudgetDoesNotAlterMainClient(t *testing.T) {
	rt := &Router{llmClientCache: llm.NewClientCache()}
	cfg := &models.AILLMConfig{Model: "synthetic", APIType: "openai", APIURL: "https://model.example/v1", APIKey: "synthetic"}
	cfg.ExtraConfig.TimeoutSeconds = 15
	main, mainBudget, err := rt.chatLLMClient(cfg)
	require.NoError(t, err)
	review, reviewBudget, err := rt.chatLLMClientForTimeout(cfg, 200000)
	require.NoError(t, err)
	require.Equal(t, 15000, mainBudget)
	require.Equal(t, 200000, reviewBudget)
	require.NotSame(t, main, review)
	require.Equal(t, 15, cfg.ExtraConfig.TimeoutSeconds)
	again, _, err := rt.chatLLMClient(cfg)
	require.NoError(t, err)
	require.Same(t, main, again)
}

func TestDeterministicCountRequiresWholePositivePredicate(t *testing.T) {
	rt, _, turns := optimizationWorkbook(t)
	for _, q := range []string{
		"请对《合成设备表.xlsx》清单的A2:A4统计“服务器”且状态正常的记录条数。",
		"请对《合成设备表.xlsx》清单的A2:A4统计位置为“服务器”的记录条数。",
		"请对《合成设备表.xlsx》清单的A2:A4统计“服务器”的记录条数，再说明用途。",
	} {
		_, handled, err := rt.deterministicWorkbookAnswer(context.Background(), 1, q, turns)
		require.NoError(t, err)
		require.False(t, handled, q)
	}
	for _, q := range []string{
		"请对《合成设备表.xlsx》清单的A2:A4统计设备类型为“服务器”的记录条数。",
		"请对《合成设备表.xlsx》清单的A2:A4统计“设备类型”明确填“服务器”的记录条数，不把空白当成“否”。",
	} {
		answer, handled, err := rt.deterministicWorkbookAnswer(context.Background(), 1, q, turns)
		require.NoError(t, err)
		require.True(t, handled, q)
		require.Contains(t, answer, "**2 条**")
	}
}

func TestDeterministicSumRecomputesNamedColumnAndKeepsMissingValueBoundary(t *testing.T) {
	rt, _, _ := optimizationWorkbook(t)
	b := workbook.Book{Sheets: []workbook.Sheet{{Name: "清单", Rows: 4, Columns: 1, Cells: []workbook.Cell{
		{Address: "A1", Row: 1, Column: 1, Type: "s", Value: "CPU数量"},
		{Address: "A2", Row: 2, Column: 1, Type: "n", Raw: "2", Value: "2"},
		{Address: "A4", Row: 4, Column: 1, Type: "n", Raw: "4", Value: "4"},
	}}}}
	raw, _ := json.Marshal(b)
	require.NoError(t, rt.Library.DB.Model(&models.LibraryWorkbook{}).Where("version_id=4").Update("data", models.LibraryText(raw)).Error)
	result, _, _, err := rt.Library.Workbook(1, 4, workbook.Query{Operation: "sum", Sheet: "清单", Range: "A2:A4", Column: "A"})
	require.NoError(t, err)
	result.Sum = "999"
	body, _ := json.Marshal(result)
	receipt := aiagent.KnowledgeDataEvidence{Operation: "workbook_sum", WorkbookVersion: 4, WorkbookDigest: "fixture", Result: body}
	payload, _ := json.Marshal(receipt)
	turns := []aiagent.ChatMessage{{Role: "tool", ToolName: "sum_workbook_column", Content: string(payload)}}
	q := "请对《合成设备表.xlsx》清单的A2:A4：对CPU数量列的已填写数值求和，并说明空白值不能自动当成已核实的零。请实际统计完整指定范围并给出依据。"
	text, handled, err := rt.deterministicWorkbookAnswer(context.Background(), 1, q, turns)
	require.NoError(t, err)
	require.True(t, handled)
	require.Contains(t, text, "**6**")
	require.Contains(t, text, "不能作为全部记录的完整总数")
	require.NotContains(t, text, "999")
	for _, extra := range []string{"，仅统计服务器", "，再给出服务器用途"} {
		_, handled, err = rt.deterministicWorkbookAnswer(context.Background(), 1, "请对《合成设备表.xlsx》清单的A2:A4：对CPU数量列的已填写数值求和"+extra+"。", turns)
		require.NoError(t, err)
		require.False(t, handled)
	}
}

func TestDiagramJudgmentsAndIntroducedConclusionsRequireReview(t *testing.T) {
	evidence := knowledgeAnswerEvidence{Sections: []int64{1}}
	require.True(t, knowledgeNeedsAudit("图示能据此断定真实设备情况吗", evidence))
	require.True(t, knowledgeNeedsAudit("解释标注", evidence, "因此，实际机箱高度为2U"))
	require.False(t, knowledgeNeedsAudit("报告日期是什么", evidence, "报告记载日期为2026年10月26日"))
}

func TestSourceScopeRefusalDoesNotTurnEmptySearchIntoTechnicalFailure(t *testing.T) {
	e := knowledgeAnswerEvidence{}
	e.observe("read_knowledge", "Error: 本轮仅可读取用户指定资料及指定标识范围，不能扩大到其他来源")
	e.observe("search_knowledge", `{"documents":[],"read_documents":[],"procedures":[]}`)
	require.False(t, e.Failed)
	require.True(t, e.Searched)
	require.True(t, e.Empty)
}

func TestNamedDocumentIdentityDoesNotRequireTheContentIdentifierToExist(t *testing.T) {
	scope := knowledgeQuestionScope("《原始报告.docx》的日期是什么，里面是否记载CN791304C2？")
	require.Equal(t, "原始报告.docx", scope.Title)
	require.Empty(t, scope.Identifiers)
	scope = knowledgeQuestionScope("查找记载CN791304C2的资料")
	require.Empty(t, scope.Title)
	require.Equal(t, []string{"cn791304c2"}, scope.Identifiers)
}
