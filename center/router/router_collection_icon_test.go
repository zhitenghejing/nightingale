package router

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/collection"
	"github.com/stretchr/testify/require"
)

func TestIconRoutesPermissionsPersistenceAndSafeContent(t *testing.T) {
	r, rt, _, _ := collectionRouterFixture(t)
	require.NoError(t, rt.Ctx.DB.Create(&models.RoleOperation{RoleName: "CollectionReader", Operation: "/components/put"}).Error)
	raw := `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><rect width="32" height="32"/></svg>`
	input := collection.IconInput{Name: "Device.svg", DataURL: "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(raw))}
	for _, path := range []string{"/api/n9e/collection/icons"} {
		require.Equal(t, 401, collectionCall(r, "GET", path, nil, "").Code)
		require.Equal(t, 401, collectionCall(r, "POST", path, input, "").Code)
		require.Equal(t, 403, collectionCall(r, "POST", path, input, "collection-reader").Code)
	}
	w := collectionCall(r, "POST", "/api/n9e/collection/icons", input, "collection-admin")
	require.Equal(t, 200, w.Code, w.Body.String())
	var reply struct {
		Dat collection.Icon `json:"dat"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reply))
	content := collectionCall(r, "GET", reply.Dat.URL, nil, "")
	require.Equal(t, 200, content.Code)
	require.Equal(t, raw, content.Body.String())
	require.Equal(t, "image/svg+xml", content.Header().Get("Content-Type"))
	require.Contains(t, content.Header().Get("Content-Security-Policy"), "sandbox")
	require.Equal(t, "nosniff", content.Header().Get("X-Content-Type-Options"))
	w = collectionCall(r, "GET", "/api/n9e/collection/icons", nil, "collection-reader")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), reply.Dat.URL)
	require.NotContains(t, w.Body.String(), "base64")
	input.DataURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("broken"))
	require.Equal(t, 400, collectionCall(r, "POST", "/api/n9e/collection/icons", input, "collection-admin").Code)
	require.Equal(t, 400, collectionCall(r, "POST", "/api/n9e/collection/icons", map[string]string{"name": "x", "data_url": strings.Repeat("x", 3<<20)}, "collection-admin").Code)
	require.Equal(t, 404, collectionCall(r, "GET", "/api/n9e/collection/icons/invalid/content", nil, "").Code)
}
