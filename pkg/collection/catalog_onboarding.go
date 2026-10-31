package collection

import (
	"encoding/json"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	"gorm.io/gorm"
)

// Catalog visibility is independent of execution readiness. In particular,
// native integrations imported as documentation must not disappear from the
// device taxonomy just because they do not have an immutable release yet.
type CatalogOnboarding struct {
	State     string   `json:"state"`
	Ready     bool     `json:"ready"`
	Execution string   `json:"execution"`
	Issues    []string `json:"issues"`
}

// Fill only the exact old, unconfigured documentation draft. Never reinterpret
// an author's fields/recipe, a published version or an existing device snapshot.
// Execution is also the one-time marker, so deliberately clearing fields later
// does not cause another bootstrap on read.
func (s *Store) initializeOnboarding(entry *CatalogEntry) error {
	d := entry.Draft
	if entry.PublishedVersionID != "" || d.Adapter != "documentation" || len(d.Fields) != 0 || d.Config != "" || d.Execution != "" || d.Compatibility != "" || d.Notify != nil {
		return nil
	}
	var paths map[string][]string
	if err := json.Unmarshal(catalogSeed, &paths); err != nil {
		return err
	}
	path, known := paths[entry.Ident]
	if !known {
		return nil // New/custom templates are wholly owned by their author.
	}
	d.Execution = "collector"
	if path[0] == "主机专项" || entry.Ident == "Linux" || entry.Ident == "Windows" || entry.Ident == "IIS" || entry.Ident == "Docker" {
		d.Execution = "local"
	}
	if path[0] == "云平台与 SaaS" {
		d.Execution = "cloud"
	}
	if entry.Ident == "Kubernetes" {
		d.Execution = "discovery"
	}
	d.Fields, d.Adapter = onboardingFields(entry.Ident, d.Execution)
	updated := string(canonical(d))
	result := s.DB.Model(&models.MonitorCatalog{}).
		Where("component_id = ? AND record_version = ? AND draft = ? AND published_version_id = ?", entry.ComponentID, entry.RecordVersion, string(entry.MonitorCatalog.Draft), "").
		Updates(map[string]interface{}{"draft": updated, "record_version": gorm.Expr("record_version + 1")})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		entry.Draft = d
		entry.MonitorCatalog.Draft = models.MonitorText(updated)
		entry.RecordVersion++
		return nil
	}
	// A concurrent editor won. Return that saved draft instead of stale defaults.
	if err := s.DB.First(&entry.MonitorCatalog, "component_id = ?", entry.ComponentID).Error; err != nil {
		return err
	}
	entry.Logo = string(entry.MonitorCatalog.Logo)
	if entry.PublishedVersionID != "" {
		doc, err := loadTemplate(s.DB, entry.PublishedVersionID)
		if err != nil {
			return err
		}
		entry.Published = doc.Catalog
		if doc.Profile != nil {
			entry.PublishedMetrics = doc.Profile.Metrics
			if entry.Published != nil && entry.Published.Definition.Pack == nil {
				entry.Published.Definition.Pack = doc.Profile
			}
		}
	}
	return json.Unmarshal([]byte(entry.MonitorCatalog.Draft), &entry.Draft)
}

// These are editable form drafts extracted from the existing integration
// instructions, not executable recipes or a compatibility certification. Other
// integrations keep their actual instructions rather than a fictitious login.
func onboardingFields(ident, execution string) ([]DeviceField, string) {
	text := func(key, label string, required bool) DeviceField {
		return DeviceField{Key: key, Label: label, Type: "text", Required: required}
	}
	secret := func(key, label string, required bool) DeviceField {
		return DeviceField{Key: key, Label: label, Type: "password", Secret: true, Required: required}
	}
	port := func(value int) DeviceField {
		minimum, maximum := 1.0, 65535.0
		return DeviceField{Key: "port", Label: "端口", Type: "number", Required: true, Default: float64(value), Min: &minimum, Max: &maximum}
	}
	account := func(value int) []DeviceField {
		return []DeviceField{port(value), text("username", "监控账号", true), secret("password", "监控密码", true)}
	}
	url := func(key, label string) DeviceField {
		return DeviceField{Key: key, Label: label, Type: "url", Required: true}
	}
	endpoints := func(key, label string) DeviceField {
		return DeviceField{Key: key, Label: label, Type: "endpoints", Required: true}
	}
	switch ident {
	case "MySQL":
		return account(3306), "mysql"
	case "SQLServer":
		return account(1433), "sqlserver"
	case "Oracle":
		return append(account(1521), text("service", "Service Name", true)), "oracle"
	case "PostgreSQL", "Greenplum":
		return append(account(5432), text("database", "连接数据库", false)), "pgsql"
	case "Dameng":
		return account(5236), "documentation"
	case "Redis":
		return []DeviceField{port(6379), text("username", "ACL 用户名（可选）", false), secret("password", "Redis 密码（可选）", false)}, "redis"
	case "Redfish":
		return []DeviceField{url("url", "BMC / iDRAC / iLO 地址"), text("username", "BMC 账号", true), secret("password", "BMC 密码", true)}, "redfish"
	case "IPMI":
		return []DeviceField{text("username", "IPMI 账号", true), secret("password", "IPMI 密码", true), {Key: "driver", Label: "IPMI 驱动", Type: "select", Required: true, Default: "LAN_2_0", Options: []string{"LAN", "LAN_2_0"}}}, "documentation"
	case "SNMP", "Switch_Legacy", "Windows (snmp)":
		return []DeviceField{port(161), {Key: "protocol", Label: "SNMP 版本", Type: "select", Required: true, Default: "snmp_v2c", Options: []string{"snmp_v2c"}}, secret("community", "团体字", true)}, "snmp_v2c"
	case "HTTP_Response":
		return []DeviceField{endpoints("targets", "HTTP / HTTPS 探测地址"), {Key: "method", Label: "HTTP 方法", Type: "select", Default: "GET", Required: true, Options: []string{"GET", "POST", "HEAD"}}}, "documentation"
	case "Nginx":
		return []DeviceField{endpoints("urls", "Nginx 状态页 URL"), text("username", "Basic Auth 用户名（可选）", false), secret("password", "Basic Auth 密码（可选）", false)}, "documentation"
	case "Kafka":
		return []DeviceField{endpoints("kafka_uris", "Broker 地址（host:port）"), {Key: "use_sasl", Label: "启用 SASL", Type: "switch", Default: false}, {Key: "sasl_username", Label: "SASL 用户名", Type: "text", Required: true, When: []FieldCondition{{Key: "use_sasl", Values: []string{"true"}}}}, {Key: "sasl_password", Label: "SASL 密码", Type: "password", Secret: true, Required: true, When: []FieldCondition{{Key: "use_sasl", Values: []string{"true"}}}}}, "documentation"
	case "Docker":
		f := text("endpoint", "Docker API / Socket", true)
		f.Default = "unix:///var/run/docker.sock"
		return []DeviceField{f}, "documentation"
	case "AliYun":
		return []DeviceField{text("region", "Region", true), text("endpoint", "云监控 Endpoint", true), text("access_key_id", "Access Key ID", true), secret("access_key_secret", "Access Key Secret", true), endpoints("namespaces", "监控产品 Namespaces")}, "documentation"
	case "CloudWatch":
		return []DeviceField{text("region", "AWS Region", true), text("profile", "采集器上的 AWS Profile（可选）", false), text("access_key", "Access Key（可选）", false), secret("secret_key", "Secret Key（可选）", false)}, "documentation"
	case "Ping":
		return []DeviceField{endpoints("targets", "Ping 目标")}, "documentation"
	case "Whois":
		return []DeviceField{endpoints("domains", "域名")}, "documentation"
	case "Prometheus", "cAdvisor":
		return []DeviceField{endpoints("urls", "指标端点 URL")}, "documentation"
	}
	if execution == "local" {
		return []DeviceField{{Key: "agent_ident", Label: "目标本机 Agent 标识", Type: "text", Required: true, Help: "此模板读取采集器本机资源；应先按采集说明在目标上安装 Agent，不能通过另一台采集器远程读取。"}}, "documentation"
	}
	return []DeviceField{}, "documentation"
}

func catalogOnboarding(entry CatalogEntry, categories []models.MonitorCategory) CatalogOnboarding {
	d := entry.Draft
	if entry.Published != nil {
		d = entry.Published.Definition
	}
	out := CatalogOnboarding{State: "unpublished", Execution: d.Execution, Issues: []string{}}
	if out.Execution == "" {
		out.Execution = "collector"
	}
	if entry.Disabled {
		out.Issues = append(out.Issues, "模板已停用")
		out.State = "disabled"
	}
	byID := map[string]models.MonitorCategory{}
	for _, c := range categories {
		byID[c.ID] = c
	}
	seen := map[string]bool{}
	for id := entry.CategoryID; id != ""; {
		c, ok := byID[id]
		if !ok || seen[id] {
			out.Issues = append(out.Issues, "分类路径不完整")
			break
		}
		seen[id] = true
		if c.Disabled {
			out.Issues = append(out.Issues, "分类已停用")
			out.State = "disabled"
			break
		}
		id = c.ParentID
	}
	if entry.Published == nil {
		out.Issues = append(out.Issues, "尚未发布监控包")
	}
	if len(d.Fields) == 0 {
		out.Issues = append(out.Issues, "需在模板中心配置设备表单")
	}
	if d.Adapter != "snmp_v2c" && d.Adapter != "mysql" {
		out.Issues = append(out.Issues, "此采集方式尚未接通自动下发；请参考原采集说明")
	} else if strings.TrimSpace(d.Config) == "" {
		out.Issues = append(out.Issues, "缺少可执行采集定义")
	}
	if d.Pack == nil || strings.TrimSpace(d.Pack.Readme) == "" {
		out.Issues = append(out.Issues, "缺少采集说明")
	}
	if d.Pack == nil || len(d.Pack.Metrics) == 0 {
		out.Issues = append(out.Issues, "缺少指标说明与契约")
	}
	if d.Pack == nil || len(d.Pack.Dashboards) == 0 {
		out.Issues = append(out.Issues, "缺少仪表盘模板")
	}
	if d.Pack == nil || len(d.Pack.Alerts) == 0 {
		out.Issues = append(out.Issues, "缺少告警规则模板")
	}
	out.Ready = entry.Published != nil && len(out.Issues) == 0
	if out.Ready {
		out.State = "ready"
	}
	return out
}
