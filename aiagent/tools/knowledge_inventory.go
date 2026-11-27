package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/models"
	"gorm.io/gorm"
)

var inventoryIdentifierWords = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_-]*`)

func init() {
	register(opsDef("target_statistics", "Count or group the current authorized platform Target database, over the complete scope (not Excel or a list page). Both query and group_by are REQUIRED. query: copy the user's complete device identifier/name/IP/prefix literally, including trailing hyphens; use an explicit empty string ONLY for a requested all-device count. group_by: os, agent_version or engine_name for distributions; none for total only. This is current registered inventory, not a time-window health query. Combine with document/monitoring tools as the question requires.",
		aiagent.ToolParameter{Name: "query", Type: "string", Required: true, Description: "Required literal device filter from the question; preserve complete punctuation. Explicit empty string means all authorized devices."},
		aiagent.ToolParameter{Name: "group_by", Type: "string", Required: true, Enum: []string{"none", "os", "agent_version", "engine_name"}, Description: "Required: requested distribution field, or none explicitly for total count."}), targetStatisticsTool)
}

func targetStatisticsTool(ctx context.Context, d *aiagent.ToolDeps, a map[string]interface{}, p map[string]string) (string, error) {
	if getArgString(a, "group_by") == "" {
		return "", fmt.Errorf("缺少group_by：按用户所问字段传os/agent_version/engine_name；只问总数时明确传none。请修正后继续完成原问题，不要重复未携带分组字段的调用")
	}
	args := map[string]interface{}{"operation": "target_statistics"}
	for k, v := range a {
		if k != "query" && k != "group_by" {
			return "", fmt.Errorf("target_statistics仅接受query和group_by；不使用table、SQL或time_range")
		}
		if k != "group_by" || v != "none" {
			args[k] = v
		}
	}
	return queryKnowledgeData(ctx, d, args, p)
}

// Device counts run over the entire authorized current Target catalog in SQL.
// A paginated list or an uploaded workbook is never a platform inventory count.
func inventoryScope(d *aiagent.ToolDeps, user *models.User) (*gorm.DB, string, error) {
	groups, admin, err := getUserBgids(d, user)
	if err != nil {
		return nil, "", err
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i] < groups[j] })
	fingerprint := fmt.Sprintf("%t:%v", admin, groups)
	db := d.DBCtx.DB.Model(&models.Target{})
	if !admin {
		if len(groups) == 0 {
			db = db.Where("1=0")
		} else {
			db = db.Where("ident IN (?)", d.DBCtx.DB.Model(&models.TargetBusiGroup{}).Select("target_ident").Where("group_id IN ?", groups))
		}
	}
	return db, fingerprint, nil
}

func queryKnowledgeInventory(ctx context.Context, d *aiagent.ToolDeps, a map[string]interface{}, p map[string]string) (string, error) {
	if _, present := a["query"]; !present {
		return "", fmt.Errorf("target_statistics必须显式提供query筛选：按问题中的完整设备标识/前缀原样填写；只有用户要求全部设备才传query=\"\"。分布用group_by=os/agent_version/engine_name；优先使用独立target_statistics工具，不需要table或time_range。请补全参数继续原任务")
	}
	if getArgString(a, "database") != "" || getArgString(a, "sql") != "" {
		return "", fmt.Errorf("target_statistics不接受SQL的database/sql参数；分布请传group_by=os或agent_version或engine_name")
	}
	group := getArgString(a, "group_by")
	groupAlias := getArgString(a, "table")
	if groupAlias != "" {
		// This operation has no SQL table input. Accept only an unambiguous
		// whitelisted dimension alias, never a table expression or inferred field.
		if groupAlias != "os" && groupAlias != "agent_version" && groupAlias != "engine_name" {
			return "", fmt.Errorf("target_statistics的table仅兼容分组字段os/agent_version/engine_name；请使用group_by")
		}
		if group != "" && group != groupAlias {
			return "", fmt.Errorf("group_by与table分组字段冲突，请明确一个统计维度")
		}
		group = groupAlias
	}
	user, err := getUser(d, p)
	if err != nil {
		return "", err
	}
	db, scope, err := inventoryScope(d, user)
	if err != nil {
		return "", err
	}
	db = db.WithContext(ctx)
	query := getArgString(a, "query")
	if len(query) > 200 {
		return "", fmt.Errorf("设备筛选最多200字节")
	}
	if inventoryQueryTruncatesLiteral(query, p["user_input"]) {
		return "", fmt.Errorf("%s", aiagent.LangText(p["lang"], "query截短了问题中的设备标识或筛选串；请保留完整字符（含连字符、下划线）后重试", "query truncates a device identifier/filter from the question; retry with its complete characters, including hyphens and underscores"))
	}
	if query != "" {
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(query) + "%"
		db = db.Where("(ident LIKE ? ESCAPE '!' OR host_ip LIKE ? ESCAPE '!' OR note LIKE ? ESCAPE '!' OR os LIKE ? ESCAPE '!')", pattern, pattern, pattern, pattern)
	}
	if group != "" && group != "os" && group != "agent_version" && group != "engine_name" {
		return "", fmt.Errorf("group_by仅支持os/agent_version/engine_name；不从台账推断实时健康")
	}
	var total int64
	var buckets []struct {
		Value string `json:"value"`
		Count int64  `json:"count"`
	}
	// Count and grouping share a snapshot, including catalogs larger than the list limit.
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Count(&total).Error; err != nil {
			return err
		}
		if group == "" {
			return nil
		}
		return tx.Select("COALESCE(" + group + ", '') AS value, COUNT(*) AS count").Group(group).Order("count DESC,value").Limit(101).Scan(&buckets).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return "", err
	}
	truncated := len(buckets) > 100
	if truncated {
		buckets = buckets[:100]
	}
	normalized := map[string]string{}
	if groupAlias != "" {
		normalized["table"] = "group_by=" + group
	}
	result, _ := json.Marshal(map[string]interface{}{"source": "platform_target_database", "scope": "当前账号可读的已接入Target设备；不代表所有CMDB资产或客户全部设备", "total": total, "group_by": group, "groups": buckets, "groups_truncated": truncated, "count_complete": true, "health": "not_evaluated", "normalized_arguments": normalized, "note": "按设备ident计数，不按IP合并；仅group_by指定字段的空值属于此分组，不得归因于其他字段；省略group_by只给总数、不推断任何字段分布或设备健康；总数覆盖全范围，不受分页限制"})
	label := "Target设备库 · 总数"
	if group != "" {
		label = "Target设备库 · " + group + "分组"
	}
	return opsJSON(aiagent.KnowledgeDataEvidence{SourceLabel: label, Operation: "target_statistics", InventoryScope: scope, QueriedAt: time.Now().Unix(), Query: query, Result: result, Truncated: truncated}, nil)
}

// Refuse an observed loss of literal specificity; this does not infer a filter
// from natural language or constrain a plan's document/database tool choices.
func inventoryQueryTruncatesLiteral(query, question string) bool {
	q := strings.ToLower(query)
	if len(q) < 3 {
		return false
	}
	words := inventoryIdentifierWords.FindAllString(strings.ToLower(question), -1)
	for _, word := range words {
		if word == q {
			return false
		}
	}
	for _, word := range words {
		if len(word) > len(q) && strings.HasPrefix(word, q) && (strings.ContainsAny(word, "0123456789") || strings.ContainsAny(word, "-_")) {
			return true
		}
	}
	return false
}
