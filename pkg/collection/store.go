package collection

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string      { return e.Message }
func bad(message string) error      { return &Error{400, message} }
func conflict(message string) error { return &Error{409, message} }
func denied() error                 { return &Error{403, "forbidden"} }
func missing() error                { return &Error{404, "collection object not found"} }

type Scope struct {
	ActorID     int64
	Admin       bool
	ReadGroups  []int64
	WriteGroups []int64
}

func (s Scope) Can(group int64, write bool) bool {
	if s.Admin {
		return true
	}
	ids := s.ReadGroups
	if write {
		ids = s.WriteGroups
	}
	for _, id := range ids {
		if id == group {
			return true
		}
	}
	return false
}
func (s Scope) Check(group int64, write bool) error {
	if !s.Can(group, write) {
		return denied()
	}
	return nil
}

type Store struct {
	DB            *gorm.DB
	Keys          *Keyring
	Writers       map[string]bool
	MaxDevices    int
	MaxBytes      int
	FutureSeconds int
	// SQLite needs process serialization; row locks serialize independent Center replicas on server databases.
	mu sync.Mutex
}

func (s *Store) Migrate() error {
	db := s.DB
	if err := models.MigrateMonitorAddressConstraints(db); err != nil {
		return err
	}
	if db.Dialector.Name() == "mysql" {
		db = db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci").Session(&gorm.Session{Context: context.Background()})
	}
	if err := db.AutoMigrate(models.MonitorTables()...); err != nil {
		return err
	}
	tpl := templateDefinition()
	var existing models.MonitorTemplateVersion
	err := s.DB.First(&existing, "id = ?", tpl.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.DB.Create(&tpl).Error
	}
	if err != nil {
		return err
	}
	if existing.Digest != tpl.Digest {
		return errors.New("immutable collection template digest mismatch")
	}
	return nil
}
func (s *Store) write(fn func(*gorm.DB) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.DB.Transaction(fn, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}
func (s *Store) collector(tx *gorm.DB, id string) (models.MonitorCollector, error) {
	var c models.MonitorCollector
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return c, missing()
	}
	return c, err
}
func (s *Store) audit(tx *gorm.DB, scope Scope, group int64, id, action, publication string, detail interface{}) error {
	return tx.Create(&models.MonitorAudit{ID: uuid.NewString(), ActorID: scope.ActorID, GroupID: group, ObjectID: id, Action: action, Publication: publication, Detail: models.MonitorText(canonical(detail)), CreatedAt: time.Now().Unix()}).Error
}
func groupExists(tx *gorm.DB, id int64) error {
	var group models.BusiGroup
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return bad("business group does not exist")
	}
	return err
}

type CollectorInput struct {
	Name          string   `json:"name"`
	AgentIdent    string   `json:"agent_ident"`
	DatasourceID  int64    `json:"datasource_id"`
	GroupIDs      []int64  `json:"group_ids"`
	Networks      []string `json:"networks"`
	Ports         []int    `json:"ports"`
	WriterUser    string   `json:"writer_user"`
	RecordVersion int64    `json:"record_version"`
}
type CollectorView struct {
	models.MonitorCollector
	GroupIDs []int64  `json:"group_ids"`
	Networks []string `json:"networks"`
	Ports    []int    `json:"ports"`
}

func (s *Store) CreateCollector(scope Scope, f CollectorInput) (models.MonitorCollector, string, error) {
	var c models.MonitorCollector
	if !scope.Admin {
		return c, "", denied()
	}
	if strings.TrimSpace(f.Name) == "" || len(f.Name) > 128 || len(f.AgentIdent) > 191 || f.AgentIdent == "" || len(f.GroupIDs) == 0 || len(f.GroupIDs) > 100 {
		return c, "", bad("collector name, agent and groups are required")
	}
	if !s.Writers[f.WriterUser] {
		return c, "", bad("writer identity must be a dedicated configured agent account")
	}
	secret, err := newSecret()
	if err != nil {
		return c, "", err
	}
	c = models.MonitorCollector{ID: uuid.NewString(), Name: f.Name, AgentIdent: f.AgentIdent, DatasourceID: f.DatasourceID, Networks: models.MonitorText(canonical(f.Networks)), Ports: models.MonitorText(canonical(f.Ports)), WriterUser: f.WriterUser, TokenHash: digest([]byte(secret)), Enabled: true, RecordVersion: 1}
	err = s.write(func(tx *gorm.DB) error {
		var ds models.Datasource
		if tx.First(&ds, "id = ?", f.DatasourceID).Error != nil {
			return bad("datasource does not exist")
		}
		if ds.PluginType != "prometheus" {
			return bad("collector requires a Prometheus datasource")
		}
		if err := tx.Create(&c).Error; err != nil {
			return conflict("collector writer identity already registered")
		}
		sort.Slice(f.GroupIDs, func(i, j int) bool { return f.GroupIDs[i] < f.GroupIDs[j] })
		seen := map[int64]bool{}
		for _, id := range f.GroupIDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			if err := groupExists(tx, id); err != nil {
				return err
			}
			if err := tx.Create(&models.MonitorCollectorGroup{CollectorID: c.ID, GroupID: id}).Error; err != nil {
				return err
			}
		}
		if err := s.publish(tx, &c, scope, "collector_created"); err != nil {
			return err
		}
		return s.audit(tx, scope, 0, c.ID, "collector_created", c.DesiredVersion, nil)
	})
	if err != nil {
		return models.MonitorCollector{}, "", err
	}
	return c, secret, nil
}
func (s *Store) Collectors(scope Scope) ([]CollectorView, error) {
	out := []CollectorView{}
	var cs []models.MonitorCollector
	if err := s.DB.Order("name").Find(&cs).Error; err != nil {
		return nil, err
	}
	for _, c := range cs {
		var ids []int64
		if err := s.DB.Model(&models.MonitorCollectorGroup{}).Where("collector_id = ?", c.ID).Pluck("group_id", &ids).Error; err != nil {
			return nil, err
		}
		allowed := scope.Admin
		visible := []int64{}
		for _, id := range ids {
			if scope.Can(id, false) {
				allowed = true
				visible = append(visible, id)
			}
		}
		if !allowed {
			continue
		}
		v := CollectorView{MonitorCollector: c, GroupIDs: visible}
		if scope.Admin {
			_ = json.Unmarshal([]byte(c.Networks), &v.Networks)
			_ = json.Unmarshal([]byte(c.Ports), &v.Ports)
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Store) RotateCollector(scope Scope, id string, version int64) (string, error) {
	if !scope.Admin {
		return "", denied()
	}
	secret, err := newSecret()
	if err != nil {
		return "", err
	}
	err = s.write(func(tx *gorm.DB) error {
		c, err := s.collector(tx, id)
		if err != nil {
			return err
		}
		if c.RecordVersion != version {
			return conflict("record version conflict")
		}
		c.PreviousTokenHash = c.TokenHash
		c.PreviousTokenUntil = time.Now().Add(5 * time.Minute).Unix()
		c.TokenHash = digest([]byte(secret))
		c.RecordVersion++
		if err = tx.Save(&c).Error; err != nil {
			return err
		}
		return s.audit(tx, scope, 0, id, "collector_token_rotated", "", nil)
	})
	if err != nil {
		return "", err
	}
	return secret, nil
}
func (s *Store) DeleteCollector(scope Scope, id string, version int64) error {
	if !scope.Admin {
		return denied()
	}
	waiting := false
	err := s.write(func(tx *gorm.DB) error {
		c, err := s.collector(tx, id)
		if err != nil {
			return err
		}
		if c.RecordVersion != version {
			return conflict("record version conflict")
		}
		var n int64
		if err = tx.Model(&models.MonitorDevice{}).Where("collector_id = ?", id).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return conflict("collector still has devices or withdrawal records")
		}
		if err = tx.Model(&models.MonitorProbe{}).Where("collector_id = ? AND state <> ?", id, "cleaned").Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return conflict("collector still has probes awaiting cleanup")
		}
		var publication models.MonitorPublication
		if err = tx.First(&publication, "collector_id = ? AND version = ?", c.ID, c.DesiredVersion).Error; err != nil {
			return err
		}
		var manifest []string
		if json.Unmarshal([]byte(publication.Manifest), &manifest) != nil {
			return errors.New("invalid collection manifest")
		}
		for _, item := range manifest {
			if strings.HasPrefix(item, "agent-ping:") {
				// Persist an authoritative withdrawal before revoking provider
				// access, which otherwise leaves the Agent's last task running.
				if err = s.publish(tx, &c, scope, "collector_withdrawal"); err != nil {
					return err
				}
				waiting = true
				return s.audit(tx, scope, 0, id, "collector_withdrawal", c.DesiredVersion, nil)
			}
		}
		// The host may already have been removed and its withdrawal published
		// by a poll. An empty current snapshot alone does not prove it was applied.
		if err = tx.Model(&models.MonitorPublication{}).Where("collector_id = ? AND manifest LIKE ?", c.ID, `%"agent-ping:%`).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 && (c.LastClientVersion != c.DesiredVersion || c.ClientSeenAt == 0 || time.Now().Unix()-c.ClientSeenAt < 90) {
			waiting = true
			return nil
		}
		if err = tx.Delete(&c).Error; err != nil {
			return err
		}
		if err = tx.Where("collector_id = ?", id).Delete(&models.MonitorCollectorGroup{}).Error; err != nil {
			return err
		}
		return s.audit(tx, scope, 0, id, "collector_deleted", "", nil)
	})
	if err == nil && waiting {
		return conflict("ICMP withdrawal published; retry deletion 90 seconds after the collector acknowledges it")
	}
	return err
}

func (s *Store) credential(tx *gorm.DB, id string, scope Scope, group int64) (models.MonitorCredential, error) {
	var c models.MonitorCredential
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c, missing()
		}
		return c, err
	}
	if c.GroupID != group || !scope.Can(c.GroupID, true) {
		return c, denied()
	}
	return c, nil
}

type CredentialInput struct {
	Name          string `json:"name"`
	GroupID       int64  `json:"group_id"`
	Community     string `json:"community"`
	RecordVersion int64  `json:"record_version"`
}

func (s *Store) SaveCredential(scope Scope, id string, f CredentialInput) (models.MonitorCredential, error) {
	var out models.MonitorCredential
	if err := scope.Check(f.GroupID, true); err != nil {
		return out, err
	}
	if len(f.Name) == 0 || len(f.Name) > 128 || len(f.Community) == 0 || len(f.Community) > 256 || strings.ContainsRune(f.Community, 0) {
		return out, bad("credential name and community are required")
	}
	err := s.write(func(tx *gorm.DB) error {
		if err := groupExists(tx, f.GroupID); err != nil {
			return err
		}
		if id != "" {
			if _, err := s.credential(tx, id, scope, f.GroupID); err != nil {
				return err
			}
		}
		// Hold the credential row first, then lock every affected collector in a stable order.
		var ids []string
		if id != "" {
			if err := tx.Model(&models.MonitorDevice{}).Where("credential_id = ?", id).Distinct().Pluck("collector_id", &ids).Error; err != nil {
				return err
			}
		}
		var collectors []models.MonitorCollector
		for _, cid := range sortedUnique(ids) {
			c, err := s.collector(tx, cid)
			if err != nil {
				return err
			}
			collectors = append(collectors, c)
		}
		if id == "" {
			out = models.MonitorCredential{ID: uuid.NewString(), GroupID: f.GroupID, Kind: "snmp_v2c", RecordVersion: 1}
		} else {
			var err error
			out, err = s.credential(tx, id, scope, f.GroupID)
			if err != nil {
				return err
			}
			if out.RecordVersion != f.RecordVersion {
				return conflict("record version conflict")
			}
			out.RecordVersion++
		}
		sealed, key, err := s.Keys.Seal([]byte(f.Community), "credential:"+out.ID)
		if err != nil {
			return err
		}
		out.Name = f.Name
		out.Ciphertext = models.MonitorText(sealed)
		out.KeyID = key
		out.UpdatedAt = time.Now().Unix()
		if err = tx.Save(&out).Error; err != nil {
			return err
		}
		// A new revision changes the content checksum even if the secret was restored to an old value.
		var devices []models.MonitorDevice
		if err = tx.Where("credential_id = ? AND enabled = ? AND deleted = ?", out.ID, true, false).Find(&devices).Error; err != nil {
			return err
		}
		for _, d := range devices {
			before := d
			d.DesiredRevision = uuid.NewString()
			d.RecordVersion++
			d.UpdatedAt = time.Now().Unix()
			d.LastClockErrorAt = 0
			if err = tx.Save(&d).Error; err != nil {
				return err
			}
			var owner models.MonitorCollector
			if err = tx.First(&owner, "id = ?", d.CollectorID).Error; err != nil {
				return err
			}
			if err = syncDeviceProfile(tx, &d, before, false, owner.DatasourceID); err != nil {
				return err
			}
		}
		for i := range collectors {
			if err = s.publish(tx, &collectors[i], scope, "credential_changed"); err != nil {
				return err
			}
		}
		return s.audit(tx, scope, out.GroupID, out.ID, "credential_saved", "", map[string]interface{}{"affected_devices": len(devices), "record_version": out.RecordVersion})
	})
	return out, err
}
func (s *Store) DeleteCredential(scope Scope, id string, version int64) error {
	return s.write(func(tx *gorm.DB) error {
		var c models.MonitorCredential
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "id = ?", id).Error; err != nil {
			return missing()
		}
		if err := scope.Check(c.GroupID, true); err != nil {
			return err
		}
		if c.RecordVersion != version {
			return conflict("record version conflict")
		}
		var n int64
		if err := tx.Model(&models.MonitorDevice{}).Where("credential_id = ?", id).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return conflict("credential still referenced by a device or withdrawal record")
		}
		var probes []models.MonitorProbe
		if err := tx.Where("group_id = ? AND state <> ?", c.GroupID, "cleaned").Find(&probes).Error; err != nil {
			return err
		}
		if len(probes) > 0 {
			return conflict("business group still has pending probes")
		}
		if err := tx.Delete(&c).Error; err != nil {
			return err
		}
		return s.audit(tx, scope, c.GroupID, id, "credential_deleted", "", nil)
	})
}
func (s *Store) Credentials(scope Scope) ([]models.MonitorCredential, error) {
	out := []models.MonitorCredential{}
	q := s.DB.Order("name")
	if !scope.Admin {
		q = q.Where("group_id IN ?", scope.ReadGroups)
	}
	err := q.Find(&out).Error
	return out, err
}
func (s *Store) References(scope Scope, id string) (int64, error) {
	var c models.MonitorCredential
	if err := s.DB.First(&c, "id = ?", id).Error; err != nil {
		return 0, missing()
	}
	if err := scope.Check(c.GroupID, true); err != nil {
		return 0, err
	}
	var n int64
	err := s.DB.Model(&models.MonitorDevice{}).Where("credential_id = ?", id).Count(&n).Error
	return n, err
}

// publish is called only while holding the collector row lock inside the mutation transaction.
func (s *Store) publish(tx *gorm.DB, c *models.MonitorCollector, scope Scope, reason string) error {
	configs := map[string]map[string]ConfigContent{}
	manifest := []string{}
	var devices []models.MonitorDevice
	if err := tx.Where("collector_id = ? AND enabled = ? AND deleted = ?", c.ID, true, false).Order("id").Find(&devices).Error; err != nil {
		return err
	}
	limit := s.MaxDevices
	if limit <= 0 {
		limit = 1000
	}
	if len(devices) > limit {
		return bad("collector device limit reached")
	}
	for _, d := range devices {
		var cred models.MonitorCredential
		if err := tx.First(&cred, "id = ?", d.CredentialID).Error; err != nil {
			return errors.New("collection credential unavailable")
		}
		if cred.GroupID != d.GroupID {
			return errors.New("collection credential scope mismatch")
		}
		secret, err := s.Keys.Open(string(cred.Ciphertext), cred.KeyID, "credential:"+cred.ID)
		if err != nil {
			return err
		}
		tpl, err := loadTemplate(tx, d.TemplateVersionID)
		if err != nil {
			return err
		}
		f := inputFromDevice(d)
		params, err := executionParameters(f, cred, secret)
		if err != nil {
			return err
		}
		var content ConfigContent
		plugin := "snmp"
		if d.Protocol == "mysql" {
			if tpl.Catalog == nil {
				return bad("MySQL requires a published package")
			}
			plugin = "mysql"
			content, err = compileMySQL(f, d.ID, d.DesiredRevision, params, documentConfig(tpl))
		} else {
			community, _ := params["community"].(string)
			content, err = compile(f, d.ID, d.DesiredRevision, community, "managed", tpl.TemplateConfig)
		}
		if err != nil {
			return err
		}
		if configs[plugin] == nil {
			configs[plugin] = map[string]ConfigContent{}
		}
		configs[plugin][digest([]byte(content.Config))] = content
		ping, err := compilePing(f, d.ID, d.DesiredRevision)
		if err != nil {
			return err
		}
		if configs["ping"] == nil {
			configs["ping"] = map[string]ConfigContent{}
		}
		configs["ping"][digest([]byte(ping.Config))] = ping
		manifest = append(manifest, d.ID+":"+d.DesiredRevision)
	}
	if reason != "collector_withdrawal" {
		agentPings, err := agentPingConfigs(tx, c.ID)
		if err != nil {
			return err
		}
		for hash, content := range agentPings {
			if configs["ping"] == nil {
				configs["ping"] = map[string]ConfigContent{}
			}
			configs["ping"][hash] = content
		}
		manifest = append(manifest, agentPingManifest(agentPings)...)
	}
	var probes []models.MonitorProbe
	if err := tx.Where("collector_id = ? AND cleanup_at = 0", c.ID).Order("id").Find(&probes).Error; err != nil {
		return err
	}
	for _, p := range probes {
		b, err := s.Keys.Open(string(p.Ciphertext), p.KeyID, "probe:"+p.ID)
		if err != nil {
			return err
		}
		var envelope struct {
			ConfigContent
			Plugin string `json:"plugin"`
		}
		if json.Unmarshal(b, &envelope) != nil {
			return errors.New("invalid encrypted probe configuration")
		}
		plugin := envelope.Plugin
		if plugin == "" {
			plugin = "snmp"
		}
		if plugin != "snmp" && plugin != "mysql" {
			return bad("unknown probe adapter")
		}
		if configs[plugin] == nil {
			configs[plugin] = map[string]ConfigContent{}
		}
		configs[plugin][digest([]byte(envelope.Config))] = envelope.ConfigContent
		manifest = append(manifest, "probe:"+p.ID+":"+p.Revision)
	}
	b := canonical(configs)
	hash := digest(b)
	if c.DesiredVersion != "" {
		var current models.MonitorPublication
		if err := tx.First(&current, "version = ? AND collector_id = ?", c.DesiredVersion, c.ID).Error; err != nil {
			return err
		}
		if current.Digest == hash {
			return nil
		}
	}
	max := s.MaxBytes
	if max <= 0 {
		max = 8 << 20
	}
	if len(b) > max-256 {
		return bad("collector configuration exceeds response size limit")
	}
	p := models.MonitorPublication{ID: uuid.NewString(), CollectorID: c.ID, Version: uuid.NewString(), Digest: hash, Manifest: models.MonitorText(canonical(manifest)), Reason: reason, CreatedBy: scope.ActorID, CreatedAt: time.Now().Unix()}
	sealed, key, err := s.Keys.Seal(b, "publication:"+p.ID)
	if err != nil {
		return err
	}
	p.Ciphertext = models.MonitorText(sealed)
	p.KeyID = key
	if err = tx.Create(&p).Error; err != nil {
		return err
	}
	c.DesiredVersion = p.Version
	if err = tx.Model(c).Update("desired_version", p.Version).Error; err != nil {
		return err
	}
	if err = tx.Model(&models.MonitorDevice{}).Where("collector_id = ? AND enabled = ? AND deleted = ?", c.ID, true, false).Update("publication_version", p.Version).Error; err != nil {
		return err
	}
	if err = tx.Model(&models.MonitorDevice{}).Where("collector_id = ? AND (enabled = ? OR deleted = ?) AND withdraw_version = ?", c.ID, false, true, "").Updates(map[string]interface{}{"withdraw_version": p.Version, "withdraw_at": p.CreatedAt}).Error; err != nil {
		return err
	}
	return tx.Model(&models.MonitorProbe{}).Where("collector_id = ? AND cleanup_at > 0 AND cleanup_version = ?", c.ID, "").Update("cleanup_version", p.Version).Error
}
func (s *Store) Provider(user, secret, clientVersion string) (FullSnapshot, bool, error) {
	var out FullSnapshot
	unchanged := false
	err := s.write(func(tx *gorm.DB) error {
		c, err := s.collector(tx, user)
		if err != nil {
			if _, ok := err.(*Error); ok {
				return &Error{401, "unauthorized"}
			}
			return err
		}
		hash := digest([]byte(secret))
		valid := subtle.ConstantTimeCompare([]byte(hash), []byte(c.TokenHash)) == 1
		if c.PreviousTokenUntil > time.Now().Unix() {
			valid = valid || subtle.ConstantTimeCompare([]byte(hash), []byte(c.PreviousTokenHash)) == 1
		}
		if !valid {
			return &Error{401, "unauthorized"}
		}
		if !c.Enabled {
			return denied()
		}
		var p models.MonitorPublication
		if err = tx.First(&p, "collector_id = ? AND version = ?", c.ID, c.DesiredVersion).Error; err != nil {
			return errors.New("collection snapshot unavailable")
		}
		plain, err := s.Keys.Open(string(p.Ciphertext), p.KeyID, "publication:"+p.ID)
		if err != nil {
			return err
		}
		if digest(plain) != p.Digest {
			return errors.New("collection snapshot integrity failure")
		}
		// Authenticated polling reconciles newly registered Agents, IP/group
		// changes and deletions. The primary device compiler only runs on change.
		if p.Reason != "collector_withdrawal" {
			pings, err := agentPingConfigs(tx, c.ID)
			if err != nil {
				return err
			}
			var manifest []string
			if json.Unmarshal([]byte(p.Manifest), &manifest) != nil {
				return errors.New("invalid collection manifest")
			}
			actual := []string{}
			for _, item := range manifest {
				if strings.HasPrefix(item, "agent-ping:") {
					actual = append(actual, item)
				}
			}
			sort.Strings(actual)
			if string(canonical(actual)) != string(canonical(agentPingManifest(pings))) {
				if err = s.publish(tx, &c, Scope{Admin: true}, "agent_icmp"); err != nil {
					return err
				}
				p = models.MonitorPublication{}
				if err = tx.First(&p, "collector_id = ? AND version = ?", c.ID, c.DesiredVersion).Error; err != nil {
					return err
				}
				plain, err = s.Keys.Open(string(p.Ciphertext), p.KeyID, "publication:"+p.ID)
				if err != nil {
					return err
				}
				if digest(plain) != p.Digest {
					return errors.New("collection snapshot integrity failure")
				}
			}
		}
		out.Version = p.Version
		if err = json.Unmarshal(plain, &out.Configs); err != nil || out.Configs == nil {
			return errors.New("invalid collection snapshot")
		}
		// Only an exact current version can count as received; unknown client strings are never persisted.
		now := time.Now().Unix()
		updates := map[string]interface{}{"last_served_version": p.Version, "last_fetched_at": now}
		if clientVersion == p.Version {
			updates["last_client_version"] = p.Version
			if c.LastClientVersion != p.Version {
				updates["client_seen_at"] = now
			}
			unchanged = true
		}
		return tx.Model(&c).Updates(updates).Error
	})
	return out, unchanged, err
}
func (s *Store) Publications(scope Scope) ([]models.MonitorPublication, error) {
	out := []models.MonitorPublication{}
	q := s.DB.Order("created_at DESC, id").Limit(200)
	if !scope.Admin {
		q = q.Where("version IN (?)", s.DB.Model(&models.MonitorAudit{}).Select("publication").Where("group_id IN ?", scope.ReadGroups))
	}
	err := q.Find(&out).Error
	if !scope.Admin {
		for i := range out {
			out[i].Digest = ""
			out[i].CreatedBy = 0
		}
	}
	return out, err
}

func (s *Store) Rollback(scope Scope, id string) error {
	// Complete collector rollback can affect several groups and is administrator-only.
	if !scope.Admin {
		return denied()
	}
	return s.write(func(tx *gorm.DB) error {
		var selected models.MonitorPublication
		if tx.First(&selected, "id = ?", id).Error != nil {
			return missing()
		}
		c, err := s.collector(tx, selected.CollectorID)
		if err != nil {
			return err
		}
		b, err := s.Keys.Open(string(selected.Ciphertext), selected.KeyID, "publication:"+selected.ID)
		if err != nil {
			return err
		}
		var configs map[string]map[string]ConfigContent
		if json.Unmarshal(b, &configs) != nil {
			return errors.New("invalid historical snapshot")
		}
		// Rollback restores device parameters, credentials, and labels from validated, server-produced TOML.
		restored, err := decodeHistorical(configs, c.ID)
		if err != nil {
			return err
		}
		var current []models.MonitorDevice
		if err = tx.Where("collector_id = ?", c.ID).Find(&current).Error; err != nil {
			return err
		}
		sort.Slice(current, func(i, j int) bool { return current[i].ID < current[j].ID })
		for _, d := range current {
			r, ok := restored[d.ID]
			if ok {
				if d.Deleted {
					return conflict("rollback includes a deleted device; restore explicitly")
				}
				f := inputFromDevice(d)
				f.TemplateVersionID = r.templateID
				if f.TemplateVersionID == "" {
					f.TemplateVersionID = WindowsTemplate
				}
				if _, err = loadTemplate(tx, f.TemplateVersionID); err != nil {
					return err
				}
				f.Address = r.address
				f.Port = r.port
				f.IntervalSeconds = r.interval
				f.TimeoutSeconds = r.timeout
				f.Retries = r.retries
				f.Labels = r.labels
				f.Protocol = r.protocol
				f.Parameters = r.parameters
				f.Enabled = true
				if err = f.normalize(); err != nil {
					return err
				}
				// Credentials restored into a fresh, private record, avoiding mutation of a shared secret.
				credID := uuid.NewString()
				secret, kind := []byte(r.community), r.protocol
				if r.secrets != nil {
					secret = canonical(r.secrets)
					kind = "parameters:" + r.protocol
				}
				sealed, key, e := s.Keys.Seal(secret, "credential:"+credID)
				if e != nil {
					return e
				}
				cred := models.MonitorCredential{ID: credID, Name: "Rollback credential", GroupID: d.GroupID, Kind: kind, Ciphertext: models.MonitorText(sealed), KeyID: key, RecordVersion: 1, UpdatedAt: time.Now().Unix()}
				if err = tx.Create(&cred).Error; err != nil {
					return err
				}
				f.CredentialID = credID
				replacement := inputDevice(f)
				replacement.ID = d.ID
				replacement.DesiredRevision = uuid.NewString()
				replacement.RecordVersion = d.RecordVersion + 1
				replacement.CreatedAt = d.CreatedAt
				replacement.UpdatedAt = time.Now().Unix()
				replacement.RequestKey = d.RequestKey
				replacement.RequestHash = d.RequestHash
				replacement.GroupEndpoint = endpointKey(fmt.Sprint(f.GroupID), f)
				replacement.CollectorEndpoint = endpointKey(c.ID, f)
				if err = tx.Save(&replacement).Error; err != nil {
					return err
				}
				if err = syncDeviceProfile(tx, &replacement, d, replacement.TemplateVersionID != d.TemplateVersionID, c.DatasourceID); err != nil {
					return err
				}
				delete(restored, d.ID)
			} else if d.Enabled {
				before := d
				d.Enabled = false
				if err = syncDeviceProfile(tx, &d, before, false, c.DatasourceID); err != nil {
					return err
				}
				if err = tx.Model(&d).Updates(map[string]interface{}{"enabled": false, "record_version": d.RecordVersion + 1, "withdraw_version": "", "withdraw_at": 0, "withdraw_interval_seconds": d.IntervalSeconds}).Error; err != nil {
					return err
				}
			}
		}
		if len(restored) > 0 {
			return conflict("historical snapshot contains unavailable devices")
		}
		if err = s.publish(tx, &c, scope, "rollback"); err != nil {
			return err
		}
		return s.audit(tx, scope, 0, c.ID, "rollback", c.DesiredVersion, map[string]string{"source": id})
	})
}

func (s *Store) UpdateCollector(scope Scope, id string, f CollectorInput) (models.MonitorCollector, error) {
	var out models.MonitorCollector
	if !scope.Admin {
		return out, denied()
	}
	if strings.TrimSpace(f.Name) == "" || len(f.Name) > 128 || f.AgentIdent == "" || len(f.AgentIdent) > 191 || len(f.GroupIDs) == 0 || len(f.GroupIDs) > 100 {
		return out, bad("invalid collector fields")
	}
	err := s.write(func(tx *gorm.DB) error {
		sort.Slice(f.GroupIDs, func(i, j int) bool { return f.GroupIDs[i] < f.GroupIDs[j] })
		groups := map[int64]bool{}
		for _, group := range f.GroupIDs {
			if err := groupExists(tx, group); err != nil {
				return err
			}
			groups[group] = true
		}
		c, err := s.collector(tx, id)
		if err != nil {
			return err
		}
		if c.RecordVersion != f.RecordVersion {
			return conflict("record version conflict")
		}
		if f.WriterUser != c.WriterUser {
			return conflict("collector writer identity is immutable")
		}
		var devices []models.MonitorDevice
		if err = tx.Where("collector_id = ?", id).Find(&devices).Error; err != nil {
			return err
		}
		if f.DatasourceID != c.DatasourceID && len(devices) > 0 {
			return conflict("cannot change the datasource of a referenced collector")
		}
		var datasource models.Datasource
		if tx.First(&datasource, "id = ? AND plugin_type = ?", f.DatasourceID, "prometheus").Error != nil {
			return bad("collector requires a Prometheus datasource")
		}
		c.Name = f.Name
		c.AgentIdent = f.AgentIdent
		c.DatasourceID = f.DatasourceID
		c.Networks = models.MonitorText(canonical(f.Networks))
		c.Ports = models.MonitorText(canonical(f.Ports))
		c.RecordVersion++
		for _, d := range devices {
			if !groups[d.GroupID] {
				return conflict("collector group is still referenced")
			}
		}
		var probes []models.MonitorProbe
		if err = tx.Where("collector_id = ? AND state <> ?", id, "cleaned").Find(&probes).Error; err != nil {
			return err
		}
		if len(probes) > 0 {
			return conflict("wait for probe cleanup before changing collector policy")
		}
		if err = tx.Save(&c).Error; err != nil {
			return err
		}
		if err = tx.Where("collector_id = ?", id).Delete(&models.MonitorCollectorGroup{}).Error; err != nil {
			return err
		}
		for group := range groups {
			if err = tx.Create(&models.MonitorCollectorGroup{CollectorID: id, GroupID: group}).Error; err != nil {
				return err
			}
		}
		out = c
		return s.audit(tx, scope, 0, id, "collector_updated", c.DesiredVersion, map[string]interface{}{"record_version": c.RecordVersion})
	})
	return out, err
}
