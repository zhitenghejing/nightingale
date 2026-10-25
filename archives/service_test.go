package archives

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var alice = Actor{ID: 1, Username: "alice", Name: "Alice"}
var bob = Actor{ID: 2, Username: "bob", Name: "Bob"}

func newTestService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err = db.AutoMigrate(models.ArchiveTables()...); err != nil {
		t.Fatal(err)
	}
	s := New(db, t.TempDir())
	s.Now = func() time.Time { return time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC) }
	return s
}
func body(text string) models.ArchiveJSON {
	return encode(map[string]interface{}{"version": "1.0.5", "schema_version": 1, "data": map[string]interface{}{"header": []interface{}{}, "main": []interface{}{map[string]interface{}{"value": text}}, "footer": []interface{}{}}, "options": map[string]interface{}{"width": 794, "height": 1123, "margins": []int{80, 70, 80, 70}}})
}
func other(t *testing.T, s *Service) (*Detail, int64) {
	t.Helper()
	a, err := s.Create(alice, "other", Metadata{Title: "Archive"})
	if err != nil {
		t.Fatal(err)
	}
	a, err = s.CreateOnline(alice, a.ID, CreateDocument{Title: "Document", Purpose: "other", Body: body("Original")})
	if err != nil {
		t.Fatal(err)
	}
	return a, a.Documents[0].ID
}
func problemStatus(t *testing.T, err error, status int) {
	t.Helper()
	var p *Problem
	if !errors.As(err, &p) || p.Status != status {
		t.Fatalf("want HTTP %d problem, got %v", status, err)
	}
}
func pngBytes(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func edit(l models.ArchiveEditLease, b models.ArchiveJSON) EditInput {
	return EditInput{SessionID: l.SessionID, Epoch: l.Epoch, BaseVersionID: l.BaseVersionID, Mode: "visual", Body: b}
}

func TestArchiveTrainingMaterialsAndArchivedEditing(t *testing.T) {
	s := newTestService(t)
	a, err := s.Create(alice, "training", Metadata{Title: "Training", Lecturer: "Handwritten name"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Archive(alice, a.ID, a.Revision)
	problemStatus(t, err, 422)
	a, err = s.CreateOnline(alice, a.ID, CreateDocument{Title: "Teaching", Purpose: "training_material", Body: body("Teaching material")})
	if err != nil {
		t.Fatal(err)
	}
	id := a.Documents[0].ID
	_, err = s.Archive(alice, a.ID, a.Revision)
	problemStatus(t, err, 422)
	a, err = s.Upload(alice, a.ID, 0, "attendance.png", "attendance", "", 0, bytes.NewReader(pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	a, err = s.Archive(alice, a.ID, a.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != "archived" {
		t.Fatal(a.State)
	}
	session, err := s.Acquire(bob, id)
	if err != nil {
		t.Fatal(err)
	}
	in := edit(session.Lease, body("Updated teaching"))
	if err = s.Draft(bob, id, in); err != nil {
		t.Fatal(err)
	}
	current, _ := s.Document(id, 0)
	if strings.Contains(string(current.Version.Body), "Updated") {
		t.Fatal("autosave exposed a draft")
	}
	saved, err := s.Save(bob, id, in)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version.Sequence != 2 {
		t.Fatal(saved.Version.Sequence)
	}
	a, _ = s.Get(a.ID)
	if a.State != "archived" {
		t.Fatal("editing cleared archived state")
	}
	logs, err := s.Logs(a.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, log := range logs {
		if log.Action == "document_version" && log.ActorID == bob.ID {
			found = true
			if log.ActorName == "Handwritten name" {
				t.Fatal("business person used as audit identity")
			}
		}
	}
	if !found {
		t.Fatal("missing real actor log")
	}
}

func TestArchiveLeaseFencesOldWindowsAndAutosave(t *testing.T) {
	s := newTestService(t)
	a, id := other(t, s)
	first, err := s.Acquire(alice, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Acquire(alice, id)
	problemStatus(t, err, 409)
	_, err = s.Acquire(bob, id)
	problemStatus(t, err, 409)
	draft := edit(first.Lease, body("Private draft"))
	if err = s.Draft(alice, id, draft); err != nil {
		t.Fatal(err)
	}
	current, _ := s.Document(id, 0)
	if strings.Contains(string(current.Version.Body), "Private") {
		t.Fatal("draft became public")
	}
	var count int64
	s.DB.Model(&models.ArchiveDocumentVersion{}).Where("document_id = ?", id).Count(&count)
	if count != 1 {
		t.Fatal("autosave created formal versions")
	}
	err = s.Delete(bob, a.ID, 0)
	problemStatus(t, err, 409)
	oldNow := s.Now()
	s.Now = func() time.Time { return oldNow.Add(6 * time.Minute) }
	secondService := New(s.DB, s.Root)
	secondService.Now = s.Now
	second, err := secondService.Acquire(bob, id)
	if err != nil {
		t.Fatal(err)
	}
	if second.Lease.Epoch <= first.Lease.Epoch {
		t.Fatal("lease fencing epoch did not advance")
	}
	if second.Draft != nil {
		t.Fatal("another actor's recovery draft leaked")
	}
	_, err = s.Save(alice, id, draft)
	problemStatus(t, err, 409)
	if err = s.Release(alice, id, first.Lease.SessionID, first.Lease.Epoch); err != nil {
		t.Fatal(err)
	}
	if _, err = secondService.Heartbeat(bob, id, second.Lease.SessionID, second.Lease.Epoch); err != nil {
		t.Fatal("old window released new lease:", err)
	}
}

func TestArchiveRevisionConflictAndVersionRestore(t *testing.T) {
	s := newTestService(t)
	a, id := other(t, s)
	oldRevision := a.Revision
	a, err := s.Update(alice, a.ID, a.Revision, Metadata{Title: "Renamed"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Update(bob, a.ID, oldRevision, Metadata{Title: "Stale"}, 0)
	problemStatus(t, err, 409)
	session, err := s.Acquire(alice, id)
	if err != nil {
		t.Fatal(err)
	}
	original := session.Document.Version.ID
	saved, err := s.Save(alice, id, edit(session.Lease, body("New body")))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version.Sequence != 2 {
		t.Fatal(saved.Version.Sequence)
	}
	if err = s.Release(alice, id, session.Lease.SessionID, session.Lease.Epoch); err != nil {
		t.Fatal(err)
	}
	restored, err := s.RestoreDocument(bob, id, original)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Version.Sequence != 3 || !strings.Contains(string(restored.Version.Body), "Original") {
		t.Fatal("restoration failed to create new immutable history")
	}
	var count int64
	s.DB.Model(&models.ArchiveDocumentVersion{}).Where("document_id = ?", id).Count(&count)
	if count != 3 {
		t.Fatal(count)
	}
	a, _ = s.Get(a.ID)
	if a.Title != "Renamed" {
		t.Fatal("document restore reverted unrelated metadata")
	}
}

func TestArchiveMarkdownCannotOverwriteProtectedObjects(t *testing.T) {
	s := newTestService(t)
	a, err := s.Create(alice, "other", Metadata{Title: "Protected"})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]interface{}
	json.Unmarshal([]byte(body("Original")), &value)
	data := value["data"].(map[string]interface{})
	data["header"] = []interface{}{map[string]interface{}{"value": "Header"}}
	data["main"] = append(data["main"].([]interface{}), map[string]interface{}{"type": "table", "value": "", "trList": []interface{}{map[string]interface{}{"tdList": []interface{}{map[string]interface{}{"colspan": 2, "rowspan": 1, "value": []interface{}{map[string]interface{}{"value": "Protected"}}}}}}})
	canonical := encode(value)
	a, err = s.CreateOnline(alice, a.ID, CreateDocument{Title: "Protected", Purpose: "other", Body: canonical})
	if err != nil {
		t.Fatal(err)
	}
	id := a.Documents[0].ID
	session, err := s.Acquire(alice, id)
	if err != nil {
		t.Fatal(err)
	}
	in := edit(session.Lease, canonical)
	in.Mode = "markdown"
	in.SwitchMode = true
	in.Markdown = "Original\n:::archive-block complex\n"
	if err = s.Draft(alice, id, in); err != nil {
		t.Fatal(err)
	}
	bad := in
	bad.Body = models.ArchiveJSON(strings.Replace(string(canonical), "\"colspan\":2", "\"colspan\":1", 1))
	_, err = s.Save(alice, id, bad)
	problemStatus(t, err, 422)
	good := in
	good.Body = models.ArchiveJSON(strings.Replace(string(canonical), "Original", "Edited", 1))
	saved, err := s.Save(alice, id, good)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved.Version.Body), "Edited") || !strings.Contains(string(saved.Version.Body), "\"colspan\":2") {
		t.Fatal("Markdown preservation failed")
	}
}

func TestArchiveRejectsUnreadableUploadsAndRetainsHistoryOnDelete(t *testing.T) {
	s := newTestService(t)
	a, id := other(t, s)
	_, err := s.Upload(alice, a.ID, 0, "broken.pdf", "other", "", 0, strings.NewReader("%PDF-1.7 broken"))
	problemStatus(t, err, 422)
	_, err = s.Upload(alice, a.ID, 0, "wrong.jpg", "other", "", 0, bytes.NewReader(pngBytes(t)))
	problemStatus(t, err, 422)
	var blobs int64
	s.DB.Model(&models.ArchiveBlob{}).Count(&blobs)
	if blobs != 0 {
		t.Fatal("failed upload left database files")
	}
	entries, err := os.ReadDir(s.Root + "/objects")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("failed upload left objects")
	}
	if err = s.Delete(alice, a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	deleted, _ := s.Get(a.ID)
	if !deleted.Deleted {
		t.Fatal("not deleted")
	}
	restored, err := s.Restore(bob, a.ID, deleted.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Deleted || len(restored.Documents) != 1 || restored.Documents[0].ID != id {
		t.Fatal("delete did not retain materials")
	}
	var versions int64
	s.DB.Model(&models.ArchiveDocumentVersion{}).Where("document_id = ?", id).Count(&versions)
	if versions != 1 {
		t.Fatal("deletion destroyed document history")
	}
}

func TestArchiveDocumentLimitIsAtomic(t *testing.T) {
	s := newTestService(t)
	a, err := s.Create(alice, "other", Metadata{Title: "Quota"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxDocuments; i++ {
		if _, err = s.CreateOnline(alice, a.ID, CreateDocument{Title: "Document", Purpose: "other", Body: body("Content")}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = s.CreateOnline(alice, a.ID, CreateDocument{Title: "Overflow", Purpose: "other", Body: body("Content")})
	problemStatus(t, err, 422)
	current, _ := s.Get(a.ID)
	if len(current.Documents) != MaxDocuments {
		t.Fatal("failed creation changed document count")
	}
	var n int64
	s.DB.Model(&models.ArchiveDocument{}).Where("archive_id = ?", a.ID).Count(&n)
	if n != MaxDocuments {
		t.Fatal("quota failure left partial row")
	}
}

func TestArchiveSnapshotRestoresReferencesWithoutCopyingBodies(t *testing.T) {
	s := newTestService(t)
	a, id := other(t, s)
	var before models.ArchiveVersion
	if err := s.DB.Where("archive_id = ? AND sequence = ?", a.ID, a.Revision).First(&before).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before.Snapshot), "Original") {
		t.Fatal("archive snapshot duplicated document body")
	}
	if err := s.DeleteDocument(alice, a.ID, id); err != nil {
		t.Fatal(err)
	}
	a, err := s.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Documents) != 0 {
		t.Fatal("not removed")
	}
	a, err = s.RestoreArchive(bob, a.ID, before.ID, a.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Documents) != 1 || a.Documents[0].ID != id || a.Documents[0].Version.Sequence != 2 {
		t.Fatal("snapshot did not create restored document version")
	}
	doc, err := s.Document(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc.Version.Body), "Original") {
		t.Fatal("snapshot reference lost document content")
	}
}

func TestArchiveStagedImagesPrivateAndLeaseBound(t *testing.T) {
	s := newTestService(t)
	a, id := other(t, s)
	_, err := s.UploadAsset(alice, a.ID, id, "image.png", bytes.NewReader(pngBytes(t)))
	problemStatus(t, err, 409)
	lease, err := s.Acquire(alice, id)
	if err != nil {
		t.Fatal(err)
	}
	asset, err := s.UploadAsset(alice, a.ID, id, "image.png", bytes.NewReader(pngBytes(t)), EditInput{SessionID: lease.Lease.SessionID, Epoch: lease.Lease.Epoch})
	if err != nil {
		t.Fatal(err)
	}
	b := encode(map[string]interface{}{"data": map[string]interface{}{"main": []interface{}{map[string]interface{}{"type": "image", "value": fmt.Sprintf("archive-asset:%d", asset.ID), "width": 40, "height": 40}}}})
	_, err = s.CreateOnline(bob, a.ID, CreateDocument{Title: "Guess private image", Purpose: "other", Body: b})
	problemStatus(t, err, 422)
	_, err = s.Save(alice, id, edit(lease.Lease, b))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateOnline(bob, a.ID, CreateDocument{Title: "Published image", Purpose: "other", Body: b}); err != nil {
		t.Fatal(err)
	}
}

func TestArchiveTimeFiltersNormalizeTimezones(t *testing.T) {
	s := newTestService(t)
	_, err := s.Create(alice, "training", Metadata{Title: "Timestamp", StartTime: "2026-10-23T09:00:00+08:00"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.List(ListQuery{Category: "training", Start: "2026-10-23T00:59:59Z", End: "2026-10-23T01:00:00.001Z"})
	if err != nil || rows.Total != 1 {
		t.Fatalf("timezone query: %v, %#v", err, rows)
	}
	rows, err = s.List(ListQuery{Category: "training", Start: "2026-10-23T01:00:00.001Z"})
	if err != nil || rows.Total != 0 {
		t.Fatalf("millisecond boundary: %v", err)
	}
	_, err = s.List(ListQuery{Start: "not a timestamp"})
	problemStatus(t, err, 422)
}

func TestArchiveRefreshResumeRotatesLeaseAndKeepsPrivateDraft(t *testing.T) {
	s := newTestService(t)
	_, id := other(t, s)
	first, err := s.Acquire(alice, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Draft(alice, id, edit(first.Lease, body("Private after refresh"))); err != nil {
		t.Fatal(err)
	}
	_, err = s.Acquire(alice, id)
	problemStatus(t, err, 409)
	_, err = s.Resume(bob, id, edit(first.Lease, body("")))
	problemStatus(t, err, 409)
	resumed, err := s.Resume(alice, id, edit(first.Lease, body("")))
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Lease.Epoch != first.Lease.Epoch+1 || resumed.Lease.SessionID == first.Lease.SessionID {
		t.Fatal("resume did not fence the pre-refresh window")
	}
	if resumed.Draft == nil || !strings.Contains(string(resumed.Draft.Body), "Private after refresh") {
		t.Fatal("private draft lost")
	}
	_, err = s.Save(alice, id, edit(first.Lease, body("Stale write")))
	problemStatus(t, err, 409)
	s.Release(alice, id, first.Lease.SessionID, first.Lease.Epoch)
	if _, err = s.Heartbeat(alice, id, resumed.Lease.SessionID, resumed.Lease.Epoch); err != nil {
		t.Fatal(err)
	}
}

type archiveZeroReader struct{}

func (archiveZeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }
func TestArchiveUploadAndAggregateLimitsLeaveExistingData(t *testing.T) {
	s := newTestService(t)
	a, _ := other(t, s)
	_, err := s.Upload(alice, a.ID, 0, "too-large.pdf", "other", "", 0, io.LimitReader(archiveZeroReader{}, MaxFileBytes+1))
	problemStatus(t, err, 422)
	_, err = s.UploadAsset(alice, a.ID, 0, "too-large.png", io.LimitReader(archiveZeroReader{}, MaxImageBytes+1))
	problemStatus(t, err, 422)
	a, err = s.Upload(alice, a.ID, 0, "current.png", "other", "", 0, bytes.NewReader(pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	uploaded := a.Documents[len(a.Documents)-1]
	remaining := MaxArchiveBytes - a.Documents[0].Version.Size
	// Seed an existing full quota without writing 500 MB in every unit-test run.
	if err := s.DB.Model(&models.ArchiveBlob{}).Where("id = ?", uploaded.File.ID).Update("size", remaining).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&models.ArchiveDocumentVersion{}).Where("id = ?", uploaded.Version.ID).Update("size", remaining).Error; err != nil {
		t.Fatal(err)
	}
	before, err := s.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.CurrentBytes != MaxArchiveBytes {
		t.Fatal(before.CurrentBytes)
	}
	_, err = s.Upload(alice, a.ID, 0, "extra.png", "other", "", 0, bytes.NewReader(pngBytes(t)))
	problemStatus(t, err, 422)
	after, err := s.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Documents) != 2 || after.Revision != before.Revision {
		t.Fatal("rejected upload changed archive")
	}
	objects, _ := os.ReadDir(filepath.Join(s.Root, "objects"))
	temporary, _ := os.ReadDir(filepath.Join(s.Root, "temporary"))
	if len(objects) != 1 || len(temporary) != 0 {
		t.Fatal("failed uploads leaked files")
	}
}

func TestArchiveRejectsTruncatedImageAfterValidHeader(t *testing.T) {
	s := newTestService(t)
	a, _ := other(t, s)
	data := pngBytes(t)
	_, err := s.Upload(alice, a.ID, 0, "truncated.png", "other", "", 0, bytes.NewReader(data[:len(data)-16]))
	problemStatus(t, err, 422)
	after, err := s.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Documents) != 1 || after.Revision != a.Revision {
		t.Fatal("invalid image became a formal document")
	}
}

func TestArchiveRejectsMalformedEditorContainers(t *testing.T) {
	s := newTestService(t)
	a, id := other(t, s)
	lease, err := s.Acquire(alice, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, nodes := range []string{`[null]`, `["text"]`, `[{"value":42}]`, `[{"type":"unknown","value":"x"}]`, `[{"type":"title","valueList":null}]`, `[{"type":"table","trList":[{"tdList":[{"value":[null]}]}]}]`} {
		malformed := models.ArchiveJSON(`{"data":{"main":` + nodes + `}}`)
		_, err := s.CreateOnline(alice, a.ID, CreateDocument{Title: "Malformed", Purpose: "other", Body: malformed})
		problemStatus(t, err, 422)
		err = s.Draft(alice, id, edit(lease.Lease, malformed))
		problemStatus(t, err, 422)
	}
	current, err := s.Document(id, 0)
	if err != nil || current.Version.Sequence != 1 {
		t.Fatal("rejected nodes changed the formal version", err)
	}
}

func TestArchiveHistoricalBookmarkPinsItsVersion(t *testing.T) {
	s := newTestService(t)
	a, id := other(t, s)
	original := a.Documents[0].Version.ID
	lease, _ := s.Acquire(alice, id)
	if _, err := s.Save(alice, id, edit(lease.Lease, body("Second version"))); err != nil {
		t.Fatal(err)
	}
	bookmark, err := s.Bookmark(alice, id, original, encode(map[string]int{"page": 2}))
	if err != nil || bookmark.VersionID != original {
		t.Fatal("history bookmark was attached to the current version", err)
	}
	_, otherID := other(t, s)
	otherDoc, _ := s.Document(otherID, 0)
	_, err = s.Bookmark(alice, id, otherDoc.Version.ID, encode(map[string]int{"page": 1}))
	problemStatus(t, err, 404)
	if b, err := s.GetBookmark(bob, id); err != nil || b != nil {
		t.Fatal("bookmark leaked to another account", err)
	}
}

func TestArchiveIncidentTitleFitsDatabaseColumn(t *testing.T) {
	s := newTestService(t)
	m := Metadata{AffectedBusiness: strings.Repeat("业", 255), Level: "III", OccurredAt: "2026-10-23T10:00:00+08:00"}
	a, err := s.Create(alice, "incident", m)
	if err != nil || a.Title != m.AffectedBusiness {
		t.Fatal("derived title exceeds the validated business name", err)
	}
}
