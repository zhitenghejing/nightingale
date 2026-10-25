package cmdb

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func catalogCall(t *testing.T, s *Store, user User, method, path string, data interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(data)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	(&API{Store: s}).ServeUser(w, httptest.NewRequest(method, "/api/cmdb/"+path, bytes.NewReader(b)), user)
	return w
}

func assertCatalogDeletion(t *testing.T, s *Store) {
	t.Helper()
	admin := User{ID: 1, Admin: true}
	create := func(path string, data interface{}, result interface{}) {
		w := catalogCall(t, s, admin, "POST", path, data)
		require.Equal(t, 200, w.Code, w.Body.String())
		var envelope struct{ Dat json.RawMessage }
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		require.NoError(t, json.Unmarshal(envelope.Dat, result))
	}
	var room Room
	create("rooms", Room{Name: "deletion room"}, &room)
	var rack Rack
	create("racks", Rack{RoomID: room.ID, Name: "deletion rack", Units: 42}, &rack)
	check := func(method, path string, data interface{}, status int, contains string) {
		w := catalogCall(t, s, admin, method, path, data)
		require.Equal(t, status, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), contains)
	}
	check("DELETE", "rooms/"+room.ID, nil, 400, "还有机柜")
	check("DELETE", "racks/"+rack.ID, Rack{Version: 0}, 400, "已修改")
	// A device without U positions still belongs to the rack.
	doc := asset("delete-guard", 0, 0)
	doc["rackId"] = rack.ID
	saved := save(t, s, doc, true)
	check("DELETE", "racks/"+rack.ID, rack, 400, "还有设备")
	check("POST", "delete", []Document{saved}, 200, `"err":""`)
	check("DELETE", "racks/"+rack.ID, rack, 400, "回收站")
	recycled, err := s.Assets(true)
	require.NoError(t, err)
	var target Document
	for _, d := range recycled {
		if str(d, "id") == str(saved, "id") {
			target = d
		}
	}
	require.NotNil(t, target)
	check("POST", "restore", []Document{target}, 200, `"err":""`)
	active, err := s.Assets(false)
	require.NoError(t, err)
	for _, d := range active {
		if str(d, "id") == str(saved, "id") {
			target = d
		}
	}
	target["rackId"] = ""
	target["rack"] = ""
	target["room"] = ""
	save(t, s, target, false)
	// Edit permission cannot grant destructive catalog operations.
	require.NoError(t, s.DB.Save(&Access{UserID: 902, Read: true, Edit: true}).Error)
	for _, path := range []string{"racks/" + rack.ID, "rooms/" + room.ID} {
		w := catalogCall(t, s, User{ID: 902}, "DELETE", path, rack)
		require.Equal(t, 403, w.Code, w.Body.String())
	}
	require.NoError(t, s.DB.Model(&Access{}).Where("user_id = ?", 902).Updates(map[string]interface{}{"edit": false, "delete": true}).Error)
	w := catalogCall(t, s, User{ID: 902}, "DELETE", "racks/"+rack.ID, rack)
	require.Equal(t, 200, w.Code, w.Body.String())
	check("DELETE", "racks/"+rack.ID, rack, 400, "不存在")
	check("DELETE", "rooms/"+room.ID, nil, 200, `"err":""`)
	check("DELETE", "rooms/"+room.ID, nil, 400, "不存在")
	var changes []Change
	require.NoError(t, s.DB.Where("action IN ? AND asset_id IN ?", []string{"rack.delete", "room.delete"}, []string{room.ID, rack.ID}).Order("id").Find(&changes).Error)
	require.Len(t, changes, 2)
	require.Equal(t, int64(902), changes[0].UserID)
	require.Contains(t, string(changes[0].Before), rack.Name)
	require.Contains(t, string(changes[1].Before), room.Name)
	var n int64
	require.NoError(t, s.DB.Model(&Rack{}).Where("room_id = ?", room.ID).Count(&n).Error)
	require.Zero(t, n)
}

func TestCatalogDeletion(t *testing.T) { assertCatalogDeletion(t, fixture(t)) }

func TestCatalogDeleteAuditFailureRollsBack(t *testing.T) {
	s := fixture(t)
	require.NoError(t, s.DB.Migrator().DropTable(&Change{}))
	w := catalogCall(t, s, User{ID: 1, Admin: true}, "DELETE", "racks/rack", Rack{Version: 1})
	require.Equal(t, 400, w.Code)
	var rack Rack
	require.NoError(t, s.DB.First(&rack, "id = ?", "rack").Error)
	// The transaction rollback also keeps the write lock usable.
	require.NoError(t, s.Write(func(tx *gorm.DB) error { return nil }))
}

func assertRoomEditing(t *testing.T, s *Store) {
	t.Helper()
	admin := User{ID: 1, Admin: true}
	room := Room{ID: "rename-room", Name: "rename before"}
	rack := Rack{ID: "rename-rack", RoomID: room.ID, Name: "rename rack", Units: 42, Version: 1}
	require.NoError(t, s.DB.Create(&room).Error)
	require.NoError(t, s.DB.Create(&Room{ID: "rename-taken", Name: "rename taken"}).Error)
	require.NoError(t, s.DB.Create(&rack).Error)
	doc := asset("rename-asset", 1, 2)
	doc["rackId"] = rack.ID
	saved := save(t, s, doc, true)
	call := func(user User, id, name, previous string, status int, reason string) {
		w := catalogCall(t, s, user, "PUT", "rooms", map[string]interface{}{"id": id, "name": name, "previousName": previous})
		require.Equal(t, status, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), reason)
	}
	call(admin, room.ID, "rename taken", room.Name, 400, "重复")
	call(admin, room.ID, " ", room.Name, 400, "必填")
	call(admin, room.ID, "rename after", "stale name", 400, "已修改")
	call(admin, "missing-room", "rename after", room.Name, 400, "不存在")
	require.NoError(t, s.DB.Save(&Access{UserID: 903, Read: true, Delete: true}).Error)
	call(User{ID: 903}, room.ID, "rename after", room.Name, 403, "权限")
	require.NoError(t, s.DB.Model(&Access{}).Where("user_id = ?", 903).Update("edit", true).Error)
	call(User{ID: 903}, room.ID, "rename after", room.Name, 200, `"err":""`)
	call(admin, room.ID, "rename stale", room.Name, 400, "已修改")
	var actual Room
	require.NoError(t, s.DB.First(&actual, "id = ?", room.ID).Error)
	require.Equal(t, "rename after", actual.Name)
	var bound Rack
	require.NoError(t, s.DB.First(&bound, "id = ?", rack.ID).Error)
	require.Equal(t, room.ID, bound.RoomID)
	rows, err := s.Assets(false)
	require.NoError(t, err)
	for _, d := range rows {
		if str(d, "id") == str(saved, "id") {
			require.Equal(t, "rename after", str(d, "room"))
		}
	}
	w := catalogCall(t, s, admin, "POST", "delete", []Document{saved})
	require.Equal(t, 200, w.Code, w.Body.String())
	rows, err = s.Assets(true)
	require.NoError(t, err)
	for _, d := range rows {
		if str(d, "id") == str(saved, "id") {
			require.Equal(t, "rename after", str(d, "room"))
		}
	}
	var changes []Change
	require.NoError(t, s.DB.Where("asset_id = ? AND action = ?", room.ID, "room.edit").Find(&changes).Error)
	require.Len(t, changes, 1)
	require.Equal(t, int64(903), changes[0].UserID)
	require.Contains(t, string(changes[0].Before), room.Name)
	require.Contains(t, string(changes[0].After), "rename after")
}

func TestRoomEditing(t *testing.T) { assertRoomEditing(t, fixture(t)) }
