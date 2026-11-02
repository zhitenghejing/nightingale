package router

import (
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/collection"
	"github.com/stretchr/testify/require"
)

func TestCatalogMoveRoutesRequireAdministratorAndRejectStaleOrInvalidMoves(t *testing.T) {
	r, rt, _, _ := collectionRouterFixture(t)
	require.NoError(t, rt.Collection.DB.AutoMigrate(&models.BuiltinComponent{}, &models.BuiltinMetric{}, &models.BuiltinPayload{}))
	require.NoError(t, rt.Collection.DB.Create(&models.BuiltinComponent{ID: 1, Ident: "MySQL", Readme: "original instructions"}).Error)
	view, err := rt.Collection.Catalog()
	require.NoError(t, err)
	leaf := view.Entries[0].CategoryID
	var category models.MonitorCategory
	require.NoError(t, rt.Ctx.DB.First(&category, "id = ?", leaf).Error)
	require.NoError(t, rt.Ctx.DB.Create(&models.RoleOperation{RoleName: "CollectionReader", Operation: "/components/put"}).Error)
	for _, path := range []string{"/catalog/1/category", "/categories/" + leaf + "/move"} {
		require.Equal(t, 401, collectionCall(r, "PUT", "/api/n9e/collection"+path, nil, "").Code)
		require.Equal(t, 403, collectionCall(r, "PUT", "/api/n9e/collection"+path, nil, "collection-reader").Code)
	}
	metadata := map[string]interface{}{"category_id": leaf, "record_version": view.Entries[0].RecordVersion}
	require.Equal(t, 200, collectionCall(r, "PUT", "/api/n9e/collection/catalog/1/category", metadata, "collection-admin").Code)
	metadata["name"] = "must not overwrite other metadata"
	require.Equal(t, 400, collectionCall(r, "PUT", "/api/n9e/collection/catalog/1/category", metadata, "collection-admin").Code)
	w := collectionCall(r, "PUT", "/api/n9e/collection/categories/"+leaf+"/move", collection.CategoryMoveInput{ParentID: category.ParentID, RecordVersion: category.RecordVersion}, "collection-admin")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 409, collectionCall(r, "PUT", "/api/n9e/collection/categories/"+leaf+"/move", collection.CategoryMoveInput{ParentID: category.ParentID, RecordVersion: category.RecordVersion}, "collection-admin").Code)
	require.Equal(t, 400, collectionCall(r, "PUT", "/api/n9e/collection/catalog/1/category", collection.CatalogMoveInput{CategoryID: category.ParentID, RecordVersion: view.Entries[0].RecordVersion}, "collection-admin").Code)
}

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
