package router

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/remoteaccess"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestSSHTerminalOneTimeTicketAndPTY(t *testing.T) {
	t.Run("HTTP", func(t *testing.T) { testSSHTerminalOneTimeTicketAndPTY(t, httptest.NewServer) })
	t.Run("HTTPS", func(t *testing.T) { testSSHTerminalOneTimeTicketAndPTY(t, httptest.NewTLSServer) })
}

func testSSHTerminalOneTimeTicketAndPTY(t *testing.T, serve func(http.Handler) *httptest.Server) {
	t.Helper()
	endpoint, resized, disconnected := terminalSSHFixture(t)
	r, rt, _, _ := collectionRouterFixture(t)
	db := models.DB(rt.Ctx)
	require.NoError(t, db.AutoMigrate(models.OpsAccessTables()...))
	require.NoError(t, db.Create(&models.Target{Ident: "terminal-fixture"}).Error)
	require.NoError(t, db.Create(&models.TargetBusiGroup{TargetIdent: "terminal-fixture", GroupId: 1}).Error)
	rt.RemoteAccess = &remoteaccess.Store{DB: db, Keys: &remoteaccess.Keyring{Active: "test", Keys: map[string]string{"test": base64.StdEncoding.EncodeToString(make([]byte, 32))}}}
	rt.Center.RemoteDiagnostics.EnableTerminal = true
	cache := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	t.Cleanup(func() { client.Close() })
	rt.Redis = client
	ctx := context.Background()
	credential, err := rt.RemoteAccess.SaveCredential(ctx, 1, "", remoteaccess.CredentialInput{
		Name: "terminal test", GroupID: 1, Username: "fixture-user", Password: "fixture-password",
	})
	require.NoError(t, err)
	endpoint.CredentialID = credential.ID
	_, err = rt.RemoteAccess.SaveConnection(ctx, 1, "terminal-fixture", remoteaccess.ConnectionInput{Enabled: true, GroupID: 1, Target: endpoint})
	require.NoError(t, err)
	rt.configRemoteAccess(r.Group("/api/n9e"))
	const base = "/api/n9e/remote-access/terminal"
	requestTicket := func(user string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://preview.example"+base+"/ticket", strings.NewReader(`{"ident":"terminal-fixture"}`))
		req.Header.Set("X-Collection-Test-User", user)
		result := httptest.NewRecorder()
		r.ServeHTTP(result, req)
		return result
	}
	require.Equal(t, 401, requestTicket("").Code)
	require.Equal(t, 403, requestTicket("collection-reader").Code)
	require.NoError(t, db.Create(&models.RoleOperation{RoleName: "CollectionReader", Operation: remoteaccess.PermTerminal}).Error)
	require.Equal(t, 403, requestTicket("collection-reader").Code, "group read permission cannot open a terminal")
	require.NoError(t, db.Model(&models.BusiGroupMember{}).Where("user_group_id = ?", 10).Update("perm_flag", "rw").Error)

	server := serve(r)
	t.Cleanup(server.Close)
	tokenFor := func(user string) string {
		req, err := http.NewRequest("POST", server.URL+base+"/ticket", strings.NewReader(`{"ident":"terminal-fixture"}`))
		require.NoError(t, err)
		req.Header.Set("X-Collection-Test-User", user)
		resp, err := server.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode, string(raw))
		require.NotContains(t, string(raw), "fixture-password")
		var envelope struct{ Dat struct{ Ticket string } }
		require.NoError(t, json.Unmarshal(raw, &envelope))
		require.Len(t, envelope.Dat.Ticket, 64)
		return envelope.Dat.Ticket
	}
	dial := func(token, origin string) (*websocket.Conn, *http.Response, error) {
		transport := server.Client().Transport.(*http.Transport)
		dialer := websocket.Dialer{
			HandshakeTimeout: 3 * time.Second,
			TLSClientConfig:  transport.TLSClientConfig.Clone(),
			Subprotocols:     []string{terminalProtocol, "ticket." + token},
		}
		return dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+base+"/socket", http.Header{"Origin": []string{origin}})
	}

	token := tokenFor("collection-reader")
	_, denied, err := dial(token, "https://unrelated.example")
	require.Error(t, err)
	denied.Body.Close()
	require.Equal(t, 403, denied.StatusCode)
	ws, response, err := dial(token, server.URL)
	require.NoError(t, err)
	defer ws.Close()
	require.Equal(t, 101, response.StatusCode)
	require.Equal(t, terminalProtocol, ws.Subprotocol())
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(3*time.Second)))
	for {
		kind, body, err := ws.ReadMessage()
		require.NoError(t, err)
		if kind == websocket.TextMessage {
			require.JSONEq(t, `{"type":"ready","message":""}`, string(body))
			break
		}
	}
	require.NoError(t, ws.WriteJSON(map[string]interface{}{"type": "resize", "cols": 108, "rows": 36}))
	select {
	case size := <-resized:
		require.Equal(t, [2]uint32{108, 36}, size)
	case <-time.After(3 * time.Second):
		t.Fatal("SSH PTY size did not change")
	}
	input := []byte("terminal fixture\r\x03")
	require.NoError(t, ws.WriteMessage(websocket.BinaryMessage, input))
	var echoed bytes.Buffer
	for echoed.Len() < len(input) {
		kind, output, err := ws.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, websocket.BinaryMessage, kind)
		echoed.Write(output)
	}
	require.Equal(t, input, echoed.Bytes())

	_, denied, err = dial(token, server.URL)
	require.Error(t, err)
	denied.Body.Close()
	require.Equal(t, 401, denied.StatusCode)

	expired := tokenFor("collection-reader")
	cache.FastForward(terminalTicketTTL + time.Second)
	_, denied, err = dial(expired, server.URL)
	require.Error(t, err)
	denied.Body.Close()
	require.Equal(t, 401, denied.StatusCode)

	require.NoError(t, ws.Close())
	select {
	case <-disconnected:
	case <-time.After(3 * time.Second):
		t.Fatal("closing the web terminal did not disconnect SSH")
	}
	require.Eventually(t, func() bool {
		var count int64
		err := db.Model(&models.OpsAccessAudit{}).Where("actor_id = ? AND object_id = ? AND action IN ?", 2, "terminal-fixture", []string{"terminal_start", "terminal_end"}).Count(&count).Error
		return err == nil && count == 2
	}, 3*time.Second, 15*time.Millisecond)
}

func terminalSSHFixture(t *testing.T) (remoteaccess.EndpointInput, <-chan [2]uint32, <-chan struct{}) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(key)
	require.NoError(t, err)
	config := &ssh.ServerConfig{PasswordCallback: func(m ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if m.User() != "fixture-user" || string(password) != "fixture-password" {
			return nil, fmt.Errorf("unexpected SSH credentials")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })
	resized := make(chan [2]uint32, 1)
	disconnected := make(chan struct{})
	go func() {
		raw, err := listener.Accept()
		if err != nil {
			return
		}
		defer close(disconnected)
		defer raw.Close()
		conn, channels, requests, err := ssh.NewServerConn(raw, config)
		if err != nil {
			return
		}
		defer conn.Close()
		go ssh.DiscardRequests(requests)
		for channel := range channels {
			if channel.ChannelType() != "session" {
				channel.Reject(ssh.UnknownChannelType, "expected session")
				continue
			}
			session, requests, err := channel.Accept()
			if err != nil {
				return
			}
			pty := false
			for req := range requests {
				switch req.Type {
				case "pty-req":
					pty = true
					req.Reply(true, nil)
				case "shell":
					req.Reply(pty, nil)
					if pty {
						go io.Copy(session, session)
					}
				case "window-change":
					var size struct{ Cols, Rows, Width, Height uint32 }
					if ssh.Unmarshal(req.Payload, &size) == nil {
						resized <- [2]uint32{size.Cols, size.Rows}
					}
				default:
					req.Reply(false, nil)
				}
			}
		}
	}()
	return remoteaccess.EndpointInput{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Fingerprint: ssh.FingerprintSHA256(signer.PublicKey())}, resized, disconnected
}

func TestSSHTerminalOrigin(t *testing.T) {
	for _, tc := range []struct {
		scheme, peer, forwarded, origin string
		allowed                         bool
	}{
		{"http", "192.0.2.1:12345", "", "http://preview.example", true},
		{"https", "192.0.2.1:12345", "", "https://preview.example", true},
		{"https", "192.0.2.1:12345", "", "http://preview.example", false},
		{"http", "127.0.0.1:12345", "https", "https://preview.example", true},
		{"http", "127.0.0.1:12345", "https", "http://preview.example", false},
		{"http", "192.0.2.1:12345", "https", "https://preview.example", false},
		{"http", "192.0.2.1:12345", "https", "http://preview.example", true},
		{"http", "192.0.2.1:12345", "", "", false},
	} {
		req := httptest.NewRequest("GET", tc.scheme+"://preview.example/", nil)
		req.RemoteAddr = tc.peer
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-Forwarded-Proto", tc.forwarded)
		require.Equal(t, tc.allowed, sshTerminalOrigin(req), "%+v", tc)
	}
}
