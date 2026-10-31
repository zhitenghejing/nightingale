package collection

import (
	"fmt"
	"net/netip"
	"sort"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/pelletier/go-toml/v2"
	"gorm.io/gorm"
)

// ICMP is a common baseline for every device template, independently of the
// protocol-specific metric contract. Old immutable packs inherit this default.
const DefaultReachability = "icmp"

// AgentPingOwner is derived from native targets and collector authorization;
// it does not create a second managed device or change Agent collection inputs.
type AgentPingOwner struct {
	Ident, Address, CollectorID, Revision string
	DatasourceID                          int64
}

// AgentPingOwners prefers a host's registered collector. Other native Agents
// use one deterministic collector authorized for their business group. A host
// without either association has no implicit cross-group probing permission.
// A nil ident list is used by publishing; UI and ingestion pass their own batch.
func AgentPingOwners(db *gorm.DB, idents []string) (map[string]AgentPingOwner, error) {
	out := map[string]AgentPingOwner{}
	if idents != nil && len(idents) == 0 {
		return out, nil
	}
	var targets []models.Target
	query := db.Select("ident", "host_ip").Where("agent_version <> ?", "").Where("ident NOT IN (?)", db.Model(&models.MonitorDevice{}).Select("ident"))
	if idents != nil {
		query = query.Where("ident IN ?", idents)
	}
	if err := query.Find(&targets).Error; err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return out, nil
	}
	var collectors []models.MonitorCollector
	if err := db.Where("enabled = ?", true).Order("id").Find(&collectors).Error; err != nil {
		return nil, err
	}
	var permissions []models.MonitorCollectorGroup
	if err := db.Find(&permissions).Error; err != nil {
		return nil, err
	}
	allowed := map[string]map[int64]bool{}
	for _, p := range permissions {
		if allowed[p.CollectorID] == nil {
			allowed[p.CollectorID] = map[int64]bool{}
		}
		allowed[p.CollectorID][p.GroupID] = true
	}
	keys := make([]string, 0, len(targets))
	for _, target := range targets {
		keys = append(keys, target.Ident)
	}
	var groups []models.TargetBusiGroup
	if err := db.Where("target_ident IN ?", keys).Find(&groups).Error; err != nil {
		return nil, err
	}
	byIdent := map[string][]int64{}
	for _, group := range groups {
		byIdent[group.TargetIdent] = append(byIdent[group.TargetIdent], group.GroupId)
	}
	for _, target := range targets {
		address, err := netip.ParseAddr(target.HostIp)
		if err != nil || address.Zone() != "" {
			continue
		}
		var selected *models.MonitorCollector
		for i := range collectors {
			if collectors[i].AgentIdent == target.Ident {
				selected = &collectors[i]
				break
			}
		}
		if selected == nil {
			for i := range collectors {
				for _, group := range byIdent[target.Ident] {
					if allowed[collectors[i].ID][group] {
						selected = &collectors[i]
						break
					}
				}
				if selected != nil {
					break
				}
			}
		}
		if selected != nil {
			owner := AgentPingOwner{Ident: target.Ident, Address: address.String(), CollectorID: selected.ID, DatasourceID: selected.DatasourceID}
			owner.Revision = digest(canonical([]string{"agent-icmp-v1", owner.CollectorID, owner.Ident, owner.Address}))
			out[target.Ident] = owner
		}
	}
	return out, nil
}

func agentPingConfigs(db *gorm.DB, collector string) (map[string]ConfigContent, error) {
	owners, err := AgentPingOwners(db, nil)
	if err != nil {
		return nil, err
	}
	out := map[string]ConfigContent{}
	for _, owner := range owners {
		if owner.CollectorID != collector {
			continue
		}
		content, err := encodePing(DeviceInput{Address: owner.Address}, map[string]string{"ident": owner.Ident, "collection_collector_id": collector, "collection_revision": owner.Revision, "collection_mode": "agent"})
		if err != nil {
			return nil, err
		}
		out[digest([]byte(content.Config))] = content
	}
	return out, nil
}

func agentPingManifest(configs map[string]ConfigContent) []string {
	out := []string{}
	for hash := range configs {
		out = append(out, "agent-ping:"+hash)
	}
	sort.Strings(out)
	return out
}

type pingInstance struct {
	Targets      []string          `toml:"targets"`
	Count        int               `toml:"count"`
	PingInterval float64           `toml:"ping_interval"`
	Timeout      float64           `toml:"timeout"`
	Method       string            `toml:"method"`
	IPv6         bool              `toml:"ipv6"`
	Labels       map[string]string `toml:"labels"`
}
type pingConfig struct {
	Interval  int            `toml:"interval"`
	Instances []pingInstance `toml:"instances"`
}

func compilePing(f DeviceInput, id, revision string) (ConfigContent, error) {
	labels := map[string]string{}
	for k, v := range f.Labels {
		labels[k] = v
	}
	labels["ident"] = f.Ident
	labels["collection_collector_id"] = f.CollectorID
	labels["collection_device_id"] = id
	labels["collection_revision"] = revision
	labels["collection_mode"] = "managed"
	labels["collection_template_version"] = f.TemplateVersionID
	return encodePing(f, labels)
}

func encodePing(f DeviceInput, labels map[string]string) (ConfigContent, error) {
	address, err := netip.ParseAddr(f.Address)
	if err != nil || address.Zone() != "" {
		return ConfigContent{}, bad("invalid ICMP address")
	}
	labels["target"] = address.String()
	interval := f.IntervalSeconds
	if interval <= 0 {
		interval = 30
	}
	// A separate native task keeps failed/blocked ICMP from blocking SNMP/MySQL.
	data, err := toml.Marshal(pingConfig{Interval: interval, Instances: []pingInstance{{Targets: []string{address.String()}, Count: 3, PingInterval: 0.2, Timeout: 3, Method: "native", IPv6: address.Is6(), Labels: labels}}})
	if err != nil {
		return ConfigContent{}, fmt.Errorf("encode ICMP configuration: %w", err)
	}
	return ConfigContent{Format: "toml", Config: string(data)}, nil
}
func pingMetric(name string) bool {
	switch name {
	case "ping_result_code", "ping_percent_packet_loss", "ping_minimum_response_ms", "ping_average_response_ms", "ping_maximum_response_ms", "ping_standard_deviation_ms", "ping_ttl":
		return true
	default:
		return false
	}
}

// SyncReachability adds baseline tasks to existing enabled devices without
// rebinding their immutable packs, changing revisions, credentials or resources.
// Content digests make repeated starts idempotent.
func (s *Store) SyncReachability() error {
	var collectors []models.MonitorCollector
	if err := s.DB.Where("enabled = ?", true).Order("id").Find(&collectors).Error; err != nil {
		return err
	}
	for _, collector := range collectors {
		if err := s.write(func(tx *gorm.DB) error {
			current, err := s.collector(tx, collector.ID)
			if err != nil {
				return err
			}
			var publication models.MonitorPublication
			if err = tx.First(&publication, "collector_id = ? AND version = ?", current.ID, current.DesiredVersion).Error; err != nil {
				return err
			}
			if publication.Reason == "collector_withdrawal" {
				return nil
			}
			return s.publish(tx, &current, Scope{Admin: true}, "default_icmp")
		}); err != nil {
			return err
		}
	}
	return nil
}

// Ping sidecars carry no primary-device restoration input. Validate their
// integrity and ownership, then let rollback regenerate them from restored data.
func validateHistoricalPing(entries map[string]ConfigContent, collector string) error {
	for hash, content := range entries {
		if content.Format != "toml" || digest([]byte(content.Config)) != hash {
			return bad("historical content integrity failed")
		}
		var cfg pingConfig
		if toml.Unmarshal([]byte(content.Config), &cfg) != nil || len(cfg.Instances) != 1 {
			return bad("historical ICMP configuration is invalid")
		}
		instance := cfg.Instances[0]
		mode := instance.Labels["collection_mode"]
		validOwner := mode == "managed" && instance.Labels["collection_device_id"] != "" || mode == "agent" && instance.Labels["collection_device_id"] == "" && instance.Labels["ident"] != "" && instance.Labels["collection_revision"] != ""
		if instance.Method != "native" || len(instance.Targets) != 1 || instance.Labels["collection_collector_id"] != collector || !validOwner || instance.Labels["probe_id"] != "" {
			return bad("historical ICMP ownership is invalid")
		}
		address, err := netip.ParseAddr(instance.Targets[0])
		if err != nil || address.Zone() != "" || instance.Labels["target"] != address.String() {
			return bad("historical ICMP address is invalid")
		}
	}
	return nil
}
