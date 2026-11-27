package aiagent

import (
	"context"
	"strings"
	"testing"
)

func TestBuiltinStringIdentifierNeverBecomesEmptyFilter(t *testing.T) {
	name := "test_identifier_validation"
	calls := 0
	RegisterBuiltinTool(name, &BuiltinTool{Definition: AgentTool{Name: name, Parameters: []ToolParameter{{Name: "query", Type: "string"}}}, Handler: func(_ context.Context, _ *ToolDeps, a map[string]interface{}, _ map[string]string) (string, error) {
		calls++
		q, _ := a["query"].(string)
		return q, nil
	}})
	t.Cleanup(func() { delete(builtinTools, name) })
	for _, raw := range []string{`{"query":5066001979}`, `{"query":123456789012345678901234}`, `{"query":null}`, `{"query":true}`, `{"query":[]}`, `{"query":`} {
		_, handled, err := ExecuteBuiltinTool(context.Background(), nil, name, nil, raw)
		if !handled || err == nil {
			t.Fatalf("invalid arguments reached a handler: %s", raw)
		}
		if !strings.Contains(raw, `"query":`) {
			t.Fatal("bad fixture")
		}
	}
	if calls != 0 {
		t.Fatalf("invalid arguments executed %d times", calls)
	}
	raw := `{"query":"005066001979"}`
	result, _, err := ExecuteBuiltinTool(context.Background(), nil, name, nil, raw)
	if err != nil || result != "005066001979" || calls != 1 {
		t.Fatalf("literal identifier changed: %q %v calls=%d", result, err, calls)
	}
}
