package collection

import (
	"encoding/json"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
)

func TestBundledLinuxTemplates(t *testing.T) {
	s, scope, _, _, _ := testStore(t)
	require.NoError(t, s.DB.AutoMigrate(&models.BuiltinComponent{}))
	require.NoError(t, s.EnsureBuiltinCatalog())
	view, err := s.Catalog()
	require.NoError(t, err)
	entries := map[string]CatalogEntry{}
	for _, row := range view.Entries {
		entries[row.Ident] = row
	}
	require.Len(t, entries, 4)
	entry := entries[linuxX86SNMPIdent]
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

	arm := entries[linuxARMSNMPIdent]
	require.Equal(t, "SNMP采集", arm.Name)
	require.NotNil(t, arm.Published)
	require.Equal(t, 1, arm.Published.Version)
	require.Equal(t, []string{"主机系统", "Linux", "ARM"}, arm.Published.CategoryPath)
	require.True(t, arm.Onboarding.Ready)
	require.NotEqual(t, entry.PublishedVersionID, arm.PublishedVersionID)
	require.Equal(t, entry.Published.Definition.Config, arm.Published.Definition.Config)
	require.Equal(t, "Linux ARM SNMP 总览", arm.Published.Definition.Pack.Dashboards[0].Name)

	agent := entries[linuxARMAgentIdent]
	require.Equal(t, "Agent采集", agent.Name)
	require.Equal(t, arm.CategoryID, agent.CategoryID)
	require.Nil(t, agent.Published)
	require.Equal(t, "local", agent.Onboarding.Execution)
	require.False(t, agent.Onboarding.Ready)
	require.NotEmpty(t, agent.Draft.Pack.Metrics)
	require.Len(t, agent.Draft.Pack.Dashboards, 1)
	require.NotEmpty(t, agent.Draft.Pack.Alerts)
	for _, resource := range agent.Draft.Pack.Alerts {
		var rule models.AlertRule
		require.NoError(t, json.Unmarshal([]byte(resource.Content), &rule))
		require.Empty(t, rule.NotifyChannelsJSON)
		require.Empty(t, rule.PipelineConfigs)
	}

	x86Agent := entries[linuxX86AgentIdent]
	require.Equal(t, "Agent采集", x86Agent.Name)
	require.Equal(t, entry.CategoryID, x86Agent.CategoryID)
	require.NotEqual(t, agent.ComponentID, x86Agent.ComponentID)
	require.Nil(t, x86Agent.Published)
	require.Equal(t, "local", x86Agent.Onboarding.Execution)
	require.Contains(t, x86Agent.Draft.Pack.Readme, "Linux AMD64")
	require.Equal(t, "Linux X86 Agent 总览", x86Agent.Draft.Pack.Dashboards[0].Name)
	require.Len(t, x86Agent.Draft.Pack.Metrics, len(agent.Draft.Pack.Metrics))
	require.Len(t, x86Agent.Draft.Pack.Alerts, len(agent.Draft.Pack.Alerts))

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
	require.Len(t, view.Entries, 3, "restarts must respect local deletion")
	for _, row := range view.Entries {
		require.NotEqual(t, linuxX86SNMPIdent, row.Ident)
	}
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
	require.NoError(t, s.DB.Where("category_id = ? AND name = ?", categoryID, "SNMP采集").Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, id, rows[0].ComponentID)
	require.Empty(t, rows[0].PublishedVersionID)
}
