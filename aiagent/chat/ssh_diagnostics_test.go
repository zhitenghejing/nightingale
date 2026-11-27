package chat_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/aiagent/chat"
	"github.com/ccfos/nightingale/v6/aiagent/llm"
	_ "github.com/ccfos/nightingale/v6/aiagent/tools"
	"github.com/ccfos/nightingale/v6/models"
	ctxpkg "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/ccfos/nightingale/v6/pkg/remotediag"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// This model fixture makes capability claims solely from the actual initial
// tool table. It then exercises the real handler to distinguish an absent
// capability from a connection that has not been provisioned. No skill has
// been loaded and no conversation history is supplied.
type sshCapabilityLLM struct {
	sawTool         bool
	sawUnconfigured bool
}

func (m *sshCapabilityLLM) Name() string { return "ssh-capability-fixture" }
func (m *sshCapabilityLLM) Generate(_ context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	for _, message := range req.Messages {
		if message.Role == llm.RoleTool && message.ToolName == "run_target_diagnostic" {
			m.sawUnconfigured = strings.Contains(message.Content, "not configured for this target")
			return &llm.GenerateResponse{Content: "SSH diagnostic tool exists; target connection is not configured.", FinishReason: "stop"}, nil
		}
	}
	for _, tool := range req.Tools {
		if tool.Name == "run_target_diagnostic" {
			m.sawTool = true
		}
	}
	if !m.sawTool {
		return &llm.GenerateResponse{Content: "没有 SSH 通道", FinishReason: "stop"}, nil
	}
	return &llm.GenerateResponse{ToolCalls: []llm.ToolCall{{ID: "ssh-check", Name: "run_target_diagnostic", Arguments: `{"ident":"web-01","check":"service","service":"nginx.service"}`}}, FinishReason: "tool_calls"}, nil
}
func (m *sshCapabilityLLM) GenerateStream(ctx context.Context, req *llm.GenerateRequest) (<-chan llm.StreamChunk, error) {
	response, err := m.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Content: response.Content, ToolCalls: response.ToolCalls, FinishReason: response.FinishReason, Done: true}
	close(out)
	return out, nil
}

func TestGeneralChatSSHConnectionMissingDoesNotHideCapability(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Target{}, &models.TargetBusiGroup{}, &models.BusiGroup{}))
	require.NoError(t, db.Create(&models.User{Id: 1, Username: "root", Roles: models.AdminRole}).Error)
	require.NoError(t, db.Create(&models.Target{Ident: "web-01"}).Error)
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"connections":[]}`), 0600))
	handler, ok := chat.Lookup("general_chat")
	require.True(t, ok)
	request := &chat.AIChatRequest{UserInput: "通过 SSH 查看 web-01 的 nginx 服务状态"}
	require.Empty(t, handler.RequiredSkills(request))
	fake := &sshCapabilityLLM{}
	agent := aiagent.NewAgent(&aiagent.AgentConfig{MaxIterations: 3, Timeout: 5000, UserPromptRendered: handler.BuildPrompt(request), Tools: aiagent.GetBuiltinToolDefs(handler.SelectTools(request))},
		aiagent.WithLLMClient(fake), aiagent.WithToolDeps(&aiagent.ToolDeps{DBCtx: ctxpkg.NewContext(context.Background(), db, true), RemoteDiagnostics: remotediag.New(remotediag.Config{Enable: true, ConnectionsFile: path})}))
	response, err := agent.Run(context.Background(), &aiagent.AgentRequest{UserMessage: request.UserInput, Params: map[string]string{"user_id": "1", "chat_id": "fresh-ssh-chat"}})
	require.NoError(t, err)
	require.NotContains(t, response.Content, "没有 SSH 通道", "A fresh chat must discover SSH without first loading a skill")
	require.True(t, fake.sawTool, "SSH diagnostic definition must reach the first model request")
	require.True(t, fake.sawUnconfigured, "the real tool must report missing connection, not missing capability")
	require.Len(t, response.Steps, 1)
	require.Equal(t, "run_target_diagnostic", response.Steps[0].Action)
}
