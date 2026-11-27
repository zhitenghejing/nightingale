package environment

import (
	"context"
	"fmt"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"gorm.io/gorm/clause"
)

// ReconcileOne runs as the original saved-version author. Disabled users and
// missing write rights never fall back to an administrator identity.
func (s *Store) ReconcileOne() (bool, error) {
	var row struct {
		ID       int64
		CreateBy int64
	}
	err := s.DB.Table("knowledge_document_version k").Select("k.id,v.create_by").Joins("JOIN archive_document d ON d.id=k.document_id AND d.current_version_id=k.source_version_id AND d.deleted = ?", false).Joins("JOIN archive_record a ON a.id=d.archive_id AND a.deleted = ?", false).Joins("JOIN archive_document_version v ON v.id=k.source_version_id").Joins("LEFT JOIN environment_discovery j ON j.version_id=k.id").Where("k.state = ? AND (j.version_id IS NULL OR (j.next_run <= ? AND j.lease_until < ?))", "ready", now(), now()).Order("COALESCE(j.next_run,0),k.id").Limit(1).Find(&row).Error
	if err != nil {
		return false, err
	}
	if row.ID == 0 {
		return false, nil
	}
	job := models.EnvironmentDiscovery{VersionID: row.ID, ActorID: row.CreateBy}
	if err = s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&job).Error; err != nil {
		return false, err
	}
	lease := now() + 120
	r := s.DB.Model(&models.EnvironmentDiscovery{}).Where("version_id = ? AND lease_until < ?", row.ID, now()).Updates(map[string]interface{}{"lease_until": lease})
	if r.Error != nil || r.RowsAffected == 0 {
		return false, r.Error
	}
	result, e := s.Discover(row.CreateBy, fmt.Sprintf("document:%d", row.ID))
	state, note := "ready", result.Note
	if e != nil {
		state = "retry"
		note = "关联候选未完成：" + e.Error()
	}
	err = s.DB.Model(&models.EnvironmentDiscovery{}).Where("version_id = ? AND lease_until = ?", row.ID, lease).Updates(map[string]interface{}{"actor_id": row.CreateBy, "state": state, "note": note, "updated_at": now(), "next_run": now() + 300, "lease_until": int64(0)}).Error
	return true, err
}
func (s *Store) Start(ctx context.Context) func() {
	done := make(chan struct{})
	worker := &Store{DB: s.DB.WithContext(ctx)}
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			for i := 0; i < 5 && ctx.Err() == nil; i++ {
				ok, e := worker.ReconcileOne()
				if !ok || e != nil {
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
