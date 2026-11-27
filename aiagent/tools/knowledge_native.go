package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/aiagent"
)

// NativeReadEvidence records provenance alongside the existing native tool
// result. It neither executes the query again nor changes the native schema.
// Other operational tools keep their existing response contracts.
func NativeReadEvidence(ctx context.Context, d *aiagent.ToolDeps, user int64, name, arguments, raw string) (*aiagent.KnowledgeDataEvidence, error) {
	switch name {
	case "list_targets", "get_target_detail", "get_target_realtime_status", "list_datasources", "list_metrics", "query_prometheus", "list_databases", "list_tables", "describe_table", "query_log":
	default:
		return nil, nil
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return nil, err
	}
	var value interface{}
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, err
	}
	if m, ok := value.(map[string]interface{}); ok && m["error"] != nil {
		return nil, fmt.Errorf("read did not obtain evidence")
	}
	e := &aiagent.KnowledgeDataEvidence{Operation: name, QueriedAt: time.Now().Unix(), Query: getArgString(args, "query"), TimeRange: getArgString(args, "time_range")}
	if name == "query_log" && getArgString(args, "sql") != "" {
		e.Query = getArgString(args, "sql")
	}
	switch name {
	case "list_targets", "list_datasources":
		var list struct {
			Count int `json:"count"`
			Items []struct {
				ID     int64  `json:"id"`
				Ident  string `json:"ident"`
				Status string `json:"status"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return nil, err
		}
		e.Truncated = list.Count > len(list.Items) || len(list.Items) >= getArgInt(args, "limit", 50)
		for _, item := range list.Items {
			if name == "list_targets" {
				e.TargetIdents = append(e.TargetIdents, item.Ident)
			} else if item.Status == "enabled" {
				e.DatasourceIDs = append(e.DatasourceIDs, item.ID)
			}
		}
	case "get_target_detail", "get_target_realtime_status":
		ident := getArgString(args, "ident")
		if ident == "" {
			return nil, fmt.Errorf("missing ident")
		}
		e.TargetIdents = []string{ident}
	default:
		id := getArgInt64(args, "datasource_id")
		if id <= 0 {
			return nil, fmt.Errorf("missing datasource_id")
		}
		e.DatasourceIDs = []int64{id}
	}
	if err := stampKnowledgeData(ctx, d, user, e); err != nil {
		return e, err
	}
	if name == "get_target_detail" {
		e.TargetVersions[e.TargetIdents[0]] = knowledgeTargetVersion(raw)
	}
	if err := CheckKnowledgeDataAccess(ctx, d, user, *e); err != nil {
		return e, err
	}
	e.SourceLabel = strings.ReplaceAll(name, "_", " ")
	if name == "list_targets" || name == "get_target_detail" {
		e.SourceLabel = "监控目标库"
		e.SourceURL = "/targets"
	}
	return e, nil
}
