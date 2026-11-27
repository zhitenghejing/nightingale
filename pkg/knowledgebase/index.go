package knowledgebase

import (
	"sort"
	"strings"
	"unicode"

	"github.com/ccfos/nightingale/v6/models"
	"gorm.io/gorm"
)

// Document tokenization has no query-term cap. A posting is a full normalized
// Latin identifier, a Chinese bigram, or an exact mixed equipment/IP identifier.
func indexTokens(text string) []string {
	r := []rune(strings.ToLower(text))
	set := map[string]bool{}
	var word strings.Builder
	flush := func() {
		if word.Len() > 0 {
			set[word.String()] = true
		}
		word.Reset()
	}
	for i, c := range r {
		if unicode.Is(unicode.Han, c) {
			flush()
			if i+1 < len(r) && unicode.Is(unicode.Han, r[i+1]) {
				set[string(r[i:i+2])] = true
			}
			set[string(c)] = true
		} else if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '-' {
			word.WriteRune(c)
		} else {
			flush()
		}
	}
	flush()
	for _, id := range specificKnowledgeIdentifier.FindAllString(strings.ToLower(text), -1) {
		set[id] = true
	}
	out := make([]string, 0, len(set))
	for token := range set {
		out = append(out, token)
	}
	sort.Strings(out)
	return out
}
func writeTerms(tx *gorm.DB, section models.LibrarySection, title string) error {
	weights := map[string]int{}
	for _, field := range []struct {
		text   string
		weight int
	}{{string(section.Body), 1}, {section.Heading, 3}, {title, 5}} {
		for _, term := range indexTokens(field.text) {
			weights[term] += field.weight
		}
	}
	items := make([]models.LibraryTerm, 0, len(weights))
	for term, weight := range weights {
		items = append(items, models.LibraryTerm{Term: digestText(term), SectionID: section.ID, VersionID: section.VersionID, Weight: weight})
	}
	if len(items) == 0 {
		return nil
	}
	return tx.CreateInBatches(items, 100).Error
}

// Relevance is computed in the index before the candidate window. Readers use
// SQL common to MySQL/PostgreSQL/SQLite; source ACL/current-version joins remain
// authoritative even while reconciliation or rebuild is pending.
// Long serials and full IPs are literal evidence constraints. Short V2/A12
// tokens remain soft because they are often versions or cell coordinates.
func strongIdentifiers(ids []string) []string {
	out := []string{}
	for _, id := range ids {
		digits, letters := 0, 0
		for _, c := range id {
			if unicode.IsDigit(c) {
				digits++
			}
			if unicode.IsLetter(c) {
				letters++
			}
		}
		if strings.Count(id, ".") == 3 || (len(id) >= 6 && digits >= 4 && letters >= 2) || (len(id) >= 9 && digits == len(id)) {
			out = append(out, id)
		}
	}
	return out
}

func (s *Store) indexedHits(q, title string, version int64, ts, ids, required []string, documentScope bool) ([]Hit, error) {
	terms := map[string]int{}
	for _, term := range ts {
		terms[digestText(term)] = 1
	}
	for _, id := range ids {
		terms[digestText(strings.ToLower(id))] = 50
	}
	keys := []string{}
	for key := range terms {
		keys = append(keys, key)
	}
	ranking := s.DB.Table("knowledge_document_term t").Select("t.section_id, SUM(t.weight) AS relevance").Where("t.term IN ?", keys).Group("t.section_id")
	if len(ids) > 0 {
		hashes := []string{}
		for _, id := range ids {
			hashes = append(hashes, digestText(strings.ToLower(id)))
		}
		ranking = s.DB.Table("knowledge_document_term t").Select("t.section_id, SUM(t.weight + CASE WHEN t.term IN ? THEN 50 ELSE 0 END) AS relevance", hashes).Where("t.term IN ?", keys).Group("t.section_id")
	}
	if strong := required; len(strong) > 0 && !documentScope {
		hashes := []string{}
		for _, id := range strong {
			hashes = append(hashes, digestText(strings.ToLower(id)))
		}
		ranking = ranking.Having("SUM(CASE WHEN t.term IN ? THEN 1 ELSE 0 END)>0", hashes)
	}
	db := s.visible().Joins("JOIN knowledge_document_section c ON c.version_id=k.id").Joins("JOIN (?) ranking ON ranking.section_id=c.id", ranking).Where("k.state=? AND k.index_revision=?", "ready", indexRevision)
	if documentScope && len(required) > 0 {
		hashes := []string{}
		for _, id := range required {
			hashes = append(hashes, digestText(strings.ToLower(id)))
		}
		matches := s.DB.Model(&models.LibraryTerm{}).Select("version_id").Where("term IN ?", hashes)
		db = db.Where("k.id IN (?)", matches)
	}
	if title != "" {
		db = exactTitleScope(db, title)
	}
	if version > 0 {
		db = db.Where("k.id=?", version)
	}
	hits := []Hit{}
	err := db.Select("c.id,c.version_id,k.document_id,k.archive_id,k.source_version_id,k.sequence,d.title,c.heading,c.body,ranking.relevance AS score").Order("ranking.relevance DESC,k.id DESC,c.position").Limit(1000).Scan(&hits).Error
	return hits, err
}
