package collection

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/pelletier/go-toml/v2"
)

const WindowsTemplate = "windows_basic_v1"

type DeviceInput struct {
	ProbeDeviceID       string                 `json:"probe_device_id,omitempty"`
	Parameters          map[string]interface{} `json:"parameters,omitempty"`
	Notify              *NotificationPreset    `json:"notify,omitempty"`
	TemplateComponentID uint64                 `json:"template_component_id"`
	ResetResources      bool                   `json:"reset_resources"`
	UpgradeResolution   string                 `json:"upgrade_resolution,omitempty"`
	RefreshTemplate     bool                   `json:"refresh_template"`
	Customization       *ProfileCustomization  `json:"customization"`
	Name                string                 `json:"name"`
	Address             string                 `json:"address"`
	Port                int                    `json:"port"`
	Ident               string                 `json:"ident"`
	GroupID             int64                  `json:"group_id"`
	CollectorID         string                 `json:"collector_id"`
	CredentialID        string                 `json:"credential_id"`
	TemplateVersionID   string                 `json:"template_version_id"`
	Protocol            string                 `json:"protocol"`
	IntervalSeconds     int                    `json:"interval_seconds"`
	TimeoutSeconds      int                    `json:"timeout_seconds"`
	Retries             int                    `json:"retries"`
	Labels              map[string]string      `json:"labels"`
	CMDBID              string                 `json:"cmdb_id"`
	Enabled             bool                   `json:"enabled"`
	RecordVersion       int64                  `json:"record_version"`
}

var labelName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func reservedLabel(k string) bool {
	return k == "device_name" || k == "host" || k == "type" || k == "ident" || k == "snmp_target" || k == "index" || k == "hostname" || k == "agent_hostname" || k == "probe_id" || strings.HasPrefix(k, "collection_") || strings.HasPrefix(k, "__")
}
func (f *DeviceInput) normalize() error {
	f.Name = strings.TrimSpace(f.Name)
	if !utf8.ValidString(f.Name) || len([]rune(f.Name)) < 1 || len([]rune(f.Name)) > 128 {
		return errors.New("name must contain 1 to 128 characters")
	}
	f.Address = strings.TrimSpace(f.Address)
	a, err := netip.ParseAddr(f.Address)
	if err != nil || a.Zone() != "" {
		return errors.New("address must be a valid IPv4 or IPv6 address")
	}
	f.Address = a.String()
	if f.Port == 0 {
		f.Port = 161
	}
	if f.Port < 1 || f.Port > 65535 {
		return errors.New("invalid collection port")
	}
	if f.Ident == "" {
		f.Ident = f.Address
	}
	if len(f.Ident) > 191 || !utf8.ValidString(f.Ident) || strings.ContainsAny(f.Ident, "\x00\r\n\t ") {
		return errors.New("invalid target ident")
	}
	if f.GroupID <= 0 || f.CollectorID == "" || (f.CredentialID == "" && f.Parameters == nil) {
		return errors.New("group, collector and credential are required")
	}
	if f.Protocol == "" {
		f.Protocol = "snmp_v2c"
	}
	if f.Protocol != "snmp_v2c" && f.Protocol != "mysql" {
		return errors.New("collection adapter is not executable")
	}
	if f.TemplateVersionID == "" {
		f.TemplateVersionID = WindowsTemplate
	}
	if len(f.TemplateVersionID) > 64 {
		return errors.New("unknown immutable template version")
	}
	if f.IntervalSeconds == 0 {
		f.IntervalSeconds = 30
	}
	switch f.IntervalSeconds {
	case 15, 30, 60, 120:
	default:
		return errors.New("interval must be 15, 30, 60 or 120 seconds")
	}
	if f.TimeoutSeconds == 0 {
		f.TimeoutSeconds = 3
	}
	maxTimeout := 10
	if f.Protocol == "mysql" {
		maxTimeout = 60
	}
	if f.TimeoutSeconds < 1 || f.TimeoutSeconds > maxTimeout || f.Retries < 0 || f.Retries > 3 {
		return errors.New("invalid timeout or retries")
	}
	if f.Labels == nil {
		f.Labels = map[string]string{}
	}
	if len(f.Labels) > 20 {
		return errors.New("at most 20 custom labels are allowed")
	}
	for k, v := range f.Labels {
		if !labelName.MatchString(k) || len(k) > 64 || reservedLabel(k) || len(v) > 256 || !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return errors.New("invalid or reserved label")
		}
	}
	return nil
}

type snmpField struct {
	Name  string `toml:"name"`
	OID   string `toml:"oid"`
	IsTag bool   `toml:"is_tag,omitempty"`
	// Omit from old JSON snapshots to preserve their immutable digest.
	Conversion string `toml:"conversion,omitempty" json:"Conversion,omitempty"`
}
type snmpTable struct {
	Name        string      `toml:"name"`
	IndexAsTag  bool        `toml:"index_as_tag"`
	ErrorPolicy string      `toml:"error_policy"`
	Fields      []snmpField `toml:"field"`
}
type snmpInstance struct {
	Agents       []string          `toml:"agents"`
	Version      int               `toml:"version"`
	Community    string            `toml:"community"`
	Timeout      string            `toml:"timeout"`
	Retries      int               `toml:"retries"`
	AgentHostTag string            `toml:"agent_host_tag"`
	DisableICMP  bool              `toml:"disable_icmp_up"`
	Labels       map[string]string `toml:"labels"`
	Fields       []snmpField       `toml:"field"`
	Tables       []snmpTable       `toml:"table,omitempty"`
}
type snmpConfig struct {
	Interval  int            `toml:"interval"`
	Instances []snmpInstance `toml:"instances"`
}
type ConfigContent struct {
	Format string `json:"format"`
	Config string `json:"config"`
}
type Snapshot struct {
	Version string                              `json:"version"`
	Configs map[string]map[string]ConfigContent `json:"configs,omitempty"`
}

// FullSnapshot always emits configs, including the authoritative empty set.
type FullSnapshot struct {
	Version string                              `json:"version"`
	Configs map[string]map[string]ConfigContent `json:"configs"`
}

func windowsTables() []snmpTable {
	table := func(name string, fields ...snmpField) snmpTable { return snmpTable{name, true, "partial", fields} }
	field := func(name, oid string) snmpField { return snmpField{Name: name, OID: ".1.3.6.1.2.1." + oid} }
	return []snmpTable{
		table("cpu", field("load", "25.3.3.1.2")),
		table("storage", snmpField{Name: "type", OID: ".1.3.6.1.2.1.25.2.3.1.2", IsTag: true}, field("allocation_units", "25.2.3.1.4"), field("size", "25.2.3.1.5"), field("used", "25.2.3.1.6")),
		table("interface", field("type", "2.2.1.3"), field("oper_status", "2.2.1.8"), field("admin_status", "2.2.1.7")),
		table("traffic64", field("in_octets", "31.1.1.1.6"), field("out_octets", "31.1.1.1.10")),
		table("traffic32", field("in_octets", "2.2.1.10"), field("out_octets", "2.2.1.16")),
	}
}
func compile(f DeviceInput, id, revision, community, mode string, templates ...TemplateConfig) (ConfigContent, error) {
	labels := map[string]string{}
	for k, v := range f.Labels {
		labels[k] = v
	}
	labels["ident"] = f.Ident
	labels["collection_collector_id"] = f.CollectorID
	labels["collection_revision"] = revision
	labels["collection_mode"] = mode
	if mode == "probe" {
		labels["probe_id"] = id
	} else {
		labels["collection_device_id"] = id
	}
	ins := snmpInstance{Agents: []string{"udp://" + net.JoinHostPort(f.Address, fmt.Sprint(f.Port))}, Version: 2, Community: community, Timeout: fmt.Sprintf("%ds", f.TimeoutSeconds), Retries: f.Retries, AgentHostTag: "snmp_target", DisableICMP: true, Labels: labels, Fields: []snmpField{{Name: "uptime", OID: ".1.3.6.1.2.1.1.3.0"}}}
	if mode == "managed" {
		if len(templates) != 1 {
			return ConfigContent{}, errors.New("collection template is required")
		}
		ins.Fields = append(ins.Fields, templates[0].Fields...)
		disabled := map[string]bool{}
		for _, name := range templates[0].DisabledTables {
			disabled[name] = true
		}
		for _, table := range templates[0].Tables {
			if !disabled[table.Name] {
				ins.Tables = append(ins.Tables, table)
			}
		}
		// The original built-in version is implicit in historical snapshots.
		// Preserve its bytes so deploying template editing does not replace
		// otherwise unchanged running tasks.
		if f.TemplateVersionID != WindowsTemplate {
			labels["collection_template_version"] = f.TemplateVersionID
		}
	}
	b, err := toml.Marshal(snmpConfig{f.IntervalSeconds, []snmpInstance{ins}})
	if err != nil {
		return ConfigContent{}, errors.New("SNMP configuration encoding failed")
	}
	var check snmpConfig
	if toml.Unmarshal(b, &check) != nil {
		return ConfigContent{}, errors.New("SNMP configuration validation failed")
	}
	return ConfigContent{"toml", string(b)}, nil
}
func templateDefinition() models.MonitorTemplateVersion {
	b, _ := json.Marshal(map[string]interface{}{"tables": windowsTables(), "required_metrics": []string{"snmp_uptime", "snmp_cpu_load", "snmp_storage_size", "snmp_storage_used"}, "optional_metrics": []string{"snmp_traffic64_in_octets", "snmp_traffic64_out_octets", "snmp_traffic32_in_octets", "snmp_traffic32_out_octets"}, "description": "Storage identified by HOST-RESOURCES-MIB type and index; raw non-UTF-8 descriptions are excluded. 64-bit counters preferred, 32-bit fallback retained separately."})
	return models.MonitorTemplateVersion{ID: WindowsTemplate, TemplateKey: "windows_basic", Version: 1, Name: "Windows SNMP basic", Definition: models.MonitorText(b), Digest: digest(b)}
}
func inputFromDevice(d models.MonitorDevice) DeviceInput {
	f := DeviceInput{Name: d.Name, Address: d.Address, Port: d.Port, Ident: d.Ident, GroupID: d.GroupID, CollectorID: d.CollectorID, CredentialID: d.CredentialID, TemplateVersionID: d.TemplateVersionID, Protocol: d.Protocol, IntervalSeconds: d.IntervalSeconds, TimeoutSeconds: d.TimeoutSeconds, Retries: d.Retries, CMDBID: d.CMDBID, Enabled: d.Enabled, RecordVersion: d.RecordVersion}
	_ = json.Unmarshal([]byte(d.Labels), &f.Labels)
	_ = json.Unmarshal([]byte(d.Parameters), &f.Parameters)
	_ = json.Unmarshal([]byte(d.Notify), &f.Notify)
	return f
}
func inputDevice(f DeviceInput) models.MonitorDevice {
	b, _ := json.Marshal(f.Labels)
	return models.MonitorDevice{Name: f.Name, Address: f.Address, Port: f.Port, Ident: f.Ident, GroupID: f.GroupID, CollectorID: f.CollectorID, CredentialID: f.CredentialID, TemplateVersionID: f.TemplateVersionID, Protocol: f.Protocol, IntervalSeconds: f.IntervalSeconds, TimeoutSeconds: f.TimeoutSeconds, Retries: f.Retries, Labels: models.MonitorText(b), CMDBID: f.CMDBID, Enabled: f.Enabled, Parameters: models.MonitorText(canonical(f.Parameters)), Notify: models.MonitorText(canonical(f.Notify))}
}
func canonical(v interface{}) []byte { b, _ := json.Marshal(v); return b }
func sortedUnique(values []string) []string {
	sort.Strings(values)
	out := []string{}
	for _, v := range values {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}
