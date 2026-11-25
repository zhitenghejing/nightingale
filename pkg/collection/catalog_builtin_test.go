package collection

import (
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
)

func TestBundledLinuxX86SNMPRelease(t *testing.T) {
	s, scope, _, _, _ := testStore(t)
	require.NoError(t, s.DB.AutoMigrate(&models.BuiltinComponent{}))
	require.NoError(t, s.EnsureBuiltinCatalog())
	view, err := s.Catalog()
	require.NoError(t, err)
	require.Len(t, view.Entries, 1)
	entry := view.Entries[0]
	require.Equal(t, "SNMP采集", entry.Name)
	require.NotNil(t, entry.Published)
	require.Equal(t, 2, entry.Published.Version)
	require.Equal(t, []string{"主机系统", "Linux", "X86"}, entry.Published.CategoryPath)
	require.True(t, entry.Onboarding.Ready)
	pack := entry.Published.Definition.Pack
	require.Equal(t, entry.ComponentID, pack.ComponentID)
	require.Len(t, pack.Metrics, 34)
	require.Len(t, pack.Dashboards, 1)
	require.Len(t, pack.Alerts, 5)

	metadata := CatalogMetadata{CategoryID: entry.CategoryID, Name: "本地 Linux SNMP", RecordVersion: entry.RecordVersion}
	require.NoError(t, s.SaveCatalogMetadata(scope, entry.ComponentID, metadata))
	require.NoError(t, s.EnsureBuiltinCatalog())
	edited, err := s.CatalogEntry(entry.ComponentID)
	require.NoError(t, err)
	require.Equal(t, metadata.Name, edited.Name)
	require.Equal(t, entry.PublishedVersionID, edited.PublishedVersionID)

	require.NoError(t, s.DeleteCatalog(scope, entry.ComponentID, edited.RecordVersion))
	require.NoError(t, s.EnsureBuiltinCatalog())
	view, err = s.Catalog()
	require.NoError(t, err)
	require.Empty(t, view.Entries, "restarts must respect local deletion")
}

func TestBundledLinuxX86SNMPPreservesExistingTemplate(t *testing.T) {
	s, scope, _, _, _ := testStore(t)
	require.NoError(t, s.DB.AutoMigrate(&models.BuiltinComponent{}))
	categoryID, err := seedCategory(s.DB, []string{"主机系统", "Linux", "X86"})
	require.NoError(t, err)
	id, err := s.CreateCatalog(scope, CatalogCreateInput{CategoryID: categoryID, Name: "SNMP采集"})
	require.NoError(t, err)
	require.NoError(t, s.EnsureBuiltinCatalog())

	var rows []models.MonitorCatalog
	require.NoError(t, s.DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, id, rows[0].ComponentID)
	require.Empty(t, rows[0].PublishedVersionID)
}
