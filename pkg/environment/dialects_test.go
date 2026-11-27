package environment

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

func TestEnvironmentInitializationAndDialectConstraints(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(t.TempDir() + "/schema.db")
			case "postgres":
				if os.Getenv("N9E_TEST_ENV_POSTGRES") == "" {
					t.Skip("disposable PostgreSQL not configured")
				}
				driver = postgres.Open(os.Getenv("N9E_TEST_ENV_POSTGRES"))
			case "mysql":
				if os.Getenv("N9E_TEST_ENV_MYSQL") == "" {
					t.Skip("disposable MySQL not configured")
				}
				driver = mysql.Open(os.Getenv("N9E_TEST_ENV_MYSQL"))
			}
			db, e := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, e)
			conn, _ := db.DB()
			t.Cleanup(func() { conn.Close() })
			if dialect != "sqlite" {
				var name string
				q := "SELECT DATABASE()"
				if dialect == "postgres" {
					q = "SELECT current_database()"
				}
				require.NoError(t, db.Raw(q).Scan(&name).Error)
				require.True(t, strings.HasPrefix(name, "env_test_"), "refusing nondisposable database")
			}
			b, e := os.ReadFile("../../doc/sql/environment." + dialect + ".sql")
			require.NoError(t, e)
			for pass := 0; pass < 2; pass++ {
				for _, q := range strings.Split(string(b), ";\n") {
					if strings.TrimSpace(q) != "" {
						require.NoError(t, db.Exec(q).Error)
					}
				}
				require.NoError(t, db.AutoMigrate(models.EnvironmentTables()...))
			}
			a := models.EnvironmentRelation{Identity: "same", FromRef: "target:1", ToRef: "asset:sim", Note: strings.Repeat("来源说明", 2000), Revision: 1}
			require.NoError(t, db.Create(&a).Error)
			var stored models.EnvironmentRelation
			require.NoError(t, db.First(&stored, a.ID).Error)
			require.Equal(t, a.Note, stored.Note)
			require.Error(t, db.Create(&models.EnvironmentRelation{Identity: "same"}).Error)
			r := db.Model(&models.EnvironmentRelation{}).Where("id = ? AND revision = ?", a.ID, 0).Update("status", "withdrawn")
			require.NoError(t, r.Error)
			require.Zero(t, r.RowsAffected)
			job := models.EnvironmentDiscovery{VersionID: 123, ActorID: 1}
			require.NoError(t, db.Create(&job).Error)
			require.Error(t, db.Create(&job).Error)
		})
	}
}
