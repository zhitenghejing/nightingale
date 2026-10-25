package cmdb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	pool, e := db.DB()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = pool.Close() })
	s := &Store{DB: db}
	if e = s.Migrate(); e != nil {
		t.Fatal(e)
	}
	for _, v := range []interface{}{&Room{ID: "room", Name: "机房"}, &Rack{ID: "rack", RoomID: "room", Name: "A01", Units: 48, Version: 1}, &Rack{ID: "rack2", RoomID: "room", Name: "A02", Units: 42, Version: 1}} {
		if e = db.Create(v).Error; e != nil {
			t.Fatal(e)
		}
	}
	return s
}
func asset(serial string, start, end int) Document {
	return Document{"customer": "测试医院", "serial": serial, "hostname": serial, "rackId": "rack", "uStart": float64(start), "uEnd": float64(end), "disks": []interface{}{map[string]interface{}{"model": "SSD", "description": "960G", "count": float64(2)}}}
}
func save(t *testing.T, s *Store, d Document, create bool) Document {
	t.Helper()
	var out Document
	e := s.Write(func(tx *gorm.DB) error { var e error; out, e = s.Save(tx, d, 1, create); return e })
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestPersistenceMoveAndOptimisticLock(t *testing.T) {
	s := fixture(t)
	d := save(t, s, asset("S001", 45, 46), true)
	if str(d, "room") != "机房" {
		t.Fatal(d)
	}
	id := str(d, "id")
	rows, e := s.Assets(false)
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	d = rows[0]
	d["rackId"] = "rack2"
	d["uStart"] = float64(9)
	d["uEnd"] = float64(10)
	stale := Document{}
	_ = json.Unmarshal([]byte(encode(d)), &stale)
	out := save(t, s, d, false)
	if str(out, "rack") != "A02" || out["version"] != 2 {
		t.Fatal(out)
	}
	e = s.Write(func(tx *gorm.DB) error { _, e := s.Save(tx, stale, 1, false); return e })
	if e == nil {
		t.Fatal("stale write succeeded")
	}
	var disks []Disk
	s.DB.Where("asset_id = ?", id).Find(&disks)
	if len(disks) != 1 || disks[0].Count != 2 {
		t.Fatal(disks)
	}
	var n int64
	s.DB.Model(&Change{}).Count(&n)
	if n != 2 {
		t.Fatal(n)
	}
}
func TestOverlapBoundsAndImportRollback(t *testing.T) {
	s := fixture(t)
	save(t, s, asset("S1", 9, 10), true)
	for _, d := range []Document{asset("S2", 10, 11), asset("S1", 15, 16), asset("S2", 48, 49), asset("S2", 0, 1)} {
		if e := s.Write(func(tx *gorm.DB) error { _, e := s.Save(tx, d, 1, true); return e }); e == nil {
			t.Fatal("invalid placement accepted", d)
		}
	}
	e := s.Write(func(tx *gorm.DB) error {
		if _, e := s.Save(tx, asset("NEW", 15, 16), 1, true); e != nil {
			return e
		}
		_, e := s.Save(tx, asset("BAD", 9, 10), 1, true)
		return e
	})
	if e == nil {
		t.Fatal("batch should fail")
	}
	rows, _ := s.Assets(false)
	if len(rows) != 1 {
		t.Fatal("batch not atomic")
	}
}
func TestHTTPAuthPermissionsDeleteRestore(t *testing.T) {
	s := fixture(t)
	admin := false
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer valid" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"dat": User{ID: 1, Admin: admin}, "err": ""})
	}))
	defer identity.Close()
	h := NewHandler(s, identity.URL)
	call := func(method, path, token string, d interface{}) *httptest.ResponseRecorder {
		b, _ := json.Marshal(d)
		r := httptest.NewRequest(method, "/api/cmdb/"+path, strings.NewReader(string(b)))
		if token != "" {
			r.Header.Set("Authorization", token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "devices", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call("GET", "devices", "Bearer invalid", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := call("GET", "devices", "Bearer valid", nil); w.Code != 403 {
		t.Fatal(w.Code)
	}
	admin = true
	w := call("POST", "devices", "Bearer valid", asset("S001", 1, 2))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var envelope struct{ Dat Document }
	json.Unmarshal(w.Body.Bytes(), &envelope)
	d := envelope.Dat
	if w = call("POST", "delete", "Bearer valid", []Document{d}); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	rows, _ := s.Assets(false)
	if len(rows) != 0 {
		t.Fatal(rows)
	}
	deleted, _ := s.Assets(true)
	if len(deleted) != 1 {
		t.Fatal(deleted)
	}
	// Another asset can use the released U positions, but restoration must fail atomically.
	save(t, s, asset("S2", 1, 2), true)
	w = call("POST", "restore", "Bearer valid", deleted)
	if w.Code != 400 {
		t.Fatal(w.Body.String())
	}
	again, _ := s.Assets(true)
	if len(again) != 1 {
		t.Fatal("failed restore changed deletion state")
	}
	admin = false
	s.DB.Create(&Access{UserID: 1, Read: true})
	if w = call("GET", "devices", "Bearer valid", nil); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = call("POST", "devices", "Bearer valid", asset("S3", 15, 16)); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w = call("GET", "export", "Bearer valid", nil); w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestInitialMigrationIsRepeatable(t *testing.T) {
	s := fixture(t)
	save(t, s, asset("repeat", 1, 2), true)
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	rows, err := s.Assets(false)
	if err != nil || len(rows) != 1 {
		t.Fatal("repeat migration altered records", err)
	}
}

func TestMaterialReferencePersistsInExistingPayload(t *testing.T) {
	s := fixture(t)
	doc := asset("material-ref", 9, 10)
	doc["assetMaterialId"] = "dell-r750"
	save(t, s, doc, true)
	rows, err := s.Assets(false)
	if err != nil || len(rows) != 1 || str(rows[0], "assetMaterialId") != "dell-r750" {
		t.Fatal("material reference was not persisted", err)
	}
	rows[0]["uStart"] = float64(15)
	rows[0]["uEnd"] = float64(16)
	save(t, s, rows[0], false)
	rows, err = s.Assets(false)
	if err != nil || str(rows[0], "assetMaterialId") != "dell-r750" || num(rows[0], "uStart") != 15 {
		t.Fatal("moving the asset lost material reference", err)
	}
}
