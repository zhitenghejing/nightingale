package knowledgebase

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const parserRevision = "archive-v2"
const indexRevision = "lexical-v1"
const summaryRecipe = "section-summary-v2"

func digestText(text string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(text))) }
func (s *Store) summaryModel() string {
	if s.SummaryModel != nil {
		return s.SummaryModel()
	}
	return "configured-model"
}

// Rebuild old indexes and renamed title postings from source sections. This is
// transactional and does not call a model or modify the immutable archive.
func (s *Store) reconcileIndexes() error {
	var cursor int64
	rebuilt := 0
	for {
		var rows []struct {
			models.LibraryVersion
			CurrentTitle string
		}
		if err := s.visible().Where("k.state = ? AND k.id > ?", "ready", cursor).Select("k.id,k.title,k.parser_revision,k.index_revision,k.summary_model,k.summary_recipe,d.title AS current_title").Order("k.id").Limit(100).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, row := range rows {
			cursor = row.ID
			if row.ParserRevision != parserRevision {
				if rebuilt >= 10 {
					return nil
				}
				// The same lease protects extraction and summary work. Withdraw
				// outdated derived text atomically, without stealing an active job;
				// processSource replaces sections/workbook from the immutable source.
				res := s.DB.Model(&models.LibraryVersion{}).Where("id=? AND state=? AND COALESCE(parser_revision, '')=? AND COALESCE(lease_until, 0)<?", row.ID, "ready", row.ParserRevision, time.Now().Unix()).Updates(map[string]interface{}{"state": "queued", "summary_state": "pending", "markdown": "", "digest": "", "summary": "", "index_revision": "", "attempts": 0, "next_run": 0, "error": "解析器版本已更新，已重新排队解析", "lease_until": 0})
				if res.Error != nil {
					return res.Error
				}
				if res.RowsAffected > 0 {
					rebuilt++
				}
				continue
			}
			if row.IndexRevision == indexRevision && row.Title == row.CurrentTitle {
				if s.Summarize != nil && (row.SummaryModel != s.summaryModel() || row.SummaryRecipe != summaryRecipe) {
					if err := s.DB.Model(&models.LibraryVersion{}).Where("id=? AND lease_until<?", row.ID, time.Now().Unix()).Updates(map[string]interface{}{"summary_state": "pending", "summary": "", "next_run": 0, "summary_model": s.summaryModel(), "summary_recipe": summaryRecipe}).Error; err != nil {
						return err
					}
				}
				continue
			}
			if rebuilt >= 10 {
				return nil
			}
			rebuilt++
			if err := s.DB.Transaction(func(tx *gorm.DB) error {
				var held models.LibraryVersion
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&held, row.ID).Error; err != nil {
					return err
				}
				if held.LeaseUntil >= time.Now().Unix() {
					return nil
				}
				var sections []models.LibrarySection
				if err := tx.Where("version_id=?", row.ID).Order("position").Find(&sections).Error; err != nil {
					return err
				}
				if err := tx.Where("version_id=?", row.ID).Delete(&models.LibraryTerm{}).Error; err != nil {
					return err
				}
				for _, section := range sections {
					if err := writeTerms(tx, section, row.CurrentTitle); err != nil {
						return err
					}
				}
				values := map[string]interface{}{"title": row.CurrentTitle, "index_revision": indexRevision}
				if held.IndexRevision != indexRevision {
					values["summary"] = ""
					values["summary_state"] = "pending"
					values["next_run"] = 0
				}
				return tx.Model(&held).Updates(values).Error
			}); err != nil {
				return err
			}
		}
	}
}

// One source section per job makes completed chapters resumable. No model input
// is silently limited to the beginning of the document.
func (s *Store) processSummary(ctx context.Context) (bool, error) {
	if s.Summarize == nil {
		return false, nil
	}
	now := time.Now().Unix()
	var k models.LibraryVersion
	err := s.visible().Where("k.state=? AND k.summary_state IN ? AND k.next_run<=? AND k.lease_until<?", "ready", []string{"pending", "retry"}, now, now).Order("k.updated_at,k.id").First(&k).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lease := uuid.NewString()
	res := s.DB.Model(&k).Where("lease_until<?", now).Updates(map[string]interface{}{"lease": lease, "lease_until": now + 180})
	if res.Error != nil || res.RowsAffected != 1 {
		return false, res.Error
	}
	defer s.DB.Model(&models.LibraryVersion{}).Where("id=? AND lease=?", k.ID, lease).Update("lease_until", 0)
	var sections []models.LibrarySection
	if err := s.DB.Where("version_id=?", k.ID).Order("position").Find(&sections).Error; err != nil {
		return true, err
	}
	if len(sections) == 0 {
		// A missing derived index is repairable from the immutable source. Do not
		// leave a poison job at the head of the generation queue.
		return true, s.DB.Model(&models.LibraryVersion{}).Where("id=? AND lease=?", k.ID, lease).Updates(map[string]interface{}{"state": "queued", "summary_state": "pending", "summary": "", "attempts": 0, "next_run": 0, "error": "原文索引缺失，已重新排队解析"}).Error
	}
	model := s.summaryModel()
	var selected *models.LibraryDerivation
	var section models.LibrarySection
	for _, part := range sections {
		item := models.LibraryDerivation{SectionID: part.ID, VersionID: k.ID, SourceDigest: digestText(string(part.Body)), Recipe: summaryRecipe, Model: model, State: "pending"}
		if err := s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
			return true, err
		}
		var existing models.LibraryDerivation
		if err := s.DB.First(&existing, "section_id=?", part.ID).Error; err != nil {
			return true, err
		}
		if existing.SourceDigest != item.SourceDigest || existing.Recipe != summaryRecipe || existing.Model != model {
			if err := s.DB.Model(&existing).Updates(map[string]interface{}{"source_digest": item.SourceDigest, "recipe": summaryRecipe, "model": model, "state": "pending", "content": "", "attempts": 0, "next_run": 0, "error": ""}).Error; err != nil {
				return true, err
			}
			existing = item
		}
		if selected == nil && existing.State != "ready" && existing.State != "failed" && existing.NextRun <= now {
			item = existing
			selected = &item
			section = part
		}
	}
	if selected != nil {
		callCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
		output, callErr := s.Summarize(callCtx, "原文章节："+section.Heading+"\n"+string(section.Body))
		cancel()
		values := map[string]interface{}{"attempts": selected.Attempts + 1, "error": ""}
		if callErr != nil || strings.TrimSpace(output) == "" || len(output) > 64000 {
			values["state"] = "retry"
			if selected.Attempts >= 4 {
				values["state"] = "failed"
			}
			values["next_run"] = now + 60*int64(selected.Attempts+1)
			values["error"] = "本节 AI 整理未完成，原文仍可检索"
		} else {
			values["state"] = "ready"
			values["content"] = output
			values["next_run"] = 0
		}
		if s.summaryModel() != model {
			return true, nil
		}
		if err := s.DB.Transaction(func(tx *gorm.DB) error {
			var held models.LibraryVersion
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&held, k.ID).Error; err != nil {
				return err
			}
			var valid int64
			if err := (&Store{DB: tx}).visible().Where("k.id=? AND k.lease=? AND k.lease_until>?", k.ID, lease, time.Now().Unix()).Count(&valid).Error; err != nil {
				return err
			}
			if valid != 1 {
				return nil
			}
			return tx.Model(&models.LibraryDerivation{}).Where("section_id=? AND source_digest=? AND recipe=? AND model=?", selected.SectionID, selected.SourceDigest, summaryRecipe, model).Updates(values).Error
		}); err != nil {
			return true, err
		}
	}
	return true, s.assembleSummary(k.ID, lease, sections, model)
}

func (s *Store) assembleSummary(id int64, lease string, sections []models.LibrarySection, model string) error {
	if s.summaryModel() != model {
		return nil
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var held models.LibraryVersion
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&held, id).Error; err != nil {
			return err
		}
		var valid int64
		if err := (&Store{DB: tx}).visible().Where("k.id=? AND k.lease=? AND k.lease_until>?", id, lease, time.Now().Unix()).Count(&valid).Error; err != nil {
			return err
		}
		if valid != 1 {
			return nil
		}
		var items []models.LibraryDerivation
		if err := tx.Where("version_id=?", id).Find(&items).Error; err != nil {
			return err
		}
		byID := map[int64]models.LibraryDerivation{}
		for _, item := range items {
			byID[item.SectionID] = item
		}
		ready, failed := 0, 0
		next := int64(0)
		var body strings.Builder
		for _, section := range sections {
			item := byID[section.ID]
			if item.SourceDigest != digestText(string(section.Body)) || item.Recipe != summaryRecipe || item.Model != model {
				continue
			}
			if item.State == "ready" {
				ready++
				fmt.Fprintf(&body, "## %s\n\n%s\n\n[原文：%s](/archives/%d/documents/%d?version=%d)\n\n", section.Heading, string(item.Content), section.Heading, held.ArchiveID, held.DocumentID, held.SourceVersionID)
			} else if item.State == "failed" {
				failed++
			} else if item.NextRun > 0 && (next == 0 || item.NextRun < next) {
				next = item.NextRun
			}
		}
		state := "pending"
		note := ""
		if ready == len(sections) {
			state = "ready"
			next = 0
		} else if ready+failed == len(sections) {
			state = "failed"
			note = "部分章节整理失败，可重试；原文仍可检索"
		} else if next > 0 {
			state = "retry"
			note = "部分章节等待重试；原文仍可检索"
		}
		// Pending unattempted sections should not wait for another chapter's backoff.
		for _, section := range sections {
			item := byID[section.ID]
			if item.State == "pending" || item.State == "" {
				next = 0
				break
			}
		}
		content := ""
		if ready > 0 {
			content = fmt.Sprintf("> 覆盖范围（系统标记）：已整理 %d/%d 节；模型整理待核实，未完成章节不能据此排除事实。\n\n%s", ready, len(sections), body.String())
		}
		return tx.Model(&held).Updates(map[string]interface{}{"summary": content, "summary_model": model, "summary_recipe": summaryRecipe, "summary_state": state, "next_run": next, "updated_at": time.Now().Unix(), "error": note}).Error
	})
}
