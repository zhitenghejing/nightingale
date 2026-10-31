package collection

import (
	"encoding/json"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
)

func TestPrototypeSupportsTemplateDrivenDatabaseFields(t *testing.T) {
	var definition CatalogDefinition
	require.NoError(t, json.Unmarshal([]byte(`{"adapter":"mysql","config":"","notification":"events_only","fields":[{"key":"username","label":"监控账号","type":"text","required":true},{"key":"password","label":"密码","type":"password","required":true,"secret":true},{"key":"port","label":"端口","type":"number","required":true,"default":3306}]}`), &definition))
	require.NoError(t, validateCatalogDefinition(definition, true), "the accepted prototype defines database-specific fields; templates must not be restricted to the SNMP connection form")
}

func TestPrototypePublicationUsesWholeDraft(t *testing.T) {
	s, scope, _, _, _ := testStore(t)
	e := catalogFixture(t, s)
	var saved map[string]interface{}
	require.NoError(t, json.Unmarshal(canonical(e.Draft), &saved))
	require.Contains(t, saved, "pack", "all four native parts must belong to the draft")
	// Editing the old native resource library after loading a draft must not
	// silently change what gets published from that draft.
	require.NoError(t, s.DB.Model(&models.BuiltinComponent{}).Where("id = ?", e.ComponentID).Update("readme", "outside the saved draft").Error)
	require.NoError(t, s.SaveCatalogDraft(scope, e.ComponentID, CatalogDraftInput{Definition: e.Draft, RecordVersion: e.RecordVersion}, true))
	view, err := s.Catalog()
	require.NoError(t, err)
	doc, err := loadTemplate(s.DB, view.Entries[0].PublishedVersionID)
	require.NoError(t, err)
	require.NotEqual(t, "outside the saved draft", doc.Profile.Readme)
}

func TestPrototypeUpgradeMergesUnmodifiedRuleFields(t *testing.T) {
	s, scope, collector, credential, _ := testStore(t)
	e := catalogFixture(t, s)
	f := deviceInput(collector, credential, "192.0.2.123")
	f.TemplateVersionID = e.PublishedVersionID
	d, err := s.SaveDevice(scope, "", "prototype-upgrade-merge", f)
	require.NoError(t, err)
	p, err := s.DeviceProfile(d)
	require.NoError(t, err)
	ruleID := p.Rules[0].ID
	require.NoError(t, s.DB.Model(&models.AlertRule{}).Where("id = ?", ruleID).Update("name", "device custom title").Error)
	var rule map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(e.Draft.Pack.Alerts[0].Content), &rule))
	rule["prom_for_duration"] = 600
	e.Draft.Pack.Alerts[0].Content = string(canonical(rule))
	require.NoError(t, s.SaveCatalogDraft(scope, e.ComponentID, CatalogDraftInput{Definition: e.Draft, RecordVersion: e.RecordVersion}, true))
	view, err := s.Catalog()
	require.NoError(t, err)
	f = inputFromDevice(d)
	f.TemplateVersionID = view.Entries[0].PublishedVersionID
	_, err = s.SaveDevice(scope, d.ID, "", f)
	require.NoError(t, err)
	var actual models.AlertRule
	require.NoError(t, s.DB.First(&actual, "id = ?", ruleID).Error)
	require.Equal(t, "device custom title", actual.Name)
	require.Equal(t, 600, actual.PromForDuration, "the old implementation silently kept every field of the old resource")
}
