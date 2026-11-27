package collection

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ccfos/nightingale/v6/integrations"
	"github.com/ccfos/nightingale/v6/models"
)

const linuxARMSNMPIdent = "linux_arm_snmp"
const linuxARMAgentIdent = "linux_arm_categraf"
const linuxX86AgentIdent = "linux_x86_categraf"

func linuxARMSNMP() (CatalogDefinition, error) {
	// Net-SNMP exposes these MIBs on either architecture. Use the same OIDs
	// and expressions with a separate template identity and documentation.
	definition, err := linuxX86SNMP()
	if err != nil {
		return CatalogDefinition{}, err
	}
	definition.Compatibility = "适用于提供 Net-SNMP、HOST-RESOURCES-MIB、UCD-SNMP-MIB 及 UCD-DISKIO-MIB 的 Linux ARM（含 ARM64）。具体指标取决于目标系统、内核和 snmpd 视图。"
	definition.Pack.Readme = `# Linux ARM SNMP 采集

在 Linux ARM 目标主机启用 Net-SNMP，配置只读 SNMP v2c，并允许指定采集器访问 UDP 161。新增设备时选择可访问该主机的采集器，填写目标地址和只读团体字。建议每 30 秒采集，超时 3 秒，重试 1 次。

采集定义与 Linux X86 模板共享 Net-SNMP OID，覆盖 CPU 使用率与核数、物理内存、Swap、1/5/15 分钟负载、磁盘 I/O 和运行时间。有效内存用量扣除 buffers 和 cache。磁盘速率需要至少两个有效样本。目标没有 Swap 或未开放某组 OID 时，对应图表可能无数据。部署前请用实际 ARM 主机核对 snmpd 视图和核心指标。

内置告警覆盖 CPU、有效内存、Swap、负载和 SNMP 样本缺失，默认仅记录事件。
`
	for i := range definition.Pack.Metrics {
		definition.Pack.Metrics[i].Typ = linuxARMSNMPIdent
	}
	for i := range definition.Pack.Dashboards {
		board := &definition.Pack.Dashboards[i]
		board.Content = strings.ReplaceAll(board.Content, board.Name, "Linux ARM SNMP 总览")
		board.Name = "Linux ARM SNMP 总览"
	}
	return definition, nil
}

func linuxARMAgent() (CatalogDefinition, error) {
	return linuxAgent(linuxARMAgentIdent, "ARM", "ARM64（aarch64）请选择 Linux ARM64 构建，其他 ARM 环境需确认二进制与系统兼容。")
}

func linuxX86Agent() (CatalogDefinition, error) {
	return linuxAgent(linuxX86AgentIdent, "X86", "64 位 x86 主机（x86_64）请选择 Linux AMD64 构建；32 位 x86 主机需确认所用 Categraf 版本提供兼容构建。")
}

func linuxAgent(ident, architecture, install string) (CatalogDefinition, error) {
	readme, err := integrations.LinuxCategraf.ReadFile("Linux/markdown/README.md")
	if err != nil {
		return CatalogDefinition{}, err
	}
	pack := &ProfileDefinition{
		Name: "Agent采集",
		Readme: fmt.Sprintf(`# Linux %s Agent 采集

在目标 Linux 主机上运行与架构匹配的 Categraf。%s配置本环境的心跳和指标写入地址及认证，启动后在设备列表确认主机出现，并按实际 ident 核对 CPU、内存和磁盘指标。

在数据到达后，可将本模板的仪表盘和告警按需导入对应业务组，并选择 Agent 指标实际写入的 Prometheus 数据源。部分指标需要按目标内核和插件配置开启。以下是 Linux Categraf 插件配置参考。

`, architecture, install) + strings.TrimPrefix(string(readme), "# Linux\n\n"),
	}
	metrics, err := integrations.LinuxCategraf.ReadFile("Linux/metrics/categraf-base.json")
	if err != nil {
		return CatalogDefinition{}, err
	}
	if err := json.Unmarshal(metrics, &pack.Metrics); err != nil {
		return CatalogDefinition{}, err
	}
	for i := range pack.Metrics {
		pack.Metrics[i].Typ = ident
	}
	dashboard, err := integrations.LinuxCategraf.ReadFile("Linux/dashboards/categraf-detail.json")
	if err != nil {
		return CatalogDefinition{}, err
	}
	pack.Dashboards = []models.BuiltinPayload{{Name: "Linux " + architecture + " Agent 总览", Content: string(dashboard)}}

	alerts, err := integrations.LinuxCategraf.ReadFile("Linux/alerts/linux_by_categraf.json")
	if err != nil {
		return CatalogDefinition{}, err
	}
	var rules []models.AlertRule
	if err := json.Unmarshal(alerts, &rules); err != nil {
		return CatalogDefinition{}, err
	}
	for _, rule := range rules {
		rule.NotifyGroupsJSON = nil
		rule.NotifyChannelsJSON = nil
		rule.NotifyRuleIds = nil
		rule.CallbacksJSON = nil
		rule.PipelineConfigs = nil
		content, err := json.Marshal(rule)
		if err != nil {
			return CatalogDefinition{}, fmt.Errorf("encode Agent alert %q: %w", rule.Name, err)
		}
		pack.Alerts = append(pack.Alerts, models.BuiltinPayload{Name: rule.Name, Content: string(content)})
	}
	fields, adapter := onboardingFields("Linux", "local")
	return CatalogDefinition{
		Adapter: adapter, Fields: fields, Execution: "local", Notification: "events_only", Pack: pack,
		Compatibility: "在目标 Linux " + architecture + " 主机上手动部署相应架构的 Categraf，使用本机采集插件和现有 Agent 上报链路。",
	}, nil
}
