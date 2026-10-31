package collection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"
)

func profileFixture(t *testing.T, s *Store, name string) uint64 {
	t.Helper()
	require.NoError(t, s.DB.AutoMigrate(&models.BuiltinMetric{}, &models.Board{}, &models.BoardPayload{}, &models.BoardBusigroup{}, &models.AlertRule{}))
	if s.DB.Dialector.Name() == "postgres" {
		require.NoError(t, s.DB.AutoMigrate(&models.PostgresBuiltinComponent{}))
		require.NoError(t, s.DB.AutoMigrate(&models.PostgresBuiltinPayload{}))
	} else {
		require.NoError(t, s.DB.AutoMigrate(&models.BuiltinComponent{}))
		require.NoError(t, s.DB.AutoMigrate(&models.BuiltinPayload{}))
	}
	ctx := &nctx.Context{DB: s.DB, IsCenter: true}
	root := filepath.Join("..", "..", "integrations", name)
	readme, err := os.ReadFile(filepath.Join(root, "markdown", "README.md"))
	require.NoError(t, err)
	c := models.BuiltinComponent{Ident: name, Readme: string(readme)}
	if s.DB.Dialector.Name() == "sqlite" {
		var maximum uint64
		require.NoError(t, s.DB.Model(&models.BuiltinComponent{}).Select("coalesce(max(id),0)").Scan(&maximum).Error)
		c.ID = maximum + 1
	}
	require.NoError(t, c.Add(ctx, "fixture"))
	for _, typ := range []string{"dashboard", "alert"} {
		files, err := filepath.Glob(filepath.Join(root, typ+"s", "*.json"))
		require.NoError(t, err)
		for _, file := range files {
			b, err := os.ReadFile(file)
			require.NoError(t, err)
			rows := []json.RawMessage{b}
			if typ == "alert" {
				require.NoError(t, json.Unmarshal(b, &rows))
			}
			for _, row := range rows {
				var meta struct {
					Name string
					UUID int64
				}
				require.NoError(t, json.Unmarshal(row, &meta))
				p := models.BuiltinPayload{ComponentID: c.ID, Type: typ, Name: meta.Name, UUID: meta.UUID, Content: string(row)}
				require.NoError(t, p.Add(ctx, "fixture"))
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(root, "metrics", "categraf.json"))
	require.NoError(t, err)
	var metrics []models.BuiltinMetric
	require.NoError(t, json.Unmarshal(b, &metrics))
	for _, m := range metrics {
		require.NoError(t, m.Add(ctx, "fixture"))
	}
	return c.ID
}

func TestNativeCollectionProfiles(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	windows := profileFixture(t, s, "Windows_SNMP")
	linux := profileFixture(t, s, "Linux_SNMP")
	options, err := s.Profiles()
	require.NoError(t, err)
	require.Len(t, options, 2)
	input := deviceInput(c, cred, "192.0.2.20")
	input.TemplateComponentID = windows
	d, err := s.SaveDevice(scope, "", "profile-application", input)
	require.NoError(t, err)
	p, err := s.DeviceProfile(d)
	require.NoError(t, err)
	require.Equal(t, windows, p.ComponentID)
	require.Len(t, p.Boards, 1)
	require.Len(t, p.Rules, 7)
	require.Len(t, p.Metrics, 11)
	require.Contains(t, p.Metrics[0].Expression, d.ID)
	require.Contains(t, p.Metrics[0].Expression, d.DesiredRevision)
	var rules []models.AlertRule
	require.NoError(t, s.DB.Order("id").Find(&rules).Error)
	for i, r := range rules {
		require.Zero(t, r.Disabled)
		require.Empty(t, r.NotifyRuleIds)
		require.Len(t, r.DatasourceQueries, 1)
		require.Contains(t, r.RuleConfig, d.DesiredRevision)
		require.Contains(t, r.RuleConfig, d.ID)
		if i < 3 {
			require.Equal(t, 300, r.PromForDuration)
			require.Contains(t, r.RuleConfig, `\u003e 90`)
		} else if i == 3 {
			require.Equal(t, 60, r.PromForDuration)
			require.Contains(t, r.RuleConfig, "absent_over_time")
		} else {
			require.Equal(t, 300, r.PromForDuration)
		}
	}
	// Device-specific native edits survive an ordinary save and a new revision.
	first := rules[0]
	first.RuleConfig = strings.ReplaceAll(first.RuleConfig, `\u003e 90`, `\u003e 95`)
	require.NoError(t, s.DB.Model(&first).Update("rule_config", first.RuleConfig).Error)
	cfg := inputFromDevice(d)
	cfg.TemplateComponentID = windows
	cfg.IntervalSeconds = 60
	d, err = s.SaveDevice(scope, d.ID, "", cfg)
	require.NoError(t, err)
	require.NoError(t, s.DB.First(&first, "id = ?", first.Id).Error)
	require.Contains(t, first.RuleConfig, `\u003e 95`)
	require.Contains(t, first.RuleConfig, d.DesiredRevision)
	// A public edit does not silently replace a saved device snapshot.
	require.NoError(t, s.DB.Model(&models.BuiltinComponent{}).Where("id = ?", windows).Update("readme", "broken public definition").Error)
	cfg = inputFromDevice(d)
	cfg.TemplateComponentID = windows
	d, err = s.SaveDevice(scope, d.ID, "", cfg)
	require.NoError(t, err)
	// Stop/enable restore even independently disabled rules.
	require.NoError(t, s.DB.Model(&models.AlertRule{}).Where("id = ?", rules[1].Id).Update("disabled", 1).Error)
	d, err = s.SetDeviceState(scope, d.ID, d.RecordVersion, false, false)
	require.NoError(t, err)
	require.NoError(t, s.DB.Find(&rules).Error)
	for _, r := range rules {
		require.Equal(t, 1, r.Disabled)
	}
	d, err = s.SetDeviceState(scope, d.ID, d.RecordVersion, true, false)
	require.NoError(t, err)
	require.NoError(t, s.DB.Order("id").Find(&rules).Error)
	require.Zero(t, rules[0].Disabled)
	require.Equal(t, 1, rules[1].Disabled)
	// A component switch is explicit, installs all four parts and retires old rules.
	cfg = inputFromDevice(d)
	cfg.TemplateComponentID = linux
	d, err = s.SaveDevice(scope, d.ID, "", cfg)
	require.NoError(t, err)
	p, err = s.DeviceProfile(d)
	require.NoError(t, err)
	require.Equal(t, linux, p.ComponentID)
	require.NoError(t, s.DB.Where("id = ?", first.Id).First(&first).Error)
	require.Equal(t, 1, first.Disabled)
	// Per-device collection and metric edits create an immutable snapshot only.
	config := p.Config + "\n[[field]]\nname = \"users\"\noid = \".1.3.6.1.2.1.25.1.5.0\"\n"
	metrics := p.Metrics[:1]
	metrics[0].Name = "Device CPU"
	cfg = inputFromDevice(d)
	cfg.TemplateComponentID = linux
	cfg.Customization = &ProfileCustomization{Config: &config, Metrics: &metrics}
	d, err = s.SaveDevice(scope, d.ID, "", cfg)
	require.NoError(t, err)
	p, err = s.DeviceProfile(d)
	require.NoError(t, err)
	require.True(t, p.Customized)
	require.Contains(t, p.Config, "users")
	require.Len(t, p.Metrics, 1)
	unchanged, err := readProfile(s.DB, linux)
	require.NoError(t, err)
	require.Len(t, unchanged.Profile.Metrics, 6)
	require.NotContains(t, unchanged.MetricNames(), "snmp_users")
	_, err = s.SaveDevice(Scope{ActorID: 2, ReadGroups: []int64{1}}, d.ID, "", inputFromDevice(d))
	require.Error(t, err)
	d, err = s.SetDeviceState(scope, d.ID, d.RecordVersion, false, true)
	require.NoError(t, err)
	for _, ref := range p.Rules {
		var rule models.AlertRule
		require.NoError(t, s.DB.First(&rule, "id = ?", ref.ID).Error)
		require.Equal(t, 1, rule.Disabled)
	}
}

func TestProfileScopingAndAtomicFailure(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	component := profileFixture(t, s, "Linux_SNMP")
	input := deviceInput(c, cred, "192.0.2.25")
	input.TemplateComponentID = component
	require.NoError(t, s.DB.Model(&models.BuiltinPayload{}).Where("component_id = ? AND type = ?", component, "alert").Update("content", `{"name":"broken","rule_config":{"queries":[{"prom_ql":"bad(]"}]}}`).Error)
	_, err := s.SaveDevice(scope, "", "profile-atomic-failure", input)
	require.Error(t, err)
	var devices, boards, rules int64
	require.NoError(t, s.DB.Model(&models.MonitorDevice{}).Count(&devices).Error)
	require.NoError(t, s.DB.Model(&models.Board{}).Count(&boards).Error)
	require.NoError(t, s.DB.Model(&models.AlertRule{}).Count(&rules).Error)
	require.Zero(t, devices)
	require.Zero(t, boards)
	require.Zero(t, rules)
	d := models.MonitorDevice{ID: "device", CollectorID: "collector", DesiredRevision: "revision"}
	expr, err := scopeExpression(`absent_over_time(snmp_uptime{collection_device_id="other"}[3m])`, d)
	require.NoError(t, err)
	require.Contains(t, expr, `collection_device_id="device"`)
	require.NotContains(t, expr, "other")
	_, err = parser.ParseExpr(expr)
	require.NoError(t, err)
}

func verifyNativeProfileSQL(t *testing.T, s *Store, scope Scope, c models.MonitorCollector, cred models.MonitorCredential) {
	id := profileFixture(t, s, "Windows_SNMP")
	f := deviceInput(c, cred, "198.51.100.88")
	f.TemplateComponentID = id
	d, err := s.SaveDevice(scope, "", "sql-native-profile", f)
	require.NoError(t, err)
	profile, err := s.DeviceProfile(d)
	require.NoError(t, err)
	require.Len(t, profile.Rules, 7)
	require.Len(t, profile.Boards, 1)
	before := d.DesiredRevision
	_, err = s.SaveCredential(scope, cred.ID, CredentialInput{Name: cred.Name, GroupID: cred.GroupID, Community: "rotated-fixture-secret", RecordVersion: cred.RecordVersion})
	require.NoError(t, err)
	d, err = s.Device(scope, d.ID)
	require.NoError(t, err)
	require.NotEqual(t, before, d.DesiredRevision)
	var rule models.AlertRule
	require.NoError(t, s.DB.First(&rule, "id = ?", profile.Rules[0].ID).Error)
	require.Contains(t, rule.RuleConfig, d.DesiredRevision)
	require.NotContains(t, rule.RuleConfig, before)
	_, err = s.SetDeviceState(scope, d.ID, d.RecordVersion, false, true)
	require.NoError(t, err)
	require.NoError(t, s.DB.First(&rule, "id = ?", rule.Id).Error)
	require.Equal(t, 1, rule.Disabled)
}
