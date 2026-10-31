package models

import "gorm.io/gorm"

// MigrateMonitorAddressConstraints removes the former endpoint uniqueness rule.
// Run before AutoMigrate: older MySQL drivers treat unique constraints as foreign
// keys when Gorm automatically removes a unique tag.
func MigrateMonitorAddressConstraints(db *gorm.DB) error {
	m := db.Migrator()
	if !m.HasTable(&MonitorDevice{}) {
		return nil
	}
	for _, field := range []string{"group_endpoint", "collector_endpoint"} {
		name := db.NamingStrategy.UniqueName("monitor_device", field)
		if db.Dialector.Name() == "mysql" {
			if m.HasIndex(&MonitorDevice{}, name) {
				if err := m.DropIndex(&MonitorDevice{}, name); err != nil {
					return err
				}
			}
		} else if m.HasConstraint(&MonitorDevice{}, name) {
			if err := m.DropConstraint(&MonitorDevice{}, name); err != nil {
				return err
			}
		}
	}
	return nil
}
