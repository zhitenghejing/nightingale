package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentNativeRoutesAndEvidence(t *testing.T) {
	engine, rt, user := archiveTestRouter(t)
	tables := append(models.EnvironmentTables(), models.LibraryTables()...)
	tables = append(tables, &models.BusiGroup{}, &models.Target{}, &models.TargetBusiGroup{}, &models.MonitorDevice{}, &cmdb.Asset{})
	require.NoError(t, rt.Ctx.DB.AutoMigrate(tables...))
	require.NoError(t, rt.Ctx.DB.Create(&models.BusiGroup{Id: 1, Name: "simulation"}).Error)
	require.NoError(t, rt.Ctx.DB.Model(user).Update("roles", "Admin").Error)
	rt.configEnvironmentRoutes(engine.Group("/api/n9e"))
	in := gin.H{"group_id": 1, "kind": "service", "name": "Sim-ESB"}
	w := archiveRequest(engine, http.MethodPost, "/api/n9e/environment/objects", in, "")
	require.NotEqual(t, 200, w.Code)
	w = archiveRequest(engine, http.MethodPost, "/api/n9e/environment/objects", in, user.Username)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = archiveRequest(engine, http.MethodGet, "/api/n9e/environment/context?ref=service:1", nil, user.Username)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "Sim-ESB")
	receipt := aiagent.KnowledgeDataEvidence{Operation: "environment_context", EnvironmentRefs: map[string]string{"service:1": "test"}, QueriedAt: 1, Result: json.RawMessage(`{"entity":{}}`)}
	b, e := json.Marshal(receipt)
	require.NoError(t, e)
	ev := knowledgeAnswerEvidence{}
	ev.observe("query_environment", string(b))
	require.Len(t, ev.Data, 1)
	require.Nil(t, ev.Data[0].Result)
}
