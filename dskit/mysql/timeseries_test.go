package mysql

import (
	"context"
	"testing"

	"github.com/ccfos/nightingale/v6/dskit/sqlbase"
	"github.com/ccfos/nightingale/v6/dskit/types"
	"github.com/stretchr/testify/require"
)

func TestQuery(t *testing.T) {
	m, database, table := mysqlFixture(t)
	rows, err := m.Query(context.Background(), &sqlbase.QueryParam{
		Sql: "SELECT * FROM `" + database + "`.`" + table + "` WHERE id = 1",
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestQueryTimeseries(t *testing.T) {
	m, database, table := mysqlFixture(t)
	metrics, err := m.QueryTimeseries(context.Background(), &sqlbase.QueryParam{
		Sql:  "SELECT value, ts FROM `" + database + "`.`" + table + "`",
		Keys: types.Keys{ValueKey: "value", TimeKey: "ts"},
	})
	require.NoError(t, err)
	require.Len(t, metrics, 1)
	require.Equal(t, [][]float64{{1700000000, 42}}, metrics[0].Values)
}
