package cmdb

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeIdentityAndHealth(t *testing.T) {
	a := &API{Store: fixture(t)}
	for _, user := range []User{{}, {ID: 1, Disabled: 1}, {ID: 1}} {
		w := httptest.NewRecorder()
		a.ServeUser(w, httptest.NewRequest("GET", "/api/cmdb/health", nil), user)
		if user.ID == 0 || user.Disabled != 0 {
			require.Equal(t, 401, w.Code)
		} else {
			require.Equal(t, 200, w.Code)
		}
	}
}
