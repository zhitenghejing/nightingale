package collection

import (
	"context"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Receiving a provider response does not arm rules. Only the complete required
// metric contract queried from the datasource for this revision can do so.
func (s *Store) armDevice(d models.MonitorDevice) error {
	state, err := deviceProfileState(d)
	if err != nil || len(state.Waiting) == 0 || state.ArmedRevision == d.DesiredRevision {
		return err
	}
	return s.write(func(tx *gorm.DB) error {
		var current models.MonitorDevice
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", d.ID).Error; err != nil {
			return err
		}
		if !current.Enabled || current.Deleted || current.DesiredRevision != d.DesiredRevision {
			return nil
		}
		state, err := deviceProfileState(current)
		if err != nil {
			return err
		}
		if len(state.Waiting) == 0 || state.ArmedRevision == current.DesiredRevision {
			return nil
		}
		for id, disabled := range state.Waiting {
			// Respect an intervening native edit rather than blindly re-enabling it.
			if err := tx.Model(&models.AlertRule{}).Where("id = ? AND update_by = ?", id, "collection").Updates(map[string]interface{}{"disabled": disabled, "update_at": time.Now().Unix()}).Error; err != nil {
				return err
			}
		}
		state.Waiting = map[int64]int{}
		state.ArmedRevision = current.DesiredRevision
		return tx.Model(&current).Update("profile_state", models.MonitorText(canonical(state))).Error
	})
}

func (s *StatusService) Run(ctx context.Context) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTicker(15 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				var devices []models.MonitorDevice
				if s.Store.DB.Where("enabled = ? AND deleted = ? AND profile_state LIKE ?", true, false, "%\"waiting\":%").Order("id").Find(&devices).Error != nil {
					continue
				}
				for i := 0; i < len(devices); i += 50 {
					if ctx.Err() != nil {
						return
					}
					_, _ = s.States(ctx, devices[i:min(i+50, len(devices))])
				}
			}
		}
	}()
	return func() { <-done }
}
