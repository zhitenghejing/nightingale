// Package knowledge owns operations knowledge, immutable releases and private sources.
package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"gorm.io/gorm"
)

type Step struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	Query       string `json:"query"`
	Instruction string `json:"instruction"`
	Expected    string `json:"expected"`
	Next        string `json:"next"`
	OnFailure   string `json:"on_failure"`
}
type Check struct {
	Key           string  `json:"key"`
	Title         string  `json:"title"`
	Kind          string  `json:"kind"`
	Query         string  `json:"query"`
	Operator      string  `json:"operator"`
	Threshold     float64 `json:"threshold"`
	WindowSeconds int64   `json:"window_seconds"`
	FreshSeconds  int64   `json:"fresh_seconds"`
	Required      bool    `json:"required"`
}
type Action struct {
	RequiredSteps []string `json:"required_steps,omitempty"`
	Key           string   `json:"key"`
	Title         string   `json:"title"`
	Kind          string   `json:"kind"`
	TemplateID    int64    `json:"template_id"`
	Instruction   string   `json:"instruction"`
	Risk          string   `json:"risk"`
	Rollback      string   `json:"rollback"`
}
type Spec struct {
	Summary     string            `json:"summary"`
	Body        string            `json:"body"`
	SourceIDs   []int64           `json:"source_ids"`
	SourceNote  string            `json:"source_note"`
	RuleIDs     []int64           `json:"rule_ids"`
	Conditions  map[string]string `json:"conditions"`
	Steps       []Step            `json:"steps"`
	Actions     []Action          `json:"actions"`
	Checks      []Check           `json:"checks"`
	Escalation  string            `json:"escalation"`
	ValidatedAt int64             `json:"validated_at"`
	ReviewAt    int64             `json:"review_at"`
	TestOnly    bool              `json:"test_only"`
}
type Store struct {
	DB      *gorm.DB
	DataDir string
	Now     func() time.Time
}

func (s *Store) now() int64 {
	if s.Now != nil {
		return s.Now().Unix()
	}
	return time.Now().Unix()
}
func Digest(v []byte) string { h := sha256.Sum256(v); return hex.EncodeToString(h[:]) }
func Decode(v models.OpsJSON) (Spec, error) {
	var spec Spec
	err := json.Unmarshal([]byte(v), &spec)
	return spec, err
}

func Validate(spec Spec, published bool) error {
	if len(spec.Body) > 1024*1024 || len(spec.Summary) > 4000 || len(spec.Steps) > 50 || len(spec.Checks) > 30 || len(spec.Actions) > 20 || len(spec.SourceIDs) > 30 || len(spec.Conditions) > 30 {
		return opsauth.Fail(422, "knowledge_limit", "Knowledge exceeds the allowed size")
	}
	if !published {
		return nil
	}
	if strings.TrimSpace(spec.Summary) == "" || len(spec.Steps) == 0 || len(spec.Checks) == 0 || spec.ValidatedAt <= 0 || spec.ReviewAt <= spec.ValidatedAt || (len(spec.SourceIDs) == 0 && strings.TrimSpace(spec.SourceNote) == "") {
		return opsauth.Fail(422, "knowledge_incomplete", "Summary, source, steps, validation dates and recovery checks are required")
	}
	return ValidateProcedure(spec)
}

// ValidateProcedure checks executable structure without claiming that a method
// has been validated for reuse or published as knowledge.
func ValidateProcedure(spec Spec) error {
	if err := Validate(spec, false); err != nil {
		return err
	}
	if strings.TrimSpace(spec.Summary) == "" || len(spec.Steps) == 0 || len(spec.Checks) == 0 {
		return opsauth.Fail(422, "procedure_incomplete", "Summary, steps and recovery checks are required")
	}
	keys := map[string]bool{}
	for _, st := range spec.Steps {
		if st.Key == "" || len(st.Key) > 64 || keys[st.Key] || st.Title == "" || (st.Kind != "manual" && st.Kind != "metric") {
			return opsauth.Fail(422, "step_invalid", "Invalid or duplicate knowledge step")
		}
		keys[st.Key] = true
		if st.Kind == "metric" && strings.TrimSpace(st.Query) == "" {
			return opsauth.Fail(422, "query_missing", "Metric step requires a query")
		}
	}
	for _, st := range spec.Steps {
		for _, next := range []string{st.Next, st.OnFailure} {
			if next != "" && !keys[next] {
				return opsauth.Fail(422, "branch_invalid", "Step branch must reference an existing step")
			}
		}
	}
	keys = map[string]bool{}
	required := 0
	for _, c := range spec.Checks {
		if c.Key == "" || len(c.Key) > 64 || keys[c.Key] || c.Title == "" {
			return opsauth.Fail(422, "check_invalid", "Invalid or duplicate recovery check")
		}
		keys[c.Key] = true
		if c.Required {
			required++
		}
		if c.Kind != "manual" && c.Kind != "metric" && c.Kind != "demo_business" && c.Kind != "demo_data" {
			return opsauth.Fail(422, "check_kind", "Unsupported recovery check")
		}
		if (c.Kind == "demo_business" || c.Kind == "demo_data") && !spec.TestOnly {
			return opsauth.Fail(422, "demo_only", "Demo checks require test-only knowledge")
		}
		if c.Kind == "metric" {
			if math.IsNaN(c.Threshold) || math.IsInf(c.Threshold, 0) || c.Query == "" || c.WindowSeconds < 0 || c.WindowSeconds > 3600 || c.FreshSeconds < 1 || c.FreshSeconds > 3600 || (c.WindowSeconds > 0 && c.FreshSeconds < 5) {
				return opsauth.Fail(422, "check_window", "Metric checks require a bounded window and freshness")
			}
			switch c.Operator {
			case "<", "<=", ">", ">=", "==", "!=":
			default:
				return opsauth.Fail(422, "check_operator", "Unsupported comparison operator")
			}
		}
	}
	if required == 0 {
		return opsauth.Fail(422, "required_check", "At least one recovery check must be required")
	}
	keys = map[string]bool{}
	for _, a := range spec.Actions {
		seen := map[string]bool{}
		for _, key := range a.RequiredSteps {
			exists := false
			for _, step := range spec.Steps {
				if step.Key == key {
					exists = true
					break
				}
			}
			if !exists || seen[key] {
				return opsauth.Fail(422, "action_prerequisites", "Action prerequisites must reference distinct existing steps")
			}
			seen[key] = true
		}
		if a.Key == "" || len(a.Key) > 64 || keys[a.Key] || a.Title == "" || a.Risk == "" || a.Rollback == "" {
			return opsauth.Fail(422, "action_invalid", "Action requires a unique key, title, risks and rollback")
		}
		keys[a.Key] = true
		if a.Kind != "manual" && a.Kind != "demo_reclaim" && a.Kind != "ibex_template" {
			return opsauth.Fail(422, "action_kind", "Unsupported action kind")
		}
		if a.Kind == "demo_reclaim" && !spec.TestOnly {
			return opsauth.Fail(422, "demo_only", "Demo action requires test-only knowledge")
		}
		if a.Kind == "ibex_template" && a.TemplateID <= 0 {
			return opsauth.Fail(422, "template_required", "Action requires a fixed task template")
		}
	}
	return nil
}
func (s *Store) List(user, group int64) ([]models.KnowledgeItem, error) {
	q := s.DB.Model(&models.KnowledgeItem{})
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
	rows := []models.KnowledgeItem{}
	err := q.Order("updated_at DESC, id DESC").Limit(200).Find(&rows).Error
	// Drafts are writable-group material; read-only users only see published content.
	for i := range rows {
		if _, e := opsauth.Check(s.DB, user, rows[i].GroupID, true); e != nil {
			rows[i].Draft = "null"
			if rows[i].CurrentVersionID == 0 {
				rows[i].ID = 0
				continue
			}
			var version models.KnowledgeVersion
			if e := s.DB.First(&version, rows[i].CurrentVersionID).Error; e != nil {
				return nil, e
			}
			rows[i].Title = version.Title
		}
	}
	visible := rows[:0]
	for _, row := range rows {
		if row.ID > 0 {
			visible = append(visible, row)
		}
	}
	return visible, err
}
func (s *Store) Get(user, id int64) (*models.KnowledgeItem, error) {
	var row models.KnowledgeItem
	if err := s.DB.First(&row, id).Error; err != nil {
		return nil, err
	}
	if _, err := opsauth.Check(s.DB, user, row.GroupID, false); err != nil {
		return nil, err
	}
	if _, err := opsauth.Check(s.DB, user, row.GroupID, true); err != nil {
		row.Draft = "null"
		if row.CurrentVersionID == 0 {
			return nil, opsauth.Fail(404, "knowledge_unpublished", "Unpublished knowledge requires edit permission")
		}
		var version models.KnowledgeVersion
		if e := s.DB.First(&version, row.CurrentVersionID).Error; e != nil {
			return nil, e
		}
		row.Title = version.Title
	}
	return &row, nil
}
func (s *Store) Save(user, id, group, revision int64, title string, spec Spec) (*models.KnowledgeItem, error) {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 255 {
		return nil, opsauth.Fail(422, "title_required", "A title of at most 255 bytes is required")
	}
	if err := Validate(spec, false); err != nil {
		return nil, err
	}
	var row models.KnowledgeItem
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if id > 0 {
			if err := tx.First(&row, id).Error; err != nil {
				return err
			}
			group = row.GroupID
		}
		if _, err := opsauth.Check(tx, user, group, true); err != nil {
			return err
		}
		if err := s.checkSources(tx, group, spec.SourceIDs); err != nil {
			return err
		}
		if id == 0 {
			row = models.KnowledgeItem{GroupID: group, Title: title, Draft: opsauth.JSON(spec), Revision: 1, CreatedBy: user, UpdatedBy: user, UpdatedAt: s.now()}
			return tx.Create(&row).Error
		}
		result := tx.Model(&models.KnowledgeItem{}).Where("id = ? AND revision = ?", id, revision).Updates(map[string]interface{}{"title": title, "draft": opsauth.JSON(spec), "revision": revision + 1, "updated_by": user, "updated_at": s.now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return opsauth.Fail(409, "revision_conflict", "Knowledge was changed; reload before saving")
		}
		row.Title = title
		row.Draft = opsauth.JSON(spec)
		row.Revision++
		row.UpdatedBy = user
		row.UpdatedAt = s.now()
		return nil
	})
	return &row, err
}
func (s *Store) checkSources(tx *gorm.DB, group int64, ids []int64) error {
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			return opsauth.Fail(422, "duplicate_source", "Duplicate source")
		}
		seen[id] = true
		var row models.KnowledgeSource
		if err := tx.Where("id = ? AND group_id = ?", id, group).First(&row).Error; err != nil {
			return opsauth.Fail(422, "source_forbidden", "Source is missing or belongs to another group")
		}
	}
	return nil
}
func (s *Store) Publish(user, id, revision int64) (*models.KnowledgeVersion, error) {
	var version models.KnowledgeVersion
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var item models.KnowledgeItem
		if err := tx.First(&item, id).Error; err != nil {
			return err
		}
		if _, err := opsauth.Check(tx, user, item.GroupID, true); err != nil {
			return err
		}
		if revision != item.Revision {
			return opsauth.Fail(409, "revision_conflict", "Knowledge was changed; reload before publishing")
		}
		spec, err := Decode(item.Draft)
		if err != nil {
			return err
		}
		if err = Validate(spec, true); err != nil {
			return err
		}
		if spec.ValidatedAt > s.now() || spec.ReviewAt <= s.now() {
			return opsauth.Fail(422, "review_dates", "Knowledge validation or review date is invalid")
		}
		if err = s.checkSources(tx, item.GroupID, spec.SourceIDs); err != nil {
			return err
		}
		var seq int64
		if err = tx.Model(&models.KnowledgeVersion{}).Where("item_id = ?", id).Select("COALESCE(MAX(sequence),0)").Scan(&seq).Error; err != nil {
			return err
		}
		version = models.KnowledgeVersion{ItemID: id, GroupID: item.GroupID, Sequence: seq + 1, Title: item.Title, Payload: item.Draft, SHA256: Digest([]byte(item.Draft)), PublishedBy: user, PublishedAt: s.now()}
		if err = tx.Create(&version).Error; err != nil {
			return err
		}
		r := tx.Model(&models.KnowledgeItem{}).Where("id = ? AND revision = ?", id, revision).Updates(map[string]interface{}{"current_version_id": version.ID, "revision": revision + 1, "updated_by": user, "updated_at": s.now()})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return opsauth.Fail(409, "revision_conflict", "Knowledge was changed; reload before publishing")
		}
		return nil
	})
	return &version, err
}
func (s *Store) Version(user, id int64) (*models.KnowledgeVersion, Spec, error) {
	var v models.KnowledgeVersion
	if err := s.DB.First(&v, id).Error; err != nil {
		return nil, Spec{}, err
	}
	if _, err := opsauth.Check(s.DB, user, v.GroupID, false); err != nil {
		return nil, Spec{}, err
	}
	spec, err := Decode(v.Payload)
	return &v, spec, err
}
func (s *Store) Versions(user, item int64) ([]models.KnowledgeVersion, error) {
	if _, err := s.Get(user, item); err != nil {
		return nil, err
	}
	rows := []models.KnowledgeVersion{}
	err := s.DB.Where("item_id = ?", item).Order("sequence DESC").Limit(100).Find(&rows).Error
	return rows, err
}
func (s *Store) Withdraw(user, id int64, reason string) error {
	v, _, err := s.Version(user, id)
	if err != nil {
		return err
	}
	if _, err = opsauth.Check(s.DB, user, v.GroupID, true); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return opsauth.Fail(422, "reason_required", "Withdrawal reason is required")
	}
	return s.DB.Model(v).Where("withdrawn_at = 0").Updates(map[string]interface{}{"withdrawn_at": s.now(), "withdraw_reason": reason}).Error
}

type Condition struct {
	Key      string `json:"key"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	State    string `json:"state"`
}
type Match struct {
	Version    models.KnowledgeVersion `json:"version"`
	Spec       Spec                    `json:"spec"`
	Conditions []Condition             `json:"conditions"`
	Applicable bool                    `json:"applicable"`
	Score      int                     `json:"score"`
	Reason     string                  `json:"reason"`
}

func Conditions(spec Spec, attrs map[string]string) ([]Condition, bool) {
	rows := []Condition{}
	ok := true
	conditions := map[string]string{}
	for k, v := range spec.Conditions {
		conditions[k] = v
	}
	if spec.TestOnly {
		conditions["environment"] = "test"
	}
	keys := make([]string, 0, len(conditions))
	for k := range conditions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		want := conditions[k]
		actual, exists := attrs[k]
		state := "satisfied"
		if !exists || actual == "" {
			state = "unknown"
			ok = false
		} else if actual != want {
			state = "mismatch"
			ok = false
		}
		rows = append(rows, Condition{k, want, actual, state})
	}
	return rows, ok
}
func (s *Store) Applicable(user, version int64, attrs map[string]string) (*models.KnowledgeVersion, Spec, error) {
	v, spec, err := s.Version(user, version)
	if err != nil {
		return nil, spec, err
	}
	if v.WithdrawnAt > 0 || spec.ReviewAt <= s.now() {
		return nil, spec, opsauth.Fail(409, "knowledge_inactive", "Knowledge was withdrawn or requires review")
	}
	_, ok := Conditions(spec, attrs)
	if !ok {
		return nil, spec, opsauth.Fail(422, "knowledge_inapplicable", "Required environment conditions are missing or do not match")
	}
	return v, spec, nil
}
func (s *Store) Search(user, group, rule int64, query string, attrs map[string]string) ([]Match, error) {
	if _, err := opsauth.Check(s.DB, user, group, false); err != nil {
		return nil, err
	}
	items := []models.KnowledgeItem{}
	if err := s.DB.Where("group_id = ? AND current_version_id > 0", group).Limit(200).Find(&items).Error; err != nil {
		return nil, err
	}
	out := []Match{}
	for _, item := range items {
		v, spec, err := s.Version(user, item.CurrentVersionID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		if v.WithdrawnAt > 0 || spec.ReviewAt <= s.now() {
			continue
		}
		conds, ok := Conditions(spec, attrs)
		score := 0
		for _, rid := range spec.RuleIDs {
			if rule > 0 && rid == rule {
				score += 100
			}
		}
		if query != "" {
			for _, word := range strings.Fields(strings.ToLower(query)) {
				if strings.Contains(strings.ToLower(v.Title+" "+spec.Summary), word) {
					score += 5
				}
			}
		}
		if ok {
			score += 10
		}
		if len(spec.RuleIDs) > 0 && rule > 0 && score < 100 {
			continue
		}
		reason := fmt.Sprintf("Matching score %d; required conditions checked", score)
		out = append(out, Match{*v, spec, conds, ok, score, reason})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Score > out[j-1].Score; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > 20 {
		out = out[:20]
	}
	return out, nil
}
