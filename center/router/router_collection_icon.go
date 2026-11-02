package router

import (
	"github.com/ccfos/nightingale/v6/pkg/collection"
	"github.com/gin-gonic/gin"
)

func (rt *Router) configCollectionIcons(pages, group *gin.RouterGroup) {
	group.GET("/icons", func(c *gin.Context) {
		icons, err := rt.Collection.Icons()
		collectionReply(c, icons, err)
	})
	group.POST("/icons", rt.perm("/components/put"), rt.admin(), func(c *gin.Context) {
		var input collection.IconInput
		if err := catalogBody(c, &input); err != nil {
			collectionReply(c, nil, err)
			return
		}
		scope, err := rt.collectionScope(c)
		if err != nil {
			collectionReply(c, nil, err)
			return
		}
		icon, err := rt.Collection.AddIcon(scope, input)
		collectionReply(c, icon, err)
	})
	// Like builtin integration icons, immutable artwork is usable in normal <img>
	// elements. It contains no device data and cannot execute as a document.
	pages.GET("/collection/icons/:id/content", func(c *gin.Context) {
		raw, mime, err := rt.Collection.IconContent(c.Param("id"))
		if err != nil {
			collectionReply(c, nil, err)
			return
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Security-Policy", "default-src 'none'; img-src data:; style-src 'unsafe-inline'; sandbox")
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.Data(200, mime, raw)
	})
}
