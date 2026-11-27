package tools

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/aiagent/chat"
	"github.com/ccfos/nightingale/v6/aiagent/llm"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/remoteaccess"
	"github.com/ccfos/nightingale/v6/pkg/remotediag"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func remoteDiagnosticDeps(t *testing.T) (*aiagent.ToolDeps, string) {
	t.Helper()
	deps := newDashboardTestDeps(t)
	db := models.DB(deps.DBCtx)
	require.NoError(t, db.AutoMigrate(&models.Target{}, &models.TargetBusiGroup{}, &models.RoleOperation{}, &models.UserGroupMember{}, &models.BusiGroupMember{}))
	require.NoError(t, db.Create(&models.Target{Ident: "web-01", GroupId: 1}).Error)
	require.NoError(t, db.Create(&models.TargetBusiGroup{TargetIdent: "web-01", GroupId: 1}).Error)
	require.NoError(t, db.Create(&models.User{Id: 2, Username: "viewer", Roles: "Standard"}).Error)
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"connections":[{"ident":"web-01","group_id":1,"target":{"host":"127.0.0.1","port":1,"username":"diagnose","password":"server-secret","fingerprint":"SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}}]}`), 0600))
	deps.RemoteDiagnostics = remotediag.New(remotediag.Config{Enable: true, ConnectionsFile: path})
	return deps, path
}

func TestRemoteDiagnosticPermissionsAndGroupChanges(t *testing.T) {
	deps, _ := remoteDiagnosticDeps(t)
	args := map[string]interface{}{"ident": "web-01", "check": "unsupported"}
	params := map[string]string{"user_id": "2", "chat_id": "test"}
	_, err := runTargetDiagnostic(context.Background(), deps, args, params)
	require.ErrorContains(t, err, "no permission")
	db := models.DB(deps.DBCtx)
	require.NoError(t, db.Create(&models.RoleOperation{RoleName: "Standard", Operation: remoteaccess.PermExecute}).Error)
	_, err = runTargetDiagnostic(context.Background(), deps, args, params)
	require.ErrorContains(t, err, "no permission on targets")
	require.NoError(t, db.Create(&models.UserGroupMember{UserId: 2, GroupId: 9}).Error)
	require.NoError(t, db.Create(&models.BusiGroupMember{BusiGroupId: 1, UserGroupId: 9, PermFlag: "ro"}).Error)
	out, err := runTargetDiagnostic(context.Background(), deps, args, params)
	require.NoError(t, err)
	require.Contains(t, out, "unknown diagnostic check")
	require.NoError(t, db.Model(&models.BusiGroupMember{}).Where("user_group_id = 9").Update("perm_flag", "rw").Error)
	out, err = runTargetDiagnostic(context.Background(), deps, args, params)
	require.NoError(t, err)
	require.Contains(t, out, "unknown diagnostic check") // Passed authorization, no SSH dial.
	require.Contains(t, out, "UNTRUSTED SKILL OUTPUT")
	require.NotContains(t, out, "server-secret")
	// This admin still cannot use a stale profile after the target leaves its group.
	require.NoError(t, db.Where("target_ident = ?", "web-01").Delete(&models.TargetBusiGroup{}).Error)
	out, err = runTargetDiagnostic(context.Background(), deps, args, map[string]string{"user_id": "1"})
	require.NoError(t, err)
	require.Contains(t, out, "no longer belongs")
	require.NoError(t, db.Model(&models.User{}).Where("id = 1").Update("disabled", models.UserDisabled).Error)
	_, err = runTargetDiagnostic(context.Background(), deps, args, map[string]string{"user_id": "1"})
	require.ErrorContains(t, err, "disabled")
}

func TestRemoteDiagnosticRejectsModelOverridesAndMissingTargets(t *testing.T) {
	deps, _ := remoteDiagnosticDeps(t)
	params := map[string]string{"user_id": "1"}
	for _, field := range []string{"command", "host", "password", "group_id", "confirmed", "proposal_id"} {
		_, err := runTargetDiagnostic(context.Background(), deps, map[string]interface{}{"ident": "web-01", "check": "overview", field: "override"}, params)
		require.ErrorContains(t, err, "unsupported diagnostic parameter")
	}
	_, err := runTargetDiagnostic(context.Background(), deps, map[string]interface{}{"ident": "web-01", "check": "journal", "lines": "100"}, params)
	require.ErrorContains(t, err, "parameter types")
	_, err = runTargetDiagnostic(context.Background(), deps, map[string]interface{}{"ident": "unknown", "check": "overview"}, params)
	require.ErrorContains(t, err, "target not found")
	deps.RemoteDiagnostics = nil
	out, err := runTargetDiagnostic(context.Background(), deps, map[string]interface{}{"ident": "web-01", "check": "overview"}, params)
	require.NoError(t, err)
	require.Contains(t, out, "disabled")
	require.Contains(t, out, `"incomplete":true`)
}

type linuxQueryModel struct{}

func (linuxQueryModel) Name() string { return "linux-ssh-query-fixture" }

func (linuxQueryModel) Generate(_ context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	for _, message := range req.Messages {
		if message.Role == llm.RoleTool && message.ToolName == "run_target_diagnostic" {
			if !strings.Contains(message.Content, "Fixture Test CPU 3140") {
				return nil, fmt.Errorf("device CPU model missing from the tool observation")
			}
			return &llm.GenerateResponse{Content: "CPU 型号是 Fixture Test CPU 3140。", FinishReason: "stop"}, nil
		}
	}
	for _, tool := range req.Tools {
		if tool.Name == "run_target_diagnostic" {
			return &llm.GenerateResponse{ToolCalls: []llm.ToolCall{{
				ID: "cpu-query", Name: tool.Name, Arguments: `{"ident":"web-01","check":"linux_cli","query":"lscpu | grep \"Model name\""}`,
			}}, FinishReason: "tool_calls"}, nil
		}
	}
	return nil, fmt.Errorf("SSH tool is missing")
}

func (linuxQueryModel) GenerateStream(context.Context, *llm.GenerateRequest) (<-chan llm.StreamChunk, error) {
	return nil, fmt.Errorf("unexpected streaming request")
}

func TestRemoteDiagnosticModelQueryOverSSH(t *testing.T) {
	deps, _ := remoteDiagnosticDeps(t)
	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := ssh.NewSignerFromKey(private)
	require.NoError(t, err)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if c.User() != "diagnose" || string(password) != "server-secret" {
			return nil, fmt.Errorf("denied")
		}
		return nil, nil
	}}
	cfg.AddHostKey(key)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	commands := make(chan string, 1)
	go func() {
		raw, err := ln.Accept()
		if err != nil {
			return
		}
		defer raw.Close()
		conn, channels, requests, err := ssh.NewServerConn(raw, cfg)
		if err != nil {
			return
		}
		defer conn.Close()
		go ssh.DiscardRequests(requests)
		for c := range channels {
			if c.ChannelType() != "session" {
				c.Reject(ssh.UnknownChannelType, "unsupported")
				continue
			}
			ch, reqs, err := c.Accept()
			if err != nil {
				return
			}
			defer ch.Close()
			for req := range reqs {
				if req.Type != "exec" {
					req.Reply(false, nil)
					continue
				}
				var payload struct{ Command string }
				if ssh.Unmarshal(req.Payload, &payload) != nil {
					return
				}
				commands <- payload.Command
				req.Reply(true, nil)
				io.WriteString(ch, "Model name: Fixture Test CPU 3140\n")
				ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				return
			}
		}
	}()
	db := models.DB(deps.DBCtx)
	require.NoError(t, db.AutoMigrate(models.OpsAccessTables()...))
	store := &remoteaccess.Store{DB: db, Keys: &remoteaccess.Keyring{Active: "test", Keys: map[string]string{"test": base64.StdEncoding.EncodeToString(make([]byte, 32))}}}
	_, err = store.SaveConnection(context.Background(), 1, "web-01", remoteaccess.ConnectionInput{Enabled: true, GroupID: 1, Target: remoteaccess.EndpointInput{
		Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Fingerprint: ssh.FingerprintSHA256(key.PublicKey()),
		NewCredential: &remoteaccess.CredentialInput{Username: "diagnose", Password: "server-secret"},
	}})
	require.NoError(t, err)
	deps.RemoteDiagnostics = remotediag.NewWithStore(remotediag.Config{Enable: true}, store)
	handler, ok := chat.Lookup("general_chat")
	require.True(t, ok)
	request := &chat.AIChatRequest{UserInput: "web-01 的 CPU 是什么型号？"}
	agent := aiagent.NewAgent(&aiagent.AgentConfig{
		MaxIterations: 3, Timeout: 5000, UserPromptRendered: handler.BuildPrompt(request), Tools: aiagent.GetBuiltinToolDefs(handler.SelectTools(request)),
	}, aiagent.WithLLMClient(linuxQueryModel{}), aiagent.WithToolDeps(deps))
	response, err := agent.Run(context.Background(), &aiagent.AgentRequest{UserMessage: request.UserInput, Params: map[string]string{"user_id": "1", "chat_id": "diagnostic-e2e"}})
	require.NoError(t, err)
	require.True(t, response.Success, response.Error)
	require.Equal(t, "CPU 型号是 Fixture Test CPU 3140。", response.Content)
	require.Len(t, response.Steps, 1)
	out := response.Steps[0].Observation
	require.Contains(t, out, "Model name: Fixture Test CPU 3140")
	require.NotContains(t, out, "server-secret")
	start := strings.Index(out, "--- stdout ---\n") + len("--- stdout ---\n")
	end := strings.LastIndex(out, "\n[END UNTRUSTED")
	require.Greater(t, end, start)
	var result remotediag.Result
	require.NoError(t, json.Unmarshal([]byte(out[start:end]), &result))
	require.Zero(t, result.ExitCode)
	require.False(t, result.Incomplete)
	require.Equal(t, "web-01", result.Request.Ident)
	require.NotEmpty(t, result.ExecutionID)
	require.Equal(t, `lscpu | grep "Model name"`, result.Request.Query)
	require.Contains(t, <-commands, "lscpu | grep --")
}
