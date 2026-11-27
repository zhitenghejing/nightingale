package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/pkg/environment"
)

func init() {
	register(opsDef("query_environment", "Read authorized IT environment. operation search/overview discovers exact object refs; context returns source-backed relations, pending candidates and gaps; trace follows confirmed manual relations with explicit direction and depth 1-3; timeline returns 7-day exact-target alerts or asset-ledger audits, not all IT changes. Returned target refs already establish current identity: reuse their ident/IP in data queries instead of resolving again. Trace returns node attributes; do not reread each node without a specific gap. Prioritize requested live checks over optional documents. References come from tool output; never guess identity or infer health from inventory. Not a network discovery or write tool.",
		aiagent.ToolParameter{Name: "operation", Type: "string", Required: true, Enum: []string{"overview", "search", "context", "trace", "timeline"}}, aiagent.ToolParameter{Name: "query", Type: "string"}, aiagent.ToolParameter{Name: "ref", Type: "string"}, aiagent.ToolParameter{Name: "depth", Type: "integer"}, aiagent.ToolParameter{Name: "direction", Type: "string", Enum: []string{"out", "in", "both"}}), queryEnvironment)
}
func queryEnvironment(ctx context.Context, d *aiagent.ToolDeps, a map[string]interface{}, p map[string]string) (string, error) {
	if d == nil || d.DBCtx == nil {
		return "", fmt.Errorf("environment unavailable")
	}
	u, e := getUser(d, p)
	if e != nil || u.Disabled != 0 {
		return "", fmt.Errorf("没有权限查阅")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	s := &environment.Store{DB: d.DBCtx.DB.WithContext(ctx)}
	op := getArgString(a, "operation")
	refs := map[string]string{}
	var result interface{}
	truncated := false
	switch op {
	case "overview", "search":
		v, err := s.Search(u.Id, getArgString(a, "query"))
		e = err
		for _, x := range v.Items {
			refs[x.Ref] = x.Fingerprint
		}
		result = v
		truncated = v.Truncated
	case "context":
		v, err := s.Context(u.Id, getArgString(a, "ref"))
		result = v
		truncated = v.Truncated
		e = err
		refs = v.Evidence
	case "trace":
		depth := getArgInt(a, "depth", 2)
		direction := getArgString(a, "direction")
		if direction == "" {
			direction = "out"
		}
		v, err := s.Trace(u.Id, getArgString(a, "ref"), direction, depth)
		result = v
		truncated = v.Truncated
		e = err
		refs = v.Evidence
	case "timeline":
		v, err := s.Timeline(u.Id, getArgString(a, "ref"))
		result = v
		truncated = v.Truncated
		e = err
		refs = v.Evidence
	default:
		return "", fmt.Errorf("unsupported environment operation")
	}
	if e != nil {
		return "", e
	}
	if e = s.Validate(u.Id, refs); e != nil {
		return "", e
	}
	raw, e := json.Marshal(result)
	if e != nil {
		return "", e
	}
	if len(raw) > 128<<10 {
		return "", fmt.Errorf("环境结果过大，请缩小对象范围")
	}
	receipt := aiagent.KnowledgeDataEvidence{Operation: "environment_" + op, EnvironmentRefs: refs, QueriedAt: time.Now().Unix(), Result: raw, Truncated: truncated}
	b, e := json.Marshal(receipt)
	return string(b), e
}
