package aiagent

import (
	"github.com/ccfos/nightingale/v6/aiagent/llm"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNativeReplayDropsTransportTailsButKeepsCanonicalCallsAndQuotedProse(t *testing.T) {
	calls := []llm.ToolCall{{ID: "actual", Name: "target_statistics", Arguments: `{"query":"scope-","group_by":"agent_version"}`}}
	for _, tail := range []string{"\n</invoke>\n</parameter>\n</function>\n</tool_call>", "</search_knowledge>\n<parameter=query>字段说明</parameter>\n</function>\n</tool_call>"} {
		original := []ChatMessage{{Role: llm.RoleAssistant, Content: tail, ToolCalls: calls}, {Role: llm.RoleTool, ToolCallID: "actual", Content: `{"total":2}`}}
		req := buildLLMRequest(original)
		require.Empty(t, req.Messages[0].Content)
		require.Equal(t, calls, req.Messages[0].ToolCalls)
		require.Equal(t, "actual", req.Messages[1].ToolCallID)
		require.Equal(t, tail, original[0].Content, "diagnostic transcript remains unchanged")
	}
	text := "示例 `</function>` 是结束标签；先核对资料。"
	req := buildLLMRequest([]ChatMessage{{Role: llm.RoleAssistant, Content: text, ToolCalls: calls}})
	require.Equal(t, text, req.Messages[0].Content)
}
