package collection

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

// Explicit opt-in: read an authorized Windows agent using the actual pinned
// collector. Test mode has no writers and never submits these samples to a TSDB.
func TestWindowsExtendedLiveCategraf(t *testing.T) {
	privateFile, binary := os.Getenv("N9E_TEST_WINDOWS_SNMP_FILE"), os.Getenv("N9E_TEST_CATEGRAF_BINARY")
	if privateFile == "" || binary == "" {
		t.Skip("set N9E_TEST_WINDOWS_SNMP_FILE and N9E_TEST_CATEGRAF_BINARY for the read-only Windows gate")
	}
	var target struct {
		Host, Community string
		Port            int
	}
	b, err := os.ReadFile(privateFile)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &target))
	require.NotEmpty(t, target.Community)
	s, _, c, cred, _ := testStore(t)
	id := profileFixture(t, s, "Windows_SNMP_Extended")
	doc, err := readProfile(s.DB, id)
	require.NoError(t, err)
	input := deviceInput(c, cred, target.Host)
	input.Port, input.TimeoutSeconds, input.Retries = target.Port, 2, 0
	content, err := compile(input, "live-read-only", "live-read-only", target.Community, "managed", doc.TemplateConfig)
	require.NoError(t, err)
	dir := t.TempDir()
	config := `[global]
hostname = "windows-template-read-only"
interval = 3600
providers = ["local"]
print_configs = false
[log]
file_name = "stdout"
[writer_opt]
batch = 100
chan_size = 10000
[http]
enable = false
[heartbeat]
enable = false
[ibex]
enable = false
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "input.snmp"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "input.snmp", "snmp.toml"), []byte(content.Config), 0600))
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	out, runErr := exec.CommandContext(ctx, binary, "-test", "-inputs", "snmp", "-configs", dir).CombinedOutput()
	defer func() {
		if t.Failed() {
			redacted := strings.ReplaceAll(string(out), target.Community, "[REDACTED]")
			t.Logf("collector output (community redacted): %.6000s", redacted)
		}
	}()
	if ctx.Err() == nil {
		require.NoError(t, runErr)
	}
	counts := map[string]int{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.HasPrefix(fields[2], "snmp_") {
			counts[fields[2]]++
		}
	}
	for _, name := range []string{"snmp_uptime", "snmp_cpu_load", "snmp_storage_size", "snmp_storage_used", "snmp_process_cpu_time", "snmp_process_memory_kib", "snmp_route_route_type", "snmp_traffic32_in_octets", "snmp_interface_info_type"} {
		require.Positive(t, counts[name], fmt.Sprintf("live metric missing: %s", name))
	}
	require.Zero(t, counts["snmp_tcp_state"], "connection details are disabled by default")
	t.Logf("live Windows metrics: %d names; CPU=%d storage=%d process CPU=%d routes=%d; no TSDB writes", len(counts), counts["snmp_cpu_load"], counts["snmp_storage_size"], counts["snmp_process_cpu_time"], counts["snmp_route_route_type"])
}

func TestWindowsExtendedPackagePublishesTypedOptionalCollectionAndPinnedResources(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	legacy := profileFixture(t, s, "Windows_SNMP")
	id := profileFixture(t, s, "Windows_SNMP_Extended")
	doc, err := readProfile(s.DB, id)
	require.NoError(t, err)
	require.NoError(t, validatePublishedResources(doc))
	require.Len(t, doc.Profile.Metrics, 48)
	require.Len(t, doc.Profile.Dashboards, 1)
	require.Len(t, doc.Profile.Alerts, 4)
	cfg := doc.TemplateConfig
	require.ElementsMatch(t, []string{"tcp", "tcp6", "listener"}, cfg.DisabledTables)
	require.ElementsMatch(t, []string{"snmp_uptime", "snmp_cpu_load", "snmp_storage_size", "snmp_storage_used"}, cfg.RequiredMetrics)
	view, err := s.Catalog()
	require.NoError(t, err)
	var entry CatalogEntry
	for _, e := range view.Entries {
		if e.ComponentID == id {
			entry = e
		}
	}
	require.NotNil(t, entry.Published)
	require.Equal(t, "Windows SNMP 增强", entry.Name)
	path, err := categoryPath(s.DB, entry.CategoryID)
	require.NoError(t, err)
	require.Equal(t, []string{"操作系统", "微软", "Windows 通用"}, path)
	input := deviceInput(c, cred, "192.0.2.52")
	input.TemplateVersionID = entry.PublishedVersionID
	content, err := compile(input, "scope-device", "scope-revision", "synthetic-community", "managed", cfg)
	require.NoError(t, err)
	var compiled snmpConfig
	require.NoError(t, toml.Unmarshal([]byte(content.Config), &compiled))
	names := []string{}
	macConversion := ""
	for _, table := range compiled.Instances[0].Tables {
		names = append(names, table.Name)
		for _, f := range table.Fields {
			if f.Name == "mac" {
				macConversion = f.Conversion
			}
		}
	}
	require.Contains(t, names, "process")
	require.Contains(t, names, "route")
	require.NotContains(t, names, "tcp")
	require.NotContains(t, names, "tcp6")
	require.NotContains(t, names, "listener")
	require.Equal(t, "hwaddr", macConversion)
	cfg.DisabledTables = nil
	content, err = compile(input, "scope-device", "scope-revision", "synthetic-community", "managed", cfg)
	require.NoError(t, err)
	require.Contains(t, content.Config, "name = 'tcp'")
	device, err := s.SaveDevice(scope, "", "windows-extended-device", input)
	require.NoError(t, err)
	profile, err := s.DeviceProfile(device)
	require.NoError(t, err)
	require.Len(t, profile.Rules, 4)
	require.Len(t, profile.Boards, 1)
	for _, metric := range profile.Metrics {
		require.Contains(t, metric.Expression, device.ID)
	}
	// Source bootstrap cannot replace an author's draft or an existing device's release.
	draft := entry.Draft
	draft.Config += "\n# author maintained\n"
	require.NoError(t, s.SaveCatalogDraft(scope, id, CatalogDraftInput{Definition: draft, RecordVersion: entry.RecordVersion}, false))
	require.NoError(t, s.EnsureCatalog())
	current, err := s.CatalogEntry(id)
	require.NoError(t, err)
	require.Contains(t, current.Draft.Config, "author maintained")
	require.Equal(t, entry.PublishedVersionID, current.PublishedVersionID)
	var original models.BuiltinComponent
	require.NoError(t, s.DB.First(&original, "id = ?", legacy).Error)
	require.Equal(t, "Windows_SNMP", original.Ident)
	require.NotContains(t, original.Readme, "Windows SNMP 增强")
}

func TestSNMPOptionalTablesAndMACConversionsCannotHideRequiredMetricsOrOverrideNames(t *testing.T) {
	s, _, _, _, _ := testStore(t)
	id := profileFixture(t, s, "Windows_SNMP_Extended")
	doc, err := readProfile(s.DB, id)
	require.NoError(t, err)
	for _, disabled := range [][]string{{"cpu"}, {"missing"}, {"tcp", "tcp"}} {
		cfg := doc.TemplateConfig
		cfg.DisabledTables = disabled
		text, err := toml.Marshal(cfg)
		require.NoError(t, err)
		_, err = parseTemplate(string(text))
		require.Error(t, err)
	}
	cfg := doc.TemplateConfig
	cfg.DisabledTables = []string{"process", "route", "tcp", "tcp6", "listener"}
	text, err := toml.Marshal(cfg)
	require.NoError(t, err)
	_, err = parseTemplate(string(text))
	require.NoError(t, err)
	invalid := strings.Replace(string(text), "conversion = 'hwaddr'", "conversion = 'arbitrary'", 1)
	_, err = parseTemplate(invalid)
	require.Error(t, err)
	invalid = string(text) + "\n[[table]]\nname='identity'\n[[table.field]]\nname='device_name'\noid='.1.3.6.1.2.1.1.5.0'\nis_tag=true\n"
	_, err = parseTemplate(invalid)
	require.Error(t, err)
}
