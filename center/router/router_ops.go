package router

import (
	"errors"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/ginx"
	"github.com/ccfos/nightingale/v6/pkg/i18nx"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/logger"
	"gorm.io/gorm"
)

func opsUser(c *gin.Context) int64 { return c.MustGet("user").(*models.User).Id }
func opsID(c *gin.Context, key string) int64 {
	id, err := strconv.ParseInt(c.Param(key), 10, 64)
	if err != nil || id <= 0 {
		ginx.Bomb(400, "Invalid operations identifier")
	}
	return id
}
func opsQueryID(c *gin.Context, key string) int64 {
	id, _ := strconv.ParseInt(c.Query(key), 10, 64)
	return id
}
func opsBind(c *gin.Context, v interface{}) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	if err := c.ShouldBindJSON(v); err != nil {
		opsResult(c, nil, opsauth.Fail(400, "invalid_request", "Invalid operations request"))
		return false
	}
	return true
}
func opsResult(c *gin.Context, v interface{}, err error) {
	if err == nil {
		ginx.NewRender(c).Data(v, nil)
		return
	}
	status, code, message := 500, "operations_failed", "Operations request failed"
	var p *opsauth.Problem
	if errors.As(err, &p) {
		status, code, message = p.Status, p.Code, p.Message
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		status, code, message = 404, "not_found", "Operations record not found"
	} else {
		logger.Errorf("ops request: %v", err)
	}
	message = i18nx.Translate(c.GetHeader("X-Language"), message)
	c.JSON(status, gin.H{"err": message, "error": gin.H{"code": code, "message": message}})
}
func (rt *Router) configOpsRoutes(pages *gin.RouterGroup) {
	if rt.Ops == nil {
		return
	}
	g := pages.Group("/ops", rt.auth(), rt.user())
	g.GET("/config", rt.opsConfig)
	g.GET("/cases", rt.opsCases)
	g.POST("/cases", rt.opsCreate)
	g.GET("/cases/:id", rt.opsDetail)
	g.POST("/cases/:id/assign", rt.opsAssign)
	g.POST("/cases/:id/merge", rt.opsMerge)
	g.PUT("/cases/:id/context", rt.opsContext)
	g.GET("/cases/:id/matches", rt.opsMatches)
	g.POST("/cases/:id/knowledge", rt.opsSelectKnowledge)
	g.POST("/cases/:id/plans", rt.opsPlanPropose)
	g.POST("/cases/:id/plans/:plan/decision", rt.opsPlanDecide)
	g.POST("/cases/:id/steps", rt.opsStep)
	g.POST("/cases/:id/evidence/manual", rt.opsManualEvidence)
	g.POST("/cases/:id/collect", rt.opsCollect)
	g.POST("/cases/:id/collect-logs", rt.opsCollectLogs)
	g.POST("/cases/:id/actions", rt.opsProposeAction)
	g.POST("/cases/:id/actions/:action/decision", rt.opsDecideAction)
	g.POST("/cases/:id/actions/:action/result", rt.opsActionResult)
	g.POST("/cases/:id/actions/:action/reconcile", rt.opsActionReconcile)
	g.POST("/cases/:id/verify", rt.opsVerify)
	g.POST("/cases/:id/checks/:key", rt.opsConfirmCheck)
	g.POST("/cases/:id/close", rt.opsClose)
	g.POST("/cases/:id/feedback", rt.opsFeedback)
	g.POST("/cases/:id/runs", rt.opsQueue)
	g.DELETE("/cases/:id/runs", rt.opsCancel)
	g.GET("/knowledge", rt.opsKnowledgeList)
	g.POST("/knowledge", rt.opsKnowledgeSave)
	g.POST("/knowledge-draft", rt.opsGenerateDraft)
	g.GET("/knowledge/:id", rt.opsKnowledgeGet)
	g.PUT("/knowledge/:id", rt.opsKnowledgeSave)
	g.POST("/knowledge/:id/publish", rt.opsKnowledgePublish)
	g.GET("/knowledge/:id/versions", rt.opsKnowledgeVersions)
	g.GET("/versions/:version", rt.opsKnowledgeVersion)
	g.POST("/versions/:version/withdraw", rt.opsKnowledgeWithdraw)
	g.GET("/sources", rt.opsSources)
	g.POST("/sources", rt.opsSourceUpload)
	g.POST("/sources/from-archive", rt.opsImportArchive)
	g.GET("/sources/:source/file", rt.opsSourceFile)
	g.GET("/policies", rt.opsPolicies)
	g.PUT("/policies", rt.opsPolicySave)
	g.POST("/policies/:group/ack-gap", rt.opsAcknowledgeGap)
	if rt.Center.Ops.DemoEnable {
		g.POST("/demo/setup", rt.opsDemoSetup)
		g.POST("/demo/fault", rt.opsDemoFault)
	}
}
func (rt *Router) opsAcknowledgeGap(c *gin.Context) {
	var in struct {
		Note string `json:"note"`
	}
	if !opsBind(c, &in) {
		return
	}
	opsResult(c, nil, rt.Ops.AcknowledgeGap(opsUser(c), opsID(c, "group"), in.Note))
}
func (rt *Router) opsMerge(c *gin.Context) {
	var in struct {
		Revision       int64  `json:"revision"`
		SourceID       int64  `json:"source_id"`
		SourceRevision int64  `json:"source_revision"`
		Reason         string `json:"reason"`
	}
	if !opsBind(c, &in) {
		return
	}
	opsResult(c, nil, rt.Ops.Merge(opsUser(c), opsID(c, "id"), in.Revision, in.SourceID, in.SourceRevision, in.Reason))
}
func (rt *Router) opsActionReconcile(c *gin.Context) {
	v, e := rt.Ops.ReconcileAction(c.Request.Context(), opsUser(c), opsID(c, "id"), opsID(c, "action"))
	opsResult(c, v, e)
}
func (rt *Router) opsConfig(c *gin.Context) {
	ids, admin, err := opsauth.Groups(rt.Ctx.DB, opsUser(c))
	if err != nil {
		opsResult(c, nil, err)
		return
	}
	q := rt.Ctx.DB.Model(&models.BusiGroup{})
	if !admin {
		q = q.Where("id IN ?", ids)
	}
	groups := []models.BusiGroup{}
	if err = q.Order("name").Find(&groups).Error; err != nil {
		opsResult(c, nil, err)
		return
	}
	out := []gin.H{}
	for _, g := range groups {
		_, e := opsauth.Check(rt.Ctx.DB, opsUser(c), g.Id, true)
		out = append(out, gin.H{"id": g.Id, "name": g.Name, "writable": e == nil})
	}
	opsResult(c, gin.H{"groups": out, "user_id": opsUser(c), "model_enabled": rt.Center.Ops.ModelEnable, "execution_enabled": rt.Center.Ops.AllowExecution, "demo_enabled": rt.Center.Ops.DemoEnable}, nil)
}
func (rt *Router) opsCases(c *gin.Context) {
	v, e := rt.Ops.List(opsUser(c), opsQueryID(c, "group_id"), c.Query("state"))
	opsResult(c, v, e)
}
func (rt *Router) opsCreate(c *gin.Context) {
	var in struct {
		EventID int64             `json:"event_id"`
		GroupID int64             `json:"group_id"`
		Title   string            `json:"title"`
		Context map[string]string `json:"context"`
	}
	if !opsBind(c, &in) {
		return
	}
	if in.EventID > 0 {
		event, err := models.AlertHisEventGetById(rt.Ctx, in.EventID)
		if err != nil {
			opsResult(c, nil, err)
			return
		}
		if event == nil {
			opsResult(c, nil, gorm.ErrRecordNotFound)
			return
		}
		if _, err = opsauth.Check(rt.Ctx.DB, opsUser(c), event.GroupId, true); err != nil {
			opsResult(c, nil, err)
			return
		}
		v, e := rt.Ops.Ingest(opsUser(c), *event)
		opsResult(c, v, e)
		return
	}
	v, e := rt.Ops.Create(opsUser(c), in.GroupID, in.Title, in.Context)
	opsResult(c, v, e)
}
func (rt *Router) opsDetail(c *gin.Context) {
	v, e := rt.Ops.Detail(opsUser(c), opsID(c, "id"))
	opsResult(c, v, e)
}
func (rt *Router) opsAssign(c *gin.Context) {
	var in struct {
		Revision int64 `json:"revision"`
		OwnerID  int64 `json:"owner_id"`
	}
	if !opsBind(c, &in) {
		return
	}
	v, e := rt.Ops.Assign(opsUser(c), opsID(c, "id"), in.Revision, in.OwnerID)
	opsResult(c, v, e)
}
func (rt *Router) opsContext(c *gin.Context) {
	var in struct {
		Revision   int64             `json:"revision"`
		Context    map[string]string `json:"context"`
		WaitReason string            `json:"wait_reason"`
	}
	if !opsBind(c, &in) {
		return
	}
	e := rt.Ops.UpdateContext(opsUser(c), opsID(c, "id"), in.Revision, in.Context, in.WaitReason)
	opsResult(c, nil, e)
}
func (rt *Router) opsMatches(c *gin.Context) {
	v, e := rt.Ops.Matches(opsUser(c), opsID(c, "id"))
	opsResult(c, v, e)
}
func (rt *Router) opsSelectKnowledge(c *gin.Context) {
	var in struct {
		Revision  int64  `json:"revision"`
		VersionID int64  `json:"version_id"`
		Reason    string `json:"reason"`
	}
	if !opsBind(c, &in) {
		return
	}
	e := rt.Ops.SelectKnowledge(opsUser(c), opsID(c, "id"), in.Revision, in.VersionID, in.Reason)
	opsResult(c, nil, e)
}
func (rt *Router) opsStep(c *gin.Context) {
	var in struct {
		Key         string  `json:"key"`
		State       string  `json:"state"`
		Note        string  `json:"note"`
		EvidenceIDs []int64 `json:"evidence_ids"`
	}
	if !opsBind(c, &in) {
		return
	}
	e := rt.Ops.RecordStep(opsUser(c), opsID(c, "id"), in.Key, in.State, in.Note, in.EvidenceIDs)
	opsResult(c, nil, e)
}
func (rt *Router) opsManualEvidence(c *gin.Context) {
	var in struct {
		Note string `json:"note"`
	}
	if !opsBind(c, &in) {
		return
	}
	v, e := rt.Ops.AddManualEvidence(opsUser(c), opsID(c, "id"), in.Note)
	opsResult(c, v, e)
}
func (rt *Router) opsCollect(c *gin.Context) {
	var in struct {
		Query  string `json:"query"`
		Window int64  `json:"window_seconds"`
	}
	if !opsBind(c, &in) {
		return
	}
	v, e := rt.Ops.CollectMetric(c.Request.Context(), opsUser(c), opsID(c, "id"), in.Query, in.Window)
	opsResult(c, v, e)
}
func (rt *Router) opsProposeAction(c *gin.Context) {
	var in struct {
		Key string `json:"key"`
	}
	if !opsBind(c, &in) {
		return
	}
	v, e := rt.Ops.ProposeAction(c.Request.Context(), opsUser(c), opsID(c, "id"), in.Key)
	opsResult(c, v, e)
}
func (rt *Router) opsDecideAction(c *gin.Context) {
	var in struct {
		Accept bool `json:"accept"`
	}
	if !opsBind(c, &in) {
		return
	}
	v, e := rt.Ops.Approve(c.Request.Context(), opsUser(c), opsID(c, "id"), opsID(c, "action"), in.Accept)
	opsResult(c, v, e)
}
func (rt *Router) opsActionResult(c *gin.Context) {
	var in struct {
		Note    string `json:"note"`
		Success bool   `json:"success"`
	}
	if !opsBind(c, &in) {
		return
	}
	e := rt.Ops.ManualActionResult(opsUser(c), opsID(c, "id"), opsID(c, "action"), in.Note, in.Success)
	opsResult(c, nil, e)
}
func (rt *Router) opsVerify(c *gin.Context) {
	v, e := rt.Ops.Verify(c.Request.Context(), opsUser(c), opsID(c, "id"))
	opsResult(c, v, e)
}
func (rt *Router) opsConfirmCheck(c *gin.Context) {
	var in struct {
		Note   string `json:"note"`
		Passed bool   `json:"passed"`
	}
	if !opsBind(c, &in) {
		return
	}
	e := rt.Ops.ConfirmCheck(opsUser(c), opsID(c, "id"), c.Param("key"), in.Note, in.Passed)
	opsResult(c, nil, e)
}
func (rt *Router) opsClose(c *gin.Context) {
	var in struct {
		Revision int64  `json:"revision"`
		Kind     string `json:"kind"`
		Summary  string `json:"summary"`
		Followup string `json:"followup"`
	}
	if !opsBind(c, &in) {
		return
	}
	e := rt.Ops.Close(opsUser(c), opsID(c, "id"), in.Revision, in.Kind, in.Summary, in.Followup)
	opsResult(c, nil, e)
}
func (rt *Router) opsFeedback(c *gin.Context) {
	var in struct {
		Note string `json:"note"`
	}
	if !opsBind(c, &in) {
		return
	}
	v, e := rt.Ops.Feedback(opsUser(c), opsID(c, "id"), in.Note)
	opsResult(c, v, e)
}
func (rt *Router) opsQueue(c *gin.Context) {
	var in struct {
		Mode string `json:"mode"`
	}
	if !opsBind(c, &in) {
		return
	}
	v, e := rt.Ops.Queue(opsUser(c), opsID(c, "id"), in.Mode)
	opsResult(c, v, e)
}
func (rt *Router) opsCancel(c *gin.Context) {
	opsResult(c, nil, rt.Ops.CancelRun(opsUser(c), opsID(c, "id")))
}
func (rt *Router) opsPolicies(c *gin.Context) {
	v, e := rt.Ops.Policies(opsUser(c))
	opsResult(c, v, e)
}
func (rt *Router) opsPolicySave(c *gin.Context) {
	var in models.OpsPolicy
	if !opsBind(c, &in) {
		return
	}
	opsResult(c, nil, rt.Ops.SavePolicy(opsUser(c), in))
}
func (rt *Router) opsKnowledgeList(c *gin.Context) {
	v, e := rt.Ops.Knowledge.List(opsUser(c), opsQueryID(c, "group_id"))
	opsResult(c, v, e)
}
func (rt *Router) opsKnowledgeGet(c *gin.Context) {
	v, e := rt.Ops.Knowledge.Get(opsUser(c), opsID(c, "id"))
	opsResult(c, v, e)
}
func (rt *Router) opsKnowledgeSave(c *gin.Context) {
	var in struct {
		GroupID  int64          `json:"group_id"`
		Revision int64          `json:"revision"`
		Title    string         `json:"title"`
		Spec     knowledge.Spec `json:"spec"`
	}
	if !opsBind(c, &in) {
		return
	}
	id := int64(0)
	if c.Param("id") != "" {
		id = opsID(c, "id")
	}
	v, e := rt.Ops.Knowledge.Save(opsUser(c), id, in.GroupID, in.Revision, in.Title, in.Spec)
	opsResult(c, v, e)
}
func (rt *Router) opsKnowledgePublish(c *gin.Context) {
	var in struct {
		Revision int64 `json:"revision"`
	}
	if !opsBind(c, &in) {
		return
	}
	v, e := rt.Ops.Knowledge.Publish(opsUser(c), opsID(c, "id"), in.Revision)
	opsResult(c, v, e)
}
func (rt *Router) opsKnowledgeVersions(c *gin.Context) {
	v, e := rt.Ops.Knowledge.Versions(opsUser(c), opsID(c, "id"))
	opsResult(c, v, e)
}
func (rt *Router) opsKnowledgeVersion(c *gin.Context) {
	v, s, e := rt.Ops.Knowledge.Version(opsUser(c), opsID(c, "version"))
	opsResult(c, gin.H{"version": v, "spec": s}, e)
}
func (rt *Router) opsKnowledgeWithdraw(c *gin.Context) {
	var in struct {
		Reason string `json:"reason"`
	}
	if !opsBind(c, &in) {
		return
	}
	opsResult(c, nil, rt.Ops.Knowledge.Withdraw(opsUser(c), opsID(c, "version"), in.Reason))
}
func (rt *Router) opsSources(c *gin.Context) {
	v, e := rt.Ops.Knowledge.Sources(opsUser(c), opsQueryID(c, "group_id"))
	opsResult(c, v, e)
}
func (rt *Router) opsSourceUpload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, knowledge.MaxSourceBytes+1<<20)
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		opsResult(c, nil, opsauth.Fail(422, "source_request", "Source upload exceeds its limit or is invalid"))
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	group, _ := strconv.ParseInt(c.PostForm("group_id"), 10, 64)
	f, h, err := c.Request.FormFile("file")
	if err != nil {
		opsResult(c, nil, opsauth.Fail(422, "source_required", "Source file is required"))
		return
	}
	defer f.Close()
	v, e := rt.Ops.Knowledge.AddSource(opsUser(c), group, h.Filename, c.PostForm("description"), f)
	opsResult(c, v, e)
}
func (rt *Router) opsSourceFile(c *gin.Context) {
	row, data, err := rt.Ops.Knowledge.Source(opsUser(c), opsID(c, "source"))
	if err != nil {
		opsResult(c, nil, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": strings.ReplaceAll(row.Name, "\n", "")}))
	c.Data(200, row.MediaType, data)
}

func (rt *Router) opsCollectLogs(c *gin.Context) {
	v, e := rt.Ops.CollectLogs(c.Request.Context(), opsUser(c), opsID(c, "id"))
	opsResult(c, v, e)
}
