package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/collection"
	"github.com/ccfos/nightingale/v6/pkg/ginx"
	prompkg "github.com/ccfos/nightingale/v6/pkg/prom"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/common/model"
	"gorm.io/gorm"
)

type deviceMetricInfo struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Unit       string `json:"unit"`
	Note       string `json:"note"`
}
type deviceRuleInfo struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Disabled int    `json:"disabled"`
	Severity int    `json:"severity"`
	Source   string `json:"source"`
}
type deviceDetailView struct {
	Target        *models.Target        `json:"target"`
	Device        *collectionDeviceView `json:"device,omitempty"`
	CollectorName string                `json:"collector_name"`
	Metrics       []deviceMetricInfo    `json:"metrics"`
	Rules         []deviceRuleInfo      `json:"rules"`
}

// A Center UI read only. Native target visibility and collection read scope are
// both checked before reading metrics, rules or events; no adoption is performed.
func (rt *Router) deviceDetailSubject(c *gin.Context) (*models.Target, *models.MonitorDevice, error) {
	ident := c.Query("ident")
	var device *models.MonitorDevice
	if id := c.Query("device_id"); id != "" {
		if rt.Collection == nil {
			return nil, nil, &collection.Error{Code: http.StatusNotFound, Message: "collection unavailable"}
		}
		scope, err := rt.collectionScope(c)
		if err != nil {
			return nil, nil, err
		}
		d, err := rt.Collection.Device(scope, id)
		if err != nil {
			return nil, nil, err
		}
		device = &d
		ident = d.Ident
	}
	if ident == "" || len(ident) > 191 {
		return nil, nil, &collection.Error{Code: 400, Message: "device identity required"}
	}
	user := c.MustGet("user").(*models.User)
	query := rt.Ctx.DB.Model(&models.Target{}).Where("target.ident = ?", ident)
	if !user.IsAdmin() {
		groups, err := models.MyBusiGroupIds(rt.Ctx, user.Id)
		if err != nil {
			return nil, nil, err
		}
		query = models.BuildTargetWhereWithBgids(append(groups, 0))(query)
	}
	var target models.Target
	if err := query.First(&target).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil, &collection.Error{Code: 404, Message: "device not found"}
		}
		return nil, nil, err
	}
	if device == nil && rt.Collection != nil {
		var d models.MonitorDevice
		err := rt.Collection.DB.Where("ident = ?", ident).First(&d).Error
		if err != nil && err != gorm.ErrRecordNotFound {
			return nil, nil, err
		}
		if err == nil {
			if d.Deleted {
				return nil, nil, &collection.Error{Code: 404, Message: "device not found"}
			}
			device = &d
		}
	}
	if device != nil {
		allowed, err := user.CheckPerm(rt.Ctx, "/collection/read")
		if err != nil {
			return nil, nil, err
		}
		if !allowed {
			return nil, nil, &collection.Error{Code: 403, Message: "forbidden"}
		}
		scope, err := rt.collectionScope(c)
		if err != nil {
			return nil, nil, err
		}
		if !scope.Can(device.GroupID, false) {
			return nil, nil, &collection.Error{Code: 403, Message: "forbidden"}
		}
	}
	target.TagsJSON = strings.Fields(target.Tags)
	if err := target.FillGroup(rt.Ctx, map[int64]*models.BusiGroup{}); err != nil {
		return nil, nil, err
	}
	return &target, device, nil
}
func deviceOwnedRuleIDs(device *models.MonitorDevice) []int64 {
	ids := []int64{}
	if device != nil {
		var state struct {
			Rules []struct {
				ID int64 `json:"id"`
			} `json:"rules"`
		}
		if json.Unmarshal([]byte(device.ProfileState), &state) == nil {
			for _, rule := range state.Rules {
				ids = append(ids, rule.ID)
			}
		}
	}
	return ids
}
func (rt *Router) targetDeviceDetail(c *gin.Context) {
	target, device, err := rt.deviceDetailSubject(c)
	if err != nil {
		collectionReply(c, nil, err)
		return
	}
	out := deviceDetailView{Target: target, Metrics: []deviceMetricInfo{}, Rules: []deviceRuleInfo{}}
	if device != nil {
		states, e := rt.collectionStatesForUser(c, []models.MonitorDevice{*device})
		if e != nil {
			collectionReply(c, nil, e)
			return
		}
		v := collectionView(*device, states[device.ID])
		v.Profile, err = rt.Collection.DeviceProfile(*device)
		if err != nil {
			collectionReply(c, nil, err)
			return
		}
		out.Device = &v
		target.Collection = v.Collection
		var collector models.MonitorCollector
		if err = rt.Collection.DB.First(&collector, "id = ?", device.CollectorID).Error; err != nil {
			collectionReply(c, nil, err)
			return
		}
		out.CollectorName = collector.Name
	}
	if err = rt.targetInventory(c, []*models.Target{target}); err != nil {
		collectionReply(c, nil, err)
		return
	}
	// Archived records remain inspectable by their scoped collection ID.
	if device != nil && device.Deleted {
		target.Inventory.Name = device.Name
		target.Inventory.Method, _ = inventoryMethod(device.Protocol)
		target.Inventory.CollectionOK = false
		if out.Device.Profile != nil && out.Device.Profile.Catalog != nil {
			target.Inventory.DeviceType, target.Inventory.DeviceModel = inventoryClassification(out.Device.Profile.Catalog.CategoryPath)
			if out.Device.Profile.Catalog.Taxonomy == collection.TypeBrandModelTaxonomy && len(out.Device.Profile.Catalog.CategoryPath) == 3 {
				target.Inventory.DeviceModel = inventoryText(out.Device.Profile.Catalog.CategoryPath[2])
			}
		} else if device.TemplateVersionID == collection.WindowsTemplate {
			target.Inventory.DeviceType = "操作系统"
			target.Inventory.DeviceModel = "Windows"
		}
	}
	if device == nil && rt.Collection != nil {
		owners, e := collection.AgentPingOwners(rt.Collection.DB, []string{target.Ident})
		if e != nil {
			collectionReply(c, nil, e)
			return
		}
		if owner, ok := owners[target.Ident]; ok {
			var collector models.MonitorCollector
			if err = rt.Collection.DB.First(&collector, "id = ?", owner.CollectorID).Error; err != nil {
				collectionReply(c, nil, err)
				return
			}
			out.CollectorName = collector.Name
		}
	}
	if out.Device != nil && out.Device.Profile != nil {
		for _, metric := range out.Device.Profile.Metrics {
			out.Metrics = append(out.Metrics, deviceMetricInfo{Name: metric.Name, Expression: metric.Expression, Unit: metric.Unit, Note: metric.Note})
		}
	} else {
		var queries map[string]string
		if device != nil {
			cfg, e := rt.Collection.TemplateConfig(device.TemplateVersionID)
			if e != nil {
				collectionReply(c, nil, e)
				return
			}
			queries = collection.MetricQueries(cfg, fmt.Sprintf(`collection_device_id=%q,collection_collector_id=%q,collection_revision=%q,collection_mode="managed"`, device.ID, device.CollectorID, device.DesiredRevision))
		} else {
			queries = nativeDeviceQueries(target.Ident)
		}
		names := []string{}
		for name := range queries {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			unit := ""
			switch name {
			case "cpu", "memory", "disk":
				unit = "percent"
			case "network_in", "network_out":
				unit = "bits/sec"
			}
			out.Metrics = append(out.Metrics, deviceMetricInfo{Name: name, Expression: queries[name], Unit: unit})
		}
	}
	owned := deviceOwnedRuleIDs(device)
	events, e := rt.deviceEventsQuery(c, []string{target.Ident}, owned)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	ids := append([]int64{}, owned...)
	var eventRules []int64
	if err = events.Distinct("rule_id").Limit(1000).Pluck("rule_id", &eventRules).Error; err != nil {
		collectionReply(c, nil, err)
		return
	}
	ids = append(ids, eventRules...)
	if len(ids) > 0 {
		query := rt.Ctx.DB.Model(&models.AlertRule{}).Select("id,name,disabled,severity").Where("id IN ?", ids)
		user := c.MustGet("user").(*models.User)
		if !user.IsAdmin() {
			groups, e := models.MyBusiGroupIds(rt.Ctx, user.Id)
			if e != nil {
				collectionReply(c, nil, e)
				return
			}
			query = query.Where("group_id IN ?", groups)
		}
		if err = query.Order("id").Find(&out.Rules).Error; err != nil {
			collectionReply(c, nil, err)
			return
		}
		for i := range out.Rules {
			out.Rules[i].Source = "event"
			for _, id := range owned {
				if out.Rules[i].ID == id {
					out.Rules[i].Source = "template"
					break
				}
			}
		}
	}
	collectionReply(c, out, nil)
}

func (rt *Router) targetDeviceAlerts(c *gin.Context) {
	target, device, err := rt.deviceDetailSubject(c)
	if err != nil {
		collectionReply(c, nil, err)
		return
	}
	page, limit := ginx.QueryInt(c, "p", 1), ginx.QueryInt(c, "limit", 20)
	if page < 1 || page > 100000 || limit < 1 || limit > 100 {
		collectionReply(c, nil, &collection.Error{Code: 400, Message: "invalid pagination"})
		return
	}
	query, err := rt.deviceEventsQuery(c, []string{target.Ident}, deviceOwnedRuleIDs(device))
	if err != nil {
		collectionReply(c, nil, err)
		return
	}
	var total int64
	if err = query.Count(&total).Error; err != nil {
		collectionReply(c, nil, err)
		return
	}
	// Deliberately omit rule configs, notification destinations and credentials.
	var events = []struct {
		ID           int64  `json:"id"`
		RuleID       int64  `json:"rule_id"`
		RuleName     string `json:"rule_name"`
		Severity     int    `json:"severity"`
		TriggerTime  int64  `json:"trigger_time"`
		TriggerValue string `json:"trigger_value"`
		TargetIdent  string `json:"target_ident"`
	}{}
	err = query.Select("id,rule_id,rule_name,severity,trigger_time,trigger_value,target_ident").Order("severity,id DESC").Limit(limit).Offset((page - 1) * limit).Find(&events).Error
	collectionReply(c, gin.H{"list": events, "total": total}, err)
}
func nativeDeviceQueries(ident string) map[string]string {
	selector := fmt.Sprintf(`ident=%q,collection_mode=""`, ident)
	return map[string]string{"cpu": `cpu_usage_active{` + selector + `}`, "memory": `mem_used_percent{` + selector + `}`, "disk": `disk_used_percent{` + selector + `}`, "network_in": `8 * rate(net_bytes_recv{` + selector + `}[5m])`, "network_out": `8 * rate(net_bytes_sent{` + selector + `}[5m])`}
}
func (rt *Router) targetDeviceMetrics(c *gin.Context) {
	target, device, err := rt.deviceDetailSubject(c)
	if err != nil {
		collectionReply(c, nil, err)
		return
	}
	if device != nil {
		collectionReply(c, nil, &collection.Error{Code: 400, Message: "use the collection metrics endpoint for a managed device"})
		return
	}
	if rt.PromClients == nil || rt.DatasourceCache == nil {
		collectionReply(c, nil, &collection.Error{Code: 503, Message: "datasource unavailable"})
		return
	}
	sources := rt.DatasourceCache.GetByCate("prometheus")
	if rt.DatasourceCache.DatasourceFilter != nil {
		sources = rt.DatasourceCache.DatasourceFilter(sources, c.MustGet("user").(*models.User))
	}
	if len(sources) == 0 {
		collectionReply(c, nil, &collection.Error{Code: 503, Message: "datasource unavailable"})
		return
	}
	now := time.Now()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	results := map[string]model.Matrix{}
	failed := map[string]bool{}
	for key, query := range nativeDeviceQueries(target.Ident) {
		results[key] = model.Matrix{}
		for _, source := range sources {
			client := rt.PromClients.GetCli(source.Id)
			if client == nil {
				failed[key] = true
				continue
			}
			value, _, e := client.QueryRange(ctx, query, prompkg.Range{Start: now.Add(-time.Hour), End: now, Step: 30 * time.Second})
			if e != nil {
				failed[key] = true
				continue
			}
			if matrix, ok := value.(model.Matrix); ok {
				for _, series := range matrix {
					series.Metric["datasource_id"] = model.LabelValue(fmt.Sprint(source.Id))
					results[key] = append(results[key], series)
				}
			}
		}
	}
	collectionReply(c, gin.H{"metrics": results, "metric_errors": failed, "checked_at": now.Unix()}, nil)
}
