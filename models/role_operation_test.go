package models

import (
	"testing"

	"github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Replace the old manual permission-insertion script against a fixed MySQL DB
// with a test of the real binding operation and its repeatability.
func TestRoleOperationBindIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&RoleOperation{}))
	c := &ctx.Context{DB: db, IsCenter: true}
	ops := []string{"/alert-mutes/put", "/log/index-patterns", "/help/variable-configs", "/ibex-settings"}
	for i := 0; i < 2; i++ {
		require.NoError(t, RoleOperationBind(c, "Standard", ops))
		got, err := OperationsOfRole(c, []string{"Standard"})
		require.NoError(t, err)
		require.ElementsMatch(t, ops, got)
		var count int64
		require.NoError(t, db.Model(&RoleOperation{}).Count(&count).Error)
		require.EqualValues(t, len(ops), count)
	}
}
