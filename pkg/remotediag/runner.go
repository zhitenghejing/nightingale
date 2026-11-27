package remotediag

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ccfos/nightingale/v6/pkg/remoteaccess"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

type Result struct {
	ExecutionID         string           `json:"execution_id"`
	Request             Request          `json:"request"`
	StartedAt           time.Time        `json:"started_at"`
	FinishedAt          time.Time        `json:"finished_at"`
	ViaJump             bool             `json:"via_jump"`
	ApprovedLogPaths    []string         `json:"approved_log_paths"`
	ExitCode            int              `json:"exit_code"`
	Stdout              string           `json:"stdout"`
	Stderr              string           `json:"stderr"`
	StdoutTruncated     bool             `json:"stdout_truncated"`
	StderrTruncated     bool             `json:"stderr_truncated"`
	Incomplete          bool             `json:"incomplete"`
	Error               string           `json:"error,omitempty"`
	Interface           string           `json:"interface,omitempty"`
	CandidateInterfaces []string         `json:"candidate_interfaces,omitempty"`
	LogBuffer           *HuaweiLogBuffer `json:"log_buffer,omitempty"`
}

type Runner struct {
	cfg   Config
	store *remoteaccess.Store
	slots chan struct{}
}

func New(cfg Config) *Runner {
	if cfg.TimeoutSeconds <= 0 || cfg.TimeoutSeconds > 120 {
		cfg.TimeoutSeconds = 30
	}
	if cfg.MaxOutputBytes <= 0 || cfg.MaxOutputBytes > 256<<10 {
		cfg.MaxOutputBytes = 32 << 10
	}
	if cfg.MaxConcurrent <= 0 || cfg.MaxConcurrent > 32 {
		cfg.MaxConcurrent = 4
	}
	return &Runner{cfg: cfg, slots: make(chan struct{}, cfg.MaxConcurrent)}
}

func NewWithStore(cfg Config, store *remoteaccess.Store) *Runner {
	r := New(cfg)
	r.store = store
	return r
}

// Run binds authorization to the configured business group before connecting.
// authorize MUST recheck the initiating user's execution permission and the
// current target/group membership. No caller-controlled host or credential.
func (r *Runner) Run(ctx context.Context, request Request, authorize func(groupID int64) error) (result Result, err error) {
	result = Result{ExecutionID: uuid.NewString(), Request: request, StartedAt: time.Now().UTC(), ExitCode: -1}
	defer func() {
		result.FinishedAt = time.Now().UTC()
		if err != nil {
			result.Error = err.Error()
			result.Incomplete = true
		}
	}()
	if r == nil || !r.cfg.Enable {
		return result, fmt.Errorf("SSH diagnostics is disabled; configure Center.RemoteDiagnostics")
	}
	if authorize == nil {
		return result, fmt.Errorf("SSH diagnostics authorization is required")
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	var c connection
	if r.store != nil {
		resolved, e := r.store.Resolve(ctx, request.Ident, authorize)
		if e != nil {
			return result, e
		}
		convert := func(e remoteaccess.Endpoint) endpoint {
			return endpoint{Host: e.Host, Port: e.Port, Fingerprint: e.Fingerprint, Username: e.Username, Password: e.Password}
		}
		c = connection{Ident: resolved.Ident, GroupID: resolved.GroupID, Target: convert(resolved.Target), LogPaths: resolved.LogPaths}
		if resolved.Jump != nil {
			j := convert(*resolved.Jump)
			c.Jump = &j
		}
	} else {
		if r.cfg.KeyFile != "" {
			return result, fmt.Errorf("SSH credential store is unavailable")
		}
		c, err = load(r.cfg.ConnectionsFile, request.Ident)
		if err != nil {
			return result, err
		}
		if err = authorize(c.GroupID); err != nil {
			return result, err
		}
	}
	result.ApprovedLogPaths = append([]string{}, c.LogPaths...)
	var cmd string
	var query huaweiQuery
	isHuawei := request.Check == huaweiCLICheck || request.Check == huaweiInterfaceCheck
	if isHuawei {
		query, result.Request, err = prepareHuaweiQuery(request, r.cfg.HuaweiReadCommands)
	} else {
		cmd, result.Request, err = command(request, c, time.Now(), r.cfg.TimeoutSeconds)
	}
	if err != nil {
		return result, err
	}
	defer func() {
		for _, secret := range []string{c.Target.Password, jumpPassword(c)} {
			if secret != "" {
				result.Stdout = strings.ReplaceAll(result.Stdout, secret, "[REDACTED]")
				result.Stderr = strings.ReplaceAll(result.Stderr, secret, "[REDACTED]")
			}
		}
	}()
	result.ViaJump = c.Jump != nil
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	default:
		return result, fmt.Errorf("SSH diagnostics is busy; retry later")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	client, cleanup, err := connect(ctx, c)
	if err != nil {
		return result, err
	}
	defer cleanup()
	stop := context.AfterFunc(ctx, cleanup)
	defer stop()
	session, err := client.NewSession()
	if err != nil {
		return result, fmt.Errorf("cannot open SSH diagnostic session")
	}
	defer session.Close()
	if isHuawei {
		err = runHuawei(ctx, session, query, r.cfg.MaxOutputBytes, &result)
		return result, err
	}
	stdout, stderr := &limitedOutput{limit: r.cfg.MaxOutputBytes}, &limitedOutput{limit: r.cfg.MaxOutputBytes}
	session.Stdout, session.Stderr = stdout, stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()
	select {
	case err = <-done:
	case <-ctx.Done():
		cleanup()
		<-done
		err = ctx.Err()
	}
	result.Stdout, result.StdoutTruncated = stdout.read()
	result.Stderr, result.StderrTruncated = stderr.read()
	result.Incomplete = result.StdoutTruncated || result.StderrTruncated
	if ctx.Err() != nil {
		return result, fmt.Errorf("SSH diagnostic cancelled or timed out; remote termination is unconfirmed")
	}
	if err == nil {
		result.ExitCode = 0
		return result, nil
	}
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		result.ExitCode = exit.ExitStatus()
		return result, fmt.Errorf("remote diagnostic exited with status %d", result.ExitCode)
	}
	return result, fmt.Errorf("SSH diagnostic connection interrupted; remote termination is unconfirmed")
}

func jumpPassword(c connection) string {
	if c.Jump != nil {
		return c.Jump.Password
	}
	return ""
}

func connect(ctx context.Context, c connection) (*ssh.Client, func(), error) {
	var clients []*ssh.Client
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			for i := len(clients) - 1; i >= 0; i-- {
				clients[i].Close()
			}
		})
	}
	dial := func(e endpoint, jump *ssh.Client) (*ssh.Client, error) {
		address := net.JoinHostPort(e.Host, fmt.Sprint(e.Port))
		var raw net.Conn
		var err error
		if jump == nil {
			raw, err = (&net.Dialer{}).DialContext(ctx, "tcp", address)
		} else {
			raw, err = jump.DialContext(ctx, "tcp", address)
		}
		if err != nil {
			return nil, fmt.Errorf("SSH destination is unreachable")
		}
		// A forwarded channel cannot set deadlines. Closing the underlying
		// connection on cancellation also interrupts its SSH handshake.
		stop := context.AfterFunc(ctx, func() { raw.Close() })
		defer stop()
		cfg := &ssh.ClientConfig{User: e.Username, Auth: []ssh.AuthMethod{ssh.Password(e.Password)},
			HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
				if subtle.ConstantTimeCompare([]byte(ssh.FingerprintSHA256(key)), []byte(e.Fingerprint)) != 1 {
					return fmt.Errorf("SSH host fingerprint mismatch")
				}
				return nil
			},
		}
		conn, channels, requests, err := ssh.NewClientConn(raw, address, cfg)
		if err != nil {
			raw.Close()
			return nil, fmt.Errorf("SSH authentication or host verification failed")
		}
		return ssh.NewClient(conn, channels, requests), nil
	}
	var jump *ssh.Client
	if c.Jump != nil {
		var err error
		jump, err = dial(*c.Jump, nil)
		if err != nil {
			return nil, cleanup, err
		}
		clients = append(clients, jump)
	}
	client, err := dial(c.Target, jump)
	if err != nil {
		cleanup()
		return nil, cleanup, err
	}
	clients = append(clients, client)
	return client, cleanup, nil
}

type limitedOutput struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if len(p) > w.limit-w.buf.Len() {
		p = p[:w.limit-w.buf.Len()]
		w.truncated = true
	}
	w.buf.Write(p)
	return n, nil // Drain excess too, so a remote writer cannot block SSH.
}

func (w *limitedOutput) read() (string, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.ToValidUTF8(w.buf.String(), "�"), w.truncated
}
