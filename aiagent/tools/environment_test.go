package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	"github.com/ccfos/nightingale/v6/pkg/environment"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentToolScopeReceiptsAndReadOnly(t *testing.T) {
	d := knowledgeDataFixture(t)
	db := d.DBCtx.DB
	tables := append(models.EnvironmentTables(), models.LibraryTables()...)
	tables = append(tables, models.ArchiveTables()...)
	tables = append(tables, &cmdb.Asset{}, &cmdb.Access{}, &models.MonitorDevice{}, &models.BusiGroup{})
	require.NoError(t, db.AutoMigrate(tables...))
	require.NoError(t, db.Create(&models.BusiGroup{Id: 1, Name: "simulation"}).Error)
	s := &environment.Store{DB: db}
	o, e := s.SaveObject(1, models.EnvironmentObject{Name: "Sim-ESB", Kind: "service", GroupID: 1})
	require.NoError(t, e)
	require.NotZero(t, o.ID)
	p := map[string]string{"user_id": "1"}
	raw, e := queryEnvironment(context.Background(), d, map[string]interface{}{"operation": "context", "ref": "service:1", "user_id": 3}, p)
	require.NoError(t, e)
	var receipt aiagent.KnowledgeDataEvidence
	require.NoError(t, json.Unmarshal([]byte(raw), &receipt))
	require.Contains(t, receipt.EnvironmentRefs, "service:1")
	require.NoError(t, CheckKnowledgeDataAccess(context.Background(), d, 1, receipt))
	o.Name = "Changed"
	_, e = s.SaveObject(1, o)
	require.NoError(t, e)
	require.Error(t, CheckKnowledgeDataAccess(context.Background(), d, 1, receipt))
	for _, op := range []string{"write", "delete", "create", "sql"} {
		_, e = queryEnvironment(context.Background(), d, map[string]interface{}{"operation": op}, p)
		require.Error(t, e)
	}
	_, e = queryEnvironment(context.Background(), d, map[string]interface{}{"operation": "trace", "ref": "service:1", "depth": float64(100)}, p)
	require.Error(t, e)
}
