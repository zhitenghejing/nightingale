package archives

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EditInput struct {
	SessionID     string             `json:"session_id"`
	Epoch         int64              `json:"epoch"`
	BaseVersionID int64              `json:"base_version_id"`
	Body          models.ArchiveJSON `json:"body"`
	Mode          string             `json:"mode"`
	Markdown      string             `json:"markdown"`
	SwitchMode    bool               `json:"switch_mode"`
	Description   string             `json:"description"`
}
type Editing struct {
	Lease    models.ArchiveEditLease `json:"lease"`
	Draft    *models.ArchiveDraft    `json:"draft,omitempty"`
	Document Document                `json:"document"`
}

func findDocument(tx *gorm.DB, id int64) (*models.ArchiveDocument, error) {
	var d models.ArchiveDocument
	if err := tx.Where("id = ? AND deleted = ?", id, false).First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, missing()
		}
		return nil, err
	}
	var a models.ArchiveRecord
	if err := tx.Where("id = ? AND deleted = ?", d.ArchiveID, false).First(&a).Error; err != nil {
		return nil, missing()
	}
	return &d, nil
}
func (s *Service) Document(id, versionID int64) (*Document, error) {
	var d models.ArchiveDocument
	if err := s.DB.First(&d, id).Error; err != nil {
		return nil, missing()
	}
	if versionID == 0 {
		versionID = d.CurrentVersionID
	}
	var v models.ArchiveDocumentVersion
	if err := s.DB.Where("id = ? AND document_id = ?", versionID, id).First(&v).Error; err != nil {
		return nil, missing()
	}
	out := &Document{ArchiveDocument: d, Version: v}
	if v.BlobID > 0 {
		var b models.ArchiveBlob
		if err := s.DB.First(&b, v.BlobID).Error; err != nil {
			return nil, err
		}
		out.File = &b
		out.Kind = b.Kind
	}
	return out, nil
}
func (s *Service) Acquire(actor Actor, documentID int64) (*Editing, error) {
	var lease models.ArchiveEditLease
	var draft *models.ArchiveDraft
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		d, err := findDocument(tx, documentID)
		if err != nil {
			return err
		}
		var record models.ArchiveRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND deleted = ?", d.ArchiveID, false).First(&record).Error; err != nil {
			return missing()
		}
		d, err = findDocument(tx, documentID)
		if err != nil {
			return err
		}
		candidate := models.ArchiveEditLease{DocumentID: documentID, SessionID: "", Epoch: 0}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "document_id"}}, DoNothing: true}).Create(&candidate).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lease, documentID).Error; err != nil {
			return err
		}
		if lease.ExpiresAt > s.now() {
			return conflict("Document is already being edited", map[string]interface{}{"owner": lease.OwnerName, "expires_at": lease.ExpiresAt})
		}
		previous := lease.Epoch
		lease.SessionID = uuid.NewString()
		lease.Epoch++
		lease.OwnerID = actor.ID
		lease.OwnerName = actor.Name
		lease.ExpiresAt = s.Now().Add(LeaseDuration).UnixMilli()
		lease.BaseVersionID = d.CurrentVersionID
		res := tx.Model(&models.ArchiveEditLease{}).Where("document_id = ? AND epoch = ? AND expires_at <= ?", documentID, previous, s.now()).Updates(map[string]interface{}{"session_id": lease.SessionID, "epoch": lease.Epoch, "owner_id": actor.ID, "owner_name": actor.Name, "expires_at": lease.ExpiresAt, "base_version_id": lease.BaseVersionID})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return conflict("Document was acquired by another editing session", nil)
		}
		var old models.ArchiveDraft
		err = tx.Where("document_id = ? AND owner_id = ?", documentID, actor.ID).First(&old).Error
		if err == nil {
			draft = &old
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	d, err := s.Document(documentID, 0)
	if err != nil {
		return nil, err
	}
	return &Editing{Lease: lease, Draft: draft, Document: *d}, nil
}
func (s *Service) checkLease(tx *gorm.DB, actor Actor, id int64, session string, epoch int64) (*models.ArchiveEditLease, error) {
	var l models.ArchiveEditLease
	if session == "" {
		return nil, conflict("An active editing session is required", nil)
	}
	if err := tx.First(&l, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, conflict("An active editing session is required", nil)
		}
		return nil, err
	}
	if l.OwnerID != actor.ID || l.SessionID != session || l.Epoch != epoch || l.ExpiresAt <= s.now() {
		return nil, conflict("Editing session expired; reacquire and compare versions", nil)
	}
	var d models.ArchiveDocument
	if err := tx.First(&d, id).Error; err != nil {
		return nil, err
	}
	if d.CurrentVersionID != l.BaseVersionID {
		return nil, conflict("Document version changed", d.CurrentVersionID)
	}
	return &l, nil
}
func (s *Service) Heartbeat(actor Actor, id int64, session string, epoch int64) (*models.ArchiveEditLease, error) {
	var out *models.ArchiveEditLease
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		out, err = s.checkLease(tx, actor, id, session, epoch)
		if err != nil {
			return err
		}
		next := s.Now().Add(LeaseDuration).UnixMilli()
		if next <= out.ExpiresAt {
			next = out.ExpiresAt + 1
		}
		out.ExpiresAt = next
		res := tx.Model(&models.ArchiveEditLease{}).Where("document_id = ? AND session_id = ? AND epoch = ? AND owner_id = ? AND expires_at > ?", id, session, epoch, actor.ID, s.now()).Update("expires_at", out.ExpiresAt)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return conflict("Editing session expired", nil)
		}
		return nil
	})
	return out, err
}
func (s *Service) Release(actor Actor, id int64, session string, epoch int64) error {
	res := s.DB.Model(&models.ArchiveEditLease{}).Where("document_id = ? AND session_id = ? AND epoch = ? AND owner_id = ?", id, session, epoch, actor.ID).Updates(map[string]interface{}{"expires_at": s.now(), "session_id": ""})
	return res.Error
}

func sameJSON(a, b interface{}) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(aa) == string(bb)
}
func complexBlocks(v interface{}) []interface{} {
	out := []interface{}{}
	var walk func(interface{})
	walk = func(x interface{}) {
		switch e := x.(type) {
		case map[string]interface{}:
			complex := false
			if e["type"] == "table" {
				var cells func(interface{})
				cells = func(n interface{}) {
					switch q := n.(type) {
					case map[string]interface{}:
						for _, k := range []string{"rowspan", "colspan"} {
							if count, ok := q[k].(float64); ok && count > 1 {
								complex = true
							}
						}
						for _, child := range q {
							cells(child)
						}
					case []interface{}:
						for _, c := range q {
							cells(c)
						}
					}
				}
				cells(e)
			}
			if e["type"] == "image" && (e["imgDisplay"] == "float-top" || e["imgDisplay"] == "float-bottom" || e["imgDisplay"] == "surround") {
				complex = true
			}
			if complex {
				out = append(out, e)
				return
			}
			keys := make([]string, 0, len(e))
			for k := range e {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(e[k])
			}
		case []interface{}:
			for _, child := range e {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}
func protectMarkdown(before, after models.ArchiveJSON) error {
	var a, b map[string]interface{}
	if json.Unmarshal([]byte(before), &a) != nil || json.Unmarshal([]byte(after), &b) != nil {
		return invalid("Invalid Markdown preservation snapshot")
	}
	ad, _ := a["data"].(map[string]interface{})
	bd, _ := b["data"].(map[string]interface{})
	if ad == nil || bd == nil {
		return invalid("Missing document data")
	}
	for _, k := range []string{"header", "footer"} {
		if !sameJSON(ad[k], bd[k]) {
			return invalid("Markdown cannot change page headers or footers")
		}
	}
	ao, _ := a["options"].(map[string]interface{})
	bo, _ := b["options"].(map[string]interface{})
	for _, k := range []string{"width", "height", "margins", "paperDirection", "header", "footer", "pageNumber"} {
		if av, ok := ao[k]; ok && !sameJSON(av, bo[k]) {
			return invalid("Markdown cannot change page settings")
		}
	}
	if !reflect.DeepEqual(complexBlocks(ad["main"]), complexBlocks(bd["main"])) {
		return invalid("Protected objects changed during Markdown conversion; original preserved")
	}
	return nil
}
func (s *Service) putDraft(tx *gorm.DB, actor Actor, d *models.ArchiveDocument, in EditInput, body models.ArchiveJSON) error {
	raw := encode(in.Markdown)
	draft := models.ArchiveDraft{DocumentID: d.ID, OwnerID: actor.ID, BaseVersionID: d.CurrentVersionID, Body: body, Mode: in.Mode, Markdown: raw, UpdateAt: s.now()}
	return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "document_id"}, {Name: "owner_id"}}, DoUpdates: clause.AssignmentColumns([]string{"base_version_id", "body", "mode", "markdown", "update_at"})}).Create(&draft).Error
}
func (s *Service) Draft(actor Actor, id int64, in EditInput) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		d, err := findDocument(tx, id)
		if err != nil {
			return err
		}
		var record models.ArchiveRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND deleted = ?", d.ArchiveID, false).First(&record).Error; err != nil {
			return missing()
		}
		d, err = findDocument(tx, id)
		if err != nil {
			return err
		}
		l, err := s.checkLease(tx, actor, id, in.SessionID, in.Epoch)
		if err != nil {
			return err
		}
		if in.BaseVersionID != l.BaseVersionID {
			return conflict("Draft is based on another document version", l.BaseVersionID)
		}
		if len(in.Markdown) > MaxBodyBytes {
			return invalid("Markdown draft exceeds 5 MiB")
		}
		body := in.Body
		if d.Kind == "online" {
			body, err = validateBody(tx, d.ArchiveID, body, actor.ID)
			if err != nil {
				return err
			}
			if in.Mode != "visual" && in.Mode != "markdown" {
				return invalid("Unknown document editing mode")
			}
			var old models.ArchiveDraft
			exists := tx.Where("document_id = ? AND owner_id = ?", id, actor.ID).First(&old).Error == nil
			if exists && old.Mode == "markdown" && !in.SwitchMode {
				if in.Mode != "markdown" || !sameJSON(json.RawMessage(old.Body), json.RawMessage(body)) {
					return invalid("Markdown preservation baseline cannot be replaced by autosave")
				}
			}
			if exists && old.Mode == "markdown" && in.SwitchMode && in.Mode == "visual" {
				if err := protectMarkdown(old.Body, body); err != nil {
					return err
				}
			}
		} else {
			if d.Kind != "pdf" {
				return invalid("This file has no editable content")
			}
			in.Mode = "annotations"
			var current models.ArchiveDocumentVersion
			if err := tx.First(&current, d.CurrentVersionID).Error; err != nil {
				return err
			}
			body, err = s.annotationsForVersion(tx, actor, current, body)
			if err != nil {
				return err
			}
		}
		return s.putDraft(tx, actor, d, in, body)
	})
}
func (s *Service) Save(actor Actor, id int64, in EditInput) (*Document, error) {
	var archiveID int64
	doc, err := s.Document(id, 0)
	if err != nil {
		return nil, err
	}
	archiveID = doc.ArchiveID
	err = s.mutate(actor, archiveID, 0, "save_document", func(tx *gorm.DB, a *models.ArchiveRecord) error {
		d, err := findDocument(tx, id)
		if err != nil {
			return err
		}
		l, err := s.checkLease(tx, actor, id, in.SessionID, in.Epoch)
		if err != nil {
			return err
		}
		if in.BaseVersionID != l.BaseVersionID {
			return conflict("Document changed before saving", l.BaseVersionID)
		}
		body := in.Body
		blobID := doc.Version.BlobID
		if d.Kind == "online" {
			body, err = validateBody(tx, archiveID, body, actor.ID)
			if err != nil {
				return err
			}
			var draft models.ArchiveDraft
			draftErr := tx.Where("document_id = ? AND owner_id = ?", id, actor.ID).First(&draft).Error
			if in.Mode == "markdown" && (draftErr != nil || draft.Mode != "markdown") {
				return invalid("Switch into Markdown mode before saving")
			}
			if draftErr == nil && draft.Mode == "markdown" {
				if err := protectMarkdown(draft.Body, body); err != nil {
					return err
				}
			}

		} else if d.Kind == "pdf" {
			body, err = s.annotationsForVersion(tx, actor, doc.Version, body)
			if err != nil {
				return err
			}
		} else {
			return invalid("Use replacement for uploaded Word or image files")
		}
		if len(in.Description) > 1000 {
			return invalid("Version description is too long")
		}
		description := strings.TrimSpace(in.Description)
		if description == "" {
			description = "Saved document"
		}
		if err := s.newDocumentVersion(tx, d, actor, blobID, body, description); err != nil {
			return err
		}
		if err := tx.Model(&models.ArchiveEditLease{}).Where("document_id = ? AND session_id = ? AND epoch = ?", id, in.SessionID, in.Epoch).Update("base_version_id", d.CurrentVersionID).Error; err != nil {
			return err
		}
		return s.putDraft(tx, actor, d, in, body)
	})
	if err != nil {
		return nil, err
	}
	return s.Document(id, 0)
}

type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type Annotation struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Page       int    `json:"page"`
	Rects      []Rect `json:"rects"`
	Text       string `json:"text"`
	Color      string `json:"color"`
	AuthorID   int64  `json:"author_id"`
	AuthorName string `json:"author_name"`
	CreateAt   int64  `json:"create_at"`
	UpdateBy   int64  `json:"update_by"`
	UpdateName string `json:"update_name"`
	UpdateAt   int64  `json:"update_at"`
}
type AnnotationBody struct {
	Annotations []Annotation `json:"annotations"`
}

func (s *Service) annotations(actor Actor, previous, body models.ArchiveJSON) (models.ArchiveJSON, error) {
	if len(body) > MaxBodyBytes {
		return "", invalid("PDF annotations exceed 5 MiB")
	}
	var input, old AnnotationBody
	if json.Unmarshal([]byte(body), &input) != nil {
		return "", invalid("Invalid PDF annotations")
	}
	_ = json.Unmarshal([]byte(previous), &old)
	byID := map[string]Annotation{}
	for _, a := range old.Annotations {
		byID[a.ID] = a
	}
	seen := map[string]bool{}
	if input.Annotations == nil {
		input.Annotations = []Annotation{}
	}
	if len(input.Annotations) > 10000 {
		return "", invalid("Too many PDF annotations")
	}
	for i := range input.Annotations {
		a := &input.Annotations[i]
		if a.ID == "" {
			a.ID = uuid.NewString()
		}
		if len(a.ID) > 64 || seen[a.ID] || a.Page < 1 || a.Page > 100000 {
			return "", invalid("Invalid annotation identity or page")
		}
		seen[a.ID] = true
		if a.Type != "highlight" && a.Type != "strikeout" && a.Type != "note" {
			return "", invalid("Unsupported PDF annotation type")
		}
		if len(a.Text) > 10000 || len(a.Rects) == 0 || len(a.Rects) > 100 {
			return "", invalid("Invalid PDF annotation content")
		}
		for _, r := range a.Rects {
			if r.X < 0 || r.Y < 0 || r.Width <= 0 || r.Height <= 0 || r.X+r.Width > 1.001 || r.Y+r.Height > 1.001 {
				return "", invalid("Annotation is outside the PDF page")
			}
		}
		if a.Color != "#ffe58f" && a.Color != "#ffccc7" && a.Color != "#b7eb8f" && a.Color != "#91d5ff" {
			a.Color = "#ffe58f"
		}
		if previous, ok := byID[a.ID]; ok {
			a.AuthorID = previous.AuthorID
			a.AuthorName = previous.AuthorName
			a.CreateAt = previous.CreateAt
			if a.Type == previous.Type && a.Page == previous.Page && sameJSON(a.Rects, previous.Rects) && a.Text == previous.Text && a.Color == previous.Color {
				a.UpdateBy = previous.UpdateBy
				a.UpdateName = previous.UpdateName
				a.UpdateAt = previous.UpdateAt
				continue
			}
		} else {
			a.AuthorID = actor.ID
			a.AuthorName = actor.Name
			a.CreateAt = s.now()
		}
		a.UpdateBy = actor.ID
		a.UpdateName = actor.Name
		a.UpdateAt = s.now()
	}
	normalized := encode(input)
	if len(normalized) > MaxBodyBytes {
		return "", invalid("PDF annotations exceed 5 MiB")
	}
	return normalized, nil
}

func (s *Service) Versions(archiveID, documentID int64) (interface{}, error) {
	if documentID > 0 {
		rows := []models.ArchiveDocumentVersion{}
		err := s.DB.Where("document_id = ?", documentID).Order("sequence desc").Find(&rows).Error
		return rows, err
	}
	rows := []models.ArchiveVersion{}
	err := s.DB.Where("archive_id = ?", archiveID).Order("sequence desc").Find(&rows).Error
	return rows, err
}
func (s *Service) Logs(id int64, offset int) ([]models.ArchiveLog, error) {
	if offset < 0 {
		offset = 0
	}
	rows := []models.ArchiveLog{}
	err := s.DB.Where("archive_id = ?", id).Order("id desc").Offset(offset).Limit(100).Find(&rows).Error
	return rows, err
}
func (s *Service) RestoreDocument(actor Actor, id, versionID int64) (*Document, error) {
	d, err := s.Document(id, 0)
	if err != nil {
		return nil, err
	}
	err = s.mutate(actor, d.ArchiveID, 0, "restore_document", func(tx *gorm.DB, a *models.ArchiveRecord) error {
		if err := s.unoccupied(tx, a.ID, id); err != nil {
			return err
		}
		var current models.ArchiveDocument
		if err := tx.First(&current, id).Error; err != nil {
			return err
		}
		var v models.ArchiveDocumentVersion
		if err := tx.Where("id = ? AND document_id = ?", versionID, id).First(&v).Error; err != nil {
			return missing()
		}
		if v.BlobID > 0 {
			var blob models.ArchiveBlob
			if err := tx.First(&blob, v.BlobID).Error; err != nil {
				return err
			}
			current.Kind = blob.Kind
		}
		return s.newDocumentVersion(tx, &current, actor, v.BlobID, v.Body, "Restored historical version")
	})
	if err != nil {
		return nil, err
	}
	return s.Document(id, 0)
}
func (s *Service) RestoreArchive(actor Actor, id, versionID, revision int64) (*Detail, error) {
	err := s.mutate(actor, id, revision, "restore_version", func(tx *gorm.DB, a *models.ArchiveRecord) error {
		if err := s.unoccupied(tx, id, 0); err != nil {
			return err
		}
		var v models.ArchiveVersion
		if err := tx.Where("id = ? AND archive_id = ?", versionID, id).First(&v).Error; err != nil {
			return missing()
		}
		var snap Snapshot
		if err := json.Unmarshal([]byte(v.Snapshot), &snap); err != nil {
			return err
		}
		var m Metadata
		if err := json.Unmarshal([]byte(snap.Record.Metadata), &m); err != nil {
			return err
		}
		if err := ValidateMetadata(a.Category, &m, snap.Record.State == "archived"); err != nil {
			return err
		}
		project(a, m)
		a.State = snap.Record.State
		a.Deleted = false
		a.PrimaryDocumentID = snap.Record.PrimaryDocumentID
		if err := tx.Model(&models.ArchiveDocument{}).Where("archive_id = ?", id).Update("deleted", true).Error; err != nil {
			return err
		}
		for _, item := range snap.Documents {
			var d models.ArchiveDocument
			if err := tx.Where("id = ? AND archive_id = ?", item.Document.ID, id).First(&d).Error; err != nil {
				return missing()
			}
			d.Title = item.Document.Title
			d.Purpose = item.Document.Purpose
			d.Kind = item.Document.Kind
			d.Deleted = false
			if err := tx.Save(&d).Error; err != nil {
				return err
			}
			var version models.ArchiveDocumentVersion
			if err := tx.Where("id = ? AND document_id = ?", item.VersionID, d.ID).First(&version).Error; err != nil {
				return missing()
			}
			if err := s.newDocumentVersion(tx, &d, actor, version.BlobID, version.Body, "Restored archive snapshot"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}
func (s *Service) Bookmark(actor Actor, id, versionID int64, position models.ArchiveJSON) (*models.ArchiveBookmark, error) {
	d, err := s.Document(id, versionID)
	if err != nil {
		return nil, err
	}
	if len(position) > 4096 {
		return nil, invalid("Bookmark is too large")
	}
	row := models.ArchiveBookmark{DocumentID: id, OwnerID: actor.ID, VersionID: d.Version.ID, Position: position, UpdateAt: s.now()}
	err = s.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "document_id"}, {Name: "owner_id"}}, DoUpdates: clause.AssignmentColumns([]string{"version_id", "position", "update_at"})}).Create(&row).Error
	return &row, err
}
func (s *Service) GetBookmark(actor Actor, id int64) (*models.ArchiveBookmark, error) {
	var row models.ArchiveBookmark
	err := s.DB.Where("document_id = ? AND owner_id = ?", id, actor.ID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

func (s *Service) DiscardDraft(actor Actor, id int64, in EditInput) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		d, err := findDocument(tx, id)
		if err != nil {
			return err
		}
		var a models.ArchiveRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&a, d.ArchiveID).Error; err != nil {
			return err
		}
		if _, err := s.checkLease(tx, actor, id, in.SessionID, in.Epoch); err != nil {
			return err
		}
		return tx.Where("document_id = ? AND owner_id = ?", id, actor.ID).Delete(&models.ArchiveDraft{}).Error
	})
}

// Resume requires the exact session secret and generation. It never grants an
// account-wide reentrant lock; a normal second window still uses Acquire.
func (s *Service) Resume(actor Actor, id int64, in EditInput) (*Editing, error) {
	var lease *models.ArchiveEditLease
	var draft *models.ArchiveDraft
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		d, err := findDocument(tx, id)
		if err != nil {
			return err
		}
		var a models.ArchiveRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&a, d.ArchiveID).Error; err != nil {
			return err
		}
		lease, err = s.checkLease(tx, actor, id, in.SessionID, in.Epoch)
		if err != nil {
			return err
		}
		lease.ExpiresAt = s.Now().Add(LeaseDuration).UnixMilli()
		lease.SessionID = uuid.NewString()
		lease.Epoch++
		res := tx.Model(&models.ArchiveEditLease{}).Where("document_id = ? AND session_id = ? AND epoch = ? AND owner_id = ?", id, in.SessionID, in.Epoch, actor.ID).Updates(map[string]interface{}{"expires_at": lease.ExpiresAt, "session_id": lease.SessionID, "epoch": lease.Epoch})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return conflict("Editing session expired", nil)
		}
		var stored models.ArchiveDraft
		if err := tx.Where("document_id = ? AND owner_id = ?", id, actor.ID).First(&stored).Error; err == nil {
			draft = &stored
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	document, err := s.Document(id, 0)
	if err != nil {
		return nil, err
	}
	return &Editing{Lease: *lease, Draft: draft, Document: *document}, nil
}
