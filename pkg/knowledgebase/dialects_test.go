package knowledgebase

import (
	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"os"
	"strings"
	"testing"
)

func TestLibraryInitializationAndDialectQueries(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(t.TempDir() + "/schema.db")
			case "postgres":
				dsn := os.Getenv("N9E_TEST_LIBRARY_POSTGRES")
				if dsn == "" {
					t.Skip("disposable PostgreSQL not configured")
				}
				driver = postgres.Open(dsn)
			case "mysql":
				dsn := os.Getenv("N9E_TEST_LIBRARY_MYSQL")
				if dsn == "" {
					t.Skip("disposable MySQL not configured")
				}
				driver = mysql.Open(dsn)
			}
			db, e := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, e)
			raw, _ := db.DB()
			defer raw.Close()
			sql, e := os.ReadFile("../../doc/sql/knowledge-documents." + dialect + ".sql")
			require.NoError(t, e)
			for pass := 0; pass < 2; pass++ {
				for _, statement := range strings.Split(string(sql), ";\n") {
					if strings.TrimSpace(statement) != "" {
						require.NoError(t, db.Exec(statement).Error)
					}
				}
				require.NoError(t, db.AutoMigrate(models.LibraryTables()...))
			}
			item := models.LibraryVersion{SourceVersionID: 1, Markdown: models.LibraryText(strings.Repeat("中文表格 15 分钟\n", 12000))}
			require.NoError(t, db.Create(&item).Error)
			var stored models.LibraryVersion
			require.NoError(t, db.First(&stored, item.ID).Error)
			require.Equal(t, item.Markdown, stored.Markdown)
			duplicate := models.LibraryVersion{SourceVersionID: 1}
			require.Error(t, db.Create(&duplicate).Error)
			// Exercise LIKE escaping and non-ASCII long text on each engine.
			require.NoError(t, db.Create(&models.LibrarySection{VersionID: item.ID, Body: "接口_delay 15 分钟"}).Error)
			var count int64
			require.NoError(t, db.Model(&models.LibrarySection{}).Where("LOWER(body) LIKE ? ESCAPE '!'", "%接口!_delay%").Count(&count).Error)
			require.Equal(t, int64(1), count)
		})
	}
}
