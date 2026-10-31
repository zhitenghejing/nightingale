package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	prompkg "github.com/ccfos/nightingale/v6/pkg/prom"
	"github.com/prometheus/common/model"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/collection"
	"github.com/ccfos/nightingale/v6/pkg/ginx"
	"github.com/ccfos/nightingale/v6/pkg/i18nx"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (rt *Router) collectionScope(c *gin.Context) (collection.Scope, error) {
	me := c.MustGet("user").(*models.User)
	scope := collection.Scope{ActorID: me.Id, Admin: me.IsAdmin()}
	if scope.Admin {
		return scope, nil
	}
	ids, err := models.MyBusiGroupIds(rt.Ctx, me.Id)
	if err != nil {
		return scope, err
	}
	scope.ReadGroups = ids
	for _, id := range ids {
		bg, err := models.BusiGroupGetById(rt.Ctx, id)
		if err != nil {
			return scope, err
		}
		if bg == nil {
			continue
		}
		can, err := me.CanDoBusiGroup(rt.Ctx, bg, "rw")
		if err != nil {
			return scope, err
		}
		if can {
			scope.WriteGroups = append(scope.WriteGroups, id)
		}
	}
	return scope, nil
}
func collectionReply(c *gin.Context, data interface{}, err error) {
	status := 200
	message := ""
	if err != nil {
		status = 503
		message = "collection operation failed"
		var e *collection.Error
		if errors.As(err, &e) {
			status = e.Code
			message = e.Message
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"dat": data, "err": i18nx.Translate(c.GetHeader("X-Language"), message)})
}
func collectionBody(c *gin.Context, value interface{}) error {
	return collectionDecodeBody(c, value, 1<<20)
}
func collectionDecodeBody(c *gin.Context, value interface{}, limit int64) error {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil {
		return &collection.Error{Code: 400, Message: "invalid collection request"}
	}
	if decoder.Decode(new(interface{})) != io.EOF {
		return &collection.Error{Code: 400, Message: "invalid collection request"}
	}
	return nil
}
func collectionVersion(c *gin.Context) (int64, error) {
	var f struct {
		RecordVersion int64 `json:"record_version"`
	}
	if err := collectionBody(c, &f); err != nil {
		return 0, err
	}
	if f.RecordVersion < 1 {
		return 0, &collection.Error{Code: 400, Message: "record_version is required"}
	}
	return f.RecordVersion, nil
}

type collectionDeviceView struct {
	models.MonitorDevice
	Labels     map[string]string              `json:"labels"`
	Parameters map[string]interface{}         `json:"parameters"`
	Notify     *collection.NotificationPreset `json:"notify"`
	Collection collection.State               `json:"collection"`
	Profile    *collection.ProfileView        `json:"profile"`
}

func collectionView(d models.MonitorDevice, state collection.State) collectionDeviceView {
	v := collectionDeviceView{MonitorDevice: d, Collection: state, Labels: map[string]string{}}
	_ = json.Unmarshal([]byte(d.Labels), &v.Labels)
	_ = json.Unmarshal([]byte(d.Parameters), &v.Parameters)
	_ = json.Unmarshal([]byte(d.Notify), &v.Notify)
	return v
}
func (rt *Router) configCollection(r *gin.Engine, pages *gin.RouterGroup) {
	pages.GET("/collection/features", rt.auth(), rt.user(), func(c *gin.Context) { collectionReply(c, gin.H{"enabled": rt.Collection != nil}, nil) })
	if rt.Collection == nil {
		return
	}
	if rt.Center.Collection.ProviderEnable && rt.HTTP.APIForAgent.Enable {
		r.GET("/v1/n9e/collection/configs", rt.collectionConfigs)
	}
	group := pages.Group("/collection", rt.auth(), rt.user())
	rt.configCollectionCatalog(group)
	read := rt.perm("/collection/read")
	write := rt.perm("/collection/write")
	remove := rt.perm("/collection/delete")
	group.GET("/collectors", read, rt.collectionCollectors)
	group.POST("/collectors", write, rt.collectionCollectorCreate)
	group.PUT("/collectors/:id", write, rt.collectionCollectorUpdate)
	group.POST("/collectors/:id/rotate", write, rt.collectionCollectorRotate)
	group.DELETE("/collectors/:id", remove, rt.collectionCollectorDelete)
	group.GET("/profiles", read, func(c *gin.Context) {
		out, err := rt.Collection.Profiles()
		collectionReply(c, out, err)
	})
	group.GET("/templates", read, func(c *gin.Context) {
		out, err := rt.Collection.Templates()
		collectionReply(c, out, err)
	})
	group.POST("/templates", write, func(c *gin.Context) {
		var f collection.TemplateInput
		if err := collectionBody(c, &f); err != nil {
			collectionReply(c, nil, err)
			return
		}
		scope, err := rt.collectionScope(c)
		if err != nil {
			collectionReply(c, nil, err)
			return
		}
		out, err := rt.Collection.SaveTemplate(scope, f)
		collectionReply(c, out, err)
	})
	group.GET("/credentials", read, rt.collectionCredentials)
	group.POST("/credentials", write, rt.collectionCredentialSave)
	group.PUT("/credentials/:id", write, rt.collectionCredentialSave)
	group.GET("/credentials/:id/references", write, rt.collectionCredentialReferences)
	group.DELETE("/credentials/:id", remove, rt.collectionCredentialDelete)
	group.GET("/devices", read, rt.collectionDevices)
	group.POST("/devices", write, rt.collectionDeviceSave)
	group.GET("/devices/:id", read, rt.collectionDevice)
	group.GET("/devices/:id/metrics", read, rt.collectionMetrics)
	group.GET("/devices/:id/template-diff", read, func(c *gin.Context) {
		scope, err := rt.collectionScope(c)
		if err != nil {
			collectionReply(c, nil, err)
			return
		}
		out, err := rt.Collection.DeviceTemplateDiff(scope, c.Param("id"), c.Query("version_id"))
		collectionReply(c, out, err)
	})
	group.PUT("/devices/:id", write, rt.collectionDeviceSave)
	group.POST("/devices/:id/enable", write, rt.collectionDeviceState)
	group.POST("/devices/:id/disable", write, rt.collectionDeviceState)
	group.DELETE("/devices/:id", remove, rt.collectionDeviceState)
	group.POST("/probes", write, rt.collectionProbeCreate)
	group.GET("/probes/:id", read, rt.collectionProbe)
	group.DELETE("/probes/:id", write, rt.collectionProbeCancel)
	group.GET("/publications", read, rt.collectionPublications)
	group.GET("/audits", read, rt.collectionAudits)
	group.POST("/publications/:id/rollback", write, rt.collectionRollback)
}
func (rt *Router) collectionConfigs(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	user, secret, ok := c.Request.BasicAuth()
	if !ok {
		c.Header("WWW-Authenticate", `Basic realm="collection"`)
		c.JSON(401, gin.H{"error": "unauthorized"})
		return
	}
	snapshot, same, err := rt.Collection.Provider(user, secret, c.Query("version"))
	if err != nil {
		status := 503
		message := "configuration unavailable"
		var e *collection.Error
		if errors.As(err, &e) {
			status = e.Code
			message = e.Message
		}
		c.JSON(status, gin.H{"error": message})
		return
	}
	if same {
		c.JSON(200, collection.Snapshot{Version: snapshot.Version})
		return
	}
	c.JSON(200, snapshot)
}
func (rt *Router) collectionCollectors(c *gin.Context) {
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	out, e := rt.Collection.Collectors(s)
	collectionReply(c, out, e)
}
func (rt *Router) collectionCollectorCreate(c *gin.Context) {
	var f collection.CollectorInput
	if e := collectionBody(c, &f); e != nil {
		collectionReply(c, nil, e)
		return
	}
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	out, secret, e := rt.Collection.CreateCollector(s, f)
	collectionReply(c, gin.H{"collector": out, "secret": secret}, e)
}
func (rt *Router) collectionCollectorRotate(c *gin.Context) {
	v, e := collectionVersion(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	secret, e := rt.Collection.RotateCollector(s, c.Param("id"), v)
	collectionReply(c, gin.H{"secret": secret}, e)
}
func (rt *Router) collectionCollectorDelete(c *gin.Context) {
	v, e := collectionVersion(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	s, e := rt.collectionScope(c)
	if e == nil {
		e = rt.Collection.DeleteCollector(s, c.Param("id"), v)
	}
	collectionReply(c, nil, e)
}
func (rt *Router) collectionCredentials(c *gin.Context) {
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	out, e := rt.Collection.Credentials(s)
	collectionReply(c, out, e)
}
func (rt *Router) collectionCredentialSave(c *gin.Context) {
	var f collection.CredentialInput
	if e := collectionBody(c, &f); e != nil {
		collectionReply(c, nil, e)
		return
	}
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	out, e := rt.Collection.SaveCredential(s, c.Param("id"), f)
	collectionReply(c, out, e)
}
func (rt *Router) collectionCredentialDelete(c *gin.Context) {
	v, e := collectionVersion(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	s, e := rt.collectionScope(c)
	if e == nil {
		e = rt.Collection.DeleteCredential(s, c.Param("id"), v)
	}
	collectionReply(c, nil, e)
}
func (rt *Router) collectionCredentialReferences(c *gin.Context) {
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	n, e := rt.Collection.References(s, c.Param("id"))
	collectionReply(c, gin.H{"affected_devices": n}, e)
}
func (rt *Router) collectionDevices(c *gin.Context) {
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	group, _ := strconv.ParseInt(c.Query("group_id"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	page, _ := strconv.Atoi(c.Query("p"))
	if limit < 1 || limit > 50 {
		limit = 50
	}
	if page < 1 {
		page = 1
	}
	devices, total, e := rt.Collection.Devices(s, group, c.Query("collector_id"), c.Query("include_deleted") == "true", limit, (page-1)*limit)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	states, e := rt.CollectionStatus.States(c.Request.Context(), devices)
	out := []collectionDeviceView{}
	for _, d := range devices {
		out = append(out, collectionView(d, states[d.ID]))
	}
	collectionReply(c, gin.H{"list": out, "total": total}, e)
}
func (rt *Router) collectionDevice(c *gin.Context) {
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	d, e := rt.Collection.Device(s, c.Param("id"))
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	states, e := rt.CollectionStatus.States(c.Request.Context(), []models.MonitorDevice{d})
	v := collectionView(d, states[d.ID])
	if e == nil {
		v.Profile, e = rt.Collection.DeviceProfile(d)
	}
	collectionReply(c, v, e)
}
func (rt *Router) collectionDeviceSave(c *gin.Context) {
	var f collection.DeviceInput
	if e := collectionBody(c, &f); e != nil {
		collectionReply(c, nil, e)
		return
	}
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	d, e := rt.Collection.SaveDevice(s, c.Param("id"), c.GetHeader("Idempotency-Key"), f)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	states, e := rt.CollectionStatus.States(c.Request.Context(), []models.MonitorDevice{d})
	v := collectionView(d, states[d.ID])
	if e == nil {
		v.Profile, e = rt.Collection.DeviceProfile(d)
	}
	collectionReply(c, v, e)
}
func (rt *Router) collectionDeviceState(c *gin.Context) {
	v, e := collectionVersion(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	enable := c.Request.Method == "POST" && strings.HasSuffix(c.FullPath(), "/enable")
	d, e := rt.Collection.SetDeviceState(s, c.Param("id"), v, enable, c.Request.Method == "DELETE")
	collectionReply(c, d, e)
}
func (rt *Router) collectionProbeCreate(c *gin.Context) {
	var f collection.DeviceInput
	if e := collectionBody(c, &f); e != nil {
		collectionReply(c, nil, e)
		return
	}
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	p, e := rt.Collection.CreateProbe(s, f)
	collectionReply(c, p, e)
}
func (rt *Router) collectionProbe(c *gin.Context) {
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	p, e := rt.Collection.Probe(s, c.Param("id"))
	collectionReply(c, p, e)
}
func (rt *Router) collectionProbeCancel(c *gin.Context) {
	s, e := rt.collectionScope(c)
	if e == nil {
		e = rt.Collection.CancelProbe(s, c.Param("id"))
	}
	collectionReply(c, nil, e)
}
func (rt *Router) collectionPublications(c *gin.Context) {
	s, e := rt.collectionScope(c)
	if e != nil {
		collectionReply(c, nil, e)
		return
	}
	p, e := rt.Collection.Publications(s)
	collectionReply(c, p, e)
}
func (rt *Router) collectionRollback(c *gin.Context) {
	s, e := rt.collectionScope(c)
	if e == nil {
		e = rt.Collection.Rollback(s, c.Param("id"))
	}
	collectionReply(c, nil, e)
}

func (rt *Router) collectionTargets(c *gin.Context, targets []*models.Target) error {
	if rt.Collection == nil || len(targets) == 0 {
		return nil
	}
	ids := []string{}
	for _, t := range targets {
		ids = append(ids, t.Ident)
	}
	var devices []models.MonitorDevice
	if err := rt.Collection.DB.Where("ident IN ? AND deleted = ?", ids, false).Find(&devices).Error; err != nil {
		return err
	}
	states, err := rt.CollectionStatus.States(c.Request.Context(), devices)
	if err != nil {
		return err
	}
	byIdent := map[string]collection.State{}
	for _, d := range devices {
		byIdent[d.Ident] = states[d.ID]
	}
	for _, t := range targets {
		if state, ok := byIdent[t.Ident]; ok {
			t.Collection = state
		}
	}
	return nil
}
func (rt *Router) collectionTargetFilter(c *gin.Context) models.BuildTargetWhereOption {
	return func(db *gorm.DB) *gorm.DB {
		if rt.Collection == nil {
			return db
		}
		scope, err := rt.collectionScope(c)
		if err != nil {
			db.AddError(err)
			return db
		}
		me := c.MustGet("user").(*models.User)
		canRead, err := me.CheckPerm(rt.Ctx, "/collection/read")
		if err != nil {
			db.AddError(err)
			return db
		}
		all := db.Session(&gorm.Session{NewDB: true}).Model(&models.MonitorDevice{}).Select("ident")
		if !canRead || (!scope.Admin && len(scope.ReadGroups) == 0) {
			db = db.Where("target.ident NOT IN (?)", all)
		} else if !scope.Admin {
			db = db.Where("target.ident NOT IN (?)", all.Where("group_id NOT IN ?", scope.ReadGroups))
		}
		kind := c.Query("collection_kind")
		collector := c.Query("collector_id")
		q := db.Session(&gorm.Session{NewDB: true}).Model(&models.MonitorDevice{}).Select("ident").Where("deleted = ?", false)
		if collector != "" {
			q = q.Where("collector_id = ?", collector)
		}
		if kind == "snmp" {
			q = q.Where("protocol = ?", "snmp_v2c")
		}
		if kind == "mysql" {
			q = q.Where("protocol = ?", "mysql")
		}
		if kind == "snmp" || kind == "mysql" || kind == "managed" || collector != "" {
			return db.Where("target.ident IN (?)", q)
		}
		if kind == "agent" {
			return db.Where("target.ident NOT IN (?)", q)
		}
		return db
	}
}

func (rt *Router) collectionMetrics(c *gin.Context) {
	scope, err := rt.collectionScope(c)
	if err != nil {
		collectionReply(c, nil, err)
		return
	}
	d, err := rt.Collection.Device(scope, c.Param("id"))
	if err != nil {
		collectionReply(c, nil, err)
		return
	}
	var collector models.MonitorCollector
	if err = rt.Collection.DB.First(&collector, "id = ?", d.CollectorID).Error; err != nil {
		collectionReply(c, nil, err)
		return
	}
	client := rt.PromClients.GetCli(collector.DatasourceID)
	if client == nil {
		collectionReply(c, nil, errors.New("datasource unavailable"))
		return
	}
	selector := fmt.Sprintf(`collection_device_id=%q,collection_collector_id=%q,collection_revision=%q,collection_mode="managed"`, d.ID, d.CollectorID, d.DesiredRevision)
	cfg, err := rt.Collection.TemplateConfig(d.TemplateVersionID)
	if err != nil {
		collectionReply(c, nil, err)
		return
	}
	queries := collection.MetricQueries(cfg, selector)
	now := time.Now()
	results := map[string]model.Value{}
	timeout, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	for key, query := range queries {
		value, _, e := client.QueryRange(timeout, query, prompkg.Range{Start: now.Add(-time.Hour), End: now, Step: time.Duration(max(d.IntervalSeconds, 15)) * time.Second})
		if e != nil {
			collectionReply(c, nil, errors.New("datasource query failed"))
			return
		}
		results[key] = value
	}
	collectionReply(c, gin.H{"checked_at": now.Unix(), "metrics": results}, nil)
}

func (rt *Router) collectionStatistics(c *gin.Context, bgids []int64) (map[string]bool, map[string]collection.State) {
	managed := map[string]bool{}
	states := map[string]collection.State{}
	if rt.Collection == nil {
		return managed, states
	}
	var all []string
	ginx.Dangerous(rt.Collection.DB.Model(&models.MonitorDevice{}).Pluck("ident", &all).Error)
	for _, id := range all {
		managed[id] = true
	}
	if c.Query("collection_kind") == "agent" {
		return managed, states
	}
	scope, err := rt.collectionScope(c)
	ginx.Dangerous(err)
	canRead, err := c.MustGet("user").(*models.User).CheckPerm(rt.Ctx, "/collection/read")
	ginx.Dangerous(err)
	if !canRead {
		return managed, states
	}
	q := rt.Collection.DB.Where("deleted = ?", false)
	if kind := c.Query("collection_kind"); kind == "snmp" {
		q = q.Where("protocol = ?", "snmp_v2c")
	} else if kind == "mysql" {
		q = q.Where("protocol = ?", "mysql")
	}
	if !scope.Admin {
		q = q.Where("group_id IN ?", scope.ReadGroups)
	}
	if len(bgids) > 0 {
		q = q.Where("group_id IN ?", bgids)
	}
	if id := c.Query("collector_id"); id != "" {
		q = q.Where("collector_id = ?", id)
	}
	var devices []models.MonitorDevice
	ginx.Dangerous(q.Order("id").Find(&devices).Error)
	for start := 0; start < len(devices); start += 50 {
		end := min(start+50, len(devices))
		batch, err := rt.CollectionStatus.States(c.Request.Context(), devices[start:end])
		ginx.Dangerous(err)
		for _, d := range devices[start:end] {
			states[d.Ident] = batch[d.ID]
		}
	}
	return managed, states
}
func (rt *Router) collectionDowntimeFilter(c *gin.Context, downtime int64) (models.BuildTargetWhereOption, bool) {
	if rt.Collection == nil {
		return rt.downtimeFilter(downtime)
	}
	managed, states := rt.collectionStatistics(c, nil)
	ids := []string{}
	now := time.Now().Unix()
	matches := func(timestamp int64) bool {
		return (downtime > 0 && timestamp < now-downtime) || (downtime < 0 && timestamp > now+downtime)
	}
	for _, target := range rt.TargetCache.GetAll() {
		if !managed[target.Ident] && matches(target.BeatTime) {
			ids = append(ids, target.Ident)
		}
	}
	for ident, state := range states {
		if state.Health != "query_error" && state.Health != "clock_error" && state.LastSampleAt > 0 && matches(state.LastSampleAt) {
			ids = append(ids, ident)
		}
	}
	if len(ids) == 0 {
		return nil, false
	}
	return models.BuildTargetWhereWithIdents(ids), true
}

func (rt *Router) collectionCollectorUpdate(c *gin.Context) {
	var f collection.CollectorInput
	if err := collectionBody(c, &f); err != nil {
		collectionReply(c, nil, err)
		return
	}
	scope, err := rt.collectionScope(c)
	if err != nil {
		collectionReply(c, nil, err)
		return
	}
	out, err := rt.Collection.UpdateCollector(scope, c.Param("id"), f)
	collectionReply(c, out, err)
}

func (rt *Router) collectionAudits(c *gin.Context) {
	scope, err := rt.collectionScope(c)
	if err != nil {
		collectionReply(c, nil, err)
		return
	}
	q := rt.Collection.DB.Order("created_at DESC, id").Limit(200)
	if !scope.Admin {
		q = q.Where("group_id IN ?", scope.ReadGroups)
	}
	if object := c.Query("object_id"); object != "" {
		q = q.Where("object_id = ?", object)
	}
	out := []models.MonitorAudit{}
	err = q.Find(&out).Error
	collectionReply(c, out, err)
}
