package router

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/remoteaccess"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRemoteAccessRoutesNeverExposeSecrets(t *testing.T) {
	r, rt, _, _ := collectionRouterFixture(t)
	db := models.DB(rt.Ctx)
	require.NoError(t, db.AutoMigrate(models.OpsAccessTables()...))
	rt.RemoteAccess = &remoteaccess.Store{DB: db, Keys: &remoteaccess.Keyring{Active: "test", Keys: map[string]string{"test": base64.StdEncoding.EncodeToString(make([]byte, 32))}}}
	rt.configRemoteAccess(r.Group("/api/n9e"))
	call := func(method, path, body, user string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://preview.example/api/n9e/remote-access/"+path, strings.NewReader(body))
		req.Header.Set("X-Collection-Test-User", user)
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	require.Equal(t, 401, call("GET", "features", "", "").Code)
	require.Equal(t, 404, call("GET", "credentials", "", "collection-admin").Code)
	require.Equal(t, 404, call("POST", "credentials", "{}", "collection-admin").Code)
	require.Contains(t, call("GET", "features", "", "collection-admin").Body.String(), `"secure_input":true`)

	require.Equal(t, 403, call("POST", "host-key", `{"endpoint":"target","connection":{"group_id":1,"target":{"host":"127.0.0.1","port":22}}}`, "collection-reader").Code)
	require.Equal(t, 400, call("POST", "host-key", `{"endpoint":"target","connection":{"group_id":1,"target":{"host":"example.com","port":22}}}`, "collection-admin").Code)
	require.Equal(t, 403, call("POST", "connection/test", `{"connection":{"enabled":true,"group_id":1}}`, "collection-reader").Code)
	for _, ident := range []string{"windows-a", "windows-b"} {
		require.NoError(t, db.Create(&models.Target{Ident: ident}).Error)
		require.NoError(t, db.Create(&models.TargetBusiGroup{TargetIdent: ident, GroupId: 1}).Error)
	}
	const endpoint = "connection?ident=windows-a"
	body := `{"enabled":true,"protocol":"rdp","group_id":1,"target":{"host":"192.0.2.10","port":3389,"new_credential":{"username":"private-user","password":"private-test-password"}}}`
	saved := call("PUT", endpoint, body, "collection-admin")
	require.Equal(t, 200, saved.Code, saved.Body.String())
	require.Equal(t, "no-store", saved.Header().Get("Cache-Control"))
	var envelope struct {
		Dat remoteaccess.ConnectionView `json:"dat"`
	}
	require.NoError(t, json.Unmarshal(saved.Body.Bytes(), &envelope))
	require.Equal(t, "rdp", envelope.Dat.Protocol)
	for _, response := range []*httptest.ResponseRecorder{saved, call("GET", endpoint, "", "collection-admin")} {
		require.Equal(t, 200, response.Code)
		require.NotContains(t, response.Body.String(), "private-user")
		require.NotContains(t, response.Body.String(), "private-test-password")
		require.NotContains(t, response.Body.String(), "ciphertext")
	}
	var credential models.OpsCredential
	require.NoError(t, db.First(&credential, "id = ?", envelope.Dat.Target.CredentialID).Error)
	require.True(t, credential.DeviceOnly)
	reused := `{"enabled":true,"protocol":"rdp","group_id":1,"target":{"host":"192.0.2.10","port":3389,"credential_id":"` + credential.ID + `"}}`
	require.Equal(t, 400, call("PUT", "connection?ident=windows-b", reused, "collection-admin").Code)
	for _, invalid := range []string{body + `{}`, `{"password":"private-test-password","unsupported":true}`, strings.Repeat("x", 40000)} {
		out := call("PUT", endpoint, invalid, "collection-admin")
		require.Equal(t, 400, out.Code)
		require.NotContains(t, out.Body.String(), "private-test-password")
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	collectionReply(c, nil, &remoteaccess.Error{Code: 403, Message: "SSH access denied"})
	require.Equal(t, 403, w.Code)
}
