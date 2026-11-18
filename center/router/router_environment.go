package router

import (
	"strconv"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/environment"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"github.com/gin-gonic/gin"
)

// Center-only UI reads/writes; no alert-engine hot path or edge contract changes.
func (rt *Router) configEnvironmentRoutes(r *gin.RouterGroup) {
	g := r.Group("/environment", rt.auth(), rt.user())
	store := func(c *gin.Context) *environment.Store {
		return &environment.Store{DB: rt.Ctx.DB.WithContext(c.Request.Context())}
	}
	g.GET("/search", func(c *gin.Context) { v, e := store(c).Search(opsUser(c), c.Query("q")); opsResult(c, v, e) })
	g.GET("/context", func(c *gin.Context) { v, e := store(c).Context(opsUser(c), c.Query("ref")); opsResult(c, v, e) })
	g.GET("/trace", func(c *gin.Context) {
		depth, _ := strconv.Atoi(c.DefaultQuery("depth", "2"))
		v, e := store(c).Trace(opsUser(c), c.Query("ref"), c.DefaultQuery("direction", "out"), depth)
		opsResult(c, v, e)
	})
	g.GET("/timeline", func(c *gin.Context) { v, e := store(c).Timeline(opsUser(c), c.Query("ref")); opsResult(c, v, e) })
	g.POST("/objects", func(c *gin.Context) {
		var in models.EnvironmentObject
		if c.ShouldBindJSON(&in) != nil {
			opsResult(c, nil, opsauth.Fail(422, "input", "参数错误"))
			return
		}
		v, e := store(c).SaveObject(opsUser(c), in)
		opsResult(c, v, e)
	})
	g.POST("/relations", func(c *gin.Context) {
		var in environment.RelationInput
		if c.ShouldBindJSON(&in) != nil {
			opsResult(c, nil, opsauth.Fail(422, "input", "参数错误"))
			return
		}
		v, e := store(c).Add(opsUser(c), in)
		opsResult(c, v, e)
	})
	g.POST("/relations/:id", func(c *gin.Context) {
		var in struct {
			Revision int64  `json:"revision"`
			Action   string `json:"action"`
			environment.RelationInput
		}
		if c.ShouldBindJSON(&in) != nil {
			opsResult(c, nil, opsauth.Fail(422, "input", "参数错误"))
			return
		}
		id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
		v, e := store(c).Change(opsUser(c), id, in.Revision, in.Action, in.RelationInput)
		opsResult(c, v, e)
	})
	g.POST("/discover", func(c *gin.Context) {
		var in struct {
			Ref string `json:"ref"`
		}
		if c.ShouldBindJSON(&in) != nil {
			opsResult(c, nil, opsauth.Fail(422, "input", "参数错误"))
			return
		}
		v, e := store(c).Discover(opsUser(c), in.Ref)
		opsResult(c, v, e)
	})
}
