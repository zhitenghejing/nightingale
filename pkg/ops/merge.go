package ops

import (
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"gorm.io/gorm"
)

// Merge only combines episodes with the same trusted target and datasource.
// Original evidence remains on the closed source case and stays auditable.
func (s *Service) Merge(user, id, revision, sourceID, sourceRevision int64, reason string) error {
	if s.scope != nil || id == sourceID || reason == "" {
		return opsauth.Fail(422, "merge_invalid", "An operator must provide a different source case and a reason")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		target, err := s.get(tx, user, id, true)
		if err != nil {
			return err
		}
		source, err := s.get(tx, user, sourceID, true)
		if err != nil {
			return err
		}
		if target.GroupID != source.GroupID || target.TargetIdent == "" || target.TargetIdent != source.TargetIdent || target.DatasourceID != source.DatasourceID {
			return opsauth.Fail(422, "merge_scope", "Only cases for the same group, monitored target and datasource can be merged")
		}
		fork := s.withDB(tx)
		if err = fork.actionUnresolved(target); err != nil {
			return err
		}
		if err = fork.actionUnresolved(source); err != nil {
			return err
		}
		if err = s.change(tx, target, revision, map[string]interface{}{"generation": target.Generation + 1, "state": "processing"}); err != nil {
			return err
		}
		if err = s.change(tx, source, sourceRevision, map[string]interface{}{"state": "closed", "close_type": "merged", "summary": reason, "followup": "Continue in case " + jsonNumber(id), "closed_at": s.now()}); err != nil {
			return err
		}
		if err = tx.Model(&models.OpsEpisode{}).Where("case_id = ?", sourceID).Update("case_id", id).Error; err != nil {
			return err
		}
		if err = tx.Model(&models.OpsInbox{}).Where("case_id = ?", sourceID).Update("case_id", id).Error; err != nil {
			return err
		}
		if err = tx.Model(&models.OpsRun{}).Where("active_case_id IN ?", []int64{id, sourceID}).Updates(map[string]interface{}{"state": "cancelled", "active_case_id": nil, "fence": gorm.Expr("fence + 1")}).Error; err != nil {
			return err
		}
		if err = s.audit(tx, id, user, "merged", map[string]interface{}{"source_id": sourceID, "reason": reason}); err != nil {
			return err
		}
		return s.audit(tx, sourceID, user, "merged_into", map[string]interface{}{"case_id": id, "reason": reason})
	})
}
