package externalpush

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/internal/testenv"
	"github.com/ccfos/nightingale/v6/models"
	gomysql "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Each test creates its own database. Never write to the DSN's source database.
func TestExternalPushDatabaseDialects(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			var open func(string) *gorm.DB
			var drop func(string)
			name := fmt.Sprintf("n9e_external_push_test_%d", time.Now().UnixNano())
			if dialect == "postgres" {
				cfg, err := pgx.ParseConfig(testenv.Require(t, "N9E_TEST_EXTERNAL_PUSH_POSTGRES_DSN"))
				if err != nil {
					t.Fatal(err)
				}
				admin, err := pgx.ConnectConfig(context.Background(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				defer admin.Close(context.Background())
				if _, err := admin.Exec(context.Background(), "CREATE DATABASE "+name); err != nil {
					t.Fatal(err)
				}
				drop = func(n string) {
					if _, err := admin.Exec(context.Background(), "DROP DATABASE "+n+" WITH (FORCE)"); err != nil {
						t.Error(err)
					}
				}
				open = func(n string) *gorm.DB {
					c := cfg.Copy()
					c.Database = n
					pool := stdlib.OpenDB(*c)
					t.Cleanup(func() { pool.Close() })
					db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
					if err != nil {
						t.Fatal(err)
					}
					return db
				}
			} else {
				cfg, err := gomysql.ParseDSN(testenv.Require(t, "N9E_TEST_EXTERNAL_PUSH_MYSQL_DSN"))
				if err != nil {
					t.Fatal(err)
				}
				admin, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{})
				if err != nil {
					t.Fatal(err)
				}
				pool, _ := admin.DB()
				defer pool.Close()
				if err := admin.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4 COLLATE utf8mb4_bin").Error; err != nil {
					t.Fatal(err)
				}
				drop = func(n string) {
					if err := admin.Exec("DROP DATABASE " + n).Error; err != nil {
						t.Error(err)
					}
				}
				open = func(n string) *gorm.DB {
					c := cfg.Clone()
					c.DBName = n
					db, err := gorm.Open(mysql.Open(c.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
					if err != nil {
						t.Fatal(err)
					}
					pool, _ := db.DB()
					t.Cleanup(func() { pool.Close() })
					return db
				}
			}
			defer drop(name)
			db := open(name)
			for _, initialization := range []string{"gorm", "upgrade_sql"} {
				t.Run(initialization, func(t *testing.T) {
					for _, table := range []string{"external_push_task", "external_push_attempt", "external_push_operation", "external_push_stream", "alert_cur_event", "alert_his_event", "notify_channel"} {
						if err := db.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
							t.Fatal(err)
						}
					}
					if initialization == "upgrade_sql" {
						for _, table := range []string{"alert_cur_event", "alert_his_event"} {
							if err := db.Exec("CREATE TABLE " + table + " (id bigint PRIMARY KEY)").Error; err != nil {
								t.Fatal(err)
							}
						}
						path := "../../docker/migratesql/external-push.postgres.sql"
						var sqlText string
						if dialect == "mysql" {
							raw, err := os.ReadFile("../../docker/migratesql/migrate.sql")
							if err != nil {
								t.Fatal(err)
							}
							parts := strings.Split(string(raw), "-- External push lifecycle identity")
							if len(parts) != 2 {
								t.Fatal("missing SQL upgrade section")
							}
							sqlText = "-- External push lifecycle identity" + parts[1]
						} else {
							raw, err := os.ReadFile(path)
							if err != nil {
								t.Fatal(err)
							}
							sqlText = string(raw)
						}
						var clean []string
						for _, line := range strings.Split(sqlText, "\n") {
							if !strings.HasPrefix(strings.TrimSpace(line), "--") {
								clean = append(clean, line)
							}
						}
						for _, stmt := range strings.Split(strings.Join(clean, "\n"), ";") {
							if strings.TrimSpace(stmt) != "" {
								if err := db.Exec(stmt).Error; err != nil {
									t.Fatal(err)
								}
							}
						}
					} else {
						if err := db.AutoMigrate(&models.AlertCurEvent{}, &models.AlertHisEvent{}, &models.ExternalPushTask{}, &models.ExternalPushAttempt{}, &models.ExternalPushOperation{}, &models.ExternalPushStream{}); err != nil {
							t.Fatal(err)
						}
					}
					if err := db.AutoMigrate(&models.NotifyChannelConfig{}); err != nil {
						t.Fatal(err)
					}
					for _, table := range []string{"alert_cur_event", "alert_his_event"} {
						for _, col := range []string{"dispatch_id", "incident_id", "incident_sequence"} {
							if !db.Migrator().HasColumn(table, col) {
								t.Fatalf("missing %s.%s", table, col)
							}
						}
					}
					var calls atomic.Int32
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"status":200}`) }))
					defer srv.Close()
					s := simulation(srv.URL)
					var wg sync.WaitGroup
					for i := 0; i < 8; i++ {
						wg.Add(1)
						go func() {
							defer wg.Done()
							if _, err := Enqueue(db, s, 0, 1); err != nil {
								t.Error(err)
							}
						}()
					}
					wg.Wait()
					var count int64
					db.Model(&models.ExternalPushTask{}).Count(&count)
					if count != 1 {
						t.Fatalf("unique constraint produced %d tasks", count)
					}
					for i := 0; i < 4; i++ {
						wg.Add(1)
						go func() {
							defer wg.Done()
							if err := (Worker{DB: db}).Step(context.Background()); err != nil {
								t.Error(err)
							}
						}()
					}
					wg.Wait()
					if calls.Load() != 1 {
						t.Fatalf("parallel workers sent %d requests", calls.Load())
					}
					var tasks []models.ExternalPushTask
					db.Find(&tasks)
					if tasks[0].State != Accepted {
						t.Fatalf("state %s", tasks[0].State)
					}
					d := state(t, db, tasks[0].ID)
					if len(d.Attempts) != 1 {
						t.Fatal("missing persisted attempt")
					}
					// The same schema/lease must also serialize different
					// reminder dispatches of a single approved incident.
					reminders := labelPlan(srv.URL)
					for i := 0; i < 8; i++ {
						candidate := reminders
						candidate.Event = *reminders.Event.DeepCopy()
						candidate.Event.DispatchID = uuid.NewString()
						candidate.Event.IncidentSequence = int64(i + 1)
						enqueue(t, db, candidate)
					}
					for i := 0; i < 4; i++ {
						wg.Add(1)
						go func() {
							defer wg.Done()
							if err := (Worker{DB: db}).Step(context.Background()); err != nil {
								t.Error(err)
							}
						}()
					}
					wg.Wait()
					if calls.Load() != 9 {
						t.Fatalf("eight distinct dispatches produced %d additional requests", calls.Load()-1)
					}
					var delivered int64
					if err := db.Model(&models.ExternalPushTask{}).Where("incident_id = ? AND state = ?", reminders.Event.IncidentID, Accepted).Count(&delivered).Error; err != nil || delivered != 8 {
						t.Fatalf("delivered=%d err=%v", delivered, err)
					}

				})
			}
		})
	}
}
