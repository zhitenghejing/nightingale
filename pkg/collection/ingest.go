package collection

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/prometheus/prometheus/prompb"
	"gorm.io/gorm"
)

// FilterSamples authenticates ownership before enrichment or metric-driven registration.
// Rejected old samples still advance the withdrawal observation, without storing values or secrets.
func (s *Store) FilterSamples(writer string, series []prompb.TimeSeries) ([]prompb.TimeSeries, error) {
	out := make([]prompb.TimeSeries, 0, len(series))
	err := s.write(func(tx *gorm.DB) error {
		deviceIDs, collectorIDs, probeIDs, idents, agentIdents := []string{}, []string{}, []string{}, []string{}, []string{}
		for _, ts := range series {
			agentPing, ident := false, ""
			for _, label := range ts.Labels {
				switch label.Name {
				case "collection_device_id":
					deviceIDs = append(deviceIDs, label.Value)
				case "collection_collector_id":
					collectorIDs = append(collectorIDs, label.Value)
				case "probe_id":
					probeIDs = append(probeIDs, label.Value)
				case "ident":
					idents = append(idents, label.Value)
					ident = label.Value
				case "collection_mode":
					agentPing = label.Value == "agent"
				}
			}
			if agentPing {
				agentIdents = append(agentIdents, ident)
			}
		}
		agentOwners, err := AgentPingOwners(tx, sortedUnique(agentIdents))
		if err != nil {
			return err
		}
		var collectors []models.MonitorCollector
		if err := tx.Where("id IN ?", sortedUnique(collectorIDs)).Find(&collectors).Error; err != nil {
			return err
		}
		byID := map[string]models.MonitorCollector{}
		for _, c := range collectors {
			byID[c.ID] = c
		}
		var devices []models.MonitorDevice
		if err := tx.Where("id IN ? OR ident IN ?", sortedUnique(deviceIDs), sortedUnique(idents)).Find(&devices).Error; err != nil {
			return err
		}
		byDevice := map[string]models.MonitorDevice{}
		byIdent := map[string]bool{}
		for _, d := range devices {
			byDevice[d.ID] = d
			byIdent[d.Ident] = true
		}
		var probes []models.MonitorProbe
		if err := tx.Where("id IN ? AND state <> ?", sortedUnique(probeIDs), "cleaned").Find(&probes).Error; err != nil {
			return err
		}
		byProbe := map[string]models.MonitorProbe{}
		for _, p := range probes {
			byProbe[p.ID] = p
		}
		received := map[string]bool{}
		clockErrors := map[string]bool{}
		rejected := map[string]bool{}
		probeReceived := map[string]string{}
		for _, ts := range series {
			labels := map[string]string{}
			reserved := false
			valid := true
			for _, l := range ts.Labels {
				if _, ok := labels[l.Name]; ok {
					valid = false
				}
				if !utf8.ValidString(l.Value) {
					valid = false
				}
				labels[l.Name] = l.Value
				if strings.HasPrefix(l.Name, "collection_") || l.Name == "probe_id" {
					reserved = true
				}
			}
			if !valid {
				continue
			}
			if !reserved {
				if !byIdent[labels["ident"]] {
					out = append(out, ts)
				}
				continue
			}
			if len(ts.Samples) == 0 {
				continue
			}
			collector, ok := byID[labels["collection_collector_id"]]
			if !ok || collector.WriterUser != writer || !collector.Enabled {
				continue
			}
			name := labels["__name__"]
			isPing := pingMetric(name)
			if !isPing && !strings.HasPrefix(name, "snmp_") && !strings.HasPrefix(name, "mysql_") {
				continue
			}
			if labels["collection_mode"] == "agent" {
				owner, ok := agentOwners[labels["ident"]]
				if ok && isPing && owner.CollectorID == collector.ID && owner.Revision == labels["collection_revision"] && owner.Address == labels["target"] && labels["collection_device_id"] == "" && labels["probe_id"] == "" {
					out = append(out, ts)
				}
				continue
			} else if labels["collection_mode"] == "managed" {
				d, ok := byDevice[labels["collection_device_id"]]
				if !ok {
					continue
				}
				if !isPing && (d.Protocol == "mysql") != strings.HasPrefix(name, "mysql_") {
					continue
				}
				// Even a previously assigned collector's rejected samples prevent a premature withdrawal observation.
				if d.CollectorID != collector.ID || d.Deleted || !d.Enabled || d.DesiredRevision != labels["collection_revision"] || d.Ident != labels["ident"] || labels["probe_id"] != "" {
					if d.CollectorID == collector.ID {
						rejected[d.ID] = true
					} else {
						var previous int64
						if err := tx.Model(&models.MonitorPublication{}).Where("collector_id = ? AND manifest LIKE ?", collector.ID, "%"+d.ID+":"+labels["collection_revision"]+"%").Count(&previous).Error; err != nil {
							return err
						}
						if previous > 0 {
							rejected[d.ID] = true
						}
					}
					continue
				}
				if labels["snmp_target"] != "" && labels["snmp_target"] != d.Address {
					rejected[d.ID] = true
					continue
				}
				if isPing {
					if labels["target"] == d.Address {
						out = append(out, ts)
					}
					continue
				}
				received[d.ID] = true
				for _, sample := range ts.Samples {
					future := s.FutureSeconds
					if future <= 0 {
						future = 30
					}
					if sample.Timestamp > time.Now().Add(time.Duration(future)*time.Second).UnixMilli() {
						clockErrors[d.ID] = true
					}
				}
				out = append(out, ts)
			} else if labels["collection_mode"] == "probe" {
				if isPing {
					continue
				}
				p, ok := byProbe[labels["probe_id"]]
				if !ok || p.CollectorID != collector.ID || p.Revision != labels["collection_revision"] || labels["collection_device_id"] != "" {
					continue
				}
				result := ""
				if p.CleanupAt == 0 && p.Deadline > time.Now().Unix() {
					out = append(out, ts)
					if name == "snmp_up" || name == "mysql_up" {
						for _, sample := range ts.Samples {
							future := s.FutureSeconds
							if future <= 0 {
								future = 30
							}
							if sample.Timestamp > time.Now().Add(time.Duration(future)*time.Second).UnixMilli() || sample.Timestamp < time.Now().Add(-90*time.Second).UnixMilli() {
								continue
							}
							if sample.Value == 1 {
								result = "passed"
							} else if sample.Value == 0 && result == "" {
								result = "failed"
							}
						}
					}
				}
				if result != "" || probeReceived[p.ID] == "" {
					probeReceived[p.ID] = result
				}
			}
		}
		now := time.Now().Unix()
		for id := range clockErrors {
			if err := tx.Model(&models.MonitorDevice{}).Where("id = ?", id).Update("last_clock_error_at", now).Error; err != nil {
				return err
			}
		}
		for id := range received {
			if err := tx.Model(&models.MonitorDevice{}).Where("id = ?", id).Update("last_received_at", now).Error; err != nil {
				return err
			}
		}
		for id := range rejected {
			if err := tx.Model(&models.MonitorDevice{}).Where("id = ?", id).Update("last_rejected_at", now).Error; err != nil {
				return err
			}
		}
		for id, result := range probeReceived {
			updates := map[string]interface{}{"last_received_at": now}
			if result != "" {
				updates["result"] = result
				updates["state"] = "completed"
			}
			if err := tx.Model(&models.MonitorProbe{}).Where("id = ?", id).Updates(updates).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}
