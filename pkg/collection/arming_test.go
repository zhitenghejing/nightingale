package collection

import (
	"context"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestPublishedPackageWaitsForCompleteRevisionBeforeArming(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	e := catalogFixture(t, s)
	f := deviceInput(c, cred, "192.0.2.125")
	f.TemplateVersionID = e.PublishedVersionID
	d, err := s.SaveDevice(scope, "", "package-arming-fixture", f)
	require.NoError(t, err)
	var rules []models.AlertRule
	require.NoError(t, s.DB.Find(&rules).Error)
	for _, rule := range rules {
		require.Equal(t, 1, rule.Disabled, "no absent alerts before first successful sample contract")
	}
	service := &StatusService{Store: s, Query: func(context.Context, int64, string, time.Time) (model.Value, error) { return model.Matrix{}, nil }}
	_, err = service.States(context.Background(), []models.MonitorDevice{d})
	require.NoError(t, err)
	state, err := deviceProfileState(d)
	require.NoError(t, err)
	require.NotEmpty(t, state.Waiting)
	config, err := s.TemplateConfig(d.TemplateVersionID)
	require.NoError(t, err)
	matrix := model.Matrix{}
	for _, name := range append(config.RequiredMetrics, "snmp_up") {
		matrix = append(matrix, &model.SampleStream{Metric: model.Metric{"__name__": model.LabelValue(name), "collection_device_id": model.LabelValue(d.ID), "collection_collector_id": model.LabelValue(d.CollectorID), "collection_revision": model.LabelValue(d.DesiredRevision), "collection_mode": "managed"}, Values: []model.SamplePair{{Timestamp: model.Time(time.Now().UnixMilli()), Value: 1}}})
	}
	service = &StatusService{Store: s, Query: func(context.Context, int64, string, time.Time) (model.Value, error) { return matrix, nil }}
	states, err := service.States(context.Background(), []models.MonitorDevice{d})
	require.NoError(t, err)
	require.Equal(t, "healthy", states[d.ID].Health)
	require.NoError(t, s.DB.Find(&rules).Error)
	for _, rule := range rules {
		require.Zero(t, rule.Disabled)
	}
	updated, err := s.Device(scope, d.ID)
	require.NoError(t, err)
	state, err = deviceProfileState(updated)
	require.NoError(t, err)
	require.Empty(t, state.Waiting)
	require.Equal(t, d.DesiredRevision, state.ArmedRevision)
}

func TestArmedPackageKeepsOutageRulesActiveAfterFailedReconfiguration(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	e := catalogFixture(t, s)
	f := deviceInput(c, cred, "192.0.2.126")
	f.TemplateVersionID = e.PublishedVersionID
	d, err := s.SaveDevice(scope, "", "armed-config-change", f)
	require.NoError(t, err)
	// The complete initial contract is covered above; start with armed resources.
	require.NoError(t, s.armDevice(d))
	d, err = s.Device(scope, d.ID)
	require.NoError(t, err)
	state, err := deviceProfileState(d)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(state.Rules), 2)
	userDisabled := state.Rules[0].ID
	require.NoError(t, s.DB.Model(&models.AlertRule{}).Where("id = ?", userDisabled).Updates(map[string]interface{}{"disabled": 1, "update_by": "human"}).Error)
	changed := inputFromDevice(d)
	changed.Port++ // A broken endpoint must still be monitored for interruption.
	next, err := s.SaveDevice(scope, d.ID, "", changed)
	require.NoError(t, err)
	require.NotEqual(t, d.DesiredRevision, next.DesiredRevision)
	service := &StatusService{Store: s, Query: func(context.Context, int64, string, time.Time) (model.Value, error) {
		return model.Matrix{&model.SampleStream{Metric: model.Metric{"__name__": "snmp_up", "collection_device_id": model.LabelValue(next.ID), "collection_collector_id": model.LabelValue(next.CollectorID), "collection_revision": model.LabelValue(next.DesiredRevision), "collection_mode": "managed"}, Values: []model.SamplePair{{Timestamp: model.Now(), Value: 0}}}}, nil
	}}
	states, err := service.States(context.Background(), []models.MonitorDevice{next})
	require.NoError(t, err)
	require.Equal(t, "failed", states[next.ID].Health)
	for _, ref := range state.Rules {
		var rule models.AlertRule
		require.NoError(t, s.DB.First(&rule, "id = ?", ref.ID).Error)
		if rule.Id == userDisabled {
			require.Equal(t, 1, rule.Disabled)
		} else {
			require.Zero(t, rule.Disabled, "previously armed rules remain active after collection failure")
		}
		require.Contains(t, rule.RuleConfig, next.DesiredRevision)
		require.NotContains(t, rule.RuleConfig, d.DesiredRevision)
	}
	paused, err := s.SetDeviceState(scope, next.ID, next.RecordVersion, false, false)
	require.NoError(t, err)
	resumed, err := s.SetDeviceState(scope, next.ID, paused.RecordVersion, true, false)
	require.NoError(t, err)
	for _, ref := range state.Rules {
		var rule models.AlertRule
		require.NoError(t, s.DB.First(&rule, "id = ?", ref.ID).Error)
		if rule.Id == userDisabled {
			require.Equal(t, 1, rule.Disabled)
		} else {
			require.Zero(t, rule.Disabled)
		}
		require.Contains(t, rule.RuleConfig, resumed.DesiredRevision)
	}
}

func TestNativeRuleEditsBeforeAndDuringInitialSuspensionRemainDistinct(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	e := catalogFixture(t, s)
	f := deviceInput(c, cred, "192.0.2.127")
	f.TemplateVersionID = e.PublishedVersionID
	d, err := s.SaveDevice(scope, "", "customized-arming", f)
	require.NoError(t, err)
	state, err := deviceProfileState(d)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(state.Rules), 3)
	for i, disabled := range []int{0, 1} {
		require.NoError(t, s.DB.Model(&models.AlertRule{}).Where("id = ?", state.Rules[i].ID).Updates(map[string]interface{}{"disabled": disabled, "update_by": "human-before-pause", "note": "device-specific customization"}).Error)
	}
	paused, err := s.SetDeviceState(scope, d.ID, d.RecordVersion, false, false)
	require.NoError(t, err)
	resumed, err := s.SetDeviceState(scope, d.ID, paused.RecordVersion, true, false)
	require.NoError(t, err)
	// An edit after the system's forced pause must not be overwritten by arming.
	require.NoError(t, s.DB.Model(&models.AlertRule{}).Where("id = ?", state.Rules[2].ID).Updates(map[string]interface{}{"disabled": 1, "update_by": "human-during-wait"}).Error)
	require.NoError(t, s.armDevice(resumed))
	for i, disabled := range []int{0, 1, 1} {
		var rule models.AlertRule
		require.NoError(t, s.DB.First(&rule, "id = ?", state.Rules[i].ID).Error)
		require.Equal(t, disabled, rule.Disabled)
		if i < 2 {
			require.Equal(t, "device-specific customization", rule.Note)
		}
	}
}

func TestArmedPackageUpgradeWaitsOnlyForNewRules(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	e := catalogFixture(t, s)
	f := deviceInput(c, cred, "192.0.2.128")
	f.TemplateVersionID = e.PublishedVersionID
	d, err := s.SaveDevice(scope, "", "new-rule-arming", f)
	require.NoError(t, err)
	require.NoError(t, s.armDevice(d))
	d, err = s.Device(scope, d.ID)
	require.NoError(t, err)
	original, err := deviceProfileState(d)
	require.NoError(t, err)
	added := e.Draft.Pack.Alerts[0]
	added.ID = 0
	added.Name = "new independently gated rule"
	e.Draft.Pack.Alerts = append(e.Draft.Pack.Alerts, added)
	require.NoError(t, s.SaveCatalogDraft(scope, e.ComponentID, CatalogDraftInput{Definition: e.Draft, RecordVersion: e.RecordVersion}, true))
	entry, err := s.CatalogEntry(e.ComponentID)
	require.NoError(t, err)
	f = inputFromDevice(d)
	f.TemplateVersionID = entry.PublishedVersionID
	next, err := s.SaveDevice(scope, d.ID, "", f)
	require.NoError(t, err)
	current, err := deviceProfileState(next)
	require.NoError(t, err)
	require.Len(t, current.Rules, len(original.Rules)+1)
	require.Len(t, current.Waiting, 1)
	paused, err := s.SetDeviceState(scope, next.ID, next.RecordVersion, false, false)
	require.NoError(t, err)
	next, err = s.SetDeviceState(scope, next.ID, paused.RecordVersion, true, false)
	require.NoError(t, err)
	for _, ref := range current.Rules {
		var rule models.AlertRule
		require.NoError(t, s.DB.First(&rule, "id = ?", ref.ID).Error)
		if ref.Name == added.Name {
			require.Equal(t, 1, rule.Disabled)
		} else {
			require.Zero(t, rule.Disabled)
		}
	}
	require.NoError(t, s.armDevice(next))
	var rules []models.AlertRule
	require.NoError(t, s.DB.Find(&rules).Error)
	for _, rule := range rules {
		require.Zero(t, rule.Disabled)
	}
}

func TestNativeWaitingRuleEnableSurvivesLaterMetadataSave(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	e := catalogFixture(t, s)
	f := deviceInput(c, cred, "192.0.2.129")
	f.TemplateVersionID = e.PublishedVersionID
	d, err := s.SaveDevice(scope, "", "native-enable-while-waiting", f)
	require.NoError(t, err)
	state, err := deviceProfileState(d)
	require.NoError(t, err)
	id := state.Rules[0].ID
	require.NoError(t, s.DB.Model(&models.AlertRule{}).Where("id = ?", id).Updates(map[string]interface{}{"disabled": 0, "update_by": "human"}).Error)
	f = inputFromDevice(d)
	f.Name = "metadata edit while waiting"
	next, err := s.SaveDevice(scope, d.ID, "", f)
	require.NoError(t, err)
	require.NoError(t, s.armDevice(next))
	var rule models.AlertRule
	require.NoError(t, s.DB.First(&rule, "id = ?", id).Error)
	require.Zero(t, rule.Disabled)
	paused, err := s.SetDeviceState(scope, next.ID, next.RecordVersion, false, false)
	require.NoError(t, err)
	require.NoError(t, s.DB.Model(&models.AlertRule{}).Where("id = ?", id).Updates(map[string]interface{}{"disabled": 1, "update_by": "human-while-paused"}).Error)
	_, err = s.SetDeviceState(scope, next.ID, paused.RecordVersion, true, false)
	require.NoError(t, err)
	require.NoError(t, s.DB.First(&rule, "id = ?", id).Error)
	require.Equal(t, 1, rule.Disabled)
}
