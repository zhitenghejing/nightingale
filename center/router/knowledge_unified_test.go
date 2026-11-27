package router

import (
	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUnifiedQuestionDefaultAndOperationalCompatibility(t *testing.T) {
	for _, text := range []string{"然后呢？", "当前设备数量", "《清单.xlsx》的设备现在接入了多少台？", "先查平台再按资料解释分布", "请查找包含独立编码的资料"} {
		mode, err := assistantQueryMode(models.AssistantMessageQuery{Content: text})
		require.NoError(t, err)
		require.Equal(t, "auto", mode, text)
	}
	for _, mode := range []string{"ops", "knowledge_qa"} {
		got, err := assistantQueryMode(models.AssistantMessageQuery{Mode: mode, Content: "当前设备数量"})
		require.NoError(t, err)
		require.Equal(t, mode, got)
	}
	for _, text := range []string{"创建一个仪表盘", "导入告警规则模板"} {
		mode, err := assistantQueryMode(models.AssistantMessageQuery{Content: text})
		require.NoError(t, err)
		require.Equal(t, "auto", mode)
	}
	mode, err := assistantQueryMode(models.AssistantMessageQuery{Content: "查询当前数据", Action: models.AssistantAction{Param: map[string]interface{}{"datasource_id": float64(1)}}})
	require.NoError(t, err)
	require.Equal(t, "auto", mode)
	_, err = assistantQueryMode(models.AssistantMessageQuery{Mode: "unknown"})
	require.Error(t, err)
}

func TestJointWorkbookAndPlatformEvidenceNeedsReviewAndPreservesBothSources(t *testing.T) {
	e := knowledgeAnswerEvidence{Data: []aiagent.KnowledgeDataEvidence{{Operation: "workbook_read", WorkbookVersion: 4}, {Operation: "target_statistics"}}}
	require.True(t, e.combinedSources())
	require.True(t, knowledgeNeedsAudit("按两边的口径对照", e))
	require.Len(t, e.Data, 2)
	prior := reusableKnowledgeEvidence(e)
	require.Len(t, prior.Data, 1)
	require.Equal(t, "workbook_read", prior.Data[0].Operation)
	require.Len(t, e.Data, 2, "filtering history must not mutate current observations")
	e = knowledgeAnswerEvidence{Sections: []int64{3}, Data: []aiagent.KnowledgeDataEvidence{{Operation: "target_statistics"}}}
	require.True(t, e.combinedSources())
	require.True(t, knowledgeNeedsAudit("两边对照", e))
	require.False(t, (knowledgeAnswerEvidence{Data: []aiagent.KnowledgeDataEvidence{{Operation: "target_statistics"}}}).combinedSources())
}

func TestDocumentAndDatabaseReceiptsSurviveJointAnswerSelection(t *testing.T) {
	e := knowledgeAnswerEvidence{Data: []aiagent.KnowledgeDataEvidence{{Operation: "workbook_read", WorkbookVersion: 4}, {Operation: "target_statistics"}}}
	got := knowledgeQuestionEvidence("《清单.xlsx》的设备情况", e)
	require.Len(t, got.Data, 2, "a workbook receipt must not erase the platform query")
	missing := knowledgeQuestionEvidence("请查找只含独立编码的资料", knowledgeAnswerEvidence{Searched: true, Empty: true, Data: []aiagent.KnowledgeDataEvidence{{Operation: "target_statistics"}}})
	require.Empty(t, missing.Data, "a device catalog is not a document match")
}

func TestDeviceIdentityDoesNotHideSupplementaryKnowledge(t *testing.T) {
	scope := knowledgeQuestionScope("查询当前CN791304C2设备，再按维护手册解释字段含义")
	require.Empty(t, scope.Identifiers, "generic definitions need not repeat an individual device serial")
	scope = knowledgeQuestionScope("查找记载CN791304C2的资料")
	require.Equal(t, []string{"cn791304c2"}, scope.Identifiers)
}
