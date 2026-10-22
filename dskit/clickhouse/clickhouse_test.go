package clickhouse

import (
	"context"
	"os"
	"testing"

	"github.com/ccfos/nightingale/v6/dskit/types"
	"github.com/ccfos/nightingale/v6/internal/testenv"
	"github.com/stretchr/testify/require"
)

func Test_Timeseries(t *testing.T) {
	address := testenv.Require(t, "N9E_TEST_CLICKHOUSE_ADDR")
	user := os.Getenv("N9E_TEST_CLICKHOUSE_USER")
	if user == "" {
		user = "default"
	}
	ck := &Clickhouse{
		Nodes: []string{address}, Protocol: "http", User: user,
		Password: os.Getenv("N9E_TEST_CLICKHOUSE_PASSWORD"),
	}
	require.NoError(t, ck.InitCli())
	t.Cleanup(func() { require.NoError(t, ck.ClientByHTTP.Close()) })
	// Exercise the real driver and formatter without a pre-existing table.
	data, err := ck.QueryTimeseries(context.Background(), &QueryParam{
		Sql:  "SELECT toFloat64(42) AS value, toInt64(1700000000) AS ts, 'fixture' AS label",
		Keys: types.Keys{ValueKey: "value", TimeKey: "ts", LabelKey: "label"},
	})
	require.NoError(t, err)
	require.Len(t, data, 1)
	require.Equal(t, "fixture", string(data[0].Metric["label"]))
	require.Equal(t, [][]float64{{1700000000, 42}}, data[0].Values)
}
