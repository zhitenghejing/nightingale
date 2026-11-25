package chat_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/aiagent/chat"
	"github.com/ccfos/nightingale/v6/aiagent/llm"
	_ "github.com/ccfos/nightingale/v6/aiagent/tools"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	ctxpkg "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type cmdbWarrantyModel struct {
	hosts []string
}

func (*cmdbWarrantyModel) Name() string { return "cmdb-warranty-fixture" }

func (m *cmdbWarrantyModel) Generate(_ context.Context, req *llm.GenerateRequest) (*llm.GenerateResponse, error) {
	page := 1
	for i := len(req.Messages) - 1; i >= 0; i-- {
		message := req.Messages[i]
		if message.Role != llm.RoleTool || message.ToolName != "search_cmdb_devices" {
			continue
		}
		_, output, _ := strings.Cut(message.Content, "--- stdout ---\n")
		raw, _, _ := strings.Cut(output, "\n[END UNTRUSTED")
		var result cmdb.AssetSearchResult
		if json.Unmarshal([]byte(raw), &result) != nil || result.Total != 2 || result.ExpiryUnknownTotal != 1 || len(result.Devices) != 1 {
			return nil, fmt.Errorf("unexpected CMDB evidence")
		}
		m.hosts = append(m.hosts, result.Devices[0].Hostname)
		page = result.NextPage
		break
	}
	if page == 0 {
		return &llm.GenerateResponse{Content: "2027 年过保设备：" + strings.Join(m.hosts, "、"), FinishReason: "stop"}, nil
	}
	for _, tool := range req.Tools {
		if tool.Name == "search_cmdb_devices" {
			return &llm.GenerateResponse{ToolCalls: []llm.ToolCall{{
				ID: fmt.Sprintf("cmdb-page-%d", page), Name: tool.Name,
				Arguments: fmt.Sprintf(`{"expire_from":"2027-01-01","expire_to":"2027-12-31","page_size":1,"page":%d}`, page),
			}}, FinishReason: "tool_calls"}, nil
		}
	}
	return nil, fmt.Errorf("CMDB search tool is missing")
}

func (*cmdbWarrantyModel) GenerateStream(context.Context, *llm.GenerateRequest) (<-chan llm.StreamChunk, error) {
	return nil, fmt.Errorf("unexpected streaming request")
}

func TestGeneralChatQueriesCMDBWarrantyInventory(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { pool.Close() })
	require.NoError(t, db.AutoMigrate(&models.User{}))
	require.NoError(t, db.Create(&models.User{Id: 2, Username: "inventory-reader", Roles: "Standard"}).Error)
	store := &cmdb.Store{DB: db}
	require.NoError(t, store.Migrate())
	require.NoError(t, db.Create(&cmdb.Access{UserID: 2, Read: true}).Error)
	for _, item := range []struct{ host, expiry string }{
		{"最后一天设备", "2027-12-31"}, {"第一天设备", "2027-01-01"}, {"范围外设备", "2028-01-01"}, {"未知到期设备", ""},
	} {
		require.NoError(t, store.Write(func(tx *gorm.DB) error {
			_, err := store.Save(tx, cmdb.Document{
				"customer": "验收", "serial": item.host, "hostname": item.host, "expire": item.expiry,
				"managementIp": "198.51.100.26", "note": "fixture internal note",
			}, 1, true)
			return err
		}))
	}

	handler, ok := chat.Lookup("general_chat")
	require.True(t, ok)
	request := &chat.AIChatRequest{UserInput: "2027年会过保的设备有哪些"}
	fake := &cmdbWarrantyModel{}
	deps := &aiagent.ToolDeps{DBCtx: ctxpkg.NewContext(ctx, db, true), CMDB: store}
	agent := aiagent.NewAgent(&aiagent.AgentConfig{
		MaxIterations: 4, Timeout: 5000,
		UserPromptRendered: handler.BuildPrompt(request),
		Tools:              aiagent.GetBuiltinToolDefs(handler.SelectTools(request)),
	}, aiagent.WithLLMClient(fake), aiagent.WithToolDeps(deps))
	response, err := agent.Run(ctx, &aiagent.AgentRequest{UserMessage: request.UserInput, Params: map[string]string{"user_id": "2"}})
	require.NoError(t, err)
	require.True(t, response.Success, response.Error)
	require.Equal(t, "2027 年过保设备：第一天设备、最后一天设备", response.Content)
	require.Len(t, response.Steps, 2)
	for _, step := range response.Steps {
		require.Equal(t, "search_cmdb_devices", step.Action)
		require.NotContains(t, step.Observation, "198.51.100.26")
		require.NotContains(t, step.Observation, "fixture internal note")
	}
	t.Run("recorded rack placement", func(t *testing.T) {
		require.NoError(t, db.Create(&cmdb.Room{ID: "rack-room", Name: "公司机房"}).Error)
		require.NoError(t, db.Create(&cmdb.Rack{ID: "fixture-rack", RoomID: "rack-room", Name: "公司机柜", Units: 42, Version: 1}).Error)
		for _, doc := range []cmdb.Document{
			{"customer": "验收", "serial": "placed", "hostname": "OpenVPN", "businessIp": "192.0.2.250", "rackId": "fixture-rack", "uStart": float64(29), "uEnd": float64(30)},
			{"customer": "验收", "serial": "one-unit", "hostname": "单 U 设备", "rackId": "fixture-rack", "uStart": float64(31), "uEnd": float64(31)},
			{"customer": "验收", "serial": "pending", "hostname": "待上架设备", "rackId": "fixture-rack"},
		} {
			require.NoError(t, store.Write(func(tx *gorm.DB) error {
				_, err := store.Save(tx, doc, 1, true)
				return err
			}))
		}
		for _, scenario := range []struct {
			keyword string
			want    map[string]float64
			uRange  string
		}{
			{"192.0.2.250", map[string]float64{"u_start": 29, "u_end": 30, "occupied_units": 2, "rack_units": 42}, "29U-30U"},
			{"单 U 设备", map[string]float64{"u_start": 31, "u_end": 31, "occupied_units": 1, "rack_units": 42}, "31U"},
			{"待上架设备", map[string]float64{"rack_units": 42}, ""},
		} {
			out, handled, err := aiagent.ExecuteBuiltinTool(ctx, deps, "search_cmdb_devices", map[string]string{"user_id": "2"},
				fmt.Sprintf(`{"keyword":%q}`, scenario.keyword))
			require.NoError(t, err)
			require.True(t, handled)
			_, stdout, _ := strings.Cut(out, "--- stdout ---\n")
			raw, _, _ := strings.Cut(stdout, "\n[END UNTRUSTED")
			var result struct{ Devices []map[string]interface{} }
			require.NoError(t, json.Unmarshal([]byte(raw), &result))
			require.Len(t, result.Devices, 1)
			if scenario.uRange != "" {
				require.Equal(t, scenario.uRange, result.Devices[0]["u_range"])
			} else {
				require.NotContains(t, result.Devices[0], "u_range")
			}
			for _, field := range []string{"u_start", "u_end", "occupied_units", "rack_units"} {
				if value, ok := scenario.want[field]; ok {
					require.Equal(t, value, result.Devices[0][field], field)
				} else {
					require.NotContains(t, result.Devices[0], field)
				}
			}
		}
	})
	require.NoError(t, db.Model(&cmdb.Access{}).Where("user_id = ?", 2).Update("read", false).Error)
	_, handled, err := aiagent.ExecuteBuiltinTool(ctx, deps, "search_cmdb_devices", map[string]string{"user_id": "2"}, "{}")
	require.True(t, handled)
	require.ErrorContains(t, err, "没有 CMDB 读取权限")
	_, _, err = aiagent.ExecuteBuiltinTool(ctx, deps, "search_cmdb_devices", map[string]string{"user_id": "2"}, `{"user_id":1}`)
	require.ErrorContains(t, err, "查询参数无效")
}
