package cmdb

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// AssetQuery filters the active inventory. Expiry bounds include both dates.
type AssetQuery struct {
	Keyword      string `json:"keyword,omitempty"`
	Room         string `json:"room,omitempty"`
	DeviceType   string `json:"device_type,omitempty"`
	WarrantyType string `json:"warranty_type,omitempty"`
	ExpireFrom   string `json:"expire_from,omitempty"`
	ExpireTo     string `json:"expire_to,omitempty"`
	Page         int    `json:"page,omitempty"`
	PageSize     int    `json:"page_size,omitempty"`
}

type AssetSummary struct {
	ID            string `json:"id"`
	Hostname      string `json:"hostname"`
	DeviceType    string `json:"device_type,omitempty"`
	Vendor        string `json:"vendor,omitempty"`
	Model         string `json:"model,omitempty"`
	Room          string `json:"room,omitempty"`
	Rack          string `json:"rack,omitempty"`
	UStart        int    `json:"u_start,omitempty"`
	UEnd          int    `json:"u_end,omitempty"`
	URange        string `json:"u_range,omitempty"`
	OccupiedUnits int    `json:"occupied_units,omitempty"`
	RackUnits     int    `json:"rack_units,omitempty"`
	Expire        string `json:"expire,omitempty"`
	WarrantyType  string `json:"warranty_type,omitempty"`
}

type AssetSearchResult struct {
	Query              AssetQuery     `json:"query"`
	Devices            []AssetSummary `json:"devices"`
	Total              int            `json:"total"`
	ExpiryUnknownTotal int            `json:"expiry_unknown_total"`
	NextPage           int            `json:"next_page,omitempty"`
}

var assetKeywordFields = [...]string{"room", "rack", "hostname", "vendor", "serial", "business", "model", "businessIp"}

func validAssetDate(value string) bool {
	parsed, err := time.Parse(time.DateOnly, value)
	return err == nil && parsed.Format(time.DateOnly) == value
}

// SearchAssets returns a bounded projection of the active inventory after
// applying the same user's CMDB read permission as the inventory page.
func (s *Store) SearchAssets(ctx context.Context, user User, query AssetQuery) (AssetSearchResult, error) {
	var result AssetSearchResult
	if user.ID < 1 || user.Disabled != 0 {
		return result, errors.New("当前账号不可用")
	}
	access, err := s.accessFor(ctx, user)
	if err != nil {
		return result, errors.New("CMDB 权限暂不可用")
	}
	if !access.Read {
		return result, errors.New("没有 CMDB 读取权限，请联系管理员授权")
	}

	for _, value := range []string{query.Keyword, query.Room, query.DeviceType, query.WarrantyType} {
		if len(value) > 256 {
			return result, errors.New("CMDB 筛选文本过长")
		}
	}
	if (query.ExpireFrom != "" && !validAssetDate(query.ExpireFrom)) || (query.ExpireTo != "" && !validAssetDate(query.ExpireTo)) {
		return result, errors.New("过保日期必须为有效的 YYYY-MM-DD")
	}
	if query.ExpireFrom != "" && query.ExpireTo != "" && query.ExpireFrom > query.ExpireTo {
		return result, errors.New("过保开始日期不能晚于结束日期")
	}
	if query.Page < 0 || query.PageSize < 0 || query.PageSize > 25 {
		return result, errors.New("页码必须为正整数，每页最多 25 台设备")
	}
	if query.Page == 0 {
		query.Page = 1
	}
	if query.PageSize == 0 {
		query.PageSize = 20
	}
	query.Keyword = strings.TrimSpace(query.Keyword)
	query.Room = strings.TrimSpace(query.Room)
	query.DeviceType = strings.TrimSpace(query.DeviceType)
	query.WarrantyType = strings.TrimSpace(query.WarrantyType)

	rows, err := readAssetRows(s.DB.WithContext(ctx), false)
	if err != nil {
		return result, errors.New("CMDB 设备台账暂不可用")
	}
	result.Query = query
	result.Devices = []AssetSummary{}
	words := strings.Fields(strings.ToLower(query.Keyword))
	room := strings.ToLower(query.Room)
	deviceType := strings.ToLower(query.DeviceType)
	withDateRange := query.ExpireFrom != "" || query.ExpireTo != ""
	var matches []AssetSummary
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return AssetSearchResult{}, err
		}
		doc := row.document()
		if !strings.Contains(strings.ToLower(str(doc, "room")), room) ||
			!strings.Contains(strings.ToLower(str(doc, "type")), deviceType) ||
			(query.WarrantyType != "" && str(doc, "warrantyType") != query.WarrantyType) ||
			!matchesAssetKeywords(doc, words) {
			continue
		}

		expire, _ := doc["expire"].(string)
		if !validAssetDate(expire) {
			result.ExpiryUnknownTotal++
			if withDateRange {
				continue
			}
			expire = ""
		}
		if (query.ExpireFrom != "" && expire < query.ExpireFrom) || (query.ExpireTo != "" && expire > query.ExpireTo) {
			continue
		}
		asset := AssetSummary{
			ID: str(doc, "id"), Hostname: str(doc, "hostname"), DeviceType: str(doc, "type"),
			Vendor: str(doc, "vendor"), Model: str(doc, "model"), Room: str(doc, "room"), Rack: str(doc, "rack"),
			Expire: expire, WarrantyType: str(doc, "warrantyType"),
		}
		if row.RackUnits != nil && *row.RackUnits > 0 {
			asset.RackUnits = *row.RackUnits
			if row.Start > 0 && row.End >= row.Start && row.End <= asset.RackUnits {
				asset.UStart, asset.UEnd = row.Start, row.End
				asset.OccupiedUnits = row.End - row.Start + 1
				asset.URange = fmt.Sprintf("%dU", row.Start)
				if row.End != row.Start {
					asset.URange = fmt.Sprintf("%s-%dU", asset.URange, row.End)
				}
			}
		}
		matches = append(matches, asset)
	}
	slices.SortFunc(matches, func(a, b AssetSummary) int {
		if (a.Expire == "") != (b.Expire == "") {
			return cmp.Compare(b.Expire, a.Expire) // Undated devices sort last.
		}
		if order := cmp.Compare(a.Expire, b.Expire); order != 0 {
			return order
		}
		return cmp.Compare(a.ID, b.ID)
	})
	result.Total = len(matches)
	if query.Page-1 <= result.Total/query.PageSize {
		start := (query.Page - 1) * query.PageSize
		end := start + min(query.PageSize, result.Total-start)
		result.Devices = append(result.Devices, matches[start:end]...)
		if end < result.Total {
			result.NextPage = query.Page + 1
		}
	}
	return result, nil
}

func matchesAssetKeywords(doc Document, words []string) bool {
	if len(words) == 0 {
		return true
	}
	var fields [len(assetKeywordFields)]string
	for i, field := range assetKeywordFields {
		fields[i] = strings.ToLower(str(doc, field))
	}
	for _, word := range words {
		if !slices.ContainsFunc(fields[:], func(field string) bool { return strings.Contains(field, word) }) {
			return false
		}
	}
	return true
}
