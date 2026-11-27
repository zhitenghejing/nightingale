package ops

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ActionPayload struct {
	Definition    knowledge.Action `json:"definition"`
	Target        string           `json:"target"`
	EvidenceMaxID int64            `json:"evidence_max_id"`
	Template      interface{}      `json:"template,omitempty"`
}

// PendingExecution means an executor accepted a task, not that it finished.
type PendingExecution struct {
	TaskID       int64  `json:"task_id"`
	OperationKey string `json:"operation_key"`
}

func actionBaseline(c *models.OpsCase, payload models.OpsJSON) string {
	return knowledge.Digest([]byte(string(opsauth.JSON(map[string]interface{}{"target": c.TargetIdent, "group_id": c.GroupID, "datasource_id": c.DatasourceID, "version_id": c.KnowledgeVersionID, "plan_id": c.PlanID, "generation": c.Generation, "context": c.Context, "payload": payload}))))
}
func (s *Service) ProposeAction(ctx context.Context, user, id int64, key string) (*models.OpsAction, error) {
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
	var def *knowledge.Action
	for i := range spec.Actions {
		if spec.Actions[i].Key == key {
			def = &spec.Actions[i]
		}
	}
	if def == nil {
		return nil, opsauth.Fail(422, "action_missing", "Action is not in the active procedure")
	}
	if err = s.actionPrerequisites(c, spec, *def); err != nil {
		return nil, err
	}
	if def.Kind == "demo_reclaim" && (!s.DemoEnabled || !spec.TestOnly || Attrs(c)["environment"] != "test") {
		return nil, opsauth.Fail(403, "demo_disabled", "Demo execution is not enabled for this case")
	}
	anchor, err := s.evidenceAnchor(c)
	if err != nil {
		return nil, err
	}
	p := ActionPayload{Definition: *def, Target: c.TargetIdent, EvidenceMaxID: anchor}
	if def.Kind == "ibex_template" {
		if s.Prepare == nil {
			return nil, opsauth.Fail(503, "execute_disabled", "Task execution is not enabled")
		}
		p.Template, err = s.Prepare(ctx, user, c, *def)
		if err != nil {
			return nil, err
		}
	}
	raw := opsauth.JSON(p)
	base := actionBaseline(c, raw)
	a := models.OpsAction{CaseID: id, Generation: c.Generation, VersionID: c.KnowledgeVersionID, PlanID: c.PlanID, Kind: def.Kind, State: "proposed", OperationKey: uuid.NewString(), Baseline: base, Payload: raw, Result: "null", ProposedBy: user, ExpiresAt: s.now() + 1800, CreatedAt: s.now(), UpdatedAt: s.now()}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		latest, err := s.get(tx, user, id, s.scope == nil)
		if err != nil {
			return err
		}
		currentAnchor, anchorErr := s.withDB(tx).evidenceAnchor(latest)
		if anchorErr != nil {
			return anchorErr
		}
		if latest.State == "closed" || latest.Generation != c.Generation || currentAnchor != anchor || actionBaseline(latest, raw) != base {
			return opsauth.Fail(409, "action_stale", "Case changed while preparing the action")
		}
		procedure, err := s.withDB(tx).activeProcedure(user, latest)
		if err != nil {
			return err
		}
		if err = s.withDB(tx).actionPrerequisites(latest, procedure, *def); err != nil {
			return err
		}
		var existing models.OpsAction
		found := tx.Where("case_id = ? AND baseline = ? AND state = ? AND expires_at > ?", id, base, "proposed", s.now()).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			a = existing
			return nil
		}
		older := []models.OpsAction{}
		if err = tx.Where("case_id = ? AND state = ?", id, "proposed").Find(&older).Error; err != nil {
			return err
		}
		for _, old := range older {
			payload, decodeErr := ParseAction(&old)
			if decodeErr != nil {
				return decodeErr
			}
			if old.Generation != c.Generation || payload.Definition.Key == def.Key {
				if err = tx.Model(&models.OpsAction{}).Where("id = ? AND state = ?", old.ID, "proposed").Updates(map[string]interface{}{"state": "stale", "updated_at": s.now()}).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Create(&a).Error; err != nil {
			return err
		}
		return s.audit(tx, id, user, "action_proposed", map[string]interface{}{"action_id": a.ID, "key": key, "baseline": base})
	})
	return &a, err
}
func (s *Service) Approve(ctx context.Context, user, id, action int64, accept bool) (*models.OpsAction, error) {
	if s.scope != nil {
		return nil, opsauth.Fail(403, "human_approval", "Only the human approval endpoint can authorize an action")
	}
	var a models.OpsAction
	var current *models.OpsCase
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		c, err := s.get(tx, user, id, true)
		if err != nil {
			return err
		}
		current = c
		if err = tx.Where("id = ? AND case_id = ?", action, id).First(&a).Error; err != nil {
			return err
		}
		if a.State != "proposed" {
			return opsauth.Fail(409, "action_consumed", "Action has already been decided or executed")
		}
		state := "rejected"
		if accept {
			if err = s.withDB(tx).actionUnresolved(c); err != nil {
				return err
			}
			if c.State == "closed" || a.ExpiresAt <= s.now() || a.Generation != c.Generation || a.VersionID != c.KnowledgeVersionID || a.PlanID != c.PlanID || a.Baseline != actionBaseline(c, a.Payload) {
				return opsauth.Fail(409, "action_stale", "Action expired or its environment changed; create a new proposal")
			}
			payload, parseErr := ParseAction(&a)
			if parseErr != nil {
				return parseErr
			}
			anchor, anchorErr := s.withDB(tx).evidenceAnchor(c)
			if anchorErr != nil {
				return anchorErr
			}
			if anchor != payload.EvidenceMaxID {
				return opsauth.Fail(409, "action_evidence_changed", "New evidence arrived after the proposal; review a new proposal")
			}
			procedure, err := s.withDB(tx).activeProcedure(user, c)
			if err != nil {
				return err
			}
			if err = s.withDB(tx).actionPrerequisites(c, procedure, payload.Definition); err != nil {
				return err
			}
			if a.Kind == "manual" {
				state = "awaiting_manual"
			} else {
				if s.ExecutionAllowed != nil {
					if err = s.ExecutionAllowed(a.Kind); err != nil {
						return err
					}
				}
				if s.Execute == nil {
					return opsauth.Fail(503, "execute_disabled", "Action execution is not available")
				}
				state = "dispatching"
			}
		}
		r := tx.Model(&models.OpsAction{}).Where("id = ? AND state = ?", action, "proposed").Updates(map[string]interface{}{"state": state, "approved_by": user, "updated_at": s.now()})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return opsauth.Fail(409, "action_consumed", "Action was already decided")
		}
		a.State = state
		a.ApprovedBy = user
		a.UpdatedAt = s.now()
		return s.audit(tx, id, user, "action_decided", map[string]interface{}{"action_id": action, "accepted": accept})
	})
	if err != nil {
		return nil, err
	}
	if a.State != "dispatching" {
		return &a, nil
	}
	// A committed dispatching state is never blindly retried after a timeout.
	result, executeErr := s.Execute(ctx, user, current, &a)
	state := "succeeded"
	if _, pending := result.(PendingExecution); pending {
		state = "submitted"
	}
	if executeErr != nil {
		state = "unknown"
		result = map[string]interface{}{"error": executeErr.Error(), "operation_key": a.OperationKey, "instruction": "Check the executor before retrying"}
	}
	r := s.DB.Model(&models.OpsAction{}).Where("id = ? AND state = ?", a.ID, "dispatching").Updates(map[string]interface{}{"state": state, "result": opsauth.JSON(result), "updated_at": s.now()})
	if r.Error != nil {
		return nil, r.Error
	}
	a.State = state
	a.Result = opsauth.JSON(result)
	a.UpdatedAt = s.now()
	return &a, nil
}
func (s *Service) ManualActionResult(user, id, action int64, note string, success bool) error {
	if s.scope != nil {
		return opsauth.Fail(403, "manual_only", "Manual action results require an operator")
	}
	if strings.TrimSpace(note) == "" {
		return opsauth.Fail(422, "result_required", "A result and supporting details are required")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		c, err := s.get(tx, user, id, true)
		if err != nil {
			return err
		}
		if c.State == "closed" {
			return opsauth.Fail(409, "case_closed", "Case is closed")
		}
		var a models.OpsAction
		if err = tx.Where("id = ? AND case_id = ?", action, id).First(&a).Error; err != nil {
			return err
		}
		if a.State != "awaiting_manual" && a.State != "unknown" && a.State != "dispatching" && a.State != "submitted" {
			return opsauth.Fail(409, "action_state", "Action is not waiting for a manual result")
		}
		state := "failed"
		if success {
			state = "succeeded"
		}
		r := tx.Model(&a).Where("state = ?", a.State).Updates(map[string]interface{}{"state": state, "result": opsauth.JSON(map[string]interface{}{"note": note, "confirmed_by": user}), "updated_at": s.now()})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return opsauth.Fail(409, "action_consumed", "Action changed; reload")
		}
		if c.WaitReason == "action_confirmation" {
			if err = s.change(tx, c, c.Revision, map[string]interface{}{"wait_reason": ""}); err != nil {
				return err
			}
		}
		return s.audit(tx, id, user, "manual_action_result", map[string]interface{}{"action_id": action, "state": state, "note": note})
	})
}
func ParseAction(a *models.OpsAction) (ActionPayload, error) {
	var p ActionPayload
	err := json.Unmarshal([]byte(a.Payload), &p)
	return p, err
}
func (s *Service) ReconcileAction(ctx context.Context, user, id, action int64) (*models.OpsAction, error) {
	c, err := s.Get(user, id, false)
	if err != nil {
		return nil, err
	}
	var a models.OpsAction
	if err = s.DB.Where("id = ? AND case_id = ?", action, id).First(&a).Error; err != nil {
		return nil, err
	}
	if a.Kind != "ibex_template" || s.Reconcile == nil {
		return nil, opsauth.Fail(422, "reconcile_unavailable", "This action has no external task reconciliation adapter")
	}
	if a.State != "submitted" && a.State != "unknown" && a.State != "dispatching" {
		return &a, nil
	}
	state, result, err := s.Reconcile(ctx, user, c, &a)
	if err != nil {
		return nil, err
	}
	if state != "succeeded" && state != "failed" && state != "submitted" && state != "unknown" {
		return nil, opsauth.Fail(500, "executor_state", "Executor returned an invalid state")
	}
	r := s.DB.Model(&a).Where("state = ?", a.State).Updates(map[string]interface{}{"state": state, "result": opsauth.JSON(result), "updated_at": s.now()})
	if r.Error != nil {
		return nil, r.Error
	}
	if r.RowsAffected != 1 {
		return nil, opsauth.Fail(409, "action_changed", "Action was changed during reconciliation")
	}
	a.State = state
	a.Result = opsauth.JSON(result)
	return &a, nil
}
func (s *Service) lastActionAt(c *models.OpsCase) (int64, error) {
	var at int64
	err := s.DB.Model(&models.OpsAction{}).Where("case_id = ? AND generation = ? AND state IN ?", c.ID, c.Generation, []string{"succeeded", "failed"}).Select("COALESCE(MAX(updated_at),0)").Scan(&at).Error
	return at, err
}
func (s *Service) actionUnresolved(c *models.OpsCase) error {
	var count int64
	if err := s.DB.Model(&models.OpsAction{}).Where("case_id = ? AND generation = ? AND state IN ?", c.ID, c.Generation, []string{"dispatching", "unknown", "awaiting_manual", "submitted"}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return opsauth.Fail(409, "action_unresolved", "An action result is still unknown or awaiting confirmation")
	}
	return nil
}

func (s *Service) evidenceAnchor(c *models.OpsCase) (int64, error) {
	var anchor int64
	err := s.DB.Model(&models.OpsEvidence{}).Where("case_id = ? AND generation = ?", c.ID, c.Generation).Select("COALESCE(MAX(id),0)").Scan(&anchor).Error
	return anchor, err
}

// A newer native occurrence on the same target/rule invalidates an earlier
// observation window even when it lives in its own operations case.
func (s *Service) recoveryBaselineAt(c *models.OpsCase) (int64, error) {
	at, err := s.lastActionAt(c)
	if err != nil {
		return 0, err
	}
	if c.TargetIdent != "" && c.RuleID > 0 {
		var latest int64
		err = s.DB.Model(&models.OpsCase{}).Where("group_id = ? AND target_ident = ? AND datasource_id = ? AND rule_id = ?", c.GroupID, c.TargetIdent, c.DatasourceID, c.RuleID).Select("COALESCE(MAX(created_at),0)").Scan(&latest).Error
		if latest > at {
			at = latest
		}
	}
	return at, err
}
