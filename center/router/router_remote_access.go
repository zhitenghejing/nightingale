package router

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/i18nx"
	"github.com/ccfos/nightingale/v6/pkg/remoteaccess"
	"github.com/gin-gonic/gin"
)

func remoteAccessBody(c *gin.Context, value interface{}) error {
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 32768))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil || d.Decode(new(interface{})) != io.EOF {
		return &remoteaccess.Error{Code: 400, Message: "invalid remote access configuration request"}
	}
	return nil
}
func remoteAccessReply(c *gin.Context, value interface{}, err error) {
	status, message := 200, ""
	if err != nil {
		value = nil
		status = 503
		message = "remote access operation failed"
		var e *remoteaccess.Error
		if errors.As(err, &e) {
			status = e.Code
			message = e.Message
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"dat": value, "err": i18nx.Translate(c.GetHeader("X-Language"), message)})
}
func (rt *Router) configRemoteAccess(pages *gin.RouterGroup) {
	g := pages.Group("/remote-access", rt.auth(), rt.user())
	g.GET("/features", func(c *gin.Context) {
		actor := c.MustGet("user").(*models.User).Id
		can := func(op string) bool { _, e := remoteaccess.Permission(models.DB(rt.Ctx), actor, op); return e == nil }
		// Keep legacy clients' input controls enabled on both transports.
		remoteAccessReply(c, gin.H{"enabled": rt.RemoteAccess != nil, "secure_input": true, "configure": can(remoteaccess.PermConfigure), "manage_credentials": can(remoteaccess.PermCredentials), "execute": can(remoteaccess.PermExecute), "terminal": rt.sshTerminalEnabled() && can(remoteaccess.PermTerminal), "rdp": rt.rdpConsoleEnabled() && can(remoteaccess.PermTerminal)}, nil)
	})
	if rt.RemoteAccess == nil {
		return
	}
	if rt.sshTerminalEnabled() || rt.rdpConsoleEnabled() {
		g.POST("/terminal/ticket", rt.sshTerminalTicket)
		pages.GET("/remote-access/terminal/socket", rt.sshTerminalSocket)
	}
	actor := func(c *gin.Context) int64 { return c.MustGet("user").(*models.User).Id }
	g.POST("/connection/test", func(c *gin.Context) {
		var f remoteaccess.ConnectionTestInput
		if err := remoteAccessBody(c, &f); err != nil {
			remoteAccessReply(c, nil, err)
			return
		}
		f.Connection = f.Connection.ForDevice()
		out, err := rt.RemoteAccess.TestConnection(c.Request.Context(), actor(c), f)
		remoteAccessReply(c, out, err)
	})
	g.POST("/host-key", func(c *gin.Context) {
		var f remoteaccess.HostKeyInput
		if err := remoteAccessBody(c, &f); err != nil {
			remoteAccessReply(c, nil, err)
			return
		}
		f.Connection = f.Connection.ForDevice()
		out, err := rt.RemoteAccess.ProbeHostKey(c.Request.Context(), actor(c), f)
		remoteAccessReply(c, out, err)
	})
	g.GET("/connection", func(c *gin.Context) {
		out, e := rt.RemoteAccess.GetConnection(c.Request.Context(), actor(c), c.Query("ident"))
		remoteAccessReply(c, out, e)
	})
	g.PUT("/connection", func(c *gin.Context) {
		var f remoteaccess.ConnectionInput
		e := remoteAccessBody(c, &f)
		if e != nil {
			remoteAccessReply(c, nil, e)
			return
		}
		out, e := rt.RemoteAccess.SaveConnection(c.Request.Context(), actor(c), c.Query("ident"), f.ForDevice())
		remoteAccessReply(c, out, e)
	})
	g.DELETE("/connection", func(c *gin.Context) {
		var f struct {
			RecordVersion int64 `json:"record_version"`
		}
		e := remoteAccessBody(c, &f)
		if e == nil {
			e = rt.RemoteAccess.DeleteConnection(c.Request.Context(), actor(c), c.Query("ident"), f.RecordVersion)
		}
		remoteAccessReply(c, nil, e)
	})
}
