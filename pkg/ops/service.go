// Package ops owns durable alert handling and evidence-based recovery checks.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/modeloutput"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Point struct {
	At         int64   `json:"at"`
	Value      float64 `json:"value"`
	ObservedAt int64   `json:"observed_at,omitempty"`
}
type MetricResult struct {
	Points         []Point `json:"points"`
	Status         string  `json:"status"`
	Query          string  `json:"query"`
	Reason         string  `json:"reason"`
	RequestedQuery string  `json:"requested_query"`
}
type QueryFunc func(context.Context, int64, *models.OpsCase, string, int64, int64) (MetricResult, error)
type ExecuteFunc func(context.Context, int64, *models.OpsCase, *models.OpsAction) (interface{}, error)
type ProbeFunc func(context.Context, *models.OpsCase, string) (interface{}, bool, error)
type RunnerFunc func(context.Context, Scope) (string, error)

type Service struct {
	DB                     *gorm.DB
	Knowledge              *knowledge.Store
	Now                    func() time.Time
	Logs                   func(context.Context, int64, *models.OpsCase) (interface{}, string, error)
	Query                  QueryFunc
	ExecutionAllowed       func(string) error
	Execute                ExecuteFunc
	Reconcile              func(context.Context, int64, *models.OpsCase, *models.OpsAction) (string, interface{}, error)
	Prepare                func(context.Context, int64, *models.OpsCase, knowledge.Action) (interface{}, error)
	Probe                  ProbeFunc
	Runner                 RunnerFunc
	DemoEnabled            bool
	SourceRetentionSeconds int64
	scope                  *Scope
}
type Scope struct {
	UserID int64 `json:"user_id"`
	CaseID int64 `json:"case_id"`
	RunID  int64 `json:"run_id"`
	Fence  int64 `json:"fence"`
}

func (s *Service) now() int64 {
	if s.Now != nil {
		return s.Now().Unix()
	}
	return time.Now().Unix()
}
func (s *Service) withDB(db *gorm.DB) *Service {
	fork := *s
	fork.DB = db
	k := *s.Knowledge
	k.DB = db
	fork.Knowledge = &k
	return &fork
}
func (s *Service) Get(user, id int64, write bool) (*models.OpsCase, error) {
	return s.get(s.DB, user, id, write)
}
func (s *Service) get(db *gorm.DB, user, id int64, write bool) (*models.OpsCase, error) {
	var c models.OpsCase
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, id).Error; err != nil {
		return nil, err
	}
	if err := s.checkScope(db, user, id); err != nil {
		return nil, err
	}
	if _, err := opsauth.Check(db, user, c.GroupID, write); err != nil {
		return nil, err
	}
	return &c, nil
}
func (s *Service) List(user, group int64, state string) ([]models.OpsCase, error) {
	q := s.DB.Model(&models.OpsCase{})
	if group > 0 {
		if _, err := opsauth.Check(s.DB, user, group, false); err != nil {
			return nil, err
		}
		q = q.Where("group_id = ?", group)
	} else {
		ids, admin, err := opsauth.Groups(s.DB, user)
		if err != nil {
			return nil, err
		}
		if !admin {
			q = q.Where("group_id IN ?", ids)
		}
	}
	if state != "" {
		q = q.Where("state = ?", state)
	}
	rows := []models.OpsCase{}
	err := q.Order("updated_at DESC, id DESC").Limit(200).Find(&rows).Error
	return rows, err
}
func (s *Service) audit(tx *gorm.DB, cid, user int64, action string, v interface{}) error {
	return tx.Create(&models.OpsAudit{CaseID: cid, ActorID: user, Action: action, Detail: opsauth.JSON(v), CreatedAt: s.now()}).Error
}
func (s *Service) change(tx *gorm.DB, c *models.OpsCase, revision int64, values map[string]interface{}) error {
	if c.State == "closed" {
		return opsauth.Fail(409, "case_closed", "Closed case cannot be changed")
	}
	if revision != c.Revision {
		return opsauth.Fail(409, "revision_conflict", "Case changed; reload before continuing")
	}
	values["revision"] = revision + 1
	values["updated_at"] = s.now()
	r := tx.Model(&models.OpsCase{}).Where("id = ? AND revision = ? AND state <> ?", c.ID, revision, "closed").Updates(values)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return opsauth.Fail(409, "revision_conflict", "Case changed; reload before continuing")
	}
	return nil
}
func (s *Service) Create(user, group int64, title string, attrs map[string]string) (*models.OpsCase, error) {
	if _, err := opsauth.Check(s.DB, user, group, true); err != nil {
		return nil, err
	}
	if strings.TrimSpace(title) == "" || len(title) > 255 {
		return nil, opsauth.Fail(422, "title_required", "Case title is required")
	}
	if len(attrs) > 30 {
		return nil, opsauth.Fail(422, "context_limit", "Too many context fields")
	}
	c := models.OpsCase{GroupID: group, Title: title, State: "pending", Context: opsauth.JSON(attrs), Revision: 1, Generation: 1, CreatedBy: user, CreatedAt: s.now(), UpdatedAt: s.now()}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&c).Error; err != nil {
			return err
		}
		return s.audit(tx, c.ID, user, "create", map[string]string{"title": title})
	})
	return &c, err
}
func Attrs(c *models.OpsCase) map[string]string {
	attrs := map[string]string{}
	_ = json.Unmarshal([]byte(c.Context), &attrs)
	if attrs == nil {
		attrs = map[string]string{}
	}
	return attrs
}

// MatchAttrs adds immutable platform identity after user-supplied background.
func MatchAttrs(c *models.OpsCase) map[string]string {
	attrs := Attrs(c)
	for key, value := range map[string]string{"target_ident": c.TargetIdent, "datasource_id": strconv.FormatInt(c.DatasourceID, 10), "rule_id": strconv.FormatInt(c.RuleID, 10), "group_id": strconv.FormatInt(c.GroupID, 10)} {
		delete(attrs, key)
		if value != "" && value != "0" {
			attrs[key] = value
		}
	}
	return attrs
}
func (s *Service) Assign(user, id, revision, owner int64) (*models.OpsCase, error) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		c, err := s.get(tx, user, id, true)
		if err != nil {
			return err
		}
		if owner > 0 {
			if _, err = opsauth.Check(tx, owner, c.GroupID, true); err != nil {
				return err
			}
		}
		state := c.State
		if state == "pending" && owner > 0 {
			state = "processing"
		}
		if err = s.change(tx, c, revision, map[string]interface{}{"owner_id": owner, "state": state}); err != nil {
			return err
		}
		if err = tx.Model(&models.OpsRun{}).Where("case_id = ? AND state IN ?", id, []string{"queued", "running", "waiting"}).Updates(map[string]interface{}{"state": "cancelled", "active_case_id": nil, "fence": gorm.Expr("fence + 1"), "updated_at": s.now()}).Error; err != nil {
			return err
		}
		return s.audit(tx, id, user, "assign", map[string]int64{"owner_id": owner})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(user, id, false)
}
func (s *Service) UpdateContext(user, id, revision int64, attrs map[string]string, wait string) error {
	if len(attrs) > 30 || len(wait) > 64 {
		return opsauth.Fail(422, "context_limit", "Context or waiting reason is too large")
	}
	for k, v := range attrs {
		if len(k) > 64 || len(v) > 1000 || strings.HasPrefix(k, "label.") || k == "target_ident" || k == "datasource_id" {
			return opsauth.Fail(422, "context_reserved", "Context field is invalid or reserved")
		}
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		c, err := s.get(tx, user, id, true)
		if err != nil {
			return err
		}
		all := Attrs(c)
		for k, v := range attrs {
			all[k] = v
		}
		if err = s.withDB(tx).actionUnresolved(c); err != nil {
			return err
		}
		generation := c.Generation
		if string(opsauth.JSON(all)) != string(c.Context) {
			generation++
		}
		if err = s.change(tx, c, revision, map[string]interface{}{"context": opsauth.JSON(all), "wait_reason": wait, "generation": generation}); err != nil {
			return err
		}
		if generation != c.Generation {
			if err = tx.Model(&models.OpsRun{}).Where("case_id = ? AND active_case_id = ?", id, id).Updates(map[string]interface{}{"state": "cancelled", "active_case_id": nil, "fence": gorm.Expr("fence + 1"), "updated_at": s.now()}).Error; err != nil {
				return err
			}
		}
		return s.audit(tx, id, user, "context", map[string]interface{}{"fields": attrs, "wait_reason": wait})
	})
}
func (s *Service) SelectKnowledge(user, id, revision, version int64, reason string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		c, err := s.get(tx, user, id, s.scope == nil)
		if err != nil {
			return err
		}
		if c.State == "closed" {
			return opsauth.Fail(409, "case_closed", "Case is closed")
		}
		if revision != c.Revision {
			return opsauth.Fail(409, "revision_conflict", "Case changed; reload before continuing")
		}
		ks := *s.Knowledge
		ks.DB = tx
		v, _, err := ks.Applicable(user, version, MatchAttrs(c))
		if err != nil {
			return err
		}
		if v.GroupID != c.GroupID {
			return opsauth.Fail(403, "knowledge_group", "Knowledge must belong to this case's business group")
		}
		if s.scope != nil && (c.PlanID > 0 || c.KnowledgeVersionID > 0 && c.KnowledgeVersionID != version) {
			return opsauth.Fail(403, "human_version_change", "Changing an adopted version requires an operator")
		}
		if (c.PlanID > 0 || c.KnowledgeVersionID > 0 && c.KnowledgeVersionID != version) && strings.TrimSpace(reason) == "" {
			return opsauth.Fail(422, "reason_required", "Changing the adopted knowledge version requires a reason")
		}
		if c.PlanID == 0 && c.KnowledgeVersionID == version {
			return nil
		}
		if err = s.withDB(tx).actionUnresolved(c); err != nil {
			return err
		}
		if err = s.change(tx, c, revision, map[string]interface{}{"knowledge_version_id": version, "plan_id": 0, "generation": c.Generation + 1, "state": "processing", "wait_reason": ""}); err != nil {
			return err
		}
		return s.audit(tx, id, user, "select_knowledge", map[string]interface{}{"version_id": version, "previous_version_id": c.KnowledgeVersionID, "reason": reason})
	})
}
func (s *Service) activeKnowledge(user int64, c *models.OpsCase) (knowledge.Spec, error) {
	if c.KnowledgeVersionID == 0 {
		return knowledge.Spec{}, opsauth.Fail(422, "knowledge_required", "Select an applicable knowledge version or confirm a case plan first")
	}
	v, spec, err := s.Knowledge.Applicable(user, c.KnowledgeVersionID, MatchAttrs(c))
	if err == nil && v.GroupID != c.GroupID {
		return spec, opsauth.Fail(403, "knowledge_group", "Knowledge belongs to another group")
	}
	return spec, err
}
func (s *Service) Matches(user, id int64) ([]knowledge.Match, error) {
	c, err := s.Get(user, id, false)
	if err != nil {
		return nil, err
	}
	return s.Knowledge.Search(user, c.GroupID, c.RuleID, c.Title, MatchAttrs(c))
}

type Detail struct {
	Plans         []models.OpsPlan         `json:"plans"`
	Case          *models.OpsCase          `json:"case"`
	Episodes      []models.OpsEpisode      `json:"episodes"`
	Evidence      []models.OpsEvidence     `json:"evidence"`
	Steps         []models.OpsStep         `json:"steps"`
	Actions       []models.OpsAction       `json:"actions"`
	Verifications []models.OpsVerification `json:"verifications"`
	Runs          []models.OpsRun          `json:"runs"`
	Audit         []models.OpsAudit        `json:"audit"`
}

func (s *Service) Detail(user, id int64) (*Detail, error) {
	c, err := s.Get(user, id, false)
	if err != nil {
		return nil, err
	}
	d := &Detail{Case: c, Plans: []models.OpsPlan{}, Episodes: []models.OpsEpisode{}, Evidence: []models.OpsEvidence{}, Steps: []models.OpsStep{}, Actions: []models.OpsAction{}, Verifications: []models.OpsVerification{}, Runs: []models.OpsRun{}, Audit: []models.OpsAudit{}}
	for _, target := range []interface{}{&d.Plans, &d.Episodes, &d.Evidence, &d.Steps, &d.Actions, &d.Verifications, &d.Runs, &d.Audit} {
		if err = s.DB.Where("case_id = ?", id).Order("id DESC").Limit(200).Find(target).Error; err != nil {
			return nil, err
		}
	}
	for i := range d.Runs {
		d.Runs[i].Result = modeloutput.Visible(d.Runs[i].Result)
		if d.Runs[i].Result == "" && d.Runs[i].State != "running" && d.Runs[i].State != "queued" {
			d.Runs[i].Result = "本次运行没有可展示的结论，请核对已保存证据。"
		}
	}
	if c.PlanID > 0 {
		found := false
		for _, p := range d.Plans {
			if p.ID == c.PlanID {
				found = true
				break
			}
		}
		if !found {
			var p models.OpsPlan
			if err = s.DB.Where("id = ? AND case_id = ?", c.PlanID, id).First(&p).Error; err != nil {
				return nil, err
			}
			d.Plans = append(d.Plans, p)
		}
	}
	return d, nil
}

func (s *Service) persistEvidence(c *models.OpsCase, user int64, kind, source, status string, observed, start, end int64, payload interface{}) (*models.OpsEvidence, error) {
	raw := opsauth.JSON(payload)
	if len(raw) > 1<<20 {
		return nil, opsauth.Fail(422, "evidence_limit", "Evidence exceeds the size limit")
	}
	e := models.OpsEvidence{CaseID: c.ID, Generation: c.Generation, Kind: kind, Source: source, Status: status, ObservedAt: observed, WindowStart: start, WindowEnd: end, CollectedAt: s.now(), Payload: raw, SHA256: knowledge.Digest([]byte(raw)), CreatedBy: user}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		current, err := s.get(tx, user, c.ID, false)
		if err != nil {
			return err
		}
		if current.Generation != c.Generation || current.State == "closed" {
			return opsauth.Fail(409, "evidence_generation", "Case changed while collecting evidence")
		}
		return tx.Create(&e).Error
	})
	return &e, err
}
func (s *Service) CollectMetric(ctx context.Context, user, id int64, query string, window int64) (*models.OpsEvidence, error) {
	c, err := s.Get(user, id, false)
	if err != nil {
		return nil, err
	}
	if c.State == "closed" {
		return nil, opsauth.Fail(409, "case_closed", "Case is closed")
	}
	if s.Query == nil {
		return nil, opsauth.Fail(503, "query_unavailable", "Metric query is unavailable")
	}
	if window < 0 || window > 3600 || len(query) > 16000 {
		return nil, opsauth.Fail(422, "query_invalid", "Query window or expression is invalid")
	}
	end := s.now()
	result, err := s.Query(ctx, user, c, query, end-window, end)
	result.RequestedQuery = strings.TrimSpace(query)
	status := result.Status
	if err != nil {
		status = "error"
		result.Reason = err.Error()
	}
	if status == "" {
		status = "ok"
	}
	observed := int64(0)
	for _, p := range result.Points {
		at := p.ObservedAt
		if at == 0 {
			at = p.At
		}
		if at > observed {
			observed = at
		}
	}
	if status == "ok" && len(result.Points) == 0 {
		status = "empty"
		result.Reason = "No metric samples were returned"
	}
	if status == "ok" && observed > 0 && observed < end-60 {
		status = "stale"
		result.Reason = "Latest underlying sample is older than 60 seconds"
	}
	return s.persistEvidence(c, user, "metric", "datasource", status, observed, end-window, end, result)
}
func (s *Service) RecordAgentStep(user, id int64, key, state, note string, ids []int64) error {
	if s.scope == nil {
		return opsauth.Fail(403, "run_required", "An active investigation scope is required")
	}
	return s.recordStep(user, id, key, state, note, ids, true)
}
func (s *Service) AddManualEvidence(user, id int64, note string) (*models.OpsEvidence, error) {
	c, err := s.Get(user, id, true)
	if err != nil {
		return nil, err
	}
	if c.State == "closed" || strings.TrimSpace(note) == "" {
		return nil, opsauth.Fail(422, "manual_evidence", "A note on an open case is required")
	}
	return s.persistEvidence(c, user, "manual", "user", "ok", s.now(), 0, 0, map[string]interface{}{"note": note, "confirmed_by": user})
}
func (s *Service) evidenceRefs(tx *gorm.DB, c *models.OpsCase, ids []int64) error {
	if len(ids) == 0 || len(ids) > 30 {
		return opsauth.Fail(422, "evidence_required", "Evidence references are required")
	}
	for _, id := range ids {
		var e models.OpsEvidence
		if err := tx.Where("id = ? AND case_id = ? AND generation = ?", id, c.ID, c.Generation).First(&e).Error; err != nil {
			return opsauth.Fail(422, "evidence_invalid", "Evidence is missing or belongs to another case generation")
		}
	}
	return nil
}
func (s *Service) RecordStep(user, id int64, key, state, note string, ids []int64) error {
	return s.recordStep(user, id, key, state, note, ids, false)
}
func (s *Service) recordStep(user, id int64, key, state, note string, ids []int64, agent bool) error {
	if state != "completed" && state != "failed" && state != "skipped" {
		return opsauth.Fail(422, "step_state", "Invalid step result")
	}
	if len(note) > 16000 {
		return opsauth.Fail(422, "note_limit", "Step note is too large")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		c, err := s.get(tx, user, id, !agent)
		if err != nil {
			return err
		}
		if c.State == "closed" {
			return opsauth.Fail(409, "case_closed", "Case is closed")
		}
		spec, err := s.withDB(tx).activeProcedure(user, c)
		if err != nil {
			return err
		}
		found := false
		var definition knowledge.Step
		for _, st := range spec.Steps {
			if st.Key == key {
				found = true
				definition = st
			}
		}
		if !found {
			return opsauth.Fail(422, "step_missing", "Step is not part of the active procedure")
		}
		if agent && definition.Kind == "manual" {
			return opsauth.Fail(403, "manual_step", "A manual step requires an operator")
		}
		if state != "skipped" && definition.Kind == "metric" {
			for _, evidenceID := range ids {
				var evidence models.OpsEvidence
				if err = tx.Where("id = ? AND case_id = ? AND generation = ?", evidenceID, id, c.Generation).First(&evidence).Error; err != nil {
					return opsauth.Fail(422, "evidence_invalid", "Evidence is missing or belongs to another case generation")
				}
				var metric MetricResult
				if evidence.Kind != "metric" || json.Unmarshal([]byte(evidence.Payload), &metric) != nil || metric.RequestedQuery != strings.TrimSpace(definition.Query) {
					return opsauth.Fail(422, "step_query", "Metric step evidence must come from that step's query")
				}
			}
		}
		if state == "skipped" {
			if strings.TrimSpace(note) == "" {
				return opsauth.Fail(422, "reason_required", "Skipping a step requires a reason")
			}
		} else if err = s.evidenceRefs(tx, c, ids); err != nil {
			return err
		}
		if state == "completed" {
			var failed int64
			if err = tx.Model(&models.OpsEvidence{}).Where("id IN ? AND status <> ?", ids, "ok").Count(&failed).Error; err != nil {
				return err
			}
			if failed > 0 {
				return opsauth.Fail(422, "step_evidence_status", "A completed step requires successful evidence; record failed collection as a failed step")
			}
		}
		step := models.OpsStep{CaseID: id, Generation: c.Generation, VersionID: c.KnowledgeVersionID, PlanID: c.PlanID, StepKey: key, State: state, EvidenceIDs: opsauth.JSON(ids), Note: note, CreatedBy: user, CreatedAt: s.now()}
		if err = tx.Create(&step).Error; err != nil {
			return err
		}
		return s.audit(tx, id, user, "step_result", map[string]interface{}{"step_id": step.ID, "state": state})
	})
}
func (s *Service) Feedback(user, id int64, note string) (*models.KnowledgeItem, error) {
	c, err := s.Get(user, id, true)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(note) == "" {
		return nil, opsauth.Fail(422, "feedback_required", "Knowledge feedback is required")
	}
	spec := knowledge.Spec{Summary: note, SourceNote: "Operations case feedback; review required", Body: note, Conditions: map[string]string{}, Steps: []knowledge.Step{}, Actions: []knowledge.Action{}, Checks: []knowledge.Check{}, SourceIDs: []int64{}, RuleIDs: []int64{c.RuleID}}
	if c.PlanID > 0 {
		var p models.OpsPlan
		if err = s.DB.Where("id = ? AND case_id = ?", c.PlanID, id).First(&p).Error; err != nil {
			return nil, err
		}
		spec, err = knowledge.Decode(p.Payload)
		if err != nil {
			return nil, err
		}
		spec.ValidatedAt = 0
		spec.ReviewAt = 0
		spec.SourceIDs = []int64{}
		spec.RuleIDs = []int64{}
		if c.RuleID > 0 {
			spec.RuleIDs = []int64{c.RuleID}
		}
		spec.SourceNote = "Case plan feedback; case " + jsonNumber(id) + "; plan " + jsonNumber(p.ID) + "; SHA-256 " + p.SHA256 + "; review required"
		spec.Body += "\n\nCase scope: " + p.Scope + "\nRationale: " + p.Rationale + "\nSupporting evidence: " + string(p.EvidenceIDs) + "\nFeedback: " + note
	}
	spec.SourceNote += "; case " + jsonNumber(id)
	item, err := s.Knowledge.Save(user, 0, c.GroupID, 0, "Feedback: "+truncateBytes(c.Title, 220), spec)
	if err == nil {
		err = s.audit(s.DB, id, user, "knowledge_feedback", map[string]int64{"draft_id": item.ID, "adopted_version_id": c.KnowledgeVersionID, "plan_id": c.PlanID})
	}
	return item, err
}
func jsonNumber(v int64) string { b, _ := json.Marshal(v); return string(b) }
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
func IsConflict(err error) bool { var p *opsauth.Problem; return errors.As(err, &p) && p.Status == 409 }

func truncateBytes(s string, limit int) string {
	for len(s) > limit {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}
