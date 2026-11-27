package knowledgebase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ccfos/nightingale/v6/pkg/workbook"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ccfos/nightingale/v6/archives"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"github.com/google/uuid"
	"github.com/toolkits/pkg/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct {
	DB           *gorm.DB
	Archives     *archives.Service
	Summarize    func(context.Context, string) (string, error)
	SummaryModel func() string
}

func (s *Store) WithContext(ctx context.Context) *Store {
	copy := *s
	copy.DB = s.DB.WithContext(ctx)
	return &copy
}
func (s *Store) allowed(user int64) error {
	_, e := opsauth.User(s.DB, user)
	if e != nil {
		return opsauth.Fail(403, "knowledge_forbidden", "没有权限查阅")
	}
	return nil
}
func (s *Store) visible() *gorm.DB {
	return s.DB.Table("knowledge_document_version AS k").Joins("JOIN archive_document d ON d.id=k.document_id AND d.current_version_id=k.source_version_id").Joins("JOIN archive_record a ON a.id=d.archive_id").Where("d.deleted = ? AND a.deleted = ?", false, false)
}

// Scan reconciles current immutable versions. Repeated scans recover lost wakeups
// and backfill documents created before this module; editor drafts are excluded.
func (s *Store) Scan() error {
	var cursor int64
	for {
		var docs []models.ArchiveDocument
		if e := s.DB.Table("archive_document AS d").Select("d.*").Joins("JOIN archive_record a ON a.id=d.archive_id").Joins("LEFT JOIN knowledge_document_version known_version ON known_version.source_version_id=d.current_version_id").Where("d.deleted = ? AND a.deleted = ? AND d.id > ? AND d.current_version_id > 0 AND known_version.id IS NULL", false, false, cursor).Order("d.id").Limit(100).Find(&docs).Error; e != nil {
			return e
		}
		if len(docs) == 0 {
			return s.reconcileIndexes()
		}
		for _, d := range docs {
			var v models.ArchiveDocumentVersion
			if e := s.DB.Select("id,sequence,has_content").First(&v, "id = ? AND document_id = ?", d.CurrentVersionID, d.ID).Error; e != nil {
				return e
			}
			cursor = d.ID
			k := models.LibraryVersion{SourceVersionID: v.ID, DocumentID: d.ID, ArchiveID: d.ArchiveID, Title: d.Title, Sequence: v.Sequence, State: "queued", SummaryState: "pending", UpdatedAt: time.Now().Unix()}
			if !v.HasContent {
				k.State = "empty"
				k.SummaryState = "not_applicable"
			}
			if e := s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&k).Error; e != nil {
				return e
			}
		}
	}
}

// Parsing and generation have independent loops: a slow model never holds up
// discovery or deterministic source indexing. Per-version leases fence both.
func (s *Store) Start(ctx context.Context) func() {
	done := make(chan struct{}, 2)
	launch := func(parse bool) {
		go func() {
			defer func() { done <- struct{}{} }()
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for ctx.Err() == nil {
				if parse {
					if err := s.WithContext(ctx).Scan(); err != nil {
						logger.Warningf("[Knowledge] reconciliation failed: %v", err)
					}
				}
				for i := 0; i < 10 && ctx.Err() == nil; i++ {
					var worked bool
					var err error
					if parse {
						worked, err = s.WithContext(ctx).processSource(ctx)
					} else {
						worked, err = s.WithContext(ctx).processSummary(ctx)
					}
					if err != nil {
						logger.Warningf("[Knowledge] worker parse=%t failed: %v", parse, err)
						break
					}
					if !worked {
						break
					}
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	launch(true)
	launch(false)
	return func() { <-done; <-done }
}

// ProcessOne retains the synchronous entry point for callers and tests. Runtime
// workers deliberately use separate parse and generation entry points.
func (s *Store) ProcessOne(ctx context.Context) (bool, error) {
	worked, err := s.processSource(ctx)
	if err != nil {
		return worked, err
	}
	summary, err := s.processSummary(ctx)
	return worked || summary, err
}

func (s *Store) processSource(ctx context.Context) (bool, error) {
	now := time.Now().Unix()
	var k models.LibraryVersion
	q := s.visible().Where("k.state IN ? AND k.next_run <= ? AND k.lease_until < ?", []string{"queued", "retry"}, now, now).Order("k.id").Limit(1).Find(&k)
	if q.Error != nil {
		return false, q.Error
	}
	if k.ID == 0 {
		return false, nil
	}
	lease := uuid.NewString()
	res := s.DB.Model(&models.LibraryVersion{}).Where("id = ? AND lease_until < ?", k.ID, now).Updates(map[string]interface{}{"lease": lease, "lease_until": now + 180, "attempts": gorm.Expr("attempts + 1")})
	if res.Error != nil || res.RowsAffected == 0 {
		return false, res.Error
	}
	save := func(values map[string]interface{}) error {
		values["lease_until"] = int64(0)
		values["updated_at"] = time.Now().Unix()
		return s.DB.Model(&models.LibraryVersion{}).Where("id = ? AND lease = ? AND lease_until > ?", k.ID, lease, time.Now().Unix()).Updates(values).Error
	}
	fail := func(err error) error {
		state := "retry"
		summary := "retry"
		if errors.Is(err, ErrOCR) {
			state = "awaiting_ocr"
			summary = "not_applicable"
		}
		if k.Attempts >= 4 && state == "retry" {
			state = "failed"
		}
		return save(map[string]interface{}{"state": state, "summary_state": summary, "error": err.Error(), "next_run": now + 60*int64(k.Attempts+1)})
	}
	if k.State != "ready" {
		var d models.ArchiveDocument
		var v models.ArchiveDocumentVersion
		if e := s.DB.First(&d, k.DocumentID).Error; e != nil {
			return true, fail(e)
		}
		if e := s.DB.First(&v, k.SourceVersionID).Error; e != nil {
			return true, fail(e)
		}
		path := ""
		if v.BlobID > 0 {
			blob, p, e := s.Archives.Blob(v.BlobID)
			if e != nil {
				return true, fail(e)
			}
			if blob.DocumentID != d.ID || blob.ArchiveID != d.ArchiveID {
				return true, fail(fmt.Errorf("source ownership mismatch"))
			}
			f, e := os.Open(p)
			if e != nil {
				return true, fail(fmt.Errorf("source file unavailable"))
			}
			h := sha256.New()
			n, e := io.Copy(h, io.LimitReader(f, blob.Size+1))
			f.Close()
			if e != nil || n != blob.Size || hex.EncodeToString(h.Sum(nil)) != blob.SHA256 {
				return true, fail(fmt.Errorf("source checksum mismatch"))
			}
			path = p
		}
		extractCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
		var md string
		var parts []workbook.Section
		var structured []byte
		var e error
		if d.Kind == "xlsx" {
			var book *workbook.Book
			book, e = workbook.Parse(extractCtx, path)
			if e == nil {
				md, parts, e = book.Markdown()
			}
			if e == nil {
				structured, e = json.Marshal(book)
				if len(structured) > 16<<20 {
					e = fmt.Errorf("Excel 结构化内容超过 16 MiB")
				}
			}
		} else {
			md, e = Extract(extractCtx, d.Kind, path, v.Body)
			if e == nil {
				for _, p := range Split(md) {
					parts = append(parts, workbook.Section{Heading: p.Heading, Body: p.Body})
				}
			}
		}
		cancel()
		if e == nil {
			bounded := []workbook.Section{}
			for _, part := range parts {
				runes := []rune(part.Body)
				for offset := 0; offset < len(runes); offset += 6000 {
					end := offset + 6000
					if end > len(runes) {
						end = len(runes)
					}
					bounded = append(bounded, workbook.Section{Heading: part.Heading, Body: string(runes[offset:end])})
				}
			}
			parts = bounded
		}
		if e != nil {
			return true, fail(e)
		}
		sum := sha256.Sum256([]byte(md))
		e = s.DB.Transaction(func(tx *gorm.DB) error {
			var held models.LibraryVersion
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&held, k.ID).Error; e != nil {
				return e
			}
			if held.Lease != lease || held.LeaseUntil < time.Now().Unix() {
				return fmt.Errorf("index lease lost")
			}
			var current int64
			if err := (&Store{DB: tx}).visible().Where("k.id=?", k.ID).Count(&current).Error; err != nil {
				return err
			}
			if current != 1 {
				return fmt.Errorf("source version changed during parsing")
			}
			if e := tx.Where("version_id = ?", k.ID).Delete(&models.LibrarySection{}).Error; e != nil {
				return e
			}
			if e := tx.Where("version_id = ?", k.ID).Delete(&models.LibraryTerm{}).Error; e != nil {
				return e
			}
			if e := tx.Where("version_id = ?", k.ID).Delete(&models.LibraryDerivation{}).Error; e != nil {
				return e
			}
			for i, p := range parts {
				section := models.LibrarySection{VersionID: k.ID, Position: i + 1, Heading: p.Heading, Body: models.LibraryText(p.Body)}
				if e := tx.Create(&section).Error; e != nil {
					return e
				}
				if e := writeTerms(tx, section, d.Title); e != nil {
					return e
				}
			}
			if len(structured) > 0 {
				if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&models.LibraryWorkbook{VersionID: k.ID, Data: models.LibraryText(structured)}).Error; err != nil {
					return err
				}
			}
			return tx.Model(&held).Updates(map[string]interface{}{"state": "ready", "markdown": md, "digest": hex.EncodeToString(sum[:]), "parser_revision": parserRevision, "index_revision": indexRevision, "summary_state": "pending", "attempts": 0, "next_run": 0, "error": ""}).Error
		})
		if e != nil {
			return true, fail(e)
		}
		k.Markdown = models.LibraryText(md)
	}
	return true, save(map[string]interface{}{"next_run": 0, "attempts": 0})
}

func (s *Store) Retry(user, id int64) error { return s.requeue(user, id, nil) }

// Reprocess carries an explicit UI intent so completion racing with a retry
// cannot accidentally regenerate successful chapters. Retry retains its legacy
// behavior for existing callers.
func (s *Store) Reprocess(user, id int64, regenerate bool) error {
	return s.requeue(user, id, &regenerate)
}

func (s *Store) requeue(user, id int64, force *bool) error {
	if err := s.allowed(user); err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var k models.LibraryVersion
		if err := (&Store{DB: tx}).visible().Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "k"}}).Where("k.id=?", id).First(&k).Error; err != nil {
			return err
		}
		if k.LeaseUntil >= time.Now().Unix() {
			return opsauth.Fail(409, "knowledge_busy", "资料正在处理，请稍后重试")
		}
		state := "queued"
		if k.State == "ready" {
			state = "ready"
		}
		regenerate := k.State == "ready" && k.SummaryState == "ready"
		if force != nil {
			regenerate = *force
		}
		values := map[string]interface{}{"state": state, "summary_state": "pending", "attempts": 0, "next_run": 0, "error": ""}
		if regenerate {
			values["summary"] = ""
		}
		if err := tx.Model(&k).Updates(values).Error; err != nil {
			return err
		}
		derived := tx.Model(&models.LibraryDerivation{}).Where("version_id=?", id)
		reset := map[string]interface{}{"state": "pending", "attempts": 0, "next_run": 0, "error": ""}
		if regenerate {
			reset["content"] = ""
		} else {
			derived = derived.Where("state<>?", "ready")
		}
		return derived.Updates(reset).Error
	})
}
func (s *Store) List(user int64) ([]models.LibraryVersion, error) {
	if e := s.allowed(user); e != nil {
		return nil, e
	}
	items := []models.LibraryVersion{}
	e := s.visible().Select("k.id,k.source_version_id,k.document_id,k.archive_id,d.title,k.sequence,k.state,k.summary_state,k.error,k.updated_at,k.parser_revision,k.index_revision,k.summary_model,k.summary_recipe").Order("k.id DESC").Limit(500).Find(&items).Error
	return items, e
}

type Hit struct {
	SectionID       int64  `json:"section_id"`
	ID              int64  `json:"id"`
	VersionID       int64  `json:"version_id"`
	DocumentID      int64  `json:"document_id"`
	ArchiveID       int64  `json:"archive_id"`
	SourceVersionID int64  `json:"source_version_id"`
	Sequence        int64  `json:"sequence"`
	Title           string `json:"title"`
	Heading         string `json:"heading"`
	Body            string `json:"body"`
	URL             string `json:"url"`
	Score           int    `json:"-"`
}

// MarshalJSON provides a ready-to-copy citation so internal locator IDs are
// not confused with human version numbers or original-source URLs.
func (h Hit) MarshalJSON() ([]byte, error) {
	type record Hit
	return json.Marshal(struct {
		record
		Citation string `json:"citation"`
	}{record(h), fmt.Sprintf("[%s · V%d](%s)", h.Title, h.Sequence, h.URL)})
}

func tokens(q string) []string {
	r := []rune(strings.ToLower(q))
	set := map[string]bool{}
	if len(r) == 1 && unicode.Is(unicode.Han, r[0]) {
		return []string{string(r)}
	}
	var latin strings.Builder
	flush := func() {
		if latin.Len() > 1 {
			set[latin.String()] = true
		}
		latin.Reset()
	}
	for i, c := range r {
		if unicode.Is(unicode.Han, c) {
			flush()
			if i+1 < len(r) && unicode.Is(unicode.Han, r[i+1]) {
				set[string(r[i:i+2])] = true
			}
		} else if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '-' {
			latin.WriteRune(c)
		} else {
			flush()
		}
	}
	flush()
	result := []string{}
	stop := map[string]bool{"什么": true, "怎么": true, "请问": true, "是否": true, "哪些": true, "资料": true, "相关": true, "一下": true, "说明": true, "如何": true, "需要": true, "可以": true, "内容": true, "检索": true, "查询": true, "回答": true, "引用": true, "是什": true, "请引": true, "多少": true}
	for t := range set {
		if !stop[t] {
			result = append(result, t)
		}
	}
	sort.Strings(result)
	if len(result) > 24 {
		result = result[:24]
	}
	return result
}

var specificKnowledgeIdentifier = regexp.MustCompile(`(?i)\b(?:[a-z][a-z0-9_-]*[0-9][a-z0-9_-]*|[0-9]+[a-z][a-z0-9_-]*|(?:[0-9]{1,3}\.){3}[0-9]{1,3}|[0-9]{9,64})\b`)

func (s *Store) Search(user int64, q string) ([]Hit, error) {
	return s.SearchScoped(user, q, "", 0)
}

// SearchScoped narrows discovery to an explicit title/version without changing
// source visibility. It never falls back to unrelated documents on no match.
func (s *Store) SearchScoped(user int64, q, title string, version int64) ([]Hit, error) {
	return s.SearchWithin(user, q, version, SourceScope{Title: title})
}

// SearchWithin preserves constraints from the real user question even when a
// model rewrites a query to a prefix, synonym, or another identifier.
func (s *Store) SearchWithin(user int64, q string, version int64, scope SourceScope) ([]Hit, error) {
	title := scope.Title
	if len(scope.Identifiers) > 8 {
		return nil, fmt.Errorf("too many literal identifiers")
	}
	for _, id := range scope.Identifiers {
		if len(id) > 200 {
			return nil, fmt.Errorf("literal identifier exceeds limit")
		}
	}
	if version < 0 || len(title) > 500 {
		return nil, opsauth.Fail(422, "invalid_scope", "资料查询范围无效")
	}
	if len(q) > 1000 {
		return nil, opsauth.Fail(422, "query_large", "Search query is too long")
	}
	if e := s.allowed(user); e != nil {
		return nil, e
	}
	ts := tokens(q)
	identifierOnly := len(ts) == 0
	if identifierOnly {
		for _, id := range scope.Identifiers {
			ts = append(ts, strings.ToLower(id))
		}
	}
	if len(ts) == 0 {
		return []Hit{}, nil
	}
	db := s.visible().Joins("JOIN knowledge_document_section c ON c.version_id=k.id").Where("k.state = ? AND (k.index_revision <> ? OR d.title <> k.title)", "ready", indexRevision)
	if title != "" {
		db = exactTitleScope(db, title)
	}
	if version > 0 {
		db = db.Where("k.id = ?", version)
	}
	// Prefer exact identifier matches before the bounded window, without hard
	// exclusion: V2/A12 can be version/cell notation rather than equipment IDs.
	ids := specificKnowledgeIdentifier.FindAllString(strings.ToLower(q), -1)
	if identifierOnly {
		ids = append(ids, scope.Identifiers...)
	}
	required := scope.Identifiers
	if len(required) == 0 {
		required = strongIdentifiers(ids)
	}
	if strong := required; len(strong) > 0 {
		clauses := []string{}
		values := []interface{}{}
		for _, id := range strong {
			pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(id)) + "%"
			bodyColumn := "c.body"
			if len(scope.Identifiers) > 0 {
				bodyColumn = "k.markdown"
			}
			clauses = append(clauses, "(LOWER("+bodyColumn+") LIKE ? ESCAPE '!' OR LOWER(d.title) LIKE ? ESCAPE '!')")
			values = append(values, pattern, pattern)
		}
		db = db.Where(strings.Join(clauses, " OR "), values...)
	}
	columns := "c.id,c.version_id,k.document_id,k.archive_id,k.source_version_id,k.sequence,d.title,c.heading,c.body"
	selectArgs := []interface{}{}
	if len(ids) > 0 {
		clauses := []string{}
		args := []interface{}{}
		for _, id := range ids {
			pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(id) + "%"
			clauses = append(clauses, "(LOWER(c.body) LIKE ? ESCAPE '!' OR LOWER(d.title) LIKE ? ESCAPE '!')")
			args = append(args, pattern, pattern)
		}
		columns += ", CASE WHEN " + strings.Join(clauses, " OR ") + " THEN 1 ELSE 0 END AS identifier_rank"
		selectArgs = args
		db = db.Order("identifier_rank DESC")
	}
	clauses := []string{}
	args := []interface{}{}
	for _, t := range ts {
		clauses = append(clauses, "(LOWER(c.body) LIKE ? OR LOWER(d.title) LIKE ?)")
		pattern := "%" + strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(t, "!", "!!"), "%", "!%"), "_", "!_") + "%" // use an explicit portable LIKE escape
		clauses[len(clauses)-1] = "(LOWER(c.body) LIKE ? ESCAPE '!' OR LOWER(d.title) LIKE ? ESCAPE '!')"
		args = append(args, pattern, pattern)
	}
	hits := []Hit{}
	e := db.Where(strings.Join(clauses, " OR "), args...).Select(columns, selectArgs...).Order("k.id DESC,c.position").Limit(1000).Scan(&hits).Error
	if e != nil {
		return nil, e
	}
	indexed, err := s.indexedHits(q, title, version, ts, ids, required, len(scope.Identifiers) > 0)
	if err != nil {
		return nil, err
	}
	hits = append(indexed, hits...)
	for i := range hits {
		h := &hits[i]
		h.Score = 0
		for _, id := range ids {
			if strings.Contains(strings.ToLower(h.Title+" "+h.Body), id) {
				h.Score += 50
				break
			}
		}
		for _, t := range ts {
			if strings.Contains(strings.ToLower(h.Title), t) {
				h.Score += 5
			}
			if strings.Contains(strings.ToLower(h.Heading), t) {
				h.Score += 3
			}
			if strings.Contains(strings.ToLower(h.Body), t) {
				h.Score++
			}
		}
		h.SectionID = h.ID
		h.URL = fmt.Sprintf("/archives/%d/documents/%d?version=%d", h.ArchiveID, h.DocumentID, h.SourceVersionID)
		r := []rune(h.Body)
		if len(r) > 800 {
			start := 0
			lower := strings.ToLower(h.Body)
			for _, term := range ts {
				at := strings.Index(lower, term)
				if at >= 0 {
					pos := len([]rune(lower[:at]))
					if pos > 120 {
						start = pos - 120
					}
					break
				}
			}
			end := start + 800
			if end > len(r) {
				end = len(r)
			}
			h.Body = string(r[start:end]) + "…（请读取章节全文）"
		}

	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	// Suppress weak incidental matches and avoid one long report occupying all
	// candidates. This is deterministic lexical scoring, not semantic reranking.
	selected := []Hit{}
	counts := map[int64]int{}
	minimum := 1
	if title == "" && version == 0 && len(hits) > 0 && hits[0].Score > 4 {
		minimum = (hits[0].Score + 1) / 2
	}
	perDocument := 2
	if title != "" || version > 0 {
		perDocument = 6
	}
	for _, h := range hits {
		if h.Score < minimum || counts[h.DocumentID] >= perDocument {
			continue
		}
		selected = append(selected, h)
		counts[h.DocumentID]++
		if len(selected) == 6 {
			break
		}
	}
	return selected, nil
}
func (s *Store) Read(user, section int64) (*Hit, error) {
	if e := s.allowed(user); e != nil {
		return nil, e
	}
	var h Hit
	e := s.visible().Joins("JOIN knowledge_document_section c ON c.version_id=k.id").Where("c.id = ? AND k.state = ?", section, "ready").Select("c.id,c.version_id,k.document_id,k.archive_id,k.source_version_id,k.sequence,d.title,c.heading,c.body").Limit(1).Scan(&h).Error
	if e != nil {
		return nil, e
	}
	if h.ID == 0 {
		return nil, opsauth.Fail(404, "knowledge_missing", "未找到相关资料，或没有权限查阅")
	}
	h.SectionID = h.ID
	h.URL = fmt.Sprintf("/archives/%d/documents/%d?version=%d", h.ArchiveID, h.DocumentID, h.SourceVersionID)
	return &h, nil
}
func (s *Store) Version(user, id int64) (*models.LibraryVersion, error) {
	if e := s.allowed(user); e != nil {
		return nil, e
	}
	var k models.LibraryVersion
	e := s.visible().Where("k.id = ?", id).First(&k).Error
	if e == nil {
		var d models.ArchiveDocument
		if err := s.DB.First(&d, k.DocumentID).Error; err != nil {
			return nil, err
		}
		k.Title = d.Title
		if s.SummaryModel != nil && (k.SummaryModel != s.summaryModel() || k.SummaryRecipe != summaryRecipe) {
			k.Summary = ""
			k.SummaryState = "pending"
		}
		if k.ParserRevision == "" && k.Summary != "" {
			k.Summary = models.LibraryText(summaryCoverage(string(k.Markdown), string(k.Summary)))
		}
	}
	return &k, e
}

// Relevant is shared by archive and procedure retrieval; it deliberately does
// not infer applicability, root cause or semantic similarity.
func Relevant(query, text string) bool {
	for _, t := range tokens(query) {
		if strings.Contains(strings.ToLower(text), t) {
			return true
		}
	}
	return false
}

// Coverage is a server assertion about the actual v1 model input, never an
// instruction that the model is trusted to repeat. It also labels older rows.
func summaryCoverage(markdown, summary string) string {
	if len([]rune(markdown)) > 14000 && !strings.HasPrefix(summary, "> 覆盖范围（系统标记）：此摘要仅整理原文前 14000 字符，全文仍可分节检索，请结合原件核对。") {
		return "> 覆盖范围（系统标记）：此摘要仅整理原文前 14000 字符，全文仍可分节检索，请结合原件核对。\n\n" + summary
	}
	return summary
}
