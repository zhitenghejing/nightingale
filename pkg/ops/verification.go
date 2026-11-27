package ops

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"gorm.io/gorm"
)

func compare(v float64, op string, want float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	switch op {
	case "<":
		return v < want
	case "<=":
		return v <= want
	case ">":
		return v > want
	case ">=":
		return v >= want
	case "==":
		return v == want
	case "!=":
		return v != want
	}
	return false
}
func (s *Service) Verify(ctx context.Context, user, id int64) ([]models.OpsVerification, error) {
	c, err := s.Get(user, id, s.scope == nil)
	if err != nil {
		return nil, err
	}
	if c.State == "closed" {
		return nil, opsauth.Fail(409, "case_closed", "Case is closed")
	}
	spec, err := s.activeProcedure(user, c)
	if err != nil {
		return nil, err
	}
	if err = s.actionUnresolved(c); err != nil {
		return nil, err
	}
	actionAt, err := s.recoveryBaselineAt(c)
	if err != nil {
		return nil, err
	}
	out := []models.OpsVerification{}
	for _, check := range spec.Checks {
		if check.Kind == "manual" {
			continue
		}
		state, reason := "pending", "Verification evidence is unavailable"
		ids := []int64{}
		if check.Kind == "metric" {
			e, eerr := s.CollectMetric(ctx, user, id, check.Query, check.WindowSeconds)
			if eerr != nil {
				reason = eerr.Error()
			} else {
				ids = append(ids, e.ID)
				state, reason = s.evaluateMetric(e, check, actionAt)
			}
		} else if s.DemoEnabled && spec.TestOnly && Attrs(c)["environment"] == "test" && s.Probe != nil {
			payload, ok, perr := s.Probe(ctx, c, check.Kind)
			status := "ok"
			if perr != nil {
				status = "error"
				payload = map[string]string{"error": perr.Error()}
			}
			e, eerr := s.persistEvidence(c, user, check.Kind, "demo_probe", status, s.now(), 0, 0, payload)
			if eerr != nil {
				return nil, eerr
			}
			ids = append(ids, e.ID)
			if perr != nil {
				reason = perr.Error()
			} else if ok {
				state = "passed"
				reason = "Business probe and independent test oracle passed"
			} else {
				state = "failed"
				reason = "Business probe or data reconciliation failed"
			}
		}
		v := models.OpsVerification{CaseID: id, Generation: c.Generation, VersionID: c.KnowledgeVersionID, PlanID: c.PlanID, CheckKey: check.Key, State: state, Reason: reason, EvidenceIDs: opsauth.JSON(ids), CreatedAt: s.now()}
		err = s.DB.Transaction(func(tx *gorm.DB) error {
			current, err := s.get(tx, user, id, s.scope == nil)
			if err != nil {
				return err
			}
			if current.Generation != c.Generation || current.KnowledgeVersionID != c.KnowledgeVersionID || current.PlanID != c.PlanID || current.State == "closed" {
				return opsauth.Fail(409, "verification_changed", "Case changed during verification")
			}
			return tx.Create(&v).Error
		})
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		current, err := s.get(tx, user, id, s.scope == nil)
		if err != nil {
			return err
		}
		if current.Generation != c.Generation {
			return opsauth.Fail(409, "verification_changed", "Case changed during verification")
		}
		return s.change(tx, current, current.Revision, map[string]interface{}{"state": "verifying", "wait_reason": ""})
	})
	return out, err
}
func (s *Service) evaluateMetric(e *models.OpsEvidence, c knowledge.Check, actionAt int64) (string, string) {
	if e.Status != "ok" {
		return "pending", "Metric evidence status is " + e.Status
	}
	var r MetricResult
	if err := json.Unmarshal([]byte(e.Payload), &r); err != nil || len(r.Points) == 0 {
		return "pending", "Metric evidence is empty or invalid"
	}
	now := s.now()
	if e.ObservedAt < now-c.FreshSeconds || e.ObservedAt > now+5 {
		return "pending", "Metric observation is stale or in the future"
	}
	if actionAt > 0 && e.WindowStart < actionAt {
		return "pending", "Observation window includes time before the latest action"
	}
	first, last := r.Points[0].At, r.Points[len(r.Points)-1].At
	if c.WindowSeconds > 0 {
		if first > e.WindowStart+c.FreshSeconds || last < e.WindowEnd-c.FreshSeconds {
			return "pending", "Observation window has incomplete coverage"
		}
		for i := 1; i < len(r.Points); i++ {
			if r.Points[i].At-r.Points[i-1].At > c.FreshSeconds {
				return "pending", "Observation window has missing samples"
			}
		}
	}
	for _, p := range r.Points {
		if p.ObservedAt > 0 && (p.At-p.ObservedAt > c.FreshSeconds || p.ObservedAt > p.At+5) {
			return "pending", "Underlying metric samples are stale or in the future"
		}
		if p.At > now+5 || !compare(p.Value, c.Operator, c.Threshold) {
			return "failed", "Metric does not meet the recovery comparison throughout the window"
		}
	}
	return "passed", "All fresh samples in the required window meet the recovery comparison"
}
func (s *Service) ConfirmCheck(user, id int64, key, note string, passed bool) error {
	if s.scope != nil {
		return opsauth.Fail(403, "manual_only", "Manual verification requires an operator")
	}
	if strings.TrimSpace(note) == "" {
		return opsauth.Fail(422, "confirmation_required", "Business verification evidence is required")
	}
	c, err := s.Get(user, id, true)
	if err != nil {
		return err
	}
	if c.State == "closed" {
		return opsauth.Fail(409, "case_closed", "Case is closed")
	}
	spec, err := s.activeProcedure(user, c)
	if err != nil {
		return err
	}
	found := false
	for _, check := range spec.Checks {
		if check.Key == key && check.Kind == "manual" {
			found = true
		}
	}
	if !found {
		return opsauth.Fail(422, "manual_check", "Only manual checks can be confirmed by an operator")
	}
	e, err := s.AddManualEvidence(user, id, note)
	if err != nil {
		return err
	}
	state := "failed"
	if passed {
		state = "passed"
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		current, err := s.get(tx, user, id, true)
		if err != nil {
			return err
		}
		if current.Generation != c.Generation {
			return opsauth.Fail(409, "verification_changed", "Case changed during confirmation")
		}
		v := models.OpsVerification{CaseID: id, Generation: c.Generation, VersionID: c.KnowledgeVersionID, PlanID: c.PlanID, CheckKey: key, State: state, Reason: note, EvidenceIDs: opsauth.JSON([]int64{e.ID}), ConfirmedBy: user, CreatedAt: s.now()}
		if err = tx.Create(&v).Error; err != nil {
			return err
		}
		return s.audit(tx, id, user, "manual_verification", map[string]interface{}{"check": key, "state": state})
	})
}
func (s *Service) Close(user, id, revision int64, kind, summary, followup string) error {
	if s.scope != nil {
		return opsauth.Fail(403, "human_close", "Only an operator can close a case")
	}
	if strings.TrimSpace(summary) == "" || len(summary) > 16000 || len(followup) > 16000 {
		return opsauth.Fail(422, "summary_required", "A bounded resolution summary is required")
	}
	switch kind {
	case "resolved", "temporary", "self_recovered", "monitoring_issue", "false_positive", "cancelled":
	default:
		return opsauth.Fail(422, "close_type", "Unsupported resolution type")
	}
	if kind == "temporary" && strings.TrimSpace(followup) == "" {
		return opsauth.Fail(422, "followup_required", "Temporary recovery requires a follow-up owner and task")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		c, err := s.get(tx, user, id, true)
		if err != nil {
			return err
		}
		fork := s.withDB(tx)
		if err = fork.actionUnresolved(c); err != nil {
			return err
		}
		if kind != "cancelled" {
			var active int64
			if err = tx.Model(&models.OpsEpisode{}).Where("case_id = ? AND recovered = ?", id, false).Count(&active).Error; err != nil {
				return err
			}
			if c.TargetIdent != "" && c.RuleID > 0 {
				if err = tx.Table("ops_episode AS episode").Joins("JOIN ops_case AS related ON related.id = episode.case_id").Where("related.group_id = ? AND related.target_ident = ? AND related.datasource_id = ? AND related.rule_id = ? AND related.state <> ? AND episode.recovered = ?", c.GroupID, c.TargetIdent, c.DatasourceID, c.RuleID, "closed", false).Count(&active).Error; err != nil {
					return err
				}
			}
			if active > 0 {
				return opsauth.Fail(409, "alerts_active", "Linked alert episodes are still active; verify recovery or explicitly cancel without claiming recovery")
			}
			spec, err := fork.activeProcedure(user, c)
			if err != nil {
				return err
			}
			actionAt, err := fork.recoveryBaselineAt(c)
			if err != nil {
				return err
			}
			for _, check := range spec.Checks {
				if !check.Required {
					continue
				}
				var v models.OpsVerification
				err = tx.Where("case_id = ? AND generation = ? AND version_id = ? AND plan_id = ? AND check_key = ?", id, c.Generation, c.KnowledgeVersionID, c.PlanID, check.Key).Order("id DESC").First(&v).Error
				if err != nil || v.State != "passed" {
					return opsauth.Fail(409, "verification_required", "Every required recovery check must pass before closing")
				}
				fresh := check.FreshSeconds
				if fresh <= 0 {
					fresh = 300
				}
				if check.Kind != "metric" && v.CreatedAt < s.now()-fresh {
					return opsauth.Fail(409, "verification_stale", "Business verification is no longer fresh; check again")
				}
				if actionAt > 0 && actionAt >= v.CreatedAt {
					return opsauth.Fail(409, "verification_stale", "A newer action requires fresh verification")
				}
				ids := []int64{}
				if json.Unmarshal([]byte(v.EvidenceIDs), &ids) != nil || len(ids) == 0 {
					return opsauth.Fail(409, "verification_evidence", "Verification evidence is missing")
				}
				if err = fork.evidenceRefs(tx, c, ids); err != nil {
					return err
				}
				if check.Kind == "metric" {
					var e models.OpsEvidence
					if err = tx.First(&e, ids[0]).Error; err != nil {
						return err
					}
					state, _ := fork.evaluateMetric(&e, check, actionAt)
					if state != "passed" {
						return opsauth.Fail(409, "verification_stale", "Recovery evidence is no longer fresh or complete")
					}
				}
			}
		}
		if err = s.change(tx, c, revision, map[string]interface{}{"state": "closed", "close_type": kind, "summary": summary, "followup": followup, "closed_at": s.now(), "wait_reason": ""}); err != nil {
			return err
		}
		if err = tx.Model(&models.OpsRun{}).Where("case_id = ? AND active_case_id = ?", id, id).Updates(map[string]interface{}{"state": "cancelled", "active_case_id": nil, "fence": gorm.Expr("fence + 1")}).Error; err != nil {
			return err
		}
		return s.audit(tx, id, user, "closed", map[string]string{"type": kind, "summary": summary, "followup": followup})
	})
}
