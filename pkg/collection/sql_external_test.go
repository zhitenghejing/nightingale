package collection

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
	"github.com/ccfos/nightingale/v6/models"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCollectionSQLDialects(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			key := "N9E_TEST_POSTGRES_DSN"
			if dialect == "mysql" {
				key = "N9E_TEST_MYSQL_DSN"
			}
			dsn := testenv.Require(t, key)
			for _, initialization := range []string{"gorm", "sql"} {
				t.Run(initialization, func(t *testing.T) {
					name := fmt.Sprintf("n9e_collection_test_%d", time.Now().UnixNano())
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
						script, err := os.ReadFile(filepath.Join("..", "..", "doc", "sql", "collection."+dialect+".sql"))
						require.NoError(t, err)
						for i := 0; i < 2; i++ {
							for _, statement := range strings.Split(string(script), ";") {
								if strings.TrimSpace(statement) != "" {
									require.NoError(t, db.Exec(statement).Error)
								}
							}
						}
					}
					s, scope, c, cred, secret := fixtureStore(t, db)
					verifyLegacyAddressUpgrade(t, s, scope, c, cred)
					// Independent Store instances prove database serialization rather than a process mutex.
					other := &Store{DB: db, Keys: s.Keys, Writers: s.Writers}
					start := make(chan struct{})
					errs := make(chan error, 2)
					for i, store := range []*Store{s, other} {
						go func(i int, store *Store) {
							<-start
							_, err := store.SaveDevice(scope, "", fmt.Sprint("sql-concurrent-", i), deviceInput(c, cred, "198.51.100.10"))
							errs <- err
						}(i, store)
					}
					close(start)
					require.NoError(t, <-errs)
					require.NoError(t, <-errs)
					snapshot, _, err := s.Provider(c.ID, secret, "")
					require.NoError(t, err)
					require.Len(t, snapshot.Configs["snmp"], 2)
					var d models.MonitorDevice
					require.NoError(t, db.First(&d).Error)
					f := inputFromDevice(d)
					f.Name = "One optimistic update"
					start = make(chan struct{})
					for _, store := range []*Store{s, other} {
						go func(store *Store) { <-start; _, err := store.SaveDevice(scope, d.ID, "", f); errs <- err }(store)
					}
					close(start)
					a, b := <-errs, <-errs
					require.True(t, (a == nil) != (b == nil), "exactly one update must win")
					if a != nil {
						require.Equal(t, 409, a.(*Error).Code)
					}
					if b != nil {
						require.Equal(t, 409, b.(*Error).Code)
					}
					// Large authenticated snapshot storage must exceed MySQL TEXT's limit safely.
					large := strings.Repeat("fixture", 20000)
					sealed, key, err := s.Keys.Seal([]byte(large), "publication:large")
					require.NoError(t, err)
					p := models.MonitorPublication{ID: "large", CollectorID: c.ID, Version: "large", Ciphertext: models.MonitorText(sealed), KeyID: key}
					require.NoError(t, db.Create(&p).Error)
					var read models.MonitorPublication
					require.NoError(t, db.First(&read, "id = ?", "large").Error)
					plain, err := s.Keys.Open(string(read.Ciphertext), read.KeyID, "publication:large")
					require.NoError(t, err)
					require.Equal(t, large, string(plain))
					verifyTemplateVersions(t, s, scope, c, cred, secret)
					versions, err := s.Templates()
					require.NoError(t, err)
					var latest TemplateView
					for _, v := range versions {
						if v.Version == 2 {
							latest = v
						}
					}
					input := TemplateInput{Name: latest.Name, Component: latest.Component, Config: latest.Config, BaseVersionID: latest.ID}
					start = make(chan struct{})
					for _, store := range []*Store{s, other} {
						go func(store *Store) { <-start; _, err := store.SaveTemplate(scope, input); errs <- err }(store)
					}
					close(start)
					a, b = <-errs, <-errs
					require.True(t, (a == nil) != (b == nil), "exactly one template publication must win")
					if a != nil {
						require.Equal(t, 409, a.(*Error).Code)
					}
					if b != nil {
						require.Equal(t, 409, b.(*Error).Code)
					}
					verifyNativeProfileSQL(t, s, scope, c, cred)
					verifyCatalogSQL(t, s, scope)
					verifyCatalogOnboardingSQL(t, s, scope)
				})
			}
		})
	}
}
