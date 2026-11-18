package ops

import (
	"context"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/stretchr/testify/require"
)

func newPlanInput(t *testing.T, s *Service, c *models.OpsCase) PlanInput {
	t.Helper()
	e, err := s.AddManualEvidence(1, c.ID, "Operator confirmed application errors and queue delay; cause remains a hypothesis")
	require.NoError(t, err)
	return PlanInput{Revision: c.Revision, Title: "New fault, local plan", Scope: "This case and its application instance only", Rationale: "Confirmed queue delay; hypothesized connection exhaustion; root cause remains unknown", EvidenceIDs: []int64{e.ID}, Spec: knowledge.Spec{Summary: "Restore report processing and verify a round trip", Conditions: map[string]string{"environment": "test"}, Steps: []knowledge.Step{{Key: "inspect", Title: "Inspect application errors", Kind: "manual", Instruction: "Inspect current application logs", Expected: "Record actual errors"}}, Actions: []knowledge.Action{{Key: "repair", Title: "Operator repairs the test application", Kind: "manual", Instruction: "Apply the scoped repair after rechecking target", Risk: "Stop on any unexpected business impact", Rollback: "Restore previous configuration and escalate"}}, Checks: []knowledge.Check{{Key: "metric", Title: "Queue below threshold", Kind: "metric", Query: "queue_size", Operator: "<", Threshold: 5, FreshSeconds: 30, Required: true}, {Key: "business", Title: "Report round trip succeeds", Kind: "manual", Required: true}}, Escalation: "Stop and contact the application owner if the hypothesis is not confirmed"}}
}
func proposeLocalPlan(t *testing.T, s *Service, c *models.OpsCase) *models.OpsPlan {
	t.Helper()
	p, err := s.ProposePlan(1, c.ID, newPlanInput(t, s, c))
	require.NoError(t, err)
	return p
}
func confirmLocalPlan(t *testing.T, s *Service, c *models.OpsCase, p *models.OpsPlan) *models.OpsCase {
	t.Helper()
	current, err := s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.NoError(t, s.DecidePlan(1, c.ID, p.ID, current.Revision, true, "Operator reviewed scope, evidence, risk, rollback and recovery standard"))
	current, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	return current
}
func TestCasePlanClosesNewFaultWithoutPublishedKnowledge(t *testing.T) {
	s, now := fixture(t)
	c, err := s.Create(1, 1, "Previously unseen fault", map[string]string{"environment": "test"})
	require.NoError(t, err)
	p := proposeLocalPlan(t, s, c)
	_, err = s.ProposeAction(context.Background(), 1, c.ID, "repair")
	require.Error(t, err, "draft cannot authorize an action")
	_, err = s.Verify(context.Background(), 1, c.ID)
	require.Error(t, err)
	c = confirmLocalPlan(t, s, c, p)
	require.Zero(t, c.KnowledgeVersionID)
	require.Equal(t, p.ID, c.PlanID)
	require.Error(t, s.DecidePlan(1, c.ID, p.ID, c.Revision, true, "Repeat confirmation"))
	// Confirmation starts a new stage: supporting evidence stays historical.
	require.Error(t, s.RecordStep(1, c.ID, "inspect", "completed", "Old evidence", planEvidenceIDs(*p)))
	evidence, err := s.AddManualEvidence(1, c.ID, "Current application errors inspected")
	require.NoError(t, err)
	require.NoError(t, s.RecordStep(1, c.ID, "inspect", "completed", "Current observations", []int64{evidence.ID}))
	a, err := s.ProposeAction(context.Background(), 1, c.ID, "repair")
	require.NoError(t, err)
	require.Equal(t, p.ID, a.PlanID)
	require.Zero(t, a.VersionID)
	a, err = s.Approve(context.Background(), 1, c.ID, a.ID, true)
	require.NoError(t, err)
	require.Equal(t, "awaiting_manual", a.State)
	require.Error(t, s.Close(1, c.ID, c.Revision, "resolved", "Premature", ""))
	*now++
	require.NoError(t, s.ManualActionResult(1, c.ID, a.ID, "Scoped repair applied and checked", true))
	*now++
	s.Query = func(_ context.Context, _ int64, _ *models.OpsCase, q string, _, end int64) (MetricResult, error) {
		return MetricResult{Status: "ok", Query: q, Points: []Point{{At: end, Value: 1}}}, nil
	}
	checks, err := s.Verify(context.Background(), 1, c.ID)
	require.NoError(t, err)
	require.Equal(t, p.ID, checks[0].PlanID)
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.Error(t, s.Close(1, c.ID, c.Revision, "resolved", "Only metric is normal", ""))
	require.NoError(t, s.ConfirmCheck(1, c.ID, "business", "Round trip failed", false))
	require.Error(t, s.Close(1, c.ID, c.Revision, "resolved", "Business still broken", ""))
	require.NoError(t, s.ConfirmCheck(1, c.ID, "business", "Report round trip checked and passed", true))
	require.NoError(t, s.Close(1, c.ID, c.Revision, "temporary", "Service recovered; root cause still under investigation", "Application owner follows up the suspected connection leak"))
	draft, err := s.Feedback(1, c.ID, "Verified recovery; reuse needs review")
	require.NoError(t, err)
	require.Zero(t, draft.CurrentVersionID)
	spec, err := knowledge.Decode(draft.Draft)
	require.NoError(t, err)
	require.Zero(t, spec.ValidatedAt)
	require.Zero(t, spec.ReviewAt)
	require.Len(t, spec.Actions, 1)
	_, err = s.Knowledge.Publish(1, draft.ID, draft.Revision)
	require.Error(t, err, "feedback must not silently become reusable knowledge")
}
func TestCasePlanScopePermissionsAndAgentConfirmation(t *testing.T) {
	s, _ := fixture(t)
	c, err := s.Create(1, 1, "Unknown fault", map[string]string{"environment": "test"})
	require.NoError(t, err)
	in := newPlanInput(t, s, c)
	_, err = s.ProposePlan(2, c.ID, in)
	require.Error(t, err)
	_, err = s.ProposePlan(3, c.ID, in)
	require.Error(t, err)
	other, err := s.Create(1, 2, "Other group", map[string]string{"environment": "test"})
	require.NoError(t, err)
	_, err = s.ProposePlan(1, other.ID, in)
	require.Error(t, err, "cannot borrow another case's evidence")
	p, err := s.ProposePlan(1, c.ID, in)
	require.NoError(t, err)
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.Error(t, s.DecidePlan(2, c.ID, p.ID, c.Revision, true, "Reader"))
	require.Error(t, s.DecidePlan(1, other.ID, p.ID, other.Revision, true, "Other case"))
	require.Error(t, s.Scoped(Scope{UserID: 1, CaseID: c.ID}).DecidePlan(1, c.ID, p.ID, c.Revision, true, "Agent"))
	require.NoError(t, s.DecidePlan(1, c.ID, p.ID, c.Revision, false, "Need more evidence"))
	require.Error(t, s.DecidePlan(1, c.ID, p.ID, c.Revision+1, true, "Cannot revive rejected content"))
}
func TestCasePlanStalenessAndVersionIsolation(t *testing.T) {
	s, _ := fixture(t)
	c, err := s.Create(1, 1, "New fault", map[string]string{"environment": "test"})
	require.NoError(t, err)
	p := proposeLocalPlan(t, s, c)
	_, err = s.AddManualEvidence(1, c.ID, "New fact contradicts original proposal")
	require.NoError(t, err)
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.Error(t, s.DecidePlan(1, c.ID, p.ID, c.Revision, true, "Stale evidence"))
	p2 := proposeLocalPlan(t, s, c)
	require.Equal(t, int64(2), p2.Sequence)
	c = confirmLocalPlan(t, s, c, p2)
	completeInvestigation(t, s, c)
	a, err := s.ProposeAction(context.Background(), 1, c.ID, "repair")
	require.NoError(t, err)
	require.NoError(t, s.UpdateContext(1, c.ID, c.Revision, map[string]string{"business": "Newly identified affected service"}, ""))
	_, err = s.Approve(context.Background(), 1, c.ID, a.ID, true)
	require.Error(t, err)
	_, err = s.ProposeAction(context.Background(), 1, c.ID, "repair")
	require.Error(t, err, "changed background needs new plan confirmation")
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	p3 := proposeLocalPlan(t, s, c)
	c = confirmLocalPlan(t, s, c, p3)
	var old models.OpsPlan
	require.NoError(t, s.DB.First(&old, p2.ID).Error)
	require.Equal(t, p2.Payload, old.Payload)
	require.Equal(t, "confirmed", old.State)
	require.Equal(t, p3.ID, c.PlanID)
	var count int64
	require.NoError(t, s.DB.Model(&models.KnowledgeItem{}).Count(&count).Error)
	require.Zero(t, count)
}
func TestCasePlanValidationIntegrityAndUnresolvedActions(t *testing.T) {
	s, _ := fixture(t)
	c, err := s.Create(1, 1, "Unknown", map[string]string{"environment": "test"})
	require.NoError(t, err)
	in := newPlanInput(t, s, c)
	bad := in
	bad.EvidenceIDs = nil
	_, err = s.ProposePlan(1, c.ID, bad)
	require.Error(t, err)
	bad = in
	bad.Spec = in.Spec
	bad.Spec.Checks = nil
	_, err = s.ProposePlan(1, c.ID, bad)
	require.Error(t, err)
	bad = in
	bad.Spec = in.Spec
	bad.Spec.ValidatedAt = 1
	_, err = s.ProposePlan(1, c.ID, bad)
	require.Error(t, err)
	p, err := s.ProposePlan(1, c.ID, in)
	require.NoError(t, err)
	require.NoError(t, s.DB.Model(&models.OpsPlan{}).Where("id = ?", p.ID).Update("scope", "Unexpected altered target").Error)
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.Error(t, s.DecidePlan(1, c.ID, p.ID, c.Revision, true, "Tampered"))
	p = proposeLocalPlan(t, s, c)
	c = confirmLocalPlan(t, s, c, p)
	completeInvestigation(t, s, c)
	a, err := s.ProposeAction(context.Background(), 1, c.ID, "repair")
	require.NoError(t, err)
	_, err = s.Approve(context.Background(), 1, c.ID, a.ID, true)
	require.NoError(t, err)
	replacement := proposeLocalPlan(t, s, c)
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.Error(t, s.DecidePlan(1, c.ID, replacement.ID, c.Revision, true, "Cannot switch during pending manual operation"))
}
func TestCasePlanAgentCanProposeButCannotActivate(t *testing.T) {
	s, _ := fixture(t)
	c, err := s.Create(1, 1, "No knowledge", map[string]string{"environment": "test"})
	require.NoError(t, err)
	in := newPlanInput(t, s, c)
	_, err = s.Queue(1, c.ID, "agent")
	require.NoError(t, err)
	s.Runner = func(_ context.Context, scope Scope) (string, error) {
		scoped := s.Scoped(scope)
		p, e := scoped.ProposePlan(scope.UserID, c.ID, in)
		require.NoError(t, e)
		current, e := s.Get(1, c.ID, false)
		require.NoError(t, e)
		require.Error(t, scoped.DecidePlan(1, c.ID, p.ID, current.Revision, true, "Agent self approval"))
		_, e = scoped.ProposeAction(context.Background(), 1, c.ID, "repair")
		require.Error(t, e)
		return "Draft proposed; waiting for operator", nil
	}
	require.NoError(t, s.RunNext(context.Background()))
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.Zero(t, c.PlanID)
	d, err := s.Detail(1, c.ID)
	require.NoError(t, err)
	require.Len(t, d.Plans, 1)
	require.Equal(t, "draft", d.Plans[0].State)
}

func TestCasePlanSwitchToKnowledgeRequiresReasonAndFreshChecks(t *testing.T) {
	s, now := fixture(t)
	c, v := caseWithKnowledge(t, s, *now)
	p := proposeLocalPlan(t, s, c)
	c = confirmLocalPlan(t, s, c, p)
	require.NoError(t, s.ConfirmCheck(1, c.ID, "business", "Previous plan round trip passes", true))
	require.Error(t, s.SelectKnowledge(1, c.ID, c.Revision, v.ID, ""))
	require.NoError(t, s.SelectKnowledge(1, c.ID, c.Revision, v.ID, "Published guide now matches the verified conditions"))
	c, err := s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.Zero(t, c.PlanID)
	require.Equal(t, v.ID, c.KnowledgeVersionID)
	require.Error(t, s.Close(1, c.ID, c.Revision, "resolved", "Old checks must not count", ""))
}

func TestCasePlanWithoutActionsUsesEmptyArrayForClients(t *testing.T) {
	s, _ := fixture(t)
	c, err := s.Create(1, 1, "Observation only", map[string]string{"environment": "test"})
	require.NoError(t, err)
	in := newPlanInput(t, s, c)
	in.Spec.Actions = nil
	p, err := s.ProposePlan(1, c.ID, in)
	require.NoError(t, err)
	spec, err := knowledge.Decode(p.Payload)
	require.NoError(t, err)
	require.NotNil(t, spec.Actions)
	require.Empty(t, spec.Actions)
}
