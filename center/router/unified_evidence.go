package router

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/aiagent"
	agenttools "github.com/ccfos/nightingale/v6/aiagent/tools"
	"github.com/ccfos/nightingale/v6/pkg/modeloutput"
	"github.com/toolkits/pkg/logger"
)

func (e knowledgeAnswerEvidence) observed() bool {
	return len(e.Sections)+len(e.Procedures)+len(e.Data) > 0 || e.Searched || e.Failed || e.Denied
}

var archiveCitation = regexp.MustCompile(`\]\(([^\s)]+)\)`)
var archivePath = regexp.MustCompile(`^/archives/[0-9]+/documents/[0-9]+$`)
var documentFilename = regexp.MustCompile(`(?i)\.(?:docx?|xlsx?|pdf|md|txt|csv|tsv)$`)

func documentCitationKey(raw string) string {
	// Normalize only the common model spelling of local archive links.
	if strings.HasPrefix(raw, "http://archives/") || strings.HasPrefix(raw, "https://archives/") {
		raw = raw[strings.Index(raw, "://")+2:]
	}
	u, err := url.Parse(raw)
	if err != nil || !archivePath.MatchString(u.Path) || u.Query().Get("version") == "" {
		return ""
	}
	return u.Path + "?version=" + u.Query().Get("version")
}

func procedureCitationKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path != "/ops/knowledge" {
		return ""
	}
	version := u.Query().Get("version")
	if version == "" {
		return ""
	}
	id, err := strconv.ParseInt(version, 10, 64)
	if err != nil || id <= 0 {
		return "/ops/knowledge?version=" + version
	}
	return fmt.Sprintf("/ops/knowledge?version=%d", id)
}
func citedDocument(text, source, title string) bool {
	key := documentCitationKey(source)
	for _, match := range archiveCitation.FindAllStringSubmatch(text, -1) {
		if key != "" && documentCitationKey(match[1]) == key {
			return true
		}
	}
	return title != "" && (strings.Contains(text, "《"+title) ||
		(documentFilename.MatchString(title) && (strings.Contains(text, title) || strings.Contains(text, safeCitationLabel(title)))))
}

// The main agent owns planning and wording. This layer checks the sources it
// observes or cites, reauthorizes current records and adds concise provenance. It
// does not route the question or rewrite the answer through a second model.
func (rt *Router) finalizeUnifiedAnswer(ctx context.Context, user int64, e, prior knowledgeAnswerEvidence, text string) (string, knowledgeAnswerEvidence) {
	text = modeloutput.Answer(text)
	selected := knowledgeAnswerEvidence{Searched: e.Searched, Empty: e.Empty, Failed: e.Failed, Denied: e.Denied}
	refs := []string{}
	seen := map[string]bool{}
	allowed := map[string]bool{}
	invalid := false
	sections := append(append([]int64{}, e.Sections...), prior.Sections...)
	// Repair only aliases proven to describe the exact source read this turn.
	for _, id := range sections {
		if rt.Library == nil {
			continue
		}
		h, err := rt.Library.WithContext(ctx).Read(user, id)
		if err != nil {
			continue
		}
		alias := fmt.Sprintf("/archives/%d/documents/%d?version=%d", h.ArchiveID, h.DocumentID, h.VersionID)
		text = replaceVerifiedCitationAlias(text, alias, h.URL)
		text = replaceVerifiedCitationAlias(text, fmt.Sprintf("/archives/%d/documents/%d?version=%d", h.ArchiveID, h.DocumentID, h.ID), h.URL)
		text = replaceVerifiedCitationAlias(text, fmt.Sprintf("/archives/%d/documents/%d?version=%d", h.ArchiveID, h.DocumentID, h.Sequence), h.URL)
	}
	for _, id := range sections {
		if rt.Library == nil {
			continue
		}
		h, err := rt.Library.WithContext(ctx).Read(user, id)
		if err != nil {
			invalid = true
			continue
		}
		if !citedDocument(text, h.URL, h.Title) {
			continue
		}
		key := documentCitationKey(h.URL)
		allowed[key] = true
		selected.Sections = append(selected.Sections, id)
		if seen[key] {
			continue
		}
		seen[key] = true
		refs = append(refs, fmt.Sprintf("[%s · V%d](%s)", safeCitationLabel(h.Title), h.Sequence, h.URL))
	}
	// A search result can contain the full published procedure specification.
	// Keep every observed dependency for later history reauthorization, including
	// reads the model did not turn into an explicit link.
	procedures := append(append([]int64{}, e.Procedures...), prior.Procedures...)
	for _, id := range procedures {
		v, err := rt.readProcedureEvidence(ctx, user, id)
		if err != nil {
			invalid = true
			continue
		}
		key := fmt.Sprintf("/ops/knowledge?version=%d", v.ID)
		if seen[key] {
			continue
		}
		selected.Procedures = append(selected.Procedures, id)
		allowed[key] = true
		seen[key] = true
		refs = append(refs, fmt.Sprintf("[%s · V%d](%s)", safeCitationLabel(v.Title), v.Sequence, key))
	}
	data := append(append([]aiagent.KnowledgeDataEvidence{}, e.Data...), prior.Data...)
	workbookFacts := map[int64]bool{}
	for _, r := range data {
		if r.WorkbookVersion > 0 && strings.HasPrefix(r.Operation, "workbook_") && r.Operation != "workbook_sheets" {
			workbookFacts[r.WorkbookVersion] = true
		}
	}
	for _, receipt := range data {
		if receipt.Operation == "workbook_sheets" && workbookFacts[receipt.WorkbookVersion] {
			continue
		}
		if receipt.WorkbookVersion > 0 && rt.Library != nil {
			if k, err := rt.Library.WithContext(ctx).Version(user, receipt.WorkbookVersion); err == nil {
				alias := fmt.Sprintf("/archives/%d/documents/%d?version=%d", k.ArchiveID, k.DocumentID, k.ID)
				text = replaceVerifiedCitationAlias(text, alias, receipt.SourceURL)
			}
		}
		include := receipt.Operation == "asset_lookup" || receipt.Operation == "target_statistics"
		if receipt.WorkbookVersion > 0 {
			include = citedDocument(text, receipt.SourceURL, "")
		}
		if receipt.Operation != "environment_search" && receipt.Operation != "resolve_knowledge_object" && receipt.WorkbookVersion == 0 {
			include = true
		}
		if !include {
			continue
		}
		if agenttools.CheckKnowledgeDataAccess(ctx, rt.buildToolDeps(), user, receipt) != nil {
			logger.Warningf("[UnifiedEvidence] invalid data receipt operation=%s", receipt.Operation)
			invalid = true
			continue
		}
		selected.Data = append(selected.Data, receipt)
		key := receipt.Operation + ":" + receipt.Query + ":" + receipt.SourceURL + ":" + receipt.SourceLabel
		if receipt.Operation == "list_targets" {
			key = receipt.Operation // one catalog source, retain all receipts above
		}
		if receipt.WorkbookVersion > 0 {
			key = documentCitationKey(receipt.SourceURL)
			allowed[key] = true
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		label := receipt.SourceLabel
		if label == "" {
			label = receipt.Operation
		}
		label = safeCitationLabel(label)
		if receipt.SourceURL != "" {
			label = fmt.Sprintf("[%s](%s)", label, receipt.SourceURL)
		}
		if receipt.WorkbookVersion == 0 {
			label += fmt.Sprintf("（查询 %s）", time.Unix(receipt.QueriedAt, 0).Format("2006-01-02 15:04:05 MST"))
		}
		refs = append(refs, label)
	}
	// A source cited from stale history must be reread or reauthorized. Unknown
	// internal archive links are not accepted just because the model printed one.
	for _, match := range archiveCitation.FindAllStringSubmatch(text, -1) {
		if key := documentCitationKey(match[1]); key != "" && !allowed[key] {
			logger.Warningf("[UnifiedEvidence] unobserved document citation=%s", key)
			invalid = true
		}
		if key := procedureCitationKey(match[1]); key != "" && !allowed[key] {
			logger.Warningf("[UnifiedEvidence] unobserved procedure citation=%s", key)
			invalid = true
		}
	}
	if invalid {
		return "引用的资料或资产记录已变更、失效或没有权限查阅，请重新查询。", selected
	}
	if len(refs) == 0 {
		return text, selected
	}
	return text + "\n\n来源（本轮已核验）：\n\n- " + strings.Join(refs, "\n- "), selected
}

// Only an exact archive/document + observed knowledge-version alias is repaired;
// arbitrary versions, paths and external links cannot acquire source authority.
func replaceVerifiedCitationAlias(text, alias, source string) string {
	return archiveCitation.ReplaceAllStringFunc(text, func(link string) string {
		match := archiveCitation.FindStringSubmatch(link)
		if len(match) == 2 && documentCitationKey(match[1]) == alias {
			return "](" + source + ")"
		}
		return link
	})
}
