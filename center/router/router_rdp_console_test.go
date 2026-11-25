package router

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/remoteaccess"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRDPConsoleRoutesCredentialsThroughGateway(t *testing.T) {
	gateway, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { gateway.Close() })
	received := make(chan []string, 1)
	go func() {
		conn, err := gateway.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(conn)
		selectRDP, _ := reader.ReadString(';')
		_, _ = fmt.Fprint(conn, "4.args,8.hostname,4.port,8.username,8.password,6.domain,9.cert-tofu;")
		for i := 0; i < 4; i++ {
			if _, err := reader.ReadString(';'); err != nil {
				return
			}
		}
		connect, _ := reader.ReadString(';')
		_, _ = fmt.Fprint(conn, "5.ready,4.test;4.sync,1.1;")
		keepalive, _ := reader.ReadString(';')
		input, _ := reader.ReadString(';')
		received <- []string{selectRDP, connect, keepalive, input}
	}()

	ws := openRDPConsoleFixture(t, gateway.Addr().String(), "192.0.2.20", `EXAMPLE\fixture`, "fixture-password")
	kind, body, err := ws.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.BinaryMessage, kind)
	require.Equal(t, "4.sync,1.1;", string(body))
	require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte("3.nop;")))
	require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte("3.key,2.65,1.1;")))
	select {
	case got := <-received:
		require.Equal(t, []string{
			"6.select,3.rdp;",
			"7.connect,10.192.0.2.20,4.3389,7.fixture,16.fixture-password,7.EXAMPLE,4.true;",
			"3.nop;",
			"3.key,2.65,1.1;",
		}, got)
	case <-time.After(5 * time.Second):
		t.Fatal("gateway did not receive expected browser input")
	}
}

func TestRDPConsoleLiveGateway(t *testing.T) {
	gateway, host, secret := os.Getenv("N9E_TEST_RDP_GATEWAY"), os.Getenv("N9E_TEST_RDP_HOST"), os.Getenv("N9E_TEST_RDP_PASSWORD")
	if gateway == "" || host == "" || secret == "" {
		t.Skip("requires an isolated guacd and RDP fixture")
	}
	ws := openRDPConsoleFixture(t, gateway, host, "fixture", secret)
	var stream strings.Builder
	for stream.Len() < 2<<20 {
		kind, body, err := ws.ReadMessage()
		require.NoError(t, err)
		require.NotContains(t, string(body), secret)
		require.Equal(t, websocket.BinaryMessage, kind, string(body))
		stream.Write(body)
		if strings.Contains(stream.String(), "4.sync,") && (strings.Contains(stream.String(), "3.img,") || strings.Contains(stream.String(), "3.png,")) {
			require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte("3.key,2.65,1.1;")))
			return
		}
	}
	t.Fatal("RDP desktop frames were not received")
}

func openRDPConsoleFixture(t *testing.T, gateway, host, username, password string) *websocket.Conn {
	t.Helper()
	r, rt, _, _ := collectionRouterFixture(t)
	db := models.DB(rt.Ctx)
	require.NoError(t, db.AutoMigrate(models.OpsAccessTables()...))
	require.NoError(t, db.Create(&models.Target{Ident: "rdp-fixture"}).Error)
	require.NoError(t, db.Create(&models.TargetBusiGroup{TargetIdent: "rdp-fixture", GroupId: 1}).Error)
	rt.RemoteAccess = &remoteaccess.Store{DB: db, Keys: &remoteaccess.Keyring{Active: "test", Keys: map[string]string{"test": base64.StdEncoding.EncodeToString(make([]byte, 32))}}}
	rt.Center.RemoteDiagnostics.RDPGatewayAddress = gateway
	rt.Center.RemoteDiagnostics.EnableTerminal = true
	cache := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: cache.Addr()})
	rt.Redis = client
	t.Cleanup(func() { client.Close() })
	rt.configRemoteAccess(r.Group("/api/n9e"))

	saved := collectionCall(r, "PUT", "/api/n9e/remote-access/connection?ident=rdp-fixture",
		remoteaccess.ConnectionInput{Protocol: remoteaccess.ProtocolRDP, Enabled: true, GroupID: 1, Target: remoteaccess.EndpointInput{
			Host: host, Port: 3389, NewCredential: &remoteaccess.CredentialInput{Username: username, Password: password},
		}}, "collection-admin")
	require.Equal(t, 200, saved.Code, saved.Body.String())
	require.NotContains(t, saved.Body.String(), password)
	const base = "/api/n9e/remote-access/terminal"
	require.Equal(t, 400, collectionCall(r, "POST", base+"/ticket", map[string]string{"ident": "rdp-fixture", "protocol": "ssh"}, "collection-admin").Code)
	payload := map[string]string{"ident": "rdp-fixture", "protocol": "rdp"}
	require.Equal(t, 403, collectionCall(r, "POST", base+"/ticket", payload, "collection-reader").Code)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	ticket := collectionCall(r, "POST", server.URL+base+"/ticket", payload, "collection-admin")
	require.Equal(t, 200, ticket.Code, ticket.Body.String())
	require.NotContains(t, ticket.Body.String(), password)
	var reply struct{ Dat struct{ Ticket string } }
	require.NoError(t, json.Unmarshal(ticket.Body.Bytes(), &reply))
	dialer := websocket.Dialer{Subprotocols: []string{terminalProtocol, "ticket." + reply.Dat.Ticket}}
	ws, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+base+"/socket", http.Header{"Origin": []string{server.URL}})
	require.NoError(t, err)
	t.Cleanup(func() { ws.Close() })
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(20*time.Second)))
	kind, body, err := ws.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, kind)
	require.Contains(t, string(body), `"type":"ready"`)
	return ws
}
