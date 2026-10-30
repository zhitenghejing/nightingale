package router

import (
	"github.com/ccfos/nightingale/v6/pkg/i18nx"
	"net/http"
	"strconv"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/externalpush"
	"github.com/ccfos/nightingale/v6/pkg/ginx"
	"github.com/gin-gonic/gin"
)

func (rt *Router) externalPushRegister(c *gin.Context) {
	var in externalpush.DispatchSubmission
	ginx.BindJSON(c, &in)
	id, err := externalpush.RegisterDispatch(rt.Ctx, in)
	ginx.NewRender(c).Data(id, err)
}
func (rt *Router) externalPushTasks(c *gin.Context) {
	tasks := []models.ExternalPushTask{}
	q := models.DB(rt.Ctx)
	if state := c.Query("state"); state != "" {
		q = q.Where("state = ?", state)
	}
	if channel := c.Query("channel_id"); channel != "" {
		id, err := strconv.ParseInt(channel, 10, 64)
		if err != nil {
			bombI18n(c, http.StatusBadRequest, "invalid channel_id")
		}
		q = q.Where("channel_id = ?", id)
	}
	before, _ := strconv.ParseInt(c.Query("before"), 10, 64)
	if before > 0 {
		q = q.Where("id < ?", before)
	}
	err := q.Order("id desc").Limit(100).Find(&tasks).Error
	for i := range tasks {
		tasks[i].Reason = externalPushText(c, tasks[i].Reason)
	}
	ginx.NewRender(c).Data(tasks, err)
}
func (rt *Router) externalPushDetail(c *gin.Context) {
	d, err := externalpush.Get(models.DB(rt.Ctx), ginx.UrlParamInt64(c, "id"))
	d.Task.Reason = externalPushText(c, d.Task.Reason)
	for i := range d.Attempts {
		d.Attempts[i].Summary = externalPushText(c, d.Attempts[i].Summary)
	}
	for i := range d.Operations {
		if d.Operations[i].Actor == "system" {
			d.Operations[i].Reason = externalPushText(c, d.Operations[i].Reason)
		}
	}
	ginx.NewRender(c).Data(d, err)
}
func (rt *Router) externalPushAction(c *gin.Context) {
	var a externalpush.Action
	ginx.BindJSON(c, &a)
	me := c.MustGet("user").(*models.User)
	err := externalpush.Act(models.DB(rt.Ctx), ginx.UrlParamInt64(c, "id"), me.Username, a)
	if err != nil {
		bombI18n(c, http.StatusConflict, err.Error())
	}
	ginx.NewRender(c).Message(nil)
}

// Localize persisted machine reasons only at the presentation edge. Keep the
// durable audit stable and retain technical HTTP/hash evidence verbatim.
func externalPushText(c *gin.Context, text string) string {
	lang := c.GetHeader("X-Language")
	translated := i18nx.Translate(lang, text)
	if translated != text || !strings.HasPrefix(text, "HTTP ") {
		return translated
	}
	parts := strings.Split(text, "; ")
	for i := range parts {
		parts[i] = i18nx.Translate(c.GetHeader("X-Language"), parts[i])
	}
	return strings.Join(parts, "; ")
}

// Querying readiness reads only local files. It never sends an alarm, returns
// file paths/key material, or writes delivery state.
func (rt *Router) externalPushSigningKey(c *gin.Context) {
	ref := c.Query("reference")
	if ref == "" {
		ref = models.ExternalPushDefaultKeyRef
	}
	if len(ref) > 64 {
		bombI18n(c, http.StatusBadRequest, "invalid signing key reference")
	}
	status := externalpush.NewFileKeys(rt.Center.ExternalPushKeyFiles).Status(ref)
	status.Reason = externalPushText(c, status.Reason)
	ginx.NewRender(c).Data(status, nil)
}
