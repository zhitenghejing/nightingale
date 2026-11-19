package tools

import (
	"context"
	"encoding/json"
	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledgebase"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestWorkbookToolRevalidatesVersionAndActor(t *testing.T) {
	d := knowledgeDataFixture(t)
	db := d.DBCtx.DB
	require.NoError(t, db.AutoMigrate(models.ArchiveTables()...))
	require.NoError(t, db.AutoMigrate(models.LibraryTables()...))
	a := models.ArchiveRecord{ID: 1}
	require.NoError(t, db.Create(&a).Error)
	doc := models.ArchiveDocument{ID: 1, ArchiveID: 1, CurrentVersionID: 10, Kind: "xlsx"}
	require.NoError(t, db.Create(&doc).Error)
	k := models.LibraryVersion{ID: 20, DocumentID: 1, ArchiveID: 1, SourceVersionID: 10, Sequence: 2, Title: "模拟巡检", State: "ready", Digest: "fixed-source-digest"}
	require.NoError(t, db.Create(&k).Error)
	require.NoError(t, db.Create(&models.LibraryWorkbook{VersionID: 20, Data: `{"sheets":[{"name":"数据","rows":2,"columns":1,"cells":[{"address":"A2","row":2,"column":1,"raw":"2","value":"2","type":"n"}]}]}`}).Error)
	d.Library = &knowledgebase.Store{DB: db}
	raw, e := queryKnowledgeWorkbook(context.Background(), d, map[string]interface{}{"version_id": float64(20), "operation": "sum", "sheet": "数据", "range": "A2:A2", "column": "A", "user_id": 999}, map[string]string{"user_id": "1"})
	require.NoError(t, e)
	var receipt aiagent.KnowledgeDataEvidence
	require.NoError(t, json.Unmarshal([]byte(raw), &receipt))
	require.Equal(t, int64(20), receipt.WorkbookVersion)
	require.Contains(t, receipt.SourceLabel, "V2")
	require.Contains(t, string(receipt.Result), "2.000000000000")
	require.NoError(t, CheckKnowledgeDataAccess(context.Background(), d, 1, receipt))
	require.NoError(t, db.Model(&models.User{}).Where("id=1").Update("disabled", 1).Error)
	require.Error(t, CheckKnowledgeDataAccess(context.Background(), d, 1, receipt))
	require.NoError(t, db.Model(&models.User{}).Where("id=1").Update("disabled", 0).Error)
	require.NoError(t, db.Model(&doc).Update("current_version_id", 11).Error)
	require.Error(t, CheckKnowledgeDataAccess(context.Background(), d, 1, receipt))
}
