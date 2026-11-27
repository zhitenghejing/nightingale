package remoteaccess

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func keyServer(t *testing.T, forward bool) (EndpointInput, *atomic.Int32) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := ssh.NewSignerFromKey(private)
	require.NoError(t, err)
	calls := &atomic.Int32{}
	cfg := &ssh.ServerConfig{PasswordCallback: func(m ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		calls.Add(1)
		if m.User() != "diagnostic-user" || string(p) != credentialInput().Password {
			return nil, fmt.Errorf("denied")
		}
		return nil, nil
	}}
	cfg.AddHostKey(key)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			raw, e := listener.Accept()
			if e != nil {
				return
			}
			go func() {
				defer raw.Close()
				conn, channels, requests, e := ssh.NewServerConn(raw, cfg)
				if e != nil {
					return
				}
				defer conn.Close()
				go ssh.DiscardRequests(requests)
				for ch := range channels {
					if !forward || ch.ChannelType() != "direct-tcpip" {
						ch.Reject(ssh.Prohibited, "no commands allowed")
						continue
					}
					var destination struct {
						Host       string
						Port       uint32
						Origin     string
						OriginPort uint32
					}
					if ssh.Unmarshal(ch.ExtraData(), &destination) != nil {
						ch.Reject(ssh.Prohibited, "invalid")
						continue
					}
					remote, e := net.DialTimeout("tcp", net.JoinHostPort(destination.Host, fmt.Sprint(destination.Port)), time.Second)
					if e != nil {
						ch.Reject(ssh.ConnectionFailed, "unreachable")
						continue
					}
					channel, reqs, e := ch.Accept()
					if e != nil {
						remote.Close()
						continue
					}
					go ssh.DiscardRequests(reqs)
					go func() {
						defer remote.Close()
						defer channel.Close()
						go func() { io.Copy(channel, remote); channel.Close() }()
						io.Copy(remote, channel)
					}()
				}
			}()
		}
	}()
	return EndpointInput{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Fingerprint: ssh.FingerprintSHA256(key.PublicKey())}, calls
}
func TestHostKeyProbeStopsBeforeAuthenticationAndDoesNotSave(t *testing.T) {
	s := fixture(t)
	endpoint, auth := keyServer(t, false)
	f := HostKeyInput{Ident: "web-01", Endpoint: "target", Connection: ConnectionInput{GroupID: 1, Target: endpoint}}
	endpoint.Fingerprint = ""
	f.Connection.Target = endpoint
	_, err := s.ProbeHostKey(context.Background(), 2, f)
	require.ErrorContains(t, err, "denied")
	key, err := s.ProbeHostKey(context.Background(), 1, f)
	require.NoError(t, err)
	require.NotEmpty(t, key.Fingerprint)
	require.Equal(t, "ssh-ed25519", key.Algorithm)
	require.Zero(t, auth.Load())
	c, err := s.GetConnection(context.Background(), 1, "web-01")
	require.NoError(t, err)
	require.False(t, c.Configured)
	f.Connection.GroupID = 2
	_, err = s.ProbeHostKey(context.Background(), 1, f)
	require.ErrorContains(t, err, "denied")
}
func TestHostKeyProbeViaConfirmedJump(t *testing.T) {
	s := fixture(t)
	target, targetAuth := keyServer(t, false)
	jump, jumpAuth := keyServer(t, true)
	c, err := s.SaveCredential(context.Background(), 1, "", credentialInput())
	require.NoError(t, err)
	jump.CredentialID = c.ID
	f := HostKeyInput{Endpoint: "target", Connection: ConnectionInput{GroupID: 1, Target: target, Jump: &jump}}
	correct := jump.Fingerprint
	jump.Fingerprint = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	_, err = s.ProbeHostKey(context.Background(), 1, f)
	require.ErrorContains(t, err, "host-key verification failed")
	require.Zero(t, jumpAuth.Load())
	jump.Fingerprint = correct
	key, err := s.ProbeHostKey(context.Background(), 1, f)
	require.NoError(t, err)
	require.Equal(t, target.Fingerprint, key.Fingerprint)
	require.Equal(t, int32(1), jumpAuth.Load())
	require.Zero(t, targetAuth.Load())
	// Inline jump credentials are used transiently, without creating a record.
	jump.CredentialID = ""
	input := credentialInput()
	jump.NewCredential = &input
	_, err = s.ProbeHostKey(context.Background(), 1, f)
	require.NoError(t, err)
	rows, err := s.Credentials(context.Background(), 1, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}
func TestHostKeyProbeTimeoutAndInvalidDestination(t *testing.T) {
	s := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.ProbeHostKey(ctx, 1, HostKeyInput{Endpoint: "target", Connection: ConnectionInput{GroupID: 1, Target: EndpointInput{Host: "127.0.0.1", Port: 22}}})
	require.Error(t, err)
	for _, host := range []string{"example.com", "0.0.0.0", "169.254.1.1"} {
		_, err = s.ProbeHostKey(context.Background(), 1, HostKeyInput{Endpoint: "target", Connection: ConnectionInput{GroupID: 1, Target: EndpointInput{Host: host, Port: 22}}})
		require.ErrorContains(t, err, "fixed IPv4/IPv6")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	go func() {
		raw, e := listener.Accept()
		if e == nil {
			defer raw.Close()
			io.Copy(io.Discard, raw)
		}
	}()
	ctx, cancel = context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err = s.ProbeHostKey(ctx, 1, HostKeyInput{Endpoint: "target", Connection: ConnectionInput{GroupID: 1, Target: EndpointInput{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port}}})
	require.ErrorContains(t, err, "timed out")
}

func TestConnectionCheckAuthenticatesWithoutSavingOrExecuting(t *testing.T) {
	s := fixture(t)
	target, targetAuth := keyServer(t, false)
	jump, jumpAuth := keyServer(t, true)
	input := credentialInput()
	target.NewCredential = &input
	jump.NewCredential = &input
	f := ConnectionTestInput{Ident: "web-01", Connection: ConnectionInput{Enabled: true, GroupID: 1, Target: target}}
	result, err := s.TestConnection(context.Background(), 1, f)
	require.NoError(t, err)
	require.True(t, result.OK)
	require.False(t, result.ViaJump)
	require.Equal(t, int32(1), targetAuth.Load())
	f.Connection.Jump = &jump
	result, err = s.TestConnection(context.Background(), 1, f)
	require.NoError(t, err)
	require.True(t, result.ViaJump)
	require.Equal(t, int32(1), jumpAuth.Load())
	require.Equal(t, int32(2), targetAuth.Load())
	rows, err := s.Credentials(context.Background(), 1, 1)
	require.NoError(t, err)
	require.Empty(t, rows)
	connection, err := s.GetConnection(context.Background(), 1, "web-01")
	require.NoError(t, err)
	require.False(t, connection.Configured)
	f.Connection.Jump = nil
	f.Connection.Target.Fingerprint = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	_, err = s.TestConnection(context.Background(), 1, f)
	require.Error(t, err)
	require.Equal(t, int32(2), targetAuth.Load())
	f.Connection.Target = target
	bad := credentialInput()
	bad.Password = "wrong-secret-test"
	f.Connection.Target.NewCredential = &bad
	_, err = s.TestConnection(context.Background(), 1, f)
	require.ErrorContains(t, err, "authentication")
	require.NotContains(t, err.Error(), bad.Password)
	_, err = s.TestConnection(context.Background(), 2, f)
	require.ErrorContains(t, err, "denied")
}
