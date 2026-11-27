package aiagent

import (
	"context"
	"github.com/ccfos/nightingale/v6/aiagent/llm"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestReadonlyProtocolOnlyReplyRepairsOnceAndKeepsCanonicalBoundary(t *testing.T) {
	executed := 0
	RegisterBuiltinTool("synthetic_read", &BuiltinTool{Definition: AgentTool{Name: "synthetic_read", Type: ToolTypeBuiltin}, Handler: func(context.Context, *ToolDeps, map[string]interface{}, map[string]string) (string, error) {
		executed++
		return `{"source":"synthetic"}`, nil
	}})
	t.Cleanup(func() { delete(builtinTools, "synthetic_read") })
	fake := &scriptedNativeLLM{rounds: [][]llm.StreamChunk{{{Content: "\u200b</function>\n</tool_call>"}}, {{ToolCalls: []llm.ToolCall{{ID: "actual", Name: "synthetic_read", Arguments: "{}"}}}}, {{Content: "现有证据不足以确认该设备。"}}}}
	a := &Agent{cfg: &AgentConfig{}, llmClient: fake}
	stream := make(chan *StreamChunk, 30)
	r := a.runNativeLoop(context.Background(), &AgentRequest{}, []ChatMessage{{Role: "user", Content: "原始问题"}}, nil, &ToolLoopConfig{MaxIterations: 5, MaxToolCalls: 4, FinishOnToolBudget: true, StreamChan: stream, EmitTranscript: true})
	close(stream)
	require.True(t, r.Success)
	require.Contains(t, r.Content, "证据不足")
	require.Equal(t, 3, fake.call)
	actual := 0
	for ch := range stream {
		if ch.Type == StreamTypeTranscript {
			for _, m := range ch.Transcript {
				require.NotEqual(t, "user", m.Role, "repair prompt is projection-only")
				if m.Role == llm.RoleTool {
					actual++
					require.Equal(t, "actual", m.ToolCallID)
				}
			}
		}
	}
	require.Equal(t, 1, actual)
	require.Equal(t, 1, executed)
	require.Equal(t, 1, r.ProtocolRecoveries)
}

func TestReadonlyProtocolRecoveryAndBudgetSummaryCannotPublishMarkers(t *testing.T) {
	for _, rounds := range [][][]llm.StreamChunk{
		{{{Content: "</function>"}}, {{Content: "</tool_call>"}}},
		{{{ToolCalls: []llm.ToolCall{{ID: "x", Name: "missing", Arguments: "{}"}}}}, {{Content: "</function>"}}},
	} {
		fake := &scriptedNativeLLM{rounds: rounds}
		a := &Agent{cfg: &AgentConfig{}, llmClient: fake}
		r := a.runNativeLoop(context.Background(), &AgentRequest{}, nil, nil, &ToolLoopConfig{MaxIterations: 4, MaxToolCalls: 1, FinishOnToolBudget: true})
		require.False(t, r.Success)
		require.Empty(t, r.Content)
		require.NotEmpty(t, r.Error)
		require.LessOrEqual(t, fake.call, 2)
	}
	fake := &scriptedNativeLLM{rounds: [][]llm.StreamChunk{{{Content: "示例中 </function> 是结束标签。"}}}}
	a := &Agent{cfg: &AgentConfig{}, llmClient: fake}
	r := a.runNativeLoop(context.Background(), &AgentRequest{}, nil, nil, &ToolLoopConfig{MaxIterations: 3, FinishOnToolBudget: true})
	require.True(t, r.Success)
	require.True(t, strings.Contains(r.Content, "示例"))
	require.Equal(t, 1, fake.call)
}
