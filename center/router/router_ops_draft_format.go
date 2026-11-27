package router

import (
	"fmt"
	"github.com/ccfos/nightingale/v6/aiagent/llm"
)

func opsDraftName(plan bool) string {
	if plan {
		return "submit_ops_plan_draft"
	}
	return "submit_ops_knowledge_draft"
}

// These functions are output formats only. They are never registered with the
// tool executor, and no returned tool call is dispatched. Unknown calls fail.
func opsDraftTools(plan bool) []llm.ToolDefinition {
	str := func(description string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": description}
	}
	object := func(properties map[string]interface{}, required ...string) map[string]interface{} {
		return map[string]interface{}{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	step := object(map[string]interface{}{"key": str("Unique short step key"), "title": str("Short title"), "kind": map[string]interface{}{"type": "string", "enum": []string{"manual"}}, "instruction": str("Manual investigation, at most 160 characters"), "expected": str("Fact to record, not an invented outcome")}, "key", "title", "kind", "instruction", "expected")
	check := object(map[string]interface{}{"key": str("Unique check key"), "title": str("Business function and data integrity acceptance criteria"), "kind": map[string]interface{}{"type": "string", "enum": []string{"manual"}}, "required": map[string]interface{}{"type": "boolean", "enum": []bool{true}}}, "key", "title", "kind", "required")
	spec := object(map[string]interface{}{"summary": str("Concise investigation goal, not a recovery claim"), "body": str("Facts and missing information only; no hidden reasoning"), "steps": map[string]interface{}{"type": "array", "minItems": 1, "maxItems": 3, "items": step}, "checks": map[string]interface{}{"type": "array", "minItems": 1, "maxItems": 2, "items": check}, "escalation": str("Stop condition and next responsible role")}, "summary", "body", "steps", "checks", "escalation")
	props := map[string]interface{}{"title": str("Short title in the user's language"), "spec": spec}
	required := []string{"title", "spec"}
	if plan {
		props["scope"] = str("Only this case and authorized target")
		props["rationale"] = str("Observed facts, hypotheses, unknowns; at most 500 characters")
		required = append(required, "scope", "rationale")
	}
	return []llm.ToolDefinition{{Name: opsDraftName(plan), Description: "Submit an UNVERIFIED draft as structured data. No operation, approval or execution is performed. Use at most three manual investigation steps and two manual checks. Do not invent causes or successful recovery.", Parameters: object(props, required...)}}
}

func opsDraftPayload(response *llm.GenerateResponse, plan bool) (string, error) {
	if response == nil {
		return "", fmt.Errorf("model returned no draft")
	}
	if len(response.ToolCalls) > 0 {
		if len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != opsDraftName(plan) {
			return "", fmt.Errorf("model did not use the declared draft format")
		}
		return response.ToolCalls[0].Arguments, nil
	}
	return response.Content, nil
}
