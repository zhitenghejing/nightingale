package aiagent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/aiagent/llm"
)

func TestReadonlyQAStopsExecutingIdenticalFailedCalls(t *testing.T) {
	calls := 0
	name := "test_failed_read"
	RegisterBuiltinTool(name, &BuiltinTool{Definition: AgentTool{Name: name, Type: ToolTypeBuiltin}, Handler: func(context.Context, *ToolDeps, map[string]interface{}, map[string]string) (string, error) {
		calls++
		return "", fmt.Errorf("missing range")
	}})
	t.Cleanup(func() { delete(builtinTools, name) })
	fake := &scriptedNativeLLM{rounds: [][]llm.StreamChunk{{{ToolCalls: []llm.ToolCall{{ID: "1", Name: name, Arguments: "{}"}, {ID: "2", Name: name, Arguments: "{}"}, {ID: "3", Name: name, Arguments: "{}"}}}}, {{Content: "范围未提供，无法完成读取。"}}}}
	a := NewAgent(&AgentConfig{RestrictTools: true, FinishOnToolBudget: true, MaxIterations: 5, MaxToolCalls: 5, Timeout: 3000, Tools: []AgentTool{{Name: name, Type: ToolTypeBuiltin}}}, WithLLMClient(fake))
	response, err := a.Run(context.Background(), &AgentRequest{UserMessage: "读取"})
	if err != nil || !response.Success || calls != 2 || !strings.Contains(response.Steps[2].Observation, "此调用未执行") {
		t.Fatalf("repeated failures not bounded: calls=%d response=%+v err=%v", calls, response, err)
	}
}

func TestRestrictedWorkflowDoesNotDispatchHiddenBuiltin(t *testing.T) {
	calls := 0
	name := "ops_test_hidden_writer"
	RegisterBuiltinTool(name, &BuiltinTool{Definition: AgentTool{Name: name, Type: ToolTypeBuiltin}, Handler: func(context.Context, *ToolDeps, map[string]interface{}, map[string]string) (string, error) {
		calls++
		return "written", nil
	}})
	t.Cleanup(func() { delete(builtinTools, name) })
	a := NewAgent(&AgentConfig{RestrictTools: true, MaxIterations: 2, Timeout: 1000})
	out, _ := a.executeToolI(context.Background(), name, "{}", &AgentRequest{}, []AgentTool{{Name: "allowed_read"}})
	if calls != 0 || !strings.Contains(out, "outside the permitted") {
		t.Fatalf("hidden builtin executed or not rejected: calls=%d result=%s", calls, out)
	}
}
func TestRestrictedWorkflowBudgetIncludesBatchToolCalls(t *testing.T) {
	calls := 0
	name := "ops_test_counted_read"
	RegisterBuiltinTool(name, &BuiltinTool{Definition: AgentTool{Name: name, Type: ToolTypeBuiltin}, Handler: func(context.Context, *ToolDeps, map[string]interface{}, map[string]string) (string, error) {
		calls++
		return `{"ok":true}`, nil
	}})
	t.Cleanup(func() { delete(builtinTools, name) })
	fake := &scriptedNativeLLM{rounds: [][]llm.StreamChunk{{{ToolCalls: []llm.ToolCall{{ID: "a", Name: name, Arguments: "{}"}, {ID: "b", Name: name, Arguments: "{}"}, {ID: "c", Name: name, Arguments: "{}"}}}}}}
	a := NewAgent(&AgentConfig{RestrictTools: true, MaxToolCalls: 2, MaxIterations: 5, Timeout: 3000, Tools: []AgentTool{{Name: name, Type: ToolTypeBuiltin}}}, WithLLMClient(fake))
	response, err := a.Run(context.Background(), &AgentRequest{UserMessage: "Run batch"})
	if err != nil || response.Success || calls != 2 || !strings.Contains(response.Error, "maximum tool calls") {
		t.Fatalf("budget not enforced: calls=%d response=%+v err=%v", calls, response, err)
	}
}
func TestOpsArrayToolSchemaHasTypedItems(t *testing.T) {
	defs := buildNativeToolDefs([]AgentTool{{Name: "record", Parameters: []ToolParameter{{Name: "ids", Type: "array", ItemType: "integer"}}}})
	property := defs[0].Parameters["properties"].(map[string]interface{})["ids"].(map[string]interface{})
	if property["items"].(map[string]interface{})["type"] != "integer" {
		t.Fatal("missing typed array items")
	}
}

func TestNativeToolSchemaCarriesDeclaredStateEnums(t *testing.T) {
	defs := buildNativeToolDefs([]AgentTool{{Name: "record", Parameters: []ToolParameter{{Name: "state", Type: "string", Enum: []string{"completed", "failed", "skipped"}, Required: true}}}})
	property := defs[0].Parameters["properties"].(map[string]interface{})["state"].(map[string]interface{})
	if values, ok := property["enum"].([]string); !ok || len(values) != 3 || values[0] != "completed" {
		t.Fatalf("missing state enum: %v", property)
	}
}
