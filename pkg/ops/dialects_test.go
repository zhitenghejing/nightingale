package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledge"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestOpsInitializationSQLAndDialectConstraints(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			if dialect == "sqlite" {
				driver = sqlite.Open(filepath.Join(t.TempDir(), "schema.db"))
			} else {
				env := "N9E_TEST_OPS_" + strings.ToUpper(dialect)
				dsn := os.Getenv(env)
				if dsn == "" {
					t.Skip(env + " is not configured")
				}
				if dialect == "postgres" {
					driver = postgres.Open(dsn)
				} else {
					driver = mysql.Open(dsn)
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			conn, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			if dialect != "sqlite" {
				var name string
				query := "SELECT DATABASE()"
				if dialect == "postgres" {
					query = "SELECT current_database()"
				}
				require.NoError(t, db.Raw(query).Scan(&name).Error)
				require.True(t, strings.HasPrefix(name, "ops_test_"), "Refusing to run against a non-disposable database")
			}
			sql, err := os.ReadFile(filepath.Join("..", "..", "doc", "sql", "ops."+dialect+".sql"))
			require.NoError(t, err)
			for pass := 0; pass < 2; pass++ {
				for _, statement := range strings.Split(string(sql), ";") {
					if strings.TrimSpace(statement) != "" {
						require.NoError(t, db.Exec(statement).Error)
					}
				}
			}
			tables := append(models.OpsTables(), models.KnowledgeTables()...)
			require.NoError(t, db.AutoMigrate(tables...))
			require.NoError(t, db.AutoMigrate(tables...))
			for _, table := range tables {
				require.True(t, db.Migrator().HasTable(table))
			}
			// Upgrade a pre-plan schema in place, preserving existing rows.
			legacy := models.OpsCase{GroupID: 1, Title: "Existing case before plans", State: "processing", KnowledgeVersionID: 17, Revision: 3, Generation: 2}
			require.NoError(t, db.Create(&legacy).Error)
			for _, table := range []interface{}{&models.OpsCase{}, &models.OpsStep{}, &models.OpsAction{}, &models.OpsVerification{}} {
				require.NoError(t, db.Migrator().DropColumn(table, "plan_id"))
			}
			require.NoError(t, db.Migrator().DropTable(&models.OpsPlan{}))
			upgrade, err := os.ReadFile(filepath.Join("..", "..", "docker", "migratesql", "ops-case-plan."+dialect+".sql"))
			require.NoError(t, err)
			for _, statement := range strings.Split(string(upgrade), ";") {
				if strings.TrimSpace(statement) != "" {
					require.NoError(t, db.Exec(statement).Error)
				}
			}
			require.NoError(t, db.AutoMigrate(tables...))
			require.NoError(t, db.AutoMigrate(tables...))
			var upgraded models.OpsCase
			require.NoError(t, db.First(&upgraded, legacy.ID).Error)
			require.Equal(t, legacy.KnowledgeVersionID, upgraded.KnowledgeVersionID)
			require.Zero(t, upgraded.PlanID)
			plan := models.OpsPlan{CaseID: legacy.ID, Sequence: 1, Title: "First plan", State: "draft", SHA256: "fixture"}
			require.NoError(t, db.Create(&plan).Error)
			plan.ID = 0
			require.Error(t, db.Create(&plan).Error)
			item := models.KnowledgeItem{GroupID: 1, Title: "Large payload", Draft: models.OpsJSON(fmt.Sprintf(`{"body":%q}`, strings.Repeat("verified-知识", 12000))), Revision: 1}
			require.NoError(t, db.Create(&item).Error)
			var got models.KnowledgeItem
			require.NoError(t, db.First(&got, item.ID).Error)
			require.Equal(t, item.Draft, got.Draft)
			id := int64(99)
			one := models.OpsRun{CaseID: id, ActiveCaseID: &id, State: "queued", Mode: "guided"}
			require.NoError(t, db.Create(&one).Error)
			duplicate := models.OpsRun{CaseID: id, ActiveCaseID: &id, State: "queued", Mode: "guided"}
			require.Error(t, db.Create(&duplicate).Error)
			require.NoError(t, db.Model(&one).Update("active_case_id", nil).Error)
			require.NoError(t, db.Create(&duplicate).Error)
			v := models.KnowledgeVersion{ItemID: item.ID, GroupID: 1, Sequence: 1, Title: "v1", Payload: item.Draft, SHA256: knowledge.Digest([]byte(item.Draft))}
			require.NoError(t, db.Create(&v).Error)
			v.ID = 0
			require.Error(t, db.Create(&v).Error)
		})
	}
}
