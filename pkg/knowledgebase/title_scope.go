package knowledgebase

import (
	"gorm.io/gorm"
	"regexp"
	"strings"
)

// DocumentTitleMatches permits only an exact title or the exact filename after
// the archive's display prefix. A substring is not a source identity.
func DocumentTitleMatches(stored, requested string) bool {
	if strings.EqualFold(stored, requested) {
		return true
	}
	if strings.HasPrefix(stored, "[") && !strings.HasPrefix(requested, "[") {
		if end := strings.Index(stored, "] "); end >= 0 {
			return strings.EqualFold(stored[end+2:], requested)
		}
	}
	return false
}

func exactTitleScope(db *gorm.DB, title string) *gorm.DB {
	escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(title))
	if strings.HasPrefix(title, "[") {
		return db.Where("LOWER(d.title) = ?", strings.ToLower(title))
	}
	return db.Where("(LOWER(d.title) = ? OR LOWER(d.title) LIKE ? ESCAPE '!')", strings.ToLower(title), "[%] "+escaped)
}

// UniqueTitleVersion refuses ambiguous source names rather than selecting the
// version the model happened to query. It uses the same current-source scope.
func (s *Store) UniqueTitleVersion(user int64, title string) (int64, error) {
	if err := s.allowed(user); err != nil {
		return 0, err
	}
	ids := []int64{}
	err := exactTitleScope(s.visible(), title).Where("k.state = ?", "ready").Limit(2).Pluck("k.id", &ids).Error
	if err != nil || len(ids) != 1 {
		return 0, err
	}
	return ids[0], nil
}

// SourceScope narrows document identity independently of authorization. Multiple
// literal identifiers form a discovery union, supporting cross-source comparison.
// It never establishes that a particular claim is supported by a document.
type SourceScope struct {
	Title       string
	Identifiers []string
}

func LiteralIdentifiers(question string) []string {
	return strongIdentifiers(specificKnowledgeIdentifier.FindAllString(strings.ToLower(question), -1))
}

func (scope SourceScope) Matches(title, body string) bool {
	if scope.Title != "" && !DocumentTitleMatches(title, scope.Title) {
		return false
	}
	if len(scope.Identifiers) == 0 {
		return true
	}
	text := strings.ToLower(title + "\n" + body)
	for _, id := range scope.Identifiers {
		pattern := `(^|[^a-z0-9_-])` + regexp.QuoteMeta(strings.ToLower(id)) + `($|[^a-z0-9_-])`
		if regexp.MustCompile(pattern).MatchString(text) {
			return true
		}
	}
	return false
}
