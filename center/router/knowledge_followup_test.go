package router

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/aiagent/chat"
	"github.com/ccfos/nightingale/v6/aiagent/llm"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledgebase"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"testing"
)

func TestKnowledgeQAFollowupCanReachReadonlyData(t *testing.T) {
	h, _ := chat.Lookup("knowledge_qa")
	names := h.SelectTools(&chat.AIChatRequest{UserInput: "这个故障还会复发吗，现在的环境情况"})
	require.Contains(t, names, "query_knowledge_data", "Current-state followups cannot be answered from old inspection reports alone")
	require.Contains(t, names, "resolve_knowledge_object", "Same vendor or model is not an identity match")
	for _, name := range names {
		_, ok := aiagent.GetBuiltinToolDef(name)
		require.True(t, ok, name)
		require.NotContains(t, name, "create_")
		require.NotContains(t, name, "dispatch_")
	}
}

func TestKnowledgeFollowupKeepsOnlyRevalidatedAnswer(t *testing.T) {
	msg := &models.AssistantMessage{IsFinish: true, Query: models.AssistantMessageQuery{Mode: "knowledge_qa", Content: "这次故障是什么原因"}, Response: []models.AssistantMessageResponse{{ContentType: models.ContentTypeMarkdown, Content: "F5 HA 状态不一致。"}}}
	msg.Extra.KnowledgeEvidence = []byte(`{"Sections":[320]}`)
	h := knowledgeFollowupHistory([]*models.AssistantMessage{msg}, func(e knowledgeAnswerEvidence) bool { return len(e.Sections) == 1 && e.Sections[0] == 320 })
	require.Len(t, h, 2)
	require.Equal(t, "assistant", h[1].Role)
	require.Contains(t, h[1].Content, "F5 HA")
	require.Contains(t, h[1].Content, "当前状态需要重新查询")
	h = knowledgeFollowupHistory([]*models.AssistantMessage{msg}, func(knowledgeAnswerEvidence) bool { return false })
	require.Len(t, h, 1)
	require.NotContains(t, h[0].Content, "HA")
	msg.Query.Mode = "ops"
	h = knowledgeFollowupHistory([]*models.AssistantMessage{msg}, func(knowledgeAnswerEvidence) bool { return true })
	require.Len(t, h, 1)
}
func TestKnowledgeHistoryDoesNotTreatOlderToolResultsAsCurrentTurnEvidence(t *testing.T) {
	msg := &models.AssistantMessage{}
	env := aiagent.TranscriptEnvelope{Messages: []aiagent.ChatMessage{{Role: "user", Content: "first"}, {Role: "tool", ToolName: "read_knowledge", Content: `{"id":320}`}, {Role: "user", Content: "second"}, {Role: "tool", ToolName: "read_knowledge", Content: `{"id":314}`}}}
	msg.Extra.HistoryMessages, _ = json.Marshal(env)
	e := knowledgeMessageEvidence(msg)
	require.Equal(t, []int64{314}, e.Sections)
}

func TestKnowledgeDocumentSearchCannotUseTargetCatalogAsDocumentEvidence(t *testing.T) {
	h, _ := chat.Lookup("knowledge_qa")
	names := h.SelectTools(&chat.AIChatRequest{UserInput: "请查找只含 ZZNOFOLLOWUP781923 的资料"})
	require.Equal(t, []string{"search_knowledge", "read_knowledge", "query_knowledge_workbook", "query_environment"}, names)
	require.True(t, chat.KnowledgeDocumentSearch("请查找这个编号的资料"))
}

func TestKnowledgeImplicitDataQuestionKeepsReadonlyDiscovery(t *testing.T) {
	h, _ := chat.Lookup("knowledge_qa")
	require.Contains(t, h.SelectTools(&chat.AIChatRequest{UserInput: "统计昨天订单总量"}), "query_knowledge_data")
}

func TestKnowledgeDeniedLookupCanRecoverWithAuthorizedEvidence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/knowledge.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, _ := db.DB()
	t.Cleanup(func() { conn.Close() })
	tables := append(models.ArchiveTables(), models.LibraryTables()...)
	tables = append(tables, &models.User{})
	require.NoError(t, db.AutoMigrate(tables...))
	require.NoError(t, db.Create(&models.User{Id: 1, Username: "reader", Roles: "Admin"}).Error)
	a := models.ArchiveRecord{Category: "other", Title: "Fixture"}
	require.NoError(t, db.Create(&a).Error)
	doc := models.ArchiveDocument{ArchiveID: a.ID, Title: "Authorized report", Kind: "online"}
	require.NoError(t, db.Create(&doc).Error)
	source := models.ArchiveDocumentVersion{DocumentID: doc.ID, Sequence: 1}
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Model(&doc).Update("current_version_id", source.ID).Error)
	version := models.LibraryVersion{DocumentID: doc.ID, ArchiveID: a.ID, SourceVersionID: source.ID, Title: doc.Title, Sequence: 1, State: "ready"}
	require.NoError(t, db.Create(&version).Error)
	section := models.LibrarySection{VersionID: version.ID, Heading: "Check", Body: "Verified fact"}
	require.NoError(t, db.Create(&section).Error)
	rt := &Router{Library: &knowledgebase.Store{DB: db}}
	var e knowledgeAnswerEvidence
	e.observe("read_knowledge", "Error: 没有权限查阅，或资料已失效")
	e.observe("read_knowledge", fmt.Sprintf(`{"id":%d}`, section.ID))
	answer := rt.groundKnowledgeAnswer(1, e, "Verified fact")
	require.Contains(t, answer, "Verified fact")
	require.NotContains(t, answer, "请重新查询")
	model := &knowledgeAuditTestModel{}
	audited, err := rt.auditKnowledgeAnswer(context.Background(), model, 1, "这个故障还会复发吗", e, "永久整改未闭环", nil)
	require.NoError(t, err)
	require.Equal(t, "现有资料不足以确认整改是否完成。", audited)
	require.Empty(t, model.request.Tools)
	require.Contains(t, model.request.Messages[1].Content, "Verified fact")
	require.NoError(t, db.Model(&doc).Update("deleted", true).Error)
	model.request = nil
	_, err = rt.auditKnowledgeAnswer(context.Background(), model, 1, "这个故障还会复发吗", e, "Do not leak", nil)
	require.Error(t, err)
	require.Nil(t, model.request, "revoked source cannot reach the model")

	require.Contains(t, rt.groundKnowledgeAnswer(1, e, "Do not leak"), "失效")
}

type knowledgeAuditTestModel struct{ request *llm.GenerateRequest }

func (m *knowledgeAuditTestModel) Name() string { return "audit-test" }
func (m *knowledgeAuditTestModel) Generate(_ context.Context, r *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	m.request = r
	return &llm.GenerateResponse{Content: "现有资料不足以确认整改是否完成。"}, nil
}
func (m *knowledgeAuditTestModel) GenerateStream(context.Context, *llm.GenerateRequest) (<-chan llm.StreamChunk, error) {
	panic("audit must not stream unverified conclusions")
}
func TestEnvironmentConclusionsAreReviewedWithoutDocumentReads(t *testing.T) {
	e := knowledgeAnswerEvidence{Data: []aiagent.KnowledgeDataEvidence{{Operation: "environment_trace"}}}
	require.True(t, knowledgeNeedsAudit("当前依赖及采样情况", e))
	require.False(t, knowledgeNeedsAudit("解释术语", knowledgeAnswerEvidence{}))
	model := &knowledgeAuditTestModel{}
	rt := &Router{}
	raw := `{"operation":"environment_trace","queried_at":123,"result":{"nodes":[{"ref":"target:1","name":"Sim-App","attributes":{"host_ip":"10.0.0.1"},"fingerprint":"private-hash"}],"relations":[{"from_ref":"service:1","to_ref":"target:1","effective":"confirmed","from":{"ref":"service:1","name":"Sim-ESB","fingerprint":"private-hash"}}],"truncated":true,"evidence":{"private":"hash"}}}`
	_, err := rt.auditKnowledgeAnswer(context.Background(), model, 1, "当前依赖及采样情况", e, "所有设备无采样", []aiagent.ChatMessage{{Role: "tool", ToolName: "query_environment", Content: raw}})
	require.NoError(t, err)
	require.NotNil(t, model.request)
	require.Empty(t, model.request.Tools)
	payload := model.request.Messages[1].Content
	require.Contains(t, payload, "Sim-App")
	require.Contains(t, payload, "confirmed")
	require.NotContains(t, payload, "private-hash")
	require.Contains(t, model.request.Messages[0].Content, "指定选择器")
	require.Contains(t, model.request.Messages[0].Content, "60秒步进")
}

func TestWorkbookReceiptsAreDocumentEvidence(t *testing.T) {
	var evidence knowledgeAnswerEvidence
	evidence.observe("query_knowledge_workbook", `{"operation":"workbook_sum","workbook_version":7,"queried_at":10,"result":{"sum":"3","complete":false}}`)
	require.Len(t, evidence.Data, 1)
	require.Nil(t, evidence.Data[0].Result)
	require.True(t, knowledgeNeedsAudit("总数多少", evidence))
	data := append(evidence.Data, aiagent.KnowledgeDataEvidence{Operation: "environment_search", QueriedAt: 10})
	require.Len(t, documentEvidenceData(data), 1)
	require.Empty(t, documentEvidenceData(data[1:]))
}

func TestWorkbookCitationRestoresOnlyAuthorizedLocalLinks(t *testing.T) {
	source := "/archives/10/documents/14?version=16&sheet=test&range=A1%3AD5"
	text := "[来源](http:/" + source + ") [未核对](http://archives/99/documents/77) [外部](https://example.com/report)"
	normalized := normalizeKnowledgeLinks(text, []string{source})
	require.Contains(t, normalized, "[来源]("+source+")")
	require.Contains(t, normalized, "(http://archives/99/documents/77)")
	require.Contains(t, normalized, "(https://example.com/report)")
}
