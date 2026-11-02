package router

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/ccfos/nightingale/v6/memsto"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/aop"
	"github.com/ccfos/nightingale/v6/pkg/collection"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func collectionRouterFixture(t *testing.T) (*gin.Engine, *Router, models.MonitorCollector, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	pool, _ := db.DB()
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.BusiGroup{}, &models.Target{}, &models.TargetBusiGroup{}, &models.Datasource{}, &models.BusiGroupMember{}, &models.UserGroupMember{}, &models.RoleOperation{}))
	for _, u := range []models.User{{Id: 1, Username: "collection-admin", Roles: "Admin"}, {Id: 2, Username: "collection-reader", Roles: "CollectionReader"}, {Id: 3, Username: "collection-outsider", Roles: "CollectionReader"}} {
		require.NoError(t, db.Create(&u).Error)
	}
	require.NoError(t, db.Create(&models.BusiGroup{Id: 1, Name: "fixture"}).Error)
	require.NoError(t, db.Create(&models.Datasource{Id: 1, Name: "fixture", PluginType: "prometheus"}).Error)
	require.NoError(t, db.Create(&models.UserGroupMember{GroupId: 10, UserId: 2}).Error)
	require.NoError(t, db.Create(&models.BusiGroupMember{BusiGroupId: 1, UserGroupId: 10, PermFlag: "ro"}).Error)
	require.NoError(t, db.Create(&models.RoleOperation{RoleName: "CollectionReader", Operation: "/collection/read"}).Error)
	store := &collection.Store{DB: db, Keys: &collection.Keyring{Active: "fixture", Keys: map[string]string{"fixture": base64.StdEncoding.EncodeToString(make([]byte, 32))}}, Writers: map[string]bool{"fixture-writer": true}}
	require.NoError(t, store.Migrate())
	collector, secret, err := store.CreateCollector(collection.Scope{Admin: true, ActorID: 1}, collection.CollectorInput{Name: "fixture", AgentIdent: "fixture", DatasourceID: 1, GroupIDs: []int64{1}, Networks: []string{"192.0.2.0/24"}, WriterUser: "fixture-writer"})
	require.NoError(t, err)
	rt := &Router{Ctx: nctx.NewContext(context.Background(), db, true), UserCache: &memsto.UserCacheType{}, Collection: store, CollectionStatus: &collection.StatusService{Store: store}}
	rt.HTTP.ProxyAuth.Enable = true
	rt.HTTP.ProxyAuth.HeaderUserNameKey = "X-Collection-Test-User"
	rt.HTTP.APIForAgent.Enable = true
	rt.Center.Collection.Enable = true
	rt.Center.Collection.ProviderEnable = true
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(aop.Recovery())
	rt.configCollection(r, r.Group("/api/n9e"))
	return r, rt, collector, secret
}
func collectionCall(r *gin.Engine, method, path string, body interface{}, user string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "router-fixture-request")
	if user != "" {
		req.Header.Set("X-Collection-Test-User", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestCollectionRoutesAuthenticateAndKeepProviderProtocolBare(t *testing.T) {
	r, rt, c, secret := collectionRouterFixture(t)
	for _, path := range []string{"features", "devices", "collectors", "credentials", "templates", "publications", "devices/unknown", "probes/unknown"} {
		w := collectionCall(r, "GET", "/api/n9e/collection/"+path, nil, "")
		require.Equal(t, 401, w.Code, path)
	}
	request := httptest.NewRequest("GET", "/v1/n9e/collection/configs?agent_hostname=some-other-collector&version=unknown", nil)
	request.SetBasicAuth(c.ID, secret)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, request)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var full collection.FullSnapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &full))
	require.NotEmpty(t, full.Version)
	require.NotNil(t, full.Configs)
	require.NotContains(t, w.Body.String(), `"dat"`)
	request = httptest.NewRequest("GET", "/v1/n9e/collection/configs", nil)
	request.SetBasicAuth(c.ID, "wrong")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, request)
	require.Equal(t, 401, w.Code)
	require.NotContains(t, w.Body.String(), "version")
	require.NotContains(t, w.Body.String(), "configs")
	require.NoError(t, rt.Collection.DB.Model(&models.MonitorPublication{}).Where("version = ?", full.Version).Update("ciphertext", "broken").Error)
	request = httptest.NewRequest("GET", "/v1/n9e/collection/configs", nil)
	request.SetBasicAuth(c.ID, secret)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, request)
	require.Equal(t, 503, w.Code)
	require.NotContains(t, w.Body.String(), "version")
	require.NotContains(t, w.Body.String(), "configs")
}
func TestCollectionRoutesEnforceOperationAndObjectPermissions(t *testing.T) {
	r, rt, c, _ := collectionRouterFixture(t)
	scope := collection.Scope{Admin: true, ActorID: 1}
	cred, err := rt.Collection.SaveCredential(scope, "", collection.CredentialInput{Name: "fixture", GroupID: 1, Community: "fixture-only-secret"})
	require.NoError(t, err)
	d, err := rt.Collection.SaveDevice(scope, "", "router-create-fixture", collection.DeviceInput{Name: "fixture", Address: "192.0.2.11", GroupID: 1, CollectorID: c.ID, CredentialID: cred.ID, Enabled: true})
	require.NoError(t, err)
	w := collectionCall(r, "GET", "/api/n9e/collection/devices/"+d.ID, nil, "collection-reader")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "query_error")
	require.NotContains(t, w.Body.String(), "fixture-only-secret")
	w = collectionCall(r, "GET", "/api/n9e/collection/devices/"+d.ID, nil, "collection-outsider")
	require.Equal(t, 403, w.Code)
	w = collectionCall(r, "POST", "/api/n9e/collection/devices/"+d.ID+"/disable", map[string]interface{}{"record_version": d.RecordVersion}, "collection-reader")
	require.Equal(t, 403, w.Code)
	w = collectionCall(r, "GET", "/api/n9e/collection/credentials", nil, "collection-outsider")
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), cred.ID)
	w = collectionCall(r, "PUT", "/api/n9e/collection/devices/"+d.ID, map[string]interface{}{"record_version": d.RecordVersion, "ciphertext": "forged"}, "collection-admin")
	require.Equal(t, 400, w.Code)
}
func TestCollectionProviderRequiresExplicitCollectionAndAgentGate(t *testing.T) {
	_, rt, _, _ := collectionRouterFixture(t)
	rt.HTTP.APIForAgent.Enable = false
	r := gin.New()
	rt.configCollection(r, r.Group("/api/n9e"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/v1/n9e/collection/configs", nil))
	require.Equal(t, 404, w.Code)
	rt.Collection = nil
	r = gin.New()
	rt.configCollection(r, r.Group("/api/n9e"))
	w = collectionCall(r, "GET", "/api/n9e/collection/features", nil, "collection-admin")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"enabled":false`)
}

func TestCollectionNativeFilterDoesNotLeakToUsersWithoutGroups(t *testing.T) {
	_, rt, c, _ := collectionRouterFixture(t)
	scope := collection.Scope{Admin: true, ActorID: 1}
	cred, err := rt.Collection.SaveCredential(scope, "", collection.CredentialInput{Name: "fixture", GroupID: 1, Community: "fixture"})
	require.NoError(t, err)
	_, err = rt.Collection.SaveDevice(scope, "", "native-filter-fixture", collection.DeviceInput{Name: "fixture", Address: "192.0.2.20", GroupID: 1, CollectorID: c.ID, CredentialID: cred.ID, Enabled: true})
	require.NoError(t, err)
	for _, item := range []struct {
		id    int64
		count int
	}{{2, 1}, {3, 0}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/api/n9e/targets", nil)
		c.Set("user", &models.User{Id: item.id, RolesLst: []string{"CollectionReader"}})
		var targets []models.Target
		require.NoError(t, rt.collectionTargetFilter(c)(rt.Ctx.DB.Model(&models.Target{})).Find(&targets).Error)
		require.Len(t, targets, item.count)
	}
}

func TestCollectionTemplateManagementPermissionsAndValidation(t *testing.T) {
	r, rt, _, _ := collectionRouterFixture(t)
	require.NoError(t, rt.Collection.DB.AutoMigrate(&models.BuiltinComponent{}))
	require.NoError(t, rt.Collection.DB.Create(&models.BuiltinComponent{Ident: "SNMP"}).Error)
	input := collection.TemplateInput{Name: "Router SNMP", Component: "SNMP", Config: "[[field]]\nname=\"services\"\noid=\".1.3.6.1.2.1.1.7.0\""}
	require.Equal(t, 401, collectionCall(r, "POST", "/api/n9e/collection/templates", input, "").Code)
	require.Equal(t, 403, collectionCall(r, "POST", "/api/n9e/collection/templates", input, "collection-reader").Code)
	w := collectionCall(r, "POST", "/api/n9e/collection/templates", input, "collection-admin")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "Router SNMP")
	w = collectionCall(r, "GET", "/api/n9e/collection/templates", nil, "collection-reader")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "Router SNMP")
	input.Config = "community=\"forbidden\""
	require.Equal(t, 400, collectionCall(r, "POST", "/api/n9e/collection/templates", input, "collection-admin").Code)
}
