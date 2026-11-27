package router

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/memsto"
	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/stretchr/testify/require"
)

func TestUnifiedEvidenceUsesOnlyCitedDocumentsAndDeduplicatesVersion(t *testing.T) {
	rt, _, _ := optimizationWorkbook(t)
	db := rt.Library.DB
	for _, section := range []models.LibrarySection{{ID: 10, VersionID: 4, Heading: "位置", Body: "B3 29U"}, {ID: 11, VersionID: 4, Heading: "巡检", Body: "内存36%"}, {ID: 12, VersionID: 4, Heading: "目录", Body: "目录"}} {
		require.NoError(t, db.Create(&section).Error)
	}
	answer := "该历史记录为B3 29U，内存36%。[原文](/archives/1/documents/2?version=3)"
	got, selected := rt.finalizeUnifiedAnswer(t.Context(), 1, knowledgeAnswerEvidence{Sections: []int64{10, 11, 12}}, knowledgeAnswerEvidence{}, answer)
	require.True(t, strings.HasPrefix(got, answer))
	require.Equal(t, 1, strings.Count(got, "合成设备表.xlsx · V1"))
	require.NotContains(t, got, "· 目录")
	require.Len(t, selected.Sections, 3)
	ordinary, _ := rt.finalizeUnifiedAnswer(t.Context(), 1, knowledgeAnswerEvidence{Sections: []int64{12}}, knowledgeAnswerEvidence{}, "查询未得到实时采样。")
	require.Equal(t, "查询未得到实时采样。", ordinary, "unreferenced reads must not clutter or rewrite the answer")
	require.NoError(t, db.Model(&models.ArchiveDocument{}).Where("id=2").Update("deleted", true).Error)
	got, _ = rt.finalizeUnifiedAnswer(t.Context(), 1, knowledgeAnswerEvidence{Sections: []int64{10}}, knowledgeAnswerEvidence{}, answer)
	require.NotContains(t, got, "内存36%")
	require.Contains(t, got, "失效")
}
func TestUnifiedEvidenceRejectsUnobservedArchiveCitation(t *testing.T) {
	rt, _, _ := optimizationWorkbook(t)
	got, _ := rt.finalizeUnifiedAnswer(t.Context(), 1, knowledgeAnswerEvidence{}, knowledgeAnswerEvidence{}, fmt.Sprintf("[伪造](/archives/1/documents/2?version=%d)", 3))
	require.Contains(t, got, "失效")
	require.Equal(t, "/archives/1/documents/2?version=3", documentCitationKey("http://archives/1/documents/2?version=3"))
}

func TestUnifiedCitationRepairsOnlyObservedVersionAliases(t *testing.T) {
	rt, _, _ := optimizationWorkbook(t)
	require.NoError(t, rt.Library.DB.Create(&models.LibrarySection{ID: 10, VersionID: 4, Heading: "位置", Body: "B3"}).Error)
	answer, _ := rt.finalizeUnifiedAnswer(t.Context(), 1, knowledgeAnswerEvidence{Sections: []int64{10}}, knowledgeAnswerEvidence{}, "登记B3。[原文](/archives/1/documents/2?version=4)")
	require.Contains(t, answer, "[原文](/archives/1/documents/2?version=3)")
	require.NotContains(t, answer, "失效")
	for _, alias := range []int{1, 10} {
		a, _ := rt.finalizeUnifiedAnswer(t.Context(), 1, knowledgeAnswerEvidence{Sections: []int64{10}}, knowledgeAnswerEvidence{}, fmt.Sprintf("[原文](/archives/1/documents/2?version=%d)", alias))
		require.NotContains(t, a, "失效")
		require.Contains(t, a, "?version=3)")
	}
	answer, _ = rt.finalizeUnifiedAnswer(t.Context(), 1, knowledgeAnswerEvidence{Sections: []int64{10}}, knowledgeAnswerEvidence{}, "伪造。[原文](/archives/1/documents/2?version=999)")
	require.Contains(t, answer, "失效")
}

func TestWorkbookFactCitationTakesPriorityOverDiscoveryDirectory(t *testing.T) {
	rt, e, _ := optimizationWorkbook(t)
	rt.Ctx = &nctx.Context{DB: rt.Library.DB, IsCenter: true}
	rt.DatasourceCache = &memsto.DatasourceCacheType{}
	fact := e.Data[0]
	directory := fact
	directory.Operation = "workbook_sheets"
	directory.SourceLabel = "未使用的工作簿目录"
	directory.SourceURL = "/archives/1/documents/2?version=3&sheet=&range="
	e.Data = []aiagent.KnowledgeDataEvidence{directory, fact}
	text := "服务器共2条。[原文](/archives/1/documents/2?version=3&sheet=清单&range=A2:A4)"
	got, selected := rt.finalizeUnifiedAnswer(t.Context(), 1, e, knowledgeAnswerEvidence{}, text)
	require.True(t, strings.HasPrefix(got, text))
	require.NotContains(t, got, "未使用的工作簿目录")
	require.Contains(t, got, "A2:A4")
	require.Len(t, selected.Data, 1)
}

func TestPlainObservedDocumentFilenameIsRecognizedAsUsedSource(t *testing.T) {
	title := "[东院] 医院东院2026年度2月巡检报告（安全）.docx"
	require.True(t, citedDocument("| "+title+" · V1 | 主机房B03-18U |", "/archives/1/documents/2?version=3", title))
	require.True(t, citedDocument("| "+safeCitationLabel(title)+" | 18U |", "/archives/1/documents/2?version=3", title))
	require.False(t, citedDocument("另一本资料的文件名.docx", "/archives/1/documents/2?version=3", title))
	require.False(t, citedDocument("普通说明文字", "/archives/1/documents/2?version=3", "说明"), "generic prose is not a filename citation")
}

func TestUnifiedProcedureSourcesAreCapturedAndRevalidated(t *testing.T) {
	for _, action := range []string{"withdraw", "expire", "disable", "revoke_group"} {
		t.Run(action, func(t *testing.T) {
			rt, _, _ := optimizationWorkbook(t)
			db := rt.Library.DB
			require.NoError(t, db.AutoMigrate(&models.KnowledgeVersion{}, &models.BusiGroup{}, &models.BusiGroupMember{}, &models.UserGroupMember{}))
			require.NoError(t, db.Create(&models.BusiGroup{Id: 1, Name: "procedure group"}).Error)
			rt.Ctx = &nctx.Context{DB: db, IsCenter: true}
			require.NoError(t, db.Model(&models.User{}).Where("id=1").Update("roles", "Standard").Error)
			require.NoError(t, db.Create(&models.BusiGroupMember{BusiGroupId: 1, UserGroupId: 3, PermFlag: "ro"}).Error)
			require.NoError(t, db.Create(&models.UserGroupMember{GroupId: 3, UserId: 1}).Error)
			payload := fmt.Sprintf(`{"summary":"受限规程","body":"受限原文","review_at":%d}`, time.Now().Add(time.Hour).Unix())
			v := models.KnowledgeVersion{ID: 7, ItemID: 1, GroupID: 1, Sequence: 1, Title: "受限规程", Payload: models.OpsJSON(payload)}
			require.NoError(t, db.Create(&v).Error)
			var e knowledgeAnswerEvidence
			e.observe("search_knowledge", fmt.Sprintf(`{"documents":[],"read_documents":[],"procedures":[{"version":{"id":7},"spec":%s}]}`, payload))
			require.Equal(t, []int64{7}, e.Procedures)
			text := "受限原文。[规程](/ops/knowledge?version=7)"
			answer, selected := rt.finalizeUnifiedAnswer(t.Context(), 1, e, knowledgeAnswerEvidence{}, text)
			require.Contains(t, answer, "受限原文")
			require.Equal(t, []int64{7}, selected.Procedures)
			unknown, _ := rt.finalizeUnifiedAnswer(t.Context(), 1, e, knowledgeAnswerEvidence{}, "受限原文。[伪造](/ops/knowledge?version=8)")
			require.NotContains(t, unknown, "受限原文")
			require.Contains(t, unknown, "失效")
			unknown, _ = rt.finalizeUnifiedAnswer(t.Context(), 1, knowledgeAnswerEvidence{}, knowledgeAnswerEvidence{}, "受限原文。[伪造](/ops/knowledge?version=7)")
			require.NotContains(t, unknown, "受限原文")
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			canceled, _ := rt.finalizeUnifiedAnswer(ctx, 1, e, knowledgeAnswerEvidence{}, text)
			require.NotContains(t, canceled, "受限原文")
			switch action {
			case "withdraw":
				require.NoError(t, db.Model(&v).Update("withdrawn_at", time.Now().Unix()).Error)
			case "expire":
				v.Payload = `{"body":"受限原文","review_at":1}`
				require.NoError(t, db.Model(&v).Update("payload", v.Payload).Error)
			case "revoke_group":
				require.NoError(t, db.Where("user_id=1").Delete(&models.UserGroupMember{}).Error)
			case "disable":
				require.NoError(t, db.Model(&models.User{}).Where("id=1").Update("disabled", true).Error)
			}
			for _, evidence := range []struct{ now, prior knowledgeAnswerEvidence }{{e, knowledgeAnswerEvidence{}}, {knowledgeAnswerEvidence{}, e}} {
				answer, _ = rt.finalizeUnifiedAnswer(t.Context(), 1, evidence.now, evidence.prior, text)
				require.NotContains(t, answer, "受限原文")
				require.Contains(t, answer, "失效")
			}
		})
	}
}
