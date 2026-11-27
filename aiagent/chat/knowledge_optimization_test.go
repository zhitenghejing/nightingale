package chat

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestWorkbookMonitoringColumnIsNotLiveInvestigation(t *testing.T) {
	q := "请对《合成资产表.xlsx》Sheet1的AM2:AM19统计“是否监控”明确填“是”的记录条数。"
	require.False(t, knowledgeLiveQuestion(q))
	names := selectKnowledgeTools(&AIChatRequest{UserInput: q})
	require.Contains(t, names, "query_knowledge_data")
	require.Contains(t, names, "sum_workbook_column")
	require.Contains(t, names, "read_workbook_range")
	require.True(t, knowledgeLiveQuestion("资产表标是，能确认现在已经接入监控吗？"))
	require.True(t, knowledgeLiveQuestion("统计《合成表.xlsx》Sheet1的A2:A9，目前对应设备是否在线？"))
	require.Contains(t, selectKnowledgeTools(&AIChatRequest{UserInput: "统计昨天订单总量"}), "query_knowledge_data")
}

func TestCurrentDeviceInformationUsesPlatformDatabaseEvenWithWorkbookContext(t *testing.T) {
	for _, q := range []string{"当前设备库有多少台设备，按操作系统统计", "看过《设备台账.xlsx》后，请查询平台当前设备信息分布", "设备数量按已接入情况查库统计"} {
		require.True(t, knowledgeLiveQuestion(q))
		require.Contains(t, selectKnowledgeTools(&AIChatRequest{UserInput: q}), "query_knowledge_data")
	}
}

func TestKnowledgeDocumentTitlePinsOneExplicitSourceOnly(t *testing.T) {
	require.Equal(t, "维护说明.xlsx", KnowledgeDocumentTitle("只按《维护说明.xlsx》中的记录回答"))
	require.Equal(t, "维护说明.xlsx", KnowledgeDocumentTitle("《维护说明.xlsx》和刚才的《维护说明.xlsx》"))
	require.Empty(t, KnowledgeDocumentTitle("比较《维护说明.xlsx》与《巡检报告.docx》"))
	require.Empty(t, KnowledgeDocumentTitle("查询平台当前设备总数"))
	require.True(t, KnowledgeDocumentSearch("请查找《知识维护说明》，读取其保留期"))
	require.Contains(t, selectKnowledgeTools(&AIChatRequest{UserInput: "请查找《知识维护说明》，读取其保留期"}), "query_knowledge_data")
}

func TestUnifiedQuestionKeepsBothEvidenceFamiliesAvailable(t *testing.T) {
	for _, q := range []string{
		"平台当前设备库中已接入设备多少台？不是Excel台账或列表第一页。",
		"平台已接入的设备按操作系统分布怎样？",
		"查平台当前设备库，按agent_version统计。",
		"同IP的设备，查平台库按设备标识统计总数，能证明健康吗？",
	} {
		names := selectKnowledgeTools(&AIChatRequest{UserInput: q})
		require.Contains(t, names, "query_knowledge_data", q)
		require.Contains(t, names, "search_knowledge", q)
		require.Contains(t, names, "read_workbook_range", q)
	}
	for _, q := range []string{
		"比较平台当前设备库与Excel的设备数量",
		"平台当前设备库和《历史台账.xlsx》的差异",
		"查平台库并结合报告说明差异",
	} {
		names := selectKnowledgeTools(&AIChatRequest{UserInput: q})
		require.Contains(t, names, "query_knowledge_data", q)
		require.Contains(t, names, "search_knowledge", q)
	}
}

func TestOpaqueFollowupCanPlanEitherEvidenceOrder(t *testing.T) {
	for _, q := range []string{"然后呢？", "这些里面哪些已经接入？", "那个数按刚才的定义重新算一下", "先查平台，再找对应说明", "按报告查平台，再补读资料解释差异"} {
		names := selectKnowledgeTools(&AIChatRequest{UserInput: q})
		for _, name := range []string{"search_knowledge", "read_knowledge", "read_workbook_range", "resolve_knowledge_object", "query_knowledge_data"} {
			require.Contains(t, names, name, q)
		}
		require.NotContains(t, names, "dispatch_task_stateless")
		require.NotContains(t, names, "create_alert_rule")
	}
}

func TestMainAgentExposesComposableKnowledgeAndOperations(t *testing.T) {
	h, ok := Lookup("general_chat")
	require.True(t, ok)
	for _, q := range []string{"5066001979是什么设备", "查设备并结合巡检解释差异", "加载知识Skill后查库", "创建一个仪表盘"} {
		names := h.SelectTools(&AIChatRequest{UserInput: q})
		for _, name := range []string{"query_knowledge_data", "target_statistics", "search_knowledge", "read_workbook_range", "query_prometheus", "create_dashboard", "dispatch_task_stateless"} {
			require.Contains(t, names, name)
		}
		require.Empty(t, h.RequiredSkills(&AIChatRequest{}), "no forced knowledge-only skill profile; dynamic catalog stays available")
	}
}
