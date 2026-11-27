package knowledgebase

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
)

func TestMaintenanceSummarizesEndOfLongDocument(t *testing.T) {
	s := fixture(t)
	source(t, s, strings.Repeat("前部说明。", 3200)+"\n## 最后验收\n末章唯一依据：保留期为73天。")
	seen := false
	s.Summarize = func(_ context.Context, input string) (string, error) {
		if strings.Contains(input, "73天") {
			seen = true
		}
		return "有来源的章节摘要", nil
	}
	require.NoError(t, s.Scan())
	for i := 0; i < 20; i++ {
		_, err := s.ProcessOne(t.Context())
		require.NoError(t, err)
	}
	require.True(t, seen, "AI organization must cover the final section, not just a prefix")
}

func TestMaintenanceRejectsSummaryAfterSourceReplacement(t *testing.T) {
	s := fixture(t)
	d, _ := source(t, s, "旧资料")
	s.Summarize = func(_ context.Context, input string) (string, error) {
		require.NoError(t, s.DB.Model(&d).Update("current_version_id", 999).Error)
		return "不得发布的旧摘要", nil
	}
	require.NoError(t, s.Scan())
	_, err := s.ProcessOne(t.Context())
	require.NoError(t, err)
	var k models.LibraryVersion
	require.NoError(t, s.DB.First(&k).Error)
	require.Empty(t, k.Summary)
}

func TestMaintenanceVersionUsesRenamedSourceTitle(t *testing.T) {
	s := fixture(t)
	d, _ := source(t, s, "通用资料正文")
	require.NoError(t, s.Scan())
	_, err := s.ProcessOne(t.Context())
	require.NoError(t, err)
	rows, err := s.List(1)
	require.NoError(t, err)
	require.NoError(t, s.DB.Model(&d).Update("title", "改名后的资料").Error)
	k, err := s.Version(1, rows[0].ID)
	require.NoError(t, err)
	require.Equal(t, "改名后的资料", k.Title)
}

func TestMaintenanceSourceIndexingContinuesDuringSlowSummary(t *testing.T) {
	s := fixture(t)
	source(t, s, "第一份资料")
	require.NoError(t, s.Scan())
	_, err := s.processSource(t.Context())
	require.NoError(t, err)
	entered := make(chan struct{})
	release := make(chan struct{})
	s.Summarize = func(context.Context, string) (string, error) {
		close(entered)
		<-release
		return "第一份摘要", nil
	}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = s.processSummary(t.Context()) }()
	<-entered
	defer func() { close(release); <-done }()
	source(t, s, "第二份资料的确定性正文")
	require.NoError(t, s.Scan())
	worked, err := s.processSource(t.Context())
	require.NoError(t, err)
	require.True(t, worked)
	hits, err := s.Search(1, "第二份资料")
	require.NoError(t, err)
	require.NotEmpty(t, hits)
}

func TestMaintenanceRetryRetainsCompletedChaptersAndModelChangeInvalidates(t *testing.T) {
	s := fixture(t)
	source(t, s, "## 第一章\n前部事实\n## 第二章\n后部事实")
	model := "model-A"
	s.SummaryModel = func() string { return model }
	calls := map[string]int{}
	failSecond := true
	s.Summarize = func(_ context.Context, input string) (string, error) {
		key := "first"
		if strings.Contains(input, "后部") {
			key = "second"
		}
		calls[key]++
		if key == "second" && failSecond {
			return "", context.DeadlineExceeded
		}
		return "对应章节摘要 " + model, nil
	}
	require.NoError(t, s.Scan())
	for i := 0; i < 2; i++ {
		_, err := s.ProcessOne(t.Context())
		require.NoError(t, err)
	}
	var k models.LibraryVersion
	require.NoError(t, s.DB.First(&k).Error)
	failSecond = false
	require.NoError(t, s.Retry(1, k.ID))
	_, err := s.ProcessOne(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, calls["first"])
	require.Equal(t, 2, calls["second"])
	current, err := s.Version(1, k.ID)
	require.NoError(t, err)
	require.Contains(t, string(current.Summary), "2/2")
	model = "model-B"
	current, err = s.Version(1, k.ID)
	require.NoError(t, err)
	require.Empty(t, current.Summary)
	require.NoError(t, s.Scan())
	for i := 0; i < 2; i++ {
		_, err = s.ProcessOne(t.Context())
		require.NoError(t, err)
	}
	current, err = s.Version(1, k.ID)
	require.NoError(t, err)
	require.Contains(t, string(current.Summary), "model-B")
	require.NotContains(t, string(current.Summary), "model-A")
}

func TestIndexedRelevancePrecedesWindowAndScopedSearchReturnsMoreChapters(t *testing.T) {
	s := fixture(t)
	d, _ := source(t, s, "## 联合证据\n罕见项目蓝鲸的存储容量规划\n## 第二章\n蓝鲸规划第二部分\n## 第三章\n蓝鲸规划第三部分")
	require.NoError(t, s.Scan())
	_, err := s.ProcessOne(t.Context())
	require.NoError(t, err)
	noiseDoc, v := source(t, s, "噪声")
	require.NoError(t, s.Scan())
	_, err = s.ProcessOne(t.Context())
	require.NoError(t, err)
	var k models.LibraryVersion
	require.NoError(t, s.DB.Where("source_version_id=?", v.ID).First(&k).Error)
	for i := 0; i < 1005; i++ {
		section := models.LibrarySection{VersionID: k.ID, Position: i + 2, Body: "普通项目的存储容量记录"}
		require.NoError(t, s.DB.Create(&section).Error)
		require.NoError(t, writeTerms(s.DB, section, noiseDoc.Title))
	}
	hits, err := s.Search(1, "蓝鲸存储容量规划")
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	require.Equal(t, d.ID, hits[0].DocumentID)
	hits, err = s.SearchScoped(1, "蓝鲸规划", d.Title, 0)
	require.NoError(t, err)
	// The source helper uses identical titles; explicit version avoids ambiguity.
	var correct models.LibraryVersion
	require.NoError(t, s.DB.Where("document_id=?", d.ID).First(&correct).Error)
	hits, err = s.SearchScoped(1, "蓝鲸规划", "", correct.ID)
	require.NoError(t, err)
	require.Len(t, hits, 3)
}

func TestMaintenanceRepairsMissingSectionsWithoutBlockingOtherJobs(t *testing.T) {
	s := fixture(t)
	source(t, s, "可重建的原文唯一事实")
	require.NoError(t, s.Scan())
	_, err := s.processSource(t.Context())
	require.NoError(t, err)
	var k models.LibraryVersion
	require.NoError(t, s.DB.First(&k).Error)
	require.NoError(t, s.DB.Where("version_id=?", k.ID).Delete(&models.LibrarySection{}).Error)
	s.Summarize = func(context.Context, string) (string, error) { return "原文事实摘要", nil }
	worked, err := s.processSummary(t.Context())
	require.NoError(t, err)
	require.True(t, worked)
	require.NoError(t, s.DB.First(&k, k.ID).Error)
	require.Equal(t, "queued", k.State)
	_, err = s.ProcessOne(t.Context())
	require.NoError(t, err)
	hits, err := s.Search(1, "可重建的原文")
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	current, err := s.Version(1, k.ID)
	require.NoError(t, err)
	require.Equal(t, "ready", current.SummaryState)
}

func TestCompletedOrganizationCanBeRegeneratedWithoutChangingSource(t *testing.T) {
	s := fixture(t)
	_, original := source(t, s, "原件只保留事实73天")
	text := "第一版AI草稿"
	s.Summarize = func(context.Context, string) (string, error) { return text, nil }
	require.NoError(t, s.Scan())
	_, err := s.ProcessOne(t.Context())
	require.NoError(t, err)
	var k models.LibraryVersion
	require.NoError(t, s.DB.First(&k).Error)
	require.Equal(t, "ready", k.SummaryState)
	digest := k.Digest
	text = "重新生成后的AI草稿"
	require.NoError(t, s.Retry(1, k.ID))
	current, err := s.Version(1, k.ID)
	require.NoError(t, err)
	require.Empty(t, current.Summary)
	hits, err := s.Search(1, "原件只保留事实")
	require.NoError(t, err)
	require.NotEmpty(t, hits)
	_, err = s.ProcessOne(t.Context())
	require.NoError(t, err)
	current, err = s.Version(1, k.ID)
	require.NoError(t, err)
	require.Contains(t, string(current.Summary), text)
	require.Equal(t, original.ID, current.SourceVersionID)
	require.Equal(t, digest, current.Digest)
}

func TestExplicitUnfinishedRetryDoesNotRegenerateJustCompletedWork(t *testing.T) {
	s := fixture(t)
	source(t, s, "保持原文与已完成整理")
	calls := 0
	s.Summarize = func(context.Context, string) (string, error) { calls++; return "已完成整理", nil }
	require.NoError(t, s.Scan())
	_, err := s.ProcessOne(t.Context())
	require.NoError(t, err)
	var k models.LibraryVersion
	require.NoError(t, s.DB.First(&k).Error)
	require.NoError(t, s.Reprocess(1, k.ID, false))
	_, err = s.ProcessOne(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.NoError(t, s.Reprocess(1, k.ID, true))
	_, err = s.ProcessOne(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestParserRevisionRequeuesAndRebuildsFromImmutableSource(t *testing.T) {
	s := fixture(t)
	_, sourceVersion := source(t, s, "原件真值73天")
	require.NoError(t, s.Scan())
	_, err := s.processSource(t.Context())
	require.NoError(t, err)
	var k models.LibraryVersion
	require.NoError(t, s.DB.First(&k).Error)
	var section models.LibrarySection
	require.NoError(t, s.DB.Where("version_id=?", k.ID).First(&section).Error)
	require.NoError(t, s.DB.Model(&k).Updates(map[string]interface{}{"parser_revision": "archive-v1", "markdown": "旧解析正文", "summary": "旧整理"}).Error)
	require.NoError(t, s.Scan())
	require.NoError(t, s.DB.First(&k, k.ID).Error)
	require.Equal(t, "queued", k.State)
	require.Empty(t, k.Markdown)
	require.Empty(t, k.Summary)
	_, err = s.Read(1, section.ID)
	require.Error(t, err, "outdated extraction must leave retrieval until rebuilt")
	_, err = s.processSource(t.Context())
	require.NoError(t, err)
	require.NoError(t, s.DB.First(&k, k.ID).Error)
	require.Equal(t, parserRevision, k.ParserRevision)
	require.Equal(t, "ready", k.State)
	require.Contains(t, string(k.Markdown), "原件真值73天")
	require.Equal(t, sourceVersion.ID, k.SourceVersionID)
}

func TestParserRevisionRequeueRespectsActiveLease(t *testing.T) {
	s := fixture(t)
	source(t, s, "原件")
	require.NoError(t, s.Scan())
	_, err := s.processSource(t.Context())
	require.NoError(t, err)
	var k models.LibraryVersion
	require.NoError(t, s.DB.First(&k).Error)
	require.NoError(t, s.DB.Model(&k).Updates(map[string]interface{}{"parser_revision": "archive-v1", "lease_until": time.Now().Add(time.Minute).Unix()}).Error)
	require.NoError(t, s.Scan())
	require.NoError(t, s.DB.First(&k, k.ID).Error)
	require.Equal(t, "ready", k.State)
	require.NoError(t, s.DB.Model(&k).Update("lease_until", time.Now().Add(-time.Minute).Unix()).Error)
	require.NoError(t, s.Scan())
	require.NoError(t, s.DB.First(&k, k.ID).Error)
	require.Equal(t, "queued", k.State)
}

func TestParserRevisionRequeueIsBoundedAndResumes(t *testing.T) {
	s := fixture(t)
	for i := 0; i < 12; i++ {
		source(t, s, "待重建原件")
	}
	require.NoError(t, s.Scan())
	for i := 0; i < 12; i++ {
		_, err := s.processSource(t.Context())
		require.NoError(t, err)
	}
	require.NoError(t, s.DB.Model(&models.LibraryVersion{}).Where("state=?", "ready").Update("parser_revision", "archive-v1").Error)
	require.NoError(t, s.Scan())
	var n int64
	require.NoError(t, s.DB.Model(&models.LibraryVersion{}).Where("state=?", "queued").Count(&n).Error)
	require.EqualValues(t, 10, n)
	require.NoError(t, s.Scan())
	require.NoError(t, s.DB.Model(&models.LibraryVersion{}).Where("state=?", "queued").Count(&n).Error)
	require.EqualValues(t, 12, n)
}

func TestParserRevisionRequeuesLegacyNullableMetadata(t *testing.T) {
	s := fixture(t)
	source(t, s, "兼容旧元数据的原件")
	require.NoError(t, s.Scan())
	_, err := s.processSource(t.Context())
	require.NoError(t, err)
	var k models.LibraryVersion
	require.NoError(t, s.DB.First(&k).Error)
	require.NoError(t, s.DB.Model(&k).Updates(map[string]interface{}{"parser_revision": nil, "lease_until": nil}).Error)
	require.NoError(t, s.Scan())
	require.NoError(t, s.DB.First(&k, k.ID).Error)
	require.Equal(t, "queued", k.State)
	worked, err := s.processSource(t.Context())
	require.NoError(t, err)
	require.True(t, worked)
	require.NoError(t, s.DB.First(&k, k.ID).Error)
	require.Equal(t, parserRevision, k.ParserRevision)
}
