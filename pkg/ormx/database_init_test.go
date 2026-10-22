package ormx

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/internal/testenv"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTestDatabase(t *testing.T, config DBConfig) *gorm.DB {
	t.Helper()
	var dialect gorm.Dialector
	switch config.DBType {
	case "mysql":
		dialect = mysql.Open(config.DSN)
	case "postgres":
		dialect = postgres.Open(config.DSN)
	case "sqlite":
		dialect = sqlite.Open(config.DSN)
	}
	db, err := gorm.Open(dialect, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	return db
}

// External services need database creation privileges. Only this generated DB
// is initialized and dropped; the database named in the input DSN is untouched.
func isolatedDatabaseConfig(t *testing.T, dialect string) DBConfig {
	t.Helper()
	if dialect == "sqlite" {
		return DBConfig{DBType: dialect, DSN: filepath.Join(t.TempDir(), "n9e.db")}
	}
	name := fmt.Sprintf("n9e_test_%d", time.Now().UnixNano())
	config := DBConfig{DBType: dialect}
	admin := DBConfig{DBType: dialect}
	if dialect == "mysql" {
		parsed, err := mysqlDriver.ParseDSN(testenv.Require(t, "N9E_TEST_MYSQL_DSN"))
		require.NoError(t, err)
		parsed.DBName = ""
		admin.DSN = parsed.FormatDSN()
		parsed.DBName = name
		config.DSN = parsed.FormatDSN()
	} else {
		dsn := testenv.Require(t, "N9E_TEST_POSTGRES_DSN")
		require.False(t, strings.Contains(dsn, "://"), "PostgreSQL tests use keyword=value DSNs, like the database creation implementation")
		var fields []string
		for _, field := range strings.Fields(dsn) {
			if !strings.HasPrefix(field, "dbname=") {
				fields = append(fields, field)
			}
		}
		base := strings.Join(fields, " ")
		admin.DSN = base + " dbname=postgres"
		config.DSN = base + " dbname=" + name
	}
	db := openTestDatabase(t, admin)
	t.Cleanup(func() { require.NoError(t, db.Exec("DROP DATABASE IF EXISTS "+name).Error) })
	return config
}

func TestCheckDatabaseExist(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			config := isolatedDatabaseConfig(t, dialect)
			exists, err := checkDatabaseExist(config)
			require.NoError(t, err)
			require.False(t, exists)
			require.NoError(t, createDatabase(config, &gorm.Config{}))
			exists, err = checkDatabaseExist(config)
			require.NoError(t, err)
			require.True(t, exists)
		})
	}
}

func TestDataBaseInit(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			config := isolatedDatabaseConfig(t, dialect)
			require.NoError(t, createDatabase(config, &gorm.Config{}))
			db := openTestDatabase(t, config)
			require.NoError(t, DataBaseInit(config, db))
			var roots int64
			require.NoError(t, db.Table("users").Where("username = ?", "root").Count(&roots).Error)
			require.EqualValues(t, 1, roots)
			var operations int64
			require.NoError(t, db.Table("role_operation").Count(&operations).Error)
			require.Greater(t, operations, int64(0))
		})
	}
}
