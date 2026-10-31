package router

import (
	"strconv"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/collection"
	"github.com/gin-gonic/gin"
)

func (rt *Router) configCollectionCatalog(group *gin.RouterGroup) {
	group.GET("/catalog", func(c *gin.Context) {
		out, err := rt.Collection.CatalogSummary()
		collectionReply(c, out, err)
	})
	group.GET("/catalog/:id", func(c *gin.Context) {
		id, err := catalogID(c)
		if err != nil {
			collectionReply(c, nil, err)
			return
		}
		out, err := rt.Collection.CatalogEntry(id)
		collectionReply(c, out, err)
	})
	// Catalog authors reuse the existing component permissions; device writers
	// may read published definitions but cannot publish or alter global templates.
	admin := rt.admin()
	group.POST("/catalog", rt.perm("/components/add"), admin, func(c *gin.Context) {
		var f collection.CatalogCreateInput
		if err := catalogBody(c, &f); err != nil {
			collectionReply(c, nil, err)
			return
		}
		scope, err := rt.collectionScope(c)
		if err != nil {
			collectionReply(c, nil, err)
			return
		}
		id, err := rt.Collection.CreateCatalog(scope, f)
		collectionReply(c, gin.H{"component_id": id}, err)
	})
	group.DELETE("/catalog/:id", rt.perm("/components/del"), admin, func(c *gin.Context) {
		version, err := collectionVersion(c)
		if err != nil {
			collectionReply(c, nil, err)
			return
		}
		scope, err := rt.collectionScope(c)
		if err == nil {
			var id uint64
			id, err = catalogID(c)
			if err == nil {
				err = rt.Collection.DeleteCatalog(scope, id, version)
			}
		}
		collectionReply(c, nil, err)
	})
	group.PUT("/catalog/:id/metadata", rt.perm("/components/put"), admin, func(c *gin.Context) {
		var f collection.CatalogMetadata
		if err := catalogBody(c, &f); err != nil {
			collectionReply(c, nil, err)
			return
		}
		scope, err := rt.collectionScope(c)
		if err == nil {
			var id uint64
			id, err = catalogID(c)
			if err == nil {
				err = rt.Collection.SaveCatalogMetadata(scope, id, f)
			}
		}
		collectionReply(c, nil, err)
	})
	for _, action := range []string{"draft", "publish"} {
		publish := action == "publish"
		group.PUT("/catalog/:id/"+action, rt.perm("/components/put"), admin, func(c *gin.Context) {
			var f collection.CatalogDraftInput
			if err := collectionDecodeBody(c, &f, 8<<20); err != nil {
				collectionReply(c, nil, err)
				return
			}
			scope, err := rt.collectionScope(c)
			if err == nil {
				var id uint64
				id, err = catalogID(c)
				if err == nil {
					err = rt.Collection.SaveCatalogDraft(scope, id, f, publish)
				}
			}
			collectionReply(c, nil, err)
		})
	}
	for _, path := range []string{"/categories", "/categories/:id"} {
		group.PUT(path, rt.perm("/components/put"), admin, func(c *gin.Context) {
			var f models.MonitorCategory
			if err := collectionBody(c, &f); err != nil {
				collectionReply(c, nil, err)
				return
			}
			scope, err := rt.collectionScope(c)
			if err == nil {
				err = rt.Collection.SaveCategory(scope, c.Param("id"), f)
			}
			collectionReply(c, nil, err)
		})
	}
	group.DELETE("/categories/:id", rt.perm("/components/del"), admin, func(c *gin.Context) {
		version, err := collectionVersion(c)
		if err != nil {
			collectionReply(c, nil, err)
			return
		}
		scope, err := rt.collectionScope(c)
		if err == nil {
			err = rt.Collection.DeleteCategory(scope, c.Param("id"), version)
		}
		collectionReply(c, nil, err)
	})
}

// Icons are limited to 2 MiB before base64 encoding. Other collection requests
// retain their smaller request limit.
func catalogBody(c *gin.Context, value interface{}) error {
	return collectionDecodeBody(c, value, 3<<20)
}

func catalogID(c *gin.Context) (uint64, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, &collection.Error{Code: 400, Message: "invalid template identifier"}
	}
	return id, nil
}
