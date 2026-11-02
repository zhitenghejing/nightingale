package collection

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

func catalogFixture(t *testing.T, s *Store) CatalogEntry {
	t.Helper()
	id := profileFixture(t, s, "Windows_SNMP")
	view, err := s.Catalog()
	require.NoError(t, err)
	for _, e := range view.Entries {
		if e.ComponentID == id {
			return e
		}
	}
	t.Fatal("catalog entry missing")
	return CatalogEntry{}
}
func TestCatalogPublicationAndDevicePinning(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	e := catalogFixture(t, s)
	require.NotNil(t, e.Published)
	require.Equal(t, TypeBrandModelTaxonomy, e.Published.Taxonomy)
	require.Equal(t, 1, e.Published.Version)
	original := e.PublishedVersionID
	var before models.BuiltinComponent
	require.NoError(t, s.DB.First(&before, "id = ?", e.ComponentID).Error)
	metadata := CatalogMetadata{Name: "Windows SNMP 自定义名称", Logo: "https://example.org/windows.svg", CategoryID: e.CategoryID, RecordVersion: e.RecordVersion}
	require.NoError(t, s.SaveCatalogMetadata(scope, e.ComponentID, metadata))
	require.Error(t, s.SaveCatalogMetadata(scope, e.ComponentID, metadata))
	var after models.BuiltinComponent
	require.NoError(t, s.DB.First(&after, "id = ?", e.ComponentID).Error)
	require.Equal(t, before, after, "metadata must not rewrite identity or localized instructions")
	view, err := s.Catalog()
	require.NoError(t, err)
	e = view.Entries[0]
	input := deviceInput(c, cred, "192.0.2.81")
	input.TemplateVersionID = original
	d, err := s.SaveDevice(scope, "", "catalog-pinned-device", input)
	require.NoError(t, err)
	profile, err := s.DeviceProfile(d)
	require.NoError(t, err)
	require.Len(t, profile.Boards, 1)
	require.Len(t, profile.Rules, 7)
	require.Equal(t, before.Readme, profile.Readme)
	definition := e.Draft
	definition.Config += "\n[[field]]\nname=\"users\"\noid=\".1.3.6.1.2.1.25.1.5.0\"\n"
	require.NoError(t, s.SaveCatalogDraft(scope, e.ComponentID, CatalogDraftInput{Definition: definition, RecordVersion: e.RecordVersion}, false))
	view, err = s.Catalog()
	require.NoError(t, err)
	e = view.Entries[0]
	require.Equal(t, original, e.PublishedVersionID)
	require.NoError(t, s.SaveCatalogDraft(scope, e.ComponentID, CatalogDraftInput{Definition: definition, RecordVersion: e.RecordVersion}, true))
	view, err = s.Catalog()
	require.NoError(t, err)
	e = view.Entries[0]
	require.Equal(t, 2, e.Published.Version)
	require.NotEqual(t, original, e.PublishedVersionID)
	saved, err := s.Device(scope, d.ID)
	require.NoError(t, err)
	require.Equal(t, original, saved.TemplateVersionID)
	// Existing customized rule instances survive an explicit version upgrade.
	ruleID := profile.Rules[0].ID
	require.NoError(t, s.DB.Model(&models.AlertRule{}).Where("id = ?", ruleID).Update("name", "device-only override").Error)
	upgrade := inputFromDevice(d)
	upgrade.TemplateVersionID = e.PublishedVersionID
	d, err = s.SaveDevice(scope, d.ID, "", upgrade)
	require.NoError(t, err)
	profile, err = s.DeviceProfile(d)
	require.NoError(t, err)
	require.Equal(t, ruleID, profile.Rules[0].ID)
	var rule models.AlertRule
	require.NoError(t, s.DB.First(&rule, "id = ?", ruleID).Error)
	require.Equal(t, "device-only override", rule.Name)
	// Retired versions cannot be used for a new device, but remain valid for reads.
	_, err = s.SaveDevice(scope, "", "catalog-retired-version", input)
	require.Error(t, err)
	require.Error(t, s.DeleteCatalog(scope, e.ComponentID, e.RecordVersion))
	require.Error(t, s.SaveCatalogDraft(Scope{ActorID: 2}, e.ComponentID, CatalogDraftInput{Definition: definition, RecordVersion: e.RecordVersion}, true))
	// Bootstrap is idempotent and preserves every user edit and published version.
	require.NoError(t, s.EnsureCatalog())
	again, err := s.Catalog()
	require.NoError(t, err)
	require.Equal(t, e, again.Entries[0])
}

func TestCatalogRejectsMissingMetricContract(t *testing.T) {
	s, scope, _, _, _ := testStore(t)
	e := catalogFixture(t, s)
	definition := e.Draft
	config, err := parseTemplate(definition.Config)
	require.NoError(t, err)
	for i, table := range config.Tables {
		if table.Name == "cpu" {
			config.Tables = append(config.Tables[:i], config.Tables[i+1:]...)
			break
		}
	}
	required := []string{}
	for _, name := range config.RequiredMetrics {
		if name != "snmp_cpu_load" {
			required = append(required, name)
		}
	}
	config.RequiredMetrics = required
	text, err := toml.Marshal(config)
	require.NoError(t, err)
	definition.Config = string(text)
	err = s.SaveCatalogDraft(scope, e.ComponentID, CatalogDraftInput{Definition: definition, RecordVersion: e.RecordVersion}, true)
	require.ErrorContains(t, err, "metric not produced")
	view, err := s.Catalog()
	require.NoError(t, err)
	require.Equal(t, e.PublishedVersionID, view.Entries[0].PublishedVersionID)
}

func TestCatalogIncompleteDraftNeverReplacesRelease(t *testing.T) {
	s, scope, _, _, _ := testStore(t)
	e := catalogFixture(t, s)
	definition := e.Draft
	definition.Config = "[[unfinished table"
	require.NoError(t, s.SaveCatalogDraft(scope, e.ComponentID, CatalogDraftInput{Definition: definition, RecordVersion: e.RecordVersion}, false))
	view, err := s.Catalog()
	require.NoError(t, err)
	saved := view.Entries[0]
	require.Equal(t, definition.Config, saved.Draft.Config)
	require.Equal(t, e.PublishedVersionID, saved.PublishedVersionID)
	require.Error(t, s.SaveCatalogDraft(scope, e.ComponentID, CatalogDraftInput{Definition: definition, RecordVersion: saved.RecordVersion}, true))
}
func TestCatalogValidationAndCategoryLifecycle(t *testing.T) {
	s, scope, _, _, _ := testStore(t)
	e := catalogFixture(t, s)
	badDefinition := e.Draft
	badDefinition.Fields = append(append([]DeviceField{}, badDefinition.Fields...), DeviceField{Key: "command", Label: "Command", Type: "text", Required: true})
	require.Error(t, s.SaveCatalogDraft(scope, e.ComponentID, CatalogDraftInput{Definition: badDefinition, RecordVersion: e.RecordVersion}, true))
	require.NoError(t, s.SaveCategory(scope, "", models.MonitorCategory{Name: "New family"}))
	view, err := s.Catalog()
	require.NoError(t, err)
	var root models.MonitorCategory
	for _, cat := range view.Categories {
		if cat.Name == "New family" {
			root = cat
		}
	}
	require.NotEmpty(t, root.ID)
	require.Error(t, s.SaveCategory(scope, "", models.MonitorCategory{Name: "New family"}))
	require.NoError(t, s.SaveCategory(scope, "", models.MonitorCategory{Name: "Vendor", ParentID: root.ID}))
	require.Error(t, s.DeleteCategory(scope, root.ID, root.RecordVersion))
	root.ParentID = e.CategoryID
	require.Error(t, s.SaveCategory(scope, root.ID, root))
	require.Error(t, s.DeleteCategory(scope, e.CategoryID, 1))
	metadata := CatalogMetadata{Name: e.Name, Logo: "javascript:alert(1)", CategoryID: e.CategoryID, RecordVersion: e.RecordVersion}
	require.Error(t, s.SaveCatalogMetadata(scope, e.ComponentID, metadata))
	require.NoError(t, s.DeleteCatalog(scope, e.ComponentID, e.RecordVersion))
	view, err = s.Catalog()
	require.NoError(t, err)
	require.Empty(t, view.Entries)
	var raw map[string][]string
	require.NoError(t, json.Unmarshal(catalogSeed, &raw))
	require.Len(t, raw, 93)
	for key, path := range raw {
		require.Len(t, path, 3, key)
		require.NotContains(t, strings.Join(path, "/"), "iDRAC 地址")
	}
}

func verifyCatalogSQL(t *testing.T, s *Store, scope Scope) {
	t.Helper()
	view, err := s.Catalog()
	require.NoError(t, err)
	var entry CatalogEntry
	for _, e := range view.Entries {
		if e.Ident == "Windows_SNMP" {
			entry = e
		}
	}
	require.NotEmpty(t, entry.PublishedVersionID)
	definition := entry.Draft
	start := make(chan struct{})
	errs := make(chan error, 2)
	other := &Store{DB: s.DB, Keys: s.Keys, Writers: s.Writers}
	for _, store := range []*Store{s, other} {
		go func(store *Store) {
			<-start
			errs <- store.SaveCatalogDraft(scope, entry.ComponentID, CatalogDraftInput{Definition: definition, RecordVersion: entry.RecordVersion}, true)
		}(store)
	}
	close(start)
	a, b := <-errs, <-errs
	require.True(t, (a == nil) != (b == nil), "exactly one catalog publication must win")
	if a != nil {
		require.Equal(t, 409, a.(*Error).Code, a.Error())
	}
	if b != nil {
		require.Equal(t, 409, b.(*Error).Code, b.Error())
	}
	view, err = s.Catalog()
	require.NoError(t, err)
	for _, e := range view.Entries {
		if e.ComponentID == entry.ComponentID {
			require.Equal(t, 2, e.Published.Version)
			require.Equal(t, entry.RecordVersion+1, e.RecordVersion)
		}
	}
	cloneID, err := s.CreateCatalog(scope, CatalogCreateInput{Name: "Independent SQL clone", CategoryID: entry.CategoryID, BaseComponentID: entry.ComponentID})
	require.NoError(t, err)
	clone, err := s.CatalogEntry(cloneID)
	require.NoError(t, err)
	require.Empty(t, clone.PublishedVersionID)
	require.NotNil(t, clone.Draft.Pack)
	clone.Draft.Pack.Readme = "clone-only instructions"
	require.NoError(t, s.SaveCatalogDraft(scope, cloneID, CatalogDraftInput{Definition: clone.Draft, RecordVersion: clone.RecordVersion}, true))
	original, err := s.CatalogEntry(entry.ComponentID)
	require.NoError(t, err)
	require.NotEqual(t, "clone-only instructions", original.Draft.Pack.Readme)
}
