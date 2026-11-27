package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCMDBRackAlertsAssociateIPAndRespectAccess(t *testing.T) {
	r, rt, _, _ := collectionRouterFixture(t)
	db := rt.Ctx.DB
	store := &cmdb.Store{DB: db}
	require.NoError(t, store.Migrate())
	require.NoError(t, db.AutoMigrate(&models.AlertCurEvent{}))
	rt.CMDB = &cmdb.API{Store: store}
	rt.configCMDB(r)
	for _, id := range []int64{2, 3} {
		require.NoError(t, db.Create(&cmdb.Access{UserID: id, Read: true}).Error)
	}
	add := func(serial string, addresses cmdb.Document) string {
		t.Helper()
		addresses["serial"], addresses["customer"] = serial, "Test"
		var saved cmdb.Document
		require.NoError(t, store.Write(func(tx *gorm.DB) error {
			var err error
			saved, err = store.Save(tx, addresses, 1, true)
			return err
		}))
		return saved["id"].(string)
	}
	switchID := add("switch", cmdb.Document{"businessIp": "192.0.2.10", "managementIp": "192.0.2.10；198.51.100.5"})
	agentID := add("agent", cmdb.Document{"businessIp": "192.0.2.100", "managementIp": "2001:db8::1"})
	retired := add("retired", cmdb.Document{"businessIp": "198.51.100.5"})
	require.NoError(t, db.Model(&cmdb.Asset{}).Where("id = ?", retired).Update("deleted", true).Error)
	require.NoError(t, db.Create(&models.Target{Ident: "agent-name", HostIp: "192.0.2.100"}).Error)
	for _, event := range []models.AlertCurEvent{
		{Id: 10, GroupId: 1, Severity: 1, RuleName: "Switch port", TargetIdent: "agent-name", Tags: "snmp_target=udp://192.0.2.10:161,,instance=192.0.2.100:9100"},
		{Id: 11, GroupId: 1, Severity: 3, RuleName: "Management port", OriginalTags: "instance=198.51.100.5:9100"},
		{Id: 12, GroupId: 1, Severity: 2, RuleName: "Agent load", TargetIdent: "agent-name"},
		{Id: 13, GroupId: 1, Severity: 3, RuleName: "IPv6", Tags: "ip=[2001:0db8:0::1]:9100"},
		{Id: 14, GroupId: 2, Severity: 1, RuleName: "Private event", TargetIdent: "192.0.2.10"},
		{Id: 15, GroupId: 1, Severity: 1, RuleName: "Other IP", Tags: "instance=192.0.2.101:9100,,note=192.0.2.10"},
	} {
		require.NoError(t, db.Create(&event).Error)
	}
	const path = "/api/cmdb/rack-alerts"
	require.Equal(t, http.StatusUnauthorized, collectionCall(r, "GET", path, nil, "").Code)
	require.NoError(t, db.Model(&cmdb.Access{}).Where("user_id = ?", 3).Update("read", false).Error)
	require.Equal(t, http.StatusForbidden, collectionCall(r, "GET", path, nil, "collection-outsider").Code)
	require.NoError(t, db.Model(&cmdb.Access{}).Where("user_id = ?", 3).Update("read", true).Error)

	load := func(username string) map[string][]rackAlert {
		t.Helper()
		w := collectionCall(r, "GET", path, nil, username)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		var response struct {
			Dat map[string][]rackAlert
			Err string
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Empty(t, response.Err)
		return response.Dat
	}
	require.Empty(t, load("collection-outsider"))
	alerts := load("collection-reader")
	require.Len(t, alerts, 2)
	require.Equal(t, []rackAlert{{ID: 10, Severity: 1, RuleName: "Switch port"}, {ID: 11, Severity: 3, RuleName: "Management port"}}, alerts[switchID])
	require.Equal(t, []rackAlert{{ID: 12, Severity: 2, RuleName: "Agent load"}, {ID: 13, Severity: 3, RuleName: "IPv6"}}, alerts[agentID])
	require.Len(t, load("collection-admin")[switchID], 3)
	require.NoError(t, db.Delete(&models.AlertCurEvent{}, 10).Error)
	require.EqualValues(t, 3, load("collection-reader")[switchID][0].Severity)
}
