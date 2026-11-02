package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/aop"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/ccfos/nightingale/v6/pkg/externalpush"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestExternalTaskAdminQueryAndExplicitActions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "routes.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.ExternalPushTask{}, &models.ExternalPushAttempt{}, &models.ExternalPushOperation{}); err != nil {
		t.Fatal(err)
	}
	admin := models.User{Username: "external-test-admin", Roles: models.AdminRole}
	ordinary := models.User{Username: "external-test-viewer", Roles: "Standard"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ordinary).Error; err != nil {
		t.Fatal(err)
	}
	task := models.ExternalPushTask{DedupeKey: "test", StreamKey: "test", State: externalpush.Unknown, Reason: "ambiguous", Version: 2, Attempts: 1, Payload: `{"alarmType":"problem","companyId":"TEST_CUSTOMER","deviceName":"TEST_NODE_192.0.2.8:1443","sign":"NEVER_QUERY_THIS"}`, Snapshot: "NEVER_QUERY_THIS", CreatedAt: time.Now().UnixMilli()}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	rt := &Router{Ctx: nctx.NewContext(context.Background(), db, true)}
	engine := gin.New()
	engine.Use(aop.Recovery())
	// Exercise the exact admin middleware used by the mounted routes, without
	// introducing a test login token or touching any running instance.
	engine.Use(func(c *gin.Context) {
		id := ordinary.Id
		if c.GetHeader("X-Test-Admin") == "1" {
			id = admin.Id
		}
		c.Set("userid", id)
	})
	engine.GET("/signing-key", rt.admin(), rt.externalPushSigningKey)
	engine.GET("/tasks", rt.admin(), rt.externalPushTasks)
	engine.GET("/tasks/:id", rt.admin(), rt.externalPushDetail)
	engine.POST("/tasks/:id/actions", rt.admin(), rt.externalPushAction)
	call := func(method, path, body string, isAdmin bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		if isAdmin {
			r.Header.Set("X-Test-Admin", "1")
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w
	}
	if w := call("GET", "/tasks", "", false); w.Code != 403 {
		t.Fatalf("non-admin list allowed: %d", w.Code)
	}
	for _, path := range []string{"/tasks", "/tasks/1"} {
		w := call("GET", path, "", true)
		if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("NEVER_QUERY_THIS")) {
			t.Fatalf("query exposes payload or fails: %d %s", w.Code, w.Body.String())
		}
	}
	var response struct {
		Dat struct {
			PayloadPreview map[string]string `json:"payload_preview"`
		} `json:"dat"`
	}
	wPreview := call("GET", "/tasks/1", "", true)
	if err := json.Unmarshal(wPreview.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Dat.PayloadPreview["companyId"] != "TEST_CUSTOMER" || response.Dat.PayloadPreview["deviceName"] != "TEST_NODE_192.0.2.8:1443" || response.Dat.PayloadPreview["sign"] != "[REDACTED]" {
		t.Fatal("admin task detail must expose the saved JSON with sign redacted")
	}
	keyPath := filepath.Join(t.TempDir(), "test-only.key")
	if err := os.WriteFile(keyPath, []byte("NEVER_EXPOSE_FILE_KEY"), 0600); err != nil {
		t.Fatal(err)
	}
	rt.Center.ExternalPushKeyFiles = map[string]string{"TEST-v1": keyPath}
	if w := call("GET", "/signing-key?reference=TEST-v1", "", false); w.Code != 403 {
		t.Fatal("non-admin key status allowed")
	}
	wKey := call("GET", "/signing-key?reference=TEST-v1", "", true)
	if wKey.Code != 200 || !bytes.Contains(wKey.Body.Bytes(), []byte(`"ready":true`)) || bytes.Contains(wKey.Body.Bytes(), []byte(keyPath)) || bytes.Contains(wKey.Body.Bytes(), []byte("NEVER_EXPOSE_FILE_KEY")) {
		t.Fatal("key status fails or exposes sensitive data")
	}
	var untouched models.ExternalPushTask
	db.First(&untouched, task.ID)
	if untouched.State != externalpush.Unknown || untouched.Version != 2 {
		t.Fatal("query changed delivery state")
	}
	if w := call("POST", "/tasks/1/actions", `{"action":"confirm_received","version":2,"reason":""}`, true); w.Code != 409 {
		t.Fatal("empty audit reason allowed")
	}
	if w := call("POST", "/tasks/1/actions", `{"action":"confirm_received","version":2,"reason":"TEST: matched mock record"}`, false); w.Code != 403 {
		t.Fatal("non-admin action allowed")
	}
	w := call("POST", "/tasks/1/actions", `{"action":"confirm_received","version":2,"reason":"TEST: matched mock record"}`, true)
	if w.Code != 200 {
		t.Fatalf("action failed: %s", w.Body.String())
	}
	if w := call("POST", "/tasks/1/actions", `{"action":"confirm_received","version":2,"reason":"double click"}`, true); w.Code != 409 {
		t.Fatal("stale action allowed")
	}
	d, err := externalpush.Get(db, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(d)
	if d.Task.State != externalpush.Confirmed || len(d.Operations) != 1 || d.Operations[0].Actor != admin.Username || bytes.Contains(raw, []byte("NEVER_QUERY_THIS")) {
		t.Fatalf("audit or redaction failed: %s", raw)
	}
}

func TestExternalMappingPreviewIsAdminReadOnly(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "preview.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.NotifyChannelConfig{}, &models.AlertHisEvent{}, &models.ExternalPushTask{}, &models.ExternalPushAttempt{}, &models.ExternalPushOperation{}); err != nil {
		t.Fatal(err)
	}
	admin := models.User{Username: "preview-admin", Roles: models.AdminRole}
	viewer := models.User{Username: "preview-viewer", Roles: "Standard"}
	db.Create(&admin)
	db.Create(&viewer)
	cfg := models.ExternalPushConfig{Mode: "simulation", Endpoint: "http://127.0.0.1:19091/mock", Destination: "TEST_preview", ProblemType: "problem", RecoveryType: "ok", TimeZone: "Asia/Shanghai", TimeoutMS: 1000, TestSign: "NEVER_EXPOSE_SIGN"}
	channel := models.NotifyChannelConfig{Name: "TEST preview", Enable: false, RequestType: models.ExternalPushType, RequestConfig: &models.RequestConfig{ExternalPushConfig: &cfg}}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	event := models.AlertHisEvent{DispatchID: uuid.NewString(), IncidentID: uuid.NewString(), IncidentSequence: 1, Severity: 1, FirstTriggerTime: 1786400002, RuleName: "TEST rule", Tags: "kind=服务器,,ident=TEST_NODE,,model=R650"}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	rt := &Router{Ctx: nctx.NewContext(context.Background(), db, true)}
	engine := gin.New()
	engine.Use(aop.Recovery())
	engine.Use(func(c *gin.Context) {
		id := viewer.Id
		if c.GetHeader("X-Test-Admin") == "1" {
			id = admin.Id
		}
		c.Set("userid", id)
	})
	engine.POST("/preview", rt.admin(), rt.externalPushPreview)
	raw, _ := json.Marshal(externalpush.PreviewInput{ChannelID: channel.ID, EventID: event.Id, Params: map[string]interface{}{"mapping_version": "event_labels_v1", "company_id": "TEST_COMPANY", "device_source": "event", "device_type_label": "kind", "device_name_label": "ident", "device_tags_source": "event_labels", "device_tags_labels": `[{"label":"model","required":true},{"label":"business","required":false}]`, "level_map": `{"1":"灾难","2":"严重","3":"警告"}`, "repeat_policy": "once_per_incident"}})
	for _, isAdmin := range []bool{false, true} {
		r := httptest.NewRequest("POST", "/preview", bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		if isAdmin {
			r.Header.Set("X-Test-Admin", "1")
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		if !isAdmin && w.Code != 403 {
			t.Fatal("preview accessible without admin")
		}
		if isAdmin && (w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"deviceTags":"R650"`)) || bytes.Contains(w.Body.Bytes(), []byte("NEVER_EXPOSE_SIGN"))) {
			t.Fatalf("preview: %d %s", w.Code, w.Body.String())
		}
	}
	var count int64
	db.Model(&models.ExternalPushTask{}).Count(&count)
	if count != 0 {
		t.Fatal("preview created a task")
	}
}
