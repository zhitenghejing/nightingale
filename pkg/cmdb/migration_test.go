package cmdb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSQLiteSQLSchemaSupportsPlacement(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	sql, err := os.ReadFile(filepath.Join("..", "..", "doc", "sql", "cmdb.sqlite.sql"))
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Exec(string(sql)).Error)
	}
	s := &Store{DB: db}
	require.NoError(t, s.Migrate())
	fixtureRoom := Room{ID: "room", Name: "Test room"}
	fixtureRack := Rack{ID: "rack", RoomID: "room", Name: "Test rack", Units: 42, Version: 1}
	require.NoError(t, db.Create(&fixtureRoom).Error)
	require.NoError(t, db.Create(&fixtureRack).Error)
	save(t, s, asset("SQL-001", 1, 2), true)
	// Losing the serialization row must fail closed, rather than allowing overlapping writes.
	require.NoError(t, db.Delete(&writeLock{}, "id = 1").Error)
	err = s.Write(func(tx *gorm.DB) error { _, e := s.Save(tx, asset("SQL-002", 1, 2), 1, true); return e })
	require.ErrorContains(t, err, "write lock is missing")
}
