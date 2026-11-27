package environment

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	db, e := gorm.Open(sqlite.Open(t.TempDir()+"/environment.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, e)
	conn, _ := db.DB()
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	tables := append(models.EnvironmentTables(), models.LibraryTables()...)
	tables = append(tables, models.ArchiveTables()...)
	tables = append(tables, &models.User{}, &models.RoleOperation{}, &models.BusiGroup{}, &models.UserGroup{}, &models.UserGroupMember{}, &models.BusiGroupMember{}, &models.Target{}, &models.TargetBusiGroup{}, &models.MonitorDevice{}, &cmdb.Asset{}, &cmdb.Access{}, &cmdb.Change{}, &models.AlertHisEvent{})
	require.NoError(t, db.AutoMigrate(tables...))
	for _, u := range []models.User{{Id: 1, Username: "admin", Roles: "Admin"}, {Id: 2, Username: "reader", Roles: "Standard"}, {Id: 3, Username: "outsider", Roles: "Standard"}, {Id: 4, Username: "operator", Roles: "Standard"}} {
		require.NoError(t, db.Create(&u).Error)
	}
	for _, g := range []models.BusiGroup{{Id: 1, Name: "sim-hospital-a"}, {Id: 2, Name: "sim-hospital-b"}} {
		require.NoError(t, db.Create(&g).Error)
	}
	for _, v := range []models.BusiGroupMember{{BusiGroupId: 1, UserGroupId: 10, PermFlag: "ro"}, {BusiGroupId: 1, UserGroupId: 11, PermFlag: "rw"}} {
		require.NoError(t, db.Create(&v).Error)
	}
	require.NoError(t, db.Create(&models.UserGroupMember{GroupId: 10, UserId: 2}).Error)
	require.NoError(t, db.Create(&models.UserGroupMember{GroupId: 11, UserId: 4}).Error)
	for _, v := range []models.Target{{Id: 1, Ident: "sim-app-a", HostIp: "10.250.0.11", GroupId: 1}, {Id: 2, Ident: "sim-app-b", HostIp: "10.250.0.12", GroupId: 1}, {Id: 3, Ident: "sim-private", HostIp: "10.250.0.11", GroupId: 2}} {
		require.NoError(t, db.Create(&v).Error)
		require.NoError(t, db.Create(&models.TargetBusiGroup{TargetIdent: v.Ident, GroupId: v.GroupId}).Error)
	}
	var asset cmdb.Asset
	require.NoError(t, json.Unmarshal([]byte(`{"ID":"sim-asset-a","Serial":"SIM-SN-001","Payload":"{\"hostname\":\"sim-app-a\",\"businessIp\":\"10.250.0.11\",\"business\":\"sim-esb\",\"password\":\"secret-must-not-return\"}"}`), &asset))
	require.NoError(t, db.Create(&asset).Error)
	return &Store{DB: db}
}
func service(t *testing.T, s *Store, name string, group int64) Entity {
	t.Helper()
	o, e := s.SaveObject(1, models.EnvironmentObject{GroupID: group, Name: name, Kind: "application", Description: "simulation only"})
	require.NoError(t, e)
	a, e := s.Get(1, fmt.Sprintf("service:%d", o.ID))
	require.NoError(t, e)
	return a
}
func add(t *testing.T, s *Store, a, b Entity, kind string) models.EnvironmentRelation {
	t.Helper()
	r, e := s.Add(1, RelationInput{FromRef: a.Ref, ToRef: b.Ref, Kind: kind, FromVersion: a.Fingerprint, ToVersion: b.Fingerprint, Note: "simulated deployment checked", ValidUntil: now() + 3600})
	require.NoError(t, e)
	return r
}
func doc(t *testing.T, s *Store) Entity {
	t.Helper()
	a := models.ArchiveRecord{ID: 1}
	require.NoError(t, s.DB.Create(&a).Error)
	d := models.ArchiveDocument{ID: 1, ArchiveID: 1, CurrentVersionID: 1, Title: "Sim ESB deployment report"}
	require.NoError(t, s.DB.Create(&d).Error)
	v := models.ArchiveDocumentVersion{ID: 1, DocumentID: 1, Sequence: 1, HasContent: true}
	require.NoError(t, s.DB.Create(&v).Error)
	k := models.LibraryVersion{ID: 1, SourceVersionID: 1, DocumentID: 1, ArchiveID: 1, State: "ready", Title: d.Title, Sequence: 1, Markdown: "Report says sim-app-a is mentioned. sim-app-b-extra is a different object.", Digest: "v1"}
	require.NoError(t, s.DB.Create(&k).Error)
	x, e := s.Get(1, "document:1")
	require.NoError(t, e)
	return x
}
func TestIdentityPermissionsAndProjection(t *testing.T) {
	s := fixture(t)
	r, e := s.Search(1, "10.250.0.11")
	require.NoError(t, e)
	require.Len(t, r.Items, 3) // two targets and inventory, still separate identities
	r, e = s.Search(2, "10.250.0.11")
	require.NoError(t, e)
	require.Len(t, r.Items, 1)
	require.Equal(t, "target:1", r.Items[0].Ref)
	_, e = s.Get(3, "target:1")
	require.Error(t, e)
	_, e = s.Get(2, "asset:sim-asset-a")
	require.Error(t, e)
	a, e := s.Get(1, "asset:sim-asset-a")
	require.NoError(t, e)
	b, _ := json.Marshal(a)
	require.NotContains(t, string(b), "secret-must-not-return")
	require.NotContains(t, string(b), "password")
	for _, ref := range []string{"target:0", "target:1 OR 1=1", "unsupported:1", "asset:", "device:missing"} {
		_, e = s.Get(1, ref)
		require.Error(t, e)
	}
	_, e = s.SaveObject(2, models.EnvironmentObject{GroupID: 1, Kind: "service", Name: "no write"})
	require.Error(t, e)
	v, e := s.SaveObject(4, models.EnvironmentObject{GroupID: 1, Kind: "service", Name: "Sim-ESB"})
	require.NoError(t, e)
	_, e = s.SaveObject(4, models.EnvironmentObject{GroupID: 2, Kind: "service", Name: "cross-group"})
	require.Error(t, e)
	v.Name = "changed"
	v.Revision = 0
	_, e = s.SaveObject(4, v)
	require.Error(t, e)
	require.NoError(t, s.DB.Model(&models.User{}).Where("id = ?", 2).Update("disabled", 1).Error)
	_, e = s.Search(2, "")
	require.Error(t, e)
}
func TestRelationLifecycleAndReceipts(t *testing.T) {
	s := fixture(t)
	a := service(t, s, "Sim-ESB", 1)
	b, e := s.Get(1, "target:1")
	require.NoError(t, e)
	r := add(t, s, a, b, "runs_on")
	same := add(t, s, a, b, "runs_on")
	require.Equal(t, r.ID, same.ID)
	ctx, e := s.Context(2, a.Ref)
	require.NoError(t, e)
	require.Len(t, ctx.Relations, 1)
	require.Equal(t, "confirmed", ctx.Relations[0].Effective)
	require.NoError(t, s.Validate(2, ctx.Evidence))
	_, e = s.Change(2, r.ID, r.Revision, "withdraw", RelationInput{})
	require.Error(t, e)
	require.NoError(t, s.DB.Model(&models.Target{}).Where("id = ?", 1).Update("update_at", now()).Error)
	require.NoError(t, s.Validate(2, ctx.Evidence))
	require.NoError(t, s.DB.Model(&models.Target{}).Where("id = ?", 1).Update("host_ip", "10.250.0.20").Error)
	require.Error(t, s.Validate(2, ctx.Evidence))
	c, e := s.Context(1, a.Ref)
	require.NoError(t, e)
	require.Equal(t, "stale", c.Relations[0].Effective)
	trace, e := s.Trace(1, a.Ref, "out", 2)
	require.NoError(t, e)
	require.Empty(t, trace.Relations)
	r, e = s.Change(1, r.ID, r.Revision, "withdraw", RelationInput{})
	require.NoError(t, e)
	_, e = s.Change(1, r.ID, r.Revision-1, "withdraw", RelationInput{})
	require.Error(t, e)
	b, e = s.Get(1, b.Ref)
	require.NoError(t, e)
	r, e = s.Change(1, r.ID, r.Revision, "renew", RelationInput{FromVersion: a.Fingerprint, ToVersion: b.Fingerprint, ValidUntil: now() + 3600, Note: "changed address checked"})
	require.NoError(t, e)
	require.Equal(t, "confirmed", r.Status)
	c, e = s.Context(1, a.Ref)
	require.NoError(t, e)
	require.NoError(t, s.Validate(1, c.Evidence))
	require.NoError(t, s.DB.Model(&models.EnvironmentRelation{}).Where("id = ?", r.ID).Update("valid_until", now()-1).Error)
	require.Error(t, s.Validate(1, c.Evidence))
	c, e = s.Context(1, a.Ref)
	require.NoError(t, e)
	require.Equal(t, "expired", c.Relations[0].Effective)
	require.NoError(t, s.Validate(1, c.Evidence))
}
func TestGraphDirectionCyclesAndHiddenEdges(t *testing.T) {
	s := fixture(t)
	a := service(t, s, "Sim-ESB", 1)
	b := service(t, s, "Sim-VIP", 1)
	c := service(t, s, "Sim-F5", 1)
	private := service(t, s, "private-system", 2)
	add(t, s, a, b, "depends_on")
	add(t, s, b, c, "depends_on")
	add(t, s, c, a, "depends_on")
	add(t, s, a, private, "depends_on")
	g, e := s.Trace(2, a.Ref, "out", 3)
	require.NoError(t, e)
	require.Len(t, g.Nodes, 3)
	require.Len(t, g.Relations, 3)
	bdy, _ := json.Marshal(g)
	require.NotContains(t, string(bdy), "private-system")
	require.NoError(t, s.Validate(2, g.Evidence))
	g, e = s.Trace(2, a.Ref, "in", 1)
	require.NoError(t, e)
	require.Len(t, g.Nodes, 2)
	require.Equal(t, c.Ref, g.Nodes[1].Ref)
	_, e = s.Trace(1, a.Ref, "out", 4)
	require.Error(t, e)
	_, e = s.Trace(1, a.Ref, "wrong", 1)
	require.Error(t, e)
	require.NoError(t, s.DB.Where("user_id = ?", 2).Delete(&models.UserGroupMember{}).Error)
	require.Error(t, s.Validate(2, g.Evidence))
}
func TestDocumentCandidatesAndVersionInvalidation(t *testing.T) {
	s := fixture(t)
	d := doc(t, s)
	r, e := s.Discover(1, d.Ref)
	require.NoError(t, e)
	require.Len(t, r.Candidates, 1)
	require.Equal(t, "target:1", r.Candidates[0].ToRef)
	repeat, e := s.Discover(1, d.Ref)
	require.NoError(t, e)
	require.Equal(t, r.Candidates[0].ID, repeat.Candidates[0].ID)
	g, e := s.Trace(1, d.Ref, "out", 3)
	require.NoError(t, e)
	require.Empty(t, g.Relations)
	edge := r.Candidates[0]
	_, e = s.Change(2, edge.ID, edge.Revision, "confirm", RelationInput{})
	require.Error(t, e)
	edge, e = s.Change(1, edge.ID, edge.Revision, "confirm", RelationInput{})
	require.NoError(t, e)
	g, e = s.Trace(1, d.Ref, "out", 1)
	require.NoError(t, e)
	require.Len(t, g.Relations, 1)
	require.NoError(t, s.DB.Model(&models.ArchiveDocument{}).Where("id = ?", 1).Update("current_version_id", 2).Error)
	require.Error(t, s.Validate(1, g.Evidence))
	_, e = s.Change(1, edge.ID, edge.Revision, "renew", RelationInput{})
	require.Error(t, e)
	_, e = s.Discover(1, d.Ref)
	require.Error(t, e)
	require.False(t, exactToken("sim-app-a-extra", "sim-app-a"))
	require.True(t, exactToken("[sim-app-a]：", "sim-app-a"))
}
func TestTimelineScopeAndTime(t *testing.T) {
	s := fixture(t)
	for _, a := range []models.AlertHisEvent{{Id: 1, GroupId: 1, TargetIdent: "sim-app-a", RuleName: "Sim down", TriggerTime: now() - 60}, {Id: 2, GroupId: 2, TargetIdent: "sim-app-a", RuleName: "private event", TriggerTime: now() - 50}, {Id: 3, GroupId: 1, TargetIdent: "sim-app-a", RuleName: "old", TriggerTime: now() - 8*86400}} {
		require.NoError(t, s.DB.Create(&a).Error)
	}
	r, e := s.Timeline(2, "target:1")
	require.NoError(t, e)
	require.Len(t, r.Items, 1)
	require.Equal(t, "Sim down", r.Items[0].Name)
	require.NoError(t, s.Validate(2, r.Evidence))
	require.NoError(t, s.DB.Create(&cmdb.Change{AssetID: "sim-asset-a", Action: "update", Before: "private before", After: "private after", CreatedAt: time.Now()}).Error)
	r, e = s.Timeline(1, "asset:sim-asset-a")
	require.NoError(t, e)
	require.Len(t, r.Items, 1)
	b, _ := json.Marshal(r)
	require.NotContains(t, string(b), "private before")
	require.True(t, strings.Contains(r.Note, "台账审计"))
}
func TestAutomaticReconciliationUsesVersionAuthorAndIsIdempotent(t *testing.T) {
	s := fixture(t)
	d := doc(t, s)
	require.NoError(t, s.DB.Model(&models.ArchiveDocumentVersion{}).Where("id = ?", 1).Update("create_by", 1).Error)
	ok, e := s.ReconcileOne()
	require.NoError(t, e)
	require.True(t, ok)
	ctx, e := s.Context(1, d.Ref)
	require.NoError(t, e)
	require.Len(t, ctx.Relations, 1)
	require.Equal(t, "candidate", ctx.Relations[0].Status)
	ok, e = s.ReconcileOne()
	require.NoError(t, e)
	require.False(t, ok)
	require.NoError(t, s.DB.Model(&models.EnvironmentDiscovery{}).Where("version_id = ?", 1).Update("next_run", 0).Error)
	ok, e = s.ReconcileOne()
	require.NoError(t, e)
	require.True(t, ok)
	ctx, e = s.Context(1, d.Ref)
	require.NoError(t, e)
	require.Len(t, ctx.Relations, 1)
	require.NoError(t, s.DB.Model(&models.User{}).Where("id = ?", 1).Update("disabled", 1).Error)
	require.NoError(t, s.DB.Model(&models.EnvironmentDiscovery{}).Where("version_id = ?", 1).Update("next_run", 0).Error)
	ok, e = s.ReconcileOne()
	require.NoError(t, e)
	require.True(t, ok)
	var job models.EnvironmentDiscovery
	require.NoError(t, s.DB.First(&job).Error)
	require.Equal(t, "retry", job.State)
}
func TestNativeBindingsAndDeletionRecheck(t *testing.T) {
	s := fixture(t)
	require.NoError(t, s.DB.Create(&models.MonitorDevice{ID: "sim-device", Name: "Sim-App-A", Ident: "sim-app-a", GroupID: 1, CMDBID: "sim-asset-a", Parameters: "secret-not-visible", Enabled: true}).Error)
	c, e := s.Context(1, "device:sim-device")
	require.NoError(t, e)
	require.Len(t, c.NativeRelations, 2)
	require.NoError(t, s.Validate(1, c.Evidence))
	c, e = s.Context(1, "target:1")
	require.NoError(t, e)
	require.Len(t, c.NativeRelations, 1)
	c, e = s.Context(1, "asset:sim-asset-a")
	require.NoError(t, e)
	require.Len(t, c.NativeRelations, 1)
	b, _ := json.Marshal(c)
	require.NotContains(t, string(b), "secret-not-visible")
	require.NoError(t, s.DB.Model(&models.MonitorDevice{}).Where("id = ?", "sim-device").Update("cmdb_id", "").Error)
	require.Error(t, s.Validate(1, c.Evidence))
	c, e = s.Context(1, "asset:sim-asset-a")
	require.NoError(t, e)
	require.Empty(t, c.NativeRelations)
	a := service(t, s, "Sim-ESB", 1)
	asset, e := s.Get(1, "asset:sim-asset-a")
	require.NoError(t, e)
	add(t, s, a, asset, "runs_on")
	c, e = s.Context(1, a.Ref)
	require.NoError(t, e)
	require.NoError(t, s.DB.Model(&cmdb.Asset{}).Where("id = ?", "sim-asset-a").Update("deleted", true).Error)
	require.Error(t, s.Validate(1, c.Evidence))
	c, e = s.Context(1, a.Ref)
	require.NoError(t, e)
	require.Empty(t, c.Relations)
}
func TestGraphAndContextLimits(t *testing.T) {
	s := fixture(t)
	a := service(t, s, "Sim-ESB", 1)
	for i := 0; i < 102; i++ {
		b := service(t, s, fmt.Sprintf("Sim-backend-%d", i), 1)
		add(t, s, a, b, "depends_on")
	}
	c, e := s.Context(1, a.Ref)
	require.NoError(t, e)
	require.True(t, c.Truncated)
	require.Len(t, c.Relations, 100)
	g, e := s.Trace(1, a.Ref, "out", 1)
	require.NoError(t, e)
	require.True(t, g.Truncated)
	require.LessOrEqual(t, len(g.Nodes), 100)
	require.LessOrEqual(t, len(g.Relations), 100)
}
func TestUnprivilegedDeviceContextDoesNotExposeAssetBinding(t *testing.T) {
	s := fixture(t)
	require.NoError(t, s.DB.Create(&models.MonitorDevice{ID: "sim-bound", Name: "sim bound", Ident: "sim-app-a", CMDBID: "sim-asset-a", GroupID: 1}).Error)
	c, e := s.Context(1, "device:sim-bound")
	require.NoError(t, e)
	require.Len(t, c.NativeRelations, 2)
	require.NoError(t, s.DB.Create(&models.RoleOperation{RoleName: "Standard", Operation: "/collection/read"}).Error)
	hidden, e := s.Context(2, "device:sim-bound")
	require.NoError(t, e)
	require.Len(t, hidden.NativeRelations, 1)
	raw, _ := json.Marshal(hidden)
	require.NotContains(t, string(raw), "sim-asset-a")
	// Projection itself has no raw asset reference; only authorized native edges expose it.
	v, e := s.Get(1, "device:sim-bound")
	require.NoError(t, e)
	require.NotContains(t, v.Attributes, "asset_ref")
}
func TestCollectorEndpointAndEnabledChangesInvalidateReceipts(t *testing.T) {
	s := fixture(t)
	require.NoError(t, s.DB.Create(&models.MonitorDevice{ID: "sim-db", Name: "Sim-DB", Ident: "sim-db", Address: "10.250.0.9", Port: 3306, Protocol: "mysql", GroupID: 1, Enabled: true}).Error)
	c, e := s.Context(1, "device:sim-db")
	require.NoError(t, e)
	require.NoError(t, s.Validate(1, c.Evidence))
	require.NoError(t, s.DB.Model(&models.MonitorDevice{}).Where("id = ?", "sim-db").Update("port", 3307).Error)
	require.Error(t, s.Validate(1, c.Evidence))
	c, e = s.Context(1, "device:sim-db")
	require.NoError(t, e)
	require.NoError(t, s.DB.Model(&models.MonitorDevice{}).Where("id = ?", "sim-db").Update("enabled", false).Error)
	require.Error(t, s.Validate(1, c.Evidence))
}
func TestPendingDocumentIsProcessingNotPermissionDenied(t *testing.T) {
	s := fixture(t)
	d := doc(t, s)
	require.NoError(t, s.DB.Model(&models.LibraryVersion{}).Where("id = ?", 1).Update("state", "queued").Error)
	_, e := s.Get(1, d.Ref)
	require.Error(t, e)
	require.Contains(t, e.Error(), "尚未解析完成")
}
func TestRejectedCandidateIsNotReopenedByReconciliation(t *testing.T) {
	s := fixture(t)
	d := doc(t, s)
	require.NoError(t, s.DB.Model(&models.ArchiveDocumentVersion{}).Where("id = ?", 1).Update("create_by", 1).Error)
	found, e := s.Discover(1, d.Ref)
	require.NoError(t, e)
	require.Len(t, found.Candidates, 1)
	candidate := found.Candidates[0]
	candidate, e = s.Change(1, candidate.ID, candidate.Revision, "reject", RelationInput{})
	require.NoError(t, e)
	require.Equal(t, "rejected", candidate.Status)
	ok, e := s.ReconcileOne()
	require.NoError(t, e)
	require.True(t, ok)
	ctx, e := s.Context(1, d.Ref)
	require.NoError(t, e)
	require.Len(t, ctx.Relations, 1)
	require.Equal(t, "rejected", ctx.Relations[0].Status)
	_, e = s.Change(1, candidate.ID, candidate.Revision, "confirm", RelationInput{})
	require.Error(t, e)
	trace, e := s.Trace(1, d.Ref, "out", 1)
	require.NoError(t, e)
	require.Empty(t, trace.Relations)
}
