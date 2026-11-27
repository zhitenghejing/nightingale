package router

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/remoteaccess"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	terminalProtocol  = "n9e-terminal-v1"
	terminalTicketKey = "n9e:ssh-terminal:"
	terminalTicketTTL = 30 * time.Second
	terminalLifetime  = 30 * time.Minute
	terminalIdleTime  = 10 * time.Minute
	terminalPingEvery = 20 * time.Second
)

// Consume the one-time ticket atomically on all supported Redis versions.
const takeTerminalTicket = `
local ticket = redis.call("GET", KEYS[1])
if ticket then redis.call("DEL", KEYS[1]) end
return ticket
`

type terminalTicket struct {
	Actor    int64  `json:"actor"`
	Ident    string `json:"ident"`
	Origin   string `json:"origin"`
	Protocol string `json:"protocol,omitempty"`
}

type terminalSlots struct {
	mu     sync.Mutex
	total  int
	actors map[int64]int
}

func (s *terminalSlots) acquire(actor int64) (release func(), ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.total >= 8 || s.actors[actor] >= 2 {
		return nil, false
	}
	if s.actors == nil {
		s.actors = make(map[int64]int)
	}
	s.total++
	s.actors[actor]++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.total--
		s.actors[actor]--
		if s.actors[actor] == 0 {
			delete(s.actors, actor)
		}
	}, true
}

func (rt *Router) sshTerminalEnabled() bool {
	return rt.Center.RemoteDiagnostics.EnableTerminal && rt.RemoteAccess != nil && rt.Redis != nil
}

func sshTerminalRequestOrigin(r *http.Request) string {
	if r.TLS != nil {
		return "https://" + r.Host
	}
	// The local proxy overwrites this header using the client-facing scheme.
	peer, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(peer); ip.IsLoopback() && r.Header.Get("X-Forwarded-Proto") == "https" {
		return "https://" + r.Host
	}
	return "http://" + r.Host
}

func sshTerminalOrigin(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Origin"), sshTerminalRequestOrigin(r))
}

func (rt *Router) sshTerminalTicket(c *gin.Context) {
	var f struct {
		Ident    string `json:"ident"`
		Protocol string `json:"protocol"`
	}
	if err := remoteAccessBody(c, &f); err != nil {
		remoteAccessReply(c, nil, err)
		return
	}
	if f.Ident == "" || len(f.Ident) > 764 {
		remoteAccessReply(c, nil, &remoteaccess.Error{Code: http.StatusBadRequest, Message: "select a device to open a console"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	actor := c.MustGet("user").(*models.User).Id
	var err error
	switch f.Protocol {
	case "", remoteaccess.ProtocolSSH:
		f.Protocol = remoteaccess.ProtocolSSH
		if !rt.sshTerminalEnabled() {
			err = &remoteaccess.Error{Code: 503, Message: "SSH console is unavailable"}
		} else {
			err = rt.RemoteAccess.CheckTerminal(ctx, actor, f.Ident)
		}
	case remoteaccess.ProtocolRDP:
		if !rt.rdpConsoleEnabled() {
			err = &remoteaccess.Error{Code: 503, Message: "RDP gateway is unavailable"}
		} else {
			err = rt.RemoteAccess.CheckRDP(ctx, actor, f.Ident)
		}
	default:
		err = &remoteaccess.Error{Code: 400, Message: "invalid console protocol"}
	}
	if err != nil {
		remoteAccessReply(c, nil, err)
		return
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		remoteAccessReply(c, nil, err)
		return
	}
	token := hex.EncodeToString(random)
	value, _ := json.Marshal(terminalTicket{Actor: actor, Ident: f.Ident, Origin: sshTerminalRequestOrigin(c.Request), Protocol: f.Protocol})
	if err := rt.Redis.Set(ctx, terminalTicketKey+token, value, terminalTicketTTL).Err(); err != nil {
		remoteAccessReply(c, nil, err)
		return
	}
	remoteAccessReply(c, gin.H{"ticket": token}, nil)
}

func (rt *Router) sshTerminalSocket(c *gin.Context) {
	r := c.Request
	if !sshTerminalOrigin(r) || !websocket.IsWebSocketUpgrade(r) {
		c.Status(http.StatusForbidden)
		return
	}
	protocols := websocket.Subprotocols(r)
	if len(protocols) != 2 || protocols[0] != terminalProtocol {
		c.Status(http.StatusUnauthorized)
		return
	}
	token, ok := strings.CutPrefix(protocols[1], "ticket.")
	if !ok || len(token) != 64 {
		c.Status(http.StatusUnauthorized)
		return
	}
	if _, err := hex.DecodeString(token); err != nil {
		c.Status(http.StatusUnauthorized)
		return
	}
	ticketCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	value, err := rt.Redis.Eval(ticketCtx, takeTerminalTicket, []string{terminalTicketKey + token}).Text()
	cancel()
	var ticket terminalTicket
	if err != nil || json.Unmarshal([]byte(value), &ticket) != nil || !strings.EqualFold(ticket.Origin, sshTerminalRequestOrigin(r)) {
		c.Status(http.StatusUnauthorized)
		return
	}
	upgrader := websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second,
		CheckOrigin:      sshTerminalOrigin,
		Subprotocols:     []string{terminalProtocol},
		// Gin's Hijack bypasses WriteHeader; preserve the status for middleware.
		Error: func(_ http.ResponseWriter, _ *http.Request, status int, _ error) { c.Status(status) },
	}
	conn, err := upgrader.Upgrade(c.Writer, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	output := &terminalSocketWriter{conn: conn}
	release, ok := rt.terminalSlots.acquire(ticket.Actor)
	if !ok {
		output.status("error", "Console session limit reached")
		return
	}
	defer release()
	if ticket.Protocol == remoteaccess.ProtocolRDP && rt.rdpConsoleEnabled() {
		rt.serveRDPConsole(r.Context(), ticket, output)
		return
	}
	if !rt.sshTerminalEnabled() || (ticket.Protocol != "" && ticket.Protocol != remoteaccess.ProtocolSSH) {
		output.status("error", "Console protocol is unavailable")
		return
	}
	rt.serveSSHTerminal(r.Context(), ticket, output)
}

type terminalSocketWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *terminalSocketWriter) Write(p []byte) (int, error) {
	if err := w.message(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *terminalSocketWriter) message(kind int, p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	return w.conn.WriteMessage(kind, p)
}

func (w *terminalSocketWriter) status(kind, message string) error {
	p, _ := json.Marshal(map[string]string{"type": kind, "message": message})
	return w.message(websocket.TextMessage, p)
}

func (rt *Router) serveSSHTerminal(parent context.Context, ticket terminalTicket, output *terminalSocketWriter) {
	ctx, cancel := context.WithTimeout(parent, terminalLifetime)
	defer cancel()
	conn := output.conn
	conn.SetReadLimit(16 << 10)
	term, err := rt.RemoteAccess.OpenTerminal(ctx, ticket.Actor, ticket.Ident, output)
	if err != nil {
		message := "SSH terminal could not open"
		if e, ok := err.(*remoteaccess.Error); ok {
			message = e.Message
		}
		output.status("error", message)
		return
	}
	defer term.Close()
	if output.status("ready", "") != nil {
		return
	}
	defer func() {
		conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	}()

	sshDone := make(chan struct{})
	go func() {
		defer close(sshDone)
		term.Wait()
	}()
	var lastInput atomic.Int64
	lastInput.Store(time.Now().UnixNano())
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		terminalReadLoop(conn, term, &lastInput)
	}()
	ticker := time.NewTicker(terminalPingEvery)
	defer ticker.Stop()
	for {
		select {
		case <-readDone:
			return
		case <-sshDone:
			output.status("closed", "SSH session ended")
			return
		case <-ctx.Done():
			output.status("closed", "SSH terminal session expired")
			return
		case now := <-ticker.C:
			if now.Sub(time.Unix(0, lastInput.Load())) >= terminalIdleTime {
				output.status("closed", "SSH terminal closed due to inactivity")
				return
			}
			checkCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			err := term.CheckAccess(checkCtx)
			stop()
			if err != nil {
				output.status("closed", "SSH terminal access or connection changed")
				return
			}
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return
			}
		}
	}
}

func terminalReadLoop(conn *websocket.Conn, term *remoteaccess.Terminal, lastInput *atomic.Int64) {
	const pongTimeout = 3 * terminalPingEvery
	conn.SetReadDeadline(time.Now().Add(pongTimeout))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongTimeout)) })
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		switch kind {
		case websocket.BinaryMessage:
			lastInput.Store(time.Now().UnixNano())
			if _, err := term.Write(data); err != nil {
				return
			}
		case websocket.TextMessage:
			var resize struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if json.Unmarshal(data, &resize) != nil || resize.Type != "resize" ||
				resize.Cols < 2 || resize.Cols > 500 || resize.Rows < 1 || resize.Rows > 250 {
				return
			}
			if err := term.Resize(resize.Cols, resize.Rows); err != nil {
				return
			}
		}
	}
}
