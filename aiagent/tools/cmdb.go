package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/aiagent"
	skillruntime "github.com/ccfos/nightingale/v6/aiagent/skill_runtime"
	"github.com/ccfos/nightingale/v6/aiagent/tools/defs"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
)

func init() { register(defs.SearchCMDBDevices, searchCMDBDevices) }

func searchCMDBDevices(ctx context.Context, deps *aiagent.ToolDeps, args map[string]interface{}, params map[string]string) (string, error) {
	user, err := getUser(deps, params)
	if err != nil {
		return "", err
	}
	if user.IsDisabled() {
		return "", errors.New("当前账号不可用")
	}
	if deps.CMDB == nil {
		return "", errors.New("CMDB 设备台账未启用")
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return "", errors.New("CMDB 查询参数无效")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var query cmdb.AssetQuery
	if err := decoder.Decode(&query); err != nil {
		return "", errors.New("CMDB 查询参数无效，请按工具定义传入筛选条件")
	}
	result, err := deps.CMDB.SearchAssets(ctx, cmdb.User{ID: user.Id, Admin: user.IsAdmin(), Disabled: user.Disabled}, query)
	if err != nil {
		return "", err
	}

	shortened := false
	for i := range result.Devices {
		device := &result.Devices[i]
		for _, field := range []*string{&device.Hostname, &device.DeviceType, &device.Vendor, &device.Model, &device.Room, &device.Rack, &device.WarrantyType} {
			*field = strings.Map(func(r rune) rune {
				if unicode.IsControl(r) {
					return ' '
				}
				return r
			}, *field)
			const maxFieldBytes = 128
			if len(*field) > maxFieldBytes {
				end := maxFieldBytes - len("…")
				for !utf8.RuneStart((*field)[end]) {
					end--
				}
				*field = (*field)[:end] + "…"
				shortened = true
			}
		}
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		return "", err
	}
	note := "CMDB 台账数据。按查询范围和 total 说明结果，用 next_page 继续分页。"
	if shortened {
		note += " 部分过长的显示字段以省略号结尾。"
	}
	return skillruntime.FenceOutput(output.String(), "", skillruntime.FenceMeta{SkillName: "CMDB inventory", Note: note}), nil
}
