package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/internal/testenv"
	"github.com/glebarez/sqlite"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func ownedDatabase(t *testing.T) (*gorm.DB, resource) {
	t.Helper()
	adminDSN := testenv.Require(t, "N9E_TEST_POSTGRES_DSN")
	admin, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	connection, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	name := fmt.Sprintf("n9ep_%016x", time.Now().UnixNano())
	password := "test-only-app-password"
	require.NoError(t, admin.Exec("CREATE ROLE "+name+" LOGIN PASSWORD '"+password+"' NOSUPERUSER NOCREATEDB NOCREATEROLE").Error)
	require.NoError(t, admin.Exec("CREATE DATABASE "+name+" OWNER "+name).Error)
	require.NoError(t, admin.Exec("REVOKE ALL ON DATABASE "+name+" FROM PUBLIC").Error)
	t.Cleanup(func() {
		require.NoError(t, admin.Exec("DROP DATABASE "+name+" WITH (FORCE)").Error)
		require.NoError(t, admin.Exec("DROP ROLE "+name).Error)
	})
	cfg, err := pgx.ParseConfig(adminDSN)
	require.NoError(t, err)
	dsn := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable", cfg.Host, cfg.Port, name, name, password)
	return admin, resource{Name: name, DSN: dsn, Password: "preview-test-password", PrometheusURL: "http://127.0.0.1:39090", EngineName: name}
}

func appDatabase(t *testing.T, r resource) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(r.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	return db
}

func TestPostgresBootstrapIsolationAndRefresh(t *testing.T) {
	_, first := ownedDatabase(t)
	_, second := ownedDatabase(t)
	require.NoError(t, bootstrap(first))
	require.NoError(t, bootstrap(second))
	db := appDatabase(t, first)
	other := appDatabase(t, second)
	var password string
	require.NoError(t, db.Raw("SELECT password FROM users WHERE username='root'").Scan(&password).Error)
	require.Equal(t, first.Password, password)
	require.NoError(t, db.Exec("UPDATE users SET nickname='only-first', password='changed-password' WHERE username='root'").Error)
	first.Initialized = true
	require.NoError(t, bootstrap(first))
	var nickname string
	require.NoError(t, other.Raw("SELECT nickname FROM users WHERE username='root'").Scan(&nickname).Error)
	require.NotEqual(t, "only-first", nickname)
	require.NoError(t, db.Raw("SELECT password FROM users WHERE username='root'").Scan(&password).Error)
	require.Equal(t, "changed-password", password)
	var dsURL string
	require.NoError(t, db.Raw("SELECT http FROM datasource WHERE identifier='preview-shared-prometheus'").Scan(&dsURL).Error)
	require.Contains(t, dsURL, first.PrometheusURL)
	_, err := gorm.Open(postgres.Open(strings.Replace(first.DSN, "dbname="+first.Name, "dbname="+second.Name, 1)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.Error(t, err, "another preview database must reject this role")
}

func TestPostgresSQLiteImportAndSequence(t *testing.T) {
	_, r := ownedDatabase(t)
	r.SQLite = filepath.Join(t.TempDir(), "old.db")
	source, err := gorm.Open(sqlite.Open(r.SQLite), &gorm.Config{})
	require.NoError(t, err)
	connection, err := source.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	require.NoError(t, source.Exec("CREATE TABLE user_group (id integer primary key, name text, note text, create_at integer, create_by text, update_at integer, update_by text)").Error)
	require.NoError(t, source.Exec("INSERT INTO user_group VALUES(41, 'preserved', '', 1, 'root', 1, 'root')").Error)
	require.NoError(t, bootstrap(r))
	db := appDatabase(t, r)
	var name string
	require.NoError(t, db.Raw("SELECT name FROM user_group WHERE id=41").Scan(&name).Error)
	require.Equal(t, "preserved", name)
	var id int64
	require.NoError(t, db.Raw("INSERT INTO user_group(name) VALUES('next') RETURNING id").Scan(&id).Error)
	require.Greater(t, id, int64(41))
	// Unsupported data must fail rather than silently discard a nonempty table.
	require.NoError(t, source.Exec("CREATE TABLE unsupported_data (id integer)").Error)
	require.NoError(t, source.Exec("INSERT INTO unsupported_data VALUES(1)").Error)
	_, err = copySQLite(db, r.SQLite)
	require.ErrorContains(t, err, "unsupported_data")
	require.NoError(t, db.Raw("SELECT count(*) FROM user_group WHERE name='next'").Scan(&id).Error)
	require.EqualValues(t, 1, id, "failed imports must roll back earlier tables")
}
