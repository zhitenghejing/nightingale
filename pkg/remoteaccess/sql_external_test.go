package remoteaccess

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/internal/testenv"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSQLDialects(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			key := "N9E_TEST_POSTGRES_DSN"
			if dialect == "mysql" {
				key = "N9E_TEST_MYSQL_DSN"
			}
			dsn := testenv.Require(t, key)
			for _, initialization := range []string{"gorm", "sql"} {
				t.Run(initialization, func(t *testing.T) {
					name := fmt.Sprintf("n9e_remote_access_test_%d", time.Now().UnixNano())
					var pool *sql.DB
					var dialector gorm.Dialector
					if dialect == "postgres" {
						config, err := pgx.ParseConfig(dsn)
						require.NoError(t, err)
						admin, err := pgx.ConnectConfig(context.Background(), config)
						require.NoError(t, err)
						t.Cleanup(func() { _ = admin.Close(context.Background()) })
						_, err = admin.Exec(context.Background(), "CREATE DATABASE "+name)
						require.NoError(t, err)
						t.Cleanup(func() {
							_, err := admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
							require.NoError(t, err)
						})
						app := config.Copy()
						app.Database = name
						pool = stdlib.OpenDB(*app)
						dialector = postgres.New(postgres.Config{Conn: pool})
					} else {
						config, err := mysqldriver.ParseDSN(dsn)
						require.NoError(t, err)
						admin, err := sql.Open("mysql", config.FormatDSN())
						require.NoError(t, err)
						t.Cleanup(func() { _ = admin.Close() })
						_, err = admin.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
						require.NoError(t, err)
						t.Cleanup(func() { _, err := admin.Exec("DROP DATABASE " + name); require.NoError(t, err) })
						app := *config
						app.DBName = name
						app.ParseTime = true
						pool, err = sql.Open("mysql", app.FormatDSN())
						require.NoError(t, err)
						dialector = mysql.New(mysql.Config{Conn: pool})
					}
					t.Cleanup(func() { _ = pool.Close() })
					db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
					require.NoError(t, err)
					if initialization == "sql" {
						script, err := os.ReadFile(filepath.Join("..", "..", "doc", "sql", "remote-access."+dialect+".sql"))
						require.NoError(t, err)
						for i := 0; i < 2; i++ {
							for _, statement := range strings.Split(string(script), ";") {
								if strings.TrimSpace(statement) != "" {
									require.NoError(t, db.Exec(statement).Error)
								}
							}
						}
					}
					store := seed(t, db)
					exerciseLifecycle(t, store)
					exerciseConcurrentEdits(t, store)
				})
			}
		})
	}
}
