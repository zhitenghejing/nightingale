package collection

import (
	"encoding/json"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/require"
)

func mysqlPackageFixture(t *testing.T, s *Store, scope Scope) CatalogEntry {
	t.Helper()
	e := catalogFixture(t, s)
	d := e.Draft
	d.Adapter = "mysql"
	d.Config = ""
	d.Notification = "events_only"
	d.Fields = []DeviceField{{Key: "port", Label: "MySQL 端口", Type: "number", Required: true, Default: 3306}, {Key: "username", Label: "监控账号", Type: "text", Required: true}, {Key: "password", Label: "监控密码", Type: "password", Secret: true, Required: true}, {Key: "tls", Label: "连接加密", Type: "select", Default: "off", Required: true, Options: []string{"off", "verify"}}}
	d.Pack.Readme = "MySQL remote package; typed Categraf input, per-device credentials and independent native resources."
	d.Pack.Metrics = []models.BuiltinMetric{}
	for _, name := range mysqlMetrics {
		d.Pack.Metrics = append(d.Pack.Metrics, models.BuiltinMetric{Name: name, Expression: name})
	}
	var dashboard map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(d.Pack.Dashboards[0].Content), &dashboard))
	panels := dashboard["configs"].(map[string]interface{})["panels"].([]interface{})
	for i, panel := range panels {
		p := panel.(map[string]interface{})
		p["name"] = mysqlMetrics[i%len(mysqlMetrics)]
		p["targets"] = []interface{}{map[string]interface{}{"expr": mysqlMetrics[i%len(mysqlMetrics)], "refId": "A", "legend": "MySQL"}}
	}
	d.Pack.Dashboards[0].Content = string(canonical(dashboard))
	d.Pack.Dashboards[0].Name = "MySQL overview"
	var alert map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(d.Pack.Alerts[0].Content), &alert))
	var replace func(interface{})
	replace = func(value interface{}) {
		switch v := value.(type) {
		case map[string]interface{}:
			for key, child := range v {
				if key == "prom_ql" || key == "expr" {
					v[key] = "mysql_up == 0"
				} else {
					replace(child)
				}
			}
		case []interface{}:
			for _, child := range v {
				replace(child)
			}
		}
	}
	replace(alert)
	d.Pack.Alerts = d.Pack.Alerts[:1]
	d.Pack.Alerts[0].Content = string(canonical(alert))
	d.Pack.Alerts[0].Name = "MySQL unavailable"
	require.NoError(t, s.SaveCatalogDraft(scope, e.ComponentID, CatalogDraftInput{Definition: d, RecordVersion: e.RecordVersion}, true))
	view, err := s.Catalog()
	require.NoError(t, err)
	return view.Entries[0]
}

func TestMySQLPackageUsesTypedParametersAndOwnPlugin(t *testing.T) {
	s, scope, c, _, token := testStore(t)
	e := mysqlPackageFixture(t, s, scope)
	f := DeviceInput{Name: "MySQL fixture", Address: "192.0.2.151", GroupID: 1, CollectorID: c.ID, TemplateVersionID: e.PublishedVersionID, Enabled: true, Parameters: map[string]interface{}{"username": "monitor", "password": "fixture-secret@/\"", "tls": "off"}}
	d, err := s.SaveDevice(scope, "", "typed-mysql-fixture", f)
	require.NoError(t, err)
	require.Equal(t, 3306, d.Port)
	require.Equal(t, "mysql", d.Protocol)
	require.NotContains(t, string(d.Parameters), "fixture-secret")
	snapshot, _, err := s.Provider(c.ID, token, "")
	require.NoError(t, err)
	require.Empty(t, snapshot.Configs["snmp"])
	require.Len(t, snapshot.Configs["mysql"], 1)
	require.Len(t, snapshot.Configs["ping"], 1)
	accepted, err := s.FilterSamples(c.WriterUser, []prompb.TimeSeries{metric(d, "ping_result_code", 0, map[string]string{"target": d.Address})})
	require.NoError(t, err)
	require.Len(t, accepted, 1)
	for _, content := range snapshot.Configs["mysql"] {
		require.Contains(t, content.Config, "collection_device_id")
		require.Contains(t, content.Config, "192.0.2.151:3306")
		require.NotContains(t, content.Config, "community")
	}
	profile, err := s.DeviceProfile(d)
	require.NoError(t, err)
	require.Len(t, profile.Boards, 1)
	require.Len(t, profile.Rules, 1)
	require.True(t, profile.SecretFields["password"])
}

func TestNativeMySQLPackageBootstrapsWholeBundleOnce(t *testing.T) {
	s, scope, _, _, _ := testStore(t)
	id := profileFixture(t, s, "MySQL_84_Remote")
	e, err := s.CatalogEntry(id)
	require.NoError(t, err)
	require.NotNil(t, e.Published)
	require.Equal(t, "mysql", e.Draft.Adapter)
	require.Equal(t, []string{"数据库", "甲骨文", "MySQL 8.4 LTS"}, e.Published.CategoryPath)
	require.Equal(t, TypeBrandModelTaxonomy, e.Published.Taxonomy)
	require.Len(t, e.Draft.Pack.Alerts, 3)
	require.Len(t, e.Draft.Pack.Dashboards, 1)
	require.Len(t, e.Draft.Pack.Metrics, 7)
	e.Draft.Pack.Readme = "edited database instructions"
	require.NoError(t, s.SaveCatalogDraft(scope, id, CatalogDraftInput{Definition: e.Draft, RecordVersion: e.RecordVersion}, false))
	again, err := s.CatalogEntry(id)
	require.NoError(t, err)
	require.Equal(t, "edited database instructions", again.Draft.Pack.Readme)
	require.Equal(t, e.PublishedVersionID, again.PublishedVersionID)
}
