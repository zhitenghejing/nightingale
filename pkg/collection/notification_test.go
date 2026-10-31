package collection

import (
	"encoding/json"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
)

func TestDeviceNotificationPresetUsesNativeRulesAndRendersPrototypeVariables(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	e := catalogFixture(t, s)
	require.NoError(t, s.DB.AutoMigrate(&models.NotifyRule{}, &models.NotifyChannelConfig{}))
	require.NoError(t, s.DB.Create(&models.NotifyChannelConfig{ID: 1, Name: "fixture only", Enable: true}).Error)
	require.NoError(t, s.DB.Create(&models.NotifyRule{ID: 1, Name: "fixture only", Enable: true, NotifyConfigs: []models.NotifyConfig{{ChannelID: 1}}}).Error)
	f := deviceInput(c, cred, "192.0.2.126")
	f.TemplateVersionID = e.PublishedVersionID
	f.Notify = &NotificationPreset{Mode: "rules", RuleIDs: []int64{1}, Message: "{{设备名称}} · {{设备分类}} · {{规则名称}} · {{当前值}} · {{发生时间}} · {{告警级别}}"}
	d, err := s.SaveDevice(scope, "", "notification-fixture", f)
	require.NoError(t, err)
	profile, err := s.DeviceProfile(d)
	require.NoError(t, err)
	var rule models.AlertRule
	require.NoError(t, s.DB.First(&rule, "id = ?", profile.Rules[0].ID).Error)
	require.Equal(t, []int64{1}, rule.NotifyRuleIds)
	require.Equal(t, 1, rule.NotifyVersion)
	require.Equal(t, 1, rule.Disabled, "mapping must not bypass the required-sample gate")
	event := models.AlertCurEvent{RuleName: "CPU high", TriggerValue: "91", TriggerTime: 123, Severity: 2, Annotations: rule.Annotations}
	require.NoError(t, event.ParseRule("annotations"))
	require.Contains(t, event.AnnotationsJSON["collection_message"], "CPU high · 91 · 123 · 2")
	event.RuleNote = rule.Note
	require.NoError(t, event.ParseRule("rule_note"))
	require.Equal(t, event.AnnotationsJSON["collection_message"], event.RuleNote)
	f = inputFromDevice(d)
	f.Notify = &NotificationPreset{Mode: "events_only", RuleIDs: []int64{1}}
	_, err = s.SaveDevice(scope, d.ID, "", f)
	require.NoError(t, err)
	require.NoError(t, s.DB.First(&rule, "id = ?", profile.Rules[0].ID).Error)
	require.Empty(t, rule.NotifyRuleIds)
	// Invalid mappings fail the entire device transaction and keep the old one.
	f.RecordVersion++
	f.Notify = &NotificationPreset{Mode: "rules", RuleIDs: []int64{999}}
	_, err = s.SaveDevice(scope, d.ID, "", f)
	require.ErrorContains(t, err, "missing or disabled")
	actual, err := s.Device(scope, d.ID)
	require.NoError(t, err)
	var preset NotificationPreset
	require.NoError(t, json.Unmarshal([]byte(actual.Notify), &preset))
	require.Equal(t, "events_only", preset.Mode)
}
