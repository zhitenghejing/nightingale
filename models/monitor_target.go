package models

import (
	"errors"

	"gorm.io/gorm"
)

// Native mutations must not bypass collection ownership, even with force=true.
// Collection writes update both records together in their own transaction.
func MonitorGuardTargets(db *gorm.DB, idents []string) error {
	if len(idents) == 0 || !db.Migrator().HasTable(&MonitorDevice{}) {
		return nil
	}
	var n int64
	if err := db.Model(&MonitorDevice{}).Where("ident IN ?", idents).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errors.New("managed SNMP targets must be changed through collection management")
	}
	return nil
}
func MonitorFilterTargets(db *gorm.DB) *gorm.DB {
	if !db.Migrator().HasTable(&MonitorDevice{}) {
		return db
	}
	return db.Where("target.ident NOT IN (?)", MonitorDeviceIdentQuery(db).Where("deleted = ?", true))
}

// MonitorDeviceIdentQuery is the Center-side subquery shared by native Target
// filters and Agent ICMP ownership. An explicit MySQL collation also works when
// legacy Target tables use a different collation from the monitor schema.
func MonitorDeviceIdentQuery(db *gorm.DB) *gorm.DB {
	column := "ident"
	if db.Dialector.Name() == "mysql" {
		column = "ident COLLATE utf8mb4_bin"
	}
	return db.Session(&gorm.Session{NewDB: true}).Model(&MonitorDevice{}).Select(column)
}
func MonitorAgentIdents(db *gorm.DB, idents []string) ([]string, error) {
	if len(idents) == 0 || !db.Migrator().HasTable(&MonitorDevice{}) {
		return idents, nil
	}
	var managed []string
	if err := db.Model(&MonitorDevice{}).Where("ident IN ?", idents).Pluck("ident", &managed).Error; err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, id := range managed {
		set[id] = true
	}
	out := []string{}
	for _, id := range idents {
		if !set[id] {
			out = append(out, id)
		}
	}
	return out, nil
}
