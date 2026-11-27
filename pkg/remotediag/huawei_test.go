package remotediag

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// This fixture accepts a PTY and interactive shell only. It records exact
// requests and responds with a VRP transcript without executing commands.
func huaweiSSH(t *testing.T, dialog func(ssh.Channel)) (endpoint, <-chan string) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := ssh.NewSignerFromKey(private)
	require.NoError(t, err)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
		if c.User() != "diagnose" || string(p) != "test-password" {
			return nil, errors.New("denied")
		}
		return nil, nil
	}}
	cfg.AddHostKey(key)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	requestsSeen := make(chan string, 8)
	serverDone := make(chan struct{})
	t.Cleanup(func() {
		listener.Close()
		select {
		case <-serverDone:
		case <-time.After(time.Second):
			t.Error("fixture session did not close")
		}
	})
	go func() {
		defer close(serverDone)
		defer close(requestsSeen)
		raw, err := listener.Accept()
		if err != nil {
			return
		}
		defer raw.Close()
		_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
		conn, channels, requests, err := ssh.NewServerConn(raw, cfg)
		if err != nil {
			return
		}
		defer conn.Close()
		go ssh.DiscardRequests(requests)
		for incoming := range channels {
			if incoming.ChannelType() != "session" {
				incoming.Reject(ssh.Prohibited, "fixture accepts only a session")
				continue
			}
			channel, requests, err := incoming.Accept()
			if err != nil {
				return
			}
			defer channel.Close()
			pty := false
			for request := range requests {
				requestsSeen <- request.Type
				switch request.Type {
				case "pty-req":
					pty = true
					request.Reply(true, nil)
				case "shell":
					request.Reply(pty, nil)
					if pty {
						dialog(channel)
					}
					return
				default:
					request.Reply(false, nil)
				}
			}
		}
	}()
	return endpoint{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Username: "diagnose", Password: "test-password", Fingerprint: ssh.FingerprintSHA256(key.PublicKey())}, requestsSeen
}

func TestHuaweiPortViaInteractiveSSH(t *testing.T) {
	for _, viaJump := range []bool{false, true} {
		t.Run(fmt.Sprint(viaJump), func(t *testing.T) {
			commands := make(chan string, 2)
			endpoint, requests := huaweiSSH(t, func(ch ssh.Channel) {
				input := bufio.NewReader(ch)
				io.WriteString(ch, "Notice: read-only fixture\r\n<Core-")
				io.WriteString(ch, "sw>")
				cmd, err := input.ReadString('\r')
				if err != nil {
					return
				}
				commands <- cmd
				io.WriteString(ch, strings.TrimSuffix(cmd, "\r")+"\r\nInterface PHY Protocol\r\nGE0/0/1 up up\r\n  ---- More ----")
				b, err := input.ReadByte()
				if err != nil || b != ' ' {
					return
				}
				io.WriteString(ch, "\x1b[2K\r\nGE0/0/2 up up\r\n<Core-sw>")
				cmd, err = input.ReadString('\r')
				if err != nil {
					return
				}
				commands <- cmd
				io.WriteString(ch, strings.TrimSuffix(cmd, "\r")+"\r\n#\r\ninterface GigabitEthernet0/0/2\r\n description #office#\r\n port default vlan 11\r\n stp edged-port enable\r\n#\r\nreturn\r\n<Core-sw>")
				io.Copy(io.Discard, input) // Remain interactive until the client closes.
			})
			c := connection{Ident: "switch-1", GroupID: 7, Target: endpoint}
			if viaJump {
				jump := diagnosticSSH(t, "", 0, false, nil)
				c.Jump = &jump
			}
			r := New(Config{Enable: true, ConnectionsFile: connectionFile(t, c), TimeoutSeconds: 3})
			result, err := r.Run(context.Background(), Request{Ident: c.Ident, Check: huaweiInterfaceCheck, Interface: "2"}, func(group int64) error { require.Equal(t, int64(7), group); return nil })
			require.NoError(t, err)
			require.Equal(t, 0, result.ExitCode)
			require.Equal(t, viaJump, result.ViaJump)
			require.False(t, result.Incomplete)
			require.Equal(t, "GigabitEthernet0/0/2", result.Interface)
			require.Contains(t, result.Stdout, " port default vlan 11")
			require.NotContains(t, result.Stdout, "display current-configuration")
			require.NotContains(t, result.Stdout, "<Core-sw>")
			require.Len(t, commands, 2)
			require.Equal(t, interfaceListCommand+"\r", <-commands)
			require.Equal(t, interfaceConfigCommand+"GigabitEthernet 0/0/2\r", <-commands)
			require.Equal(t, "pty-req", <-requests)
			require.Equal(t, "shell", <-requests)
		})
	}
}

func TestHuaweiPortAmbiguousStopsBeforeReadingConfiguration(t *testing.T) {
	endpoint, _ := huaweiSSH(t, func(ch ssh.Channel) {
		io.WriteString(ch, "<Core-sw>")
		input := bufio.NewReader(ch)
		cmd, err := input.ReadString('\r')
		if err != nil || cmd != interfaceListCommand+"\r" {
			return
		}
		io.WriteString(ch, "GE0/0/2 up up\r\nGigabitEthernet0/0/2 up up\r\nGE1/0/2 down down\r\n<Core-sw>")
		if _, err := input.ReadString('\r'); err == nil {
			t.Error("ambiguous interface triggered another command")
		}
	})
	c := connection{Ident: "switch-1", GroupID: 7, Target: endpoint}
	r := New(Config{Enable: true, ConnectionsFile: connectionFile(t, c), TimeoutSeconds: 3})
	result, err := r.Run(context.Background(), Request{Ident: c.Ident, Check: huaweiInterfaceCheck, Interface: "2"}, func(int64) error { return nil })
	require.ErrorContains(t, err, "multiple interfaces")
	require.Equal(t, []string{"GigabitEthernet0/0/2", "GigabitEthernet1/0/2"}, result.CandidateInterfaces)
	require.True(t, result.Incomplete)
	require.Empty(t, result.Stdout)
}

func TestHuaweiRequestValidationAndPartialOutput(t *testing.T) {
	for _, value := range []string{"GigabitEthernet0/0/2\rquit", "2;quit", "Ethernet0/0/2 | include vlan", "LoopBack0"} {
		_, err := parseInterfaceSelector(Request{Interface: value})
		require.Error(t, err)
	}
	endpoint, _ := huaweiSSH(t, func(ch ssh.Channel) {
		io.WriteString(ch, "<Core-sw>")
		cmd, err := bufio.NewReader(ch).ReadString('\r')
		if err != nil || cmd != interfaceConfigCommand+"GigabitEthernet 0/0/2\r" {
			return
		}
		io.WriteString(ch, "interface GigabitEthernet0/0/2\r\n port default vlan 11\r\n")
		// Disconnect before the user-view prompt confirms a complete result.
	})
	c := connection{Ident: "switch-1", GroupID: 7, Target: endpoint}
	r := New(Config{Enable: true, ConnectionsFile: connectionFile(t, c), TimeoutSeconds: 3})
	result, err := r.Run(context.Background(), Request{Ident: c.Ident, Check: huaweiInterfaceCheck, Interface: "GigabitEthernet 0/0/2"}, func(int64) error { return nil })
	require.ErrorContains(t, err, "before the Huawei prompt returned")
	require.True(t, result.Incomplete)
	require.Equal(t, -1, result.ExitCode)
}

func TestHuaweiPolicyRejectsUnapprovedCommandsAndAcceptsExtension(t *testing.T) {
	for _, query := range []string{"system-view", "display version\rshutdown", "display version | save flash:/x", "display diagnostic-information", "display saved-configuration", "display current-configuration", "display version file flash:/x"} {
		_, _, err := prepareHuaweiQuery(Request{Check: huaweiCLICheck, Query: query}, []string{query})
		require.Error(t, err, query)
	}
	request := Request{Check: huaweiCLICheck, Query: "display transceiver interface", Interface: "GE0/0/2"}
	_, _, err := prepareHuaweiQuery(request, nil)
	require.Error(t, err)
	query, _, err := prepareHuaweiQuery(request, []string{"display transceiver interface {interface}"})
	require.NoError(t, err)
	require.Equal(t, "display transceiver interface", query.command)
	require.NotNil(t, query.selector)
}

func TestHuaweiUnifiedReadQueries(t *testing.T) {
	for _, tc := range []struct {
		name, query, command, output, want string
		log                                bool
	}{
		{"port", "display interface GE0/0/2", "display interface GE 0/0/2", "GigabitEthernet0/0/2 current state : UP\r\nSpeed : 1000, Duplex : FULL\r\n", "Duplex : FULL", false},
		{"health", "display cpu-usage", "display cpu-usage", "CPU Usage: 7%\r\n", "CPU Usage: 7%", false},
		{"logs", "display logbuffer", "display logbuffer | include %[0-9][0-9][A-Za-z0-9_-]+/[0-3]/", "Current messages : 3\r\nJan  1 2026 %%01IFNET/4/UP: informational\r\nJan  1 2026 %%01IFNET/3/ERROR: error entry\r\nJan  1 2026 %%01IFNET/2/ERROR: other error\r\n", "error entry", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual := make(chan string, 1)
			endpoint, _ := huaweiSSH(t, func(ch ssh.Channel) {
				io.WriteString(ch, "<Core-sw>")
				input := bufio.NewReader(ch)
				command, err := input.ReadString('\r')
				if err != nil {
					return
				}
				actual <- command
				io.WriteString(ch, strings.TrimSuffix(command, "\r")+"\r\n"+tc.output+"<Core-sw>")
				io.Copy(io.Discard, input)
			})
			c := connection{Ident: "switch-1", GroupID: 7, Target: endpoint}
			r := New(Config{Enable: true, ConnectionsFile: connectionFile(t, c)})
			request := Request{Ident: c.Ident, Check: huaweiCLICheck, Query: tc.query}
			if tc.log {
				request.Lines = 1
			}
			result, err := r.Run(context.Background(), request, func(int64) error { return nil })
			require.NoError(t, err)
			require.Zero(t, result.ExitCode)
			require.False(t, result.Incomplete)
			require.Contains(t, result.Stdout, tc.want)
			require.Equal(t, tc.command+"\r", <-actual)
			if tc.log {
				total := 3
				require.Equal(t, &HuaweiLogBuffer{Severity: "error", Scanned: 3, Matched: 2, Returned: 1, Limited: true, DeviceFiltered: true, CurrentMessages: &total}, result.LogBuffer)
				require.NotContains(t, result.Stdout, "informational")
			}
		})
	}
}

func TestHuaweiFilteredBufferWithNoErrorsReportsObservedScope(t *testing.T) {
	terminal := &vrpTerminal{input: io.Discard, output: strings.NewReader("Logging buffer configuration and contents : enabled\r\nCurrent messages : 512\r\nOverwritten messages : 1380\r\n<Core-sw>"), prompt: "<Core-sw>", limit: 2048}
	request := Request{Check: huaweiCLICheck, Query: "display logbuffer"}
	query, request, err := prepareHuaweiQuery(request, nil)
	require.NoError(t, err)
	result := Result{Request: request, ExitCode: -1}
	require.NoError(t, query.run(context.Background(), terminal, &result))
	require.Equal(t, 0, result.ExitCode)
	require.Empty(t, result.Stdout)
	require.Zero(t, result.LogBuffer.Matched)
	require.True(t, result.LogBuffer.DeviceFiltered)
	require.Equal(t, 512, *result.LogBuffer.CurrentMessages)
	require.Equal(t, 1380, *result.LogBuffer.OverwrittenMessages)
}
