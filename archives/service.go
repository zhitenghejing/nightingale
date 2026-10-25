// Package archives owns business documents, immutable versions and edit leases.
// It is consumed by Center only; no alert/edge cache or remote read path exists.
package archives

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MaxFileBytes    int64 = 50 * 1024 * 1024
	MaxImageBytes   int64 = 10 * 1024 * 1024
	MaxBodyBytes          = 5 * 1024 * 1024
	MaxArchiveBytes int64 = 500 * 1024 * 1024
	MaxDocuments          = 20
	LeaseDuration         = 5 * time.Minute
)

type Problem struct {
	Status  int
	Code    string
	Message string
	Current interface{}
}

func (e *Problem) Error() string { return e.Message }
func invalid(msg string) error {
	return &Problem{Status: http.StatusUnprocessableEntity, Code: "invalid_archive", Message: msg}
}
func conflict(msg string, current interface{}) error {
	return &Problem{Status: http.StatusConflict, Code: "archive_conflict", Message: msg, Current: current}
}
func missing() error {
	return &Problem{Status: http.StatusNotFound, Code: "archive_not_found", Message: "Archive or document not found"}
}

type Actor struct {
	ID       int64
	Username string
	Name     string
}
type Service struct {
	ExportRuntime *ExportRuntime
	DB            *gorm.DB
	Root          string
	Now           func() time.Time
}

func New(db *gorm.DB, root string) *Service {
	if root == "" {
		root = filepath.Join("data", "archives")
	}
	root, _ = filepath.Abs(root)
	return &Service{DB: db, Root: root, Now: time.Now, ExportRuntime: &ExportRuntime{TimeoutSeconds: 180}}
}
func (s *Service) Prepare() error {
	for _, name := range []string{"objects", "temporary", "exports"} {
		if err := os.MkdirAll(filepath.Join(s.Root, name), 0700); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) now() int64                 { return s.Now().UnixMilli() }
func encode(v interface{}) models.ArchiveJSON { b, _ := json.Marshal(v); return models.ArchiveJSON(b) }

// Metadata is shared structurally, with category-specific validation. Search
// columns are projections of this value, never independently writable fields.
type Metadata struct {
	Title              string   `json:"title"`
	Department         string   `json:"department"`
	Location           string   `json:"location"`
	TrainingStatus     string   `json:"training_status"`
	StartTime          string   `json:"start_time"`
	EndTime            string   `json:"end_time"`
	Duration           *float64 `json:"duration"`
	Lecturer           string   `json:"lecturer"`
	Participants       string   `json:"participants"`
	TrainingContent    string   `json:"training_content"`
	TrainingEffect     string   `json:"training_effect"`
	InspectionResult   string   `json:"inspection_result"`
	Scope              []string `json:"scope"`
	Handler            string   `json:"handler"`
	Summary            string   `json:"summary"`
	Level              string   `json:"level"`
	AffectedBusiness   string   `json:"affected_business"`
	OccurredAt         string   `json:"occurred_at"`
	ResolvedAt         string   `json:"resolved_at"`
	MainEngineer       string   `json:"main_engineer"`
	AuxiliaryEngineers string   `json:"auxiliary_engineers"`
	CustomType         string   `json:"custom_type"`
}

func ValidateMetadata(category string, m *Metadata, archived bool) error {
	m.Title = strings.TrimSpace(m.Title)
	m.AffectedBusiness = strings.TrimSpace(m.AffectedBusiness)
	if m.Scope == nil {
		m.Scope = []string{}
	}
	if len(encode(m)) > 64*1024 {
		return invalid("Archive information is too large")
	}
	for _, v := range []string{m.Title, m.Department, m.Location, m.Lecturer, m.Handler, m.MainEngineer, m.CustomType, m.AffectedBusiness} {
		if len([]rune(v)) > 255 {
			return invalid("Archive field exceeds 255 characters")
		}
	}
	switch category {
	case "training":
		if m.Title == "" {
			return invalid("Training title is required")
		}
		m.TrainingStatus = "normal"
		if m.Duration != nil && (*m.Duration <= 0 || math.IsNaN(*m.Duration) || math.IsInf(*m.Duration, 0)) {
			return invalid("Training duration must be positive")
		}
	case "inspection":
		if m.Title == "" {
			return invalid("Inspection title is required")
		}
		if m.InspectionResult != "" && m.InspectionResult != "normal" && m.InspectionResult != "abnormal" {
			return invalid("Inspection result must be normal or abnormal")
		}
		if archived && m.InspectionResult == "" {
			return invalid("Inspection result is required before archiving")
		}
		allowed := map[string]bool{"运维平台": true, "服务器": true, "存储": true, "安全设备": true, "网络": true, "交换机": true}
		seen := map[string]bool{}
		for _, v := range m.Scope {
			if !allowed[v] || seen[v] {
				return invalid("Invalid inspection equipment category")
			}
			seen[v] = true
		}
	case "incident":
		m.Level = strings.NewReplacer("Ⅰ", "I", "Ⅱ", "II", "Ⅲ", "III", "Ⅳ", "IV", "级", "").Replace(strings.TrimSpace(m.Level))
		if m.Level != "I" && m.Level != "II" && m.Level != "III" && m.Level != "IV" {
			return invalid("Incident level must be I, II, III or IV")
		}
		if m.AffectedBusiness == "" {
			return invalid("Affected business is required")
		}
	case "other":
		if m.Title == "" {
			return invalid("Archive title is required")
		}
	default:
		return invalid("Unknown archive category")
	}
	for _, pair := range [][2]string{{m.StartTime, m.EndTime}, {m.OccurredAt, m.ResolvedAt}} {
		var a, b time.Time
		for i, v := range pair {
			if v == "" {
				continue
			}
			t, err := time.Parse(time.RFC3339Nano, v)
			if err != nil {
				return invalid("Use an ISO timestamp with timezone")
			}
			if i == 0 {
				a = t
			} else {
				b = t
			}
		}
		if !a.IsZero() && !b.IsZero() && b.Before(a) {
			return invalid("End time cannot precede start time")
		}
	}
	return nil
}
func project(a *models.ArchiveRecord, m Metadata) {
	a.Metadata = encode(m)
	a.Title = m.Title
	a.Department = m.Department
	a.Location = m.Location
	a.Result = m.InspectionResult
	a.Level = m.Level
	a.StartAt = m.StartTime
	switch a.Category {
	case "training":
		a.Person = m.Lecturer
		a.Result = "normal"
	case "inspection":
		a.StartAt = time.UnixMilli(a.CreateAt).UTC().Format("2006-01-02T15:04:05.000000000Z")
		a.Person = m.Handler
	case "incident":
		a.Title = m.AffectedBusiness
		a.Person = m.MainEngineer
		a.StartAt = m.OccurredAt
	case "other":
		a.StartAt = time.UnixMilli(a.CreateAt).UTC().Format("2006-01-02T15:04:05.000000000Z")
	}
	if t, err := time.Parse(time.RFC3339Nano, a.StartAt); err == nil {
		a.StartAt = t.UTC().Format("2006-01-02T15:04:05.000000000Z")
	}
}

type Document struct {
	models.ArchiveDocument
	Version models.ArchiveDocumentVersion `json:"version"`
	File    *models.ArchiveBlob           `json:"file,omitempty"`
}
type Detail struct {
	models.ArchiveRecord
	Documents    []Document `json:"documents"`
	CurrentBytes int64      `json:"current_bytes"`
}
type SnapshotDocument struct {
	Document  models.ArchiveDocument `json:"document"`
	VersionID int64                  `json:"version_id"`
}
type Snapshot struct {
	Record    models.ArchiveRecord `json:"record"`
	Documents []SnapshotDocument   `json:"documents"`
}
type ListQuery struct {
	Category, Keyword, State, Department, Location, Result, Level, Start, End string
	Deleted                                                                   bool
	Page, Limit                                                               int
}
type ListResult struct {
	List  []Detail `json:"list"`
	Total int64    `json:"total"`
	Page  int      `json:"page"`
	Limit int      `json:"limit"`
}

func documents(db *gorm.DB, id int64) ([]Document, error) {
	rows := []models.ArchiveDocument{}
	if err := db.Where("archive_id = ? AND deleted = ?", id, false).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Document, 0, len(rows))
	for _, d := range rows {
		v := models.ArchiveDocumentVersion{}
		if err := db.Omit("body").First(&v, d.CurrentVersionID).Error; err != nil {
			return nil, err
		}
		item := Document{ArchiveDocument: d, Version: v}
		if v.BlobID > 0 {
			var b models.ArchiveBlob
			if err := db.First(&b, v.BlobID).Error; err != nil {
				return nil, err
			}
			item.File = &b
		}
		out = append(out, item)
	}
	return out, nil
}
func detail(db *gorm.DB, a models.ArchiveRecord) (*Detail, error) {
	docs, err := documents(db, a.ID)
	if err != nil {
		return nil, err
	}
	size, err := currentBytes(db, a.ID, docs)
	if err != nil {
		return nil, err
	}
	return &Detail{ArchiveRecord: a, Documents: docs, CurrentBytes: size}, nil
}
func (s *Service) Get(id int64) (*Detail, error) {
	var a models.ArchiveRecord
	if err := s.DB.First(&a, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, missing()
		}
		return nil, err
	}
	return detail(s.DB, a)
}
func (s *Service) List(q ListQuery) (*ListResult, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Limit < 1 {
		q.Limit = 20
	}
	if q.Limit > 100 {
		q.Limit = 100
	}
	for _, value := range []*string{&q.Start, &q.End} {
		if *value != "" {
			t, err := time.Parse(time.RFC3339Nano, *value)
			if err != nil {
				return nil, invalid("Use an ISO timestamp with timezone")
			}
			*value = t.UTC().Format("2006-01-02T15:04:05.000000000Z")
		}
	}

	db := s.DB.Model(&models.ArchiveRecord{}).Where("deleted = ?", q.Deleted)
	if q.Category != "" {
		db = db.Where("category = ?", q.Category)
	}
	if q.Keyword != "" {
		key := "%" + q.Keyword + "%"
		db = db.Where("(title LIKE ? OR person LIKE ?)", key, key)
	}
	for _, f := range [][2]string{{"state", q.State}, {"department", q.Department}, {"location", q.Location}, {"result", q.Result}, {"level", q.Level}} {
		if f[1] != "" {
			db = db.Where(f[0]+" = ?", f[1])
		}
	}
	if q.Start != "" {
		db = db.Where("start_at >= ?", q.Start)
	}
	if q.End != "" {
		db = db.Where("start_at <= ?", q.End)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, err
	}
	rows := []models.ArchiveRecord{}
	if err := db.Order("update_at desc, id desc").Offset((q.Page - 1) * q.Limit).Limit(q.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := &ListResult{List: []Detail{}, Total: total, Page: q.Page, Limit: q.Limit}
	for _, a := range rows {
		d, err := detail(s.DB, a)
		if err != nil {
			return nil, err
		}
		out.List = append(out.List, *d)
	}
	return out, nil
}

func (s *Service) log(tx *gorm.DB, actor Actor, archiveID, documentID int64, action string, value interface{}) error {
	return tx.Create(&models.ArchiveLog{ArchiveID: archiveID, DocumentID: documentID, Action: action, Detail: encode(value), ActorID: actor.ID, ActorUsername: actor.Username, ActorName: actor.Name, CreateAt: s.now()}).Error
}
func (s *Service) snapshot(tx *gorm.DB, a *models.ArchiveRecord, actor Actor, action string) error {
	docs, err := documents(tx, a.ID)
	if err != nil {
		return err
	}
	refs := make([]SnapshotDocument, 0, len(docs))
	for _, d := range docs {
		refs = append(refs, SnapshotDocument{Document: d.ArchiveDocument, VersionID: d.Version.ID})
	}
	return tx.Create(&models.ArchiveVersion{ArchiveID: a.ID, Sequence: a.Revision, Snapshot: encode(Snapshot{Record: *a, Documents: refs}), Action: action, CreateAt: s.now(), CreateBy: actor.ID, CreateName: actor.Name}).Error
}
func (s *Service) Create(actor Actor, category string, m Metadata) (*Detail, error) {
	if err := ValidateMetadata(category, &m, false); err != nil {
		return nil, err
	}
	a := models.ArchiveRecord{Category: category, State: "draft", Revision: 1, CreateAt: s.now(), UpdateAt: s.now(), CreateBy: actor.ID, UpdateBy: actor.ID, CreateName: actor.Name, UpdateName: actor.Name}
	project(&a, m)
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&a).Error; err != nil {
			return err
		}
		if err := s.snapshot(tx, &a, actor, "create"); err != nil {
			return err
		}
		return s.log(tx, actor, a.ID, 0, "create", map[string]interface{}{"after": a})
	})
	if err != nil {
		return nil, err
	}
	return s.Get(a.ID)
}

// mutate locks the aggregate before quota/state checks; revision CAS also
// protects dialects whose row-lock clause is ignored and stale metadata forms.
func (s *Service) mutate(actor Actor, id, revision int64, action string, fn func(*gorm.DB, *models.ArchiveRecord) error) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var a models.ArchiveRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&a, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return missing()
			}
			return err
		}
		if revision > 0 && a.Revision != revision {
			return conflict("Archive changed; reload before saving", a.Revision)
		}
		before := a
		if a.Deleted && action != "restore" && action != "restore_version" {
			return missing()
		}
		if err := fn(tx, &a); err != nil {
			return err
		}
		docs, err := documents(tx, a.ID)
		if err != nil {
			return err
		}
		if len(docs) > MaxDocuments {
			return invalid("An archive can contain at most 20 documents")
		}
		size, err := currentBytes(tx, a.ID, docs)
		if err != nil {
			return err
		}
		if size > MaxArchiveBytes {
			return invalid("Current archive materials exceed 500 MiB")
		}
		if !a.Deleted && a.State == "archived" {
			if err := validateMaterials(a, docs); err != nil {
				return err
			}
		}
		a.Revision++
		a.UpdateAt = s.now()
		a.UpdateBy = actor.ID
		a.UpdateName = actor.Name
		res := tx.Model(&models.ArchiveRecord{}).Where("id = ? AND revision = ?", a.ID, before.Revision).Select("*").Omit("id", "create_at", "create_by", "create_name").Updates(&a)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return conflict("Archive changed; retry", nil)
		}
		if err := s.snapshot(tx, &a, actor, action); err != nil {
			return err
		}
		return s.log(tx, actor, id, 0, action, map[string]interface{}{"before": before, "after": a})
	})
}
func (s *Service) Update(actor Actor, id, revision int64, m Metadata, primary int64) (*Detail, error) {
	err := s.mutate(actor, id, revision, "update_fields", func(tx *gorm.DB, a *models.ArchiveRecord) error {
		if err := ValidateMetadata(a.Category, &m, a.State == "archived"); err != nil {
			return err
		}
		project(a, m)
		if primary > 0 {
			var n int64
			if err := tx.Model(&models.ArchiveDocument{}).Where("id = ? AND archive_id = ? AND deleted = ?", primary, id, false).Count(&n).Error; err != nil {
				return err
			}
			if n != 1 {
				return invalid("Primary document does not belong to this archive")
			}
			a.PrimaryDocumentID = primary
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}
func (s *Service) Archive(actor Actor, id, revision int64) (*Detail, error) {
	err := s.mutate(actor, id, revision, "archive", func(tx *gorm.DB, a *models.ArchiveRecord) error {
		if err := s.unoccupied(tx, id, 0); err != nil {
			return err
		}
		var m Metadata
		if err := json.Unmarshal([]byte(a.Metadata), &m); err != nil {
			return err
		}
		if err := ValidateMetadata(a.Category, &m, true); err != nil {
			return err
		}
		a.State = "archived"
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}
func (s *Service) Delete(actor Actor, id, revision int64) error {
	return s.mutate(actor, id, revision, "delete", func(tx *gorm.DB, a *models.ArchiveRecord) error {
		if err := s.unoccupied(tx, id, 0); err != nil {
			return err
		}
		a.Deleted = true
		return nil
	})
}
func (s *Service) Restore(actor Actor, id, revision int64) (*Detail, error) {
	err := s.mutate(actor, id, revision, "restore", func(tx *gorm.DB, a *models.ArchiveRecord) error { a.Deleted = false; return nil })
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}
func (s *Service) unoccupied(tx *gorm.DB, archiveID, documentID int64) error {
	q := tx.Model(&models.ArchiveEditLease{}).Where("expires_at > ?", s.now())
	if documentID > 0 {
		q = q.Where("document_id = ?", documentID)
	} else {
		q = q.Where("document_id IN (?)", tx.Model(&models.ArchiveDocument{}).Select("id").Where("archive_id = ?", archiveID))
	}
	var lease models.ArchiveEditLease
	err := q.First(&lease).Error
	if err == nil {
		return conflict("Document is already being edited", map[string]interface{}{"owner": lease.OwnerName, "expires_at": lease.ExpiresAt})
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

func validateMaterials(a models.ArchiveRecord, docs []Document) error {
	has := map[string]bool{}
	any := false
	for _, d := range docs {
		ok := d.Version.HasContent
		if ok {
			any = true
			if d.Purpose != "attendance" || d.Kind != "online" {
				has[d.Purpose] = true
			}
		}
	}
	switch a.Category {
	case "training":
		if !has["training_material"] || !has["attendance"] {
			return invalid("Training requires teaching materials and an uploaded attendance file")
		}
	case "inspection":
		if !has["inspection_report"] {
			return invalid("Inspection report is required")
		}
	case "incident":
		if !has["incident_report"] {
			return invalid("Incident report is required")
		}
	case "other":
		if !any {
			return invalid("At least one non-empty document is required")
		}
	}
	return nil
}
func purposeValid(category, purpose string) bool {
	allowed := map[string][]string{"training": {"training_material", "attendance", "other"}, "inspection": {"inspection_report", "other"}, "incident": {"incident_report", "other"}, "other": {"other"}}
	for _, p := range allowed[category] {
		if p == purpose {
			return true
		}
	}
	return false
}
func hasContent(body models.ArchiveJSON) bool {
	var v interface{}
	if json.Unmarshal([]byte(body), &v) != nil {
		return false
	}
	found := false
	var walk func(interface{})
	walk = func(x interface{}) {
		switch n := x.(type) {
		case map[string]interface{}:
			if n["type"] == "image" {
				found = true
			}
			if t, ok := n["value"].(string); ok && strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(t, "\u200b", ""), "\u200c", "")) != "" {
				found = true
			}
			for _, c := range n {
				if _, ok := c.(map[string]interface{}); ok {
					walk(c)
				}
				if _, ok := c.([]interface{}); ok {
					walk(c)
				}
			}
		case []interface{}:
			for _, c := range n {
				walk(c)
			}
		}
	}
	if root, ok := v.(map[string]interface{}); ok {
		if data, ok := root["data"].(map[string]interface{}); ok {
			walk(data["main"])
		}
	}
	return found
}

// Audit records a completed download/export request using its actual login identity.
func (s *Service) Audit(actor Actor, archiveID, documentID int64, action string, detail interface{}) error {
	var record models.ArchiveRecord
	if err := s.DB.First(&record, archiveID).Error; err != nil {
		return missing()
	}
	if documentID > 0 {
		var doc models.ArchiveDocument
		if err := s.DB.Where("id = ? AND archive_id = ?", documentID, archiveID).First(&doc).Error; err != nil {
			return missing()
		}
	}
	return s.log(s.DB, actor, archiveID, documentID, action, detail)
}
