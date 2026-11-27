package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func fixture(t *testing.T) (*Service, *int64) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/ops.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	tables := append(models.OpsTables(), models.KnowledgeTables()...)
	tables = append(tables, &models.User{}, &models.BusiGroup{}, &models.UserGroup{}, &models.UserGroupMember{}, &models.BusiGroupMember{}, &models.AlertHisEvent{}, &models.AlertCurEvent{}, &models.AlertRule{})
	require.NoError(t, db.AutoMigrate(tables...))
	for _, g := range []models.BusiGroup{{Id: 1, Name: "test"}, {Id: 2, Name: "other"}} {
		require.NoError(t, db.Create(&g).Error)
	}
	for _, u := range []models.User{{Id: 1, Username: "admin", Roles: "Admin"}, {Id: 2, Username: "reader", Roles: "Standard"}, {Id: 3, Username: "outsider", Roles: "Standard"}, {Id: 4, Username: "operator", Roles: "Standard"}} {
		require.NoError(t, db.Create(&u).Error)
	}
	require.NoError(t, db.Create(&models.BusiGroupMember{BusiGroupId: 1, UserGroupId: 10, PermFlag: "ro"}).Error)
	require.NoError(t, db.Create(&models.UserGroupMember{GroupId: 10, UserId: 2}).Error)
	require.NoError(t, db.Create(&models.BusiGroupMember{BusiGroupId: 1, UserGroupId: 11, PermFlag: "rw"}).Error)
	require.NoError(t, db.Create(&models.UserGroupMember{GroupId: 11, UserId: 4}).Error)
	now := time.Now().Unix()
	clock := func() time.Time { return time.Unix(now, 0) }
	k := &knowledge.Store{DB: db, DataDir: t.TempDir(), Now: clock}
	s := &Service{DB: db, Knowledge: k, Now: clock, DemoEnabled: true}
	return s, &now
}
func exampleSpec(now int64) knowledge.Spec {
	return knowledge.Spec{Summary: "Disk investigation", SourceNote: "Test fixture confirmed by operator", Conditions: map[string]string{"system_version": "1"}, Steps: []knowledge.Step{{Key: "inspect", Title: "Check samples", Kind: "metric", Query: "demo_used"}}, Actions: []knowledge.Action{{Key: "reclaim", Title: "Reclaim test cache", Kind: "demo_reclaim", Risk: "Test cache only", Rollback: "Regenerate test cache"}}, Checks: []knowledge.Check{{Key: "metric", Title: "Watermark normal", Kind: "metric", Query: "demo_used", Operator: "<", Threshold: 80, FreshSeconds: 30, Required: true}, {Key: "business", Title: "Business confirmed", Kind: "manual", Required: true}}, ValidatedAt: now - 60, ReviewAt: now + 3600, TestOnly: true}
}
func caseWithKnowledge(t *testing.T, s *Service, now int64) (*models.OpsCase, *models.KnowledgeVersion) {
	t.Helper()
	item, err := s.Knowledge.Save(1, 0, 1, 0, "Test recovery", exampleSpec(now))
	require.NoError(t, err)
	v, err := s.Knowledge.Publish(1, item.ID, item.Revision)
	require.NoError(t, err)
	c, err := s.Create(1, 1, "Test alert", map[string]string{"environment": "test", "system_version": "1"})
	require.NoError(t, err)
	require.NoError(t, s.SelectKnowledge(1, c.ID, c.Revision, v.ID, ""))
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	return c, v
}
func TestAccessAndKnowledgeLifecycle(t *testing.T) {
	s, now := fixture(t)
	c, v := caseWithKnowledge(t, s, *now)
	_, err := s.Get(2, c.ID, false)
	require.NoError(t, err)
	_, err = s.Get(4, c.ID, false)
	require.NoError(t, err)
	_, err = s.Get(3, c.ID, false)
	require.Error(t, err)
	_, err = s.Assign(2, c.ID, c.Revision, 2)
	require.Error(t, err)
	spec := exampleSpec(*now)
	spec.Summary = "Updated recovery"
	item, err := s.Knowledge.Get(1, v.ItemID)
	require.NoError(t, err)
	_, err = s.Knowledge.Save(1, item.ID, 1, item.Revision-1, item.Title, spec)
	require.Error(t, err)
	item, err = s.Knowledge.Save(1, item.ID, 1, item.Revision, item.Title, spec)
	require.NoError(t, err)
	next, err := s.Knowledge.Publish(1, item.ID, item.Revision)
	require.NoError(t, err)
	require.NotEqual(t, v.ID, next.ID)
	old, oldSpec, err := s.Knowledge.Version(1, v.ID)
	require.NoError(t, err)
	require.Equal(t, "Disk investigation", oldSpec.Summary)
	require.Equal(t, v.SHA256, old.SHA256)
	_, err = s.Knowledge.Get(3, item.ID)
	require.Error(t, err)
	read, err := s.Knowledge.Get(2, item.ID)
	require.NoError(t, err)
	require.Equal(t, models.OpsJSON("null"), read.Draft)
	require.NoError(t, s.Knowledge.Withdraw(1, v.ID, "Unsafe previous guidance"))
	_, err = s.ProposeAction(context.Background(), 1, c.ID, "reclaim")
	require.Error(t, err)
	require.NoError(t, s.DB.Model(&models.User{}).Where("id = ?", 2).Update("disabled", 1).Error)
	_, err = s.Get(2, c.ID, false)
	require.Error(t, err)
}
func TestIngestEpisodeIdentityAndRecoveryOrdering(t *testing.T) {
	s, now := fixture(t)
	e := models.AlertHisEvent{DispatchID: "dispatch1", IncidentID: "episode1", IncidentSequence: 1, GroupId: 1, RuleName: "Demo alert", TargetIdent: "demo", Tags: "environment=test,,system_version=1", TriggerTime: *now, LastEvalTime: *now}
	c, err := s.Ingest(1, e)
	require.NoError(t, err)
	again, err := s.Ingest(1, e)
	require.NoError(t, err)
	require.Equal(t, c.ID, again.ID)
	e.DispatchID = "recovered"
	e.IncidentSequence = 3
	e.IsRecovered = 1
	_, err = s.Ingest(1, e)
	require.NoError(t, err)
	e.DispatchID = "late-fire"
	e.IncidentSequence = 2
	e.IsRecovered = 0
	_, err = s.Ingest(1, e)
	require.NoError(t, err)
	d, err := s.Detail(1, c.ID)
	require.NoError(t, err)
	require.Len(t, d.Episodes, 1)
	require.True(t, d.Episodes[0].Recovered)
	require.Equal(t, "pending", d.Case.State)
	e.DispatchID = "new-fire"
	e.IncidentID = "episode2"
	e.IncidentSequence = 1
	next, err := s.Ingest(1, e)
	require.NoError(t, err)
	require.NotEqual(t, c.ID, next.ID)
	e.IncidentID = ""
	_, err = s.Ingest(1, e)
	require.Error(t, err)
}
func TestRecoveryRequiresFreshMetricsAndBusinessEvidence(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	s.Query = func(_ context.Context, _ int64, _ *models.OpsCase, q string, _, end int64) (MetricResult, error) {
		return MetricResult{Status: "ok", Query: q, Points: []Point{{At: end, Value: 20}}}, nil
	}
	_, err := s.Verify(context.Background(), 1, c.ID)
	require.NoError(t, err)
	c, err = s.Get(1, c.ID, false)
	require.NoError(t, err)
	err = s.Close(1, c.ID, c.Revision, "resolved", "Recovered", "")
	require.Error(t, err)
	require.NoError(t, s.ConfirmCheck(1, c.ID, "business", "Operator tested the round trip", true))
	require.NoError(t, s.Close(1, c.ID, c.Revision, "resolved", "Recovered", ""))
	_, err = s.AddManualEvidence(1, c.ID, "late mutation")
	require.Error(t, err)
	other, _ := caseWithKnowledge(t, s, *now)
	s.Query = func(_ context.Context, _ int64, _ *models.OpsCase, q string, _, end int64) (MetricResult, error) {
		return MetricResult{Status: "ok", Query: q, Points: []Point{{At: end - 300, Value: 0}}}, nil
	}
	checks, err := s.Verify(context.Background(), 1, other.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", checks[0].State)
	s.Query = func(context.Context, int64, *models.OpsCase, string, int64, int64) (MetricResult, error) {
		return MetricResult{}, fmt.Errorf("timeout")
	}
	checks, err = s.Verify(context.Background(), 1, other.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", checks[0].State)
}
func TestApprovalIsSingleUseAndCannotBeForgedByAgent(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	calls := 0
	s.Execute = func(context.Context, int64, *models.OpsCase, *models.OpsAction) (interface{}, error) {
		calls++
		return map[string]string{"result": "done"}, nil
	}
	completeInvestigation(t, s, c)
	a, err := s.ProposeAction(context.Background(), 1, c.ID, "reclaim")
	require.NoError(t, err)
	require.Zero(t, calls)
	_, err = s.Approve(context.Background(), 1, c.ID, a.ID, true)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	_, err = s.Approve(context.Background(), 1, c.ID, a.ID, true)
	require.Error(t, err)
	require.Equal(t, 1, calls)
	a, err = s.ProposeAction(context.Background(), 1, c.ID, "reclaim")
	require.NoError(t, err)
	*now += 1801
	_, err = s.Approve(context.Background(), 1, c.ID, a.ID, true)
	require.Error(t, err)
	require.Equal(t, 1, calls)
	_, err = s.Scoped(Scope{UserID: 1, CaseID: c.ID}).Approve(context.Background(), 1, c.ID, a.ID, true)
	require.Error(t, err)
}
func TestExecutionUnknownDoesNotRetry(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	calls := 0
	s.Execute = func(context.Context, int64, *models.OpsCase, *models.OpsAction) (interface{}, error) {
		calls++
		return nil, fmt.Errorf("response lost after external write")
	}
	completeInvestigation(t, s, c)
	a, err := s.ProposeAction(context.Background(), 1, c.ID, "reclaim")
	require.NoError(t, err)
	a, err = s.Approve(context.Background(), 1, c.ID, a.ID, true)
	require.NoError(t, err)
	require.Equal(t, "unknown", a.State)
	_, err = s.Approve(context.Background(), 1, c.ID, a.ID, true)
	require.Error(t, err)
	require.Equal(t, 1, calls)
	require.Error(t, s.actionUnresolved(c))
	require.NoError(t, s.ManualActionResult(1, c.ID, a.ID, "Confirmed external operation result", true))
}
func TestEvidenceIsolationAndRunFencing(t *testing.T) {
	s, now := fixture(t)
	c, _ := caseWithKnowledge(t, s, *now)
	other, _ := caseWithKnowledge(t, s, *now)
	e, err := s.AddManualEvidence(1, other.ID, "other case")
	require.NoError(t, err)
	require.Error(t, s.RecordStep(1, c.ID, "inspect", "completed", "", []int64{e.ID}))
	r, err := s.Queue(1, c.ID, "guided")
	require.NoError(t, err)
	_, err = s.Queue(1, c.ID, "guided")
	require.Error(t, err)
	require.NoError(t, s.DB.Model(r).Updates(map[string]interface{}{"state": "running", "fence": 1, "lease_until": *now + 150}).Error)
	scope := Scope{UserID: 1, CaseID: c.ID, RunID: r.ID, Fence: 1}
	_, err = s.Scoped(scope).Get(1, c.ID, false)
	require.NoError(t, err)
	_, err = s.Scoped(scope).Get(1, other.ID, false)
	require.Error(t, err)
	require.NoError(t, s.CancelRun(1, c.ID))
	_, err = s.Scoped(scope).Get(1, c.ID, false)
	require.Error(t, err)
}
func TestKnowledgeConditionsAndPrivateSources(t *testing.T) {
	s, now := fixture(t)
	spec := exampleSpec(*now)
	_, ok := knowledge.Conditions(spec, map[string]string{"system_version": "1", "environment": "production"})
	require.False(t, ok)
	_, ok = knowledge.Conditions(spec, map[string]string{"environment": "test"})
	require.False(t, ok)
	source, err := s.Knowledge.AddSource(1, 1, "runbook.md", "Test material", strings.NewReader("# Source\nValidated procedure"))
	require.NoError(t, err)
	_, _, err = s.Knowledge.Source(3, source.ID)
	require.Error(t, err)
	_, raw, err := s.Knowledge.Source(1, source.ID)
	require.NoError(t, err)
	require.Contains(t, string(raw), "Validated")
	spec.SourceIDs = []int64{source.ID}
	_, err = s.Knowledge.Save(1, 0, 2, 0, "Wrong group", spec)
	require.Error(t, err)
}
func TestScannerReconciliationAndPolicyScope(t *testing.T) {
	s, now := fixture(t)
	require.NoError(t, s.SavePolicy(1, models.OpsPolicy{GroupID: 1, ServiceUserID: 2, Enabled: true, RuleIDs: opsauth.JSON([]int64{})}))
	for i := 0; i < 3; i++ {
		e := models.AlertHisEvent{DispatchID: fmt.Sprintf("d%d", i), IncidentID: fmt.Sprintf("e%d", i), IncidentSequence: 1, GroupId: 1, RuleName: "Late event", LastEvalTime: *now - 86400}
		require.NoError(t, s.DB.Create(&e).Error)
	}
	require.NoError(t, s.Scan(context.Background()))
	rows, err := s.List(1, 1, "")
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.NoError(t, s.Scan(context.Background()))
	rows, err = s.List(1, 1, "")
	require.NoError(t, err)
	require.Len(t, rows, 3)
	raw, _ := json.Marshal([]int64{9999})
	require.Error(t, s.SavePolicy(1, models.OpsPolicy{GroupID: 1, ServiceUserID: 2, Enabled: true, RuleIDs: models.OpsJSON(raw)}))
}
