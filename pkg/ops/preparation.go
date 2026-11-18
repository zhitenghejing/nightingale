package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"strings"
)

// RuleInputs contains only query definitions, never callbacks or credentials.
// Prefer the pinned event definition so edits to a rule cannot rewrite history.
func (s *Service) RuleInputs(c *models.OpsCase) map[string]interface{} {
	expressions := []string{}
	origin := "unavailable"
	var episode models.OpsEpisode
	found := s.DB.Where("case_id = ?", c.ID).Order("id DESC").Limit(1).Find(&episode)
	if found.Error == nil && found.RowsAffected > 0 {
		var event struct {
			PromQL string                `json:"prom_ql"`
			Config models.PromRuleConfig `json:"rule_config"`
		}
		if json.Unmarshal([]byte(episode.Snapshot), &event) == nil {
			expressions = append(expressions, event.PromQL, event.Config.PromQl)
			for _, q := range event.Config.Queries {
				expressions = append(expressions, q.PromQl)
			}
		}
		origin = "alert_snapshot"
	}
	nonempty := false
	for _, q := range expressions {
		nonempty = nonempty || strings.TrimSpace(q) != ""
	}
	if !nonempty && c.RuleID > 0 {
		var rule models.AlertRule
		if s.DB.Where("id = ? AND group_id = ?", c.RuleID, c.GroupID).First(&rule).Error == nil {
			var cfg models.PromRuleConfig
			_ = json.Unmarshal([]byte(rule.RuleConfig), &cfg)
			expressions = append(expressions, rule.PromQl, cfg.PromQl)
			for _, q := range cfg.Queries {
				expressions = append(expressions, q.PromQl)
			}
			origin = "current_rule_fallback"
		}
	}
	queries := []string{}
	selectors := []string{}
	seen := map[string]bool{}
	vectors := map[string]bool{}
	for _, raw := range expressions {
		raw = strings.TrimSpace(raw)
		if raw == "" || len(raw) > 16000 || seen[raw] || len(queries) >= 5 {
			continue
		}
		seen[raw] = true
		queries = append(queries, raw)
		node, err := parser.ParseExpr(raw)
		if err != nil || c.TargetIdent == "" {
			continue
		}
		parser.Inspect(node, func(n parser.Node, _ []parser.Node) error {
			v, ok := n.(*parser.VectorSelector)
			if !ok || len(selectors) >= 2 || v.OriginalOffset != 0 || v.Timestamp != nil || v.StartOrEnd != 0 {
				return nil
			}
			// Only named selectors from the alert, with a single bound ident. Never
			// enumerate every metric on the datasource to guess an unknown name.
			if v.Name == "" {
				return nil
			}
			for _, m := range v.LabelMatchers {
				if m.Name == "ident" && (m.Type != labels.MatchEqual || m.Value != c.TargetIdent) {
					return nil
				}
			}
			copy := *v
			copy.LabelMatchers = append(append([]*labels.Matcher{}, v.LabelMatchers...), labels.MustNewMatcher(labels.MatchEqual, "ident", c.TargetIdent))
			copy.OriginalOffset = 0
			copy.Offset = 0
			copy.Timestamp = nil
			copy.StartOrEnd = 0
			q := copy.String()
			if !vectors[q] {
				selectors = append(selectors, q)
				vectors[q] = true
			}
			return nil
		})
	}
	return map[string]interface{}{"origin": origin, "expressions": queries, "diagnostic_queries": selectors}
}

type PlanPreparation struct {
	DiagnosticQueries []string
	Revision          int64
	Generation        int64
	EvidenceIDs       []int64
	Context           interface{}
}

// PreparePlan performs at most two scoped metric reads from actual alert
// selectors, then hands the facts to a no-tools draft generation phase.
func (s *Service) PreparePlan(ctx context.Context, user, id int64) (*PlanPreparation, error) {
	c, err := s.Get(user, id, true)
	if err != nil {
		return nil, err
	}
	if c.KnowledgeVersionID > 0 || c.PlanID > 0 {
		return nil, opsauth.Fail(409, "procedure_changed", "Case already has an adopted procedure")
	}
	inputs := s.RuleInputs(c)
	contextEvidence, err := s.persistEvidence(c, user, "case_context", "platform", "ok", s.now(), 0, 0, map[string]interface{}{"fact": "Case and alert identifiers supplied by the platform; this is not proof of root cause or recovery", "target_ident": c.TargetIdent, "rule_id": c.RuleID, "rule_inputs": inputs})
	if err != nil {
		return nil, err
	}
	facts := []interface{}{EvidenceSummary(*contextEvidence)}
	ids := []int64{contextEvidence.ID}
	if queries, ok := inputs["diagnostic_queries"].([]string); ok && s.Query != nil {
		for _, q := range queries {
			evidence, e := s.CollectMetric(ctx, user, id, q, 60)
			if e != nil {
				err = e
				return nil, err
			}
			facts = append(facts, EvidenceSummary(*evidence))
			if evidence.Status == "ok" {
				ids = append(ids, evidence.ID)
			}
		}
	}
	current, err := s.Get(user, id, true)
	if err != nil {
		return nil, err
	}
	if current.Generation != c.Generation || current.Revision != c.Revision {
		return nil, opsauth.Fail(409, "procedure_changed", "Case changed during preparation")
	}
	rows := []models.OpsEvidence{}
	if err = s.DB.Where("case_id = ? AND generation = ? AND status = ?", id, c.Generation, "ok").Order("id DESC").Limit(10).Find(&rows).Error; err != nil {
		return nil, err
	}
	attrs := map[string]string{}
	for key, value := range MatchAttrs(c) {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "authorization") {
			continue
		}
		attrs[key] = truncateBytes(value, 500)
	}
	for _, e := range rows {
		if e.Kind == "manual" && e.CollectedAt >= s.now()-300 && len(facts) < 5 {
			facts = append(facts, EvidenceSummary(e))
			ids = append(ids, e.ID)
		}
	}
	runID := int64(0)
	if s.scope != nil {
		runID = s.scope.RunID
	}
	if err = s.audit(s.DB, id, user, "bounded_preparation", map[string]interface{}{"run_id": runID, "diagnostic_query_limit": 2, "evidence_ids": ids}); err != nil {
		return nil, err
	}
	context := map[string]interface{}{"case_id": c.ID, "title": c.Title, "server_time": s.now(), "generation": c.Generation, "target_ident": c.TargetIdent, "rule_id": c.RuleID, "datasource_id": c.DatasourceID, "rule_inputs": inputs, "context": attrs, "evidence": facts, "unknowns": "Application version, business impact, change history and root cause require operator confirmation unless explicitly evidenced"}

	diagnostics, _ := inputs["diagnostic_queries"].([]string)
	return &PlanPreparation{DiagnosticQueries: diagnostics, Revision: c.Revision, Generation: c.Generation, EvidenceIDs: ids, Context: context}, nil
}

// OutlinePlan is an explicitly unverified investigation outline if model output
// is unavailable. It contains no repair operation and cannot bypass a human.
func (s *Service) OutlinePlan(p *PlanPreparation) PlanInput {
	return PlanInput{Revision: p.Revision, Title: "待人工完善的本次排查方案", Scope: "仅本次处置已绑定目标与授权数据源；不授权任何修复操作", Rationale: "已保存平台上下文与实际工具结果；故障根因、业务影响和修复方法尚未确认。模型草稿不可用，以下仅为系统排查提纲。", EvidenceIDs: p.EvidenceIDs, Spec: knowledge.Spec{Summary: "核对现场证据和未知项，由人员完善本次方案后再决定处置", Steps: []knowledge.Step{{Key: "review_facts", Title: "人工核对现场证据、业务影响及近期变更", Kind: "manual", Instruction: "检查已保存证据，补充应用版本、业务影响与变更；不要把假设记为事实。", Expected: "事实、假设、未知项分别记录"}}, Actions: []knowledge.Action{}, Checks: []knowledge.Check{{Key: "business", Title: "人员核实本次业务功能及数据完整性恢复", Kind: "manual", Required: true, FreshSeconds: 300}}, Escalation: "缺少适用方法、关键证据或业务验证时停止操作，交本业务组负责人进一步排查；此提纲不证明恢复"}}
}

func (s *Service) SavePreparedPlan(user, id int64, p *PlanPreparation, in PlanInput, origin string) (*models.OpsPlan, error) {
	current, err := s.Get(user, id, true)
	if err != nil {
		return nil, err
	}
	if current.Generation != p.Generation || current.Revision != p.Revision {
		return nil, opsauth.Fail(409, "procedure_changed", "Case changed while drafting; collect current facts again")
	}
	if _, ok := knowledge.Conditions(in.Spec, MatchAttrs(current)); !ok {
		return nil, opsauth.Fail(422, "plan_conditions", "Required plan conditions are missing or do not match")
	}
	allowed := map[string]bool{}
	for _, q := range p.DiagnosticQueries {
		allowed[strings.TrimSpace(q)] = true
	}
	for _, step := range in.Spec.Steps {
		if step.Kind == "metric" && !allowed[strings.TrimSpace(step.Query)] {
			return nil, opsauth.Fail(422, "plan_query", "Automatic plan metric queries must come from the observed alert selectors")
		}
	}
	for _, check := range in.Spec.Checks {
		if check.Kind == "metric" && !allowed[strings.TrimSpace(check.Query)] {
			return nil, opsauth.Fail(422, "plan_query", "Automatic plan metric queries must come from the observed alert selectors")
		}
	}
	in.Revision = p.Revision
	in.EvidenceIDs = p.EvidenceIDs
	in.Spec.ValidatedAt = 0
	in.Spec.ReviewAt = 0
	in.Spec.SourceIDs = nil
	in.Spec.RuleIDs = nil
	// An exploratory model plan is not authority to dispatch a task template.
	for _, action := range in.Spec.Actions {
		if action.Kind != "manual" {
			return nil, opsauth.Fail(422, "plan_manual_actions", "An automatically drafted new-fault plan may only suggest manual actions")
		}
	}
	business := false
	for _, c := range in.Spec.Checks {
		if c.Kind == "manual" && c.Required {
			business = true
		}
	}
	if !business {
		in.Spec.Checks = append(in.Spec.Checks, knowledge.Check{Key: "operator_business_review", Title: "人员核实业务恢复及数据完整性", Kind: "manual", Required: true, FreshSeconds: 300})
	}
	plan, err := s.ProposePlan(user, id, in)
	if err != nil {
		return nil, err
	}
	if err = s.audit(s.DB, id, user, "plan_draft_origin", map[string]interface{}{"plan_id": plan.ID, "origin": origin}); err != nil {
		return nil, err
	}
	return plan, nil
}

func EvidenceSummary(e models.OpsEvidence) map[string]interface{} {
	out := map[string]interface{}{"id": e.ID, "generation": e.Generation, "kind": e.Kind, "status": e.Status, "observed_at": e.ObservedAt, "collected_at": e.CollectedAt, "sha256": e.SHA256}
	var metric MetricResult
	if e.Kind == "metric" && json.Unmarshal([]byte(e.Payload), &metric) == nil {
		out["query"] = metric.RequestedQuery
		out["reason"] = truncateBytes(metric.Reason, 400)
		out["sample_count"] = len(metric.Points)
		if n := len(metric.Points); n > 0 {
			out["first_sample"] = metric.Points[0]
			out["latest_sample"] = metric.Points[n-1]
		}
	} else if e.Kind == "manual" {
		var payload struct {
			Note string `json:"note"`
		}
		if json.Unmarshal([]byte(e.Payload), &payload) == nil {
			out["note"] = truncateBytes(payload.Note, 1200)
		}
	} else if e.Kind == "case_context" {
		out["note"] = "Platform case and source query context; not evidence of root cause or successful recovery"
	}
	return out
}

func (s *Service) HandoffSummary(user, id int64, lead string) string {
	d, err := s.Detail(user, id)
	if err != nil {
		return "排查已停止，请重新核对处置权限后由值班人员接管。"
	}
	lines := []string{truncateBytes(lead, 2000), fmt.Sprintf("处置 #%d，目标 %s，当前阶段 %d。", id, d.Case.TargetIdent, d.Case.Generation)}
	count := 0
	for _, e := range d.Evidence {
		if e.Generation != d.Case.Generation || count >= 5 {
			continue
		}
		lines = append(lines, fmt.Sprintf("证据 #%d：%s，状态 %s；观测时间 %d。", e.ID, e.Kind, e.Status, e.ObservedAt))
		count++
	}
	lines = append(lines, "已保存证据不等于根因或业务恢复。下一步：由本业务组值班人员认领，核对缺失资料、待确认方案/操作及业务验证。")
	return strings.Join(lines, "\n")
}

// RecordModel stores only non-secret identity and limits, never API keys,
// custom header values, proxy credentials, or raw provider responses.
func (s *Service) RecordModel(user, id, configID int64, model, provider string) error {
	if s.scope == nil {
		return opsauth.Fail(403, "run_required", "An active investigation scope is required")
	}
	if _, err := s.Get(user, id, false); err != nil {
		return err
	}
	return s.audit(s.DB, id, user, "investigation_model", map[string]interface{}{"run_id": s.scope.RunID, "configuration_id": configID, "model": model, "provider": provider, "prompt_version": "ops-reliability-v2", "max_tool_calls": 12, "run_timeout_seconds": 120})
}
