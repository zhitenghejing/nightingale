package cmdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/internal/testenv"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Only disposable databases created by this test receive schema or asset writes.
func TestPostgresSchemaAndConcurrentPlacement(t *testing.T) {
	config, err := pgx.ParseConfig(testenv.Require(t, "N9E_TEST_POSTGRES_DSN"))
	require.NoError(t, err)
	admin, err := pgx.ConnectConfig(context.Background(), config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	for _, initialization := range []string{"gorm", "sql"} {
		t.Run(initialization, func(t *testing.T) {
			name := fmt.Sprintf("n9e_cmdb_test_%d", time.Now().UnixNano())
			_, err := admin.Exec(context.Background(), "CREATE DATABASE "+name)
			require.NoError(t, err)
			t.Cleanup(func() {
				_, err := admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
				require.NoError(t, err)
			})
			appConfig := config.Copy()
			appConfig.Database = name
			app, err := pgx.ConnectConfig(context.Background(), appConfig)
			require.NoError(t, err)
			t.Cleanup(func() { _ = app.Close(context.Background()) })
			if initialization == "sql" {
				sql, err := os.ReadFile(filepath.Join("..", "..", "doc", "sql", "cmdb.postgres.sql"))
				require.NoError(t, err)
				for i := 0; i < 2; i++ {
					_, err = app.Exec(context.Background(), string(sql))
					require.NoError(t, err)
				}
			}
			// ConnString retains the original parsed string; OpenDB honors the copied Database field.
			pool := stdlib.OpenDB(*appConfig)
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			var actualDatabase string
			require.NoError(t, db.Raw("SELECT current_database()").Scan(&actualDatabase).Error)
			require.Equal(t, name, actualDatabase)
			s := &Store{DB: db}
			require.NoError(t, s.Migrate())
			require.NoError(t, s.Migrate())
			assertConcurrentPlacement(t, s)
			assertCatalogDeletion(t, s)
			assertRoomEditing(t, s)
			assertRecyclePurge(t, s)
			assertConcurrentPurgeRestore(t, s)
			assertConcurrentCatalogDelete(t, s)
		})
	}
}

// Deletion races with placement through the same lock; neither ordering may leave an orphan.
func assertConcurrentCatalogDelete(t *testing.T, s *Store) {
	room := Room{ID: "delete-race-room", Name: "delete race room"}
	rack := Rack{ID: "delete-race-rack", RoomID: room.ID, Name: "race rack", Units: 42, Version: 1}
	require.NoError(t, s.DB.Create(&room).Error)
	require.NoError(t, s.DB.Create(&rack).Error)
	start := make(chan struct{})
	saved := make(chan error, 1)
	deleted := make(chan int, 1)
	go func() {
		<-start
		doc := asset("delete-race", 1, 2)
		doc["rackId"] = rack.ID
		saved <- s.Write(func(tx *gorm.DB) error { _, err := s.Save(tx, doc, 1, true); return err })
	}()
	go func() {
		<-start
		deleted <- catalogCall(t, s, User{ID: 1, Admin: true}, "DELETE", "racks/"+rack.ID, rack).Code
	}()
	close(start)
	err := <-saved
	code := <-deleted
	if code == 200 {
		require.Error(t, err)
	} else {
		require.Equal(t, 400, code)
		require.NoError(t, err)
	}
	var orphans int64
	require.NoError(t, s.DB.Model(&Asset{}).Where("rack_id = ? AND NOT EXISTS (SELECT 1 FROM cmdb_rack WHERE cmdb_rack.id = cmdb_device.rack_id)", rack.ID).Count(&orphans).Error)
	require.Zero(t, orphans)
}
