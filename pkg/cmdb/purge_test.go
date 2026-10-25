package cmdb

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func assertRecyclePurge(t *testing.T, s *Store) {
	t.Helper()
	admin := User{ID: 1, Admin: true}
	call := func(user User, path string, data interface{}, status int, reason string) {
		w := catalogCall(t, s, user, "POST", path, data)
		require.Equal(t, status, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), reason)
	}
	room := Room{ID: "purge-room", Name: "purge room"}
	rack := Rack{ID: "purge-rack", RoomID: room.ID, Name: "purge rack", Units: 42, Version: 1}
	require.NoError(t, s.DB.Create(&room).Error)
	require.NoError(t, s.DB.Create(&rack).Error)
	create := func(serial, rackID string) Document {
		d := asset(serial, 0, 0)
		d["rackId"] = rackID
		return save(t, s, d, true)
	}
	a, b, c := create("purge-a", rack.ID), create("purge-b", rack.ID), create("purge-c", rack.ID)
	active := create("purge-active", "")
	call(admin, "purge", []Document{active}, 400, "只能彻底删除回收站")
	call(admin, "purge", nil, 400, "请选择")
	call(admin, "purge", []Document{{"id": "", "version": 1}}, 400, "无效")
	call(admin, "purge", []Document{{"id": "missing", "version": 1}}, 400, "不存在")
	call(admin, "purge", []Document{{"id": str(a, "id"), "version": 1.5}}, 400, "格式错误")
	call(admin, "delete", []Document{a, b, c}, 200, `"err":""`)
	recycled := func(d Document) Document {
		return Document{"id": str(d, "id"), "version": num(d, "version") + 1}
	}
	ra, rb, rc := recycled(a), recycled(b), recycled(c)
	intact := func(d Document) {
		var n int64
		require.NoError(t, s.DB.Model(&Asset{}).Where("id = ?", str(d, "id")).Count(&n).Error)
		require.Equal(t, int64(1), n)
		require.NoError(t, s.DB.Model(&Disk{}).Where("asset_id = ?", str(d, "id")).Count(&n).Error)
		require.Equal(t, int64(1), n)
	}
	// Every invalid member rolls the entire clear operation back, including disks/audit.
	for _, targets := range [][]Document{{ra, b}, {ra, active}, {ra, ra}, {ra, {"id": "missing", "version": 1}}} {
		call(admin, "purge", targets, 400, "")
		intact(a)
		intact(b)
		var n int64
		require.NoError(t, s.DB.Model(&Change{}).Where("action = ?", "purge").Count(&n).Error)
		require.Zero(t, n)
	}
	require.NoError(t, s.DB.Save(&Access{UserID: 904, Read: true, Edit: true}).Error)
	call(User{ID: 904}, "purge", []Document{ra}, 403, "权限")
	require.NoError(t, s.DB.Model(&Access{}).Where("user_id = ?", 904).Updates(map[string]interface{}{"edit": false, "delete": true}).Error)
	call(User{ID: 904}, "purge", []Document{ra}, 200, `"dat":1`)
	var n int64
	require.NoError(t, s.DB.Model(&Asset{}).Where("id = ?", str(a, "id")).Count(&n).Error)
	require.Zero(t, n)
	require.NoError(t, s.DB.Model(&Disk{}).Where("asset_id = ?", str(a, "id")).Count(&n).Error)
	require.Zero(t, n)
	intact(b)
	intact(active)
	var auditRows []Change
	require.NoError(t, s.DB.Where("asset_id = ?", str(a, "id")).Order("id").Find(&auditRows).Error)
	require.Len(t, auditRows, 3)
	require.Equal(t, "purge", auditRows[2].Action)
	require.Equal(t, int64(904), auditRows[2].UserID)
	require.Contains(t, string(auditRows[2].Before), "purge-a")
	// Permanent removal frees the normalized serial, while soft deletion does not.
	create("PURGE-A", "")
	call(admin, "purge", []Document{ra}, 400, "不存在")
	// A clear confirmation carries a snapshot. A later deletion is never swept in.
	late := create("purge-late", rack.ID)
	call(admin, "delete", []Document{late}, 200, `"err":""`)
	call(admin, "purge", []Document{rb, rc}, 200, `"dat":2`)
	intact(late)
	w := catalogCall(t, s, admin, "DELETE", "racks/"+rack.ID, rack)
	require.Equal(t, 400, w.Code)
	call(admin, "purge", []Document{recycled(late)}, 200, `"dat":1`)
	w = catalogCall(t, s, admin, "DELETE", "racks/"+rack.ID, rack)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = catalogCall(t, s, admin, "DELETE", "rooms/"+room.ID, nil)
	require.Equal(t, 200, w.Code, w.Body.String())
	call(admin, "restore", []Document{ra}, 400, "不存在")
}

func TestRecyclePurge(t *testing.T) { assertRecyclePurge(t, fixture(t)) }

func TestRecyclePurgeAuditFailureRollsBack(t *testing.T) {
	s := fixture(t)
	saved := save(t, s, asset("purge-rollback", 1, 1), true)
	w := catalogCall(t, s, User{ID: 1, Admin: true}, "POST", "delete", []Document{saved})
	require.Equal(t, 200, w.Code)
	rows, err := s.Assets(true)
	require.NoError(t, err)
	require.NoError(t, s.DB.Migrator().DropTable(&Change{}))
	w = catalogCall(t, s, User{ID: 1, Admin: true}, "POST", "purge", rows)
	require.Equal(t, 400, w.Code)
	var stored Asset
	require.NoError(t, s.DB.First(&stored, "id = ?", str(saved, "id")).Error)
	require.True(t, stored.Deleted)
	var n int64
	require.NoError(t, s.DB.Model(&Disk{}).Where("asset_id = ?", stored.ID).Count(&n).Error)
	require.Equal(t, int64(1), n)
	require.NoError(t, s.Write(func(tx *gorm.DB) error { return nil }))
}

func assertConcurrentPurgeRestore(t *testing.T, s *Store) {
	t.Helper()
	doc := asset("purge-race", 0, 0)
	doc["rackId"] = ""
	saved := save(t, s, doc, true)
	admin := User{ID: 1, Admin: true}
	require.Equal(t, 200, catalogCall(t, s, admin, "POST", "delete", []Document{saved}).Code)
	var target Document
	rows, err := s.Assets(true)
	require.NoError(t, err)
	for _, d := range rows {
		if str(d, "id") == str(saved, "id") {
			target = d
		}
	}
	require.NotNil(t, target)
	start := make(chan struct{})
	codes := make(chan int, 2)
	for _, path := range []string{"purge", "restore"} {
		go func(path string) {
			<-start
			codes <- catalogCall(t, s, admin, "POST", path, []Document{target}).Code
		}(path)
	}
	close(start)
	first, second := <-codes, <-codes
	require.ElementsMatch(t, []int{200, 400}, []int{first, second})
	var remaining []Asset
	require.NoError(t, s.DB.Where("id = ?", str(saved, "id")).Find(&remaining).Error)
	if len(remaining) == 1 {
		require.False(t, remaining[0].Deleted)
	}
	var changes []Change
	require.NoError(t, s.DB.Where("asset_id = ? AND action IN ?", str(saved, "id"), []string{"restore", "purge"}).Find(&changes).Error)
	require.Len(t, changes, 1)
	var decoded Document
	require.NoError(t, json.Unmarshal([]byte(changes[0].Before), &decoded))
}
