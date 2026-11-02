package migrate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"context"
	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/ccfos/nightingale/v6/pkg/externalpush"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestIdentityMigrationFailureDoesNotSkipOtherModules(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "failure.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { pool.Close() })
	for _, table := range []string{"alert_cur_event", "alert_his_event"} {
		if err := db.Exec("CREATE TABLE " + table + " (id integer PRIMARY KEY)").Error; err != nil {
			t.Fatal(err)
		}
	}
	// Boundary fault injection: the database denies the required identity DDL.
	db.Callback().Raw().Before("gorm:raw").Register("test:deny_identity_ddl", func(tx *gorm.DB) {
		sql := strings.ToLower(tx.Statement.SQL.String())
		if strings.Contains(sql, "alter table") && strings.Contains(sql, "dispatch_id") {
			tx.AddError(errors.New("TEST database denied identity DDL"))
		}
	})
	err = MigrateTables(db)
	if err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("identity DDL error not propagated: %v", err)
	}
	if !db.Migrator().HasTable(&models.NotifyRule{}) {
		t.Fatal("identity migration failure skipped unrelated notification rule migration")
	}
}

func TestDisabledChannelIdentityFailureBlocksSchemaAndOldPersistence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "disabled.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	raw, err := os.ReadFile("../../docker/sqlite.sql")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") || strings.Contains(line, "`dispatch_id`") || strings.Contains(line, "`incident_id`") || strings.Contains(line, "`incident_sequence`") {
			continue
		}
		lines = append(lines, line)
	}
	for _, statement := range strings.Split(strings.Join(lines, "\n"), ";") {
		if strings.TrimSpace(statement) != "" {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	// A previously running deployment had completed these pre-existing async columns.
	for _, table := range []string{"alert_cur_event", "alert_his_event"} {
		for _, column := range []string{"notify_rule_ids", "original_tags"} {
			if !db.Migrator().HasColumn(table, column) {
				if err := db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " TEXT").Error; err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := db.AutoMigrate(&models.NotifyChannelConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.NotifyChannelConfig{Name: "TEST disabled external channel", Ident: models.ExternalPushType, RequestType: models.ExternalPushType, Enable: false}).Error; err != nil {
		t.Fatal(err)
	}
	db.Callback().Raw().Before("gorm:raw").Register("test:deny_identity_ddl", func(tx *gorm.DB) {
		if strings.Contains(strings.ToLower(tx.Statement.SQL.String()), "alter table") && strings.Contains(tx.Statement.SQL.String(), "dispatch_id") {
			tx.AddError(errors.New("TEST database denied identity DDL"))
		}
	})
	if err := MigrateChecked(db); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("required migration failure suppressed: %v", err)
	}
	if err := externalpush.CheckSchema(db); err == nil {
		t.Fatal("incomplete identity schema was considered ready")
	}
	c := nctx.NewContext(context.Background(), db, true)
	event := &models.AlertCurEvent{Hash: uuid.NewString(), DispatchID: uuid.NewString(), IncidentID: uuid.NewString(), IncidentSequence: 1, RuleName: "TEST ordinary event", Tags: "ident=TEST_device", FirstTriggerTime: 100, TriggerTime: 100, LastEvalTime: 100}
	if err := models.EventPersist(c, event); err == nil {
		t.Fatal("ordinary alert unexpectedly persisted without identity schema")
	}
	db.Callback().Raw().Remove("test:deny_identity_ddl")
	if err := MigrateChecked(db); err != nil {
		t.Fatalf("repair required schema: %v", err)
	}
	if err := externalpush.CheckSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := models.EventPersist(c, event); err != nil {
		t.Fatalf("ordinary persistence after repair, channel disabled: %v", err)
	}
	stored, err := models.AlertHisEventGetById(c, event.Id)
	if err != nil || stored == nil || stored.DispatchID != event.DispatchID {
		t.Fatalf("ordinary event identity lost: %v", err)
	}
}

func TestUnrelatedESMigrationKeepsBestEffortCompatibility(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "es-failure.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := db.AutoMigrate(&models.AlertCurEvent{}, &models.AlertHisEvent{}); err != nil {
		t.Fatal(err)
	}
	denied := false
	db.Callback().Raw().Before("gorm:raw").Register("test:deny_es_ddl", func(tx *gorm.DB) {
		sql := strings.ToLower(tx.Statement.SQL.String())
		if strings.Contains(sql, "create table") && strings.Contains(sql, "es_index_pattern") {
			denied = true
			tx.AddError(errors.New("TEST unrelated ES DDL denied"))
		}
	})
	if err := MigrateChecked(db); err != nil {
		t.Fatalf("unrelated historical best-effort failure became fatal: %v", err)
	}
	if !denied {
		t.Fatal("ES migration failure was not injected")
	}
	if err := externalpush.CheckSchema(db); err != nil {
		t.Fatal(err)
	}
}
