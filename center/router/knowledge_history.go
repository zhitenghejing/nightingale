package router

import (
	"encoding/json"
	"strings"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/modeloutput"
)

// Only the last turn's actual tool observations are evidence. Earlier model
// commentary/reasoning and prior-turn payloads are never replayed as evidence.
func knowledgeMessageEvidence(msg *models.AssistantMessage) knowledgeAnswerEvidence {
	var e knowledgeAnswerEvidence
	if len(msg.Extra.KnowledgeEvidence) > 0 && json.Unmarshal(msg.Extra.KnowledgeEvidence, &e) == nil {
		return e
	}
	var env aiagent.TranscriptEnvelope
	if json.Unmarshal(msg.Extra.HistoryMessages, &env) != nil {
		return e
	}
	start := 0
	for i, m := range env.Messages {
		if m.Role == "user" {
			start = i + 1
		}
	}
	for _, m := range env.Messages[start:] {
		if m.Role == "tool" {
			e.observe(m.ToolName, m.Content)
		}
	}
	return e
}

func knowledgeFollowupHistory(prior []*models.AssistantMessage, valid func(knowledgeAnswerEvidence) bool) []aiagent.ChatMessage {
	out := []aiagent.ChatMessage{}
	for _, msg := range prior {
		if msg == nil {
			continue
		}
		out = append(out, aiagent.ChatMessage{Role: "user", Content: msg.Query.Content})
		if msg.Query.Mode != "knowledge_qa" || !msg.IsFinish || msg.ErrCode != 0 {
			continue
		}
		e := knowledgeMessageEvidence(msg)
		if !valid(e) {
			continue
		}
		answer := ""
		for _, r := range msg.Response {
			if r.ContentType == models.ContentTypeMarkdown {
				answer = r.Content
			}
		}
		answer = modeloutput.Visible(answer)
		if i := strings.Index(answer, "\n\n依据来源（权限与有效性"); i >= 0 {
			answer = answer[:i]
		}
		if i := strings.Index(answer, "\n\n本次查询来源"); i >= 0 {
			answer = answer[:i]
		}
		text := []rune(strings.TrimSpace(answer))
		if len(text) > 3000 {
			text = text[:3000]
		}
		if len(text) > 0 {
			out = append(out, aiagent.ChatMessage{Role: "assistant", Content: "[上一轮答复，仅用于理解话题和指代。来源权限已重新核验；报告与查询数据仍是当时记录，当前状态需要重新查询。]\n" + string(text)})
		}
	}
	return out
}
