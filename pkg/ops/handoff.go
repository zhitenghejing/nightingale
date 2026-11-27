package ops

import (
	"fmt"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/modeloutput"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PendingHandoff recognizes still-valid persisted proposals before starting a
// new model run, so retry cannot immediately invalidate a pending confirmation.
func (s *Service) PendingHandoff(user, id int64) (string, error) {
	c, err := s.Get(user, id, false)
	if err != nil {
		return "", err
	}
	if spec, e := s.activeProcedure(user, c); e == nil {
		var proposed []models.OpsAction
		if err = s.DB.Where("case_id = ? AND generation = ? AND state = ? AND expires_at > ?", id, c.Generation, "proposed", s.now()).Find(&proposed).Error; err != nil {
			return "", err
		}
		anchor, e := s.evidenceAnchor(c)
		if e != nil {
			return "", e
		}
		for _, a := range proposed {
			p, e := ParseAction(&a)
			if e == nil && p.EvidenceMaxID == anchor && a.Baseline == actionBaseline(c, a.Payload) && s.actionPrerequisites(c, spec, p.Definition) == nil {
				return fmt.Sprintf("操作提案 #%d 仍待人员确认；排查暂停，不继续采集使提案失效。", a.ID), nil
			}
		}
	}
	var plans []models.OpsPlan
	if err = s.DB.Where("case_id = ? AND generation = ? AND state = ?", id, c.Generation, "draft").Order("id DESC").Limit(5).Find(&plans).Error; err != nil {
		return "", err
	}
	if c.PlanID == 0 && c.KnowledgeVersionID == 0 && len(plans) > 0 {
		anchor, e := s.evidenceAnchor(c)
		if e != nil {
			return "", e
		}
		for _, p := range plans {
			if p.EvidenceAnchor == anchor {
				return fmt.Sprintf("本次方案 #%d 待人员核对并确认；本轮不继续生成或执行。", p.ID), nil
			}
		}
	}
	return "", nil
}

func (s *Service) finishInvestigation(candidate models.OpsRun, result string, runErr error) error {
	result = modeloutput.Visible(result)
	if runErr != nil {
		result = s.HandoffSummary(candidate.UserID, candidate.CaseID, "排查未完成："+modeloutput.Visible(runErr.Error()))
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		fork := s.withDB(tx)
		var c models.OpsCase
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, candidate.CaseID).Error
		if err != nil {
			return err
		}

		// Lock the case before the lease, consistently with tool writes and takeover.
		var active models.OpsRun
		found := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND fence = ? AND state = ?", candidate.ID, candidate.Fence, "running").Limit(1).Find(&active)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 {
			return nil
		}
		state, wait := "completed", ""
		if runErr != nil {
			state, wait = "failed", "agent_failed"
		} else {
			var count int64
			if err = tx.Model(&models.OpsPlan{}).Where("case_id = ? AND generation = ? AND state = ?", c.ID, c.Generation, "draft").Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				state, wait = "awaiting_operator", "plan_confirmation"
			}
			if err = tx.Model(&models.OpsAction{}).Where("case_id = ? AND generation = ? AND state IN ?", c.ID, c.Generation, []string{"proposed", "awaiting_manual", "dispatching", "submitted", "unknown"}).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				state, wait = "awaiting_operator", "action_confirmation"
			}
			if wait == "" {
				if spec, e := fork.activeProcedure(candidate.UserID, &c); e == nil {
					for _, step := range spec.Steps {
						if step.Kind != "manual" {
							continue
						}
						var record models.OpsStep
						f := tx.Where("case_id = ? AND generation = ? AND step_key = ?", c.ID, c.Generation, step.Key).Order("id DESC").Limit(1).Find(&record)
						if f.Error != nil {
							return f.Error
						}
						if f.RowsAffected == 0 || record.State != "completed" {
							state, wait = "awaiting_operator", "manual_investigation"
							break
						}
					}
				}
			}
		}
		if c.State != "closed" {
			if wait != "" {
				if err = fork.change(tx, &c, c.Revision, map[string]interface{}{"wait_reason": wait}); err != nil {
					return err
				}
			} else if c.WaitReason == "agent_failed" || c.WaitReason == "plan_confirmation" || c.WaitReason == "action_confirmation" || c.WaitReason == "manual_investigation" {
				if err = fork.change(tx, &c, c.Revision, map[string]interface{}{"wait_reason": ""}); err != nil {
					return err
				}
			}
		}
		if result == "" {
			result = "本轮没有可展示的结论，请核对已保存证据并由值班人员接管。"
		}
		if err = tx.Model(&models.OpsRun{}).Where("id = ? AND fence = ? AND state = ?", candidate.ID, candidate.Fence, "running").Updates(map[string]interface{}{"state": state, "active_case_id": nil, "result": truncate(result, 16000), "lease_until": 0, "updated_at": s.now()}).Error; err != nil {
			return err
		}
		return fork.audit(tx, c.ID, candidate.UserID, "investigation_handoff", map[string]interface{}{"run_id": candidate.ID, "state": state, "wait_reason": wait, "owner_id": c.OwnerID, "next_role": "business_group_operator"})
	})
}
