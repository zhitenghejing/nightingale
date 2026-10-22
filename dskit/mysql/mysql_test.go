package mysql

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/dskit/pool"
	"github.com/ccfos/nightingale/v6/internal/testenv"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNewMySQLWithSettings(t *testing.T) {
	settings := `{"mysql.shards":[{"mysql.addr":"localhost:3306","mysql.user":"test","mysql.db":"fixture","mysql.max_idle_conns":5,"mysql.max_open_conns":10}]}`
	got, err := NewMySQLWithSettings(context.Background(), settings)
	require.NoError(t, err)
	require.Len(t, got.Shards, 1)
	require.Equal(t, "localhost:3306", got.Shards[0].Addr)
	require.Equal(t, "fixture", got.Shards[0].DB)
	require.Equal(t, 5, got.Shards[0].MaxIdleConns)
	require.Equal(t, 10, got.Shards[0].MaxOpenConns)
	_, err = NewMySQLWithSettings(context.Background(), 12345)
	require.Error(t, err)
	_, err = NewMySQLWithSettings(context.Background(), "invalid JSON")
	require.Error(t, err)
}

// Each test owns a table in an explicitly supplied disposable database.
func mysqlFixture(t *testing.T) (*MySQL, string, string) {
	t.Helper()
	dsn := testenv.Require(t, "N9E_TEST_MYSQL_DSN")
	cfg, err := mysqlDriver.ParseDSN(dsn)
	require.NoError(t, err)
	require.Equal(t, "tcp", cfg.Net, "integration tests require a TCP DSN")
	require.NotEmpty(t, cfg.DBName, "DSN must name a disposable test database")
	_, extra, _ := strings.Cut(cfg.FormatDSN(), "?")
	m, err := NewMySQLWithSettings(context.Background(), map[string]interface{}{
		"mysql.shards": []map[string]interface{}{{
			"mysql.addr": cfg.Addr, "mysql.user": cfg.User, "mysql.password": cfg.Passwd,
			"mysql.db": cfg.DBName, "mysql.dsn_extra_params": extra,
		}},
	})
	require.NoError(t, err)
	var connections []*gorm.DB
	t.Cleanup(func() {
		for _, db := range connections {
			pool.PoolClient.Range(func(key, value interface{}) bool {
				if value == db {
					pool.PoolClient.Delete(key)
				}
				return true
			})
			sqlDB, err := db.DB()
			require.NoError(t, err)
			require.NoError(t, sqlDB.Close())
		}
	})
	db, err := m.NewConn(context.Background(), cfg.DBName)
	require.NoError(t, err)
	connections = append(connections, db)
	server, err := m.NewConn(context.Background(), "")
	require.NoError(t, err)
	connections = append(connections, server)
	table := fmt.Sprintf("n9e_test_%d", time.Now().UnixNano())
	require.NoError(t, db.Exec("CREATE TABLE `"+table+"` (id BIGINT PRIMARY KEY, value DOUBLE, ts BIGINT)").Error)
	t.Cleanup(func() { require.NoError(t, db.Exec("DROP TABLE `"+table+"`").Error) })
	require.NoError(t, db.Exec("INSERT INTO `"+table+"` VALUES (1, 42, 1700000000)").Error)
	return m, cfg.DBName, table
}

func TestNewConn(t *testing.T) {
	m, database, _ := mysqlFixture(t)
	db, err := m.NewConn(context.Background(), database)
	require.NoError(t, err)
	var one int
	require.NoError(t, db.Raw("SELECT 1").Scan(&one).Error)
	require.Equal(t, 1, one)
}

func TestShowDatabases(t *testing.T) {
	m, database, _ := mysqlFixture(t)
	databases, err := m.ShowDatabases(context.Background())
	require.NoError(t, err)
	require.Contains(t, databases, database)
}

func TestShowTables(t *testing.T) {
	m, database, table := mysqlFixture(t)
	tables, err := m.ShowTables(context.Background(), database)
	require.NoError(t, err)
	require.Contains(t, tables, table)
}

func TestDescTable(t *testing.T) {
	m, database, table := mysqlFixture(t)
	columns, err := m.DescTable(context.Background(), database, table)
	require.NoError(t, err)
	require.Len(t, columns, 3)
	require.Equal(t, "id", columns[0].Field)
}

func TestExecQuery(t *testing.T) {
	m, database, table := mysqlFixture(t)
	rows, err := m.ExecQuery(context.Background(), database, "SELECT * FROM `"+table+"` WHERE id = 1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestSelectRows(t *testing.T) {
	m, database, table := mysqlFixture(t)
	rows, err := m.SelectRows(context.Background(), database, table, "id = 1")
	require.NoError(t, err)
	require.Len(t, rows, 1)
}
