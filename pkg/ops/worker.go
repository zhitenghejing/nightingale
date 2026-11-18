package ops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Service) Queue(user, id int64, mode string) (*models.OpsRun, error) {
	c, err := s.Get(user, id, false)
	if err != nil {
		return nil, err
	}
	if c.State == "closed" {
		return nil, opsauth.Fail(409, "case_closed", "Case is closed")
	}
	if mode != "agent" && mode != "guided" {
		return nil, opsauth.Fail(422, "run_mode", "Unsupported investigation mode")
	}
	// A read-only user may investigate, but cannot displace an assigned operator.
	if c.OwnerID > 0 && c.OwnerID != user {
		if _, err = opsauth.Check(s.DB, user, c.GroupID, true); err != nil {
			return nil, err
		}
	}
	r := models.OpsRun{CaseID: id, ActiveCaseID: &id, UserID: user, State: "queued", Mode: mode, CreatedAt: s.now(), UpdatedAt: s.now()}
	err = s.DB.Create(&r).Error
	if err != nil {
		var existing models.OpsRun
		if e := s.DB.Where("active_case_id = ?", id).First(&existing).Error; e == nil {
			return nil, opsauth.Fail(409, "run_active", "This case already has an active investigation")
		}
	}
	return &r, err
}
func (s *Service) CancelRun(user, id int64) error {
	c, err := s.Get(user, id, true)
	if err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.OpsRun{}).Where("case_id = ? AND active_case_id = ?", id, id).Updates(map[string]interface{}{"state": "cancelled", "active_case_id": nil, "fence": gorm.Expr("fence + 1"), "updated_at": s.now()}).Error; err != nil {
			return err
		}
		return s.audit(tx, c.ID, user, "cancel_investigation", map[string]string{"reason": "human_takeover"})
	})
}
func (s *Service) Scoped(scope Scope) *Service { fork := *s; fork.scope = &scope; return &fork }
func (s *Service) checkScope(db *gorm.DB, user, id int64) error {
	if s.scope == nil {
		return nil
	}
	p := s.scope
	if p.UserID != user || p.CaseID != id {
		return opsauth.Fail(403, "run_scope", "Tool request is outside the investigation scope")
	}
	var r models.OpsRun
	err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND case_id = ? AND user_id = ? AND fence = ? AND state = ? AND lease_until > ?", p.RunID, id, user, p.Fence, "running", s.now()).First(&r).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return opsauth.Fail(409, "run_lease", "Investigation lease expired or was cancelled")
	}
	return err
}
func (s *Service) RunNext(ctx context.Context) error {
	var candidate models.OpsRun
	found := s.DB.Where("(state IN ? AND next_run_at <= ?) OR (state = ? AND lease_until < ?)", []string{"queued", "waiting"}, s.now(), "running", s.now()).Order("id ASC").Limit(1).Find(&candidate)
	if found.Error != nil {
		return found.Error
	}
	if found.RowsAffected == 0 {
		return nil
	}
	var err error
	oldFence := candidate.Fence
	r := s.DB.Model(&models.OpsRun{}).Where("id = ? AND fence = ? AND state = ?", candidate.ID, oldFence, candidate.State).Updates(map[string]interface{}{"state": "running", "fence": oldFence + 1, "lease_until": s.now() + 150, "attempts": candidate.Attempts + 1, "updated_at": s.now()})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return nil
	}
	candidate.Fence++
	scope := Scope{UserID: candidate.UserID, CaseID: candidate.CaseID, RunID: candidate.ID, Fence: candidate.Fence}
	runctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	fork := s.Scoped(scope)
	var result string
	if candidate.Attempts >= 3 {
		err = fmt.Errorf("Investigation retry budget exhausted; operator review required")
	} else if _, err = fork.Get(scope.UserID, scope.CaseID, false); err == nil {
		if candidate.Mode == "guided" {
			result, err = fork.guided(runctx, scope)
		} else if s.Runner == nil {
			err = fmt.Errorf("AI model is not configured; use guided investigation or continue manually")
		} else {
			result, err = s.Runner(runctx, scope)
		}
	}
	return s.finishInvestigation(candidate, result, err)
}
func (s *Service) guided(ctx context.Context, scope Scope) (string, error) {
	c, err := s.Get(scope.UserID, scope.CaseID, false)
	if err != nil {
		return "", err
	}
	matches, err := s.Matches(scope.UserID, c.ID)
	if err != nil {
		return "", err
	}
	if c.KnowledgeVersionID == 0 && c.PlanID == 0 {
		return fmt.Sprintf("Found %d candidate knowledge versions. Select an applicable knowledge version or confirm a case plan before executing steps.", len(matches)), nil
	}
	spec, err := s.activeProcedure(scope.UserID, c)
	if err != nil {
		return "", err
	}
	count := 0
	index := 0
	visited := map[string]bool{}
	for index < len(spec.Steps) {
		step := spec.Steps[index]
		if visited[step.Key] {
			return fmt.Sprintf("Collected %d steps; branch revisits %s. Operator review required.", count, step.Key), nil
		}
		visited[step.Key] = true
		if step.Kind != "metric" {
			var prior models.OpsStep
			found := s.DB.Where("case_id = ? AND version_id = ? AND plan_id = ? AND generation = ? AND step_key = ?", c.ID, c.KnowledgeVersionID, c.PlanID, c.Generation, step.Key).Order("id DESC").Limit(1).Find(&prior)
			if found.Error != nil {
				return "", found.Error
			}
			next := ""
			if found.RowsAffected > 0 {
				if prior.State == "completed" || prior.State == "skipped" {
					next = step.Next
					if next == "" {
						index++
						continue
					}
				} else if prior.State == "failed" {
					next = step.OnFailure
				}
			}
			if next != "" {
				for i, candidate := range spec.Steps {
					if candidate.Key == next {
						index = i
						break
					}
				}
				continue
			}
			return fmt.Sprintf("Collected %d steps; waiting for manual step %s.", count, step.Title), nil
		}
		e, err := s.CollectMetric(ctx, scope.UserID, c.ID, step.Query, 300)
		if err != nil {
			return "", err
		}
		state := "completed"
		next := step.Next
		if e.Status != "ok" {
			state = "failed"
			next = step.OnFailure
		}
		if err = s.recordStep(scope.UserID, c.ID, step.Key, state, "Guided metric collection; no root cause inferred", []int64{e.ID}, true); err != nil {
			return "", err
		}
		count++
		if next != "" {
			for i, candidate := range spec.Steps {
				if candidate.Key == next {
					index = i
					break
				}
			}
		} else if state == "failed" {
			break
		} else {
			index++
		}
	}
	return fmt.Sprintf("Collected evidence for %d metric steps. Manual checks and action confirmation remain with the operator.", count), nil
}
