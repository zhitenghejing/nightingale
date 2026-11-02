package router

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ccfos/nightingale/v6/memsto"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/collection"
	prompkg "github.com/ccfos/nightingale/v6/pkg/prom"
	"github.com/ccfos/nightingale/v6/prom"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/common/model"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type collectionPermissionProm struct {
	prompkg.API
	calls int
}

func (p *collectionPermissionProm) QueryRange(context.Context, string, prompkg.Range) (model.Value, prompkg.Warnings, error) {
	p.calls++
	return model.Matrix{}, nil, nil
}

func TestCollectionReadsRespectDatasourcePermissionAfterStatusCacheWarmup(t *testing.T) {
	r, rt, d := detailFixture(t)
	allowed, statusCalls := true, 0
	rt.DatasourceCache = &memsto.DatasourceCacheType{DatasourceFilter: func(sources []*models.Datasource, _ *models.User) []*models.Datasource {
		if allowed {
			return sources
		}
		return nil
	}}
	rt.DatasourceCache.Set(map[int64]*models.Datasource{1: {Id: 1, PluginType: "prometheus"}}, 1, time.Now().Unix())
	client := &collectionPermissionProm{}
	rt.PromClients = &prom.PromClientMap{ReaderClients: map[int64]prompkg.API{1: client}}
	rt.CollectionStatus.Query = func(context.Context, int64, string, time.Time) (model.Value, error) {
		statusCalls++
		matrix := model.Matrix{}
		for name, value := range map[string]model.SampleValue{"snmp_up": 1, "snmp_uptime": 100, "snmp_cpu_load": 37} {
			matrix = append(matrix, &model.SampleStream{Metric: model.Metric{"__name__": model.LabelValue(name), "collection_device_id": model.LabelValue(d.ID), "collection_collector_id": model.LabelValue(d.CollectorID), "collection_revision": model.LabelValue(d.DesiredRevision), "collection_mode": "managed"}, Values: []model.SamplePair{{Timestamp: model.Now(), Value: value}}})
		}
		return matrix, nil
	}
	endpoint := "/api/n9e/collection/devices/" + d.ID
	warm := collectionCall(r, "GET", endpoint, nil, "collection-reader")
	require.Equal(t, 200, warm.Code, warm.Body.String())
	require.Contains(t, warm.Body.String(), `"cpu_percent":37`)
	require.Positive(t, statusCalls)
	metrics := collectionCall(r, "GET", endpoint+"/metrics", nil, "collection-reader")
	require.Equal(t, 200, metrics.Code, metrics.Body.String())
	require.Positive(t, client.calls)
	statusBefore, rangesBefore := statusCalls, client.calls
	allowed = false
	assertNoSamples := func(body []byte) {
		t.Helper()
		var response interface{}
		require.NoError(t, json.Unmarshal(body, &response))
		states := 0
		var visit func(interface{})
		visit = func(value interface{}) {
			switch value := value.(type) {
			case map[string]interface{}:
				if _, ok := value["cpu_percent"]; ok {
					states++
					require.Nil(t, value["cpu_percent"])
					require.Nil(t, value["memory_percent"])
					require.EqualValues(t, 0, value["last_sample_at"])
					require.Equal(t, "query_error", value["health"])
				}
				for _, child := range value {
					visit(child)
				}
			case []interface{}:
				for _, child := range value {
					visit(child)
				}
			}
		}
		visit(response)
		require.Positive(t, states)
	}
	for _, route := range []string{endpoint, "/api/n9e/collection/devices", "/api/n9e/targets/device-detail?device_id=" + d.ID} {
		w := collectionCall(r, "GET", route, nil, "collection-reader")
		require.Equal(t, 200, w.Code, w.Body.String())
		assertNoSamples(w.Body.Bytes())
	}
	denied := collectionCall(r, "GET", endpoint+"/metrics", nil, "collection-reader")
	require.Equal(t, 403, denied.Code, denied.Body.String())
	var reader models.User
	require.NoError(t, rt.Ctx.DB.First(&reader, "id = ?", 2).Error)
	reader.RolesLst = []string{"CollectionReader"}
	c := inventoryContext(&reader)
	target := &models.Target{Ident: d.Ident}
	require.NoError(t, rt.collectionTargets(c, []*models.Target{target}))
	state := target.Collection.(collection.State)
	require.Nil(t, state.CPUPercent)
	require.Zero(t, state.LastSampleAt)
	_, states := rt.collectionStatistics(c, nil)
	require.Nil(t, states[d.Ident].CPUPercent)
	require.Equal(t, "query_error", states[d.Ident].Health)
	update := collection.DeviceInput{Name: "renamed without datasource access", Address: d.Address, Port: d.Port, Ident: d.Ident, GroupID: d.GroupID, CollectorID: d.CollectorID, CredentialID: d.CredentialID, TemplateVersionID: d.TemplateVersionID, Enabled: true, RecordVersion: d.RecordVersion}
	saved := collectionCall(r, "PUT", endpoint, update, "collection-admin")
	require.Equal(t, 200, saved.Code, saved.Body.String())
	assertNoSamples(saved.Body.Bytes())
	require.Equal(t, statusBefore, statusCalls, "denied requests must not query or read privileged cached metrics")
	require.Equal(t, rangesBefore, client.calls)
}

func TestCollectionEnabledPreservesServiceAuthenticatedHostFilter(t *testing.T) {
	_, rt, d := detailFixture(t)
	cache := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	rt.Redis = client
	service := gin.New()
	service.Use(gin.Recovery())
	service.POST("/v1/n9e/target/list", gin.BasicAuth(gin.Accounts{"service-fixture": "synthetic-password"}), rt.targetGetsByHostFilter)
	request := func(authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/n9e/target/list", strings.NewReader(`{"queries":[],"p":1,"limit":50}`))
		req.Header.Set("Content-Type", "application/json")
		if authenticated {
			req.SetBasicAuth("service-fixture", "synthetic-password")
		}
		w := httptest.NewRecorder()
		service.ServeHTTP(w, req)
		return w
	}
	require.Equal(t, 401, request(false).Code)
	w := request(true)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), d.Ident)
	require.NotContains(t, w.Body.String(), `"cpu_percent"`)
}
