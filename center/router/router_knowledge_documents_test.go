package router

import (
	"github.com/ccfos/nightingale/v6/aiagent/chat"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestKnowledgeQAModeReadOnlyAndFreshHistory(t *testing.T) {
	h, ok := chat.Lookup("knowledge_qa")
	require.True(t, ok)
	require.Equal(t, []string{"search_knowledge", "read_knowledge", "query_environment"}, h.SelectTools(&chat.AIChatRequest{UserInput: "解释这份报告"}))
	require.Empty(t, h.RequiredSkills(&chat.AIChatRequest{}))
	require.Nil(t, h.Preflight)

}
func TestKnowledgeAnswerRequiresRetrievedEvidence(t *testing.T) {
	rt := &Router{}
	require.Contains(t, rt.groundKnowledgeAnswer(1, knowledgeAnswerEvidence{Searched: true, Empty: true}, "编造答案"), "未找到相关资料")
	require.NotContains(t, rt.groundKnowledgeAnswer(1, knowledgeAnswerEvidence{Searched: true, Empty: true}, "编造答案"), "编造")
	require.Contains(t, rt.groundKnowledgeAnswer(1, knowledgeAnswerEvidence{Failed: true}, "假装成功"), "暂时失败")
	require.Contains(t, rt.groundKnowledgeAnswer(1, knowledgeAnswerEvidence{Denied: true}, "无权标题"), "没有权限")
	var evidence knowledgeAnswerEvidence
	evidence.observe("read_knowledge", `{"id":12,"document_id":7,"body":"真实文本"}`)
	require.Equal(t, []int64{12}, evidence.Sections)
	evidence.observe("search_knowledge", `{"documents":[],"procedures":[]}`)
	require.True(t, evidence.Empty)
}
