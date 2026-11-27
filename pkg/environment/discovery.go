package environment

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	"github.com/ccfos/nightingale/v6/pkg/knowledgebase"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
)

// Trace only traverses currently confirmed relations. Direction is explicit;
// observed mappings do not establish network adjacency or causation.
type Graph struct {
	Nodes     []Entity          `json:"nodes"`
	Relations []RelationView    `json:"relations"`
	Truncated bool              `json:"truncated"`
	Evidence  map[string]string `json:"evidence"`
	Note      string            `json:"note"`
}

func (s *Store) Trace(user int64, ref, direction string, depth int) (Graph, error) {
	g := Graph{Nodes: []Entity{}, Relations: []RelationView{}, Evidence: map[string]string{}, Note: "仅遍历当前已确认的人工关系；未收录的依赖和受限对象不在结果中，不能据此认定影响范围完整。"}
	if depth < 1 || depth > 3 {
		return g, bad("依赖层级限1至3层")
	}
	if direction != "out" && direction != "in" && direction != "both" {
		return g, bad("方向须为out、in或both")
	}
	first, e := s.Get(user, ref)
	if e != nil {
		return g, e
	}
	g.Nodes = append(g.Nodes, first)
	g.Evidence[ref] = first.Fingerprint
	queue := []string{ref}
	visited := map[string]bool{ref: true}
	edgeSeen := map[int64]bool{}
	for level := 0; level < depth; level++ {
		next := []string{}
		for _, r := range queue {
			ctx, e := s.Context(user, r)
			if e != nil {
				return g, e
			}
			if ctx.Truncated {
				g.Truncated = true
			}
			for _, v := range ctx.Relations {
				if v.Effective != "confirmed" {
					continue
				}
				other := ""
				if (direction == "out" || direction == "both") && v.FromRef == r {
					other = v.ToRef
				}
				if (direction == "in" || direction == "both") && v.ToRef == r {
					other = v.FromRef
				}
				if other == "" {
					continue
				}
				if !visited[other] && len(g.Nodes) >= 100 {
					g.Truncated = true
					return g, nil
				}
				if !edgeSeen[v.ID] {
					if len(g.Relations) >= 100 {
						g.Truncated = true
						return g, nil
					}
					edgeSeen[v.ID] = true
					g.Relations = append(g.Relations, v)
					g.Evidence[fmt.Sprintf("relation:%d", v.ID)] = relationReceipt(v)
				}
				if !visited[other] {
					if len(g.Nodes) >= 100 {
						g.Truncated = true
						return g, nil
					}
					visited[other] = true
					v2, e := s.Get(user, other)
					if e != nil {
						continue
					}
					g.Nodes = append(g.Nodes, v2)
					g.Evidence[other] = v2.Fingerprint
					next = append(next, other)
				}
			}
		}
		queue = next
		if len(queue) == 0 {
			break
		}
	}
	return g, nil
}
func exactToken(text, token string) bool {
	if len(token) < 3 {
		return false
	}
	pattern := `(?i)(^|[^\pL\pN_.:/-])` + regexp.QuoteMeta(token) + `($|[^\pL\pN_.:/-])`
	ok, _ := regexp.MatchString(pattern, text)
	return ok
}

type Discovery struct {
	Candidates []models.EnvironmentRelation `json:"candidates"`
	Scanned    int                          `json:"scanned"`
	Note       string                       `json:"note"`
}

func (s *Store) Discover(user int64, document string) (Discovery, error) {
	out := Discovery{Candidates: []models.EnvironmentRelation{}, Note: "仅按明确ident/资产序列号提取资料提及候选；不推断当前部署或依赖。只扫描本次可见目录最多100个候选对象。"}
	a, e := s.resolve(user, document, true)
	if e != nil {
		return out, e
	}
	if a.Kind != "document" {
		return out, bad("请选择已整理的正式资料版本")
	}
	_, id, _ := parse(document)
	k, e := (&knowledgebase.Store{DB: s.DB}).Version(user, id)
	if e != nil {
		return out, e
	}
	if len(k.Markdown) > 4<<20 {
		return out, bad("资料过大，请按小节人工关联")
	}
	all, e := s.Search(user, "")
	if e != nil {
		return out, e
	}
	if all.Truncated {
		out.Note += " 目录结果已截断，可在对象关联中人工补充。"
	}
	for _, v := range all.Items {
		if v.Kind == "document" {
			continue
		}
		if !v.Writable {
			continue
		}
		out.Scanned++
		tokens := []string{}
		if ident, ok := v.Attributes["ident"].(string); ok {
			tokens = append(tokens, ident)
		}
		if serial, ok := v.Attributes["serial"].(string); ok {
			tokens = append(tokens, serial)
		}
		matched := ""
		for _, t := range tokens {
			if exactToken(string(k.Markdown), t) {
				matched = t
				break
			}
		}
		if matched == "" {
			continue
		}
		r, e := s.add(user, RelationInput{FromRef: a.Ref, ToRef: v.Ref, Kind: "describes", FromVersion: a.Fingerprint, ToVersion: v.Fingerprint, Note: "正式资料原文精确包含标识 " + matched + "；仅表示历史资料提及，不能证明当前状态。", ValidUntil: now() + 90*86400}, "document_identifier", "candidate")
		if e != nil {
			continue
		}
		out.Candidates = append(out.Candidates, r)
	}
	return out, nil
}

type Timeline struct {
	Items     []Entity          `json:"items"`
	Evidence  map[string]string `json:"evidence"`
	Note      string            `json:"note"`
	Truncated bool              `json:"truncated"`
}

func (s *Store) Timeline(user int64, ref string) (Timeline, error) {
	out := Timeline{Items: []Entity{}, Evidence: map[string]string{}, Note: "最近7天、最多50条。告警只按精确TargetIdent关联；资产变更仅为CMDB台账审计，不代表全部部署/网络配置变更。"}
	ctx, e := s.Context(user, ref)
	if e != nil {
		return out, e
	}
	out.Evidence[ref] = ctx.Entity.Fingerprint
	ident, _ := ctx.Entity.Attributes["ident"].(string)
	if ident != "" {
		var alerts []models.AlertHisEvent
		if e = s.DB.Where("target_ident = ? AND trigger_time >= ?", ident, now()-7*86400).Order("id desc").Limit(51).Find(&alerts).Error; e != nil {
			return out, e
		}
		for _, a := range alerts {
			if _, e = opsauth.Check(s.DB, user, a.GroupId, false); e != nil {
				continue
			}
			if len(out.Items) >= 50 {
				out.Truncated = true
				break
			}
			v, e := s.Get(user, fmt.Sprintf("alert:%d", a.Id))
			if e != nil {
				continue
			}
			out.Items = append(out.Items, v)
			out.Evidence[v.Ref] = v.Fingerprint
		}
	}
	if ctx.Entity.Kind == "asset" {
		var changes []cmdb.Change
		if e = s.DB.Where("asset_id = ? AND created_at >= ?", strings.TrimPrefix(ref, "asset:"), time.Unix(now()-7*86400, 0)).Order("id desc").Limit(51).Find(&changes).Error; e != nil {
			return out, e
		}
		for _, a := range changes {
			if len(out.Items) >= 50 {
				out.Truncated = true
				break
			}
			v, e := s.Get(user, fmt.Sprintf("asset_change:%d", a.ID))
			if e != nil {
				continue
			}
			out.Items = append(out.Items, v)
			out.Evidence[v.Ref] = v.Fingerprint
		}
	}
	return out, nil
}
