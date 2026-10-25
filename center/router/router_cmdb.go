package router

import (
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	"github.com/gin-gonic/gin"
)

func (rt *Router) configCMDB(r *gin.Engine) {
	if rt.CMDB == nil {
		return
	}
	r.Any("/api/cmdb/*path", rt.auth(), rt.user(), func(c *gin.Context) {
		me := c.MustGet("user").(*models.User)
		rt.CMDB.ServeUser(c.Writer, c.Request, cmdb.User{ID: me.Id, Admin: me.IsAdmin(), Disabled: me.Disabled})
	})
}
