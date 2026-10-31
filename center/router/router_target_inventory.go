package router

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/collection"
	"github.com/ccfos/nightingale/v6/storage"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/common/model"
	"github.com/toolkits/pkg/logger"
	"gorm.io/gorm"
)

// Enrich only the already-authorized page, never a second unscoped device list.
// Collection health and Agent heartbeat are deliberately not ICMP availability.
func (rt *Router) targetInventory(c *gin.Context, targets []*models.Target) error {
	if len(targets) == 0 {
		return nil
	}
	// The host-filter handler is also a service entry point without a UI user.
	// Keep its native contract; this projection uses user-scoped event/datasource access.
	if _, exists := c.Get("user"); !exists {
		return nil
	}
	idents := make([]string, 0, len(targets))
	byIdent := map[string]*models.Target{}
	for _, target := range targets {
		target.Inventory = &models.TargetInventory{Name: target.Ident, Method: "agent", DeviceType: nativeInventoryType(target.OS), DeviceModel: nativeInventoryModel(target.OS), Reachability: "unknown", TemplatePath: []string{}}
		idents = append(idents, target.Ident)
		byIdent[target.Ident] = target
	}
	if rt.Redis != nil {
		keys := []string{}
		for _, target := range targets {
			keys = append(keys, models.WrapExtendIdent(target.Ident))
		}
		for i, value := range storage.MGet(c.Request.Context(), rt.Redis, keys) {
			if i >= len(targets) {
				break
			}
			var meta struct {
				Platform struct {
					Name    string `json:"os_name"`
					Version string `json:"os_version"`
				} `json:"platform"`
			}
			if json.Unmarshal(value, &meta) == nil && meta.Platform.Name != "" {
				targets[i].Inventory.DeviceModel = inventoryText(meta.Platform.Name + " " + meta.Platform.Version)
			}
		}
	}
	rules := map[int64]string{}
	owners := map[string]inventoryPingOwner{}
	if rt.Collection != nil {
		var devices []models.MonitorDevice
		if err := rt.Collection.DB.Where("ident IN ? AND deleted = ?", idents, false).Find(&devices).Error; err != nil {
			return err
		}
		versions := []string{}
		for _, device := range devices {
			versions = append(versions, device.TemplateVersionID)
		}
		var releases []models.MonitorTemplateVersion
		if len(versions) > 0 {
			if err := rt.Collection.DB.Where("id IN ?", versions).Find(&releases).Error; err != nil {
				return err
			}
		}
		paths := map[string][]string{}
		for _, release := range releases {
			var doc struct {
				Catalog   *collection.CatalogRelease `json:"catalog"`
				Component string                     `json:"component"`
			}
			if err := json.Unmarshal([]byte(release.Definition), &doc); err != nil {
				return err
			}
			if doc.Catalog != nil {
				paths[release.ID] = doc.Catalog.CategoryPath
			} else if release.TemplateKey == "windows_basic" {
				// Exact key of the original immutable WindowsTemplate seed.
				// It predates component/category metadata in the release document.
				paths[release.ID] = []string{"操作系统", "Windows"}
			} else {
				switch strings.ToLower(doc.Component) {
				case "windows", "windows_snmp":
					paths[release.ID] = []string{"操作系统", "Windows"}
				case "linux", "linux_snmp":
					paths[release.ID] = []string{"操作系统", "Linux"}
				}
			}
		}
		collectorIDs := []string{}
		for _, device := range devices {
			collectorIDs = append(collectorIDs, device.CollectorID)
		}
		var collectors []models.MonitorCollector
		if len(collectorIDs) > 0 {
			if err := rt.Collection.DB.Where("id IN ?", collectorIDs).Find(&collectors).Error; err != nil {
				return err
			}
		}
		byCollector := map[string]models.MonitorCollector{}
		for _, collector := range collectors {
			byCollector[collector.ID] = collector
		}
		for _, device := range devices {
			item := byIdent[device.Ident].Inventory
			owners[device.Ident] = inventoryPingOwner{Device: device, DatasourceID: byCollector[device.CollectorID].DatasourceID}

			item.Name = device.Name
			item.TemplatePath = append([]string{}, paths[device.TemplateVersionID]...)
			if len(item.TemplatePath) > 0 {
				item.DeviceType, item.DeviceModel = inventoryClassification(item.TemplatePath)
			}
			item.Method, item.Plugin = inventoryMethod(device.Protocol)
			item.CollectionOK = false
			if state, ok := byIdent[device.Ident].Collection.(collection.State); ok {
				item.CollectionOK = device.Enabled && state.Health == "healthy"
				item.LastSampleAt = state.LastSampleAt
			}
			var state struct {
				Rules []struct {
					ID int64 `json:"id"`
				} `json:"rules"`
			}
			if err := json.Unmarshal([]byte(device.ProfileState), &state); err == nil {
				for _, rule := range state.Rules {
					rules[rule.ID] = device.Ident
				}
			}
		}
		agentOwners, err := collection.AgentPingOwners(rt.Collection.DB, idents)
		if err != nil {
			return err
		}
		for ident, owner := range agentOwners {
			owner := owner
			owners[ident] = inventoryPingOwner{Agent: &owner, DatasourceID: owner.DatasourceID}
		}
	}
	if err := rt.targetCurrentAlerts(c, targets, rules); err != nil {
		// Basic device management remains usable on an event-store failure;
		// nil explicitly means unavailable, never a fabricated zero.
		logger.Warningf("device inventory alert counts unavailable: %v", err)
	}
	rt.targetInventorySamples(c, targets, owners)
	return nil
}

func nativeInventoryType(os string) string {
	if os != "" {
		return "操作系统"
	}
	return ""
}
func nativeInventoryModel(os string) string {
	switch strings.ToLower(os) {
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	default:
		return inventoryText(os)
	}
}
func inventoryText(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "/", " ")), " ")
}
func inventoryClassification(path []string) (string, string) {
	if len(path) == 0 {
		return "", ""
	}
	kind := inventoryText(path[0])
	model := ""
	for _, part := range path[1:] {
		part = inventoryText(part)
		if part == "" || strings.HasPrefix(part, "通用") || part == "所有版本" {
			continue
		}
		if model == "" || strings.Contains(strings.ToLower(part), strings.ToLower(model)) {
			model = part
		} else {
			model += " " + part
		}
	}
	return kind, model
}

func inventoryMethod(protocol string) (string, string) {
	switch {
	case strings.HasPrefix(protocol, "snmp"):
		return "snmp", protocol
	case strings.HasPrefix(protocol, "ipmi"):
		return "ipmi", protocol
	case protocol != "":
		return protocol, protocol
	default:
		return "agent", ""
	}
}

func (rt *Router) targetCurrentAlerts(c *gin.Context, targets []*models.Target, ruleOwners map[int64]string) error {
	byIdent := map[string]*models.Target{}
	idents := []string{}
	ruleIDs := []int64{}
	for _, target := range targets {
		byIdent[target.Ident] = target
		idents = append(idents, target.Ident)
	}
	for id := range ruleOwners {
		ruleIDs = append(ruleIDs, id)
	}
	query, err := rt.deviceEventsQuery(c, idents, ruleIDs)
	if err != nil {
		return err
	}
	query = query.Select("target_ident, rule_id, severity, count(*) as total")
	var counts []struct {
		TargetIdent string
		RuleID      int64
		Severity    int
		Total       int
	}
	if err := query.Group("target_ident, rule_id, severity").Scan(&counts).Error; err != nil {
		return err
	}
	for _, target := range targets {
		target.Inventory.CurrentAlerts = &models.TargetAlertCounts{}
	}
	for _, count := range counts {
		ident := count.TargetIdent
		if ident == "" {
			ident = ruleOwners[count.RuleID]
		}
		if target := byIdent[ident]; target != nil {
			switch count.Severity {
			case 1:
				target.Inventory.CurrentAlerts.Critical += count.Total
			case 2:
				target.Inventory.CurrentAlerts.Warning += count.Total
			case 3:
				target.Inventory.CurrentAlerts.Info += count.Total
			}
		}
	}
	return nil
}

type inventoryPingOwner struct {
	Device       models.MonitorDevice
	Agent        *collection.AgentPingOwner
	DatasourceID int64
}

func (rt *Router) targetInventorySamples(c *gin.Context, targets []*models.Target, owners map[string]inventoryPingOwner) {
	if rt.PromClients == nil || rt.DatasourceCache == nil {
		return
	}
	datasources := rt.DatasourceCache.GetByCate("prometheus")
	if rt.DatasourceCache.DatasourceFilter != nil {
		datasources = rt.DatasourceCache.DatasourceFilter(datasources, c.MustGet("user").(*models.User))
	}
	agents := []string{}
	for _, target := range targets {
		if owners[target.Ident].Device.ID == "" {
			agents = append(agents, regexp.QuoteMeta(target.Ident))
		}
	}
	addresses := []string{}
	for _, target := range targets {
		if target.HostIp != "" {
			addresses = append(addresses, regexp.QuoteMeta(target.HostIp))
		}
	}
	if len(addresses) == 0 && len(agents) == 0 {
		return
	}
	sort.Strings(addresses)
	query := fmt.Sprintf(`ping_result_code{target=~%q}[3m]`, strings.Join(addresses, "|"))
	now := time.Now()
	// Read only stored samples, with one shared time budget for the page.
	queryCtx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	for _, datasource := range datasources {
		client := rt.PromClients.GetCli(datasource.Id)
		if client == nil {
			continue
		}
		if len(agents) > 0 {
			agentQuery := agentCollectionQuery(strings.Join(agents, "|"))
			if value, _, err := client.Query(queryCtx, agentQuery, now); err == nil {
				if vector, ok := value.(model.Vector); ok {
					applyAgentCollection(targets, vector, now, owners)
				}
			}
		}
		if len(addresses) == 0 {
			continue
		}
		value, _, err := client.Query(queryCtx, query, now)
		if err != nil {
			continue
		}
		matrix, ok := value.(model.Matrix)
		if !ok {
			continue
		}
		applyICMP(targets, matrix, now, owners, datasource.Id)
	}
}

func applyICMP(targets []*models.Target, matrix model.Matrix, now time.Time, owners map[string]inventoryPingOwner, datasource int64) {
	for _, series := range matrix {
		address := string(series.Metric["target"])
		for _, sample := range series.Values {
			at := sample.Timestamp.Time()
			if now.Sub(at) > 3*time.Minute || at.After(now.Add(30*time.Second)) || math.IsNaN(float64(sample.Value)) || math.IsInf(float64(sample.Value), 0) {
				continue
			}
			state := "unknown"
			switch sample.Value {
			case 0:
				state = "up"
			case 1:
				state = "down"
			case 2:
				state = "error"
			default:
				continue
			}
			for _, target := range targets {
				if owner, owned := owners[target.Ident]; owned {
					if owner.Agent != nil {
						a := owner.Agent
						if owner.DatasourceID != datasource || string(series.Metric["collection_mode"]) != "agent" || string(series.Metric["ident"]) != a.Ident || string(series.Metric["collection_collector_id"]) != a.CollectorID || string(series.Metric["collection_revision"]) != a.Revision || string(series.Metric["collection_device_id"]) != "" || address != a.Address {
							continue
						}
					} else {
						d := owner.Device
						if !d.Enabled || d.Deleted || owner.DatasourceID != datasource || string(series.Metric["collection_mode"]) != "managed" || string(series.Metric["collection_device_id"]) != d.ID || string(series.Metric["collection_collector_id"]) != d.CollectorID || string(series.Metric["collection_revision"]) != d.DesiredRevision || string(series.Metric["ident"]) != d.Ident {
							continue
						}
					}
				} else if string(series.Metric["ident"]) != target.Ident || string(series.Metric["collection_mode"]) != "" {
					continue
				}
				if target.HostIp != "" && target.HostIp == address && at.Unix() >= target.Inventory.ReachabilityAt {
					if at.Unix() == target.Inventory.ReachabilityAt && state != target.Inventory.Reachability {
						target.Inventory.Reachability = "conflict"
					} else {
						target.Inventory.Reachability, target.Inventory.ReachabilityAt = state, at.Unix()
					}
				}
			}
		}
	}
}

func agentCollectionQuery(identRegex string) string {
	// timestamp() drops __name__. Keep it in a temporary label first so CPU,
	// memory and other metrics with the same dimensions remain unique vectors.
	return fmt.Sprintf(`max by (ident) (timestamp(label_replace({ident=~%q,collection_mode="",__name__!~"ping_.*|categraf_.*"}, "inventory_metric_name", "$1", "__name__", "(.+)")))`, identRegex)
}

// An Agent heartbeat is not proof of metrics reaching the time-series database.
func applyAgentCollection(targets []*models.Target, vector model.Vector, now time.Time, owners map[string]inventoryPingOwner) {
	for _, sample := range vector {
		stamp := float64(sample.Value)
		if math.IsNaN(stamp) || math.IsInf(stamp, 0) || stamp > float64(now.Unix()+30) || stamp < float64(now.Unix()-180) {
			continue
		}
		for _, target := range targets {
			if owners[target.Ident].Device.ID == "" && string(sample.Metric["ident"]) == target.Ident {
				target.Inventory.CollectionOK = true
				target.Inventory.LastSampleAt = int64(stamp)
			}
		}
	}
}

// Counts and detail lists share exact ownership and native visibility filters.
func (rt *Router) deviceEventsQuery(c *gin.Context, idents []string, ruleIDs []int64) (*gorm.DB, error) {
	groups, err := GetBusinessGroupIds(c, rt.Ctx, rt.Center.EventHistoryGroupView, false)
	if err != nil {
		return nil, err
	}
	query := rt.Ctx.DB.Model(&models.AlertCurEvent{})
	if len(ruleIDs) > 0 {
		query = query.Where("target_ident IN ? OR (target_ident = ? AND rule_id IN ?)", idents, "", ruleIDs)
	} else {
		query = query.Where("target_ident IN ?", idents)
	}
	if len(groups) > 0 {
		query = query.Where("group_id IN ?", groups)
	}
	if ids := queryDatasourceIds(c); len(ids) > 0 {
		query = query.Where("datasource_id IN ?", ids)
	}
	if rt.DatasourceCache != nil && rt.DatasourceCache.DatasourceFilter != nil {
		var sources []*models.Datasource
		if err := rt.Ctx.DB.Find(&sources).Error; err != nil {
			return nil, err
		}
		ids := []int64{0} // Native host alerts do not need a datasource.
		for _, source := range rt.DatasourceCache.DatasourceFilter(sources, c.MustGet("user").(*models.User)) {
			ids = append(ids, source.Id)
		}
		query = query.Where("datasource_id IN ?", ids)
	}
	return query, nil
}
