package tools

import (
	"context"
	"encoding/json"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/knowledgebase"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOptionalKnowledgeScopeZeroAndExactTitle(t *testing.T) {
	d := knowledgeDataFixture(t)
	db := d.DBCtx.DB
	tables := append(models.ArchiveTables(), models.LibraryTables()...)
	tables = append(tables, &models.BusiGroup{})
	require.NoError(t, db.AutoMigrate(tables...))
	require.NoError(t, db.Create(&models.ArchiveRecord{ID: 1}).Error)
	for i, title := range []string{"[北院] 合成资产.xlsx", "[北院] 旧合成资产.xlsx"} {
		id := int64(i + 1)
		require.NoError(t, db.Create(&models.ArchiveDocument{ID: id, ArchiveID: 1, Title: title, CurrentVersionID: id + 10}).Error)
		require.NoError(t, db.Create(&models.LibraryVersion{ID: id + 20, DocumentID: id, ArchiveID: 1, SourceVersionID: id + 10, State: "ready"}).Error)
		require.NoError(t, db.Create(&models.LibrarySection{VersionID: id + 20, Body: "合成服务器 NX77"}).Error)
	}
	d.Library = &knowledgebase.Store{DB: db}
	p := map[string]string{"user_id": "1"}
	for _, args := range []map[string]interface{}{{"query": "合成服务器", "document_title": "合成资产.xlsx"}, {"query": "合成服务器", "document_title": "合成资产.xlsx", "version_id": float64(0)}} {
		raw, err := searchLibrary(context.Background(), d, args, p)
		require.NoError(t, err)
		var result struct {
			Documents     []knowledgebase.Hit `json:"documents"`
			ReadDocuments []knowledgebase.Hit `json:"read_documents"`
		}
		require.NoError(t, json.Unmarshal([]byte(raw), &result))
		require.Len(t, result.Documents, 1)
		require.Equal(t, int64(1), result.Documents[0].DocumentID)
		require.Empty(t, result.Documents[0].Body, "candidate catalogs must not masquerade as factual reads")
		require.NotEmpty(t, result.ReadDocuments)
		require.Contains(t, result.ReadDocuments[0].Body, "合成服务器")
	}
	for _, v := range []interface{}{float64(-1), 1.2, "21", nil} {
		_, err := searchLibrary(context.Background(), d, map[string]interface{}{"query": "合成服务器", "version_id": v}, p)
		require.Error(t, err)
	}
	raw, err := searchLibrary(context.Background(), d, map[string]interface{}{"query": "合成服务器", "version_id": float64(999)}, p)
	require.NoError(t, err)
	require.Contains(t, raw, "scope_note")
}

func TestKnowledgeUserNamedSourceCannotBeWidenedByModel(t *testing.T) {
	d := knowledgeDataFixture(t)
	db := d.DBCtx.DB
	require.NoError(t, db.AutoMigrate(append(models.ArchiveTables(), models.LibraryTables()...)...))
	require.NoError(t, db.AutoMigrate(&models.BusiGroup{}))
	require.NoError(t, db.Create(&models.ArchiveRecord{ID: 1}).Error)
	for i, title := range []string{"[北院] 本轮资料.xlsx", "[北院] 其他资料.xlsx"} {
		id := int64(i + 1)
		require.NoError(t, db.Create(&models.ArchiveDocument{ID: id, ArchiveID: 1, Title: title, CurrentVersionID: id + 10}).Error)
		require.NoError(t, db.Create(&models.LibraryVersion{ID: id + 20, DocumentID: id, ArchiveID: 1, SourceVersionID: id + 10, State: "ready", Title: title}).Error)
		require.NoError(t, db.Create(&models.LibrarySection{ID: id + 30, VersionID: id + 20, Body: "设备记录"}).Error)
	}
	d.Library = &knowledgebase.Store{DB: db}
	params := map[string]string{"user_id": "1", "knowledge_document_title": "本轮资料.xlsx"}
	raw, err := searchLibrary(t.Context(), d, map[string]interface{}{"query": "设备记录"}, params)
	require.NoError(t, err)
	var result struct {
		Documents []knowledgebase.Hit `json:"documents"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &result))
	require.Len(t, result.Documents, 1)
	require.EqualValues(t, 1, result.Documents[0].DocumentID)
	_, err = readLibrary(t.Context(), d, map[string]interface{}{"section_id": float64(32)}, params)
	require.ErrorContains(t, err, "指定资料")
	_, err = queryKnowledgeWorkbook(t.Context(), d, map[string]interface{}{"version_id": float64(22), "operation": "sheets"}, params)
	require.ErrorContains(t, err, "指定资料")
}

func TestKnowledgeLiteralIdentifierCannotBeDroppedByModelSearch(t *testing.T) {
	d := knowledgeDataFixture(t)
	db := d.DBCtx.DB
	require.NoError(t, db.AutoMigrate(append(models.ArchiveTables(), models.LibraryTables()...)...))
	require.NoError(t, db.AutoMigrate(&models.BusiGroup{}))
	require.NoError(t, db.Create(&models.ArchiveRecord{ID: 1}).Error)
	require.NoError(t, db.Create(&models.ArchiveDocument{ID: 1, ArchiveID: 1, Title: "无关资料", CurrentVersionID: 10}).Error)
	require.NoError(t, db.Create(&models.LibraryVersion{ID: 20, DocumentID: 1, ArchiveID: 1, SourceVersionID: 10, State: "ready", Title: "无关资料", Markdown: "独立编码说明：这是其他记录，没有所需序列号。"}).Error)
	require.NoError(t, db.Create(&models.LibrarySection{ID: 30, VersionID: 20, Body: "独立编码说明：这是其他记录，没有所需序列号。"}).Error)
	d.Library = &knowledgebase.Store{DB: db}
	params := map[string]string{"user_id": "1", "knowledge_identifiers": `["KB-NO-SOURCE-7319728"]`}
	raw, err := searchLibrary(t.Context(), d, map[string]interface{}{"query": "独立编码说明"}, params)
	require.NoError(t, err)
	var result struct {
		Documents []knowledgebase.Hit `json:"documents"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &result))
	require.Empty(t, result.Documents)
	_, err = readLibrary(t.Context(), d, map[string]interface{}{"section_id": float64(30)}, params)
	require.ErrorContains(t, err, "指定")
}
