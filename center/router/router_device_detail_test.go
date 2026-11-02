package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/memsto"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/collection"
	prompkg "github.com/ccfos/nightingale/v6/pkg/prom"
	"github.com/ccfos/nightingale/v6/prom"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"
)

func detailFixture(t *testing.T) (*gin.Engine, *Router, models.MonitorDevice) {
	t.Helper()
	r, rt, collector, _ := collectionRouterFixture(t)
	require.NoError(t, rt.Ctx.DB.AutoMigrate(&models.AlertCurEvent{}, &models.AlertRule{}))
	for path, handler := range map[string]gin.HandlerFunc{"device-detail": rt.targetDeviceDetail, "device-alerts": rt.targetDeviceAlerts, "device-metrics": rt.targetDeviceMetrics} {
		r.GET("/api/n9e/targets/"+path, rt.auth(), rt.user(), handler)
	}
	scope := collection.Scope{Admin: true, ActorID: 1}
	credential, err := rt.Collection.SaveCredential(scope, "", collection.CredentialInput{Name: "test", GroupID: 1, Community: "private-fixture-community"})
	require.NoError(t, err)
	d, err := rt.Collection.SaveDevice(scope, "", "details-device", collection.DeviceInput{Name: "SNMP device", Address: "192.0.2.10", GroupID: 1, CollectorID: collector.ID, CredentialID: credential.ID, Enabled: true})
	require.NoError(t, err)
	for _, target := range []models.Target{{Ident: "native-agent", HostIp: d.Address, OS: "linux"}, {Ident: "hidden-agent", HostIp: d.Address, OS: "linux"}} {
		require.NoError(t, rt.Ctx.DB.Create(&target).Error)
		group := int64(1)
		if target.Ident == "hidden-agent" {
			group = 2
		}
		require.NoError(t, rt.Ctx.DB.Create(&models.TargetBusiGroup{TargetIdent: target.Ident, GroupId: group}).Error)
	}
	rt.Center.EventHistoryGroupView = true
	return r, rt, d
}

func TestDeviceDetailHasSharedMetadataAndScopedReads(t *testing.T) {
	r, rt, d := detailFixture(t)
	for _, path := range []string{"device-detail", "device-alerts", "device-metrics"} {
		endpoint := "/api/n9e/targets/" + path
		require.Equal(t, 401, collectionCall(r, "GET", endpoint+"?ident=native-agent", nil, "").Code)
		require.Equal(t, 404, collectionCall(r, "GET", endpoint+"?ident=hidden-agent", nil, "collection-reader").Code)
		require.Equal(t, 404, collectionCall(r, "GET", endpoint+"?ident=native-agent", nil, "collection-outsider").Code)
		require.Equal(t, 403, collectionCall(r, "GET", endpoint+"?device_id="+d.ID, nil, "collection-outsider").Code)
	}
	for _, identity := range []string{"device_id=" + d.ID, "ident=native-agent"} {
		w := collectionCall(r, "GET", "/api/n9e/targets/device-detail?"+identity, nil, "collection-reader")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "private-fixture-community")
		var response struct{ Dat deviceDetailView }
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, "操作系统", response.Dat.Target.Inventory.DeviceType)
		require.Len(t, response.Dat.Metrics, 5)
		require.NotNil(t, response.Dat.Rules)
		if identity == "ident=native-agent" {
			require.Nil(t, response.Dat.Device)
			require.Equal(t, "agent", response.Dat.Target.Inventory.Method)
			require.Equal(t, "Linux", response.Dat.Target.Inventory.DeviceModel)
		} else {
			require.Equal(t, d.ID, response.Dat.Device.ID)
			require.Equal(t, "Windows", response.Dat.Target.Inventory.DeviceModel)
			require.Equal(t, "fixture", response.Dat.CollectorName)
		}
	}
	// Collection read permission is required even when looking up a managed ident.
	require.NoError(t, rt.Ctx.DB.Where("role_name = ?", "CollectionReader").Delete(&models.RoleOperation{}).Error)
	require.Equal(t, 403, collectionCall(r, "GET", "/api/n9e/targets/device-detail?ident="+url.QueryEscape(d.Ident), nil, "collection-reader").Code)
	require.Equal(t, 200, collectionCall(r, "GET", "/api/n9e/targets/device-detail?ident=native-agent", nil, "collection-reader").Code)
}

func TestDeviceAlertsAreExactPagedAndMatchInventoryCounts(t *testing.T) {
	r, rt, d := detailFixture(t)
	require.NoError(t, rt.Ctx.DB.Model(&models.MonitorDevice{}).Where("id = ?", d.ID).Update("profile_state", `{"rules":[{"id":80}]}`).Error)
	for _, event := range []models.AlertCurEvent{
		{Id: 1, TargetIdent: d.Ident, RuleId: 10, GroupId: 1, DatasourceId: 1, Severity: 1, RuleName: "device CPU", TriggerValue: "91"},
		{Id: 2, TargetIdent: "", RuleId: 80, GroupId: 1, DatasourceId: 1, Severity: 2, RuleName: "owned absent"},
		{Id: 3, TargetIdent: "native-agent", RuleId: 11, GroupId: 1, DatasourceId: 1, Severity: 2, RuleName: "same IP different ident"},
		{Id: 4, TargetIdent: d.Ident + "-other", RuleId: 10, GroupId: 1, DatasourceId: 1, Severity: 1},
		{Id: 5, TargetIdent: "", RuleId: 81, GroupId: 1, DatasourceId: 1, Severity: 1},
		{Id: 6, TargetIdent: d.Ident, RuleId: 12, GroupId: 2, DatasourceId: 1, Severity: 1},
		{Id: 7, TargetIdent: d.Ident, RuleId: 13, GroupId: 1, DatasourceId: 2, Severity: 1},
	} {
		require.NoError(t, rt.Ctx.DB.Create(&event).Error)
	}
	rt.DatasourceCache = &memsto.DatasourceCacheType{DatasourceFilter: func(sources []*models.Datasource, _ *models.User) []*models.Datasource {
		out := []*models.Datasource{}
		for _, ds := range sources {
			if ds.Id == 1 {
				out = append(out, ds)
			}
		}
		return out
	}}
	for page := 1; page <= 3; page++ {
		endpoint := "/api/n9e/targets/device-alerts?device_id=" + d.ID + "&limit=1&p=" + strconv.Itoa(page)
		w := collectionCall(r, "GET", endpoint, nil, "collection-reader")
		require.Equal(t, 200, w.Code, w.Body.String())
		var response struct {
			Dat struct {
				List  []struct{ ID int64 }
				Total int
			}
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, 2, response.Dat.Total)
		if page <= 2 {
			require.Len(t, response.Dat.List, 1)
			require.Equal(t, int64(page), response.Dat.List[0].ID)
		} else {
			require.Empty(t, response.Dat.List)
		}
		require.NotContains(t, w.Body.String(), "notify_channels")
	}
	w := collectionCall(r, "GET", "/api/n9e/targets/device-detail?device_id="+d.ID, nil, "collection-reader")
	require.Equal(t, 200, w.Code, w.Body.String())
	var detail struct{ Dat deviceDetailView }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	require.Equal(t, &models.TargetAlertCounts{Critical: 1, Warning: 1}, detail.Dat.Target.Inventory.CurrentAlerts)
	w = collectionCall(r, "GET", "/api/n9e/targets/device-alerts?ident=native-agent", nil, "collection-reader")
	require.Contains(t, w.Body.String(), "same IP different ident")
	require.NotContains(t, w.Body.String(), "owned absent")
	for _, query := range []string{"p=0", "limit=101", "p=100001"} {
		require.Equal(t, 400, collectionCall(r, "GET", "/api/n9e/targets/device-alerts?ident=native-agent&"+query, nil, "collection-reader").Code)
	}
}

func TestDeviceRulesUseExplicitBindingsAndAuthorizedCurrentEvents(t *testing.T) {
	r, rt, d := detailFixture(t)
	require.NoError(t, rt.Ctx.DB.Model(&models.MonitorDevice{}).Where("id = ?", d.ID).Update("profile_state", `{"rules":[{"id":80},{"id":81}]}`).Error)
	for _, rule := range []models.AlertRule{{Id: 80, Name: "Bound template rule", GroupId: 1}, {Id: 81, Name: "Inaccessible bound rule", GroupId: 2}, {Id: 82, Name: "Linked current rule", GroupId: 1, Disabled: 1}, {Id: 83, Name: "Unrelated global rule", GroupId: 1}} {
		require.NoError(t, rt.Ctx.DB.Create(&rule).Error)
	}
	require.NoError(t, rt.Ctx.DB.Create(&models.AlertCurEvent{TargetIdent: d.Ident, RuleId: 82, GroupId: 1}).Error)
	w := collectionCall(r, "GET", "/api/n9e/targets/device-detail?device_id="+d.ID, nil, "collection-reader")
	require.Equal(t, 200, w.Code, w.Body.String())
	var detail struct{ Dat deviceDetailView }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	require.Equal(t, []deviceRuleInfo{{ID: 80, Name: "Bound template rule", Source: "template"}, {ID: 82, Name: "Linked current rule", Disabled: 1, Source: "event"}}, detail.Dat.Rules)
}

type detailProm struct {
	prompkg.API
	queries []string
}

func (p *detailProm) QueryRange(_ context.Context, query string, span prompkg.Range) (model.Value, prompkg.Warnings, error) {
	p.queries = append(p.queries, query)
	if len(p.queries) == 1 {
		return nil, nil, errors.New("query failed")
	}
	return model.Matrix{&model.SampleStream{Metric: model.Metric{"ident": "native-agent"}, Values: []model.SamplePair{{Timestamp: model.TimeFromUnixNano(span.End.UnixNano()), Value: 12.5}}}}, nil, nil
}

func TestNativeDeviceMetricsUseExactIdentAuthorizedSourcesAndRealSamples(t *testing.T) {
	r, rt, d := detailFixture(t)
	client, forbidden := &detailProm{}, &detailProm{}
	rt.PromClients = &prom.PromClientMap{ReaderClients: map[int64]prompkg.API{1: client, 2: forbidden}}
	rt.DatasourceCache = &memsto.DatasourceCacheType{DatasourceFilter: func(sources []*models.Datasource, _ *models.User) []*models.Datasource {
		return []*models.Datasource{{Id: 1}}
	}}
	rt.DatasourceCache.Set(map[int64]*models.Datasource{1: {Id: 1, PluginType: "prometheus"}, 2: {Id: 2, PluginType: "prometheus"}}, 2, time.Now().Unix())
	w := collectionCall(r, "GET", "/api/n9e/targets/device-metrics?ident=native-agent", nil, "collection-reader")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Len(t, client.queries, 5)
	require.Empty(t, forbidden.queries)
	require.Contains(t, w.Body.String(), `"12.5"`)
	require.Contains(t, w.Body.String(), `"metric_errors":`)
	for _, query := range client.queries {
		require.Contains(t, query, `ident="native-agent",collection_mode=""`)
	}
	for _, query := range nativeDeviceQueries("a\"b\\c") {
		_, err := parser.ParseExpr(query)
		require.NoError(t, err, query)
	}
	require.Equal(t, 400, collectionCall(r, "GET", "/api/n9e/targets/device-metrics?device_id="+d.ID, nil, "collection-reader").Code)
}
