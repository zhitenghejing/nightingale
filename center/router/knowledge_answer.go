package router

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/aiagent/chat"
	agenttools "github.com/ccfos/nightingale/v6/aiagent/tools"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/ccfos/nightingale/v6/pkg/knowledgebase"
	"github.com/ccfos/nightingale/v6/pkg/modeloutput"
	"regexp"
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
	if tool != "read_knowledge" && tool != "search_knowledge" && !aiagent.IsKnowledgeDataTool(tool) && tool != "resolve_knowledge_object" && tool != "query_environment" && !aiagent.IsKnowledgeWorkbookTool(tool) {
		return
	}
	if strings.HasPrefix(text, "Error:") {
		// Refusing a model's attempt to leave the user-selected source is an
		// expected policy result, not a failure of the authoritative search.
		if strings.Contains(text, "本轮仅可读取用户指定资料") {
			return
		}
		e.Failed = true
		if strings.Contains(text, "权限") || strings.Contains(text, "forbidden") {
			e.Denied = true
		}
		return
	}
	if aiagent.IsKnowledgeDataTool(tool) || tool == "resolve_knowledge_object" || tool == "query_environment" || aiagent.IsKnowledgeWorkbookTool(tool) {
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
			Documents     []json.RawMessage   `json:"documents"`
			ReadDocuments []knowledgebase.Hit `json:"read_documents"`
			Procedures    []knowledge.Match   `json:"procedures"`
		}
		if json.Unmarshal([]byte(text), &result) == nil {
			e.Searched = true
			e.Empty = len(result.Documents)+len(result.Procedures) == 0
			for _, h := range result.ReadDocuments {
				if h.ID > 0 && h.SectionID == h.ID {
					e.Sections = append(e.Sections, h.ID)
				}
			}
			for _, procedure := range result.Procedures {
				if procedure.Version.ID > 0 {
					e.Procedures = append(e.Procedures, procedure.Version.ID)
				}
			}
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

func (rt *Router) readProcedureEvidence(ctx context.Context, user, id int64) (*models.KnowledgeVersion, error) {
	if rt.Ctx == nil || rt.Ctx.DB == nil {
		return nil, fmt.Errorf("没有权限查阅，或规程已失效")
	}
	v, spec, err := (&knowledge.Store{DB: rt.Ctx.DB.WithContext(ctx)}).Version(user, id)
	if err != nil {
		return nil, err
	}
	if v.WithdrawnAt > 0 || spec.ReviewAt <= time.Now().Unix() {
		return nil, fmt.Errorf("规程已撤回或超过复核期限")
	}
	return v, nil
}
func (rt *Router) groundKnowledgeAnswer(ctx context.Context, user int64, e knowledgeAnswerEvidence, text string) string {
	text = modeloutput.Visible(text)
	refs := []string{}
	sourceURLs := []string{}
	invalidReference := false
	seen := map[string]bool{}
	for _, id := range e.Sections {
		h, err := rt.Library.WithContext(ctx).Read(user, id)
		if err != nil {
			invalidReference = true
			continue
		}
		if seen[h.URL+h.Heading] {
			continue
		}
		seen[h.URL+h.Heading] = true
		sourceURLs = append(sourceURLs, h.URL)
		refs = append(refs, fmt.Sprintf("[%s · V%d · %s](%s)", safeCitationLabel(h.Title), h.Sequence, safeCitationLabel(h.Heading), h.URL))
	}
	for _, id := range e.Procedures {
		v, err := rt.readProcedureEvidence(ctx, user, id)
		if err != nil {
			invalidReference = true
			continue
		}
		url := fmt.Sprintf("/ops/knowledge?version=%d", v.ID)
		if seen[url] {
			continue
		}
		seen[url] = true
		sourceURLs = append(sourceURLs, url)
		refs = append(refs, fmt.Sprintf("[%s · V%d](%s)", safeCitationLabel(v.Title), v.Sequence, url))
	}
	for _, data := range e.Data {
		if agenttools.CheckKnowledgeDataAccess(ctx, rt.buildToolDeps(), user, data) != nil {
			invalidReference = true
			continue
		}
		label := fmt.Sprintf("%s，查询时间 %s", safeCitationLabel(data.Operation), time.Unix(data.QueriedAt, 0).Format("2006-01-02 15:04:05 MST"))
		if data.WorkbookVersion > 0 {
			sourceURLs = append(sourceURLs, data.SourceURL)
			label = fmt.Sprintf("[%s](%s)", safeCitationLabel(data.SourceLabel), data.SourceURL)
		}
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
	if modeloutput.ProtocolOnly(text) {
		return "本次模型未形成可核对的可见答复，请重试或缩小检索范围。"
	}
	text = normalizeKnowledgeLinks(text, sourceURLs)
	return text + "\n\n依据来源（权限与有效性已核对；查询时间不等于采样时间）：\n\n- " + strings.Join(refs, "\n- ")
}

func (rt *Router) knowledgeEvidenceValid(ctx context.Context, user int64, e knowledgeAnswerEvidence, scopes ...knowledgebase.SourceScope) bool {
	scope := knowledgebase.SourceScope{}
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	if len(e.Sections)+len(e.Procedures)+len(e.Data) == 0 {
		return false
	}
	for _, id := range e.Sections {
		if rt.Library == nil {
			return false
		}
		h, err := rt.Library.WithContext(ctx).Read(user, id)
		if err != nil {
			return false
		}
		if scope.Title != "" || len(scope.Identifiers) > 0 {
			k, err := rt.Library.WithContext(ctx).Version(user, h.VersionID)
			if err != nil || !scope.Matches(k.Title, string(k.Markdown)) {
				return false
			}
		}
	}
	for _, id := range e.Procedures {
		if scope.Title != "" || len(scope.Identifiers) > 0 {
			return false
		}
		if _, err := rt.readProcedureEvidence(ctx, user, id); err != nil {
			return false
		}
	}
	for _, d := range e.Data {
		if d.WorkbookVersion > 0 && (scope.Title != "" || len(scope.Identifiers) > 0) {
			k, err := rt.Library.WithContext(ctx).Version(user, d.WorkbookVersion)
			if err != nil || !scope.Matches(k.Title, string(k.Markdown)) {
				return false
			}
		}
		if agenttools.CheckKnowledgeDataAccess(ctx, rt.buildToolDeps(), user, d) != nil {
			return false
		}
	}
	return true
}

func documentEvidenceData(data []aiagent.KnowledgeDataEvidence) []aiagent.KnowledgeDataEvidence {
	out := []aiagent.KnowledgeDataEvidence{}
	for _, e := range data {
		if strings.HasPrefix(e.Operation, "workbook_") && e.WorkbookVersion > 0 {
			out = append(out, e)
		}
	}
	return out
}

// Some providers turn a local /archives/... citation into http://archives/...
// Only restore exact URLs already authorized above; do not rewrite arbitrary links.
func normalizeKnowledgeLinks(text string, authorized []string) string {
	for _, url := range authorized {
		if !strings.HasPrefix(url, "/") || strings.HasPrefix(url, "//") {
			continue
		}
		for _, scheme := range []string{"http:/", "https:/"} {
			// Match a complete authorized URL even in prose/fullwidth Chinese
			// punctuation; never rewrite an authorized prefix of another URL.
			pattern := regexp.MustCompile(regexp.QuoteMeta(scheme+url) + `(?:$|[)）\]\s>。])`)
			text = pattern.ReplaceAllStringFunc(text, func(match string) string { return strings.TrimPrefix(match, scheme) })
		}
	}
	return text
}

// A workbook and a platform query are different evidence families even though
// both travel in Data receipts. Preserve both in joint answers and review them.
func (e knowledgeAnswerEvidence) combinedSources() bool {
	document := len(e.Sections)+len(e.Procedures) > 0
	platform := false
	for _, d := range e.Data {
		if d.WorkbookVersion > 0 {
			document = true
		} else {
			platform = true
		}
	}
	return document && platform
}

// A catalog query cannot stand in for an empty document lookup. Once a source
// document was actually read, retain its platform observations for joint answers.
func knowledgeQuestionEvidence(question string, e knowledgeAnswerEvidence) knowledgeAnswerEvidence {
	if chat.KnowledgeDocumentSearch(question) && len(e.Sections)+len(e.Procedures)+len(documentEvidenceData(e.Data)) == 0 {
		e.Data = nil
	}
	return e
}
