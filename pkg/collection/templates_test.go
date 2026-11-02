package collection

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

const fixtureTemplate = `required_metrics = ["snmp_system_services"]
[[field]]
name = "system_services"
oid = ".1.3.6.1.2.1.1.7.0"
`

func templateFixture(t *testing.T, s *Store) {
	t.Helper()
	if s.DB.Dialector.Name() == "postgres" {
		require.NoError(t, s.DB.AutoMigrate(&models.PostgresBuiltinComponent{}))
	} else {
		require.NoError(t, s.DB.AutoMigrate(&models.BuiltinComponent{}))
	}
	require.NoError(t, s.DB.Create(&models.BuiltinComponent{Ident: "SNMP", Readme: "fixture"}).Error)
}
func verifyTemplateVersions(t *testing.T, s *Store, scope Scope, c models.MonitorCollector, cred models.MonitorCredential, secret string) {
	t.Helper()
	templateFixture(t, s)
	v1, err := s.SaveTemplate(scope, TemplateInput{Name: "Generic SNMP", Component: "SNMP", Config: fixtureTemplate})
	require.NoError(t, err)
	f := deviceInput(c, cred, "192.0.2.77")
	f.TemplateVersionID = v1.ID
	d, err := s.SaveDevice(scope, "", "template-version-device", f)
	require.NoError(t, err)
	before, _, err := s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	for _, content := range before.Configs["snmp"] {
		if !strings.Contains(content.Config, d.ID) {
			continue
		}
		require.Contains(t, content.Config, `name = 'system_services'`)
		require.NotContains(t, content.Config, `name = 'cpu'`)
		require.Contains(t, content.Config, `collection_template_version = '`+v1.ID+`'`)
	}
	var p models.MonitorPublication
	require.NoError(t, s.DB.First(&p, "version = ?", before.Version).Error)
	input := TemplateInput{Name: v1.Name, Component: v1.Component, Config: fixtureTemplate + "\n[[field]]\nname = \"object_id\"\noid = \".1.3.6.1.2.1.1.2.0\"\n", BaseVersionID: v1.ID}
	v2, err := s.SaveTemplate(scope, input)
	require.NoError(t, err)
	require.Equal(t, 2, v2.Version)
	require.Equal(t, v1.TemplateKey, v2.TemplateKey)
	_, err = s.SaveTemplate(scope, input)
	require.Error(t, err)
	require.Equal(t, 409, err.(*Error).Code)
	after, _, err := s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	require.Equal(t, before, after)
	f = inputFromDevice(d)
	f.TemplateVersionID = v2.ID
	switched, err := s.SaveDevice(scope, d.ID, "", f)
	require.NoError(t, err)
	require.NotEqual(t, d.DesiredRevision, switched.DesiredRevision)
	snap, _, err := s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	for _, content := range snap.Configs["snmp"] {
		if !strings.Contains(content.Config, d.ID) {
			continue
		}
		require.Contains(t, content.Config, `name = 'object_id'`)
	}
	require.NoError(t, s.Rollback(scope, p.ID))
	restored, err := s.Device(scope, d.ID)
	require.NoError(t, err)
	require.Equal(t, v1.ID, restored.TemplateVersionID)
	snap, _, err = s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	for _, content := range snap.Configs["snmp"] {
		if !strings.Contains(content.Config, d.ID) {
			continue
		}
		require.NotContains(t, content.Config, `name = 'object_id'`)
	}
	require.NoError(t, s.Migrate())
	views, err := s.Templates()
	require.NoError(t, err)
	require.Len(t, views, 3)
}
func TestEditableTemplateVersionsDrivePublicationAndRollback(t *testing.T) {
	s, scope, c, cred, secret := testStore(t)
	verifyTemplateVersions(t, s, scope, c, cred, secret)
}
func TestTemplateRejectsUnsafeConfigurationAndUnauthorizedWriters(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	templateFixture(t, s)
	_, err := s.SaveTemplate(Scope{ActorID: 2, WriteGroups: []int64{1}}, TemplateInput{Name: "template", Component: "SNMP", Config: fixtureTemplate})
	require.Error(t, err)
	require.Equal(t, 403, err.(*Error).Code)
	cases := []string{
		fixtureTemplate + "\ncommunity=\"secret\"", // also rejected inside a field
		"agents=[\"udp://192.0.2.1:161\"]\n" + fixtureTemplate,
		"[[instances]]\ncommunity=\"secret\"",
		"[[field]]\nname=\"up\"\noid=\".1.3.6\"",
		"[[field]]\nname=\"bad\"\noid=\"IF-MIB::ifDescr\"",
		"[[field]]\nname=\"bad\"\noid=\".1.99.3\"",
		"[[field]]\nname=\"bad\"\noid=\".1.3.4294967296\"",
		fixtureTemplate + "\n[[field]]\nname=\"system_services\"\noid=\".1.3.6\"",
		"required_metrics=[\"snmp_missing\"]\n" + fixtureTemplate[len("required_metrics = [\"snmp_system_services\"]\n"):],
		"[[table]]\nname=\"bad\"\n[[table.field]]\nname=\"ident\"\noid=\".1.3.6\"\nis_tag=true",
	}
	for _, config := range cases {
		_, err := s.SaveTemplate(scope, TemplateInput{Name: "unsafe", Component: "SNMP", Config: config})
		require.Error(t, err, config)
	}
	_, err = s.SaveTemplate(scope, TemplateInput{Name: "bad component", Component: "missing", Config: fixtureTemplate})
	require.Error(t, err)
	f := deviceInput(c, cred, "192.0.2.1")
	f.TemplateVersionID = "missing"
	_, err = s.SaveDevice(scope, "", "unknown-template", f)
	require.Error(t, err)
	require.Equal(t, 400, err.(*Error).Code)
	require.Equal(t, "unknown immutable template version", err.Error())
	cfg, err := s.TemplateConfig(WindowsTemplate)
	require.NoError(t, err)
	b, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.Contains(t, string(b), "snmp_cpu_load")
	// The built-in definition remains editable by copying or publishing a new version.
	var builtin models.MonitorTemplateVersion
	require.NoError(t, s.DB.First(&builtin, "id = ?", WindowsTemplate).Error)
	view, err := templateView(builtin)
	require.NoError(t, err)
	_, err = parseTemplate(view.Config)
	require.NoError(t, err)
}
func TestGenericTemplateHealthAndQueriesUseItsMetrics(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	templateFixture(t, s)
	tpl, err := s.SaveTemplate(scope, TemplateInput{Name: "Generic", Component: "SNMP", Config: fixtureTemplate})
	require.NoError(t, err)
	f := deviceInput(c, cred, "192.0.2.88")
	f.TemplateVersionID = tpl.ID
	d, err := s.SaveDevice(scope, "", "generic-health-device", f)
	require.NoError(t, err)
	now := time.Now()
	matrix := model.Matrix{}
	for _, name := range []string{"snmp_uptime", "snmp_system_services"} {
		matrix = append(matrix, &model.SampleStream{Metric: model.Metric{"__name__": model.LabelValue(name), "collection_device_id": model.LabelValue(d.ID), "collection_collector_id": model.LabelValue(c.ID), "collection_revision": model.LabelValue(d.DesiredRevision), "collection_mode": "managed"}, Values: []model.SamplePair{{Timestamp: model.Time(now.UnixMilli()), Value: 1}}})
	}
	svc := StatusService{Store: s, Query: func(_ context.Context, _ int64, query string, _ time.Time) (model.Value, error) {
		require.Contains(t, query, "snmp_system_services")
		require.NotContains(t, query, "cpu_load")
		return matrix, nil
	}}
	states, err := svc.States(context.Background(), []models.MonitorDevice{d})
	require.NoError(t, err)
	require.Equal(t, "healthy", states[d.ID].Health)
	require.Empty(t, states[d.ID].MissingMetrics)
	require.Nil(t, states[d.ID].CPUPercent)
	cfg, err := s.TemplateConfig(tpl.ID)
	require.NoError(t, err)
	queries := MetricQueries(cfg, `collection_device_id="device"`)
	require.Len(t, queries, 2)
	require.Contains(t, queries["snmp_system_services"], `collection_device_id="device"`)
	require.NotContains(t, queries, "cpu")
}

func TestStorageTemplateWithOnlySizeKeepsRawMetric(t *testing.T) {
	cfg, err := parseTemplate(`[[table]]
name="storage"
[[table.field]]
name="type"
oid=".1.3.6.1.2.1.25.2.3.1.2"
is_tag=true
[[table.field]]
name="size"
oid=".1.3.6.1.2.1.25.2.3.1.5"
`)
	require.NoError(t, err)
	queries := MetricQueries(cfg, `collection_device_id="fixture"`)
	require.NotContains(t, queries, "memory")
	require.NotContains(t, queries, "disk")
	require.Contains(t, queries, "snmp_storage_size")
}
