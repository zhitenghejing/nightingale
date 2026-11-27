package workbook

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRangeReadReportsOnlyIntersectingMergesWithoutFillingBlanks(t *testing.T) {
	b := Book{Sheets: []Sheet{{Name: "资产", Rows: 9, Columns: 4, Merges: []string{"A2:A3", "C8:C9"}, Cells: []Cell{{Address: "A2", Row: 2, Column: 1, Value: "服务器", Type: "s"}}}}}
	r, err := b.Query(Query{Operation: "read", Sheet: "资产", Range: "A3:B3"})
	require.NoError(t, err)
	require.Equal(t, []MergedRange{{Range: "A2:A3", Anchor: "A2", AnchorValue: "服务器"}}, r.MergedRanges)
	require.Empty(t, r.Rows, "merged continuation remains absent rather than becoming a new observation")
	c, err := b.Query(Query{Operation: "count", Sheet: "资产", Range: "A2:A3", Filter: &Filter{Column: "A", Operator: "eq", Value: "服务器"}})
	require.NoError(t, err)
	require.Equal(t, 1, c.MatchedRows)
}

func TestSumExplicitRangeMarksEntirelyAbsentCellsAsIncomplete(t *testing.T) {
	b := Book{Sheets: []Sheet{{Name: "数量", Rows: 3, Columns: 1, Cells: []Cell{{Address: "A2", Row: 2, Column: 1, Raw: "3", Value: "3", Type: "n"}}}}}
	r, err := b.Query(Query{Operation: "sum", Sheet: "数量", Range: "A2:A3", Column: "A"})
	require.NoError(t, err)
	require.Equal(t, "3.000000000000", r.Sum)
	require.False(t, r.Complete)
	require.NotEmpty(t, r.Issues)
	require.Equal(t, 2, r.ScannedRows)
}
