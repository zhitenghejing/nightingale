package archives

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestArchiveDatabaseDialects(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("N9E_TEST_ARCHIVE_" + strings.ToUpper(dialect))
			if dsn == "" {
				t.Skip("archive dialect DSN not configured")
			}
			var driver gorm.Dialector
			sqlFile := "docker/initsql/a-n9e.sql"
			schema := "archive_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if dialect == "postgres" {
				driver = postgres.Open(dsn + " search_path=" + schema)
				sqlFile = "docker/compose-postgres/initsql_for_postgres/a-n9e-for-Postgres.sql"
			} else {
				// A separate disposable database is mandatory. Never run on an existing business database.
				if !strings.Contains(dsn, "/archive_test_") {
					t.Fatal("MySQL DSN must select an archive_test_ database")
				}
				driver = mysql.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			defer sqlDB.Close()
			sqlDB.SetMaxOpenConns(4)
			if dialect == "postgres" {
				if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
					t.Fatal(err)
				}
				defer db.Exec("DROP SCHEMA " + schema + " CASCADE")
			}
			data, err := os.ReadFile(filepath.Join("..", sqlFile))
			if err != nil {
				t.Fatal(err)
			}
			marker := "-- Archive management: isolated records,"
			index := strings.LastIndex(string(data), marker)
			if index < 0 {
				t.Fatal("archive initialization SQL missing")
			}
			for _, statement := range strings.Split(string(data[index:]), ";") {
				if strings.TrimSpace(statement) != "" {
					if err := db.Exec(statement).Error; err != nil {
						t.Fatalf("initialization SQL: %v", err)
					}
				}
			}
			if err := db.AutoMigrate(models.ArchiveTables()...); err != nil {
				t.Fatal(err)
			}
			s := New(db, t.TempDir())
			a, err := s.Create(alice, "other", Metadata{Title: "Dialect verification"})
			if err != nil {
				t.Fatal(err)
			}
			a, err = s.CreateOnline(alice, a.ID, CreateDocument{Title: "Large body", Purpose: "other", Body: body(strings.Repeat("正文", 15000))})
			if err != nil {
				t.Fatal(err)
			}
			docID := a.Documents[0].ID
			d, err := s.Document(docID, 0)
			if err != nil || len(d.Version.Body) < 65536 {
				t.Fatalf("long JSON round-trip: %v", err)
			}
			results := make(chan error, 2)
			var group sync.WaitGroup
			for i := 0; i < 2; i++ {
				group.Add(1)
				go func() { defer group.Done(); _, err := s.Acquire(alice, docID); results <- err }()
			}
			group.Wait()
			close(results)
			successes := 0
			for err := range results {
				if err == nil {
					successes++
				} else {
					problemStatus(t, err, 409)
				}
			}
			if successes != 1 {
				t.Fatalf("expected one editing window, got %d", successes)
			}
			var lease models.ArchiveEditLease
			if err := db.First(&lease, docID).Error; err != nil {
				t.Fatal(err)
			}
			saved, err := s.Save(alice, docID, edit(lease, body("saved")))
			if err != nil {
				t.Fatal(err)
			}
			if saved.Version.Sequence != 2 {
				t.Fatal(saved.Version.Sequence)
			}
			if err := s.Release(alice, docID, lease.SessionID, lease.Epoch); err != nil {
				t.Fatal(err)
			}
			current, err := s.Get(a.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Archive(alice, a.ID, current.Revision); err != nil {
				t.Fatal(err)
			}
			restored, err := s.RestoreDocument(alice, docID, d.Version.ID)
			if err != nil || restored.Version.Sequence != 3 {
				t.Fatalf("restore: %v", err)
			}
			// Verify heartbeat updates on drivers reporting changed rows, not just SQLite matched rows.
			s.Now = func() time.Time { return time.Now() }
			next, err := s.Acquire(bob, docID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Heartbeat(bob, docID, next.Lease.SessionID, next.Lease.Epoch); err != nil {
				t.Fatal(err)
			}
		})
	}
}
