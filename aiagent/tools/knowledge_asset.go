package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
)

func queryKnowledgeAsset(ctx context.Context, d *aiagent.ToolDeps, a map[string]interface{}, p map[string]string) (string, error) {
	u, err := getUser(d, p)
	if err != nil || u.Disabled != 0 {
		return "", fmt.Errorf("没有CMDB资产读取权限")
	}
	serial, id := getArgString(a, "serial"), getArgString(a, "asset_id")
	store := &cmdb.Store{DB: d.DBCtx.DB.WithContext(ctx)}
	record, err := store.LookupAsset(cmdb.User{ID: u.Id, Admin: u.IsAdmin(), Disabled: u.Disabled}, serial, id)
	if err != nil {
		return "", err
	}
	receipt := aiagent.KnowledgeDataEvidence{Operation: "asset_lookup", QueriedAt: time.Now().Unix(), Query: serial, AssetLookupSerial: serial, AssetLookupID: id, AssetRefs: map[string]string{}, SourceURL: "/targets/cmdb", SourceLabel: "CMDB资产库"}
	result := map[string]interface{}{"source": "cmdb_device", "status": "not_found", "serial": serial, "complete": true, "note": "这是当前资产登记主数据；更新时间是记录写入时间，不是现场核验或采样时间。资产monitored/alertEnabled字段仅是登记，不证明当前Target接入或健康。未绑定机柜的位置文本仅是台账记录。"}
	if record != nil {
		receipt.AssetRefs[record.ID] = record.Fingerprint
		receipt.SourceLabel = fmt.Sprintf("CMDB资产库 · 序列号%s · 修订%d", record.Serial, record.Version)
		result["status"] = "found"
		result["record"] = record
	}
	receipt.Result, _ = json.Marshal(result)
	return opsJSON(receipt, nil)
}
