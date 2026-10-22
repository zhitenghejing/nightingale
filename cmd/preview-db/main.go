// preview-db bootstraps an owned PostgreSQL database before any preview listener starts.
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/models/migrate"
	"github.com/ccfos/nightingale/v6/pkg/ormx"
	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type resource struct {
	Name          string `json:"name"`
	DSN           string `json:"dsn"`
	Initialized   bool   `json:"initialized"`
	SQLite        string `json:"sqlite"`
	Password      string `json:"rootPassword"`
	PrometheusURL string `json:"prometheusURL"`
	EngineName    string `json:"engineName"`
}

func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// copySQLite is intentionally strict: unsupported columns/tables abort the entire
// import. The old SQLite file remains available; no partial data loss is accepted.
func copySQLite(db *gorm.DB, path string) (map[string]int, error) {
	source, err := gorm.Open(sqlite.Open("file:"+path+"?mode=ro"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("open SQLite backup: %w", err)
	}
	sourceSQL, err := source.DB()
	if err != nil {
		return nil, err
	}
	defer sourceSQL.Close()
	var tables []string
	if err = source.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name").Scan(&tables).Error; err != nil {
		return nil, err
	}
	counts := map[string]int{}
	err = db.Transaction(func(tx *gorm.DB) error {
		for _, table := range tables {
			var count int64
			if err := source.Table(table).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				continue
			}
			if !tx.Migrator().HasTable(table) {
				return fmt.Errorf("cannot import nonempty table %s: PostgreSQL table missing", table)
			}
			if err := copyTable(sourceSQL, tx, table); err != nil {
				return fmt.Errorf("import table %s: %w", table, err)
			}
			counts[table] = int(count)
		}
		return nil
	})
	return counts, err
}

func copyTable(source *sql.DB, target *gorm.DB, table string) error {
	rows, err := source.Query("SELECT * FROM " + quote(table))
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	var types []struct{ ColumnName, DataType string }
	if err := target.Raw("SELECT column_name, data_type FROM information_schema.columns WHERE table_schema='public' AND table_name=?", table).Scan(&types).Error; err != nil {
		return err
	}
	typeMap := map[string]string{}
	for _, column := range types {
		typeMap[column.ColumnName] = column.DataType
	}
	quoted, placeholders := make([]string, len(columns)), make([]string, len(columns))
	for i, column := range columns {
		if _, ok := typeMap[column]; !ok {
			return fmt.Errorf("missing target column %s", column)
		}
		quoted[i], placeholders[i] = quote(column), "?"
	}
	if err := target.Exec("DELETE FROM " + quote(table)).Error; err != nil {
		return err
	}
	for rows.Next() {
		values, pointers := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return err
		}
		for i, column := range columns {
			if raw, ok := values[i].([]byte); ok && typeMap[column] != "bytea" {
				values[i] = string(raw)
			}
			if typeMap[column] == "boolean" {
				if value, ok := values[i].(int64); ok {
					values[i] = value != 0
				}
			}
		}
		if err := target.Exec("INSERT INTO "+quote(table)+" ("+strings.Join(quoted, ",")+") VALUES ("+strings.Join(placeholders, ",")+")", values...).Error; err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, column := range columns {
		var sequence sql.NullString
		if err := target.Raw("SELECT pg_get_serial_sequence(?, ?)", table, column).Scan(&sequence).Error; err != nil {
			return err
		}
		if sequence.Valid {
			if err := target.Exec("SELECT setval(?::regclass, COALESCE((SELECT MAX("+quote(column)+") FROM "+quote(table)+"), 1), EXISTS(SELECT 1 FROM "+quote(table)+"))", sequence.String).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func datasource(db *gorm.DB, r resource) error {
	var ds models.Datasource
	result := db.Where("identifier = ?", "preview-shared-prometheus").Limit(1).Find(&ds)
	if result.Error != nil {
		return result.Error
	}
	ds.Name, ds.Identifier = "shared-prometheus", "preview-shared-prometheus"
	ds.PluginId, ds.PluginType, ds.PluginTypeName = 1, models.PROMETHEUS, "Prometheus Like"
	ds.Category, ds.ClusterName, ds.Status = "timeseries", r.EngineName, "enabled"
	ds.HTTPJson = models.HTTP{Url: r.PrometheusURL, Timeout: 10000, DialTimeout: 3000, MaxIdleConnsPerHost: 10}
	ds.SettingsJson = map[string]interface{}{}
	ds.CreatedBy, ds.UpdatedBy = "system", "system"
	ds.UpdatedAt = time.Now().Unix()
	if result.RowsAffected == 0 {
		ds.CreatedAt = ds.UpdatedAt
		var defaults int64
		if err := db.Model(&models.Datasource{}).Where("is_default = ?", true).Count(&defaults).Error; err != nil {
			return err
		}
		ds.IsDefault = defaults == 0
	}
	if err := ds.FE2DB(); err != nil {
		return err
	}
	// Old local TSDB addresses cannot be used after the backend is disabled.
	if err := db.Model(&models.Datasource{}).Where("identifier = ?", models.EmbeddedTSDBIdentifier).
		Updates(map[string]any{"status": "disabled", "is_default": false}).Error; err != nil {
		return err
	}
	return db.Save(&ds).Error
}

func bootstrap(r resource) error {
	config, err := pgx.ParseConfig(r.DSN)
	if err != nil || config.Database != r.Name || config.User != r.Name || !regexp.MustCompile(`^n9ep_[0-9a-f]{16}$`).MatchString(r.Name) {
		return fmt.Errorf("invalid owned PostgreSQL resource")
	}
	db, err := gorm.Open(postgres.Open(r.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), NamingStrategy: schema.NamingStrategy{SingularTable: true}})
	if err != nil {
		return fmt.Errorf("connect to preview database: %w", err)
	}
	connection, err := db.DB()
	if err != nil {
		return err
	}
	defer connection.Close()
	connection.SetMaxOpenConns(2)
	var privileged bool
	if err := db.Raw("SELECT rolsuper OR rolcreatedb OR rolcreaterole FROM pg_roles WHERE rolname=current_user").Scan(&privileged).Error; err != nil || privileged {
		return fmt.Errorf("preview bootstrap requires a restricted database owner")
	}
	var owner string
	if err := db.Raw("SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname=current_database()").Scan(&owner).Error; err != nil || owner != r.Name {
		return fmt.Errorf("preview database ownership mismatch")
	}
	if !r.Initialized {
		// A failed bootstrap is retriable only before the manager marks this owned
		// database initialized. It has never been exposed to the application.
		if err := db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public").Error; err != nil {
			return err
		}
		if err := ormx.DataBaseInit(ormx.DBConfig{DBType: "postgres"}, db); err != nil {
			return err
		}
		if err := migrate.MigrateTables(db); err != nil {
			return err
		}
		// The two alert event tables are migrated asynchronously.
		deadline := time.Now().Add(30 * time.Second)
		for !db.Migrator().HasTable("alert_his_event") || !db.Migrator().HasTable("alert_cur_event") {
			if time.Now().After(deadline) {
				return fmt.Errorf("preview schema initialization timed out")
			}
			time.Sleep(100 * time.Millisecond)
		}
		if r.SQLite != "" {
			counts, err := copySQLite(db, r.SQLite)
			if err != nil {
				return err
			}
			fmt.Printf("Imported %d nonempty SQLite tables\n", len(counts))
		} else {
			if len(r.Password) < 16 || len(r.Password) > 31 {
				return fmt.Errorf("invalid initial root credential")
			}
			if err := db.Table("users").Where("username = ?", "root").Update("password", r.Password).Error; err != nil {
				return err
			}
		}
	}
	return datasource(db, r)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: preview-db PRIVATE_RESOURCE_FILE")
		os.Exit(1)
	}
	var r resource
	data, err := os.ReadFile(os.Args[1])
	if err == nil {
		err = json.Unmarshal(data, &r)
	}
	if err == nil {
		err = bootstrap(r)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "preview database bootstrap failed:", err)
		os.Exit(1)
	}
}
