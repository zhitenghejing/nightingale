package cmdb

import (
	"encoding/json"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestMySQLConcurrentPlacement(t *testing.T) {
	path := os.Getenv("CMDB_MYSQL_TEST_CONFIG")
	if path == "" {
		t.Skip("isolated MySQL integration test not configured")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("test config unreadable")
	}
	var conf struct{ DSN string }
	json.Unmarshal(raw, &conf)
	dsn := strings.Replace(conf.DSN, "/n9e_cmdb?", "/n9e_cmdb_check?", 1)
	if dsn == conf.DSN {
		t.Fatal("refusing to run outside n9e_cmdb_check")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("isolated MySQL connection failed")
	}
	assertConcurrentPlacement(t, &Store{DB: db})
}

func assertConcurrentPlacement(t *testing.T, s *Store) {
	t.Helper()
	db := s.DB
	if err := s.Migrate(); err != nil {
		t.Fatal(err)
	}
	room := Room{ID: uuid.NewString(), Name: uuid.NewString()}
	rack := Rack{ID: uuid.NewString(), RoomID: room.ID, Name: "test", Units: 48, Version: 1}
	if db.Create(&room).Error != nil || db.Create(&rack).Error != nil {
		t.Fatal("setup failed")
	}
	defer func() {
		_ = db.Transaction(func(tx *gorm.DB) error {
			var assets []Asset
			tx.Where("rack_id = ?", rack.ID).Find(&assets)
			for _, asset := range assets {
				tx.Where("asset_id = ?", asset.ID).Delete(&Disk{})
				tx.Where("asset_id = ?", asset.ID).Delete(&Change{})
				tx.Delete(&asset)
			}
			tx.Delete(&rack)
			tx.Delete(&room)
			return nil
		})
	}()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := asset(uuid.NewString(), 45, 46)
			d["rackId"] = rack.ID
			results <- s.Write(func(tx *gorm.DB) error { _, err := s.Save(tx, d, 1, true); return err })
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("expected exactly one placement, got %d", success)
	}
	all, _ := s.Assets(false)
	rows := []Document{}
	for _, row := range all {
		if str(row, "rackId") == rack.ID {
			rows = append(rows, row)
		}
	}
	if len(rows) != 1 {
		t.Fatal("incorrect occupancy")
	}
	d := rows[0]
	// Two concurrent requests carrying the same version must not overwrite each other.
	results = make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy := Document{}
			json.Unmarshal([]byte(encode(d)), &copy)
			copy["note"] = uuid.NewString()
			results <- s.Write(func(tx *gorm.DB) error { _, err := s.Save(tx, copy, 1, false); return err })
		}()
	}
	wg.Wait()
	close(results)
	success = 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("expected exactly one versioned edit, got %d", success)
	}
}
