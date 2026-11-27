package ops

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"github.com/stretchr/testify/require"
)

func TestMergeInvalidatesVerificationAndPreservesSourceEvidence(t *testing.T) {
	s, now := fixture(t)
	first := models.AlertHisEvent{DispatchID: "merge-a", IncidentID: "merge-a", IncidentSequence: 1, GroupId: 1, TargetIdent: "same", DatasourceId: 5, LastEvalTime: *now}
	second := first
	second.DispatchID = "merge-b"
	second.IncidentID = "merge-b"
	a, err := s.Ingest(1, first)
	require.NoError(t, err)
	b, err := s.Ingest(1, second)
	require.NoError(t, err)
	evidence, err := s.AddManualEvidence(1, b.ID, "Original source observation")
	require.NoError(t, err)
	require.NoError(t, s.Merge(1, a.ID, a.Revision, b.ID, b.Revision, "same monitored target"))
	dest, err := s.Detail(1, a.ID)
	require.NoError(t, err)
	require.Len(t, dest.Episodes, 2)
	require.Equal(t, a.Generation+1, dest.Case.Generation)
	source, err := s.Detail(1, b.ID)
	require.NoError(t, err)
	require.Equal(t, "merged", source.Case.CloseType)
	require.Equal(t, evidence.ID, source.Evidence[0].ID)
	require.NoError(t, s.DB.Model(&models.OpsCase{}).Where("id = ?", a.ID).Update("target_ident", "different").Error)
	require.Error(t, s.Merge(1, a.ID, dest.Case.Revision, b.ID, source.Case.Revision, "invalid"))
}
func TestSourceRetentionGapPersistsUntilOperatorAcknowledges(t *testing.T) {
	s, now := fixture(t)
	s.SourceRetentionSeconds = 86400
	require.NoError(t, s.SavePolicy(1, models.OpsPolicy{GroupID: 1, Enabled: true, ServiceUserID: 2, RuleIDs: "[]"}))
	require.NoError(t, s.DB.Model(&models.OpsPolicy{}).Where("group_id = ?", 1).Update("scan_at", *now-90000).Error)
	require.NoError(t, s.Scan(context.Background()))
	rows, err := s.Policies(1)
	require.NoError(t, err)
	require.Positive(t, rows[0].GapSince)
	require.NoError(t, s.Scan(context.Background()))
	rows, err = s.Policies(1)
	require.NoError(t, err)
	require.Positive(t, rows[0].GapSince)
	require.Error(t, s.AcknowledgeGap(2, 1, "not allowed"))
	require.Error(t, s.AcknowledgeGap(1, 1, ""))
	require.NoError(t, s.AcknowledgeGap(1, 1, "Reconciled retained source with operator records"))
	rows, err = s.Policies(1)
	require.NoError(t, err)
	require.Zero(t, rows[0].GapSince)
}
func TestContextGenerationAndUnknownExecutionBlockClosure(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	s.Execute = func(context.Context, int64, *models.OpsCase, *models.OpsAction) (interface{}, error) {
		return nil, context.DeadlineExceeded
	}
	completeInvestigation(t, s, c)
	a, err := s.ProposeAction(context.Background(), 1, c.ID, "reclaim")
	require.NoError(t, err)
	_, err = s.Approve(context.Background(), 1, c.ID, a.ID, true)
	require.NoError(t, err)
	require.Error(t, s.Close(1, c.ID, c.Revision, "cancelled", "Stop processing", ""))
	require.Error(t, s.UpdateContext(1, c.ID, c.Revision, map[string]string{"business": "changed"}, ""))
	require.NoError(t, s.ManualActionResult(1, c.ID, a.ID, "Executor checked: not executed", false))
	require.NoError(t, s.UpdateContext(1, c.ID, c.Revision, map[string]string{"business": "changed"}, ""))
	current, err := s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.Equal(t, c.Generation+1, current.Generation)
}
func TestFailedEvidenceCannotCompleteStepAndToolIntentIsDurable(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	s.Query = func(context.Context, int64, *models.OpsCase, string, int64, int64) (MetricResult, error) {
		return MetricResult{}, context.DeadlineExceeded
	}
	e, err := s.CollectMetric(context.Background(), 1, c.ID, "demo_used", 0)
	require.NoError(t, err)
	require.Equal(t, "error", e.Status)
	require.Error(t, s.RecordStep(1, c.ID, "inspect", "completed", "invented success", []int64{e.ID}))
	require.NoError(t, s.RecordStep(1, c.ID, "inspect", "failed", "collection timed out", []int64{e.ID}))
	r := models.OpsRun{CaseID: c.ID, ActiveCaseID: &c.ID, UserID: 2, State: "running", Fence: 7, LeaseUntil: *now + 90, Mode: "agent"}
	require.NoError(t, s.DB.Create(&r).Error)
	scoped := s.Scoped(Scope{UserID: 2, CaseID: c.ID, RunID: r.ID, Fence: 7})
	call, err := scoped.BeginTool(2, c.ID, "get_ops_context", map[string]string{})
	require.NoError(t, err)
	require.NoError(t, scoped.FinishTool(2, c.ID, call, "get_ops_context", "context", nil))
	require.NoError(t, s.DB.Model(&models.OpsCase{}).Where("id = ?", c.ID).Update("context", opsauth.JSON(map[string]string{"password": "hidden", "system_version": "1", "environment": "test"})).Error)
	result, err := scoped.AgentContext(2, c.ID)
	require.NoError(t, err)
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "hidden")
	require.NotContains(t, string(raw), "snapshot")
}
func TestGeneratedDraftCannotAssertValidationOrActions(t *testing.T) {
	s, now := fixture(t)
	source, err := s.Knowledge.AddSource(1, 1, "runbook.md", "verified source", strings.NewReader("# Disk procedure\nCheck metrics and contact the owner."))
	require.NoError(t, err)
	input, err := s.Knowledge.DraftInput(1, 1, []int64{source.ID})
	require.NoError(t, err)
	require.Contains(t, input, source.SHA256)
	result := map[string]interface{}{"title": "Generated procedure", "spec": exampleSpec(*now)}
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	draft, err := s.Knowledge.GeneratedDraft(1, 1, []int64{source.ID}, string(raw))
	require.NoError(t, err)
	spec, err := knowledge.Decode(draft.Draft)
	require.NoError(t, err)
	require.Zero(t, spec.ValidatedAt)
	require.Zero(t, spec.ReviewAt)
	require.Empty(t, spec.Actions)
	_, err = s.Knowledge.Publish(1, draft.ID, draft.Revision)
	require.Error(t, err)
	list, err := s.Knowledge.List(2, 1)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestIngestRejectsDispatchCollisionAcrossGroups(t *testing.T) {
	s, _ := fixture(t)
	event := models.AlertHisEvent{GroupId: 1, IncidentID: "private-incident", DispatchID: "private-dispatch", IncidentSequence: 1}
	_, err := s.Ingest(1, event)
	require.NoError(t, err)
	event.GroupId = 2
	_, err = s.Ingest(1, event)
	require.Error(t, err)
	event.GroupId = 1
	event.IncidentID = "different"
	_, err = s.Ingest(1, event)
	require.Error(t, err)
}
func TestDisabledExecutionDoesNotConsumeApproval(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	s.ExecutionAllowed = func(string) error {
		return opsauth.Fail(503, "execution_disabled", "Production task execution is disabled")
	}
	calls := 0
	s.Execute = func(context.Context, int64, *models.OpsCase, *models.OpsAction) (interface{}, error) {
		calls++
		return nil, nil
	}
	completeInvestigation(t, s, c)
	action, err := s.ProposeAction(context.Background(), 1, c.ID, "reclaim")
	require.NoError(t, err)
	_, err = s.Approve(context.Background(), 1, c.ID, action.ID, true)
	require.Error(t, err)
	var saved models.OpsAction
	require.NoError(t, s.DB.First(&saved, action.ID).Error)
	require.Equal(t, "proposed", saved.State)
	require.Zero(t, calls)
}
func TestConcurrentWorkersClaimOnlyOneLease(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	_, err := s.Queue(1, c.ID, "agent")
	require.NoError(t, err)
	var calls atomic.Int32
	s.Runner = func(_ context.Context, p Scope) (string, error) {
		calls.Add(1)
		_, e := s.Scoped(p).Get(p.UserID, p.CaseID, false)
		return "Scoped read completed", e
	}
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); _ = s.RunNext(context.Background()) }()
	}
	workers.Wait()
	require.Equal(t, int32(1), calls.Load())
	detail, err := s.Detail(1, c.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", detail.Runs[0].State)
}

func TestNewEvidenceInvalidatesPreparedAction(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	completeInvestigation(t, s, c)
	action, err := s.ProposeAction(context.Background(), 1, c.ID, "reclaim")
	require.NoError(t, err)
	_, err = s.AddManualEvidence(1, c.ID, "New observation after proposal")
	require.NoError(t, err)
	calls := 0
	s.Execute = func(context.Context, int64, *models.OpsCase, *models.OpsAction) (interface{}, error) {
		calls++
		return nil, nil
	}
	_, err = s.Approve(context.Background(), 1, c.ID, action.ID, true)
	require.Error(t, err)
	require.Zero(t, calls)
}
func TestManualStepsCannotBeReportedByAgent(t *testing.T) {
	s, now := fixture(t)
	spec := exampleSpec(*now)
	spec.Steps[0].Kind = "manual"
	item, err := s.Knowledge.Save(1, 0, 1, 0, "Manual procedure", spec)
	require.NoError(t, err)
	v, err := s.Knowledge.Publish(1, item.ID, item.Revision)
	require.NoError(t, err)
	c, err := s.Create(1, 1, "Manual", nil)
	require.NoError(t, err)
	require.NoError(t, s.UpdateContext(1, c.ID, c.Revision, map[string]string{"environment": "test", "system_version": "1"}, ""))
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.NoError(t, s.SelectKnowledge(1, c.ID, c.Revision, v.ID, ""))
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	evidence, err := s.AddManualEvidence(1, c.ID, "Operator observation")
	require.NoError(t, err)
	run := models.OpsRun{CaseID: c.ID, ActiveCaseID: &c.ID, UserID: 2, State: "running", Fence: 1, LeaseUntil: *now + 100, Mode: "agent"}
	require.NoError(t, s.DB.Create(&run).Error)
	scoped := s.Scoped(Scope{UserID: 2, CaseID: c.ID, RunID: run.ID, Fence: 1})
	require.Error(t, scoped.RecordAgentStep(2, c.ID, "inspect", "completed", "Manual check claimed", []int64{evidence.ID}))
	require.NoError(t, s.RecordStep(1, c.ID, "inspect", "completed", "Operator checked", []int64{evidence.ID}))
}

func TestNewNativeOccurrenceInvalidatesEarlierRecovery(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	require.NoError(t, s.DB.Model(c).Updates(map[string]interface{}{"target_ident": "recurring", "rule_id": 77, "datasource_id": 5}).Error)
	c, err := s.Get(1, c.ID, false)
	require.NoError(t, err)
	s.Query = func(_ context.Context, _ int64, _ *models.OpsCase, q string, _, end int64) (MetricResult, error) {
		return MetricResult{Status: "ok", Query: q, Points: []Point{{At: end, Value: 0}}}, nil
	}
	_, err = s.Verify(context.Background(), 1, c.ID)
	require.NoError(t, err)
	require.NoError(t, s.ConfirmCheck(1, c.ID, "business", "Business round trip verified", true))
	*now += 2
	newCase, err := s.Ingest(1, models.AlertHisEvent{GroupId: 1, TargetIdent: "recurring", RuleId: 77, DatasourceId: 5, IncidentID: "next-occurrence", DispatchID: "next-firing", IncidentSequence: 1})
	require.NoError(t, err)
	current, err := s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.Error(t, s.Close(1, c.ID, current.Revision, "resolved", "Previous result still claimed", ""))
	require.NoError(t, s.DB.Model(&models.OpsEpisode{}).Where("case_id = ?", newCase.ID).Update("recovered", true).Error)
	// Even a quickly recovered next occurrence requires new evidence.
	require.Error(t, s.Close(1, c.ID, current.Revision, "resolved", "Reuse old verification", ""))
	*now += 1
	_, err = s.Verify(context.Background(), 1, c.ID)
	require.NoError(t, err)
	require.NoError(t, s.ConfirmCheck(1, c.ID, "business", "Rechecked after the new occurrence", true))
	current, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.NoError(t, s.Close(1, c.ID, current.Revision, "resolved", "Rechecked recovery", ""))
}

func TestMissingIdentityAndDeletedCurrentAlertDoNotProveRecovery(t *testing.T) {
	s, now := fixture(t)
	event := models.AlertHisEvent{GroupId: 1, IncidentID: "recovery-only", DispatchID: "recovery-only", IncidentSequence: 3, IsRecovered: 1}
	c, err := s.Ingest(1, event)
	require.NoError(t, err)
	require.Equal(t, "pending", c.State)
	require.Equal(t, "recovery_without_context", c.WaitReason)
	require.NoError(t, s.SavePolicy(1, models.OpsPolicy{GroupID: 1, Enabled: true, ServiceUserID: 1, RuleIDs: "[]"}))
	event.IncidentID = "deleted-current"
	event.DispatchID = "deleted-current"
	event.IsRecovered = 0
	event.LastEvalTime = *now
	pending, err := s.Ingest(1, event)
	require.NoError(t, err)
	require.NoError(t, s.Scan(context.Background()))
	detail, err := s.Detail(1, pending.ID)
	require.NoError(t, err)
	require.False(t, detail.Episodes[0].Recovered)
	require.Error(t, s.Close(1, pending.ID, detail.Case.Revision, "resolved", "Current alert disappeared", ""))
}
func TestPermissionRevocationIsRecheckedAndMissingKnowledgeStaysUnknown(t *testing.T) {
	s, _ := fixture(t)
	c, err := s.Create(1, 1, "No knowledge", nil)
	require.NoError(t, err)
	matches, err := s.Matches(2, c.ID)
	require.NoError(t, err)
	require.Empty(t, matches)
	require.NoError(t, s.DB.Where("user_id = ?", 2).Delete(&models.UserGroupMember{}).Error)
	_, err = s.Get(2, c.ID, false)
	require.Error(t, err)
	require.NoError(t, s.RunNext(context.Background()))
}
func TestPartialEmptyAndStaleSamplesNeverPassRecovery(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	for _, status := range []string{"partial", "empty", "stale", "forbidden"} {
		s.Query = func(_ context.Context, _ int64, _ *models.OpsCase, q string, _, end int64) (MetricResult, error) {
			return MetricResult{Status: status, Query: q, Points: []Point{{At: end, Value: 0}}}, nil
		}
		checks, err := s.Verify(context.Background(), 1, c.ID)
		require.NoError(t, err)
		require.Equal(t, "pending", checks[0].State, status)
	}
}
func TestTemporaryRecoveryRequiresARecordedFollowup(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	s.Query = func(_ context.Context, _ int64, _ *models.OpsCase, q string, _, end int64) (MetricResult, error) {
		return MetricResult{Status: "ok", Query: q, Points: []Point{{At: end, Value: 0}}}, nil
	}
	_, err := s.Verify(context.Background(), 1, c.ID)
	require.NoError(t, err)
	require.NoError(t, s.ConfirmCheck(1, c.ID, "business", "Business round trip verified", true))
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.Error(t, s.Close(1, c.ID, c.Revision, "temporary", "Service restored, root cause pending", ""))
	require.NoError(t, s.Close(1, c.ID, c.Revision, "temporary", "Service restored, root cause pending", "Owner #4: investigate recurrence under follow-up TEST-1"))
}

func TestGuidedInvestigationResumesAfterCurrentManualStep(t *testing.T) {
	s, now := fixture(t)
	spec := exampleSpec(*now)
	spec.Steps = []knowledge.Step{{Key: "manual", Title: "Operator prerequisite", Kind: "manual"}, {Key: "metric", Title: "Collect metric", Kind: "metric", Query: "demo_used"}}
	item, err := s.Knowledge.Save(1, 0, 1, 0, "Resumable guide", spec)
	require.NoError(t, err)
	v, err := s.Knowledge.Publish(1, item.ID, item.Revision)
	require.NoError(t, err)
	c, err := s.Create(1, 1, "Resumable case", map[string]string{"environment": "test", "system_version": "1"})
	require.NoError(t, err)
	require.NoError(t, s.SelectKnowledge(1, c.ID, c.Revision, v.ID, ""))
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	originalGeneration := c.Generation
	require.NoError(t, s.SelectKnowledge(1, c.ID, c.Revision, v.ID, ""))
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.Equal(t, originalGeneration, c.Generation)
	calls := 0
	s.Query = func(_ context.Context, _ int64, _ *models.OpsCase, q string, _, end int64) (MetricResult, error) {
		calls++
		return MetricResult{Status: "ok", Points: []Point{{At: end, Value: 90}}, Query: q}, nil
	}
	_, err = s.Queue(1, c.ID, "guided")
	require.NoError(t, err)
	require.NoError(t, s.RunNext(context.Background()))
	require.Zero(t, calls)
	evidence, err := s.AddManualEvidence(1, c.ID, "Operator checked prerequisite")
	require.NoError(t, err)
	require.NoError(t, s.RecordStep(1, c.ID, "manual", "completed", "Checked", []int64{evidence.ID}))
	_, err = s.Queue(1, c.ID, "guided")
	require.NoError(t, err)
	require.NoError(t, s.RunNext(context.Background()))
	require.Equal(t, 1, calls)
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	require.NoError(t, s.UpdateContext(1, c.ID, c.Revision, map[string]string{"business": "New scope"}, ""))
	_, err = s.Queue(1, c.ID, "guided")
	require.NoError(t, err)
	require.NoError(t, s.RunNext(context.Background()))
	require.Equal(t, 1, calls, "Older-generation manual result must not bypass the prerequisite")
}
