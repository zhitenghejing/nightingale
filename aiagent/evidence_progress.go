package aiagent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// Progress is scoped to evidence tools. Operational actions, skill loading and
// tools without reliable receipts keep their existing loop semantics.
type evidenceProgress struct {
	seen                 map[string]bool
	argumentErrors       map[string]bool
	source, other, added bool
	misses               int
}

func newEvidenceProgress() *evidenceProgress {
	return &evidenceProgress{seen: map[string]bool{}, argumentErrors: map[string]bool{}}
}
func (p *evidenceProgress) beginRound()       { p.source = false; p.other = false; p.added = false }
func (p *evidenceProgress) hasEvidence() bool { return len(p.seen) > 0 }
func (p *evidenceProgress) observe(name, args, raw string) {
	atoms, known := evidenceAtoms(name, args, raw)
	if !known {
		p.other = true
		return
	}
	if strings.HasPrefix(raw, "Error: tool ") && strings.Contains(raw, " parameter ") && strings.Contains(raw, " must be ") {
		key := name + "\x00" + raw
		if !p.argumentErrors[key] {
			p.argumentErrors[key] = true
			// One correction opportunity per stable argument error, independent
			// of its value. A first type error must not consume the last chance
			// immediately after an otherwise redundant successful read.
			p.other = true
			return
		}
	}
	p.source = true
	for _, atom := range atoms {
		if !p.seen[atom] {
			p.seen[atom] = true
			p.added = true
		}
	}
}
func (p *evidenceProgress) endRound() bool {
	if !p.source || p.other {
		p.misses = 0
		return false
	}
	if p.added {
		p.misses = 0
	} else {
		p.misses++
	}
	return true
}
func (p *evidenceProgress) stalled() bool { return p.misses >= 2 }
func (p *evidenceProgress) warn() bool    { return p.misses == 1 }
func evidenceAtom(value interface{}) string {
	b, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func evidenceAtoms(name, args, raw string) ([]string, bool) {
	source := name == "search_knowledge" || name == "read_knowledge" || IsKnowledgeWorkbookTool(name) || IsKnowledgeDataTool(name)
	if !source {
		return nil, false
	}
	var a map[string]interface{}
	_ = json.Unmarshal([]byte(args), &a)
	if name == "query_knowledge_data" && a["operation"] != "asset_lookup" && a["operation"] != "target_statistics" {
		return nil, false
	}
	if strings.HasPrefix(raw, "Error:") {
		return nil, true
	}
	var v map[string]interface{}
	if json.Unmarshal([]byte(raw), &v) != nil {
		return nil, true
	}
	atoms := []string{}
	add := func(kind string, value interface{}) { atoms = append(atoms, kind+":"+evidenceAtom(value)) }
	addRows := func(version, sheet, value interface{}) {
		rows, _ := value.([]interface{})
		for _, row := range rows {
			cells, _ := row.([]interface{})
			for _, cell := range cells {
				add("cell", []interface{}{version, sheet, cell})
			}
		}
	}
	if name == "search_knowledge" {
		for _, field := range []string{"documents", "read_documents"} {
			list, _ := v[field].([]interface{})
			for _, item := range list {
				m, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				if field == "documents" {
					add("candidate", m["id"])
				} else {
					// Sections are immutable source units. A verbose reread of an already
					// observed section is not a new source; workbook/platform values
					// are tracked separately below.
					add("section", m["id"])
				}
			}
		}
		if records, ok := v["matched_records"].([]interface{}); ok {
			for _, r := range records {
				add("matched_record", r)
				if record, ok := r.(map[string]interface{}); ok {
					if result, ok := record["result"].(map[string]interface{}); ok {
						addRows(record["version_id"], result["sheet"], result["rows"])
					}
					addRows(record["version_id"], record["sheet"], record["context_rows"])
				}
			}
		}
		if len(atoms) == 0 {
			add("empty_search_scope", []interface{}{a["document_title"], a["version_id"]})
		}
		return atoms, true
	}
	if name == "read_knowledge" {
		if v["id"] != nil {
			add("section", v["id"])
		} else {
			add("procedure", []interface{}{v["version"], v["content"]})
		}
		return atoms, true
	}
	if IsKnowledgeDataTool(name) {
		add("platform", []interface{}{v["operation"], v["query"], v["asset_lookup_serial"], v["asset_lookup_id"], v["result"]})
		return atoms, true
	}
	// Query coordinates/filters define legitimate new scopes; repeated pages
	// count as progress only when they contain newly returned cell values.
	result, _ := v["result"].(map[string]interface{})
	rows, _ := result["rows"].([]interface{})
	for _, row := range rows {
		cells, _ := row.([]interface{})
		for _, cell := range cells {
			add("cell", []interface{}{v["workbook_version"], result["sheet"], cell})
		}
	}
	if len(rows) == 0 || result["operation"] != "read" {
		add("workbook_scope", []interface{}{v["workbook_version"], result["operation"], result["sheet"], result["range"], result["filter"], result["sum"], result["matched_rows"], result["sheets"]})
	}
	return atoms, true
}
