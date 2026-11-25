package cmdb

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func namedImportDevice(serial, room, rack string, start, end int) Document {
	d := asset(serial, start, end)
	delete(d, "rackId")
	d["room"], d["rack"] = room, rack
	return d
}

func assertImportCatalog(t *testing.T, s *Store) {
	t.Helper()
	user := User{ID: 904}
	require.NoError(t, s.DB.Create(&Access{UserID: user.ID, Import: true}).Error)
	docs := []Document{
		namedImportDevice("import-low", " 新机房 ", " A01 ", 1, 2),
		namedImportDevice("import-high", "新机房", "A01", 49, 50),
		namedImportDevice("import-unplaced", "新机房", "A02", 0, 0),
		namedImportDevice("import-other-room", "机房", "A01", 1, 2),
	}
	r := catalogCall(t, s, user, "POST", "import", docs)
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	var room Room
	var racks []Rack
	require.NoError(t, s.DB.Where("name = ?", "新机房").Take(&room).Error)
	require.NoError(t, s.DB.Where("room_id = ?", room.ID).Order("name").Find(&racks).Error)
	require.Len(t, racks, 2)
	require.Equal(t, "A01", racks[0].Name)
	require.Equal(t, 50, racks[0].Units)
	require.Equal(t, "A02", racks[1].Name)
	require.Equal(t, 42, racks[1].Units)
	var assets []Asset
	require.NoError(t, s.DB.Where("rack_id IN ?", []string{racks[0].ID, racks[1].ID}).Order("start").Find(&assets).Error)
	require.Len(t, assets, 3)
	require.Equal(t, [3]int{0, 1, 49}, [3]int{assets[0].Start, assets[1].Start, assets[2].Start})
	require.Equal(t, [3]int{0, 2, 50}, [3]int{assets[0].End, assets[1].End, assets[2].End})
	require.Equal(t, racks[1].ID, assets[0].RackID)
	require.Equal(t, racks[0].ID, assets[1].RackID)
	require.Equal(t, racks[0].ID, assets[2].RackID)
	var changeCount int64
	require.NoError(t, s.DB.Model(&Change{}).Where("user_id = ? AND action IN ?", user.ID, []string{"room.create", "rack.save"}).Count(&changeCount).Error)
	require.EqualValues(t, 3, changeCount)

	// A repeated import gets the same validation without creating additional catalog entries.
	r = catalogCall(t, s, user, "POST", "import", docs)
	require.Equal(t, http.StatusBadRequest, r.Code)
	require.Contains(t, r.Body.String(), "序列号已存在")
	var existing Rack
	require.NoError(t, s.DB.Where("name = ? AND room_id <> ?", "A01", room.ID).Take(&existing).Error)
	r = catalogCall(t, s, user, "POST", "import", []Document{namedImportDevice("over-capacity", "机房", "A01", existing.Units, existing.Units+1)})
	require.Equal(t, http.StatusBadRequest, r.Code)
	require.Contains(t, r.Body.String(), "超出机柜容量")
	var after Rack
	require.NoError(t, s.DB.Take(&after, "id = ?", existing.ID).Error)
	require.Equal(t, existing, after)
}

func TestImportCreatesRackCatalog(t *testing.T) {
	assertImportCatalog(t, fixture(t))
}

func TestImportRollsBackNewCatalogAndAuditOnOverlap(t *testing.T) {
	s := fixture(t)
	user := User{ID: 1, Admin: true}
	docs := []Document{
		namedImportDevice("valid", "尚未建档", "新柜", 5, 6),
		namedImportDevice("overlap", "尚未建档", "新柜", 6, 7),
	}
	r := catalogCall(t, s, user, "POST", "import", docs)
	require.Equal(t, http.StatusBadRequest, r.Code)
	require.Contains(t, r.Body.String(), "第 3 行：机柜 U 位已被其他设备占用")
	for _, table := range []struct {
		model interface{}
		want  int64
	}{{&Room{}, 1}, {&Rack{}, 2}, {&Asset{}, 0}, {&Disk{}, 0}, {&Change{}, 0}} {
		var count int64
		require.NoError(t, s.DB.Model(table.model).Count(&count).Error)
		require.Equal(t, table.want, count)
	}
	// The same document through manual creation still requires an existing cabinet.
	r = catalogCall(t, s, user, "POST", "devices", docs[0])
	require.Equal(t, http.StatusBadRequest, r.Code)
	require.Contains(t, r.Body.String(), "机柜不存在")
}

func TestImportFillsExistingRackPositionsOnlyWithBothPermissions(t *testing.T) {
	s := fixture(t)
	user := User{ID: 905}
	require.NoError(t, s.DB.Create(&Access{UserID: user.ID, Import: true}).Error)
	placed := save(t, s, asset("already-placed", 1, 2), true)
	unplaced := namedImportDevice("unplaced", "机房", "", 0, 0)
	unplaced["note"] = "保留旧备注"
	stored := save(t, s, unplaced, true)
	docs := []Document{
		namedImportDevice("UNPLACED", "新机房", "新柜", 5, 6),
		namedImportDevice("already-placed", "新机房", "新柜", 8, 8),
	}
	docs[0]["note"] = "CSV 备注不覆盖档案"
	r := catalogCall(t, s, user, "POST", "import?mode=placement", docs)
	require.Equal(t, http.StatusForbidden, r.Code)
	require.NoError(t, s.DB.Model(&Access{}).Where("user_id = ?", user.ID).Update("edit", true).Error)
	r = catalogCall(t, s, user, "POST", "import?mode=placement", append(docs, namedImportDevice("unknown", "新机房", "新柜", 9, 9)))
	require.Equal(t, http.StatusBadRequest, r.Code)
	require.Contains(t, r.Body.String(), "序列号对应的在用设备不存在")
	var rackCount int64
	require.NoError(t, s.DB.Model(&Rack{}).Count(&rackCount).Error)
	require.EqualValues(t, 2, rackCount)
	r = catalogCall(t, s, user, "POST", "import?mode=placement", docs)
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	var updated, unchanged Asset
	require.NoError(t, s.DB.Take(&updated, "id = ?", str(stored, "id")).Error)
	require.NoError(t, s.DB.Take(&unchanged, "id = ?", str(placed, "id")).Error)
	require.Equal(t, "保留旧备注", str(decode(updated), "note"))
	require.Equal(t, "unplaced", str(decode(updated), "hostname"))
	require.Equal(t, "新柜", str(decode(updated), "rack"))
	require.Equal(t, [2]int{5, 6}, [2]int{updated.Start, updated.End})
	require.Equal(t, "rack", unchanged.RackID)
	require.Equal(t, 1, unchanged.Version)
	r = catalogCall(t, s, user, "POST", "import?mode=placement", docs)
	require.Equal(t, http.StatusOK, r.Code)
	require.Contains(t, r.Body.String(), `"dat":[]`)
}
