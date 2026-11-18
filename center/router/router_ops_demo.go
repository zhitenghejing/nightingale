package router

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (rt *Router) opsDemoSetup(c *gin.Context) {
	var in struct {
		GroupID      int64 `json:"group_id"`
		DatasourceID int64 `json:"datasource_id"`
	}
	if !opsBind(c, &in) {
		return
	}
	u, err := opsauth.Check(rt.Ctx.DB, opsUser(c), in.GroupID, true)
	if err != nil {
		opsResult(c, nil, err)
		return
	}
	if !u.IsAdmin() {
		opsResult(c, nil, opsauth.Fail(403, "admin_required", "Demo setup requires an administrator"))
		return
	}
	info, err := rt.opsDemoRequest(c.Request.Context(), "/info", map[string]string{})
	if err != nil {
		opsResult(c, nil, err)
		return
	}
	ident, ok := info["ident"].(string)
	if !ok || ident == "" {
		opsResult(c, nil, opsauth.Fail(503, "demo_identity", "Demo target identity is unavailable"))
		return
	}
	var ds models.Datasource
	if err = rt.Ctx.DB.First(&ds, in.DatasourceID).Error; err != nil {
		opsResult(c, nil, err)
		return
	}
	if ds.PluginType != "prometheus" {
		opsResult(c, nil, opsauth.Fail(422, "demo_datasource", "Demo requires its own Prometheus datasource"))
		return
	}
	var target models.Target
	err = rt.Ctx.DB.Where("ident = ?", ident).First(&target).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		opsResult(c, nil, err)
		return
	}
	if err == gorm.ErrRecordNotFound {
		target = models.Target{Ident: ident, Note: "Isolated operations demo target"}
		if err = rt.Ctx.DB.Create(&target).Error; err != nil {
			opsResult(c, nil, err)
			return
		}
	}
	var links int64
	if err = rt.Ctx.DB.Model(&models.TargetBusiGroup{}).Where("target_ident = ? AND group_id <> ?", ident, in.GroupID).Count(&links).Error; err != nil || links > 0 {
		opsResult(c, nil, opsauth.Fail(409, "demo_group", "Demo target is already associated with another group"))
		return
	}
	if err = rt.Ctx.DB.Where(models.TargetBusiGroup{TargetIdent: ident, GroupId: in.GroupID}).FirstOrCreate(&models.TargetBusiGroup{TargetIdent: ident, GroupId: in.GroupID, UpdateAt: time.Now().Unix()}).Error; err != nil {
		opsResult(c, nil, err)
		return
	}
	name := "Demo quota watermark " + ident
	var rule models.AlertRule
	err = rt.Ctx.DB.Where("group_id = ? AND name = ?", in.GroupID, name).First(&rule).Error
	if err == gorm.ErrRecordNotFound {
		rule = models.AlertRule{GroupId: in.GroupID, Name: name, Cate: "prometheus", Prod: "metric", DatasourceQueries: []models.DatasourceQuery{{MatchType: 0, Op: "in", Values: []interface{}{in.DatasourceID}}}, PromQl: "ops_demo_quota_used_ratio{ident=" + strconv.Quote(ident) + "} > 0.8", PromForDuration: 10, PromEvalInterval: 5, Severity: 2, NotifyRecovered: 1, NotifyVersion: 1, AnnotationsJSON: map[string]string{"summary": "Test-only application quota watermark", "action": "Use the isolated operations case and require human confirmation"}, RuleConfigJson: map[string]interface{}{"queries": []interface{}{map[string]interface{}{"prom_ql": "ops_demo_quota_used_ratio{ident=" + strconv.Quote(ident) + "} > 0.8", "severity": 2}}}, CreateBy: u.Username, UpdateBy: u.Username}
		err = rule.FE2DB()
		if err == nil {
			err = rule.Add(rt.Ctx)
		}
	}
	if err != nil {
		opsResult(c, nil, err)
		return
	}
	var item models.KnowledgeItem
	title := "Demo directory quota recovery " + ident
	err = rt.Ctx.DB.Where("group_id = ? AND title = ?", in.GroupID, title).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		now := time.Now().Unix()
		spec := knowledge.Spec{Summary: "Isolated demo directory quota watermark and report queue recovery", Body: "This test-only scenario uses a bounded application cache directory. It does not fill the host filesystem and contains no hospital data.", SourceNote: "ops-demo fixture, bounded directory and independent test record ledger", SourceIDs: []int64{}, RuleIDs: []int64{rule.Id}, Conditions: map[string]string{"system_version": "1", "environment": "test"}, TestOnly: true, ValidatedAt: now - 1, ReviewAt: now + 30*86400, Escalation: "If the business or data probe fails, keep the case open and notify the test operator.", Steps: []knowledge.Step{{Key: "storage", Title: "Check directory quota watermark", Kind: "metric", Query: "ops_demo_quota_used_ratio", Instruction: "Inspect the current directory quota ratio and recent trend.", Expected: "The value is above 0.8 while the test cache is under pressure."}, {Key: "queue", Title: "Check queued test records", Kind: "metric", Query: "ops_demo_queue_size", Instruction: "Inspect the test application queue.", Expected: "Queued records are reconciled after the controlled action."}}, Actions: []knowledge.Action{{Key: "reclaim", Title: "Reclaim test application cache", Kind: "demo_reclaim", Risk: "Only generated cache files under the isolated demo directory are removed. Stop if target identity differs.", Rollback: "Regenerate the test storage fault; escalate if the test business probe still fails.", Instruction: "After confirmation, reclaim the demo cache and drain test records."}}, Checks: []knowledge.Check{{Key: "metric", Title: "Quota watermark remains normal", Kind: "metric", Query: "ops_demo_quota_used_ratio", Operator: "<", Threshold: 0.8, WindowSeconds: 20, FreshSeconds: 15, Required: true}, {Key: "business", Title: "Test business probe passes", Kind: "demo_business", Required: true}, {Key: "data", Title: "Test records are complete without duplicates", Kind: "demo_data", Required: true}}}
		created, e := rt.Ops.Knowledge.Save(opsUser(c), 0, in.GroupID, 0, title, spec)
		if e != nil {
			opsResult(c, nil, e)
			return
		}
		v, e := rt.Ops.Knowledge.Publish(opsUser(c), created.ID, created.Revision)
		if e != nil {
			opsResult(c, nil, e)
			return
		}
		created.CurrentVersionID = v.ID
		item = *created
	} else if err != nil {
		opsResult(c, nil, err)
		return
	}
	policies, _ := rt.Ops.Policies(opsUser(c))
	found := false
	for _, p := range policies {
		if p.GroupID == in.GroupID {
			found = true
		}
	}
	if !found {
		raw, _ := json.Marshal([]int64{rule.Id})
		err = rt.Ops.SavePolicy(opsUser(c), models.OpsPolicy{GroupID: in.GroupID, Enabled: true, ServiceUserID: opsUser(c), AutoInvestigate: false, RuleIDs: models.OpsJSON(raw)})
		if err != nil {
			opsResult(c, nil, err)
			return
		}
	}
	opsResult(c, gin.H{"ident": ident, "rule_id": rule.Id, "knowledge_id": item.ID, "knowledge_version_id": item.CurrentVersionID, "group_id": in.GroupID, "datasource_id": in.DatasourceID, "test_only": true}, nil)
}
func (rt *Router) opsDemoFault(c *gin.Context) {
	var in struct {
		GroupID int64  `json:"group_id"`
		Kind    string `json:"kind"`
	}
	if !opsBind(c, &in) {
		return
	}
	u, err := opsauth.Check(rt.Ctx.DB, opsUser(c), in.GroupID, true)
	if err != nil {
		opsResult(c, nil, err)
		return
	}
	if !u.IsAdmin() {
		opsResult(c, nil, opsauth.Fail(403, "admin_required", "Fault injection requires a test administrator"))
		return
	}
	switch in.Kind {
	case "storage", "business", "data", "clear":
	default:
		opsResult(c, nil, opsauth.Fail(422, "fault_kind", "Unsupported test fault"))
		return
	}
	info, err := rt.opsDemoRequest(c.Request.Context(), "/info", map[string]string{})
	if err != nil {
		opsResult(c, nil, err)
		return
	}
	ident := fmt.Sprint(info["ident"])
	var count int64
	if err = rt.Ctx.DB.Model(&models.TargetBusiGroup{}).Where("target_ident = ? AND group_id = ?", ident, in.GroupID).Count(&count).Error; err != nil || count != 1 {
		opsResult(c, nil, opsauth.Fail(403, "demo_scope", "Demo target is not linked to this business group"))
		return
	}
	v, e := rt.opsDemoRequest(c.Request.Context(), "/fault", map[string]string{"kind": in.Kind, "ident": ident})
	opsResult(c, v, e)
}
