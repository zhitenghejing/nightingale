package ops

import (
	"encoding/json"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
)

// Older procedures conservatively require every step. Authors can explicitly
// name the prerequisites for actions on alternative diagnostic branches.
func (s *Service) actionPrerequisites(c *models.OpsCase, spec knowledge.Spec, action knowledge.Action) error {
	keys := action.RequiredSteps
	if len(keys) == 0 {
		for _, st := range spec.Steps {
			keys = append(keys, st.Key)
		}
	}
	if len(keys) == 0 {
		return opsauth.Fail(409, "action_prerequisites", "Action requires investigation prerequisites")
	}
	for _, key := range keys {
		var step models.OpsStep
		found := s.DB.Where("case_id = ? AND generation = ? AND version_id = ? AND plan_id = ? AND step_key = ?", c.ID, c.Generation, c.KnowledgeVersionID, c.PlanID, key).Order("id DESC").Limit(1).Find(&step)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 || step.State != "completed" {
			return opsauth.Fail(409, "action_prerequisites", "Every required investigation step must be completed before preparing or approving this action")
		}
		var ids []int64
		if json.Unmarshal([]byte(step.EvidenceIDs), &ids) != nil || len(ids) == 0 {
			return opsauth.Fail(409, "action_prerequisites", "Prerequisite evidence is missing")
		}
		if err := s.evidenceRefs(s.DB, c, ids); err != nil {
			return err
		}
		for _, id := range ids {
			var e models.OpsEvidence
			if err := s.DB.First(&e, id).Error; err != nil {
				return err
			}
			if e.Status != "ok" || e.CollectedAt < s.now()-300 || e.CollectedAt > s.now()+5 || (e.Kind == "metric" && (e.ObservedAt < s.now()-60 || e.ObservedAt > s.now()+5)) {
				return opsauth.Fail(409, "action_prerequisites", "Prerequisite evidence must be successful and fresh; repeat the check")
			}
		}
	}
	return nil
}
