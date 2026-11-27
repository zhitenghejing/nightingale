package tools

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	"github.com/stretchr/testify/require"
)

func TestAssetLookupUsesExactCMDBSerialAndRechecksChanges(t *testing.T) {
	d := knowledgeDataFixture(t)
	db := d.DBCtx.DB
	require.NoError(t, db.AutoMigrate(&cmdb.Asset{}, &cmdb.Access{}))
	// Beyond the old generic environment scanner's window, with a prefix collision.
	for i := 0; i < 501; i++ {
		require.NoError(t, db.Create(&cmdb.Asset{ID: fmt.Sprintf("a-%04d", i), Serial: fmt.Sprintf("filler-%04d", i)}).Error)
	}
	var asset cmdb.Asset
	require.NoError(t, json.Unmarshal([]byte(`{"ID":"wanted","Serial":"005066001979","Version":3,"Payload":"{\"serial\":\"005066001979\",\"vendor\":\"深信服\",\"model\":\"AF-1000-G640-HL\",\"password\":\"must-not-leak\"}"}`), &asset))
	require.NoError(t, db.Create(&asset).Error)
	require.NoError(t, db.Create(&cmdb.Asset{ID: "prefix", Serial: "0050660019799"}).Error)
	raw, handled, err := aiagent.ExecuteBuiltinTool(t.Context(), d, "query_knowledge_data", map[string]string{"user_id": "1"}, `{"operation":"asset_lookup","serial":"005066001979"}`)
	require.True(t, handled)
	require.NoError(t, err)
	require.Contains(t, raw, "深信服")
	require.NotContains(t, raw, "must-not-leak")
	require.NotContains(t, raw, `"password"`)
	var receipt aiagent.KnowledgeDataEvidence
	require.NoError(t, json.Unmarshal([]byte(raw), &receipt))
	require.Len(t, receipt.AssetRefs, 1)
	require.Contains(t, receipt.AssetRefs, "wanted")
	require.NoError(t, CheckKnowledgeDataAccess(t.Context(), d, 1, receipt))
	require.NoError(t, db.Model(&cmdb.Asset{}).Where("id = ?", "wanted").Update("version", 4).Error)
	require.Error(t, CheckKnowledgeDataAccess(t.Context(), d, 1, receipt))
	_, _, err = aiagent.ExecuteBuiltinTool(t.Context(), d, "query_knowledge_data", map[string]string{"user_id": "1"}, `{"operation":"asset_lookup","serial":5066001979}`)
	require.ErrorContains(t, err, "string")
	_, _, err = aiagent.ExecuteBuiltinTool(t.Context(), d, "query_knowledge_data", map[string]string{"user_id": "1"}, `{"operation":"asset_lookup"}`)
	require.Error(t, err)
}
func TestAssetLookupDoesNotBorrowTargetPermissions(t *testing.T) {
	d := knowledgeDataFixture(t)
	db := d.DBCtx.DB
	require.NoError(t, db.AutoMigrate(&cmdb.Asset{}, &cmdb.Access{}))
	require.NoError(t, db.Model(&models.User{}).Where("id=1").Update("roles", "Standard").Error)
	_, err := queryKnowledgeAsset(t.Context(), d, map[string]interface{}{"serial": "5066001979"}, map[string]string{"user_id": "1"})
	require.ErrorContains(t, err, "权限")
	require.NoError(t, db.Create(&cmdb.Access{UserID: 1, Read: true}).Error)
	raw, err := queryKnowledgeAsset(t.Context(), d, map[string]interface{}{"serial": "5066001979"}, map[string]string{"user_id": "1"})
	require.NoError(t, err)
	require.Contains(t, raw, "not_found")
	var receipt aiagent.KnowledgeDataEvidence
	require.NoError(t, json.Unmarshal([]byte(raw), &receipt))
	require.NoError(t, CheckKnowledgeDataAccess(t.Context(), d, 1, receipt))
	require.NoError(t, db.Model(&cmdb.Access{}).Where("user_id=1").Update("read", false).Error)
	require.Error(t, CheckKnowledgeDataAccess(t.Context(), d, 1, receipt))
}

func TestNativeReadReceiptPreservesSchemaAndReauthorizes(t *testing.T) {
	d := knowledgeDataFixture(t)
	require.NoError(t, d.DBCtx.DB.Create(&models.Target{Ident: "native-1", Note: "demo"}).Error)
	raw, err := getTargetDetail(t.Context(), d, map[string]interface{}{"ident": "native-1"}, map[string]string{"user_id": "1"})
	require.NoError(t, err)
	e, err := NativeReadEvidence(t.Context(), d, 1, "get_target_detail", `{"ident":"native-1"}`, raw)
	require.NoError(t, err)
	require.NotNil(t, e)
	require.Equal(t, []string{"native-1"}, e.TargetIdents)
	require.NoError(t, CheckKnowledgeDataAccess(t.Context(), d, 1, *e))
	require.NoError(t, d.DBCtx.DB.Model(&models.Target{}).Where("ident=?", "native-1").Update("note", "changed").Error)
	require.Error(t, CheckKnowledgeDataAccess(t.Context(), d, 1, *e))
	require.NotContains(t, raw, "queried_at", "native response schema remains unchanged")
}
