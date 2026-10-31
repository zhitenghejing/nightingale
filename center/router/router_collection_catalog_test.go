package router

import (
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/collection"
	"github.com/stretchr/testify/require"
)

func TestCatalogRoutesSeparateMetadataFromExecutableDefinitions(t *testing.T) {
	r, rt, _, _ := collectionRouterFixture(t)
	require.NoError(t, rt.Collection.DB.AutoMigrate(&models.BuiltinComponent{}, &models.BuiltinMetric{}, &models.BuiltinPayload{}))
	require.NoError(t, rt.Collection.DB.Create(&models.BuiltinComponent{ID: 1, Ident: "MySQL", Readme: "original instructions"}).Error)
	w := collectionCall(r, "GET", "/api/n9e/collection/catalog", nil, "")
	require.Equal(t, 401, w.Code)
	w = collectionCall(r, "GET", "/api/n9e/collection/catalog", nil, "collection-reader")
	require.Equal(t, 200, w.Code, w.Body.String())
	view, err := rt.Collection.Catalog()
	require.NoError(t, err)
	require.Len(t, view.Entries, 1)
	e := view.Entries[0]
	metadata := collection.CatalogMetadata{Name: "MySQL custom icon", Logo: "https://example.org/mysql.png", CategoryID: e.CategoryID, RecordVersion: e.RecordVersion}
	w = collectionCall(r, "PUT", "/api/n9e/collection/catalog/1/metadata", metadata, "collection-reader")
	require.Equal(t, 403, w.Code)
	w = collectionCall(r, "PUT", "/api/n9e/collection/catalog/1/metadata", map[string]interface{}{"name": "attempt", "config": "arbitrary configuration"}, "collection-admin")
	require.Equal(t, 400, w.Code)
	w = collectionCall(r, "PUT", "/api/n9e/collection/catalog/1/metadata", metadata, "collection-admin")
	require.Equal(t, 200, w.Code, w.Body.String())
	w = collectionCall(r, "GET", "/api/n9e/collection/catalog", nil, "collection-admin")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"logo":"https://example.org/mysql.png"`)
	w = collectionCall(r, "PUT", "/api/n9e/collection/catalog/1/metadata", metadata, "collection-admin")
	require.Equal(t, 409, w.Code)
	w = collectionCall(r, "PUT", "/api/n9e/collection/catalog/invalid/metadata", metadata, "collection-admin")
	require.Equal(t, 400, w.Code)
	var c models.BuiltinComponent
	require.NoError(t, rt.Collection.DB.First(&c, "id = ?", 1).Error)
	require.Equal(t, "MySQL", c.Ident)
	require.Equal(t, "original instructions", c.Readme)
}
