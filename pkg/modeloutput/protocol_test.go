package modeloutput

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAnswerRemovesOrphanBoundaryTagsButPreservesQuotedExamples(t *testing.T) {
	require.Equal(t, "正文", Answer("\u200b</\u200btool_call>正文"))
	require.Equal(t, "## 答复", Answer("</function>\n</tool_call>\n## 答复"))
	require.Equal(t, "示例 `</function>` 是协议标签。", Answer("示例 `</function>` 是协议标签。"))
}

func TestAnswerRemovesOnlyLeadingParameterTailWithInvocationClosures(t *testing.T) {
	raw := "<parameter=query>\nagent_version 版本标签 说明\n</parameter>\n<parameter=document_title>\n统一问答联合验收说明\n</parameter>\n</function>\n</tool_call>\n## 统计\n100 / 23"
	require.Equal(t, "## 统计\n100 / 23", Answer(raw))
	for _, quoted := range []string{
		"```xml\n" + raw + "\n```",
		"工具协议示例：\n" + raw,
		"<parameter=query>示例</parameter>\n这里没有调用结束边界",
	} {
		require.Equal(t, quoted, Answer(quoted))
	}
}
