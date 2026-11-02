package collection

import (
	"context"
	"errors"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (s *Store) CreateProbe(scope Scope, f DeviceInput) (models.MonitorProbe, error) {
	var out models.MonitorProbe
	f.Name = "Connection probe"
	f.IntervalSeconds = 15
	f.Enabled = true
	if err := inputDefaults(s.DB, &f); err != nil {
		return out, err
	}
	if err := f.normalize(); err != nil {
		return out, bad(err.Error())
	}
	err := s.write(func(tx *gorm.DB) error {
		// Serialize per-user/group quotas across replicas using the existing group row.
		if err := tx.Model(&models.BusiGroup{}).Where("id = ?", f.GroupID).UpdateColumn("id", gorm.Expr("id")).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.User{}).Where("id = ?", scope.ActorID).UpdateColumn("id", gorm.Expr("id")).Error; err != nil {
			return err
		}
		c, err := s.collector(tx, f.CollectorID)
		if err != nil {
			return err
		}
		var previous models.MonitorDevice
		if f.ProbeDeviceID != "" {
			if err := tx.First(&previous, "id = ? AND deleted = ?", f.ProbeDeviceID, false).Error; err != nil {
				return missing()
			}
			if err := scope.Check(previous.GroupID, true); err != nil {
				return err
			}
			if previous.RecordVersion != f.RecordVersion {
				return conflict("record version conflict")
			}
		}
		if _, err = resolveDeviceProfile(tx, &f, previous); err != nil {
			return err
		}
		originalCredential := f.CredentialID
		if err = s.prepareParameters(tx, scope, &f, previous); err != nil {
			return err
		}
		if err = s.validateDevice(tx, scope, f, c); err != nil {
			return err
		}
		checks := []struct {
			where string
			args  []interface{}
			limit int64
		}{{"created_by = ? AND state <> ?", []interface{}{scope.ActorID, "cleaned"}, 2}, {"collector_id = ? AND state <> ?", []interface{}{f.CollectorID, "cleaned"}, 10}, {"group_id = ? AND state <> ?", []interface{}{f.GroupID, "cleaned"}, 10}, {"created_by = ? AND created_at > ?", []interface{}{scope.ActorID, time.Now().Add(-time.Minute).Unix()}, 5}}
		for _, check := range checks {
			var n int64
			if err = tx.Model(&models.MonitorProbe{}).Where(check.where, check.args...).Count(&n).Error; err != nil {
				return err
			}
			if n >= check.limit {
				return &Error{429, "probe quota reached; wait for cleanup"}
			}
		}
		cred, err := s.credential(tx, f.CredentialID, scope, f.GroupID)
		if err != nil {
			return err
		}
		secret, err := s.Keys.Open(string(cred.Ciphertext), cred.KeyID, "credential:"+cred.ID)
		if err != nil {
			return err
		}
		out = models.MonitorProbe{ID: uuid.NewString(), GroupID: f.GroupID, CollectorID: f.CollectorID, CreatedBy: scope.ActorID, Revision: uuid.NewString(), CreatedAt: time.Now().Unix(), Deadline: time.Now().Add(90 * time.Second).Unix(), State: "waiting"}
		params, err := executionParameters(f, cred, secret)
		if err != nil {
			return err
		}
		community, _ := params["community"].(string)
		var content ConfigContent
		plugin := "snmp"
		if f.Protocol == "mysql" {
			doc, loadErr := loadTemplate(tx, f.TemplateVersionID)
			if loadErr != nil {
				return loadErr
			}
			plugin = "mysql"
			content, err = compileMySQL(f, out.ID, out.Revision, params, documentConfig(doc), "probe")
		} else {
			content, err = compile(f, out.ID, out.Revision, community, "probe")
		}
		if err != nil {
			return err
		}
		envelope := struct {
			ConfigContent
			Plugin string `json:"plugin,omitempty"`
		}{content, plugin}
		sealed, key, err := s.Keys.Seal(canonical(envelope), "probe:"+out.ID)
		if err != nil {
			return err
		}
		out.Ciphertext = models.MonitorText(sealed)
		out.KeyID = key
		if originalCredential != f.CredentialID && previous.CredentialID != f.CredentialID {
			if err := tx.Delete(&models.MonitorCredential{}, "id = ?", f.CredentialID).Error; err != nil {
				return err
			}
		}
		if err = tx.Create(&out).Error; err != nil {
			return err
		}
		if err = s.publish(tx, &c, scope, "probe_created"); err != nil {
			return err
		}
		return s.audit(tx, scope, f.GroupID, out.ID, "probe_created", c.DesiredVersion, nil)
	})
	return out, err
}
func (s *Store) Probe(scope Scope, id string) (models.MonitorProbe, error) {
	var p models.MonitorProbe
	if err := s.DB.First(&p, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return p, missing()
		}
		return p, err
	}
	return p, scope.Check(p.GroupID, false)
}
func (s *Store) CancelProbe(scope Scope, id string) error {
	return s.write(func(tx *gorm.DB) error {
		var p models.MonitorProbe
		if tx.First(&p, "id = ?", id).Error != nil {
			return missing()
		}
		if err := scope.Check(p.GroupID, true); err != nil {
			return err
		}
		c, err := s.collector(tx, p.CollectorID)
		if err != nil {
			return err
		}
		if p.CleanupAt > 0 {
			return nil
		}
		p.CleanupAt = time.Now().Unix()
		p.State = "cleanup_pending"
		p.Result = "cancelled"
		if err = tx.Save(&p).Error; err != nil {
			return err
		}
		if err = s.publish(tx, &c, scope, "probe_cancelled"); err != nil {
			return err
		}
		return s.audit(tx, scope, p.GroupID, p.ID, "probe_cancelled", c.DesiredVersion, nil)
	})
}
func (s *Store) CleanupProbes() error {
	return s.write(func(tx *gorm.DB) error {
		now := time.Now().Unix()
		var probes []models.MonitorProbe
		if err := tx.Where("state <> ?", "cleaned").Order("collector_id, id").Find(&probes).Error; err != nil {
			return err
		}
		collectors := map[string]models.MonitorCollector{}
		for _, p := range probes {
			c, ok := collectors[p.CollectorID]
			if !ok {
				var err error
				c, err = s.collector(tx, p.CollectorID)
				if err != nil {
					return err
				}
				collectors[p.CollectorID] = c
			}
			if p.CleanupAt == 0 && (p.Deadline <= now || p.Result == "passed" || p.Result == "failed") {
				p.CleanupAt = now
				p.State = "cleanup_pending"
				if p.Result == "" {
					p.Result = "timeout"
				}
				if err := tx.Save(&p).Error; err != nil {
					return err
				}
				if err := s.publish(tx, &c, Scope{Admin: true}, "probe_expired"); err != nil {
					return err
				}
				collectors[p.CollectorID] = c
			} else if p.CleanupAt > 0 && p.CleanupVersion != "" && c.LastClientVersion == c.DesiredVersion && c.ClientSeenAt >= p.CleanupAt && now-max(p.CleanupAt, c.ClientSeenAt, p.LastReceivedAt) >= 90 {
				if err := tx.Model(&p).Update("state", "cleaned").Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
func (s *Store) Run(ctx context.Context) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTicker(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				_ = s.CleanupProbes()
			}
		}
	}()
	return func() { <-done }
}
