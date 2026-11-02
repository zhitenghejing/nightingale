package integration

import (
	"os"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEditableCollectionPackSeedPreservesNativeEdits(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	require.NoError(t, db.AutoMigrate(&models.BuiltinComponent{}, &models.BuiltinPayload{}, &models.BuiltinMetric{}))
	ctx := &nctx.Context{DB: db, IsCenter: true}
	root := "../../integrations/Windows_SNMP"
	b, err := os.ReadFile(root + "/markdown/README.md")
	require.NoError(t, err)
	// Native catalog IDs are BIGINT; SQLite's BIGINT primary key needs an explicit
	// value, while server dialects use their generated sequences.
	c := models.BuiltinComponent{ID: 9001, Ident: "Windows_SNMP", Readme: string(b)}
	handled, err := seedCollectionPack(ctx, root, c)
	require.True(t, handled)
	require.NoError(t, err)
	var payloads []models.BuiltinPayload
	require.NoError(t, db.Find(&payloads).Error)
	// One dashboard plus the Windows_SNMP alert rules.
	require.Len(t, payloads, 8)
	var count int64
	require.NoError(t, db.Model(&models.BuiltinMetric{}).Count(&count).Error)
	require.EqualValues(t, 11, count)
	require.NoError(t, db.Model(&c).Update("readme", "native user edit").Error)
	require.NoError(t, db.Where("type = ?", "alert").Delete(&models.BuiltinPayload{}).Error)
	_, err = seedCollectionPack(ctx, root, c)
	require.NoError(t, err)
	require.NoError(t, db.First(&c, "id = ?", c.ID).Error)
	require.Equal(t, "native user edit", c.Readme)
	require.NoError(t, db.Find(&payloads).Error)
	require.Len(t, payloads, 1)
}
