package router

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/collection"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func inventoryContext(user *models.User) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/api/n9e/targets", nil)
	c.Set("user", user)
	return c
}

func TestInventoryUsesPinnedTypeAndRealCurrentAlerts(t *testing.T) {
	_, rt, _, _ := collectionRouterFixture(t)
	require.NoError(t, rt.Ctx.DB.AutoMigrate(&models.AlertCurEvent{}))
	version := models.MonitorTemplateVersion{ID: "pinned", TemplateKey: "inventory-test", Version: 1, Definition: `{"catalog":{"category_path":["操作系统","Linux","通用 SNMP"],"version":1},"component":"Linux_SNMP"}`}
	require.NoError(t, rt.Ctx.DB.Create(&version).Error)
	require.NoError(t, rt.Ctx.DB.Create(&models.MonitorTemplateVersion{ID: "original-windows", TemplateKey: "windows_basic", Version: 1, Definition: `{}`}).Error)
	require.NoError(t, rt.Ctx.DB.Create(&models.MonitorDevice{ID: "inventory-device", Name: "Linux 设备", Ident: "dev-one", Protocol: "snmp_v2c", TemplateVersionID: "pinned", ProfileState: `{"rules":[{"id":80}]}`}).Error)
	require.NoError(t, rt.Ctx.DB.Create(&models.MonitorDevice{ID: "old-device", Name: "旧版 Windows", Ident: "old-windows", Protocol: "snmp_v2c", TemplateVersionID: "original-windows", RequestKey: "old-device-request"}).Error)
	for _, event := range []models.AlertCurEvent{
		{Id: 1, TargetIdent: "dev-one", GroupId: 1, Severity: 1},
		{Id: 2, TargetIdent: "dev-one", GroupId: 1, Severity: 2},
		{Id: 3, TargetIdent: "", RuleId: 80, GroupId: 1, Severity: 1}, // absent() can lose ident, but owned rule is exact.
		{Id: 4, TargetIdent: "dev-one-other", GroupId: 1, Severity: 1},
		{Id: 5, TargetIdent: "dev-one", GroupId: 2, Severity: 1}, // not in reader's permitted business group.
	} {
		require.NoError(t, rt.Ctx.DB.Create(&event).Error)
	}
	rt.Center.EventHistoryGroupView = true
	targets := []*models.Target{{Ident: "dev-one", HostIp: "192.0.2.10", TargetUp: 2}, {Ident: "plain-agent", OS: "linux", TargetUp: 2}, {Ident: "old-windows"}}
	var reader models.User
	require.NoError(t, rt.Ctx.DB.First(&reader, "id = ?", 2).Error)
	require.NoError(t, rt.targetInventory(inventoryContext(&reader), targets))
	view := targets[0].Inventory
	require.Equal(t, "Linux 设备", view.Name)
	require.Equal(t, "操作系统", view.DeviceType)
	require.Equal(t, "snmp", view.Method)
	require.Equal(t, "Linux", view.DeviceModel)
	require.Equal(t, &models.TargetAlertCounts{Critical: 2, Warning: 1}, view.CurrentAlerts)
	require.Equal(t, "unknown", view.Reachability, "heartbeat and SNMP status must not be presented as ICMP")
	require.Equal(t, "agent", targets[1].Inventory.Method)
	require.Equal(t, &models.TargetAlertCounts{}, targets[1].Inventory.CurrentAlerts)
	require.Equal(t, "操作系统", targets[2].Inventory.DeviceType)
}

func TestInventoryICMPFreshnessAndProbeFailure(t *testing.T) {
	now := time.Unix(1800000000, 0)
	targets := []*models.Target{
		{HostIp: "192.0.2.1", Inventory: &models.TargetInventory{Reachability: "unknown"}},
		{HostIp: "192.0.2.2", Inventory: &models.TargetInventory{Reachability: "unknown"}},
		{HostIp: "192.0.2.3", Inventory: &models.TargetInventory{Reachability: "unknown"}},
		{HostIp: "192.0.2.4", Inventory: &models.TargetInventory{Reachability: "unknown"}},
	}
	series := func(ip string, value float64, at time.Time) *model.SampleStream {
		return &model.SampleStream{Metric: model.Metric{"target": model.LabelValue(ip)}, Values: []model.SamplePair{{Timestamp: model.TimeFromUnixNano(at.UnixNano()), Value: model.SampleValue(value)}}}
	}
	applyICMP(targets, model.Matrix{series("192.0.2.1", 0, now.Add(-10*time.Second)), series("192.0.2.2", 1, now.Add(-10*time.Second)), series("192.0.2.3", 2, now.Add(-10*time.Second)), series("192.0.2.4", 0, now.Add(-4*time.Minute))}, now, nil, 0)
	require.Equal(t, "up", targets[0].Inventory.Reachability)
	require.Equal(t, "down", targets[1].Inventory.Reachability)
	require.Equal(t, "error", targets[2].Inventory.Reachability)
	require.Equal(t, "unknown", targets[3].Inventory.Reachability)
	applyICMP(targets, model.Matrix{series("192.0.2.1", 1, now.Add(-10*time.Second))}, now, nil, 0)
	require.Equal(t, "conflict", targets[0].Inventory.Reachability)
	applyICMP(targets, model.Matrix{series("192.0.2.1", 0, now)}, now, nil, 0)
	require.Equal(t, "up", targets[0].Inventory.Reachability)
}

func TestInventoryCollectionMethodKeepsDatabasePluginExplicit(t *testing.T) {
	method, plugin := inventoryMethod("mysql")
	require.Equal(t, "mysql", method)
	require.Equal(t, "mysql", plugin)
	method, _ = inventoryMethod("ipmi")
	require.Equal(t, "ipmi", method)
}

func TestInventoryLeavesServiceTargetsUnchanged(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/n9e/target/list", nil)
	target := &models.Target{Ident: "service-target"}
	require.NoError(t, (&Router{}).targetInventory(c, []*models.Target{target}))
	require.Nil(t, target.Inventory)
}

func TestInventoryExplicitClassification(t *testing.T) {
	kind, model := inventoryClassification([]string{"数据库", "MySQL", "5.7"})
	require.Equal(t, "数据库", kind)
	require.Equal(t, "MySQL 5.7", model)
	kind, model = inventoryClassification([]string{"操作系统", "Windows", "Windows Server 2019"})
	require.Equal(t, "操作系统", kind)
	require.Equal(t, "Windows Server 2019", model)
}

func TestInventoryTypeBrandModelSnapshotDoesNotTreatBrandAsModel(t *testing.T) {
	_, rt, _, _ := collectionRouterFixture(t)
	require.NoError(t, rt.Ctx.DB.AutoMigrate(&models.AlertCurEvent{}))
	require.NoError(t, rt.Ctx.DB.Create(&models.MonitorTemplateVersion{ID: "brand-model", TemplateKey: "brand-model", Version: 1, Definition: `{"catalog":{"taxonomy":"type_brand_model","category_path":["服务器","戴尔","R640"],"version":1}}`}).Error)
	require.NoError(t, rt.Ctx.DB.Create(&models.MonitorDevice{ID: "brand-model-device", Ident: "dell", Protocol: "redfish", TemplateVersionID: "brand-model"}).Error)
	target := &models.Target{Ident: "dell"}
	require.NoError(t, rt.targetInventory(inventoryContext(&models.User{Id: 1, Roles: "Admin"}), []*models.Target{target}))
	require.Equal(t, "服务器", target.Inventory.DeviceType)
	require.Equal(t, "R640", target.Inventory.DeviceModel)
}

func TestManagedICMPUsesDeviceRevisionAndDatasource(t *testing.T) {
	now := time.Now()
	device := models.MonitorDevice{ID: "device", Ident: "one", Address: "192.0.2.10", CollectorID: "collector", DesiredRevision: "current", Enabled: true}
	targets := []*models.Target{{Ident: device.Ident, HostIp: device.Address, Inventory: &models.TargetInventory{Reachability: "unknown"}}}
	owners := map[string]inventoryPingOwner{device.Ident: {Device: device, DatasourceID: 1}}
	series := &model.SampleStream{Metric: model.Metric{"target": model.LabelValue(device.Address), "ident": "one", "collection_mode": "managed", "collection_device_id": "other-device", "collection_collector_id": "collector", "collection_revision": "current"}, Values: []model.SamplePair{{Timestamp: model.TimeFromUnixNano(now.UnixNano()), Value: 0}}}
	applyICMP(targets, model.Matrix{series}, now, owners, 1)
	require.Equal(t, "unknown", targets[0].Inventory.Reachability)
	series.Metric["collection_device_id"] = "device"
	applyICMP(targets, model.Matrix{series}, now, owners, 2)
	require.Equal(t, "unknown", targets[0].Inventory.Reachability)
	series.Metric["collection_revision"] = "old"
	applyICMP(targets, model.Matrix{series}, now, owners, 1)
	require.Equal(t, "unknown", targets[0].Inventory.Reachability)
	series.Metric["collection_revision"] = "current"
	applyICMP(targets, model.Matrix{series}, now, owners, 1)
	require.Equal(t, "up", targets[0].Inventory.Reachability)
}
func TestAgentCollectionNeedsActualFreshMetrics(t *testing.T) {
	now := time.Now()
	targets := []*models.Target{{Ident: "agent", TargetUp: 2, Inventory: &models.TargetInventory{}}, {Ident: "stale", Inventory: &models.TargetInventory{}}}
	applyAgentCollection(targets, model.Vector{&model.Sample{Metric: model.Metric{"ident": "agent"}, Value: model.SampleValue(now.Unix() - 10)}, &model.Sample{Metric: model.Metric{"ident": "stale"}, Value: model.SampleValue(now.Unix() - 240)}}, now, nil)
	require.True(t, targets[0].Inventory.CollectionOK)
	require.False(t, targets[1].Inventory.CollectionOK)
}

func TestNativeAgentICMPUsesCurrentAssignment(t *testing.T) {
	now := time.Now()
	owner := &collection.AgentPingOwner{Ident: "agent", Address: "192.0.2.10", CollectorID: "collector", Revision: "current", DatasourceID: 1}
	target := &models.Target{Ident: owner.Ident, HostIp: owner.Address, Inventory: &models.TargetInventory{Reachability: "unknown"}}
	targets := []*models.Target{target}
	owners := map[string]inventoryPingOwner{owner.Ident: {Agent: owner, DatasourceID: 1}}
	series := &model.SampleStream{Metric: model.Metric{"target": "192.0.2.10", "ident": "agent", "collection_mode": "agent", "collection_collector_id": "collector", "collection_revision": "current"}, Values: []model.SamplePair{{Timestamp: model.TimeFromUnixNano(now.UnixNano()), Value: 0}}}
	for _, label := range []model.LabelName{"collection_revision", "collection_collector_id", "ident", "target", "collection_mode", "collection_device_id"} {
		old := series.Metric[label]
		series.Metric[label] = "wrong"
		applyICMP(targets, model.Matrix{series}, now, owners, 1)
		require.Equal(t, "unknown", target.Inventory.Reachability, label)
		series.Metric[label] = old
	}
	applyICMP(targets, model.Matrix{series}, now, owners, 2)
	require.Equal(t, "unknown", target.Inventory.Reachability)
	applyICMP(targets, model.Matrix{series}, now, owners, 1)
	require.Equal(t, "up", target.Inventory.Reachability)
	require.False(t, target.Inventory.CollectionOK, "ICMP does not establish Agent collection health")
	applyAgentCollection(targets, model.Vector{&model.Sample{Metric: model.Metric{"ident": "agent"}, Value: model.SampleValue(now.Unix() - 10)}}, now, owners)
	require.True(t, target.Inventory.CollectionOK, "a ping assignment must not exclude the host from native Agent metrics")
}
