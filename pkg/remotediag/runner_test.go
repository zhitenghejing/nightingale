package remotediag

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// The fixture speaks actual SSH password authentication, session exec and
// direct-tcpip forwarding. It never executes a received command on the host.
func diagnosticSSH(t *testing.T, output string, status uint32, hold bool, commands chan<- string) endpoint {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(key)
	require.NoError(t, err)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if c.User() != "diagnose" || string(password) != "secret-password" {
			return nil, fmt.Errorf("denied")
		}
		return nil, nil
	}}
	cfg.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var connections []net.Conn
	t.Cleanup(func() {
		listener.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range connections {
			c.Close()
		}
	})
	go func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, raw)
			mu.Unlock()
			go func() {
				conn, channels, requests, err := ssh.NewServerConn(raw, cfg)
				if err != nil {
					raw.Close()
					return
				}
				defer conn.Close()
				go ssh.DiscardRequests(requests)
				for ch := range channels {
					switch ch.ChannelType() {
					case "direct-tcpip":
						var dest struct {
							Host       string
							Port       uint32
							Origin     string
							OriginPort uint32
						}
						if ssh.Unmarshal(ch.ExtraData(), &dest) != nil {
							ch.Reject(ssh.ConnectionFailed, "invalid")
							continue
						}
						upstream, err := net.DialTimeout("tcp", net.JoinHostPort(dest.Host, fmt.Sprint(dest.Port)), time.Second)
						if err != nil {
							ch.Reject(ssh.ConnectionFailed, "unreachable")
							continue
						}
						channel, reqs, err := ch.Accept()
						if err != nil {
							upstream.Close()
							continue
						}
						go ssh.DiscardRequests(reqs)
						go func() { defer channel.Close(); defer upstream.Close(); io.Copy(channel, upstream) }()
						go func() { defer channel.Close(); defer upstream.Close(); io.Copy(upstream, channel) }()
					case "session":
						channel, reqs, err := ch.Accept()
						if err != nil {
							continue
						}
						go func() {
							defer channel.Close()
							for req := range reqs {
								if req.Type != "exec" {
									req.Reply(false, nil)
									continue
								}
								var payload struct{ Command string }
								if ssh.Unmarshal(req.Payload, &payload) != nil {
									req.Reply(false, nil)
									continue
								}
								req.Reply(true, nil)
								if commands != nil {
									commands <- payload.Command
								}
								if hold {
									for range reqs {
									}
									return
								}
								io.WriteString(channel, output)
								io.WriteString(channel.Stderr(), "diagnostic stderr")
								channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
								return
							}
						}()
					default:
						ch.Reject(ssh.UnknownChannelType, "unsupported")
					}
				}
			}()
		}
	}()
	addr := listener.Addr().(*net.TCPAddr)
	return endpoint{Host: "127.0.0.1", Port: addr.Port, Username: "diagnose", Password: "secret-password", Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}
}

func connectionFile(t *testing.T, c connection) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "connections.json")
	data, err := json.Marshal(map[string]interface{}{"connections": []connection{c}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	return path
}

func TestDiagnosticSSHDirectAndJump(t *testing.T) {
	for _, viaJump := range []bool{false, true} {
		t.Run(fmt.Sprint(viaJump), func(t *testing.T) {
			commands := make(chan string, 1)
			c := connection{Ident: "host-1", GroupID: 7, Target: diagnosticSSH(t, "line secret-password\n", 0, false, commands)}
			if viaJump {
				jump := diagnosticSSH(t, "", 0, false, nil)
				c.Jump = &jump
			}
			r := New(Config{Enable: true, ConnectionsFile: connectionFile(t, c)})
			result, err := r.Run(context.Background(), Request{Ident: "host-1", Check: "journal", Service: "nginx.service"}, func(group int64) error { require.Equal(t, int64(7), group); return nil })
			require.NoError(t, err)
			require.Equal(t, 0, result.ExitCode)
			require.Equal(t, viaJump, result.ViaJump)
			require.NotContains(t, result.Stdout, "secret-password")
			require.Contains(t, result.Stdout, "[REDACTED]")
			require.Contains(t, result.Stderr, "diagnostic stderr")
			require.False(t, result.Incomplete)
			require.NotEmpty(t, result.ExecutionID)
			require.Greater(t, result.Request.Until, result.Request.Since)
			cmd := <-commands
			require.Contains(t, cmd, "/usr/bin/timeout")
			require.Contains(t, cmd, "journalctl --utc --no-pager")
		})
	}
}

func TestDiagnosticRejectsBeforeExecution(t *testing.T) {
	commands := make(chan string, 8)
	e := diagnosticSSH(t, "unexpected", 0, false, commands)
	c := connection{Ident: "host-1", GroupID: 7, Target: e, LogPaths: []string{"/var/log/app.log"}}
	r := New(Config{Enable: true, ConnectionsFile: connectionFile(t, c)})
	allow := func(int64) error { return nil }
	for _, request := range []Request{
		{Ident: "missing", Check: "overview"},
		{Ident: "host-1", Check: "bash -c id"},
		{Ident: "host-1", Check: "service", Service: "nginx;touch /tmp/pwn"},
		{Ident: "host-1", Check: "service", Service: "$(id)"},
		{Ident: "host-1", Check: "service", Service: "--help"},
		{Ident: "host-1", Check: "log_tail", LogPath: "/etc/shadow"},
		{Ident: "host-1", Check: "log_tail", LogPath: "/var/log/../log/app.log"},
		{Ident: "host-1", Check: "journal", Since: 1, Until: 90000},
		{Ident: "host-1", Check: "overview", Lines: 1001},
	} {
		_, err := r.Run(context.Background(), request, allow)
		require.Error(t, err)
	}
	_, err := r.Run(context.Background(), Request{Ident: "host-1", Check: "overview"}, nil)
	require.Error(t, err)
	_, err = r.Run(context.Background(), Request{Ident: "host-1", Check: "overview"}, func(int64) error { return fmt.Errorf("denied") })
	require.ErrorContains(t, err, "denied")
	require.Empty(t, commands)
}

func TestDiagnosticHostKeyAndPasswordFailures(t *testing.T) {
	for _, stage := range []string{"target-key", "target-password", "jump-key", "jump-password"} {
		t.Run(stage, func(t *testing.T) {
			commands := make(chan string, 1)
			c := connection{Ident: "host", GroupID: 1, Target: diagnosticSSH(t, "", 0, false, commands)}
			bad := &c.Target
			if strings.HasPrefix(stage, "jump") {
				jump := diagnosticSSH(t, "", 0, false, nil)
				c.Jump = &jump
				bad = c.Jump
			}
			if strings.HasSuffix(stage, "key") {
				bad.Fingerprint = "SHA256:" + strings.Repeat("A", 43)
			} else {
				bad.Password = "wrong-password"
			}
			r := New(Config{Enable: true, ConnectionsFile: connectionFile(t, c)})
			result, err := r.Run(context.Background(), Request{Ident: "host", Check: "overview"}, func(int64) error { return nil })
			require.ErrorContains(t, err, "authentication or host verification")
			require.True(t, result.Incomplete)
			require.NotContains(t, err.Error(), bad.Password)
			require.Empty(t, commands)
		})
	}
}

func TestDiagnosticTimeoutOutputAndExit(t *testing.T) {
	for _, test := range []struct {
		name, output string
		status       uint32
		hold         bool
	}{
		{"truncated", strings.Repeat("中", 50), 0, false},
		{"failed", "no journal permission", 1, false},
		{"timeout", "", 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := connection{Ident: "host", GroupID: 1, Target: diagnosticSSH(t, test.output, test.status, test.hold, nil)}
			r := New(Config{Enable: true, ConnectionsFile: connectionFile(t, c), MaxOutputBytes: 32, TimeoutSeconds: 1})
			start := time.Now()
			result, err := r.Run(context.Background(), Request{Ident: "host", Check: "overview"}, func(int64) error { return nil })
			if test.name == "truncated" {
				require.NoError(t, err)
				require.True(t, result.StdoutTruncated)
				require.True(t, result.Incomplete)
				require.True(t, json.Valid([]byte(fmt.Sprintf("%q", result.Stdout))))
			}
			if test.name == "failed" {
				require.Error(t, err)
				require.Equal(t, 1, result.ExitCode)
				require.Contains(t, result.Stdout, "no journal permission")
			}
			if test.hold {
				require.ErrorContains(t, err, "termination is unconfirmed")
				require.Less(t, time.Since(start), 4*time.Second)
			}
		})
	}
}

func TestDiagnosticCancellationAndAdmission(t *testing.T) {
	commands := make(chan string, 1)
	c := connection{Ident: "host", GroupID: 1, Target: diagnosticSSH(t, "", 0, true, commands)}
	r := New(Config{Enable: true, ConnectionsFile: connectionFile(t, c), MaxConcurrent: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := r.Run(ctx, Request{Ident: "host", Check: "overview"}, func(int64) error { return nil })
		done <- err
	}()
	select {
	case <-commands:
	case <-time.After(5 * time.Second):
		t.Fatal("SSH session did not start")
	}
	_, err := r.Run(context.Background(), Request{Ident: "host", Check: "disk"}, func(int64) error { return nil })
	require.ErrorContains(t, err, "busy")
	cancel()
	select {
	case err = <-done:
		require.ErrorContains(t, err, "termination is unconfirmed")
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not return")
	}
}

func TestDiagnosticPrivateFileAndReload(t *testing.T) {
	c := connection{Ident: "host", GroupID: 1, Target: diagnosticSSH(t, "ok", 0, false, nil)}
	path := connectionFile(t, c)
	require.NoError(t, os.Chmod(path, 0644))
	_, err := load(path, "host")
	require.ErrorContains(t, err, "private regular")
	require.NoError(t, os.Chmod(path, 0600))
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(path, link))
	_, err = load(link, "host")
	require.Error(t, err)
	_, err = load(path, "host")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(`{"connections":[]}`), 0600))
	_, err = load(path, "host")
	require.ErrorContains(t, err, "not configured")
	require.NoError(t, os.WriteFile(path, []byte(`{"connections":[],"password":"secret"}`), 0600))
	_, err = load(path, "host")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
}
