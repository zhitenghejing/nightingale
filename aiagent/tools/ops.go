package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/pkg/ops"
)

func opsDef(name, description string, params ...aiagent.ToolParameter) aiagent.AgentTool {
	return aiagent.AgentTool{Name: name, Description: description, Type: aiagent.ToolTypeBuiltin, Parameters: params}
}
func init() {
	registerOps(opsDef("propose_ops_plan", "Propose an immutable case-local DRAFT using current evidence. Human confirmation is required. plan_json is a JSON object with revision,title,scope,rationale,evidence_ids,spec; spec has summary,conditions,steps,actions,checks,escalation. Use only manual/metric checks and manual or existing fixed-template actions. Leave publication dates, source_ids and rule_ids empty.", aiagent.ToolParameter{Name: "plan_json", Type: "string", Required: true}), opsPlan)

	registerOps(opsDef("get_ops_context", "Get the current authorized operations case, evidence and current knowledge version; no other case can be accessed."), opsContext)
	registerOps(opsDef("search_ops_knowledge", "Search published authorized knowledge once using the current case rule and context, returning applicability conditions. This tool has no keyword parameter; repeated keyword arguments do not change results."), opsKnowledge)
	registerOps(opsDef("select_ops_knowledge", "Adopt an applicable published version for this case. Never changes an operator-adopted version.", aiagent.ToolParameter{Name: "version_id", Type: "integer", Required: true}), opsSelect)
	registerOps(opsDef("collect_ops_metric", "Query metrics only for this case's authorized target and datasource; stores source evidence.", aiagent.ToolParameter{Name: "query", Type: "string", Required: true}, aiagent.ToolParameter{Name: "window_seconds", Type: "integer"}), opsMetric)
	registerOps(opsDef("record_ops_step_result", "Record a result for a knowledge step once using evidence IDs returned by tools. Use state=completed for a successful check. The returned ready_action_keys identify the next proposal; do not try alternative state names after success.", aiagent.ToolParameter{Name: "key", Type: "string", Required: true}, aiagent.ToolParameter{Name: "state", Type: "string", Description: "Exactly completed, failed, or skipped. A successful metric collection uses completed, never success/pass/ok/done. Do not re-record a completed step.", Enum: []string{"completed", "failed", "skipped"}, Required: true}, aiagent.ToolParameter{Name: "note", Type: "string"}, aiagent.ToolParameter{Name: "evidence_ids", Type: "array", ItemType: "integer", Required: true}), opsStepResult)
	registerOps(opsDef("propose_ops_action", "Prepare a fixed knowledge action for human approval. This tool cannot approve or execute it.", aiagent.ToolParameter{Name: "key", Type: "string", Required: true}), opsAction)
	registerOps(opsDef("collect_ops_eval_logs", "Collect evaluation logs for the current case rule; stores partial/empty/error status and evidence."), opsLogs)
	registerOps(opsDef("run_ops_verification", "Collect current evidence and run server-side recovery checks. Manual checks still require an operator."), opsVerify)
}
func opsDeps(d *aiagent.ToolDeps) (*ops.Service, ops.Scope, error) {
	if d == nil || d.Ops == nil || d.OpsScope == nil {
		return nil, ops.Scope{}, fmt.Errorf("operations investigation context is not configured")
	}
	return d.Ops, *d.OpsScope, nil
}
func opsJSON(v interface{}, err error) (string, error) {
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	return string(b), err
}
func opsContext(_ context.Context, d *aiagent.ToolDeps, _ map[string]interface{}, _ map[string]string) (string, error) {
	s, p, e := opsDeps(d)
	if e != nil {
		return "", e
	}
	v, e := s.AgentContext(p.UserID, p.CaseID)
	return opsJSON(v, e)
}
func opsKnowledge(_ context.Context, d *aiagent.ToolDeps, _ map[string]interface{}, _ map[string]string) (string, error) {
	s, p, e := opsDeps(d)
	if e != nil {
		return "", e
	}
	v, e := s.Matches(p.UserID, p.CaseID)
	return opsJSON(v, e)
}
func opsSelect(_ context.Context, d *aiagent.ToolDeps, a map[string]interface{}, _ map[string]string) (string, error) {
	s, p, e := opsDeps(d)
	if e != nil {
		return "", e
	}
	c, e := s.Get(p.UserID, p.CaseID, false)
	if e != nil {
		return "", e
	}
	e = s.SelectKnowledge(p.UserID, p.CaseID, c.Revision, getArgInt64(a, "version_id"), "")
	return opsJSON(map[string]string{"status": "adopted"}, e)
}
func opsMetric(ctx context.Context, d *aiagent.ToolDeps, a map[string]interface{}, _ map[string]string) (string, error) {
	s, p, e := opsDeps(d)
	if e != nil {
		return "", e
	}
	v, e := s.CollectMetric(ctx, p.UserID, p.CaseID, getArgString(a, "query"), int64(getArgInt(a, "window_seconds", 0)))
	if e != nil {
		return "", e
	}
	return opsJSON(ops.EvidenceSummary(*v), nil)
}
func opsStepResult(_ context.Context, d *aiagent.ToolDeps, a map[string]interface{}, _ map[string]string) (string, error) {
	s, p, e := opsDeps(d)
	if e != nil {
		return "", e
	}
	raw, e := json.Marshal(a["evidence_ids"])
	if e != nil {
		return "", e
	}
	ids := []int64{}
	if e = json.Unmarshal(raw, &ids); e != nil {
		return "", e
	}
	e = s.RecordAgentStep(p.UserID, p.CaseID, getArgString(a, "key"), getArgString(a, "state"), getArgString(a, "note"), ids)
	if e != nil {
		return "", e
	}
	readiness, e := s.ActionReadiness(p.UserID, p.CaseID)
	if e != nil {
		return "", e
	}
	keys := []string{}
	for _, r := range readiness {
		if r["ready"] == true {
			keys = append(keys, r["key"].(string))
		}
	}
	return opsJSON(map[string]interface{}{"status": "recorded", "step_key": getArgString(a, "key"), "state": getArgString(a, "state"), "evidence_ids": ids, "ready_action_keys": keys, "next": "Do not change the completed state. Propose a ready action key for human confirmation; otherwise continue only unresolved checks."}, nil)
}
func opsAction(ctx context.Context, d *aiagent.ToolDeps, a map[string]interface{}, _ map[string]string) (string, error) {
	s, p, e := opsDeps(d)
	if e != nil {
		return "", e
	}
	v, e := s.ProposeAction(ctx, p.UserID, p.CaseID, getArgString(a, "key"))
	if e != nil {
		return "", e
	}
	payload, _ := ops.ParseAction(v)
	return opsJSON(map[string]interface{}{"action_id": v.ID, "state": v.State, "kind": v.Kind, "title": payload.Definition.Title, "target": payload.Target, "expires_at": v.ExpiresAt, "next": "human approval required"}, nil)
}
func opsVerify(ctx context.Context, d *aiagent.ToolDeps, _ map[string]interface{}, _ map[string]string) (string, error) {
	s, p, e := opsDeps(d)
	if e != nil {
		return "", e
	}
	v, e := s.Verify(ctx, p.UserID, p.CaseID)
	return opsJSON(v, e)
}

var OpsToolNames = []string{"propose_ops_plan", "get_ops_context", "search_ops_knowledge", "select_ops_knowledge", "collect_ops_metric", "record_ops_step_result", "propose_ops_action", "run_ops_verification", "collect_ops_eval_logs"}

func registerOps(def aiagent.AgentTool, handler aiagent.BuiltinToolFunc) {
	register(def, func(ctx context.Context, d *aiagent.ToolDeps, args map[string]interface{}, params map[string]string) (string, error) {
		s, p, err := opsDeps(d)
		if err != nil {
			return "", err
		}
		call, err := s.BeginTool(p.UserID, p.CaseID, def.Name, args)
		if err != nil {
			return "", err
		}
		result, runErr := handler(ctx, d, args, params)
		if err = s.FinishTool(p.UserID, p.CaseID, call, def.Name, result, runErr); err != nil {
			return "", err
		}
		if runErr == nil && (def.Name == "propose_ops_action" || def.Name == "propose_ops_plan") {
			return "", &aiagent.ToolInterrupt{Kind: aiagent.InterruptKindInput, Prompt: "已保存待人员确认的处置方案或操作提案。本轮排查已暂停，后续采集与验证将在人员决定并核实操作结果后继续。"}
		}
		return result, runErr
	})
}
func opsLogs(ctx context.Context, d *aiagent.ToolDeps, _ map[string]interface{}, _ map[string]string) (string, error) {
	s, p, err := opsDeps(d)
	if err != nil {
		return "", err
	}
	v, err := s.CollectLogs(ctx, p.UserID, p.CaseID)
	return opsJSON(v, err)
}

func opsPlan(_ context.Context, d *aiagent.ToolDeps, a map[string]interface{}, _ map[string]string) (string, error) {
	service, scope, err := opsDeps(d)
	if err != nil {
		return "", err
	}
	var in ops.PlanInput
	if err = json.Unmarshal([]byte(getArgString(a, "plan_json")), &in); err != nil {
		return "", err
	}
	return opsJSON(service.ProposePlan(scope.UserID, scope.CaseID, in))
}
