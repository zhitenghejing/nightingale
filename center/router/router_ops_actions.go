package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/alert/sender"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/ops"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	imodels "github.com/flashcatcloud/ibex/src/models"
)

func (rt *Router) opsPrepareTemplate(_ context.Context, user int64, c *models.OpsCase, a knowledge.Action) (interface{}, error) {
	if _, err := opsauth.Check(rt.Ctx.DB, user, c.GroupID, false); err != nil {
		return nil, err
	}
	tpl, err := models.TaskTplGetById(rt.Ctx, a.TemplateID)
	if err != nil {
		return nil, err
	}
	if tpl == nil {
		return nil, opsauth.Fail(404, "template_missing", "Task template not found")
	}
	if tpl.GroupId != c.GroupID || c.TargetIdent == "" {
		return nil, opsauth.Fail(403, "template_scope", "Template or target is outside the case scope")
	}
	var targets int64
	if err = rt.Ctx.DB.Model(&models.TargetBusiGroup{}).Where("target_ident = ? AND group_id = ?", c.TargetIdent, c.GroupID).Count(&targets).Error; err != nil {
		return nil, err
	}
	if targets == 0 {
		return nil, opsauth.Fail(403, "target_forbidden", "No access to this target")
	}
	if tpl.AuthLevel == 0 {
		return nil, opsauth.Fail(403, "template_disabled", "This task template is not enabled for AI operations")
	}
	return tpl, nil
}
func (rt *Router) opsExecute(ctx context.Context, user int64, c *models.OpsCase, a *models.OpsAction) (interface{}, error) {
	if _, err := opsauth.Check(rt.Ctx.DB, user, c.GroupID, true); err != nil {
		return nil, err
	}
	var targets int64
	if err := rt.Ctx.DB.Model(&models.TargetBusiGroup{}).Where("target_ident = ? AND group_id = ?", c.TargetIdent, c.GroupID).Count(&targets).Error; err != nil {
		return nil, err
	}
	if targets == 0 {
		return nil, opsauth.Fail(403, "target_forbidden", "Target no longer belongs to this business group")
	}
	payload, err := ops.ParseAction(a)
	if err != nil {
		return nil, err
	}
	if a.Kind == "demo_reclaim" {
		if !rt.Center.Ops.DemoEnable || ops.Attrs(c)["environment"] != "test" {
			return nil, opsauth.Fail(403, "demo_disabled", "Demo action is disabled")
		}
		return rt.opsDemoRequest(ctx, "/actions/reclaim", map[string]string{"operation_key": a.OperationKey, "ident": c.TargetIdent})
	}
	if a.Kind != "ibex_template" {
		return nil, opsauth.Fail(422, "action_kind", "Unsupported execution kind")
	}
	if !rt.Center.Ops.AllowExecution || !rt.Ibex.Enable {
		return nil, opsauth.Fail(503, "execution_disabled", "Production task execution is disabled")
	}
	operator, err := opsauth.User(rt.Ctx.DB, user)
	if err != nil {
		return nil, err
	}
	allowed, err := operator.CheckPerm(rt.Ctx, "/job-tasks/add")
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, opsauth.Fail(403, "task_forbidden", "No task dispatch permission")
	}
	if denied, e := operator.NopriIdents(rt.Ctx, []string{c.TargetIdent}); e != nil || len(denied) > 0 {
		return nil, opsauth.Fail(403, "target_forbidden", "No operation permission for this target")
	}
	latest, err := rt.opsPrepareTemplate(ctx, user, c, payload.Definition)
	if err != nil {
		return nil, err
	}
	expected, _ := json.Marshal(payload.Template)
	actual, _ := json.Marshal(latest)
	if !bytes.Equal(expected, actual) {
		return nil, opsauth.Fail(409, "template_changed", "Task template changed after proposal; prepare a new action")
	}
	tpl := latest.(*models.TaskTpl)
	u, err := opsauth.User(rt.Ctx.DB, user)
	if err != nil {
		return nil, err
	}
	f := models.TaskForm{Title: "ops:" + a.OperationKey, Account: tpl.Account, Batch: tpl.Batch, Tolerance: tpl.Tolerance, Timeout: tpl.Timeout, Pause: tpl.Pause, Script: tpl.Script, Args: tpl.Args, Action: "start", Creator: u.Username, AuthLevel: tpl.AuthLevel, SystemCaller: "ops:" + a.OperationKey, Hosts: []string{c.TargetIdent}}
	if err = f.Verify(); err != nil {
		return nil, err
	}
	id, err := sender.TaskAdd(f, u.Username, true)
	if err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, fmt.Errorf("Executor returned no task identity")
	}
	record := models.TaskRecord{Id: id, EventId: 0, GroupId: c.GroupID, Title: f.Title, Account: f.Account, Batch: f.Batch, Tolerance: f.Tolerance, Timeout: f.Timeout, Pause: f.Pause, Script: f.Script, Args: f.Args, AuthLevel: f.AuthLevel, SystemCaller: f.SystemCaller, CreateAt: time.Now().Unix(), CreateBy: u.Username}
	if err = record.Add(rt.Ctx); err != nil {
		return nil, err
	}
	return ops.PendingExecution{TaskID: id, OperationKey: a.OperationKey}, nil
}
func (rt *Router) opsDemoRequest(ctx context.Context, path string, body interface{}) (map[string]interface{}, error) {
	if !rt.Center.Ops.DemoEnable {
		return nil, opsauth.Fail(403, "demo_disabled", "Demo operations are disabled")
	}
	base, err := url.Parse(rt.Center.Ops.DemoURL)
	if err != nil || base.Scheme != "http" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || !net.ParseIP(base.Hostname()).IsLoopback() {
		return nil, fmt.Errorf("Demo URL must be an explicit loopback HTTP address")
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("Demo request failed with HTTP %d", response.StatusCode)
	}
	var v map[string]interface{}
	err = json.Unmarshal(data, &v)
	return v, err
}
func (rt *Router) opsProbe(ctx context.Context, c *models.OpsCase, kind string) (interface{}, bool, error) {
	v, err := rt.opsDemoRequest(ctx, "/probe", map[string]string{"kind": kind, "ident": c.TargetIdent})
	if err != nil {
		return nil, false, err
	}
	passed, _ := v["passed"].(bool)
	return v, passed, nil
}
func (rt *Router) opsReconcileTask(ctx context.Context, user int64, c *models.OpsCase, a *models.OpsAction) (string, interface{}, error) {
	if !rt.Ibex.Enable {
		return "", nil, opsauth.Fail(503, "ibex_disabled", "Ibex execution is disabled")
	}
	if _, err := opsauth.Check(rt.Ctx.DB, user, c.GroupID, false); err != nil {
		return "", nil, err
	}
	var tasks []imodels.TaskMeta
	if err := rt.Ctx.DB.WithContext(ctx).Where("system_caller = ?", "ops:"+a.OperationKey).Limit(2).Find(&tasks).Error; err != nil {
		return "", nil, err
	}
	if len(tasks) != 1 {
		return "unknown", map[string]interface{}{"reason": "No unique external task found; do not retry dispatch", "operation_key": a.OperationKey}, nil
	}
	task := tasks[0]
	rows := []imodels.TaskHost{}
	if err := rt.Ctx.DB.WithContext(ctx).Table(fmt.Sprintf("task_host_%d", task.Id%100)).Where("id = ? AND host = ?", task.Id, c.TargetIdent).Find(&rows).Error; err != nil {
		return "", nil, err
	}
	if len(rows) != 1 {
		return "submitted", map[string]interface{}{"task_id": task.Id, "reason": "Waiting for target execution result"}, nil
	}
	h := rows[0]
	state := "submitted"
	switch h.Status {
	case "success":
		state = "succeeded"
	case "failed", "killed", "killfailed", "timeout":
		state = "failed"
	}
	stdout, stderr := h.Stdout, h.Stderr
	if len(stdout) > 16384 {
		stdout = stdout[:16384] + " (truncated)"
	}
	if len(stderr) > 16384 {
		stderr = stderr[:16384] + " (truncated)"
	}
	return state, map[string]interface{}{"task_id": task.Id, "host": h.Host, "status": h.Status, "stdout": stdout, "stderr": stderr}, nil
}
