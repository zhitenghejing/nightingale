//go:build unix

package cmdb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReportDirectoryHasOneWriter(t *testing.T) {
	store, err := NewReportStore(t.TempDir())
	require.NoError(t, err)
	unlock, err := store.Lock()
	require.NoError(t, err)
	_, err = store.Lock()
	require.Error(t, err)
	unlock()
	unlock, err = store.Lock()
	require.NoError(t, err)
	unlock()
}
