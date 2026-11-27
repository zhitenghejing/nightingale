package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/datasource"
	"github.com/ccfos/nightingale/v6/datasource/postgresql"
	"github.com/ccfos/nightingale/v6/dskit/postgres"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
)

func TestKnowledgeSQLRealReadonlyPostgres(t *testing.T) {
	config := os.Getenv("N9E_KNOWLEDGE_SQL_FIXTURE")
	if config == "" {
		t.Skip("N9E_KNOWLEDGE_SQL_FIXTURE not configured: disposable PostgreSQL read-only fixture required")
	}
	b, err := os.ReadFile(config)
	require.NoError(t, err)
	var shard postgres.PostgreSQL
	require.NoError(t, json.Unmarshal(b, &shard))
	d := knowledgeDataFixture(t)
	require.NoError(t, d.DBCtx.DB.Model(&models.Datasource{}).Where("id=1").Update("plugin_type", "pgsql").Error)
	plug := &postgresql.PostgreSQL{Shards: []*postgres.PostgreSQL{&shard}}
	d.GetSQLDatasource = func(string, int64) (datasource.Datasource, bool) { return plug, true }
	call := func(op, sql string) (aiagent.KnowledgeDataEvidence, error) {
		raw, err := queryKnowledgeData(context.Background(), d, map[string]interface{}{"operation": op, "datasource_id": float64(1), "sql": sql, "limit": float64(100)}, map[string]string{"user_id": "1"})
		if err != nil {
			return aiagent.KnowledgeDataEvidence{}, err
		}
		var e aiagent.KnowledgeDataEvidence
		err = json.Unmarshal([]byte(raw), &e)
		return e, err
	}
	e, err := call("query_log", "SELECT record_id, backlog FROM qa_queue ORDER BY record_id")
	require.NoError(t, err)
	var result struct {
		Items []map[string]interface{} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(e.Result, &result))
	require.Len(t, result.Items, 100)
	require.Equal(t, float64(17), result.Items[0]["backlog"])
	e, err = call("query_log", "SELECT current_setting('transaction_read_only') AS read_only")
	require.NoError(t, err)
	require.Contains(t, string(e.Result), "on")
	e, err = call("list_tables", "")
	require.NoError(t, err)
	require.Contains(t, string(e.Result), "qa_queue")
	e, err = call("list_databases", "")
	require.NoError(t, err)
	var databases []string
	require.NoError(t, json.Unmarshal(e.Result, &databases))
	require.Equal(t, []string{shard.DB}, databases)
	require.Equal(t, shard.DB, plug.Shards[0].DB, "Q&A cannot mutate shared adapter database")
	_, err = call("query_log", "SELECT qa_side_effect()")
	require.Error(t, err, "SELECT function side effects must be blocked by the database transaction")
	e, err = call("query_log", "SELECT count(*) AS records FROM qa_queue")
	require.NoError(t, err)
	require.Contains(t, string(e.Result), "150")
	_, err = call("query_log", "UPDATE qa_queue SET backlog=0")
	require.Error(t, err)
}
