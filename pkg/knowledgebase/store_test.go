package knowledgebase

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/archives"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	db, e := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, e)
	sql, _ := db.DB()
	sql.SetMaxOpenConns(1)
	t.Cleanup(func() { sql.Close() })
	tables := append(models.ArchiveTables(), models.LibraryTables()...)
	tables = append(tables, &models.User{})
	require.NoError(t, db.AutoMigrate(tables...))
	require.NoError(t, db.Create(&models.User{Id: 1, Username: "reader", Roles: "Standard"}).Error)
	a := archives.New(db, t.TempDir())
	require.NoError(t, a.Prepare())
	return &Store{DB: db, Archives: a}
}
func source(t *testing.T, s *Store, text string) (models.ArchiveDocument, models.ArchiveDocumentVersion) {
	t.Helper()
	a := models.ArchiveRecord{Category: "other", Title: "资料", State: "draft", Metadata: "{}"}
	require.NoError(t, s.DB.Create(&a).Error)
	d := models.ArchiveDocument{ArchiveID: a.ID, Title: "接口维护手册", Kind: "online"}
	require.NoError(t, s.DB.Create(&d).Error)
	body, _ := json.Marshal(map[string]interface{}{"data": map[string]interface{}{"main": []interface{}{map[string]interface{}{"value": text}}}})
	v := models.ArchiveDocumentVersion{DocumentID: d.ID, Sequence: 1, HasContent: true, Body: models.ArchiveJSON(body)}
	require.NoError(t, s.DB.Create(&v).Error)
	require.NoError(t, s.DB.Model(&d).Update("current_version_id", v.ID).Error)
	return d, v
}
func TestArchiveIndexLifecycleAndPermission(t *testing.T) {
	s := fixture(t)
	d, v := source(t, s, "## 报告回传\n检验接口积压超过 15 条，先核查队列。禁止直接删除业务数据。")
	require.NoError(t, s.Scan())
	require.NoError(t, s.Scan())
	worked, e := s.ProcessOne(context.Background())
	require.True(t, worked)
	require.NoError(t, e)
	hits, e := s.Search(1, "检验接口积压")
	require.NoError(t, e)
	require.NotEmpty(t, hits)
	require.Contains(t, hits[0].Body, "15 条")
	read, e := s.Read(1, hits[0].ID)
	require.NoError(t, e)
	require.Equal(t, v.ID, read.SourceVersionID)
	require.Contains(t, read.URL, "?version=")
	require.NotContains(t, read.URL, "version_id=")
	_, e = s.Search(0, "接口")
	require.Error(t, e)
	require.NoError(t, s.DB.Model(&models.User{}).Where("id=1").Update("disabled", 1).Error)
	_, e = s.Read(1, hits[0].ID)
	require.ErrorContains(t, e, "没有权限")
	require.NoError(t, s.DB.Model(&models.User{}).Where("id=1").Update("disabled", 0).Error)
	// A new formal version invalidates old search immediately, before indexing.
	v2 := models.ArchiveDocumentVersion{DocumentID: d.ID, Sequence: 2, HasContent: true, Body: `{"data":{"main":[{"value":"新版说明：影像回传"}]}}`}
	require.NoError(t, s.DB.Create(&v2).Error)
	require.NoError(t, s.DB.Model(&d).Update("current_version_id", v2.ID).Error)
	hits2, e := s.Search(1, "检验接口积压")
	require.NoError(t, e)
	require.Empty(t, hits2)
	_, e = s.Read(1, hits[0].ID)
	require.Error(t, e)
	require.NoError(t, s.Scan())
	_, e = s.ProcessOne(context.Background())
	require.NoError(t, e)
	hits2, e = s.Search(1, "影像回传")
	require.NoError(t, e)
	require.NotEmpty(t, hits2)
	require.NoError(t, s.DB.Model(&d).Update("deleted", true).Error)
	hits2, e = s.Search(1, "影像回传")
	require.NoError(t, e)
	require.Empty(t, hits2)
	_, e = s.Read(1, hits[0].ID)
	require.Error(t, e)
}
func TestSummaryFailureDoesNotHideRawSource(t *testing.T) {
	s := fixture(t)
	source(t, s, "培训内容：接口队列积压时应联系负责人。")
	s.Summarize = func(context.Context, string) (string, error) { return "", errors.New("model down") }
	require.NoError(t, s.Scan())
	_, e := s.ProcessOne(context.Background())
	require.NoError(t, e)
	hits, e := s.Search(1, "接口队列")
	require.NoError(t, e)
	require.NotEmpty(t, hits)
	rows, e := s.List(1)
	require.NoError(t, e)
	require.Equal(t, "ready", rows[0].State)
	require.Equal(t, "retry", rows[0].SummaryState)
	require.NoError(t, s.Retry(1, rows[0].ID))
	s.Summarize = func(context.Context, string) (string, error) { return "待核实摘要", nil }
	_, e = s.ProcessOne(context.Background())
	require.NoError(t, e)
	v, e := s.Version(1, rows[0].ID)
	require.NoError(t, e)
	require.Contains(t, string(v.Summary), "待核实摘要")
}
func TestLeaseAndUncommittedDraftExcluded(t *testing.T) {
	s := fixture(t)
	d, _ := source(t, s, "磁盘容量")
	require.NoError(t, s.DB.Create(&models.ArchiveDraft{DocumentID: d.ID, OwnerID: 1, Body: `{"data":{"main":[{"value":"秘密草稿独有词"}]}}`}).Error)
	require.NoError(t, s.Scan())
	var k models.LibraryVersion
	require.NoError(t, s.DB.First(&k).Error)
	require.NoError(t, s.DB.Model(&k).Updates(map[string]interface{}{"lease": "busy", "lease_until": time.Now().Unix() + 90}).Error)
	worked, e := s.ProcessOne(context.Background())
	require.False(t, worked)
	require.NoError(t, e)
	require.NoError(t, s.DB.Model(&k).Update("lease_until", 0).Error)
	worked, e = s.ProcessOne(context.Background())
	require.True(t, worked)
	require.NoError(t, e)
	hits, e := s.Search(1, "秘密草稿独有词")
	require.NoError(t, e)
	require.Empty(t, hits)
}
func TestExtractDocxTablesAndCanvas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.docx")
	f, e := os.Create(path)
	require.NoError(t, e)
	z := zip.NewWriter(f)
	w, e := z.Create("word/document.xml")
	require.NoError(t, e)
	_, e = w.Write([]byte(`<w:document xmlns:w="urn:test"><w:body><w:tbl><w:tr><w:tc><w:p><w:r><w:t>报告延迟</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>15 分钟</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>`))
	require.NoError(t, e)
	require.NoError(t, z.Close())
	require.NoError(t, f.Close())
	text, e := Extract(context.Background(), "docx", path, "")
	require.NoError(t, e)
	require.Contains(t, text, "15 分钟")
	require.Contains(t, text, "报告延迟")
	_, e = Extract(context.Background(), "image", "", "")
	require.ErrorIs(t, e, ErrOCR)
	sections := Split("# 接口检查\n" + string(make([]byte, 0)) + "报告积压\n## 恢复标准\n验证 15 分钟")
	require.Len(t, sections, 2)
	require.Equal(t, "恢复标准", sections[1].Heading)
}

func TestExpiredSummaryWorkerCannotPublish(t *testing.T) {
	s := fixture(t)
	source(t, s, "接口观察窗口 45 分钟")
	s.Summarize = func(context.Context, string) (string, error) {
		require.NoError(t, s.DB.Model(&models.LibraryVersion{}).Where("document_id > 0").Update("lease_until", time.Now().Unix()-1).Error)
		return "expired worker summary", nil
	}
	require.NoError(t, s.Scan())
	_, e := s.ProcessOne(context.Background())
	require.NoError(t, e)
	rows, e := s.List(1)
	require.NoError(t, e)
	v, e := s.Version(1, rows[0].ID)
	require.NoError(t, e)
	require.Equal(t, "ready", v.State)
	require.Empty(t, v.Summary)
	require.Equal(t, "pending", v.SummaryState)
}

func TestSpecificKeywordsSuppressIncidentalLongReportMatches(t *testing.T) {
	s := fixture(t)
	source(t, s, "清禾接口积压检查阈值为17条，观察窗口45分钟。")
	d, _ := source(t, s, "日常说明：检查系统窗口和数据，日常检查正常。")
	require.NoError(t, s.DB.Model(&d).Update("title", "培训资料说明").Error)
	require.NoError(t, s.Scan())
	for i := 0; i < 2; i++ {
		_, e := s.ProcessOne(context.Background())
		require.NoError(t, e)
	}
	h, e := s.Search(1, "清禾接口积压检查阈值和观察窗口是什么？请引用资料。")
	require.NoError(t, e)
	require.NotEmpty(t, h)
	for _, hit := range h {
		require.NotEqual(t, d.ID, hit.DocumentID)
	}
}

func TestLongSummaryCoverageCannotBeOmittedByModel(t *testing.T) {
	s := fixture(t)
	source(t, s, strings.Repeat("客户资料内容。", 2400))
	s.Summarize = func(_ context.Context, input string) (string, error) {
		require.Less(t, len([]rune(input)), 14100)
		return "模型摘要没有主动注明覆盖范围", nil
	}
	require.NoError(t, s.Scan())
	_, e := s.ProcessOne(context.Background())
	require.NoError(t, e)
	rows, e := s.List(1)
	require.NoError(t, e)
	v, e := s.Version(1, rows[0].ID)
	require.NoError(t, e)
	require.Contains(t, string(v.Summary), "覆盖范围（系统标记）")
	require.Contains(t, string(v.Summary), "已整理 1/6 节")
}
