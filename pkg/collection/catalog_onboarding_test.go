package collection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
)

func documentationFixture(t *testing.T, s *Store, ident string) uint64 {
	t.Helper()
	if s.DB.Dialector.Name() == "postgres" {
		require.NoError(t, s.DB.AutoMigrate(&models.PostgresBuiltinComponent{}, &models.PostgresBuiltinPayload{}, &models.BuiltinMetric{}))
	} else {
		require.NoError(t, s.DB.AutoMigrate(&models.BuiltinComponent{}, &models.BuiltinPayload{}, &models.BuiltinMetric{}))
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "integrations", ident, "markdown", "README.md"))
	require.NoError(t, err)
	var maximum uint64
	require.NoError(t, s.DB.Model(&models.BuiltinComponent{}).Select("coalesce(max(id),0)").Scan(&maximum).Error)
	c := models.BuiltinComponent{ID: maximum + 1, Ident: ident, Readme: string(b)}
	require.NoError(t, s.DB.Create(&c).Error)
	return c.ID
}

func TestExistingCatalogOnboardingKeepsUnpublishedTemplates(t *testing.T) {
	s, scope, _, _, _ := testStore(t)
	for _, ident := range []string{"Redis", "Oracle", "Linux", "Redfish"} {
		documentationFixture(t, s, ident)
	}
	view, err := s.Catalog()
	require.NoError(t, err)
	require.Len(t, view.Entries, 4)
	for _, entry := range view.Entries {
		require.Nil(t, entry.Published)
		require.False(t, entry.Onboarding.Ready)
		require.Equal(t, "unpublished", entry.Onboarding.State)
		require.Contains(t, entry.Onboarding.Issues, "尚未发布监控包")
		require.NotEmpty(t, entry.Draft.Fields, entry.Ident)
		require.NoError(t, validateCatalogDefinition(entry.Draft, false))
		if entry.Ident == "Redis" {
			require.Equal(t, "redis", entry.Draft.Adapter)
			entry.Draft.Fields[0].Label = "自定义 Redis 端口"
			require.NoError(t, s.SaveCatalogDraft(scope, entry.ComponentID, CatalogDraftInput{Definition: entry.Draft, RecordVersion: entry.RecordVersion}, false))
			again, err := s.CatalogEntry(entry.ComponentID)
			require.NoError(t, err)
			require.Equal(t, "自定义 Redis 端口", again.Draft.Fields[0].Label)
		}
	}
	summary, err := s.CatalogSummary()
	require.NoError(t, err)
	require.Len(t, summary.Entries, 4)
	for _, entry := range summary.Entries {
		require.Nil(t, entry.Draft.Pack)
		require.NotEmpty(t, entry.Onboarding.Issues, "summary keeps readiness after removing the large pack")
	}
}

func TestOnboardingBackfillPreservesCustomDraftAndPublishedDevice(t *testing.T) {
	s, _, _, _, _ := testStore(t)
	e := catalogFixture(t, s)
	before := e
	_, err := s.Catalog()
	require.NoError(t, err)
	after, err := s.CatalogEntry(e.ComponentID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.True(t, after.Onboarding.Ready)
	// Metadata-only old drafts have already been upgraded to a complete pack;
	// field initialization must also work on those existing rows, exactly once.
	id := documentationFixture(t, s, "Redis")
	require.NoError(t, s.EnsureCatalog())
	var row models.MonitorCatalog
	require.NoError(t, s.DB.First(&row, "component_id = ?", id).Error)
	var d CatalogDefinition
	require.NoError(t, json.Unmarshal([]byte(row.Draft), &d))
	d.Adapter = "documentation"
	d.Fields = []DeviceField{{Key: "custom", Label: "用户字段", Type: "text"}}
	d.Execution = ""
	require.NoError(t, s.DB.Model(&row).Update("draft", string(canonical(d))).Error)
	custom, err := s.CatalogEntry(id)
	require.NoError(t, err)
	expected := d
	expected.Reachability = DefaultReachability
	require.Equal(t, expected, custom.Draft)
	var unchanged models.MonitorCatalog
	require.NoError(t, s.DB.First(&unchanged, "component_id = ?", id).Error)
	require.Equal(t, string(canonical(d)), string(unchanged.Draft), "effective baseline must not rewrite the custom draft")
}

func TestEveryNativeOnboardingFormIsAValidDraft(t *testing.T) {
	var paths map[string][]string
	require.NoError(t, json.Unmarshal(catalogSeed, &paths))
	for ident := range paths {
		t.Run(ident, func(t *testing.T) {
			fields, adapter := onboardingFields(ident, "local")
			require.NoError(t, validateCatalogDefinition(CatalogDefinition{Adapter: adapter, Fields: fields, Notification: "events_only"}, false))
		})
	}
}

func verifyCatalogOnboardingSQL(t *testing.T, s *Store, scope Scope) {
	t.Helper()
	id := documentationFixture(t, s, "Redis")
	entry, err := s.CatalogEntry(id)
	require.NoError(t, err)
	require.False(t, entry.Onboarding.Ready)
	require.Equal(t, "redis", entry.Draft.Adapter)
	require.Len(t, entry.Draft.Fields, 3)
	again, err := s.CatalogEntry(id)
	require.NoError(t, err)
	require.Equal(t, entry.RecordVersion, again.RecordVersion, "bootstrap must run once")
	entry.Draft.Fields[0].Default = float64(6380)
	require.NoError(t, s.SaveCatalogDraft(scope, id, CatalogDraftInput{Definition: entry.Draft, RecordVersion: entry.RecordVersion}, false))
	again, err = s.CatalogEntry(id)
	require.NoError(t, err)
	require.Equal(t, float64(6380), again.Draft.Fields[0].Default)
	require.False(t, again.Onboarding.Ready)
}
