package router

import (
	"testing"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/memsto"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/ccfos/nightingale/v6/pkg/remotediag"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMergedChatDependenciesServeInventoryAndKnowledge(t *testing.T) {
	rt, _, _ := optimizationWorkbook(t)
	db := rt.Library.DB
	rt.Ctx = &nctx.Context{DB: db, IsCenter: true}
	rt.DatasourceCache = &memsto.DatasourceCacheType{}
	store := &cmdb.Store{DB: db}
	require.NoError(t, store.Migrate())
	require.NoError(t, store.Write(func(tx *gorm.DB) error {
		_, err := store.Save(tx, cmdb.Document{"serial": "MERGE-001", "customer": "Fixture", "hostname": "merged-device", "model": "merge-model"}, 1, true)
		return err
	}))
	rt.CMDB = &cmdb.API{Store: store}
	rt.RemoteDiagnostics = remotediag.New(rt.Center.RemoteDiagnostics)
	require.NoError(t, db.Create(&models.LibrarySection{ID: 77, VersionID: 4, Heading: "Fixture", Body: "merge-source-fact"}).Error)
	deps := rt.buildToolDeps()
	require.Same(t, rt.RemoteDiagnostics, deps.RemoteDiagnostics)
	for _, tc := range []struct{ tool, args, want string }{
		{"search_cmdb_devices", `{"keyword":"MERGE-001"}`, "merged-device"},
		{"query_knowledge_data", `{"operation":"asset_lookup","serial":"MERGE-001"}`, "merge-model"},
		{"read_knowledge", `{"section_id":77}`, "merge-source-fact"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			result, handled, err := aiagent.ExecuteBuiltinTool(t.Context(), deps, tc.tool, map[string]string{"user_id": "1"}, tc.args)
			require.True(t, handled)
			require.NoError(t, err)
			require.Contains(t, result, tc.want)
		})
	}
}
