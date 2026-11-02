package collection

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/google/uuid"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testStore(t *testing.T) (*Store, Scope, models.MonitorCollector, models.MonitorCredential, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return fixtureStore(t, db)
}
func fixtureStore(t *testing.T, db *gorm.DB) (*Store, Scope, models.MonitorCollector, models.MonitorCredential, string) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&models.Target{}, &models.TargetBusiGroup{}, &models.BusiGroup{}, &models.User{}, &models.Datasource{}))
	if db.Dialector.Name() == "mysql" {
		require.NoError(t, db.Exec("ALTER TABLE target MODIFY ident VARCHAR(191)").Error)
	}
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX collection_test_target_ident ON target (ident)").Error)
	s := &Store{DB: db, Keys: &Keyring{Active: "fixture", Keys: map[string]string{"fixture": base64.StdEncoding.EncodeToString(make([]byte, 32))}}, Writers: map[string]bool{"fixture-writer": true, "other-writer": true}}
	require.NoError(t, s.Migrate())
	require.NoError(t, s.Migrate())
	for _, id := range []int64{1, 2} {
		require.NoError(t, db.Create(&models.BusiGroup{Id: id, Name: fmt.Sprint("group", id)}).Error)
	}
	require.NoError(t, db.Create(&models.Datasource{Id: 1, Name: "fixture", PluginType: "prometheus"}).Error)
	scope := Scope{ActorID: 1, Admin: true}
	collector, secret, err := s.CreateCollector(scope, CollectorInput{Name: "fixture", AgentIdent: "fixture-agent", DatasourceID: 1, GroupIDs: []int64{1}, Networks: []string{"192.0.2.0/24"}, WriterUser: "fixture-writer"})
	require.NoError(t, err)
	credential, err := s.SaveCredential(scope, "", CredentialInput{Name: "fixture", GroupID: 1, Community: "synthetic-secret\"\n[not_a_table]"})
	require.NoError(t, err)
	return s, scope, collector, credential, secret
}
func deviceInput(c models.MonitorCollector, cred models.MonitorCredential, address string) DeviceInput {
	return DeviceInput{Name: "Windows fixture", Address: address, GroupID: 1, CollectorID: c.ID, CredentialID: cred.ID, Enabled: true, Labels: map[string]string{"env": "fixture"}}
}
func TestPublicationLifecycleAndFailureProtection(t *testing.T) {
	s, scope, c, cred, secret := testStore(t)
	input := deviceInput(c, cred, "192.0.2.10")
	d, err := s.SaveDevice(scope, "", "request-one", input)
	require.NoError(t, err)
	var target models.Target
	require.NoError(t, s.DB.First(&target, "ident = ?", d.Ident).Error)
	require.Empty(t, target.AgentVersion)
	require.Zero(t, target.UpdateAt)
	snapshot, same, err := s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	require.False(t, same)
	require.Len(t, snapshot.Configs["snmp"], 1)
	require.NotEmpty(t, snapshot.Version)
	for hash, content := range snapshot.Configs["snmp"] {
		require.Equal(t, digest([]byte(content.Config)), hash)
		require.Contains(t, content.Config, "collection_revision")
		require.Contains(t, content.Config, "snmp_target")
		require.Contains(t, content.Config, "[[instances.table]]")
	}
	again, err := s.SaveDevice(scope, "", "request-one", input)
	require.NoError(t, err)
	require.Equal(t, d.ID, again.ID)
	input.Name = "Different"
	_, err = s.SaveDevice(scope, "", "request-one", input)
	require.Error(t, err)
	require.Equal(t, 409, err.(*Error).Code)
	edited := inputFromDevice(d)
	edited.Name = "Renamed"
	edited.RecordVersion = d.RecordVersion
	renamed, err := s.SaveDevice(scope, d.ID, "", edited)
	require.NoError(t, err)
	require.Equal(t, d.DesiredRevision, renamed.DesiredRevision)
	require.Equal(t, d.PublicationVersion, renamed.PublicationVersion)
	edited.RecordVersion = d.RecordVersion
	_, err = s.SaveDevice(scope, d.ID, "", edited)
	require.Equal(t, 409, err.(*Error).Code)
	_, _, err = s.Provider(c.ID, "wrong", snapshot.Version)
	require.Equal(t, 401, err.(*Error).Code)
	latest, same, err := s.Provider(c.ID, secret, snapshot.Version)
	require.NoError(t, err)
	require.True(t, same)
	require.Equal(t, snapshot.Version, latest.Version)
	disabled, err := s.SetDeviceState(scope, d.ID, renamed.RecordVersion, false, false)
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
	empty, _, err := s.Provider(c.ID, secret, snapshot.Version)
	require.NoError(t, err)
	require.NotEqual(t, snapshot.Version, empty.Version)
	require.Empty(t, empty.Configs)
	var current models.MonitorCollector
	require.NoError(t, s.DB.First(&current, "id = ?", c.ID).Error)
	require.False(t, withdrawn(disabled, current, time.Now().Unix()))
	_, _, err = s.Provider(c.ID, secret, empty.Version)
	require.NoError(t, err)
	require.NoError(t, s.DB.First(&current, "id = ?", c.ID).Error)
	require.True(t, withdrawn(disabled, current, time.Now().Add(91*time.Second).Unix()))
	deleted, err := s.SetDeviceState(scope, d.ID, disabled.RecordVersion, false, true)
	require.NoError(t, err)
	require.True(t, deleted.Deleted)
	require.Equal(t, disabled.WithdrawVersion, deleted.WithdrawVersion)
	list, total, err := s.Devices(scope, 0, "", false, 50, 0)
	require.NoError(t, err)
	require.Empty(t, list)
	require.Zero(t, total)
	require.NoError(t, s.DB.First(&target, "ident = ?", d.Ident).Error)
	require.Error(t, s.DeleteCredential(scope, cred.ID, cred.RecordVersion))
	require.Error(t, s.DeleteCollector(scope, c.ID, c.RecordVersion))
	// Broken encrypted snapshots produce failure, never an authoritative empty config.
	require.NoError(t, s.DB.Model(&models.MonitorPublication{}).Where("version = ?", empty.Version).Update("ciphertext", "invalid").Error)
	_, _, err = s.Provider(c.ID, secret, "")
	require.Error(t, err)
}
func TestConcurrentCreatesDoNotLoseTasks(t *testing.T) {
	s, scope, c, cred, secret := testStore(t)
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			<-start
			_, err := s.SaveDevice(scope, "", fmt.Sprint("concurrent-", i), deviceInput(c, cred, fmt.Sprint("192.0.2.", 10+i)))
			errs <- err
		}(i)
	}
	close(start)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	snapshot, _, err := s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	require.Len(t, snapshot.Configs["snmp"], 2)
}
func TestAuthorizationIdempotencyAndSecretStorage(t *testing.T) {
	s, admin, c, cred, secret := testStore(t)
	writer := Scope{ActorID: 2, ReadGroups: []int64{1}, WriteGroups: []int64{1}}
	reader := Scope{ActorID: 3, ReadGroups: []int64{1}}
	other := Scope{ActorID: 4, ReadGroups: []int64{2}, WriteGroups: []int64{2}}
	d, err := s.SaveDevice(writer, "", "writer-request", deviceInput(c, cred, "192.0.2.11"))
	require.NoError(t, err)
	_, err = s.Device(reader, d.ID)
	require.NoError(t, err)
	_, err = s.Device(other, d.ID)
	require.Equal(t, 403, err.(*Error).Code)
	_, err = s.SetDeviceState(reader, d.ID, d.RecordVersion, false, false)
	require.Equal(t, 403, err.(*Error).Code)
	f := deviceInput(c, cred, "192.0.2.12")
	f.GroupID = 2
	_, err = s.SaveDevice(other, "", "other-request", f)
	require.Equal(t, 403, err.(*Error).Code)
	f = deviceInput(c, cred, "192.0.2.12")
	f.Labels["collection_mode"] = "probe"
	_, err = s.SaveDevice(admin, "", "invalid-label", f)
	require.Error(t, err)
	body, err := json.Marshal([]interface{}{cred, c, d})
	require.NoError(t, err)
	require.NotContains(t, string(body), "synthetic-secret")
	require.NotContains(t, string(body), secret)
	var pubs []models.MonitorPublication
	require.NoError(t, s.DB.Find(&pubs).Error)
	for _, p := range pubs {
		require.NotContains(t, string(p.Ciphertext), "synthetic-secret")
	}
	rotated, err := s.RotateCollector(admin, c.ID, c.RecordVersion)
	require.NoError(t, err)
	_, _, err = s.Provider(c.ID, rotated, "")
	require.NoError(t, err)
	_, _, err = s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	require.NoError(t, s.DB.Model(&models.MonitorCollector{}).Where("id = ?", c.ID).Update("previous_token_until", time.Now().Add(-time.Second).Unix()).Error)
	_, _, err = s.Provider(c.ID, secret, "")
	require.Equal(t, 401, err.(*Error).Code)
	var n int64
	require.NoError(t, s.DB.Model(&models.MonitorDevice{}).Count(&n).Error)
	require.EqualValues(t, 1, n)
}
func TestEncryptionAndPublishRollback(t *testing.T) {
	s, scope, c, cred, secret := testStore(t)
	text, key, err := s.Keys.Seal([]byte("fixture-secret"), "object-one")
	require.NoError(t, err)
	plain, err := s.Keys.Open(text, key, "object-one")
	require.NoError(t, err)
	require.Equal(t, "fixture-secret", string(plain))
	_, err = s.Keys.Open(text, key, "object-two")
	require.Error(t, err)
	snapshot, _, err := s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	s.Keys = nil
	_, err = s.SaveDevice(scope, "", "missing-key", deviceInput(c, cred, "192.0.2.20"))
	require.Error(t, err)
	var n int64
	require.NoError(t, s.DB.Model(&models.Target{}).Count(&n).Error)
	require.Zero(t, n)
	var collector models.MonitorCollector
	require.NoError(t, s.DB.First(&collector, "id = ?", c.ID).Error)
	require.Equal(t, snapshot.Version, collector.DesiredVersion)
}
func TestProbeDoesNotRegisterTargetAndCleansUp(t *testing.T) {
	s, scope, c, cred, secret := testStore(t)
	p, err := s.CreateProbe(scope, deviceInput(c, cred, "192.0.2.20"))
	require.NoError(t, err)
	var n int64
	require.NoError(t, s.DB.Model(&models.Target{}).Count(&n).Error)
	require.Zero(t, n)
	snapshot, _, err := s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	require.Len(t, snapshot.Configs["snmp"], 1)
	require.NoError(t, s.CancelProbe(scope, p.ID))
	p, err = s.Probe(scope, p.ID)
	require.NoError(t, err)
	require.Equal(t, "cleanup_pending", p.State)
	require.NotEmpty(t, p.CleanupVersion)
	snapshot, _, err = s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	require.Empty(t, snapshot.Configs)
	require.NoError(t, s.CleanupProbes())
	p, err = s.Probe(scope, p.ID)
	require.NoError(t, err)
	require.Equal(t, "cleanup_pending", p.State)
}
func metric(d models.MonitorDevice, name string, value float64, labels map[string]string) prompb.TimeSeries {
	ls := []prompb.Label{{Name: "__name__", Value: name}, {Name: "ident", Value: d.Ident}, {Name: "collection_device_id", Value: d.ID}, {Name: "collection_collector_id", Value: d.CollectorID}, {Name: "collection_revision", Value: d.DesiredRevision}, {Name: "collection_mode", Value: "managed"}}
	for k, v := range labels {
		ls = append(ls, prompb.Label{Name: k, Value: v})
	}
	return prompb.TimeSeries{Labels: ls, Samples: []prompb.Sample{{Timestamp: time.Now().UnixMilli(), Value: value}}}
}
func TestWriterOwnershipWithdrawalAndOrdinaryLinux(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	d, err := s.SaveDevice(scope, "", "writer-owner", deviceInput(c, cred, "192.0.2.20"))
	require.NoError(t, err)
	sample := metric(d, "snmp_up", 1, nil)
	out, err := s.FilterSamples("other-writer", []prompb.TimeSeries{sample})
	require.NoError(t, err)
	require.Empty(t, out)
	out, err = s.FilterSamples("fixture-writer", []prompb.TimeSeries{sample})
	require.NoError(t, err)
	require.Len(t, out, 1)
	linux := prompb.TimeSeries{Labels: []prompb.Label{{Name: "__name__", Value: "cpu_usage"}, {Name: "ident", Value: "linux"}}}
	out, err = s.FilterSamples("fixture-writer", []prompb.TimeSeries{linux})
	require.NoError(t, err)
	require.Len(t, out, 1)
	d, err = s.SetDeviceState(scope, d.ID, d.RecordVersion, false, false)
	require.NoError(t, err)
	out, err = s.FilterSamples("fixture-writer", []prompb.TimeSeries{sample})
	require.NoError(t, err)
	require.Empty(t, out)
	require.NoError(t, s.DB.First(&d, "id = ?", d.ID).Error)
	require.NotZero(t, d.LastRejectedAt)
}
func TestStatusRequiresFreshCurrentRevisionAndHandlesQueryFailures(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	d, err := s.SaveDevice(scope, "", "status-device", deviceInput(c, cred, "192.0.2.10"))
	require.NoError(t, err)
	now := time.Now().Unix()
	matrix := model.Matrix{}
	appendMetric := func(name string, value float64, labels map[string]string) {
		ts := metric(d, name, value, labels)
		m := model.Metric{}
		for _, l := range ts.Labels {
			m[model.LabelName(l.Name)] = model.LabelValue(l.Value)
		}
		matrix = append(matrix, &model.SampleStream{Metric: m, Values: []model.SamplePair{{Timestamp: model.Time(now * 1000), Value: model.SampleValue(value)}}})
	}
	appendMetric("snmp_up", 1, nil)
	appendMetric("snmp_uptime", 123, nil)
	appendMetric("snmp_cpu_load", 25, map[string]string{"index": "8"})
	appendMetric("snmp_storage_size", 200, map[string]string{"index": "17", "type": ".1.3.6.1.2.1.25.2.1.2"})
	appendMetric("snmp_storage_used", 50, map[string]string{"index": "17", "type": ".1.3.6.1.2.1.25.2.1.2"})
	state := evaluate(d, c, matrix, now, 90, 30)
	require.Equal(t, "healthy", state.Health)
	require.Equal(t, "observed", state.ConfigurationState)
	require.Equal(t, 25.0, *state.CPUPercent)
	require.Equal(t, 25.0, *state.MemoryPercent)
	require.Equal(t, "interrupted", evaluate(d, c, matrix, now+91, 90, 30).Health)
	old := d
	old.DesiredRevision = "new-revision"
	require.Equal(t, "waiting", evaluate(old, c, matrix, now, 90, 30).Health)
	require.Equal(t, "clock_error", evaluate(d, c, matrix, now-40, 90, 30).Health)
	require.Equal(t, "partial", evaluate(d, c, matrix[:2], now, 90, 30).Health)
	failures := 0
	status := &StatusService{Store: s, Query: func(context.Context, int64, string, time.Time) (model.Value, error) {
		failures++
		return nil, errors.New("fixture datasource offline")
	}}
	states, err := status.States(context.Background(), []models.MonitorDevice{d})
	require.NoError(t, err)
	require.Equal(t, "query_error", states[d.ID].Health)
	_, err = status.States(context.Background(), []models.MonitorDevice{d})
	require.NoError(t, err)
	require.Equal(t, 1, failures)
}
func TestRollbackCreatesNewExecutableRevision(t *testing.T) {
	s, scope, c, cred, secret := testStore(t)
	d, err := s.SaveDevice(scope, "", "rollback-device", deviceInput(c, cred, "192.0.2.30"))
	require.NoError(t, err)
	original, _, err := s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	var publication models.MonitorPublication
	require.NoError(t, s.DB.First(&publication, "version = ?", original.Version).Error)
	f := inputFromDevice(d)
	f.IntervalSeconds = 60
	d, err = s.SaveDevice(scope, d.ID, "", f)
	require.NoError(t, err)
	rev := d.DesiredRevision
	require.NoError(t, s.Rollback(scope, publication.ID))
	require.NoError(t, s.DB.First(&d, "id = ?", d.ID).Error)
	require.Equal(t, 30, d.IntervalSeconds)
	require.NotEqual(t, rev, d.DesiredRevision)
	restored, _, err := s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	require.NotEqual(t, original.Version, restored.Version)
	require.Len(t, restored.Configs["snmp"], 1)
}
func TestDeviceInputRejectsInvalidIPAndReservedLabels(t *testing.T) {
	_, _, c, cred, _ := testStore(t)
	for _, address := range []string{"", "192.0.2.256", "2001:::1", "not-an-ip"} {
		f := deviceInput(c, cred, address)
		require.Error(t, f.normalize(), address)
	}
	f := deviceInput(c, cred, "192.0.2.10")
	f.Labels["ident"] = "override"
	require.Error(t, f.normalize())
}

func TestTemplateDoesNotExposeUnsafeDescriptionsOrFixedIndexes(t *testing.T) {
	tpl := templateDefinition()
	body := string(tpl.Definition)
	require.NotContains(t, body, "hrStorageDescr")
	require.NotContains(t, body, "192.168.0.151")
	require.True(t, strings.Contains(body, "25.2.3.1.2"))
}

func TestNativeTargetMutationsCannotBypassLifecycle(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	d, err := s.SaveDevice(scope, "", "native-guard", deviceInput(c, cred, "192.0.2.10"))
	require.NoError(t, err)
	ctx := nctx.NewContext(context.Background(), s.DB, true)
	calls := 0
	hook := func(tx *gorm.DB, ids []string, force bool) error { calls++; return nil }
	require.Error(t, models.TargetDel(ctx, []string{d.Ident}, true, hook))
	require.Zero(t, calls)
	require.Error(t, models.TargetUpdateNote(ctx, []string{d.Ident}, "bypass"))
	require.Error(t, models.TargetOverrideBgids(ctx, []string{d.Ident}, []int64{2}, nil))
	require.Error(t, models.TargetBindBgids(ctx, []string{d.Ident}, []int64{2}, nil))
	target, err := models.TargetGetByIdent(ctx, d.Ident)
	require.NoError(t, err)
	require.Error(t, target.AddTags(ctx, []string{"collection_mode=probe"}))
	_, err = s.SetDeviceState(scope, d.ID, d.RecordVersion, false, true)
	require.NoError(t, err)
	list, err := models.TargetGets(ctx, 50, 0, "ident", false)
	require.NoError(t, err)
	require.Empty(t, list)
	all, err := models.TargetGetsAll(ctx)
	require.NoError(t, err)
	require.Empty(t, all)
	require.NoError(t, s.DB.Create(&models.Target{Ident: "ordinary-agent"}).Error)
	require.NoError(t, models.TargetDel(ctx, []string{"ordinary-agent"}, true, hook))
	require.Equal(t, 1, calls)
}

func TestCMDBAssociationChecksReadAccessAndSurvivesAssetDeletion(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	require.NoError(t, s.DB.AutoMigrate(&cmdb.Asset{}, &cmdb.Access{}))
	asset := cmdb.Asset{ID: "fixture-asset", Serial: "fixture-serial"}
	require.NoError(t, s.DB.Create(&asset).Error)
	input := deviceInput(c, cred, "192.0.2.60")
	input.CMDBID = asset.ID
	writer := Scope{ActorID: 2, ReadGroups: []int64{1}, WriteGroups: []int64{1}}
	_, err := s.SaveDevice(writer, "", "asset-denied", input)
	require.Equal(t, 403, err.(*Error).Code)
	d, err := s.SaveDevice(scope, "", "asset-allowed", input)
	require.NoError(t, err)
	require.NoError(t, s.DB.Model(&asset).Update("deleted", true).Error)
	changed := inputFromDevice(d)
	changed.Name = "Renamed with historical asset association"
	d, err = s.SaveDevice(writer, d.ID, "", changed)
	require.NoError(t, err)
	require.Equal(t, asset.ID, d.CMDBID)
	d, err = s.SetDeviceState(writer, d.ID, d.RecordVersion, false, false)
	require.NoError(t, err)
	_, err = s.SetDeviceState(writer, d.ID, d.RecordVersion, true, false)
	require.NoError(t, err)
}
func TestCollectorGroupsRemainAuthorizedWithoutAddressRestrictions(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	_, err := s.SaveDevice(scope, "", "collector-policy", deviceInput(c, cred, "192.0.2.60"))
	require.NoError(t, err)
	input := CollectorInput{Name: "renamed collector", AgentIdent: c.AgentIdent, DatasourceID: c.DatasourceID, GroupIDs: []int64{1}, Networks: []string{"192.0.2.0/24"}, Ports: []int{161}, WriterUser: c.WriterUser, RecordVersion: c.RecordVersion}
	c, err = s.UpdateCollector(scope, c.ID, input)
	require.NoError(t, err)
	input.RecordVersion = c.RecordVersion
	input.Networks = []string{"198.51.100.0/24"}
	c, err = s.UpdateCollector(scope, c.ID, input)
	require.NoError(t, err)
	input.RecordVersion = c.RecordVersion
	input.GroupIDs = []int64{2}
	_, err = s.UpdateCollector(scope, c.ID, input)
	require.Equal(t, 409, err.(*Error).Code)
}
func TestRevisionChangesResetClockErrorsAndInitialWait(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	d, err := s.SaveDevice(scope, "", "clock-reset", deviceInput(c, cred, "192.0.2.60"))
	require.NoError(t, err)
	future := metric(d, "snmp_up", 1, nil)
	future.Samples[0].Timestamp = time.Now().Add(10 * time.Minute).UnixMilli()
	_, err = s.FilterSamples(c.WriterUser, []prompb.TimeSeries{future})
	require.NoError(t, err)
	require.NoError(t, s.DB.First(&d, "id = ?", d.ID).Error)
	require.NotZero(t, d.LastClockErrorAt)
	input := inputFromDevice(d)
	input.IntervalSeconds = 60
	d, err = s.SaveDevice(scope, d.ID, "", input)
	require.NoError(t, err)
	require.Zero(t, d.LastClockErrorAt)
	d.CreatedAt = time.Now().Add(-time.Hour).Unix()
	d.UpdatedAt = time.Now().Unix()
	require.Equal(t, "waiting", evaluate(d, c, nil, time.Now().Unix(), 90, 30).Health)
}

func TestDisabledParameterEditsRetainTheRetiredTaskObservationWindow(t *testing.T) {
	s, scope, c, cred, secret := testStore(t)
	input := deviceInput(c, cred, "192.0.2.70")
	input.IntervalSeconds = 120
	d, err := s.SaveDevice(scope, "", "disabled-edit", input)
	require.NoError(t, err)
	d, err = s.SetDeviceState(scope, d.ID, d.RecordVersion, false, false)
	require.NoError(t, err)
	withdrawVersion := d.WithdrawVersion
	withdrawAt := d.WithdrawAt
	snapshot, _, err := s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	_, _, err = s.Provider(c.ID, secret, snapshot.Version)
	require.NoError(t, err)
	input = inputFromDevice(d)
	input.IntervalSeconds = 15
	d, err = s.SaveDevice(scope, d.ID, "", input)
	require.NoError(t, err)
	require.Equal(t, withdrawVersion, d.WithdrawVersion)
	require.Equal(t, withdrawAt, d.WithdrawAt)
	require.Equal(t, 120, d.WithdrawIntervalSeconds)
	require.NoError(t, s.DB.First(&c, "id = ?", c.ID).Error)
	require.False(t, withdrawn(d, c, time.Now().Add(91*time.Second).Unix()))
	require.True(t, withdrawn(d, c, time.Now().Add(361*time.Second).Unix()))
}
