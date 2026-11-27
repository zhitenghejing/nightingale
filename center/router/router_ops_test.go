package router

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ccfos/nightingale/v6/memsto"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/aop"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/ops"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOpsRoutesAuthorizationAndCaseLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/routes.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, _ := db.DB()
	t.Cleanup(func() { _ = conn.Close() })
	tables := append(models.OpsTables(), models.KnowledgeTables()...)
	tables = append(tables, &models.User{}, &models.BusiGroup{}, &models.BusiGroupMember{}, &models.UserGroupMember{})
	require.NoError(t, db.AutoMigrate(tables...))
	u := models.User{Id: 1, Username: "ops-admin", Roles: "Admin", Contacts: []byte("{}")}
	require.NoError(t, db.Create(&u).Error)
	require.NoError(t, db.Create(&models.User{Id: 2, Username: "ops-guest", Roles: "Guest", Contacts: []byte("{}")}).Error)
	require.NoError(t, db.Create(&models.BusiGroup{Id: 1, Name: "Demo group"}).Error)
	rt := &Router{Ctx: nctx.NewContext(context.Background(), db, true), UserCache: &memsto.UserCacheType{}, Ops: &ops.Service{DB: db, Knowledge: &knowledge.Store{DB: db, DataDir: t.TempDir()}}}
	rt.HTTP.ProxyAuth.Enable = true
	rt.HTTP.ProxyAuth.HeaderUserNameKey = "X-Archive-Test-User"
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(aop.Recovery())
	rt.configOpsRoutes(engine.Group("/api/n9e"))
	response := archiveRequest(engine, http.MethodGet, "/api/n9e/ops/config", nil, "")
	require.NotEqual(t, 200, response.Code)
	response = archiveRequest(engine, http.MethodPost, "/api/n9e/ops/cases", map[string]interface{}{"group_id": 1, "title": "Manual case", "context": map[string]string{}}, u.Username)
	require.Equal(t, 200, response.Code, response.Body.String())
	var data struct {
		Dat models.OpsCase `json:"dat"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &data))
	require.Positive(t, data.Dat.ID)
	path := "/api/n9e/ops/cases/" + strconvID(data.Dat.ID)
	response = archiveRequest(engine, http.MethodGet, path, nil, "ops-guest")
	require.Equal(t, 403, response.Code)
	response = archiveRequest(engine, http.MethodPost, path+"/close", map[string]interface{}{"revision": 1, "kind": "resolved", "summary": "No evidence"}, u.Username)
	require.NotEqual(t, 200, response.Code)
	response = archiveRequest(engine, http.MethodPost, path+"/close", map[string]interface{}{"revision": 1, "kind": "cancelled", "summary": "No longer needed; no recovery claimed"}, u.Username)
	require.Equal(t, 200, response.Code, response.Body.String())
}
func TestOpsPromQLScopeRejectsOtherTargetsAndClockModifiers(t *testing.T) {
	q, selectors, err := scopeOpsQuery(`sum(rate(cpu_seconds_total[5m]))`, "target-a")
	require.NoError(t, err)
	require.Contains(t, q, `ident="target-a"`)
	require.Len(t, selectors, 1)
	for _, expr := range []string{`cpu{ident="target-b"}`, `cpu{ident=~".*"}`, `cpu offset 5m`, `cpu @ 1234`, `sum_over_time((cpu)[30m:1m] offset 1h)`, `vector(0)`} {
		_, _, err = scopeOpsQuery(expr, "target-a")
		require.Error(t, err, expr)
	}
	_, _, err = scopeOpsQuery("cpu", "")
	require.Error(t, err)
}

func TestOpsPlanRoutesRequireHumanConfirmation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/plans.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, _ := db.DB()
	t.Cleanup(func() { _ = conn.Close() })
	tables := append(models.OpsTables(), models.KnowledgeTables()...)
	tables = append(tables, &models.User{}, &models.BusiGroup{}, &models.BusiGroupMember{}, &models.UserGroupMember{})
	require.NoError(t, db.AutoMigrate(tables...))
	require.NoError(t, db.Create(&models.User{Id: 1, Username: "planner", Roles: "Admin", Contacts: []byte("{}")}).Error)
	require.NoError(t, db.Create(&models.BusiGroup{Id: 1, Name: "Plans"}).Error)
	rt := &Router{Ctx: nctx.NewContext(context.Background(), db, true), UserCache: &memsto.UserCacheType{}, Ops: &ops.Service{DB: db, Knowledge: &knowledge.Store{DB: db, DataDir: t.TempDir()}}}
	rt.HTTP.ProxyAuth.Enable = true
	rt.HTTP.ProxyAuth.HeaderUserNameKey = "X-Archive-Test-User"
	engine := gin.New()
	engine.Use(aop.Recovery())
	rt.configOpsRoutes(engine.Group("/api/n9e"))
	c, err := rt.Ops.Create(1, 1, "New fault", map[string]string{"environment": "test"})
	require.NoError(t, err)
	evidence, err := rt.Ops.AddManualEvidence(1, c.ID, "Observed current application failure")
	require.NoError(t, err)
	path := "/api/n9e/ops/cases/" + strconvID(c.ID)
	in := ops.PlanInput{Revision: c.Revision, Title: "Case-only recovery", Scope: "This test application", Rationale: "Observed failure; reason unknown", EvidenceIDs: []int64{evidence.ID}, Spec: knowledge.Spec{Summary: "Restore test round trip", Steps: []knowledge.Step{{Key: "check", Title: "Inspect current status", Kind: "manual"}}, Checks: []knowledge.Check{{Key: "business", Title: "Round trip passes", Kind: "manual", Required: true}}, Escalation: "Contact owner if verification fails"}}
	response := archiveRequest(engine, http.MethodPost, path+"/plans", in, "")
	require.NotEqual(t, 200, response.Code)
	response = archiveRequest(engine, http.MethodPost, path+"/plans", in, "planner")
	require.Equal(t, 200, response.Code, response.Body.String())
	var data struct{ Dat models.OpsPlan }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &data))
	response = archiveRequest(engine, http.MethodPost, path+"/verify", nil, "planner")
	require.Equal(t, 422, response.Code)
	decision := path + "/plans/" + strconvID(data.Dat.ID) + "/decision"
	response = archiveRequest(engine, http.MethodPost, decision, map[string]interface{}{"revision": 2, "accept": true, "note": "Scope and standards checked"}, "planner")
	require.Equal(t, 200, response.Code, response.Body.String())
	response = archiveRequest(engine, http.MethodPost, decision, map[string]interface{}{"revision": 3, "accept": true, "note": "Repeat"}, "planner")
	require.Equal(t, 409, response.Code)
	response = archiveRequest(engine, http.MethodPost, path+"/checks/business", map[string]interface{}{"passed": true, "note": "Round trip checked and passed"}, "planner")
	require.Equal(t, 200, response.Code, response.Body.String())
	response = archiveRequest(engine, http.MethodPost, path+"/close", map[string]interface{}{"revision": 3, "kind": "resolved", "summary": "Verified without knowledge"}, "planner")
	require.Equal(t, 200, response.Code, response.Body.String())
	response = archiveRequest(engine, http.MethodPost, path+"/plans", in, "planner")
	require.Equal(t, 409, response.Code)
}
