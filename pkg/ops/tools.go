package ops

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ccfos/nightingale/v6/pkg/knowledge"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"github.com/google/uuid"
)

// Tool intents survive worker termination; an intent without a matching result
// is deliberately an interrupted read, never evidence of successful execution.
func (s *Service) BeginTool(user, id int64, name string, args interface{}) (string, error) {
	if s.scope == nil {
		return "", opsauth.Fail(403, "run_required", "An active investigation scope is required")
	}
	if _, err := s.Get(user, id, false); err != nil {
		return "", err
	}
	call := uuid.NewString()
	err := s.audit(s.DB, id, user, "tool_started", map[string]interface{}{"call_id": call, "run_id": s.scope.RunID, "tool": name, "arguments": args})
	return call, err
}
func (s *Service) FinishTool(user, id int64, call, name, result string, toolErr error) error {
	state, message := "completed", ""
	if toolErr != nil {
		state = "failed"
		message = truncateBytes(toolErr.Error(), 1000)
	}
	return s.audit(s.DB, id, user, "tool_finished", map[string]interface{}{"call_id": call, "run_id": s.scope.RunID, "tool": name, "state": state, "error": message, "result_sha256": knowledge.Digest([]byte(result))})
}

// AgentContext excludes raw alert snapshots, task scripts, source attachments
// and audit payloads. Evidence still stays available through scoped tools.
func (s *Service) AgentContext(user, id int64) (interface{}, error) {
	d, err := s.Detail(user, id)
	if err != nil {
		return nil, err
	}
	attrs := map[string]string{}
	for k, v := range MatchAttrs(d.Case) {
		lower := strings.ToLower(k)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "authorization") {
			continue
		}
		attrs[k] = truncateBytes(v, 500)
	}
	evidence := []interface{}{}
	for i, e := range d.Evidence {
		if i >= 8 {
			break
		}
		evidence = append(evidence, EvidenceSummary(e))
	}
	actions := []interface{}{}
	for _, a := range d.Actions {
		actions = append(actions, map[string]interface{}{"id": a.ID, "state": a.State, "kind": a.Kind, "generation": a.Generation})
	}
	episodes := []interface{}{}
	for i, episode := range d.Episodes {
		if i >= 20 {
			break
		}
		var event models.AlertHisEvent
		_ = json.Unmarshal([]byte(episode.Snapshot), &event)
		episodes = append(episodes, map[string]interface{}{"incident_id": episode.IncidentID, "sequence": episode.Sequence, "recovered": episode.Recovered, "event_id": event.Id, "trigger_time": event.TriggerTime, "last_eval_time": event.LastEvalTime, "trigger_value": event.TriggerValue, "severity": event.Severity})
	}
	var spec interface{}
	if d.Case.KnowledgeVersionID > 0 {
		_, v, e := s.Knowledge.Version(user, d.Case.KnowledgeVersionID)
		if e != nil {
			return nil, e
		}
		spec = v
	}
	var plan interface{}
	if d.Case.PlanID > 0 {
		for _, p := range d.Plans {
			if p.ID == d.Case.PlanID {
				plan = p
				var v knowledge.Spec
				if json.Unmarshal([]byte(p.Payload), &v) == nil {
					spec = v
				}
				break
			}
		}
	}
	readiness, e := s.ActionReadiness(user, id)
	if e != nil {
		readiness = []map[string]interface{}{{"ready": false, "blocked_reason": e.Error()}}
	}
	return map[string]interface{}{"action_readiness": readiness, "case_id": d.Case.ID, "title": d.Case.Title, "state": d.Case.State, "severity": d.Case.Severity, "wait_reason": d.Case.WaitReason, "rule_inputs": s.RuleInputs(d.Case), "episodes": episodes, "group_id": d.Case.GroupID, "owner_id": d.Case.OwnerID, "target_ident": d.Case.TargetIdent, "datasource_id": d.Case.DatasourceID, "rule_id": d.Case.RuleID, "generation": d.Case.Generation, "revision": d.Case.Revision, "context": attrs, "knowledge_version_id": d.Case.KnowledgeVersionID, "knowledge": spec, "plan_id": d.Case.PlanID, "case_plan": plan, "plan_versions": planSummaries(d.Plans), "evidence": evidence, "steps": d.Steps, "actions": actions, "verifications": d.Verifications, "capabilities": map[string]interface{}{"metrics": s.Query != nil && d.Case.TargetIdent != "" && d.Case.DatasourceID > 0, "alert_evaluation_logs": s.Logs != nil && d.Case.RuleID > 0, "application_logs": "manual evidence required until a scoped log adapter is configured", "host_agent": "not assumed", "confirm_plan_approve_execute_close": "human operator only"}}, nil
}
func (s *Service) CollectLogs(ctx context.Context, user, id int64) (*models.OpsEvidence, error) {
	c, err := s.Get(user, id, false)
	if err != nil {
		return nil, err
	}
	if c.State == "closed" {
		return nil, opsauth.Fail(409, "case_closed", "Case is closed")
	}
	if s.Logs == nil || c.RuleID <= 0 {
		return nil, opsauth.Fail(422, "logs_unavailable", "Scoped alert evaluation logs are unavailable")
	}
	payload, status, err := s.Logs(ctx, user, c)
	if err != nil {
		status = "error"
		payload = map[string]string{"error": err.Error()}
	}
	return s.persistEvidence(c, user, "alert_evaluation_logs", "alert_engine", status, 0, 0, 0, payload)
}

func planSummaries(plans []models.OpsPlan) []interface{} {
	out := []interface{}{}
	for i, p := range plans {
		if i >= 5 {
			break
		}
		out = append(out, map[string]interface{}{"id": p.ID, "state": p.State, "title": p.Title, "sequence": p.Sequence})
	}
	return out
}
