package knowledgebase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ccfos/nightingale/v6/archives"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct {
	DB        *gorm.DB
	Archives  *archives.Service
	Summarize func(context.Context, string) (string, error)
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
		if e := s.DB.Table("archive_document AS d").Select("d.*").Joins("JOIN archive_record a ON a.id=d.archive_id").Where("d.deleted = ? AND a.deleted = ? AND d.id > ? AND d.current_version_id > 0", false, false, cursor).Order("d.id").Limit(100).Find(&docs).Error; e != nil {
			return e
		}
		if len(docs) == 0 {
			return nil
		}
		for _, d := range docs {
			var v models.ArchiveDocumentVersion
			if e := s.DB.First(&v, "id = ? AND document_id = ?", d.CurrentVersionID, d.ID).Error; e != nil {
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
func (s *Store) Start(ctx context.Context) func() {
	done := make(chan struct{})
	worker := *s
	worker.DB = s.DB.WithContext(ctx)
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			_ = worker.Scan()
			for i := 0; i < 10 && ctx.Err() == nil; i++ {
				worked, _ := worker.ProcessOne(ctx)
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
	return func() { <-done }
}

func (s *Store) ProcessOne(ctx context.Context) (bool, error) {
	now := time.Now().Unix()
	var k models.LibraryVersion
	q := s.visible().Where("k.state IN ? AND k.next_run <= ? AND k.lease_until < ?", []string{"queued", "retry", "ready"}, now, now).Where("k.state <> ? OR k.summary_state IN ?", "ready", []string{"pending", "retry"}).Order("k.id").Limit(1).Find(&k)
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
		md, e := Extract(extractCtx, d.Kind, path, v.Body)
		cancel()
		if e != nil {
			return true, fail(e)
		}
		sum := sha256.Sum256([]byte(md))
		parts := Split(md)
		e = s.DB.Transaction(func(tx *gorm.DB) error {
			var held models.LibraryVersion
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&held, k.ID).Error; e != nil {
				return e
			}
			if held.Lease != lease || held.LeaseUntil < time.Now().Unix() {
				return fmt.Errorf("index lease lost")
			}
			if e := tx.Where("version_id = ?", k.ID).Delete(&models.LibrarySection{}).Error; e != nil {
				return e
			}
			for i, p := range parts {
				if e := tx.Create(&models.LibrarySection{VersionID: k.ID, Position: i + 1, Heading: p.Heading, Body: models.LibraryText(p.Body)}).Error; e != nil {
					return e
				}
			}
			return tx.Model(&held).Updates(map[string]interface{}{"state": "ready", "markdown": md, "digest": hex.EncodeToString(sum[:]), "error": ""}).Error
		})
		if e != nil {
			return true, fail(e)
		}
		k.Markdown = models.LibraryText(md)
	}
	// Summary failure never removes the deterministic source index. A separate
	// retry budget prevents repeatedly parsing an already indexed document.
	if s.Summarize == nil {
		return true, save(map[string]interface{}{"summary_state": "pending", "next_run": now + 60})
	}
	summaryCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
	text := string(k.Markdown)
	if len([]rune(text)) > 14000 {
		text = string([]rune(text)[:14000]) + "\n[仅整理前14000字符，全文请按章节检索]"
	}
	summary, e := s.Summarize(summaryCtx, text)
	cancel()
	if e != nil {
		state := "retry"
		if k.Attempts >= 4 {
			state = "failed"
		}
		return true, save(map[string]interface{}{"summary_state": state, "next_run": now + 60*int64(k.Attempts+1), "error": "AI 整理暂未完成，原文仍可检索"})
	}
	if strings.TrimSpace(summary) == "" || len(summary) > 64000 {
		return true, save(map[string]interface{}{"summary_state": "failed", "error": "AI summary is empty or oversized"})
	}
	summary = summaryCoverage(string(k.Markdown), summary)
	return true, save(map[string]interface{}{"summary_state": "ready", "summary": summary, "error": ""})
}
func (s *Store) Retry(user, id int64) error {
	if e := s.allowed(user); e != nil {
		return e
	}
	var k models.LibraryVersion
	if e := s.visible().Where("k.id = ?", id).First(&k).Error; e != nil {
		return e
	}
	state := "queued"
	if k.State == "ready" {
		state = "ready"
	}
	res := s.DB.Model(&k).Where("lease_until < ?", time.Now().Unix()).Updates(map[string]interface{}{"state": state, "summary_state": "pending", "attempts": 0, "next_run": 0, "error": ""})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return opsauth.Fail(409, "knowledge_busy", "资料正在处理，请稍后重试")
	}
	return nil
}
func (s *Store) List(user int64) ([]models.LibraryVersion, error) {
	if e := s.allowed(user); e != nil {
		return nil, e
	}
	items := []models.LibraryVersion{}
	e := s.visible().Select("k.id,k.source_version_id,k.document_id,k.archive_id,d.title,k.sequence,k.state,k.summary_state,k.error,k.updated_at").Order("k.id DESC").Limit(500).Find(&items).Error
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
func (s *Store) Search(user int64, q string) ([]Hit, error) {
	if len(q) > 1000 {
		return nil, opsauth.Fail(422, "query_large", "Search query is too long")
	}
	if e := s.allowed(user); e != nil {
		return nil, e
	}
	ts := tokens(q)
	if len(ts) == 0 {
		return []Hit{}, nil
	}
	db := s.visible().Joins("JOIN knowledge_document_section c ON c.version_id=k.id").Where("k.state = ?", "ready")
	clauses := []string{}
	args := []interface{}{}
	for _, t := range ts {
		clauses = append(clauses, "(LOWER(c.body) LIKE ? OR LOWER(d.title) LIKE ?)")
		pattern := "%" + strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(t, "!", "!!"), "%", "!%"), "_", "!_") + "%" // use an explicit portable LIKE escape
		clauses[len(clauses)-1] = "(LOWER(c.body) LIKE ? ESCAPE '!' OR LOWER(d.title) LIKE ? ESCAPE '!')"
		args = append(args, pattern, pattern)
	}
	hits := []Hit{}
	e := db.Where(strings.Join(clauses, " OR "), args...).Select("c.id,c.version_id,k.document_id,k.archive_id,k.source_version_id,k.sequence,d.title,c.heading,c.body").Order("k.id DESC,c.position").Limit(1000).Scan(&hits).Error
	if e != nil {
		return nil, e
	}
	for i := range hits {
		h := &hits[i]
		for _, t := range ts {
			if strings.Contains(strings.ToLower(h.Title), t) {
				h.Score += 5
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
	if len(hits) > 0 && hits[0].Score > 4 {
		minimum = (hits[0].Score + 1) / 2
	}
	for _, h := range hits {
		if h.Score < minimum || counts[h.DocumentID] >= 2 {
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
	if e == nil && k.Summary != "" {
		k.Summary = models.LibraryText(summaryCoverage(string(k.Markdown), string(k.Summary)))
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
