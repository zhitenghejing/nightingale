package router

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ccfos/nightingale/v6/memsto"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/aop"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func archiveTestRouter(t *testing.T) (*gin.Engine, *Router, *models.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err = db.AutoMigrate(append(models.ArchiveTables(), &models.User{})...); err != nil {
		t.Fatal(err)
	}
	user := &models.User{Username: "archive-guest", Nickname: "Archive Guest", Roles: "Guest", Contacts: []byte("{}")}
	if err = db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	rt := &Router{Ctx: nctx.NewContext(context.Background(), db, true), UserCache: &memsto.UserCacheType{}}
	rt.HTTP.ProxyAuth.Enable = true
	rt.HTTP.ProxyAuth.HeaderUserNameKey = "X-Archive-Test-User"
	rt.Center.Archives.DataDir = t.TempDir()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(aop.Recovery())
	rt.configArchiveRoutes(r.Group("/api/n9e"))
	return r, rt, user
}
func archiveRequest(r *gin.Engine, method, path string, data interface{}, username string) *httptest.ResponseRecorder {
	var content []byte
	if data != nil {
		content, _ = json.Marshal(data)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(content))
	req.Header.Set("Content-Type", "application/json")
	if username != "" {
		req.Header.Set("X-Archive-Test-User", username)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestArchiveRoutesRequireAuthentication(t *testing.T) {
	r, _, _ := archiveTestRouter(t)
	for _, tc := range []struct{ method, path string }{{"GET", "/api/n9e/archives"}, {"POST", "/api/n9e/archives"}, {"GET", "/api/n9e/archives/files/1"}, {"GET", "/api/n9e/archives/documents/1"}, {"GET", "/api/n9e/archives/documents/1/versions"}, {"POST", "/api/n9e/archives/documents/1/save"}, {"POST", "/api/n9e/archives/1/assets"}, {"POST", "/api/n9e/archives/documents/1/lease/resume"}, {"GET", "/api/n9e/archives/licenses"}, {"GET", "/api/n9e/archives/exports/invalid/file"}} {
		w := archiveRequest(r, tc.method, tc.path, nil, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
}
func TestArchiveGuestCanCreateAndAuditUsesLogin(t *testing.T) {
	r, rt, user := archiveTestRouter(t)
	w := archiveRequest(r, "POST", "/api/n9e/archives", map[string]interface{}{"category": "training", "metadata": map[string]interface{}{"title": "Training", "lecturer": "Handwritten engineer"}}, user.Username)
	if w.Code != 200 {
		t.Fatalf("ordinary account blocked: %d %s", w.Code, w.Body)
	}
	var reply struct {
		Dat models.ArchiveRecord `json:"dat"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Dat.ID == 0 || reply.Dat.CreateBy != user.Id || reply.Dat.CreateName != user.Nickname {
		t.Fatal("creation identity not derived from login")
	}
	var log models.ArchiveLog
	if err := rt.Ctx.DB.Where("archive_id = ?", reply.Dat.ID).First(&log).Error; err != nil {
		t.Fatal(err)
	}
	if log.ActorID != user.Id || log.ActorName != "Archive Guest" {
		t.Fatal("audit actor incorrect")
	}
	w = archiveRequest(r, "GET", "/api/n9e/archives?category=training", nil, user.Username)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := rt.Ctx.DB.Model(user).Update("disabled", models.UserDisabled).Error; err != nil {
		t.Fatal(err)
	}
	w = archiveRequest(r, "GET", "/api/n9e/archives", nil, user.Username)
	if w.Code != 401 {
		t.Fatal("disabled account still authorized", w.Code, w.Body.String())
	}
}
