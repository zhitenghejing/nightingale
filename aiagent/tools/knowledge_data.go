package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/aiagent"
	"github.com/ccfos/nightingale/v6/datasource"
	"github.com/ccfos/nightingale/v6/datasource/postgresql"
	"github.com/ccfos/nightingale/v6/dskit/postgres"
	"github.com/ccfos/nightingale/v6/dskit/sqlbase"
	"github.com/ccfos/nightingale/v6/dskit/types"
	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/environment"
	"github.com/prometheus/prometheus/promql/parser"
)

var knowledgeDataHandlers = map[string]aiagent.BuiltinToolFunc{
	"list_datasources":           listDatasourcesBuiltin,
	"list_targets":               listTargets,
	"get_target_detail":          getTargetDetail,
	"get_target_realtime_status": getTargetRealtimeStatus,
	"list_metrics":               listMetrics,
	"query_prometheus":           queryPrometheusTool,
	"list_databases":             listDatabasesTool,
	"list_tables":                listTablesTool,
	"describe_table":             describeTableTool,
	"query_log":                  queryLogDataTool,
}

func init() {
	register(opsDef("resolve_knowledge_object", "Resolve report object against authorized monitored targets. Pass exact ident and/or host_ip when known; query only discovers name candidates. Conflicting or multiple matches remain unconfirmed. A match is current identity evidence, not proof of historical ownership or health.",
		aiagent.ToolParameter{Name: "ident", Type: "string"}, aiagent.ToolParameter{Name: "host_ip", Type: "string"}, aiagent.ToolParameter{Name: "query", Type: "string"}), resolveKnowledgeObject)
	register(opsDef("query_knowledge_data", "Read authorized platform/database/monitoring evidence. operation is one of list_datasources,list_targets,get_target_detail,get_target_realtime_status,list_metrics,query_prometheus,list_databases,list_tables,describe_table,query_log. Discover datasource/schema first. SQL query_log requires a single SELECT and an administrator-configured read-only database account; the server caps results at 100 rows. query is a target search, metric prefix or PromQL as appropriate. Empty/old data does not prove healthy. Never executes scripts or writes.",
		aiagent.ToolParameter{Name: "operation", Type: "string", Required: true, Enum: []string{"list_datasources", "list_targets", "get_target_detail", "get_target_realtime_status", "list_metrics", "query_prometheus", "list_databases", "list_tables", "describe_table", "query_log"}},
		aiagent.ToolParameter{Name: "datasource_id", Type: "integer"}, aiagent.ToolParameter{Name: "ident", Type: "string"}, aiagent.ToolParameter{Name: "query", Type: "string"}, aiagent.ToolParameter{Name: "sql", Type: "string"}, aiagent.ToolParameter{Name: "database", Type: "string"}, aiagent.ToolParameter{Name: "table", Type: "string"}, aiagent.ToolParameter{Name: "time_range", Type: "string", Description: "Up to 24h, e.g. 1h or 30m"}, aiagent.ToolParameter{Name: "query_type", Type: "string", Enum: []string{"instant", "range"}}, aiagent.ToolParameter{Name: "limit", Type: "integer"}), queryKnowledgeData)
}

// Revalidate at invocation and before publishing/history replay. Catalog results
// carry every returned object ID, so a later permission loss also invalidates them.
func CheckKnowledgeDataAccess(ctx context.Context, d *aiagent.ToolDeps, userID int64, e aiagent.KnowledgeDataEvidence) error {
	if d == nil || d.DBCtx == nil {
		return fmt.Errorf("没有权限查阅")
	}
	user, err := getUser(d, map[string]string{"user_id": fmt.Sprint(userID)})
	if err != nil || user.Disabled != 0 {
		return fmt.Errorf("没有权限查阅")
	}
	if strings.HasPrefix(e.Operation, "workbook_") {
		if d.Library == nil || e.WorkbookVersion <= 0 || e.WorkbookDigest == "" {
			return fmt.Errorf("未找到相关资料")
		}
		k, err := d.Library.WithContext(ctx).Version(userID, e.WorkbookVersion)
		if err != nil || k.State != "ready" || k.Digest != e.WorkbookDigest {
			return fmt.Errorf("资料版本已变更、失效或没有权限查阅")
		}
		return nil
	}
	if strings.HasPrefix(e.Operation, "environment_") {
		return (&environment.Store{DB: d.DBCtx.DB.WithContext(ctx)}).Validate(userID, e.EnvironmentRefs)
	}
	for _, id := range e.DatasourceIDs {
		ds, err := models.DatasourceGet(d.DBCtx, id)
		if err != nil || ds == nil || ds.Status != "enabled" || d.FilterDatasources == nil {
			return fmt.Errorf("没有权限查阅，或数据源已停用")
		}
		if expected := e.DatasourceVersions[id]; expected != "" && expected != knowledgeDatasourceVersion(ds) {
			return fmt.Errorf("数据源配置已变更，请重新查询")
		}
		if len(d.FilterDatasources([]*models.Datasource{ds}, user)) != 1 {
			return fmt.Errorf("没有权限查阅")
		}
	}
	for _, ident := range e.TargetIdents {
		raw, err := getTargetDetail(ctx, d, map[string]interface{}{"ident": ident}, map[string]string{"user_id": fmt.Sprint(userID)})
		if expected := e.TargetVersions[ident]; err == nil && expected != "" && expected != knowledgeTargetVersion(raw) {
			return fmt.Errorf("设备信息已变更，请重新核对")
		}
		if err != nil || strings.Contains(raw, `"error"`) {
			return fmt.Errorf("没有权限查阅，或设备已失效")
		}
	}
	return nil
}

func queryKnowledgeData(ctx context.Context, d *aiagent.ToolDeps, a map[string]interface{}, p map[string]string) (string, error) {
	if len(getArgString(a, "query")) > 8192 {
		return "", fmt.Errorf("query exceeds limit")
	}
	op := getArgString(a, "operation")
	handler, ok := knowledgeDataHandlers[op]
	if !ok {
		return "", fmt.Errorf("unsupported read-only operation")
	}
	u, err := getUser(d, p)
	if err != nil || u.Disabled != 0 {
		return "", fmt.Errorf("没有权限查阅")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	scopedDeps := *d
	scopedDB := *d.DBCtx
	scopedDB.DB = scopedDB.DB.WithContext(ctx)
	scopedDB.Ctx = ctx
	scopedDeps.DBCtx = &scopedDB
	d = &scopedDeps
	args := map[string]interface{}{}
	for _, k := range []string{"datasource_id", "ident", "query", "sql", "database", "table", "time_range", "query_type", "limit"} {
		if v, ok := a[k]; ok {
			args[k] = v
		}
	}
	limit := getArgInt(a, "limit", 50)
	if limit > 100 {
		limit = 100
	}
	args["limit"] = float64(limit)
	e := aiagent.KnowledgeDataEvidence{Operation: op, QueriedAt: time.Now().Unix(), Query: getArgString(a, "query"), TimeRange: getArgString(a, "time_range")}
	switch op {
	case "list_datasources", "list_targets":
		if op == "list_datasources" && d.FilterDatasources == nil {
			return "", fmt.Errorf("没有权限查阅")
		}
	case "get_target_detail", "get_target_realtime_status":
		ident := getArgString(a, "ident")
		if ident == "" {
			return "", fmt.Errorf("ident is required; resolve the object first")
		}
		e.TargetIdents = []string{ident}
	default:
		id := getArgInt64(a, "datasource_id")
		if id <= 0 {
			return "", fmt.Errorf("datasource_id is required; use list_datasources first")
		}
		e.DatasourceIDs = []int64{id}
		if err := CheckKnowledgeDataAccess(ctx, d, u.Id, e); err != nil {
			return "", err
		}
		ds, err := models.DatasourceGet(d.DBCtx, id)
		if err != nil {
			return "", err
		}
		args["datasource_type"] = ds.PluginType
		if sqlDatasourceTypes[ds.PluginType] && d.GetSQLDatasource == nil {
			return "", fmt.Errorf("SQL datasource adapter unavailable")
		}
		if ds.PluginType == "pgsql" {
			plug, ok := d.GetSQLDatasource(ds.PluginType, id)
			pg, typed := plug.(*postgresql.PostgreSQL)
			if !ok || !typed || len(pg.Shards) == 0 || pg.Shards[0].DB == "" {
				return "", fmt.Errorf("PostgreSQL source must configure an explicit database")
			}
			configured := pg.Shards[0].DB
			if requested := getArgString(a, "database"); op != "list_databases" && requested != "" && requested != configured {
				return "", fmt.Errorf("没有权限查阅其他数据库")
			}
			args["database"] = configured
			clone := *pg
			clone.Shards = make([]*postgres.PostgreSQL, len(pg.Shards))
			for i, shard := range pg.Shards {
				v := *shard
				clone.Shards[i] = &v
			}
			scoped := *d
			scoped.GetSQLDatasource = func(string, int64) (datasource.Datasource, bool) { return &clone, true }
			d = &scoped
			if op == "list_databases" {
				handler = func(context.Context, *aiagent.ToolDeps, map[string]interface{}, map[string]string) (string, error) {
					return opsJSON([]string{configured}, nil)
				}
			}
		}

		if op == "list_metrics" {
			args["keyword"] = getArgString(a, "query")
		}
		switch op {
		case "query_prometheus":
			if ds.PluginType != "prometheus" {
				return "", fmt.Errorf("not a Prometheus datasource")
			}
			args["step"] = float64(60)
		case "query_log":
			if ds.PluginType != "mysql" && ds.PluginType != "pgsql" {
				return "", fmt.Errorf("query_log currently requires a MySQL or PostgreSQL adapter with database-enforced read-only transactions")
			}
			sql := getArgString(a, "sql")
			if err := validateKnowledgeSQL(sql); err != nil {
				return "", err
			}
			e.Query = sql
			args["sql"] = "SELECT * FROM (" + strings.TrimRight(strings.TrimSpace(sql), "; ") + ") AS knowledge_read LIMIT 100"
		case "list_metrics":
			if ds.PluginType != "prometheus" {
				return "", fmt.Errorf("metric discovery requires a Prometheus datasource")
			}
			// Metric discovery can be expensive; underlying handler bounds output.
		}
	}
	if op == "query_prometheus" || op == "query_log" {
		duration := e.TimeRange
		if duration == "" {
			duration = "1h"
		}
		v, err := time.ParseDuration(duration)
		if err != nil || v <= 0 || v > 24*time.Hour {
			return "", fmt.Errorf("time_range must be a duration up to 24h")
		}
		args["time_range"] = duration
		e.TimeRange = duration
	}
	if err := CheckKnowledgeDataAccess(ctx, d, u.Id, e); err != nil {
		return "", err
	}
	if err := stampKnowledgeData(ctx, d, u.Id, &e); err != nil {
		return "", err
	}
	// Do not inherit page scope or approval params. All effective query args are explicit.
	ctx = types.WithCallContext(ctx, types.CallContext{Operator: u.Username, DatasourceID: getArgInt64(a, "datasource_id"), EnforceReadOnly: true, EnforceReadOnlyTransaction: op == "query_log"})
	raw, err := handler(ctx, d, args, map[string]string{"user_id": fmt.Sprint(u.Id)})
	if err != nil {
		return "", err
	}
	if op == "query_prometheus" {
		raw = knowledgePrometheusSamples(ctx, d, args, p, raw)
	}
	var value interface{}
	if err = json.Unmarshal([]byte(raw), &value); err != nil {
		return "", err
	}
	if m, ok := value.(map[string]interface{}); ok && m["error"] != nil {
		return "", fmt.Errorf("query did not obtain evidence: %v", m["error"])
	}
	if op == "list_datasources" || op == "list_targets" {
		if op == "list_datasources" {
			var catalog struct {
				Items []datasourceResult `json:"items"`
			}
			if err := json.Unmarshal([]byte(raw), &catalog); err != nil {
				return "", err
			}
			enabled := []datasourceResult{}
			for _, ds := range catalog.Items {
				if ds.Status == "enabled" {
					enabled = append(enabled, ds)
				}
			}
			raw = marshalList(len(enabled), enabled)
		}
		var list struct {
			Items []struct {
				ID     int64  `json:"id"`
				Ident  string `json:"ident"`
				Status string `json:"status"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return "", err
		}
		for _, item := range list.Items {
			if op == "list_datasources" {
				if item.Status == "enabled" {
					e.DatasourceIDs = append(e.DatasourceIDs, item.ID)
				}
			} else {
				e.TargetIdents = append(e.TargetIdents, item.Ident)
			}
		}
		// Disabled datasource names are configuration facts, but not query candidates.
	}
	if op == "list_datasources" || op == "list_targets" {
		if err := stampKnowledgeData(ctx, d, u.Id, &e); err != nil {
			return "", err
		}
	}
	// Never cut raw JSON or return a successful empty result after truncation.
	if len(raw) > 64<<10 {
		return "", fmt.Errorf("result exceeds 64 KiB; narrow query or time range")
	}
	var coverage struct {
		Count int               `json:"count"`
		Items []json.RawMessage `json:"items"`
	}
	if json.Unmarshal([]byte(raw), &coverage) == nil {
		e.Truncated = coverage.Count >= limit || len(coverage.Items) >= limit
	}
	e.Result = json.RawMessage(raw)
	if err := CheckKnowledgeDataAccess(ctx, d, u.Id, e); err != nil {
		return "", err
	}
	return opsJSON(e, nil)
}

func resolveKnowledgeObject(ctx context.Context, d *aiagent.ToolDeps, a map[string]interface{}, p map[string]string) (string, error) {
	ident, ip, q := strings.TrimSpace(getArgString(a, "ident")), strings.TrimSpace(getArgString(a, "host_ip")), strings.TrimSpace(getArgString(a, "query"))
	if len(ident) > 250 || len(q) > 1000 || len(ip) > 64 {
		return "", fmt.Errorf("object query exceeds limit")
	}
	if ident == "" && ip == "" && q == "" {
		return "", fmt.Errorf("ident, host_ip or query is required")
	}
	if ip != "" && net.ParseIP(ip) == nil {
		return "", fmt.Errorf("host_ip must be an exact IP address")
	}
	u, err := getUser(d, p)
	if err != nil || u.Disabled != 0 {
		return "", fmt.Errorf("没有权限查阅")
	}
	// Exact predicates run before LIMIT: a target later in the list is not lost.
	bgids, admin, err := getUserBgids(d, u)
	if err != nil {
		return "", err
	}
	db := models.DB(d.DBCtx).Model(&models.Target{})
	if !admin {
		if len(bgids) == 0 {
			return objectResult("not_found"), nil
		}
		db = db.Where("ident IN (?)", models.DB(d.DBCtx).Table("target_busi_group").Select("target_ident").Where("group_id IN ?", bgids))
	}
	if ident != "" {
		db = db.Where("ident = ?", ident)
	}
	if ip != "" {
		db = db.Where("host_ip = ?", ip)
	}
	if ident == "" && ip == "" {
		like := "%" + strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(q, "\\", "\\\\"), "%", "\\%"), "_", "\\_") + "%"
		db = db.Where("ident LIKE ? ESCAPE '\\' OR note LIKE ? ESCAPE '\\'", like, like)
	}
	var targets []models.Target
	if err := db.WithContext(ctx).Order("ident").Limit(21).Find(&targets).Error; err != nil {
		return "", err
	}
	status := "unconfirmed"
	if len(targets) == 0 {
		status = "not_found"
	} else if len(targets) == 1 && (ident != "" || ip != "") {
		status = "exact_identifier_match"
	} else if len(targets) > 1 {
		status = "ambiguous"
	}
	candidates := []targetResult{}
	idents := []string{}
	for i, t := range targets {
		if i >= 20 {
			break
		}
		candidates = append(candidates, targetResult{Id: t.Id, Ident: t.Ident, HostIp: t.HostIp, Note: t.Note, Tags: strings.Fields(t.Tags)})
		idents = append(idents, t.Ident)
	}
	e := aiagent.KnowledgeDataEvidence{Operation: "resolve_knowledge_object", TargetIdents: idents, QueriedAt: time.Now().Unix(), Truncated: len(targets) > 20}
	body, _ := json.Marshal(map[string]interface{}{"status": status, "candidates": candidates, "identity_basis": map[string]string{"ident": ident, "host_ip": ip}, "historical_mapping": "not_established", "note": "仅为当前监控对象标识核对；名称/型号相似不是关联，仍需核对院区、系统、时间和设备更换。"})
	e.Result = body
	if err := stampKnowledgeData(ctx, d, u.Id, &e); err != nil {
		return "", err
	}
	return opsJSON(e, nil)
}
func objectResult(status string) string {
	b, _ := json.Marshal(map[string]interface{}{"status": status, "candidates": []interface{}{}})
	out, _ := opsJSON(aiagent.KnowledgeDataEvidence{Operation: "resolve_knowledge_object", QueriedAt: time.Now().Unix(), Result: b}, nil)
	return out
}

var knowledgeSQLStart = regexp.MustCompile(`(?i)^(SELECT|WITH)\b`)

func validateKnowledgeSQL(sql string) error {
	if len(sql) > 8192 {
		return fmt.Errorf("SQL exceeds 8192 bytes")
	}
	if !knowledgeSQLStart.MatchString(strings.TrimSpace(sql)) {
		return fmt.Errorf("only SELECT/WITH queries are allowed")
	}
	if err := sqlbase.ValidateReadOnly(sql); err != nil {
		return err
	}
	return nil
}

// PromQL result timestamps can be evaluation times even when a source sample is
// stale. Preserve independent timestamp(selector) evidence for up to four inputs.
func knowledgePrometheusSamples(ctx context.Context, d *aiagent.ToolDeps, a map[string]interface{}, p map[string]string, raw string) string {
	expression, err := parser.ParseExpr(getArgString(a, "query"))
	if err != nil {
		return raw
	}
	selectors := map[string]bool{}
	samples := []interface{}{}
	parser.Inspect(expression, func(node parser.Node, _ []parser.Node) error {
		selector, ok := node.(*parser.VectorSelector)
		if !ok || len(selectors) >= 4 {
			return nil
		}
		q := selector.String()
		if selectors[q] {
			return nil
		}
		selectors[q] = true
		args := map[string]interface{}{"datasource_id": a["datasource_id"], "query": "timestamp(" + q + ")", "query_type": "instant", "time_range": a["time_range"]}
		result, e := queryPrometheusTool(ctx, d, args, p)
		if e != nil {
			samples = append(samples, map[string]interface{}{"selector": q, "status": "unavailable"})
		} else {
			var v interface{}
			if json.Unmarshal([]byte(result), &v) == nil {
				samples = append(samples, map[string]interface{}{"selector": q, "timestamp_result": v, "sample_age_checks": knowledgeSampleAges(result)})
			}
		}
		return nil
	})
	var result interface{}
	if json.Unmarshal([]byte(raw), &result) != nil {
		return raw
	}
	out, _ := json.Marshal(map[string]interface{}{"query_result": result, "sample_times": samples, "freshness_note": "timestamp_result 的值是底层采样时间；query_result 的时间可能只是求值时间。空数据、无法取样或未覆盖的输入不能证明实时正常。最多核对四个输入选择器。"})
	return string(out)
}

func knowledgeFingerprint(raw string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) }
func knowledgeDatasourceVersion(ds *models.Datasource) string {
	return knowledgeFingerprint(fmt.Sprintf("%s|%s|%s|%s|%d", ds.PluginType, ds.Settings, ds.HTTP, ds.Auth, ds.UpdatedAt))
}
func stampKnowledgeData(ctx context.Context, d *aiagent.ToolDeps, user int64, e *aiagent.KnowledgeDataEvidence) error {
	e.DatasourceVersions = map[int64]string{}
	e.TargetVersions = map[string]string{}
	for _, id := range e.DatasourceIDs {
		ds, err := models.DatasourceGet(d.DBCtx, id)
		if err != nil {
			return err
		}
		e.DatasourceVersions[id] = knowledgeDatasourceVersion(ds)
	}
	for _, ident := range e.TargetIdents {
		raw, err := getTargetDetail(ctx, d, map[string]interface{}{"ident": ident}, map[string]string{"user_id": fmt.Sprint(user)})
		if err != nil {
			return err
		}
		e.TargetVersions[ident] = knowledgeTargetVersion(raw)
	}
	return nil
}

func knowledgeTargetVersion(raw string) string {
	var v map[string]interface{}
	if json.Unmarshal([]byte(raw), &v) != nil {
		return ""
	}
	delete(v, "update_at")
	b, _ := json.Marshal(v)
	return knowledgeFingerprint(string(b))
}

func knowledgeSampleAges(raw string) []map[string]interface{} {
	var r struct {
		Data []struct {
			Metric map[string]string `json:"metric"`
			Value  []json.RawMessage `json:"value"`
		} `json:"data"`
	}
	out := []map[string]interface{}{}
	if json.Unmarshal([]byte(raw), &r) != nil {
		return out
	}
	for _, sample := range r.Data {
		if len(sample.Value) != 2 {
			continue
		}
		var evaluated float64
		var value string
		if json.Unmarshal(sample.Value[0], &evaluated) != nil || json.Unmarshal(sample.Value[1], &value) != nil {
			continue
		}
		observed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			continue
		}
		out = append(out, map[string]interface{}{"metric": sample.Metric, "evaluation_unix": evaluated, "sample_unix": observed, "lag_seconds": evaluated - observed, "interpretation": "正数表示采样早于求值；是否过期还须结合采集间隔与业务标准。"})
	}
	return out
}
