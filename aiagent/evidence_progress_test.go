package aiagent

import (
	"context"
	"fmt"
	"github.com/ccfos/nightingale/v6/aiagent/llm"
	"github.com/stretchr/testify/require"
	"testing"
)

type progressModel struct {
	scriptedNativeLLM
	tools []int
}

func (m *progressModel) Generate(ctx context.Context, r *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	m.tools = append(m.tools, len(r.Tools))
	return m.scriptedNativeLLM.Generate(ctx, r)
}
func (m *progressModel) GenerateStream(ctx context.Context, r *llm.GenerateRequest) (<-chan llm.StreamChunk, error) {
	m.tools = append(m.tools, len(r.Tools))
	return m.scriptedNativeLLM.GenerateStream(ctx, r)
}
func TestEvidenceLoopStopsRepeatedSuccessfulSearchesAndSynthesizes(t *testing.T) {
	name := "search_knowledge"
	old, exists := builtinTools[name]
	defer func() {
		if exists {
			builtinTools[name] = old
		} else {
			delete(builtinTools, name)
		}
	}()
	calls := 0
	RegisterBuiltinTool(name, &BuiltinTool{Definition: AgentTool{Name: name, Type: ToolTypeBuiltin}, Handler: func(context.Context, *ToolDeps, map[string]interface{}, map[string]string) (string, error) {
		calls++
		return `{"documents":[{"id":42}],"read_documents":[{"id":42,"body":"verified fact"}]}`, nil
	}})
	rounds := [][]llm.StreamChunk{}
	for i := 0; i < 3; i++ {
		rounds = append(rounds, []llm.StreamChunk{{ToolCalls: []llm.ToolCall{{ID: fmt.Sprint(i), Name: name, Arguments: fmt.Sprintf(`{"query":"phrase%d"}`, i)}}}})
	}
	rounds = append(rounds, []llm.StreamChunk{{Content: "verified fact; remaining status unknown"}})
	m := &progressModel{scriptedNativeLLM: scriptedNativeLLM{rounds: rounds}}
	a := NewAgent(&AgentConfig{Tools: []AgentTool{{Name: name, Type: ToolTypeBuiltin}}, GuardEvidenceProgress: true, MaxIterations: 10, Timeout: 3000}, WithLLMClient(m))
	// A short test deadline would enter the answer reserve after the first read;
	// exercise the no-progress condition without a wall-clock deadline instead.
	a.cfg.Timeout = 600000
	r, err := a.Run(context.Background(), &AgentRequest{UserMessage: "find fact"})
	require.NoError(t, err)
	require.True(t, r.Success)
	require.Equal(t, 3, calls)
	require.Equal(t, 0, m.tools[len(m.tools)-1])
	require.Contains(t, r.Content, "verified fact")
}
func TestEvidenceProgressKeepsNewSourcesPagesValuesAndOperationalActions(t *testing.T) {
	p := newEvidenceProgress()
	round := func(name, args, raw string) bool { p.beginRound(); p.observe(name, args, raw); return p.endRound() }
	require.True(t, round("search_knowledge", `{}`, `{"documents":[{"id":1}],"read_documents":[{"id":1,"body":"A"}]}`))
	round("search_knowledge", `{"query":"rephrase"}`, `{"documents":[{"id":1}],"read_documents":[{"id":1,"body":"A"}]}`)
	require.True(t, p.warn())
	round("search_knowledge", `{}`, `{"documents":[{"id":1},{"id":2}],"read_documents":[{"id":1,"body":"A"},{"id":2,"body":"B"}]}`)
	require.False(t, p.warn())
	for _, cell := range []string{`{"address":"A2","value":"1"}`, `{"address":"A3","value":"2"}`, `{"address":"A3","value":"3"}`} {
		round("read_workbook_range", `{}`, `{"workbook_version":7,"result":{"operation":"read","sheet":"s","rows":[[`+cell+`]]}}`)
		require.False(t, p.warn())
	}
	require.False(t, round("load_skill", `{}`, `loaded`))
	require.False(t, round("update_dashboard", `{}`, `proposal`))
	require.False(t, round("query_prometheus", `{}`, `{"data":[]}`), "live and operational tools keep their own semantics")
}
func TestEvidenceFinalAnswerHonorsExplicitCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := &progressModel{}
	a := NewAgent(&AgentConfig{}, WithLLMClient(m))
	r := a.finishEvidenceCollection(ctx, &AgentRequest{}, nil, &ToolLoopConfig{TimeoutMessage: "cancelled"}, &AgentResponse{}, 1)
	require.False(t, r.Success)
	require.Equal(t, "cancelled", r.Error)
	require.Empty(t, m.tools)
}

func TestEvidenceLoopReservesLiveContextForFinalAnswer(t *testing.T) {
	name := "search_knowledge"
	old, exists := builtinTools[name]
	defer func() {
		if exists {
			builtinTools[name] = old
		} else {
			delete(builtinTools, name)
		}
	}()
	count := 0
	RegisterBuiltinTool(name, &BuiltinTool{Definition: AgentTool{Name: name, Type: ToolTypeBuiltin}, Handler: func(context.Context, *ToolDeps, map[string]interface{}, map[string]string) (string, error) {
		count++
		return `{"read_documents":[{"id":42,"body":"verified"}]}`, nil
	}})
	m := &progressModel{scriptedNativeLLM: scriptedNativeLLM{rounds: [][]llm.StreamChunk{{{ToolCalls: []llm.ToolCall{{ID: "first", Name: name, Arguments: `{}`}}}}, {{Content: "verified; no further measurements"}}}}}
	a := NewAgent(&AgentConfig{Tools: []AgentTool{{Name: name, Type: ToolTypeBuiltin}}, GuardEvidenceProgress: true, MaxIterations: 10, Timeout: 60000}, WithLLMClient(m))
	r, err := a.Run(context.Background(), &AgentRequest{UserMessage: "find"})
	require.NoError(t, err)
	require.True(t, r.Success)
	require.Equal(t, 1, count)
	require.Equal(t, []int{1, 0}, m.tools)
}

func TestImmutableSectionRereadDoesNotResetProgress(t *testing.T) {
	p := newEvidenceProgress()
	p.beginRound()
	p.observe("search_knowledge", `{}`, `{"documents":[{"id":42}],"read_documents":[{"id":42,"body":"matching row has been read"}]}`)
	require.True(t, p.endRound())
	p.beginRound()
	p.observe("read_knowledge", `{"section_id":42}`, `{"id":42,"body":"same immutable section, verbose cells"}`)
	require.True(t, p.endRound())
	require.True(t, p.warn())
	p.beginRound()
	p.observe("read_knowledge", `{"section_id":43}`, `{"id":43,"body":"new section"}`)
	require.True(t, p.endRound())
	require.False(t, p.warn())
}

func TestWorkbookRowRereadAfterSearchIsNotNewEvidence(t *testing.T) {
	p := newEvidenceProgress()
	p.beginRound()
	p.observe("search_knowledge", `{}`, `{"matched_records":[{"version_id":7,"sheet":"s","result":{"sheet":"s","rows":[[{"address":"A54","value":"005066001979"}]]},"context_rows":[[{"address":"A1","value":"SN"}]]}]}`)
	p.endRound()
	p.beginRound()
	p.observe("read_workbook_range", `{}`, `{"workbook_version":7,"result":{"operation":"read","sheet":"s","rows":[[{"address":"A54","value":"005066001979"}]]}}`)
	p.endRound()
	require.True(t, p.warn())
	p.beginRound()
	p.observe("read_workbook_range", `{}`, `{"workbook_version":7,"result":{"operation":"read","sheet":"s","rows":[[{"address":"A1","value":"SN"}]]}}`)
	p.endRound()
	require.True(t, p.stalled())
}

func TestDedicatedStatisticsUsesTheSameProgressReceipts(t *testing.T) {
	p := newEvidenceProgress()
	for i := 0; i < 3; i++ {
		p.beginRound()
		p.observe("target_statistics", `{"query":"scope-","group_by":"agent_version"}`, `{"operation":"target_statistics","query":"scope-","queried_at":123,"result":{"group_by":"agent_version","total":2}}`)
		require.True(t, p.endRound())
	}
	require.True(t, p.stalled())
}

func TestFirstArgumentTypeErrorAllowsCorrectionAfterRedundantRead(t *testing.T) {
	p := newEvidenceProgress()
	round := func(name, args, raw string) { p.beginRound(); p.observe(name, args, raw); p.endRound() }
	round("read_knowledge", `{"section_id":1}`, `{"id":1}`)
	round("read_knowledge", `{"section_id":1}`, `{"id":1}`)
	require.True(t, p.warn())
	raw := "Error: tool search_knowledge parameter query must be string; encode numeric identifiers as quoted JSON strings"
	round("search_knowledge", `{"query":123}`, raw)
	require.False(t, p.stalled(), "first type error receives a chance to correct")
	round("search_knowledge", `{"query":124}`, raw)
	require.True(t, p.warn())
	round("search_knowledge", `{"query":125}`, raw)
	require.True(t, p.stalled(), "changing the invalid value cannot reset the guard")
}
