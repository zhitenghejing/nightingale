package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/opsauth"
	"github.com/toolkits/pkg/logger"
	"gorm.io/gorm"
)

func (s *Service) Ingest(user int64, event models.AlertHisEvent) (*models.OpsCase, error) {
	if _, err := opsauth.Check(s.DB, user, event.GroupId, false); err != nil {
		return nil, err
	}
	if event.IncidentID == "" || event.DispatchID == "" {
		return nil, opsauth.Fail(422, "event_identity", "Legacy event identity is missing; create a manual case instead")
	}
	var result models.OpsCase
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var old models.OpsInbox
		err := tx.Where("dispatch_id = ?", event.DispatchID).First(&old).Error
		if err == nil {
			if err = tx.First(&result, old.CaseID).Error; err != nil {
				return err
			}
			if result.GroupID != event.GroupId || old.IncidentID != event.IncidentID {
				return opsauth.Fail(409, "event_identity_conflict", "Dispatch identity conflicts with its recorded incident or group")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var episode models.OpsEpisode
		err = tx.Where("incident_id = ?", event.IncidentID).First(&episode).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			event.DB2FE()
			attrs := map[string]string{}
			for _, label := range event.TagsJSON {
				pair := strings.SplitN(label, "=", 2)
				if len(pair) == 2 {
					attrs["label."+pair[0]] = pair[1]
				}
			}
			if attrs["label.environment"] != "" {
				attrs["environment"] = attrs["label.environment"]
			}
			if attrs["label.system_version"] != "" {
				attrs["system_version"] = attrs["label.system_version"]
			}
			title := event.RuleName
			if event.TargetIdent == "" {
				event.TargetIdent = attrs["label.ident"]
			}
			if title == "" {
				title = "Alert investigation"
			}
			wait := ""
			if event.IsRecovered == 1 {
				wait = "recovery_without_context"
			}
			result = models.OpsCase{GroupID: event.GroupId, Title: truncate(title, 80), Severity: event.Severity, State: "pending", WaitReason: wait, TargetIdent: event.TargetIdent, DatasourceID: event.DatasourceId, RuleID: event.RuleId, Context: opsauth.JSON(attrs), Revision: 1, Generation: 1, CreatedBy: user, CreatedAt: s.now(), UpdatedAt: s.now()}
			if err = tx.Create(&result).Error; err != nil {
				return err
			}
			episode = models.OpsEpisode{CaseID: result.ID, GroupID: event.GroupId, IncidentID: event.IncidentID, Sequence: event.IncidentSequence, Recovered: event.IsRecovered == 1, Snapshot: opsauth.JSON(event), UpdatedAt: s.now()}
			if err = tx.Create(&episode).Error; err != nil {
				return err
			}
			if err = s.audit(tx, result.ID, user, "alert_ingested", map[string]interface{}{"incident_id": event.IncidentID, "recovered": event.IsRecovered == 1}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if episode.GroupID != event.GroupId {
				return opsauth.Fail(409, "episode_group", "Alert episode belongs to another business group")
			}
			if err = tx.First(&result, episode.CaseID).Error; err != nil {
				return err
			}
			// A recovered episode is terminal. A delayed firing snapshot cannot reopen it.
			if event.IncidentSequence > episode.Sequence && !episode.Recovered {
				if err = tx.Model(&episode).Updates(map[string]interface{}{"sequence": event.IncidentSequence, "recovered": event.IsRecovered == 1, "snapshot": opsauth.JSON(event), "updated_at": s.now()}).Error; err != nil {
					return err
				}
				if err = s.audit(tx, result.ID, user, "alert_updated", map[string]interface{}{"sequence": event.IncidentSequence, "recovered": event.IsRecovered == 1}); err != nil {
					return err
				}
			}
		}
		return tx.Create(&models.OpsInbox{DispatchID: event.DispatchID, IncidentID: event.IncidentID, CaseID: result.ID, CreatedAt: s.now()}).Error
	})
	if err != nil {
		// Another scanner may have committed the same identity concurrently.
		var seen models.OpsInbox
		if e := s.DB.Where("dispatch_id = ?", event.DispatchID).First(&seen).Error; e == nil {
			if e = s.DB.First(&result, seen.CaseID).Error; e == nil && result.GroupID == event.GroupId && seen.IncidentID == event.IncidentID {
				return &result, nil
			}
		}
	}
	return &result, err
}

func (s *Service) Policies(user int64) ([]models.OpsPolicy, error) {
	ids, admin, err := opsauth.Groups(s.DB, user)
	if err != nil {
		return nil, err
	}
	q := s.DB.Model(&models.OpsPolicy{})
	if !admin {
		q = q.Where("group_id IN ?", ids)
	}
	rows := []models.OpsPolicy{}
	err = q.Find(&rows).Error
	return rows, err
}
func (s *Service) SavePolicy(user int64, in models.OpsPolicy) error {
	if _, err := opsauth.Check(s.DB, user, in.GroupID, true); err != nil {
		return err
	}
	if in.Enabled {
		if _, err := opsauth.Check(s.DB, in.ServiceUserID, in.GroupID, false); err != nil {
			return err
		}
	}
	ids := []int64{}
	if len(in.RuleIDs) > 0 {
		if err := json.Unmarshal([]byte(in.RuleIDs), &ids); err != nil || len(ids) > 100 {
			return opsauth.Fail(422, "policy_rules", "Invalid rule allowlist")
		}
	}
	for _, id := range ids {
		var r models.AlertRule
		if err := s.DB.Where("id = ? AND group_id = ?", id, in.GroupID).First(&r).Error; err != nil {
			return opsauth.Fail(422, "policy_rule_scope", "Rule does not belong to this business group")
		}
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var old models.OpsPolicy
		err := tx.Where("group_id = ?", in.GroupID).First(&old).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			in.ID = 0
			in.Revision = 1
			in.ScanAt = s.now()
			in.ScanID = 0
			in.RuleIDs = opsauth.JSON(ids)
			in.ReconcileAt = 0
			in.Health = ""
			in.GapSince = 0
			return tx.Create(&in).Error
		}
		if err != nil {
			return err
		}
		r := tx.Model(&old).Where("revision = ?", in.Revision).Updates(map[string]interface{}{"enabled": in.Enabled, "auto_investigate": in.AutoInvestigate, "service_user_id": in.ServiceUserID, "rule_ids": opsauth.JSON(ids), "revision": old.Revision + 1})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return opsauth.Fail(409, "revision_conflict", "Policy changed; reload before saving")
		}
		return nil
	})
}

// Scan runs a bounded, stable history pass. Reconciliation restarts from the
// retained source window periodically, covering late commits and inbox retries.
func (s *Service) Scan(ctx context.Context) error {
	policies := []models.OpsPolicy{}
	if err := s.DB.WithContext(ctx).Where("enabled = ?", true).Find(&policies).Error; err != nil {
		return err
	}
	for _, p := range policies {
		if s.SourceRetentionSeconds > 0 && p.ScanAt > 0 && s.now()-p.ScanAt > s.SourceRetentionSeconds && p.GapSince == 0 {
			p.GapSince = p.ScanAt
			_ = s.DB.Model(&models.OpsPolicy{}).Where("id = ?", p.ID).Update("gap_since", p.GapSince).Error
		}
		if _, err := opsauth.Check(s.DB, p.ServiceUserID, p.GroupID, false); err != nil {
			s.policyHealth(p.ID, err.Error())
			continue
		}
		q := s.DB.WithContext(ctx).Where("group_id = ?", p.GroupID)
		ids := []int64{}
		_ = json.Unmarshal([]byte(p.RuleIDs), &ids)
		if len(ids) > 0 {
			q = q.Where("rule_id IN ?", ids)
		}
		// A cursor scans full retained history in bounded pages every 10 minutes.
		reconcile := p.ReconcileAt == 0 || s.now()-p.ReconcileAt >= 600
		if reconcile {
			q = q.Where("id > ?", p.ScanID)
		} else {
			q = q.Where("last_eval_time >= ?", p.ScanAt-60)
		}
		rows := []models.AlertHisEvent{}
		if err := q.Order("id ASC").Limit(500).Find(&rows).Error; err != nil {
			s.policyHealth(p.ID, err.Error())
			continue
		}
		failed := false
		for _, e := range rows {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if e.IncidentID == "" || e.DispatchID == "" {
				continue
			}
			c, err := s.Ingest(p.ServiceUserID, e)
			if err != nil {
				failed = true
				s.policyHealth(p.ID, err.Error())
				break
			}
			if p.AutoInvestigate && e.IsRecovered == 0 && c.State != "closed" && c.OwnerID == 0 {
				var n int64
				if err = s.DB.Model(&models.OpsRun{}).Where("case_id = ?", c.ID).Count(&n).Error; err == nil && n == 0 {
					_, _ = s.Queue(p.ServiceUserID, c.ID, "agent")
				}
			}
		}
		if failed {
			continue
		}
		update := map[string]interface{}{"health": "ok"}
		if p.GapSince > 0 {
			update["health"] = "Source retention gap: operator reconciliation required"
		}
		if reconcile {
			if len(rows) < 500 {
				update["reconcile_at"] = s.now()
				update["scan_id"] = 0
				update["scan_at"] = s.now()
			} else {
				update["scan_id"] = rows[len(rows)-1].Id
			}
		} else if len(rows) < 500 {
			update["scan_at"] = s.now()
		} else {
			update["reconcile_at"] = 0
			update["scan_id"] = 0
		}
		if err := s.DB.Model(&models.OpsPolicy{}).Where("id = ?", p.ID).Updates(update).Error; err != nil {
			return err
		}
		// Active reconciliation is independent of whether notification snapshots exist.
		cursor := int64(0)
		for {
			active := []models.AlertCurEvent{}
			aq := s.DB.WithContext(ctx).Where("group_id = ? AND id > ?", p.GroupID, cursor)
			if len(ids) > 0 {
				aq = aq.Where("rule_id IN ?", ids)
			}
			if err := aq.Order("id ASC").Limit(500).Find(&active).Error; err != nil {
				s.policyHealth(p.ID, err.Error())
				break
			}
			for _, e := range active {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if e.IncidentID != "" && e.DispatchID != "" {
					c, err := s.Ingest(p.ServiceUserID, *e.ToHis(nil))
					if err != nil {
						s.policyHealth(p.ID, err.Error())
						continue
					}
					if p.AutoInvestigate && c.State != "closed" && c.OwnerID == 0 {
						var n int64
						if err = s.DB.Model(&models.OpsRun{}).Where("case_id = ?", c.ID).Count(&n).Error; err == nil && n == 0 {
							_, _ = s.Queue(p.ServiceUserID, c.ID, "agent")
						}
					}
				}
				cursor = e.Id
			}
			if len(active) < 500 {
				break
			}
		}
	}
	return nil
}
func (s *Service) AcknowledgeGap(user, group int64, note string) error {
	if _, err := opsauth.Check(s.DB, user, group, true); err != nil {
		return err
	}
	if strings.TrimSpace(note) == "" {
		return opsauth.Fail(422, "reason_required", "Describe the manual reconciliation before acknowledging a source gap")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.OpsPolicy{}).Where("group_id = ?", group).Updates(map[string]interface{}{"gap_since": 0, "health": "Acknowledged by operator", "scan_at": s.now()}).Error; err != nil {
			return err
		}
		return s.audit(tx, 0, user, "source_gap_acknowledged", map[string]interface{}{"group_id": group, "note": note})
	})
}
func (s *Service) policyHealth(id int64, message string) {
	_ = s.DB.Model(&models.OpsPolicy{}).Where("id = ?", id).Update("health", truncate(message, 200)).Error
}
func (s *Service) Start(ctx context.Context) func() {
	var workers sync.WaitGroup
	start := func(label string, work func(context.Context) error) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				if err := work(ctx); err != nil && ctx.Err() == nil {
					logger.Warningf("ops %s: %v", label, err)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}
	start("scanner", s.Scan)
	start("worker", s.RunNext)
	start("worker", s.RunNext)
	return workers.Wait
}
