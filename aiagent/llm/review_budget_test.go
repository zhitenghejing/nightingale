package llm

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReviewThinkingBudgetIsScopedAndDoesNotMutateSharedConfig(t *testing.T) {
	budget := 1024
	cfg := &Config{Model: "Qwen/Qwen3.8-27B", BaseURL: "https://api.siliconflow.cn/v1/chat/completions", ExtraBody: map[string]any{"thinking_budget": float64(8192)}}
	client := &OpenAI{config: cfg}
	request := client.convertRequest(&GenerateRequest{ThinkingBudget: &budget})
	require.Equal(t, 1024, request.extraBody["thinking_budget"])
	require.Equal(t, float64(8192), cfg.ExtraBody["thinking_budget"])
	main := client.convertRequest(&GenerateRequest{})
	require.Equal(t, float64(8192), main.extraBody["thinking_budget"], "main agent retains user configuration")
	cfg.ExtraBody["thinking_budget"] = float64(512)
	require.Equal(t, 512, client.convertRequest(&GenerateRequest{ThinkingBudget: &budget}).extraBody["thinking_budget"])
	cfg.ExtraBody["enable_thinking"] = false
	require.Equal(t, float64(512), client.convertRequest(&GenerateRequest{ThinkingBudget: &budget}).extraBody["thinking_budget"])
}

func TestUnknownProviderOrModelDoesNotReceiveVendorReviewBudget(t *testing.T) {
	budget := 1024
	for _, cfg := range []*Config{{Model: "gpt-example", BaseURL: "https://api.siliconflow.cn/v1"}, {Model: "Qwen/Qwen3.8-27B", BaseURL: "https://custom.example/v1"}} {
		require.NotContains(t, (&OpenAI{config: cfg}).convertRequest(&GenerateRequest{ThinkingBudget: &budget}).extraBody, "thinking_budget")
	}
}
