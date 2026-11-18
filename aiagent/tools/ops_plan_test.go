package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/aiagent/llm"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/ops"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOpsPlanToolPersistsAuditedDraftWithinRunScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/tool.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, _ := db.DB()
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	tables := append(models.OpsTables(), models.KnowledgeTables()...)
	tables = append(tables, &models.User{}, &models.BusiGroup{})
	require.NoError(t, db.AutoMigrate(tables...))
	require.NoError(t, db.Create(&models.User{Id: 1, Username: "tool-operator", Roles: "Admin"}).Error)
	require.NoError(t, db.Create(&models.BusiGroup{Id: 1, Name: "Fixture"}).Error)
	s := &ops.Service{DB: db, Knowledge: &knowledge.Store{DB: db, DataDir: t.TempDir()}}
	c, err := s.Create(1, 1, "Unknown incident", map[string]string{"environment": "test"})
	require.NoError(t, err)
	e, err := s.AddManualEvidence(1, c.ID, "Current application failure confirmed by operator")
	require.NoError(t, err)
	input := ops.PlanInput{Revision: c.Revision, Title: "Local recovery", Scope: "Only current test application", Rationale: "Observed failure, root cause not confirmed", EvidenceIDs: []int64{e.ID}, Spec: knowledge.Spec{Summary: "Verify recovery", Steps: []knowledge.Step{{Key: "inspect", Kind: "manual", Title: "Check actual error"}}, Checks: []knowledge.Check{{Key: "business", Kind: "manual", Title: "Round trip passes", Required: true}}, Escalation: "Stop and ask the test owner on unexpected results"}}
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	_, exists := aiagent.GetBuiltinToolDef("propose_ops_plan")
	require.True(t, exists)
	args, _ := json.Marshal(map[string]string{"plan_json": string(raw)})
	_, err = s.Queue(1, c.ID, "agent")
	require.NoError(t, err)
	s.Runner = func(ctx context.Context, scope ops.Scope) (string, error) {
		deps := &aiagent.ToolDeps{Ops: s.Scoped(scope), OpsScope: &scope}
		fake := &opsBatchModel{args: string(args)}
		runner := aiagent.NewAgent(&aiagent.AgentConfig{RestrictTools: true, MaxIterations: 3, MaxToolCalls: 12, Timeout: 3000, Tools: aiagent.GetBuiltinToolDefs(OpsToolNames)}, aiagent.WithLLMClient(fake), aiagent.WithToolDeps(deps))
		result, runErr := runner.Run(ctx, &aiagent.AgentRequest{UserMessage: "Prepare plan"})
		require.NoError(t, runErr)
		require.True(t, result.Success)
		require.Contains(t, result.Content, "暂停")
		require.Equal(t, 1, fake.calls)

		return "Draft only, requires operator", nil
	}
	require.NoError(t, s.RunNext(context.Background()))
	d, err := s.Detail(1, c.ID)
	require.NoError(t, err)
	require.Len(t, d.Plans, 1)
	require.Zero(t, d.Case.PlanID)
	for _, action := range []string{"tool_started", "tool_finished"} {
		var count int64
		require.NoError(t, db.Model(&models.OpsAudit{}).Where("case_id = ? AND action = ?", c.ID, action).Count(&count).Error)
		require.Equal(t, int64(1), count)
	}
	// An idle or forged investigation cannot reuse the same proposal tool.
	scope := ops.Scope{UserID: 1, CaseID: c.ID, RunID: 99, Fence: 1}
	_, _, err = aiagent.ExecuteBuiltinTool(context.Background(), &aiagent.ToolDeps{Ops: s.Scoped(scope), OpsScope: &scope}, "propose_ops_plan", nil, string(args))
	require.Error(t, err)
	for _, name := range OpsToolNames {
		require.NotEqual(t, "confirm_ops_plan", name)
	}
}

type opsBatchModel struct {
	args  string
	calls int
}

func (m *opsBatchModel) Name() string { return "test-batch" }
func (m *opsBatchModel) Generate(context.Context, *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	m.calls++
	return &llm.GenerateResponse{ToolCalls: []llm.ToolCall{{ID: "draft", Name: "propose_ops_plan", Arguments: m.args}, {ID: "late-read", Name: "collect_ops_metric", Arguments: `{"query":"cpu","window_seconds":60}`}}}, nil
}
func (m *opsBatchModel) GenerateStream(context.Context, *llm.GenerateRequest) (<-chan llm.StreamChunk, error) {
	panic("unexpected streaming")
}
