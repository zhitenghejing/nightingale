package router

import (
	"context"
	"github.com/ccfos/nightingale/v6/aiagent/llm"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/ops"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"testing"
)

type planDraftModel struct {
	valid bool
	calls int
	t     *testing.T
}

func (m *planDraftModel) Name() string { return "draft-test" }
func (m *planDraftModel) Generate(_ context.Context, r *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	m.calls++
	require.Len(m.t, r.Tools, 1)
	require.Equal(m.t, "submit_ops_plan_draft", r.Tools[0].Name)
	require.Contains(m.t, r.Messages[1].Content, "rule_inputs")
	require.NotContains(m.t, r.Messages[1].Content, "API_KEY")
	if !m.valid {
		return &llm.GenerateResponse{Content: "<think>PRIVATE_REASONING</think>invalid JSON"}, nil
	}
	return &llm.GenerateResponse{Content: "<think>PRIVATE_REASONING</think>", ToolCalls: []llm.ToolCall{{Name: "submit_ops_plan_draft", Arguments: `{"title":"Draft for this case","scope":"Current authorized target only","rationale":"Facts: report stored; Hypothesis: unknown; Missing: owner confirmation","spec":{"summary":"Inspect actual state","steps":[{"key":"inspect","title":"Operator checks current state","kind":"manual","instruction":"Record actual evidence"}],"actions":[],"checks":[{"key":"business","title":"Business and data integrity checked","kind":"manual","required":true}],"escalation":"Stop and contact the business group operator if facts are missing"}}`}}}, nil
}
func (m *planDraftModel) GenerateStream(context.Context, *llm.GenerateRequest) (<-chan llm.StreamChunk, error) {
	panic("unexpected streaming")
}

func TestOpsUnknownFaultDraftPhaseIsBoundedAndWaitsForHuman(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "model_draft", false: "safe_outline_on_invalid_output"}[valid], func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(t.TempDir()+"/draft.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			conn, _ := db.DB()
			conn.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = conn.Close() })
			tables := append(models.OpsTables(), models.KnowledgeTables()...)
			tables = append(tables, &models.User{}, &models.BusiGroup{})
			require.NoError(t, db.AutoMigrate(tables...))
			require.NoError(t, db.Create(&models.User{Id: 1, Username: "admin", Roles: "Admin"}).Error)
			require.NoError(t, db.Create(&models.BusiGroup{Id: 1, Name: "test"}).Error)
			s := &ops.Service{DB: db, Knowledge: &knowledge.Store{DB: db, DataDir: t.TempDir()}}
			c, err := s.Create(1, 1, "Unknown fault", map[string]string{"environment": "test"})
			require.NoError(t, err)
			_, err = s.Queue(1, c.ID, "agent")
			require.NoError(t, err)
			model := &planDraftModel{valid: valid, t: t}
			s.Runner = func(ctx context.Context, p ops.Scope) (string, error) {
				return draftOpsPlan(ctx, s.Scoped(p), p, model)
			}
			require.NoError(t, s.RunNext(context.Background()))
			d, err := s.Detail(1, c.ID)
			require.NoError(t, err)
			require.Len(t, d.Plans, 1)
			require.Equal(t, "draft", d.Plans[0].State)
			require.Zero(t, d.Case.PlanID)
			require.Empty(t, d.Actions)
			require.Equal(t, "plan_confirmation", d.Case.WaitReason)
			require.Equal(t, "awaiting_operator", d.Runs[0].State)
			require.NotContains(t, d.Runs[0].Result, "PRIVATE_REASONING")
			require.NotContains(t, string(d.Plans[0].Payload), "PRIVATE_REASONING")
			calls := 1
			origin := "model"
			if !valid {
				calls = 2
				origin = "system_outline"
				require.Contains(t, d.Plans[0].Title, "待人工完善")
			}
			require.Equal(t, calls, model.calls)
			seen := false
			for _, a := range d.Audit {
				if a.Action == "plan_draft_origin" {
					require.Contains(t, string(a.Detail), origin)
					seen = true
				}
			}
			require.True(t, seen)
		})
	}
}

func TestOpsDraftFormattingNeverDispatchesUnknownTools(t *testing.T) {
	_, err := opsDraftPayload(&llm.GenerateResponse{ToolCalls: []llm.ToolCall{{Name: "dispatch_task", Arguments: `{"host":"other"}`}}}, true)
	require.Error(t, err)
	_, err = opsDraftPayload(&llm.GenerateResponse{ToolCalls: []llm.ToolCall{{Name: opsDraftName(true), Arguments: "{}"}, {Name: opsDraftName(true), Arguments: "{}"}}}, true)
	require.Error(t, err)
	raw, err := opsDraftPayload(&llm.GenerateResponse{ToolCalls: []llm.ToolCall{{Name: opsDraftName(false), Arguments: `{"title":"Draft","spec":{}}`}}, Content: "<think>PRIVATE</think>"}, false)
	require.NoError(t, err)
	require.NotContains(t, raw, "PRIVATE")
}
