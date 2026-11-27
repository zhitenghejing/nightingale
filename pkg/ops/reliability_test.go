package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/stretchr/testify/require"
	"testing"
)

func completeInvestigation(t *testing.T, s *Service, c *models.OpsCase) {
	t.Helper()
	spec, err := s.activeProcedure(1, c)
	require.NoError(t, err)
	for _, step := range spec.Steps {
		var e *models.OpsEvidence
		if step.Kind == "metric" {
			e, err = s.persistEvidence(c, 1, "metric", "test", "ok", s.now(), s.now(), s.now(), MetricResult{RequestedQuery: step.Query, Status: "ok", Points: []Point{{At: s.now(), Value: 1, ObservedAt: s.now()}}})
		} else {
			e, err = s.AddManualEvidence(1, c.ID, "Operator verified the required precondition")
		}
		require.NoError(t, err)
		require.NoError(t, s.RecordStep(1, c.ID, step.Key, "completed", "Verified before action", []int64{e.ID}))
	}
}
func TestActionPreconditionsAreEnforcedAtProposalAndApproval(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	_, err := s.ProposeAction(context.Background(), 1, c.ID, "reclaim")
	require.Error(t, err, "missing investigation must prevent proposal")
	completeInvestigation(t, s, c)
	a, err := s.ProposeAction(context.Background(), 1, c.ID, "reclaim")
	require.NoError(t, err)
	var step models.OpsStep
	require.NoError(t, s.DB.Where("case_id = ?", c.ID).First(&step).Error)
	require.NoError(t, s.RecordStep(1, c.ID, "inspect", "failed", "New assessment invalidates prerequisite", func() []int64 {
		var ids []int64
		require.NoError(t, json.Unmarshal([]byte(step.EvidenceIDs), &ids))
		return ids
	}()))
	_, err = s.Approve(context.Background(), 1, c.ID, a.ID, true)
	require.Error(t, err, "latest failed prerequisite must prevent approval even without new evidence")
	var stored models.OpsAction
	require.NoError(t, s.DB.First(&stored, a.ID).Error)
	require.Equal(t, "proposed", stored.State)
}
func TestAuthoritativeTargetConditionMatchesWithoutContextCopy(t *testing.T) {
	s, now := fixture(t)
	c, err := s.Create(1, 1, "Target rule", map[string]string{"environment": "test", "target_ident": "spoof"})
	require.NoError(t, err)
	require.NoError(t, s.DB.Model(&models.OpsCase{}).Where("id = ?", c.ID).Update("target_ident", "host-a").Error)
	spec := exampleSpec(*now)
	spec.Conditions = map[string]string{"target_ident": "host-a"}
	k, err := s.Knowledge.Save(1, 0, 1, 0, "Target-specific knowledge", spec)
	require.NoError(t, err)
	v, err := s.Knowledge.Publish(1, k.ID, k.Revision)
	require.NoError(t, err)
	matches, err := s.Matches(1, c.ID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.True(t, matches[0].Applicable)
	require.NoError(t, s.SelectKnowledge(1, c.ID, c.Revision, v.ID, ""))
}

func TestNewFaultPreparationUsesAlertSelectorsAndKeepsEmptyDataUnknown(t *testing.T) {
	s, now := fixture(t)
	event := models.AlertHisEvent{IncidentID: "new-fault", DispatchID: "new-fault", IncidentSequence: 1, GroupId: 1, TargetIdent: "host-a", DatasourceId: 1, RuleName: "CPU absent", PromQl: `absent_over_time(cpu_usage_active{ident="host-a",cpu="cpu-total"}[5m])`, LastEvalTime: *now}
	c, err := s.Ingest(1, event)
	require.NoError(t, err)
	calls := 0
	s.Query = func(_ context.Context, _ int64, _ *models.OpsCase, q string, _, _ int64) (MetricResult, error) {
		calls++
		require.Contains(t, q, "cpu_usage_active")
		require.NotContains(t, q, "absent")
		return MetricResult{Status: "empty"}, nil
	}
	p, err := s.PreparePlan(context.Background(), 1, c.ID)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	plan, err := s.SavePreparedPlan(1, c.ID, p, s.OutlinePlan(p), "system_outline")
	require.NoError(t, err)
	require.Equal(t, "draft", plan.State)
	d, err := s.Detail(1, c.ID)
	require.NoError(t, err)
	require.Zero(t, d.Case.PlanID)
	require.Len(t, d.Actions, 0)
	require.Equal(t, "empty", d.Evidence[0].Status)
	spec, err := knowledge.Decode(plan.Payload)
	require.NoError(t, err)
	require.Empty(t, spec.Actions)
	require.Equal(t, "manual", spec.Checks[0].Kind)
}

func TestFailureHandoffPreservesFactsWithoutReasoningAndReleasesRun(t *testing.T) {
	s, _ := fixture(t)
	c, err := s.Create(1, 1, "Failed model", nil)
	require.NoError(t, err)
	_, err = s.AddManualEvidence(1, c.ID, "Operator observed a failure; cause unknown")
	require.NoError(t, err)
	_, err = s.Queue(1, c.ID, "agent")
	require.NoError(t, err)
	s.Runner = func(context.Context, Scope) (string, error) {
		return "", fmt.Errorf("model budget exhausted <think>PRIVATE_REASONING</think>")
	}
	require.NoError(t, s.RunNext(context.Background()))
	d, err := s.Detail(1, c.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", d.Runs[0].State)
	require.Nil(t, d.Runs[0].ActiveCaseID)
	require.Equal(t, "agent_failed", d.Case.WaitReason)
	require.Contains(t, d.Runs[0].Result, "证据 #")
	require.Contains(t, d.Runs[0].Result, "值班人员")
	require.NotContains(t, d.Runs[0].Result, "PRIVATE_REASONING")
}
func TestHumanBackgroundChangeCancelsAndFencesOldRun(t *testing.T) {
	s, _ := fixture(t)
	c, err := s.Create(1, 1, "Concurrent edit", nil)
	require.NoError(t, err)
	_, err = s.Queue(1, c.ID, "agent")
	require.NoError(t, err)
	s.Runner = func(_ context.Context, p Scope) (string, error) {
		require.NoError(t, s.UpdateContext(1, c.ID, c.Revision, map[string]string{"system_version": "new"}, "operator_review"))
		_, err := s.Scoped(p).Get(1, c.ID, false)
		require.Error(t, err)
		return "Old result must not overwrite human input", err
	}
	require.NoError(t, s.RunNext(context.Background()))
	d, err := s.Detail(1, c.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", d.Runs[0].State)
	require.Equal(t, "operator_review", d.Case.WaitReason)
	require.NotContains(t, d.Runs[0].Result, "Old result")
	var stored models.OpsRun
	require.NoError(t, s.DB.First(&stored, d.Runs[0].ID).Error)
	require.Empty(t, stored.Result)
}
func TestRevokedAccountDoesNotLeaveWorkerLeaseRunning(t *testing.T) {
	s, _ := fixture(t)
	c, err := s.Create(1, 1, "Permission revoked", nil)
	require.NoError(t, err)
	_, err = s.Queue(1, c.ID, "agent")
	require.NoError(t, err)
	s.Runner = func(context.Context, Scope) (string, error) {
		require.NoError(t, s.DB.Model(&models.User{}).Where("id = ?", 1).Update("disabled", 1).Error)
		return "", fmt.Errorf("account revoked")
	}
	require.NoError(t, s.RunNext(context.Background()))
	var r models.OpsRun
	require.NoError(t, s.DB.Where("case_id = ?", c.ID).First(&r).Error)
	require.Equal(t, "failed", r.State)
	require.Nil(t, r.ActiveCaseID)
}
func TestActionExplicitBranchPrerequisitesAndFreshness(t *testing.T) {
	s, now := fixture(t)
	spec := exampleSpec(*now)
	spec.Steps = append(spec.Steps, knowledge.Step{Key: "alternative", Title: "Other branch", Kind: "manual"})
	spec.Actions[0].RequiredSteps = []string{"inspect"}
	k, err := s.Knowledge.Save(1, 0, 1, 0, "Branch", spec)
	require.NoError(t, err)
	v, err := s.Knowledge.Publish(1, k.ID, k.Revision)
	require.NoError(t, err)
	c, err := s.Create(1, 1, "Branch", map[string]string{"environment": "test", "system_version": "1"})
	require.NoError(t, err)
	require.NoError(t, s.SelectKnowledge(1, c.ID, c.Revision, v.ID, ""))
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	e, err := s.persistEvidence(c, 1, "metric", "fixture", "ok", *now, *now, *now, MetricResult{RequestedQuery: "demo_used", Status: "ok"})
	require.NoError(t, err)
	require.NoError(t, s.RecordStep(1, c.ID, "inspect", "completed", "Measured prerequisite", []int64{e.ID}))
	_, err = s.ProposeAction(context.Background(), 1, c.ID, "reclaim")
	require.NoError(t, err, "alternative branch is not required")
	*now += 301
	_, err = s.ProposeAction(context.Background(), 1, c.ID, "reclaim")
	require.Error(t, err, "expired precondition must not be reused")
	spec.Actions[0].RequiredSteps = []string{"missing"}
	require.Error(t, knowledge.ValidateProcedure(spec))
}

func TestActionReadinessUsesTheSameGateAndStopsRepeatedProposals(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	ready, err := s.ActionReadiness(1, c.ID)
	require.NoError(t, err)
	require.Equal(t, false, ready[0]["ready"])
	completeInvestigation(t, s, c)
	ready, err = s.ActionReadiness(1, c.ID)
	require.NoError(t, err)
	require.Equal(t, true, ready[0]["ready"])
	require.Equal(t, "reclaim", ready[0]["key"])
	_, err = s.ProposeAction(context.Background(), 1, c.ID, "reclaim")
	require.NoError(t, err)
	ready, err = s.ActionReadiness(1, c.ID)
	require.NoError(t, err)
	require.Equal(t, false, ready[0]["ready"])
	_, err = s.AddManualEvidence(1, c.ID, "New facts invalidate the previous proposal")
	require.NoError(t, err)
	ready, err = s.ActionReadiness(1, c.ID)
	require.NoError(t, err)
	require.Equal(t, true, ready[0]["ready"])
	_, err = s.ActionReadiness(3, c.ID)
	require.Error(t, err)
}

func TestOperatorDecisionsClearSatisfiedHandoffReasons(t *testing.T) {
	s, _ := fixture(t)
	c, err := s.Create(1, 1, "Operator handoff", map[string]string{"environment": "test"})
	require.NoError(t, err)
	require.NoError(t, s.UpdateContext(1, c.ID, c.Revision, nil, "plan_confirmation"))
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	p := proposeLocalPlan(t, s, c)
	c = confirmLocalPlan(t, s, c, p)
	require.Empty(t, c.WaitReason)
	completeInvestigation(t, s, c)
	_, err = s.Queue(1, c.ID, "agent")
	require.NoError(t, err)
	s.Runner = func(ctx context.Context, p Scope) (string, error) {
		_, err := s.Scoped(p).ProposeAction(ctx, 1, c.ID, "repair")
		return "Waiting for operator", err
	}
	require.NoError(t, s.RunNext(context.Background()))
	d, err := s.Detail(1, c.ID)
	require.NoError(t, err)
	require.Equal(t, "action_confirmation", d.Case.WaitReason)
	_, err = s.Approve(context.Background(), 1, c.ID, d.Actions[0].ID, true)
	require.NoError(t, err)
	require.NoError(t, s.ManualActionResult(1, c.ID, d.Actions[0].ID, "Operator verified outcome", true))
	d, err = s.Detail(1, c.ID)
	require.NoError(t, err)
	require.Empty(t, d.Case.WaitReason)
}
