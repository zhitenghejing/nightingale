package ops

import (
	"encoding/json"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"gorm.io/gorm"
)

// PlanInput proposes a method for this case only, not a reusable knowledge entry.
type PlanInput struct {
	Revision    int64          `json:"revision"`
	Title       string         `json:"title"`
	Scope       string         `json:"scope"`
	Rationale   string         `json:"rationale"`
	EvidenceIDs []int64        `json:"evidence_ids"`
	Spec        knowledge.Spec `json:"spec"`
}

func validatePlan(in PlanInput) error {
	if strings.TrimSpace(in.Title) == "" || len(in.Title) > 255 || strings.TrimSpace(in.Scope) == "" || len(in.Scope) > 4000 || strings.TrimSpace(in.Rationale) == "" || len(in.Rationale) > 16000 {
		return opsauth.Fail(422, "plan_incomplete", "Plan title, explicit scope and evidence-based rationale are required")
	}
	if strings.TrimSpace(in.Spec.Escalation) == "" || len(in.Spec.Escalation) > 4000 {
		return opsauth.Fail(422, "plan_escalation", "A case plan requires stopping and escalation conditions")
	}
	if err := knowledge.ValidateProcedure(in.Spec); err != nil {
		return err
	}
	// Case plans do not borrow private knowledge sources or assert reuse validation.
	if len(in.Spec.SourceIDs) > 0 || len(in.Spec.RuleIDs) > 0 || in.Spec.ValidatedAt != 0 || in.Spec.ReviewAt != 0 {
		return opsauth.Fail(422, "plan_local", "Case plans must use case evidence, not knowledge publication metadata")
	}
	for _, a := range in.Spec.Actions {
		if strings.TrimSpace(a.Instruction) == "" {
			return opsauth.Fail(422, "plan_instruction", "Every plan action requires an explicit instruction")
		}
	}
	return nil
}

func planDigest(p *models.OpsPlan) string {
	return knowledge.Digest([]byte(opsauth.JSON(map[string]interface{}{"case_id": p.CaseID, "title": p.Title, "scope": p.Scope, "rationale": p.Rationale, "spec": p.Payload, "evidence_ids": p.EvidenceIDs, "generation": p.Generation, "evidence_anchor": p.EvidenceAnchor})))
}

func (s *Service) ProposePlan(user, id int64, in PlanInput) (*models.OpsPlan, error) {
	if in.Spec.Actions == nil {
		in.Spec.Actions = []knowledge.Action{}
	}
	if in.Spec.SourceIDs == nil {
		in.Spec.SourceIDs = []int64{}
	}
	if in.Spec.RuleIDs == nil {
		in.Spec.RuleIDs = []int64{}
	}
	if in.Spec.Conditions == nil {
		in.Spec.Conditions = map[string]string{}
	}

	if err := validatePlan(in); err != nil {
		return nil, err
	}
	var p models.OpsPlan
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		c, err := s.get(tx, user, id, true)
		if err != nil {
			return err
		}
		if c.State == "closed" {
			return opsauth.Fail(409, "case_closed", "Case is closed")
		}
		if c.Revision != in.Revision {
			return opsauth.Fail(409, "revision_conflict", "Case changed; reload before continuing")
		}
		fork := s.withDB(tx)
		if err = fork.evidenceRefs(tx, c, in.EvidenceIDs); err != nil {
			return err
		}
		var failed int64
		if err = tx.Model(&models.OpsEvidence{}).Where("id IN ? AND status <> ?", in.EvidenceIDs, "ok").Count(&failed).Error; err != nil {
			return err
		}
		if failed > 0 {
			return opsauth.Fail(422, "plan_evidence", "A plan requires successful supporting evidence")
		}
		var sequence int64
		if err = tx.Model(&models.OpsPlan{}).Where("case_id = ?", id).Select("COALESCE(MAX(sequence),0)").Scan(&sequence).Error; err != nil {
			return err
		}
		anchor, err := fork.evidenceAnchor(c)
		if err != nil {
			return err
		}
		p = models.OpsPlan{CaseID: id, Sequence: sequence + 1, Title: in.Title, Scope: in.Scope, Rationale: in.Rationale, Payload: opsauth.JSON(in.Spec), EvidenceIDs: opsauth.JSON(in.EvidenceIDs), Generation: c.Generation, EvidenceAnchor: anchor, State: "draft", CreatedBy: user, CreatedAt: s.now()}
		p.SHA256 = planDigest(&p)
		if err = tx.Create(&p).Error; err != nil {
			return err
		}
		// Serialize proposals and give editors an observable revision for concurrency.
		if err = s.change(tx, c, in.Revision, map[string]interface{}{}); err != nil {
			return err
		}
		return s.audit(tx, id, user, "plan_proposed", map[string]interface{}{"plan_id": p.ID, "sequence": p.Sequence, "sha256": p.SHA256})
	})
	return &p, err
}

func (s *Service) DecidePlan(user, id, planID, revision int64, accept bool, note string) error {
	if s.scope != nil {
		return opsauth.Fail(403, "human_plan", "Only an operator can confirm a case plan")
	}
	if strings.TrimSpace(note) == "" || len(note) > 16000 {
		return opsauth.Fail(422, "reason_required", "Plan confirmation or rejection requires a reason")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		c, err := s.get(tx, user, id, true)
		if err != nil {
			return err
		}
		if c.State == "closed" || c.Revision != revision {
			return opsauth.Fail(409, "revision_conflict", "Case changed; reload before continuing")
		}
		var p models.OpsPlan
		if err = tx.Where("id = ? AND case_id = ?", planID, id).First(&p).Error; err != nil {
			return err
		}
		if p.State != "draft" {
			return opsauth.Fail(409, "plan_decided", "This plan version was already decided")
		}
		state := "rejected"
		generation := int64(0)
		values := map[string]interface{}{}
		if accept {
			fork := s.withDB(tx)
			if err = fork.actionUnresolved(c); err != nil {
				return err
			}
			anchor, e := fork.evidenceAnchor(c)
			if e != nil {
				return e
			}
			if p.Generation != c.Generation || p.EvidenceAnchor != anchor {
				return opsauth.Fail(409, "plan_stale", "Background or evidence changed; propose a new plan version")
			}
			if p.SHA256 != planDigest(&p) {
				return opsauth.Fail(409, "plan_integrity", "Plan content integrity check failed")
			}
			spec, e := knowledge.Decode(p.Payload)
			if e != nil {
				return e
			}
			if err = validatePlan(PlanInput{Title: p.Title, Scope: p.Scope, Rationale: p.Rationale, Spec: spec}); err != nil {
				return err
			}
			if _, ok := knowledge.Conditions(spec, MatchAttrs(c)); !ok {
				return opsauth.Fail(422, "plan_conditions", "Required plan conditions are missing or do not match")
			}
			state = "confirmed"
			generation = c.Generation + 1
			values = map[string]interface{}{"plan_id": p.ID, "knowledge_version_id": 0, "generation": generation, "state": "processing", "wait_reason": ""}
			// An active Agent must re-read the newly confirmed procedure.
			if err = tx.Model(&models.OpsRun{}).Where("case_id = ? AND active_case_id = ?", id, id).Updates(map[string]interface{}{"state": "cancelled", "active_case_id": nil, "fence": gorm.Expr("fence + 1")}).Error; err != nil {
				return err
			}
		}
		result := tx.Model(&models.OpsPlan{}).Where("id = ? AND state = ?", p.ID, "draft").Updates(map[string]interface{}{"state": state, "confirmed_by": user, "confirmed_at": s.now(), "confirmed_generation": generation, "decision_note": note})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return opsauth.Fail(409, "plan_decided", "This plan version was already decided")
		}
		if err = s.change(tx, c, revision, values); err != nil {
			return err
		}
		return s.audit(tx, id, user, "plan_decided", map[string]interface{}{"plan_id": p.ID, "accepted": accept, "reason": note})
	})
}

// activeProcedure is the single seam shared by steps, actions and verification.
func (s *Service) activeProcedure(user int64, c *models.OpsCase) (knowledge.Spec, error) {
	if c.PlanID == 0 {
		return s.activeKnowledge(user, c)
	}
	var p models.OpsPlan
	if err := s.DB.Where("id = ? AND case_id = ?", c.PlanID, c.ID).First(&p).Error; err != nil {
		return knowledge.Spec{}, err
	}
	spec, err := knowledge.Decode(p.Payload)
	if err != nil {
		return spec, err
	}
	if c.KnowledgeVersionID != 0 || p.State != "confirmed" || p.ConfirmedGeneration != c.Generation || p.SHA256 != planDigest(&p) {
		return spec, opsauth.Fail(409, "plan_stale", "The case plan must be confirmed again for the current background")
	}
	if _, ok := knowledge.Conditions(spec, MatchAttrs(c)); !ok {
		return spec, opsauth.Fail(422, "plan_conditions", "Required plan conditions are missing or do not match")
	}
	return spec, nil
}

func planEvidenceIDs(p models.OpsPlan) []int64 {
	var ids []int64
	_ = json.Unmarshal([]byte(p.EvidenceIDs), &ids)
	return ids
}
