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
			require.NoError(t, db.Create(&models.LibrarySection{VersionID: item.ID, Body: "接口_delay 15 分钟 CN791304C2"}).Error)
			var count int64
			require.NoError(t, db.Model(&models.LibrarySection{}).Where("LOWER(body) LIKE ? ESCAPE '!'", "%接口!_delay%").Count(&count).Error)
			require.Equal(t, int64(1), count)
			// Exact filename scope uses SQL LIKE only for the archive display prefix.
			require.NoError(t, db.Model(&item).Update("title", "[北院] 合成_设备.xlsx").Error)
			require.NoError(t, db.Create(&models.LibraryVersion{SourceVersionID: 2, Title: "[北院] 旧合成_设备.xlsx"}).Error)
			require.NoError(t, exactTitleScope(db.Table("knowledge_document_version AS d"), "合成_设备.xlsx").Count(&count).Error)
			require.Equal(t, int64(1), count)
			// Execute the actual scoped join and soft identifier ranking on all engines.
			require.NoError(t, db.AutoMigrate(append(models.ArchiveTables(), &models.User{})...))
			require.NoError(t, db.Create(&models.User{Id: 1, Username: "reader", Roles: "Admin"}).Error)
			require.NoError(t, db.Create(&models.ArchiveRecord{ID: 1}).Error)
			require.NoError(t, db.Create(&models.ArchiveDocument{ID: 1, ArchiveID: 1, Title: "[北院] 合成_设备.xlsx", CurrentVersionID: 1}).Error)
			require.NoError(t, db.Model(&item).Updates(map[string]interface{}{"document_id": 1, "archive_id": 1, "state": "ready"}).Error)
			hits, err := (&Store{DB: db}).SearchScoped(1, "接口_delay V2", "合成_设备.xlsx", 0)
			require.NoError(t, err)
			require.NotEmpty(t, hits)
			require.Equal(t, int64(1), hits[0].DocumentID)
			// The new posting query and fenced retry execute on real dialects too.
			var section models.LibrarySection
			require.NoError(t, db.Where("version_id=?", item.ID).First(&section).Error)
			require.NoError(t, writeTerms(db, section, "[北院] 合成_设备.xlsx"))
			require.NoError(t, db.Model(&item).Update("index_revision", indexRevision).Error)
			hits, err = (&Store{DB: db}).SearchScoped(1, "接口_delay V2", "合成_设备.xlsx", 0)
			require.NoError(t, err)
			require.NotEmpty(t, hits)
			hits, err = (&Store{DB: db}).SearchScoped(1, "CN791304C2", "合成_设备.xlsx", 0)
			require.NoError(t, err)
			require.NotEmpty(t, hits)
			hits, err = (&Store{DB: db}).SearchWithin(1, "接口_delay", 0, SourceScope{Identifiers: []string{"cn791304c2"}})
			require.NoError(t, err)
			require.NotEmpty(t, hits)
			require.NoError(t, (&Store{DB: db}).Retry(1, item.ID))
		})
	}
}
