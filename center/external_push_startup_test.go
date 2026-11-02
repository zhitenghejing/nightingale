package center

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestInitializeRefusesReadOnlyIdentityMigrationBeforeServices(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged OS permissions for read-only database fault")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "old.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"alert_cur_event", "alert_his_event", "users"} {
		if err := db.Exec("CREATE TABLE " + table + " (id integer PRIMARY KEY)").Error; err != nil {
			t.Fatal(err)
		}
	}
	pool, _ := db.DB()
	pool.Close()
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0600) })
	configDir := filepath.Join(dir, "config")
	os.Mkdir(configDir, 0700)
	config := fmt.Sprintf("[DB]\nDBType='sqlite'\nDSN=%q\n[Log]\nDir=%q\nLevel='error'\n[Alert.Heartbeat]\nIP='127.0.0.1'\n[Redis]\nAddress='127.0.0.1:1'\n", path, filepath.Join(dir, "logs"))
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	cleanup, err := Initialize(configDir, "")
	if cleanup != nil {
		cleanup()
		t.Fatal("partial database started application services")
	}
	if err == nil || !strings.Contains(err.Error(), "initialize database schema") || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("migration failure not propagated by center startup: %v", err)
	}
}
