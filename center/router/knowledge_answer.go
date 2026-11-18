package router

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ccfos/nightingale/v6/aiagent"
	agenttools "github.com/ccfos/nightingale/v6/aiagent/tools"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/knowledgebase"
	"github.com/ccfos/nightingale/v6/pkg/modeloutput"
	"strings"
	"time"
)

// Evidence is collected from successful reader results, never from model text.
type knowledgeAnswerEvidence struct {
	Data       []aiagent.KnowledgeDataEvidence
	Sections   []int64
	Procedures []int64
	Searched   bool
	Empty      bool
	Failed     bool
	Denied     bool
}

func (e *knowledgeAnswerEvidence) observe(tool, text string) {
	if tool != "read_knowledge" && tool != "search_knowledge" && tool != "query_knowledge_data" && tool != "resolve_knowledge_object" {
		return
	}
	if strings.HasPrefix(text, "Error:") {
		e.Failed = true
		if strings.Contains(text, "权限") || strings.Contains(text, "forbidden") {
			e.Denied = true
		}
		return
	}
	if tool == "query_knowledge_data" || tool == "resolve_knowledge_object" {
		var data aiagent.KnowledgeDataEvidence
		if json.Unmarshal([]byte(text), &data) == nil && data.Operation != "" && data.QueriedAt > 0 && len(data.Result) > 0 {
			data.Result = nil
			e.Data = append(e.Data, data)
		} else {
			e.Failed = true
		}
		return
	}
	if tool == "search_knowledge" {
		var result struct {
			Documents  []json.RawMessage `json:"documents"`
			Procedures []json.RawMessage `json:"procedures"`
		}
		if json.Unmarshal([]byte(text), &result) == nil {
			e.Searched = true
			e.Empty = len(result.Documents)+len(result.Procedures) == 0
		}
		return
	}
	var hit knowledgebase.Hit
	if json.Unmarshal([]byte(text), &hit) == nil && hit.ID > 0 {
		e.Sections = append(e.Sections, hit.ID)
		return
	}
	var procedure struct {
		Version models.KnowledgeVersion `json:"version"`
	}
	if json.Unmarshal([]byte(text), &procedure) == nil && procedure.Version.ID > 0 {
		e.Procedures = append(e.Procedures, procedure.Version.ID)
	}
}
func safeCitationLabel(s string) string {
	return strings.NewReplacer("[", "\\[", "]", "\\]", "\n", " ", "\r", " ", "<", "", ">", "").Replace(s)
}
func (rt *Router) groundKnowledgeAnswer(user int64, e knowledgeAnswerEvidence, text string) string {
	text = modeloutput.Visible(text)
	refs := []string{}
	invalidReference := false
	seen := map[string]bool{}
	for _, id := range e.Sections {
		h, err := rt.Library.Read(user, id)
		if err != nil {
			invalidReference = true
			continue
		}
		if seen[h.URL+h.Heading] {
			continue
		}
		seen[h.URL+h.Heading] = true
		refs = append(refs, fmt.Sprintf("[%s · V%d · %s](%s)", safeCitationLabel(h.Title), h.Sequence, safeCitationLabel(h.Heading), h.URL))
	}
	for _, id := range e.Procedures {
		ks := &knowledge.Store{DB: rt.Ctx.DB}
		v, _, err := ks.Version(user, id)
		if err != nil || v.WithdrawnAt > 0 {
			invalidReference = true
			continue
		}
		url := fmt.Sprintf("/ops/knowledge?version=%d", v.ID)
		if seen[url] {
			continue
		}
		seen[url] = true
		refs = append(refs, fmt.Sprintf("[%s · V%d](%s)", safeCitationLabel(v.Title), v.Sequence, url))
	}
	for _, data := range e.Data {
		if agenttools.CheckKnowledgeDataAccess(context.Background(), rt.buildToolDeps(), user, data) != nil {
			invalidReference = true
			continue
		}
		label := fmt.Sprintf("%s，查询时间 %s", safeCitationLabel(data.Operation), time.Unix(data.QueriedAt, 0).Format("2006-01-02 15:04:05 MST"))
		if data.TimeRange != "" {
			label += "，范围 " + safeCitationLabel(data.TimeRange)
		}
		if !seen[label] {
			refs = append(refs, label)
			seen[label] = true
		}
	}
	if invalidReference {
		return "引用的资料已变更、失效或没有权限查阅，请重新查询。"
	}
	if len(refs) == 0 {
		if e.Denied {
			return "没有权限查阅，或引用的资料版本已失效。"
		}
		if e.Failed {
			return "资料检索暂时失败，请稍后重试。"
		}
		if e.Searched && e.Empty {
			return "未找到相关资料。可以尝试资料标题或更具体的关键词。"
		}
		return "尚未取得可核对的资料章节，无法给出基于资料的回答。请提供资料名称或更具体的关键词。"
	}
	return text + "\n\n依据来源（权限与有效性已核对；查询时间不等于采样时间）：\n\n- " + strings.Join(refs, "\n- ")
}

func (rt *Router) knowledgeEvidenceValid(ctx context.Context, user int64, e knowledgeAnswerEvidence) bool {
	if len(e.Sections)+len(e.Procedures)+len(e.Data) == 0 {
		return false
	}
	for _, id := range e.Sections {
		if rt.Library == nil {
			return false
		}
		if _, err := rt.Library.WithContext(ctx).Read(user, id); err != nil {
			return false
		}
	}
	for _, id := range e.Procedures {
		ks := &knowledge.Store{DB: rt.Ctx.DB}
		v, spec, err := ks.Version(user, id)
		if err != nil || v.WithdrawnAt > 0 || spec.ReviewAt <= time.Now().Unix() {
			return false
		}
	}
	for _, d := range e.Data {
		if agenttools.CheckKnowledgeDataAccess(ctx, rt.buildToolDeps(), user, d) != nil {
			return false
		}
	}
	return true
}
