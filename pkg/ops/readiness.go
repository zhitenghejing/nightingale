package ops

import "github.com/ccfos/nightingale/v6/models"

// ActionReadiness exposes the same server-side prerequisites used at proposal
// and approval, so a model need not guess whether "recorded" means completed.
func (s *Service) ActionReadiness(user, id int64) ([]map[string]interface{}, error) {
	out := []map[string]interface{}{}
	c, err := s.Get(user, id, false)
	if err != nil {
		return nil, err
	}
	if c.KnowledgeVersionID == 0 && c.PlanID == 0 {
		return out, nil
	}
	spec, err := s.activeProcedure(user, c)
	if err != nil {
		return nil, err
	}
	actions := []models.OpsAction{}
	if err = s.DB.Where("case_id = ? AND generation = ?", id, c.Generation).Order("id DESC").Limit(200).Find(&actions).Error; err != nil {
		return nil, err
	}
	anchor, err := s.evidenceAnchor(c)
	if err != nil {
		return nil, err
	}
	for _, def := range spec.Actions {
		ready, reason := true, ""
		if e := s.actionPrerequisites(c, spec, def); e != nil {
			ready, reason = false, e.Error()
		}
		for _, a := range actions {
			p, e := ParseAction(&a)
			if e != nil {
				return nil, e
			}
			if p.Definition.Key != def.Key {
				continue
			}
			if a.State == "proposed" && (a.ExpiresAt <= s.now() || p.EvidenceMaxID != anchor || a.Baseline != actionBaseline(c, a.Payload)) {
				break
			}
			if a.State != "stale" {
				ready = false
				reason = "Action already has a decision or proposal; follow its current state instead of proposing again"
			}
			break
		}
		out = append(out, map[string]interface{}{"key": def.Key, "title": def.Title, "ready": ready, "blocked_reason": reason, "next_tool": "propose_ops_action"})
	}
	return out, nil
}
