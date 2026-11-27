package knowledgebase

import (
	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExactEquipmentIdentifierSurvivesGenericCandidateLimit(t *testing.T) {
	s := fixture(t)
	d, v := source(t, s, "NX9000 两块磁盘故障，系统仍可运行。")
	require.NoError(t, s.DB.Model(&d).Update("title", "北院2025年4月巡检报告").Error)
	require.NoError(t, s.Scan())
	_, err := s.ProcessOne(t.Context())
	require.NoError(t, err)
	d2, v2 := source(t, s, "一般巡检记录")
	require.NoError(t, s.Scan())
	_, err = s.ProcessOne(t.Context())
	require.NoError(t, err)
	var k models.LibraryVersion
	require.NoError(t, s.DB.Where("source_version_id=?", v2.ID).First(&k).Error)
	noise := make([]models.LibrarySection, 1005)
	for i := range noise {
		noise[i] = models.LibrarySection{VersionID: k.ID, Heading: "巡检", Body: "北院2025年4月巡检报告 一般检查"}
	}
	require.NoError(t, s.DB.CreateInBatches(noise, 100).Error)
	hits, err := s.Search(1, "北院2025年4月巡检 NX9000")
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.Equal(t, v.ID, hits[0].SourceVersionID)
	require.NotEqual(t, d2.ID, hits[0].DocumentID)
}

func TestEquipmentIdentifiersIncludeDigitLeadingSerialsButNotDates(t *testing.T) {
	for _, id := range []string{"7XD1W2X", "CN791304C2", "NX9000", "172.100.126.21"} {
		require.Equal(t, []string{id}, specificKnowledgeIdentifier.FindAllString(id, -1))
	}
	require.Empty(t, specificKnowledgeIdentifier.FindAllString("2025年4月 2025-04-20", -1))
}

func TestVersionAndCellTokensDoNotHardExcludeRelevantDocuments(t *testing.T) {
	s := fixture(t)
	source(t, s, "合成路由维护手册：检查网络状态。")
	require.NoError(t, s.Scan())
	_, err := s.ProcessOne(t.Context())
	require.NoError(t, err)
	for _, q := range []string{"维护手册 V2", "维护手册 A12"} {
		hits, err := s.Search(1, q)
		require.NoError(t, err)
		require.NotEmpty(t, hits, q)
	}
}

func TestStrongMissingIdentifierDoesNotReturnUnrelatedKeywordHits(t *testing.T) {
	s := fixture(t)
	source(t, s, "独立编码说明：资产编码需要人工核实；设备记录只包含CN791304C2。")
	require.NoError(t, s.Scan())
	_, err := s.ProcessOne(t.Context())
	require.NoError(t, err)
	hits, err := s.Search(1, "独立编码 KB-NO-SOURCE-7319728")
	require.NoError(t, err)
	require.Empty(t, hits)
	hits, err = s.Search(1, "设备记录 CN791304C2")
	require.NoError(t, err)
	require.NotEmpty(t, hits)
}

func TestUserLiteralScopeOverridesAContradictoryModelIdentifier(t *testing.T) {
	s := fixture(t)
	a, _ := source(t, s, "正确记录 CN791304C2")
	source(t, s, "其他记录 CN791304C3")
	require.NoError(t, s.Scan())
	for i := 0; i < 2; i++ {
		_, err := s.ProcessOne(t.Context())
		require.NoError(t, err)
	}
	hits, err := s.SearchWithin(1, "其他记录 CN791304C3", 0, SourceScope{Identifiers: []string{"CN791304C2"}})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	for _, hit := range hits {
		require.Equal(t, a.ID, hit.DocumentID)
	}
	require.True(t, (SourceScope{Identifiers: []string{"CN791304C2"}}).Matches("资料", "SN: CN791304C2"))
	require.False(t, (SourceScope{Identifiers: []string{"CN791304C2"}}).Matches("资料", "CN791304C22"))
}

func TestLiteralDocumentScopeStillAllowsReadingOtherChapters(t *testing.T) {
	s := fixture(t)
	source(t, s, "## 设备列表\n设备序列号 CN791304C2。\n## 容量规划\n本报告的存储容量规划为120TB。")
	require.NoError(t, s.Scan())
	_, err := s.ProcessOne(t.Context())
	require.NoError(t, err)
	hits, err := s.SearchWithin(1, "容量规划", 0, SourceScope{Identifiers: []string{"cn791304c2"}})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.Contains(t, hits[0].Body, "120TB")
}

func TestNumericSerialFindsConflictingRecordsButNotPrefixNeighbors(t *testing.T) {
	s := fixture(t)
	first, _ := source(t, s, "设备序列号5066001979，位置B3 29U。")
	second, _ := source(t, s, "设备序列号5066001979，位置B03-18U。")
	neighbor, _ := source(t, s, "设备序列号50660019799，位置其他机房。")
	require.NoError(t, s.Scan())
	for i := 0; i < 3; i++ {
		_, err := s.ProcessOne(t.Context())
		require.NoError(t, err)
	}
	hits, err := s.Search(1, "5066001979是什么设备")
	require.NoError(t, err)
	docs := map[int64]bool{}
	for _, h := range hits {
		docs[h.DocumentID] = true
	}
	require.True(t, docs[first.ID])
	require.True(t, docs[second.ID])
	require.False(t, docs[neighbor.ID])
	require.Equal(t, []string{"5066001979"}, specificKnowledgeIdentifier.FindAllString("5066001979", -1))
}

func TestNamedDocumentKeepsFieldDefinitionsDespiteIdentifierScoreBoost(t *testing.T) {
	s := fixture(t)
	d, _ := source(t, s, "## 历史统计\n标识kbaccept27-历史共124台，健康仍需核查。\n## 版本标签口径\nagent_version字段中accept-v1表示验收一组，accept-v2表示验收二组。这不是健康评级。")
	require.NoError(t, s.DB.Model(&d).Update("title", "联合验收说明").Error)
	require.NoError(t, s.Scan())
	_, err := s.ProcessOne(t.Context())
	require.NoError(t, err)
	hits, err := s.SearchWithin(1, "kbaccept27 agent_version 版本标签 解释 健康", 0, SourceScope{Title: "联合验收说明"})
	require.NoError(t, err)
	headings := []string{}
	for _, h := range hits {
		headings = append(headings, h.Heading)
	}
	require.Contains(t, headings, "版本标签口径")
	require.Contains(t, headings, "历史统计")
}
