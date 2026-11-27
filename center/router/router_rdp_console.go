package router

import (
	"context"
	"io"
	"sync/atomic"
	"time"

	"github.com/ccfos/nightingale/v6/pkg/remoteaccess"
	"github.com/gorilla/websocket"
)

func (rt *Router) rdpConsoleEnabled() bool {
	return rt.RemoteAccess != nil && rt.Redis != nil && remoteaccess.RDPGatewayConfigured(rt.Center.RemoteDiagnostics.RDPGatewayAddress)
}

func (rt *Router) serveRDPConsole(parent context.Context, ticket terminalTicket, output *terminalSocketWriter) {
	ctx, cancel := context.WithTimeout(parent, terminalLifetime)
	defer cancel()
	conn := output.conn
	conn.SetReadLimit(16 << 10)
	term, err := rt.RemoteAccess.OpenRDP(ctx, ticket.Actor, ticket.Ident, rt.Center.RemoteDiagnostics.RDPGatewayAddress)
	if err != nil {
		message := "RDP console could not open"
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
	framesDone := make(chan struct{})
	go func() {
		defer close(framesDone)
		_, _ = io.Copy(output, term)
	}()
	inputDone := make(chan struct{})
	var lastInput atomic.Int64
	lastInput.Store(time.Now().UnixNano())
	go func() {
		defer close(inputDone)
		const pongTimeout = 3 * terminalPingEvery
		conn.SetReadDeadline(time.Now().Add(pongTimeout))
		conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongTimeout)) })
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil || kind != websocket.TextMessage || term.Send(string(data)) != nil {
				return
			}
			// Guacamole sends continuous sync frames even without an operator.
			if len(data) >= 5 && (string(data[:5]) == "3.key" || len(data) >= 7 && string(data[:7]) == "5.mouse") {
				lastInput.Store(time.Now().UnixNano())
			}
		}
	}()
	ticker := time.NewTicker(terminalPingEvery)
	defer ticker.Stop()
	for {
		select {
		case <-inputDone:
			return
		case <-framesDone:
			output.status("closed", "RDP session ended")
			return
		case <-ctx.Done():
			output.status("closed", "RDP console session expired")
			return
		case now := <-ticker.C:
			if now.Sub(time.Unix(0, lastInput.Load())) >= terminalIdleTime {
				output.status("closed", "RDP console closed due to inactivity")
				return
			}
			check, stop := context.WithTimeout(ctx, 5*time.Second)
			err := term.CheckAccess(check)
			stop()
			if err != nil {
				output.status("closed", "RDP console access or connection changed")
				return
			}
			if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return
			}
		}
	}
}
