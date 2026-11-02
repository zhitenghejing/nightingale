package collection

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func endpointKey(owner string, f DeviceInput) string {
	return digest(canonical([]interface{}{owner, f.Address, f.Port, f.Protocol}))
}
func (s *Store) validateDevice(tx *gorm.DB, scope Scope, f DeviceInput, c models.MonitorCollector) error {
	if err := scope.Check(f.GroupID, true); err != nil {
		return err
	}
	if err := groupExists(tx, f.GroupID); err != nil {
		return err
	}
	if _, err := loadTemplate(tx, f.TemplateVersionID); err != nil {
		return err
	}
	if !c.Enabled {
		return conflict("collector is disabled")
	}
	var n int64
	if err := tx.Model(&models.MonitorCollectorGroup{}).Where("collector_id = ? AND group_id = ?", c.ID, f.GroupID).Count(&n).Error; err != nil {
		return err
	}
	if n != 1 {
		return denied()
	}
	cred, err := s.credential(tx, f.CredentialID, scope, f.GroupID)
	if err != nil {
		return err
	}
	if cred.Kind != f.Protocol && cred.Kind != "parameters:"+f.Protocol {
		return bad("credential protocol mismatch")
	}

	return nil
}
func configFingerprint(f DeviceInput) string {
	f.Name = ""
	f.CMDBID = ""
	f.RecordVersion = 0
	f.UpgradeResolution = ""
	f.Notify = nil
	return digest(canonical(f))
}
func (s *Store) SaveDevice(scope Scope, id, key string, f DeviceInput) (models.MonitorDevice, error) {
	f.ProbeDeviceID = ""
	var out models.MonitorDevice
	if err := inputDefaults(s.DB, &f); err != nil {
		return out, err
	}
	if err := f.normalize(); err != nil {
		return out, bad(err.Error())
	}
	if err := scope.Check(f.GroupID, true); err != nil {
		return out, err
	}
	requestHash := digest(canonical(f))
	requestKey := ""
	if id == "" {
		if len(key) < 8 || len(key) > 128 {
			return out, bad("Idempotency-Key of 8 to 128 characters is required")
		}
		requestKey = digest(canonical([]interface{}{scope.ActorID, key}))
	}
	err := s.write(func(tx *gorm.DB) error {
		if err := groupExists(tx, f.GroupID); err != nil {
			return err
		}
		var old models.MonitorDevice
		if id != "" {
			if err := tx.First(&old, "id = ?", id).Error; err != nil {
				return missing()
			}
			if err := scope.Check(old.GroupID, true); err != nil {
				return err
			}
			if old.Deleted {
				return conflict("device is deleted")
			}
		}
		initialCollector := old.CollectorID
		ids := []string{f.CollectorID}
		if old.ID != "" {
			ids = append(ids, old.CollectorID)
		}
		collectors := map[string]models.MonitorCollector{}
		for _, cid := range sortedUnique(ids) {
			c, err := s.collector(tx, cid)
			if err != nil {
				return err
			}
			collectors[cid] = c
		}
		c := collectors[f.CollectorID]
		if id == "" {
			var existing models.MonitorDevice
			err := tx.First(&existing, "request_key = ?", requestKey).Error
			if err == nil {
				if err = scope.Check(existing.GroupID, true); err != nil {
					return err
				}
				if existing.RequestHash != requestHash {
					return conflict("Idempotency-Key was already used for another request")
				}
				out = existing
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if id != "" {
			if err := tx.First(&old, "id = ?", id).Error; err != nil {
				return err
			}
			if old.CollectorID != initialCollector {
				return conflict("record version conflict")
			}
			if old.RecordVersion != f.RecordVersion {
				return conflict("record version conflict")
			}
			if old.Ident != f.Ident {
				return conflict("target ident is immutable")
			}
			if old.CollectorID != f.CollectorID {
				previous := collectors[old.CollectorID]
				if old.Enabled || !withdrawn(old, previous, time.Now().Unix()) {
					return conflict("withdraw the old collector and wait for the observation window before reassignment")
				}
			}
		}
		applyProfile, err := resolveDeviceProfile(tx, &f, old)
		if err != nil {
			return err
		}
		if err := s.prepareParameters(tx, scope, &f, old); err != nil {
			return err
		}
		if err := s.prepareNotification(tx, scope, &f, old); err != nil {
			return err
		}
		if err := s.validateDevice(tx, scope, f, c); err != nil {
			return err
		}
		if f.CMDBID != "" && (old.ID == "" || old.CMDBID != f.CMDBID) {
			if err := validateAsset(tx, scope, f.CMDBID); err != nil {
				return err
			}
		}
		out = inputDevice(f)
		out.GroupEndpoint = endpointKey(fmt.Sprint(f.GroupID), f)
		out.CollectorEndpoint = endpointKey(f.CollectorID, f)
		out.UpdatedAt = time.Now().Unix()
		if id == "" {
			var deviceCount int64
			if err := tx.Model(&models.MonitorDevice{}).Where("collector_id = ? AND deleted = ?", f.CollectorID, false).Count(&deviceCount).Error; err != nil {
				return err
			}
			limit := s.MaxDevices
			if limit <= 0 {
				limit = 1000
			}
			if deviceCount >= int64(limit) {
				return bad("collector device limit reached")
			}
			out.ID = uuid.NewString()
			out.RecordVersion = 1
			out.DesiredRevision = uuid.NewString()
			out.CreatedAt = out.UpdatedAt
			out.RequestKey = requestKey
			out.RequestHash = requestHash
			target := models.Target{Ident: f.Ident, Note: f.Name, HostIp: f.Address, Tags: targetTags(f.Labels)}
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&target)
			if created.Error != nil {
				return created.Error
			}
			if created.RowsAffected == 0 {
				if f.Ident != f.Address {
					return conflict("target ident already belongs to another object")
				}
				// Repeated IPs receive an independent identity without taking over
				// an existing managed device or ordinary Agent target.
				f.Ident = "snmp-" + out.ID
				out.Ident = f.Ident
				target.Id = 0
				target.Ident = f.Ident
				if err := tx.Create(&target).Error; err != nil {
					return err
				}
			}
		} else {
			out.ID = old.ID
			out.RecordVersion = old.RecordVersion + 1
			out.DesiredRevision = old.DesiredRevision
			out.PublicationVersion = old.PublicationVersion
			out.WithdrawVersion = old.WithdrawVersion
			out.WithdrawAt = old.WithdrawAt
			out.WithdrawIntervalSeconds = old.WithdrawIntervalSeconds
			out.LastReceivedAt = old.LastReceivedAt
			out.LastRejectedAt = old.LastRejectedAt
			out.LastClockErrorAt = old.LastClockErrorAt
			out.CreatedAt = old.CreatedAt
			out.RequestKey = old.RequestKey
			out.RequestHash = old.RequestHash
			if configFingerprint(inputFromDevice(old)) != configFingerprint(f) {
				out.DesiredRevision = uuid.NewString()
				out.LastClockErrorAt = 0
				if !f.Enabled && old.Enabled {
					out.WithdrawIntervalSeconds = old.IntervalSeconds
					out.WithdrawVersion = ""
					out.WithdrawAt = 0
				}
			}
			if old.CollectorID != f.CollectorID {
				out.PublicationVersion = ""
				out.WithdrawVersion = ""
				out.WithdrawAt = 0
				out.WithdrawIntervalSeconds = 0
			}
			if f.Enabled {
				out.WithdrawVersion = ""
				out.WithdrawAt = 0
			}
			if err := tx.Model(&models.Target{}).Where("ident = ?", old.Ident).Updates(map[string]interface{}{"note": f.Name, "host_ip": f.Address, "tags": targetTags(f.Labels), "update_at": out.UpdatedAt}).Error; err != nil {
				return err
			}
		}
		if err := tx.Save(&out).Error; err != nil {
			return err
		}
		if err := syncDeviceProfile(tx, &out, old, applyProfile, c.DatasourceID, f.UpgradeResolution == "keep_device"); err != nil {
			return err
		}
		if err := tx.Where("target_ident = ?", f.Ident).Delete(&models.TargetBusiGroup{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.TargetBusiGroup{TargetIdent: f.Ident, GroupId: f.GroupID, UpdateAt: out.UpdatedAt}).Error; err != nil {
			return err
		}
		for _, cid := range sortedUnique(ids) {
			current := collectors[cid]
			if err := s.publish(tx, &current, scope, "device_saved"); err != nil {
				return err
			}
		}
		if err := tx.First(&out, "id = ?", out.ID).Error; err != nil {
			return err
		}
		return s.audit(tx, scope, f.GroupID, out.ID, "device_saved", out.PublicationVersion, map[string]interface{}{"record_version": out.RecordVersion, "before": inputFromDevice(old), "after": inputFromDevice(out), "changed": id == "" || old.DesiredRevision != out.DesiredRevision})
	})
	return out, err
}
func targetTags(labels map[string]string) string {
	out := []string{}
	for k, v := range labels {
		out = append(out, k+"="+v)
	}
	return strings.Join(sortedUnique(out), " ")
}
func (s *Store) Device(scope Scope, id string) (models.MonitorDevice, error) {
	var d models.MonitorDevice
	if err := s.DB.First(&d, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return d, missing()
		}
		return d, err
	}
	return d, scope.Check(d.GroupID, false)
}
func (s *Store) Devices(scope Scope, group int64, collector string, deleted bool, limit, offset int) ([]models.MonitorDevice, int64, error) {
	out := []models.MonitorDevice{}
	q := s.DB.Model(&models.MonitorDevice{})
	if !scope.Admin {
		q = q.Where("group_id IN ?", scope.ReadGroups)
	}
	if group > 0 {
		if err := scope.Check(group, false); err != nil {
			return out, 0, err
		}
		q = q.Where("group_id = ?", group)
	}
	if collector != "" {
		q = q.Where("collector_id = ?", collector)
	}
	if deleted {
		if !scope.Admin {
			return out, 0, denied()
		}
	} else {
		q = q.Where("deleted = ?", false)
	}
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return out, 0, err
	}
	err := q.Order("created_at DESC, id").Limit(limit).Offset(offset).Find(&out).Error
	return out, total, err
}
func (s *Store) SetDeviceState(scope Scope, id string, version int64, enable, remove bool) (models.MonitorDevice, error) {
	var out models.MonitorDevice
	err := s.write(func(tx *gorm.DB) error {
		var d models.MonitorDevice
		if tx.First(&d, "id = ?", id).Error != nil {
			return missing()
		}
		if err := scope.Check(d.GroupID, true); err != nil {
			return err
		}
		if enable {
			if err := groupExists(tx, d.GroupID); err != nil {
				return err
			}
			if _, err := s.credential(tx, d.CredentialID, scope, d.GroupID); err != nil {
				return err
			}
		}
		c, err := s.collector(tx, d.CollectorID)
		if err != nil {
			return err
		}
		if err = tx.First(&d, "id = ?", id).Error; err != nil {
			return err
		}
		if d.RecordVersion != version {
			return conflict("record version conflict")
		}
		if d.Deleted {
			return conflict("device is deleted")
		}
		outDeviceBeforeState := d
		if enable {
			if err = s.validateDevice(tx, scope, inputFromDevice(d), c); err != nil {
				return err
			}
			d.DesiredRevision = uuid.NewString()
			d.LastClockErrorAt = 0
		}
		if !enable && !d.Enabled && !remove {
			out = d
			return nil
		}
		wasEnabled := d.Enabled
		d.Enabled = enable
		d.Deleted = remove
		d.RecordVersion++
		d.UpdatedAt = time.Now().Unix()
		if wasEnabled != enable {
			if !enable {
				d.WithdrawIntervalSeconds = d.IntervalSeconds
			}
			d.WithdrawVersion = ""
			d.WithdrawAt = 0
		}
		if err = tx.Save(&d).Error; err != nil {
			return err
		}
		if err = syncDeviceProfile(tx, &d, outDeviceBeforeState, false, c.DatasourceID); err != nil {
			return err
		}
		if err = s.publish(tx, &c, scope, "device_state_changed"); err != nil {
			return err
		}
		if err = tx.First(&out, "id = ?", id).Error; err != nil {
			return err
		}
		return s.audit(tx, scope, d.GroupID, id, "device_state_changed", c.DesiredVersion, map[string]interface{}{"enabled": enable, "deleted": remove, "record_version": d.RecordVersion})
	})
	return out, err
}
func withdrawn(d models.MonitorDevice, c models.MonitorCollector, now int64) bool {
	if d.Enabled {
		return false
	}
	if d.PublicationVersion == "" && d.WithdrawAt == 0 {
		return true
	}
	if d.WithdrawVersion == "" || c.LastClientVersion != c.DesiredVersion || c.ClientSeenAt < d.WithdrawAt {
		return false
	}
	latest := max(d.WithdrawAt, c.ClientSeenAt, d.LastReceivedAt, d.LastRejectedAt)
	interval := d.WithdrawIntervalSeconds
	if interval <= 0 {
		interval = d.IntervalSeconds
	}
	return now-latest >= int64(max(3*interval, 90))
}

type historicalDevice struct {
	address                          string
	port, interval, timeout, retries int
	community                        string
	templateID                       string
	labels                           map[string]string
	protocol                         string
	parameters                       map[string]interface{}
	secrets                          map[string]interface{}
}

func decodeHistorical(configs map[string]map[string]ConfigContent, collector string) (map[string]historicalDevice, error) {
	out := map[string]historicalDevice{}
	for plugin, entries := range configs {
		if plugin == "ping" {
			if err := validateHistoricalPing(entries, collector); err != nil {
				return nil, err
			}
			continue
		}
		if plugin == "mysql" {
			items, err := decodeMySQLHistorical(entries, collector)
			if err != nil {
				return nil, err
			}
			for id, item := range items {
				if _, duplicate := out[id]; duplicate {
					return nil, bad("duplicate historical device")
				}
				out[id] = item
			}
			continue
		}
		if plugin != "snmp" {
			return nil, bad("historical plugin is not allowed")
		}
		for hash, content := range entries {
			if content.Format != "toml" || digest([]byte(content.Config)) != hash {
				return nil, bad("historical content integrity failed")
			}
			var config snmpConfig
			if toml.Unmarshal([]byte(content.Config), &config) != nil || len(config.Instances) != 1 {
				return nil, bad("historical SNMP configuration is invalid")
			}
			ins := config.Instances[0]
			if ins.Labels["collection_mode"] == "probe" {
				continue
			}
			if ins.Labels["collection_collector_id"] != collector || len(ins.Agents) != 1 {
				return nil, bad("historical collector mismatch")
			}
			id := ins.Labels["collection_device_id"]
			if id == "" {
				return nil, bad("historical device identity is missing")
			}
			host, port, err := net.SplitHostPort(strings.TrimPrefix(ins.Agents[0], "udp://"))
			if err != nil {
				return nil, err
			}
			p, err := strconv.Atoi(port)
			if err != nil {
				return nil, err
			}
			duration, err := time.ParseDuration(ins.Timeout)
			if err != nil {
				return nil, err
			}
			labels := map[string]string{}
			for k, v := range ins.Labels {
				if !reservedLabel(k) {
					labels[k] = v
				}
			}
			if _, duplicate := out[id]; duplicate {
				return nil, bad("duplicate historical device")
			}
			out[id] = historicalDevice{address: host, port: p, interval: config.Interval, timeout: int(duration / time.Second), retries: ins.Retries, community: ins.Community, templateID: ins.Labels["collection_template_version"], labels: labels, protocol: "snmp_v2c"}
		}
	}
	return out, nil
}

func validateAsset(tx *gorm.DB, scope Scope, id string) error {
	if id != "" {
		if !scope.Admin {
			var a cmdb.Access
			if err := tx.First(&a, "user_id = ?", scope.ActorID).Error; err != nil || !a.Read {
				return denied()
			}
		}
		var asset cmdb.Asset
		if tx.First(&asset, "id = ? AND deleted = ?", id, false).Error != nil {
			return bad("linked asset is unavailable")
		}
	}
	return nil
}
