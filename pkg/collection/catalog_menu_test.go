package collection

import (
	"encoding/json"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
)

func menuCategory(t *testing.T, s *Store, scope Scope, parent, name string) models.MonitorCategory {
	t.Helper()
	require.NoError(t, s.SaveCategory(scope, "", models.MonitorCategory{Name: name, ParentID: parent}))
	var c models.MonitorCategory
	require.NoError(t, s.DB.First(&c, "parent_id = ? AND name = ?", parent, name).Error)
	return c
}

func TestCatalogMenuMovesKeepThreeLevelsAndOnlyChangeLocation(t *testing.T) {
	s, scope, _, _, _ := testStore(t)
	e := catalogFixture(t, s)
	root := menuCategory(t, s, scope, "", "Test hardware")
	a := menuCategory(t, s, scope, root.ID, "Vendor A")
	b := menuCategory(t, s, scope, root.ID, "Vendor B")
	m := menuCategory(t, s, scope, a.ID, "Model One")
	m2 := menuCategory(t, s, scope, b.ID, "Model Two")
	require.Error(t, s.MoveCategory(Scope{}, m.ID, CategoryMoveInput{ParentID: b.ID, RecordVersion: 1}))
	require.Error(t, s.MoveCatalog(Scope{}, e.ComponentID, CatalogMoveInput{CategoryID: m.ID, RecordVersion: e.RecordVersion}))
	var original models.MonitorCatalog
	require.NoError(t, s.DB.First(&original, "component_id = ?", e.ComponentID).Error)
	var releases []models.MonitorTemplateVersion
	require.NoError(t, s.DB.Order("id").Find(&releases).Error)
	require.Error(t, s.MoveCatalog(scope, e.ComponentID, CatalogMoveInput{CategoryID: b.ID, RecordVersion: e.RecordVersion}))
	require.NoError(t, s.MoveCatalog(scope, e.ComponentID, CatalogMoveInput{CategoryID: m.ID, RecordVersion: e.RecordVersion}))
	require.Error(t, s.MoveCatalog(scope, e.ComponentID, CatalogMoveInput{CategoryID: e.CategoryID, RecordVersion: e.RecordVersion}))
	require.Error(t, s.DeleteCategory(scope, m.ID, 1), "a nonempty model cannot lose its templates")
	require.NoError(t, s.MoveCategory(scope, m.ID, CategoryMoveInput{ParentID: b.ID, BeforeID: m2.ID, RecordVersion: 1}))
	require.Error(t, s.MoveCategory(scope, m.ID, CategoryMoveInput{ParentID: a.ID, RecordVersion: 1}), "stale drag snapshots must not overwrite a later move")
	path, err := categoryPath(s.DB, m.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"Test hardware", "Vendor B", "Model One"}, path)
	var siblings []models.MonitorCategory
	require.NoError(t, s.DB.Where("parent_id = ?", b.ID).Order("sort,name,id").Find(&siblings).Error)
	require.Equal(t, []string{m.ID, m2.ID}, []string{siblings[0].ID, siblings[1].ID})
	require.Error(t, s.MoveCategory(scope, m.ID, CategoryMoveInput{ParentID: root.ID, RecordVersion: 2}), "model cannot become a brand")
	require.Error(t, s.MoveCategory(scope, root.ID, CategoryMoveInput{ParentID: m.ID, RecordVersion: 1}), "no descendant cycle")
	var current models.MonitorCatalog
	require.NoError(t, s.DB.First(&current, "component_id = ?", e.ComponentID).Error)
	original.CategoryID, original.RecordVersion = m.ID, original.RecordVersion+1
	require.Equal(t, original, current, "a drag must not resubmit/replace names, icons, drafts or releases")
	var after []models.MonitorTemplateVersion
	require.NoError(t, s.DB.Order("id").Find(&after).Error)
	require.Equal(t, releases, after)
	duplicate := menuCategory(t, s, scope, a.ID, m.Name)
	require.Error(t, s.MoveCategory(scope, duplicate.ID, CategoryMoveInput{ParentID: b.ID, RecordVersion: 1}), "same-brand model names remain unique")
}

func TestCatalogTaxonomyMigratesDefaultsOnceWithoutRepublishingOrReseedingDeletedMenus(t *testing.T) {
	s, scope, collector, credential, _ := testStore(t)
	e := catalogFixture(t, s)
	d, err := s.SaveDevice(scope, "", "taxonomy-pinned", DeviceInput{Name: "pinned", Address: "192.0.2.88", GroupID: 1, CollectorID: collector.ID, CredentialID: credential.ID, TemplateVersionID: e.PublishedVersionID})
	require.NoError(t, err)
	legacy, err := seedCategory(s.DB, []string{"操作系统", "Windows", "通用 SNMP"})
	require.NoError(t, err)
	require.NoError(t, s.DB.Model(&models.MonitorCatalog{}).Where("component_id = ?", e.ComponentID).Update("category_id", legacy).Error)
	customRoot := menuCategory(t, s, scope, "", "Custom type")
	customBrand := menuCategory(t, s, scope, customRoot.ID, "Custom brand")
	customModel := menuCategory(t, s, scope, customBrand.ID, "Custom model")
	require.NoError(t, s.DB.Create(&models.BuiltinComponent{ID: 900, Ident: "Custom", Readme: "instructions"}).Error)
	require.NoError(t, s.DB.Create(&models.MonitorCatalog{ComponentID: 900, Name: "Custom", CategoryID: customModel.ID, Draft: models.MonitorText(canonical(e.Draft)), RecordVersion: 3}).Error)
	var releases []models.MonitorTemplateVersion
	require.NoError(t, s.DB.Order("id").Find(&releases).Error)
	require.NoError(t, s.DB.Where("action = ?", "catalog_taxonomy_type_brand_model").Delete(&models.MonitorAudit{}).Error)
	require.NoError(t, s.EnsureCatalog())
	var row models.MonitorCatalog
	require.NoError(t, s.DB.First(&row, "component_id = ?", e.ComponentID).Error)
	path, err := categoryPath(s.DB, row.CategoryID)
	require.NoError(t, err)
	require.Equal(t, []string{"操作系统", "微软", "Windows 通用"}, path)
	var custom models.MonitorCatalog
	require.NoError(t, s.DB.First(&custom, "component_id = ?", 900).Error)
	require.Equal(t, customModel.ID, custom.CategoryID)
	require.Equal(t, int64(3), custom.RecordVersion)
	var after []models.MonitorTemplateVersion
	require.NoError(t, s.DB.Order("id").Find(&after).Error)
	require.Equal(t, releases, after)
	saved, err := s.Device(scope, d.ID)
	require.NoError(t, err)
	require.Equal(t, d, saved)
	var empty models.MonitorCategory
	require.NoError(t, s.DB.First(&empty, "name = ? AND parent_id = ?", "安全设备", "").Error)
	require.NoError(t, s.DeleteCategory(scope, empty.ID, empty.RecordVersion))
	require.NoError(t, s.EnsureCatalog())
	var n int64
	require.NoError(t, s.DB.Model(&models.MonitorCategory{}).Where("id = ?", empty.ID).Count(&n).Error)
	require.Zero(t, n, "deleting an empty seeded type is persistent")
	// An intentional later move back to a legacy shape must not be reclassified.
	legacy, err = seedCategory(s.DB, []string{"操作系统", "Windows", "通用 SNMP"})
	require.NoError(t, err)
	require.NoError(t, s.MoveCatalog(scope, e.ComponentID, CatalogMoveInput{CategoryID: legacy, RecordVersion: row.RecordVersion}))
	require.NoError(t, s.EnsureCatalog())
	require.NoError(t, s.DB.First(&row, "component_id = ?", e.ComponentID).Error)
	require.Equal(t, legacy, row.CategoryID)
	var marker int64
	require.NoError(t, s.DB.Model(&models.MonitorAudit{}).Where("action = ?", "catalog_taxonomy_type_brand_model").Count(&marker).Error)
	require.Equal(t, int64(1), marker)
	var definition templateDocument
	require.NoError(t, json.Unmarshal([]byte(releases[0].Definition), &definition))
}

func TestCatalogTaxonomyPreservesAnIntentionalMoveToAnotherOldSeedLocation(t *testing.T) {
	s, _, _, _, _ := testStore(t)
	e := catalogFixture(t, s)
	other, err := seedCategory(s.DB, []string{"操作系统", "Linux", "通用 SNMP"})
	require.NoError(t, err)
	require.NoError(t, s.DB.Model(&models.MonitorCatalog{}).Where("component_id = ?", e.ComponentID).Update("category_id", other).Error)
	require.NoError(t, s.DB.Where("action = ?", "catalog_taxonomy_type_brand_model").Delete(&models.MonitorAudit{}).Error)
	require.NoError(t, s.EnsureCatalog())
	var current models.MonitorCatalog
	require.NoError(t, s.DB.First(&current, "component_id = ?", e.ComponentID).Error)
	require.Equal(t, other, current.CategoryID)
}
