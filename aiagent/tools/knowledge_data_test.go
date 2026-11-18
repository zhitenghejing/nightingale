package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/datasource"
	"github.com/ccfos/nightingale/v6/dskit/types"
	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func knowledgeDataFixture(t *testing.T) *aiagent.ToolDeps {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/data.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sql, _ := db.DB()
	t.Cleanup(func() { sql.Close() })
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Datasource{}, &models.Target{}, &models.TargetBusiGroup{}))
	require.NoError(t, db.Create(&models.User{Id: 1, Username: "reader", Roles: "Admin"}).Error)
	require.NoError(t, db.Create(&models.Datasource{Id: 1, Name: "approved", PluginType: "mysql", Status: "enabled"}).Error)
	return &aiagent.ToolDeps{GetSQLDatasource: func(string, int64) (datasource.Datasource, bool) { return nil, false }, DBCtx: &nctx.Context{DB: db, IsCenter: true}, FilterDatasources: func(v []*models.Datasource, u *models.User) []*models.Datasource { return v }}
}
func TestKnowledgeObjectIdentityIsNotNameSimilarity(t *testing.T) {
	d := knowledgeDataFixture(t)
	p := map[string]string{"user_id": "1"}
	for _, v := range []models.Target{{Ident: "f5-east", HostIp: "10.0.0.1", Note: "F5"}, {Ident: "f5-west", HostIp: "10.0.0.2", Note: "F5"}} {
		require.NoError(t, d.DBCtx.DB.Create(&v).Error)
	}
	run := func(a map[string]interface{}) map[string]interface{} {
		s, e := resolveKnowledgeObject(context.Background(), d, a, p)
		require.NoError(t, e)
		var ev aiagent.KnowledgeDataEvidence
		require.NoError(t, json.Unmarshal([]byte(s), &ev))
		var v map[string]interface{}
		require.NoError(t, json.Unmarshal(ev.Result, &v))
		return v
	}
	require.Equal(t, "ambiguous", run(map[string]interface{}{"query": "F5"})["status"])
	require.Equal(t, "exact_identifier_match", run(map[string]interface{}{"host_ip": "10.0.0.1"})["status"])
	require.Equal(t, "not_found", run(map[string]interface{}{"ident": "f5-east", "host_ip": "10.0.0.2"})["status"])
	require.Equal(t, "not_established", run(map[string]interface{}{"ident": "f5-east"})["historical_mapping"])
	require.Empty(t, run(map[string]interface{}{"query": "%"})["candidates"])
}
func TestKnowledgeDataAuthorizesBeforeQueryAndAddsSQLLimit(t *testing.T) {
	d := knowledgeDataFixture(t)
	p := map[string]string{"user_id": "1"}
	calls := 0
	old := knowledgeDataHandlers["query_log"]
	t.Cleanup(func() { knowledgeDataHandlers["query_log"] = old })
	knowledgeDataHandlers["query_log"] = func(ctx context.Context, d *aiagent.ToolDeps, a map[string]interface{}, p map[string]string) (string, error) {
		calls++
		require.True(t, types.ReadOnlyEnforced(ctx))
		require.Contains(t, a["sql"], "AS knowledge_read LIMIT 100")
		require.Equal(t, "mysql", a["datasource_type"])
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), 15*time.Second)
		require.Len(t, p, 1)
		return `{"items":[{"backlog":17}],"count":1}`, nil
	}
	args := map[string]interface{}{"operation": "query_log", "datasource_id": float64(1), "sql": "SELECT backlog FROM queue", "datasource_type": "pgsql"}
	raw, err := queryKnowledgeData(context.Background(), d, args, p)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	var evidence aiagent.KnowledgeDataEvidence
	require.NoError(t, json.Unmarshal([]byte(raw), &evidence))
	require.Equal(t, []int64{1}, evidence.DatasourceIDs)
	require.NotZero(t, evidence.QueriedAt)
	d.FilterDatasources = func([]*models.Datasource, *models.User) []*models.Datasource { return nil }
	_, err = queryKnowledgeData(context.Background(), d, args, p)
	require.ErrorContains(t, err, "权限")
	require.Equal(t, 1, calls)
	d.FilterDatasources = func(v []*models.Datasource, u *models.User) []*models.Datasource { return v }
	require.NoError(t, d.DBCtx.DB.Model(&models.Datasource{}).Where("id=1").Update("status", "disabled").Error)
	require.Error(t, CheckKnowledgeDataAccess(context.Background(), d, 1, evidence))
	_, err = queryKnowledgeData(context.Background(), d, args, p)
	require.Error(t, err)
	require.Equal(t, 1, calls)
}
func TestKnowledgeDataRejectsWritesAndNonQueries(t *testing.T) {
	for _, sql := range []string{"DELETE\tFROM queue", "WITH x AS (DELETE FROM queue RETURNING *) SELECT * FROM x", "SELECT 1; DROP TABLE queue", "SELECT 1 INTO OUTFILE '/tmp/x'", "SELECT 1 /*!50000 INTO OUTFILE '/tmp/x' */", "CALL bad()", "EXPLAIN ANALYZE DELETE FROM queue"} {
		require.Error(t, validateKnowledgeSQL(sql), sql)
	}
	require.NoError(t, validateKnowledgeSQL("SELECT value FROM queue WHERE kind='delete'"))
	d := knowledgeDataFixture(t)
	_, err := queryKnowledgeData(context.Background(), d, map[string]interface{}{"operation": "dispatch_task_stateless"}, map[string]string{"user_id": "1"})
	require.Error(t, err)
	require.NoError(t, d.DBCtx.DB.Model(&models.User{}).Where("id=1").Update("disabled", 1).Error)
	_, err = queryKnowledgeData(context.Background(), d, map[string]interface{}{"operation": "list_datasources"}, map[string]string{"user_id": "1"})
	require.ErrorContains(t, err, "权限")
}

func TestKnowledgeEvidenceInvalidatesChangedConfigButNotHeartbeat(t *testing.T) {
	d := knowledgeDataFixture(t)
	p := map[string]string{"user_id": "1"}
	require.NoError(t, d.DBCtx.DB.Create(&models.Target{Id: 1, Ident: "east", HostIp: "10.0.0.1", UpdateAt: 1}).Error)
	require.NoError(t, d.DBCtx.DB.Create(&models.Target{Id: 2, Ident: "west", HostIp: "10.0.0.2", UpdateAt: 1}).Error)
	raw, err := queryKnowledgeData(context.Background(), d, map[string]interface{}{"operation": "get_target_detail", "ident": "east", "id": float64(2)}, p)
	require.NoError(t, err)
	var e aiagent.KnowledgeDataEvidence
	require.NoError(t, json.Unmarshal([]byte(raw), &e))
	require.Contains(t, string(e.Result), "east")
	require.NotContains(t, string(e.Result), "west")
	require.NoError(t, d.DBCtx.DB.Model(&models.Target{}).Where("id=1").Update("update_at", 2).Error)
	require.NoError(t, CheckKnowledgeDataAccess(context.Background(), d, 1, e))
	require.NoError(t, d.DBCtx.DB.Model(&models.Target{}).Where("id=1").Update("host_ip", "10.0.0.9").Error)
	require.ErrorContains(t, CheckKnowledgeDataAccess(context.Background(), d, 1, e), "变更")
	e = aiagent.KnowledgeDataEvidence{DatasourceIDs: []int64{1}}
	require.NoError(t, stampKnowledgeData(context.Background(), d, 1, &e))
	require.NoError(t, d.DBCtx.DB.Model(&models.Datasource{}).Where("id=1").Update("http", `{"url":"http://different-source"}`).Error)
	require.ErrorContains(t, CheckKnowledgeDataAccess(context.Background(), d, 1, e), "变更")
}

func TestKnowledgeSampleAgeUsesSourceTimeNotQueryTime(t *testing.T) {
	checks := knowledgeSampleAges(`{"data":[{"metric":{"ident":"test"},"value":[2000,"1988"]}]}`)
	require.Len(t, checks, 1)
	require.Equal(t, float64(12), checks[0]["lag_seconds"])
	require.Empty(t, knowledgeSampleAges(`{"data":[]}`))
}
