package knowledgebase

import (
	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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
			db, e := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, e)
			raw, _ := db.DB()
			defer raw.Close()
			if dialect != "sqlite" {
				var name string
				q := "SELECT DATABASE()"
				if dialect == "postgres" {
					q = "SELECT current_database()"
				}
				require.NoError(t, db.Raw(q).Scan(&name).Error)
				require.True(t, strings.HasPrefix(name, "library_test_"), "refusing nondisposable database")
			}
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
			workbook := models.LibraryWorkbook{VersionID: item.ID, Data: models.LibraryText(strings.Repeat("中文单元格", 20000))}
			require.NoError(t, db.Create(&workbook).Error)
			var storedWorkbook models.LibraryWorkbook
			require.NoError(t, db.First(&storedWorkbook, "version_id=?", item.ID).Error)
			require.Equal(t, workbook.Data, storedWorkbook.Data)
			require.Error(t, db.Create(&workbook).Error)
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
