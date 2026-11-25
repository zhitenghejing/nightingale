package remoteaccess

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/toolkits/pkg/logger"
	"golang.org/x/crypto/ssh"
)

// Terminal runs the remote account's login shell using a saved connection.
type Terminal struct {
	session    *ssh.Session
	stdin      io.WriteCloser
	store      *Store
	actor      int64
	connection Connection
	close      func()
}

func (s *Store) terminalConnection(ctx context.Context, actor int64, ident string) (Connection, error) {
	return s.consoleConnection(ctx, actor, ident, ProtocolSSH)
}

func (s *Store) consoleConnection(ctx context.Context, actor int64, ident, protocol string) (Connection, error) {
	if _, err := Permission(s.DB.WithContext(ctx), actor, PermTerminal); err != nil {
		return Connection{}, err
	}
	return s.resolve(ctx, ident, protocol, func(group int64) error {
		return access(s.DB.WithContext(ctx), actor, group, PermTerminal, true)
	})
}

func (s *Store) CheckRDP(ctx context.Context, actor int64, ident string) error {
	_, err := s.consoleConnection(ctx, actor, ident, ProtocolRDP)
	return err
}

func (s *Store) CheckTerminal(ctx context.Context, actor int64, ident string) error {
	_, err := s.terminalConnection(ctx, actor, ident)
	return err
}

// OpenTerminal selects the destination, credentials and host keys exclusively
// from the authorized device binding. The caller must close the terminal.
func (s *Store) OpenTerminal(ctx context.Context, actor int64, ident string, output io.Writer) (*Terminal, error) {
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	connection, err := s.terminalConnection(startup, actor, ident)
	if err != nil {
		return nil, err
	}
	if err := audit(s.DB.WithContext(startup), actor, connection.GroupID, ident, "terminal_start"); err != nil {
		return nil, err
	}

	var client, jump *ssh.Client
	closeClients := func() {
		if jump != nil {
			jump.Close()
		}
		if client != nil {
			client.Close()
		}
	}
	closeTerminal := sync.OnceFunc(func() {
		closeClients()
		auditCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := audit(s.DB.WithContext(auditCtx), actor, connection.GroupID, ident, "terminal_end"); err != nil {
			logger.Errorf("cannot record SSH terminal end for operator %d", actor)
		}
	})
	opened := false
	defer func() {
		if !opened {
			closeTerminal()
		}
	}()

	if connection.Jump != nil {
		jump, err = dialPinnedSSH(startup, *connection.Jump, nil)
		if err != nil {
			return nil, invalid("SSH terminal jump connection failed: " + err.Error())
		}
		stop := context.AfterFunc(startup, func() { jump.Close() })
		defer stop()
	}
	client, err = dialPinnedSSH(startup, connection.Target, jump)
	if err != nil {
		return nil, invalid("SSH terminal connection failed: " + err.Error())
	}
	stop := context.AfterFunc(startup, func() { client.Close() })
	defer stop()
	session, err := client.NewSession()
	if err != nil {
		return nil, invalid("SSH server refused a terminal session")
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, invalid("SSH terminal input is unavailable")
	}
	session.Stdout = output
	session.Stderr = output
	if err = session.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{ssh.ECHO: 1}); err != nil {
		return nil, invalid("SSH server refused a pseudo-terminal (PTY)")
	}
	if err = session.Shell(); err != nil {
		return nil, invalid("SSH server refused an interactive shell")
	}
	if startup.Err() != nil {
		return nil, invalid("SSH terminal connection timed out")
	}
	stopLifetime := context.AfterFunc(ctx, closeTerminal)
	opened = true
	return &Terminal{session: session, stdin: stdin, store: s, actor: actor, connection: connection, close: func() {
		stopLifetime()
		closeTerminal()
	}}, nil
}

func (t *Terminal) Write(p []byte) (int, error) { return t.stdin.Write(p) }
func (t *Terminal) Resize(cols, rows int) error { return t.session.WindowChange(rows, cols) }
func (t *Terminal) Wait() error                 { return t.session.Wait() }
func (t *Terminal) Close()                      { t.close() }

func (t *Terminal) CheckAccess(ctx context.Context) error {
	current, err := t.store.terminalConnection(ctx, t.actor, t.connection.Ident)
	if err != nil {
		return err
	}
	sameJump := current.Jump == nil && t.connection.Jump == nil ||
		current.Jump != nil && t.connection.Jump != nil && *current.Jump == *t.connection.Jump
	if current.GroupID != t.connection.GroupID || current.Target != t.connection.Target || !sameJump {
		return denied()
	}
	return nil
}
