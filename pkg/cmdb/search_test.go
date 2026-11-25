package cmdb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssetSearchAppliesInventoryFiltersAndPagination(t *testing.T) {
	s := fixture(t)
	require.NoError(t, s.DB.Model(&Room{}).Where("id = ?", "room").Update("name", "北区机房").Error)
	for _, row := range []struct {
		id, expiry, deviceType string
		deleted                bool
	}{
		{"z", "2027-01-01", "机架式服务器", false},
		{"a", "2027-01-01", "机架式服务器", false},
		{"last", "2027-12-31", "机架式服务器", false},
		{"before", "2026-12-31", "机架式服务器", false},
		{"after", "2028-01-01", "机架式服务器", false},
		{"missing", "", "机架式服务器", false},
		{"invalid", "2027-02-30", "机架式服务器", false},
		{"other", "2027-06-01", "交换机", false},
		{"deleted", "2027-06-01", "机架式服务器", true},
	} {
		d := Document{"hostname": row.id, "expire": row.expiry, "type": row.deviceType, "vendor": "Dell", "warrantyType": "官方维保", "room": "旧机房"}
		require.NoError(t, s.DB.Create(&Asset{
			ID: row.id, Serial: row.id, RackID: "rack", Deleted: row.deleted, Payload: largeText(encode(d)),
		}).Error)
	}
	ctx := context.Background()
	user := User{ID: 1, Admin: true}
	q := AssetQuery{
		Keyword: "DELL 北区", Room: "北区", DeviceType: "服务器", WarrantyType: "官方维保",
		ExpireFrom: "2027-01-01", ExpireTo: "2027-12-31", PageSize: 2,
	}
	first, err := s.SearchAssets(ctx, user, q)
	require.NoError(t, err)
	require.Equal(t, 3, first.Total)
	require.Equal(t, 2, first.ExpiryUnknownTotal)
	require.Equal(t, 2, first.NextPage)
	require.Len(t, first.Devices, 2)
	require.Equal(t, "a", first.Devices[0].ID)
	require.Equal(t, "北区机房", first.Devices[0].Room)
	require.Equal(t, "A01", first.Devices[0].Rack)
	require.Equal(t, "z", first.Devices[1].ID)
	q.Page = first.NextPage
	last, err := s.SearchAssets(ctx, user, q)
	require.NoError(t, err)
	require.Equal(t, first.Total, last.Total)
	require.Zero(t, last.NextPage)
	require.Len(t, last.Devices, 1)
	require.Equal(t, "2027-12-31", last.Devices[0].Expire)

	q.Page = int(^uint(0) >> 1)
	pastEnd, err := s.SearchAssets(ctx, user, q)
	require.NoError(t, err)
	require.Equal(t, 3, pastEnd.Total)
	require.NotNil(t, pastEnd.Devices)
	require.Empty(t, pastEnd.Devices)

	q.Page = 1
	q.ExpireFrom, q.ExpireTo = "", "2026-12-31"
	oneSided, err := s.SearchAssets(ctx, user, q)
	require.NoError(t, err)
	require.Len(t, oneSided.Devices, 1)
	require.Equal(t, "before", oneSided.Devices[0].ID)

	all, err := s.SearchAssets(ctx, user, AssetQuery{Keyword: "dell"})
	require.NoError(t, err)
	require.Equal(t, 8, all.Total)
	require.Empty(t, all.Devices[len(all.Devices)-1].Expire)
}

func TestAssetSearchReusesCMDBReadPermissionAndRejectsInvalidQuery(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	user := User{ID: 2}
	_, err := s.SearchAssets(ctx, user, AssetQuery{})
	require.ErrorContains(t, err, "没有 CMDB 读取权限")
	require.NoError(t, s.DB.Create(&Access{UserID: 2, Export: true}).Error)
	_, err = s.SearchAssets(ctx, user, AssetQuery{})
	require.ErrorContains(t, err, "没有 CMDB 读取权限")
	require.NoError(t, s.DB.Model(&Access{}).Where("user_id = ?", 2).Update("read", true).Error)
	result, err := s.SearchAssets(ctx, user, AssetQuery{})
	require.NoError(t, err)
	require.Equal(t, 20, result.Query.PageSize)
	require.Equal(t, 1, result.Query.Page)
	require.Empty(t, result.Devices)

	for _, query := range []AssetQuery{
		{ExpireFrom: "2027-02-29"}, {ExpireTo: "2027-1-1"},
		{ExpireFrom: "2027-12-31", ExpireTo: "2027-01-01"},
		{Page: -1}, {PageSize: 26},
	} {
		_, err := s.SearchAssets(ctx, user, query)
		require.Error(t, err)
	}
	user.Disabled = 1
	_, err = s.SearchAssets(ctx, user, AssetQuery{})
	require.ErrorContains(t, err, "当前账号不可用")
}
