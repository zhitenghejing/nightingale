package modeloutput

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReasoningNeverBecomesVisibleAnswer(t *testing.T) {
	for _, s := range []string{"<think>private", " <THINK>private</THINK> Answer", "Answer<think>private</think>", "<think>outer<think>private</think>remaining-private</think>Answer"} {
		require.NotContains(t, Visible(s), "private")
	}
	require.Equal(t, "İAnswer", Visible("İ<think>private</think>Answer"))
	require.Empty(t, Visible("<think>unfinished"))
	require.Equal(t, "Answer", Visible("<think>private</think> Answer"))
	var out map[string]interface{}
	require.NoError(t, JSON("<think>private {bad}</think>\n```json\n{\"note\":\"literal <think> example\"}\n```", &out))
	require.Equal(t, "literal <think> example", out["note"])
	for _, s := range []string{"<think>{\"bad\":true}", "{\"x\":1} {\"y\":2}", "[1]", "null"} {
		require.Error(t, JSON(s, &out))
	}
}
