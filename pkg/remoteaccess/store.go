package remoteaccess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	ctxpkg "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Store struct {
	DB   *gorm.DB
	Keys *Keyring
	mu   sync.Mutex
}
type secret struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type CredentialView struct {
	models.OpsCredential
	References int64 `json:"references"`
	SecretSet  bool  `json:"secret_set"`
}

func Permission(db *gorm.DB, actorID int64, operation string) (*models.User, error) {
	c := ctxpkg.NewContext(context.Background(), db, true)
	u, err := models.UserGetById(c, actorID)
	if err != nil {
		return nil, err
	}
	if u == nil || u.IsDisabled() {
		return nil, denied()
	}
	if !u.IsAdmin() {
		ok, err := models.RoleHasOperation(c, u.RolesLst, operation)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, denied()
		}
	}
	return u, nil
}
func access(db *gorm.DB, actorID, groupID int64, operation string, write bool) error {
	u, err := Permission(db, actorID, operation)
	if err != nil {
		return err
	}
	var group models.BusiGroup
	if err = db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, "id = ?", groupID).Error; err != nil {
		return denied()
	}
	c := ctxpkg.NewContext(context.Background(), db, true)
	var ok bool
	if write {
		ok, err = u.CanDoBusiGroup(c, &group, "rw")
	} else {
		ok, err = u.CanDoBusiGroup(c, &group)
	}
	if err != nil {
		return err
	}
	if !ok {
		return denied()
	}
	return nil
}
func target(db *gorm.DB, ident string) (models.Target, []int64, error) {
	var t models.Target
	if err := db.First(&t, "ident = ?", ident).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return t, nil, missing()
		}
		return t, nil, err
	}
	groups, err := models.TargetGroupIdsGetByIdent(ctxpkg.NewContext(context.Background(), db, true), ident)
	return t, groups, err
}
func audit(tx *gorm.DB, actor, group int64, id, action string) error {
	return tx.Create(&models.OpsAccessAudit{ID: uuid.NewString(), ActorID: actor, GroupID: group, ObjectID: id, Action: action, CreatedAt: time.Now().Unix()}).Error
}
func (s *Store) write(ctx context.Context, fn func(*gorm.DB) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.DB.WithContext(ctx).Transaction(fn)
}
func (s *Store) secret(c models.OpsCredential) (secret, error) {
	var out secret
	b, err := s.Keys.open(c.Ciphertext, c.KeyID, aad(c.ID, c.GroupID))
	if err != nil {
		return out, err
	}
	if json.Unmarshal(b, &out) != nil {
		return out, fmt.Errorf("invalid encrypted SSH credential")
	}
	return out, nil
}
func (s *Store) saveCredential(tx *gorm.DB, actor int64, id string, f CredentialInput, deviceOnly bool) (models.OpsCredential, error) {
	var out models.OpsCredential
	if err := access(tx, actor, f.GroupID, PermCredentials, true); err != nil {
		return out, err
	}
	if len(strings.TrimSpace(f.Name)) == 0 || len(f.Name) > 128 || len(f.Username) > 256 || len(f.Password) > 4096 || strings.ContainsAny(f.Username+f.Password, "\x00\r\n") {
		return out, invalid("invalid SSH credential name or account")
	}
	var value secret
	if id != "" {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "id = ?", id).Error; err != nil {
			return out, missing()
		}
		if out.GroupID != f.GroupID {
			return out, denied()
		}
		if out.RecordVersion != f.RecordVersion {
			return out, conflict()
		}
		// Revocation remains possible even if an old encryption key is lost.
		if f.Disabled != nil && *f.Disabled && f.Username == "" && f.Password == "" {
			out.Disabled = true
			out.Name = f.Name
			out.RecordVersion++
			out.UpdatedBy = actor
			out.UpdatedAt = time.Now().Unix()
			if err := tx.Save(&out).Error; err != nil {
				return out, err
			}
			return out, audit(tx, actor, out.GroupID, out.ID, "credential_disabled")
		}
		var err error
		value, err = s.secret(out)
		if err != nil {
			return out, err
		}
	} else {
		if f.RecordVersion != 0 {
			return out, conflict()
		}
		out = models.OpsCredential{ID: uuid.NewString(), GroupID: f.GroupID, DeviceOnly: deviceOnly}
	}
	if f.Username != "" {
		value.Username = f.Username
	}
	if f.Password != "" {
		value.Password = f.Password
	}
	if strings.TrimSpace(value.Username) == "" || value.Password == "" {
		return out, invalid("SSH account and password are required for a new credential")
	}
	b, _ := json.Marshal(value)
	sealed, key, err := s.Keys.seal(b, aad(out.ID, out.GroupID))
	if err != nil {
		return out, err
	}
	out.Ciphertext = sealed
	out.KeyID = key
	out.Name = f.Name
	out.RecordVersion++
	out.UpdatedBy = actor
	out.UpdatedAt = time.Now().Unix()
	if f.Disabled != nil {
		out.Disabled = *f.Disabled
	}
	if err = tx.Save(&out).Error; err != nil {
		return out, err
	}
	return out, audit(tx, actor, out.GroupID, out.ID, "credential_saved")
}
func (s *Store) SaveCredential(ctx context.Context, actor int64, id string, f CredentialInput) (CredentialView, error) {
	var out models.OpsCredential
	err := s.write(ctx, func(tx *gorm.DB) error {
		var err error
		out, err = s.saveCredential(tx, actor, id, f, false)
		return err
	})
	return CredentialView{OpsCredential: out, SecretSet: out.ID != ""}, err
}
func (s *Store) Credentials(ctx context.Context, actor, group int64) ([]CredentialView, error) {
	db := s.DB.WithContext(ctx)
	if _, err := Permission(db, actor, PermCredentials); err != nil {
		if _, e := Permission(db, actor, PermConfigure); e != nil {
			return nil, denied()
		}
	}
	u, err := models.UserGetById(ctxpkg.NewContext(ctx, db, true), actor)
	if err != nil {
		return nil, err
	}
	if u == nil || u.IsDisabled() {
		return nil, denied()
	}
	var rows []models.OpsCredential
	q := db.Model(&models.OpsCredential{})
	if !u.IsAdmin() {
		ids, err := models.MyBusiGroupIds(ctxpkg.NewContext(ctx, db, true), actor)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return []CredentialView{}, nil
		}
		q = q.Where("group_id IN ?", ids)
	}
	if group > 0 {
		q = q.Where("group_id = ?", group)
	}
	if err = q.Order("name,id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]CredentialView, 0, len(rows))
	for _, c := range rows {
		var count int64
		if err = db.Model(&models.OpsSSHConnection{}).Where("credential_id = ? OR jump_credential_id = ?", c.ID, c.ID).Count(&count).Error; err != nil {
			return nil, err
		}
		out = append(out, CredentialView{OpsCredential: c, References: count, SecretSet: true})
	}
	return out, nil
}
func (s *Store) DeleteCredential(ctx context.Context, actor int64, id string, version int64) error {
	return s.write(ctx, func(tx *gorm.DB) error {
		var c models.OpsCredential
		if err := tx.First(&c, "id = ?", id).Error; err != nil {
			return missing()
		}
		if err := access(tx, actor, c.GroupID, PermCredentials, true); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "id = ?", id).Error; err != nil {
			return missing()
		}
		if c.RecordVersion != version {
			return conflict()
		}
		var n int64
		if err := tx.Model(&models.OpsSSHConnection{}).Where("credential_id = ? OR jump_credential_id = ?", id, id).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return &Error{409, "credential is referenced; disable it to revoke access or detach its connections first"}
		}
		if err := tx.Delete(&c).Error; err != nil {
			return err
		}
		return audit(tx, actor, c.GroupID, id, "credential_deleted")
	})
}
func (s *Store) endpoint(tx *gorm.DB, actor, group int64, label string, e EndpointInput, saved EndpointInput, scoped bool) (string, error) {
	if e.NewCredential != nil {
		if e.CredentialID != "" {
			return "", invalid("choose an existing credential or create a new one")
		}
		f := *e.NewCredential
		f.GroupID = group
		if f.Name == "" {
			f.Name = label
		}
		c, err := s.saveCredential(tx, actor, "", f, true)
		if err != nil {
			return "", err
		}
		return c.ID, nil
	}
	if scoped && (e.CredentialID == "" || e.CredentialID != saved.CredentialID || e.Host != saved.Host || e.Port != saved.Port) {
		return "", invalid("enter the account and password for this device endpoint")
	}
	var c models.OpsCredential
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "id = ?", e.CredentialID).Error; err != nil {
		return "", missing()
	}
	if c.GroupID != group || c.Disabled {
		return "", denied()
	}
	if c.DeviceOnly && e.CredentialID != saved.CredentialID {
		return "", denied()
	}
	return c.ID, nil
}

func retireDeviceCredential(tx *gorm.DB, id string) error {
	if id == "" {
		return nil
	}
	var references int64
	if err := tx.Model(&models.OpsSSHConnection{}).Where("credential_id = ? OR jump_credential_id = ?", id, id).Count(&references).Error; err != nil || references > 0 {
		return err
	}
	return tx.Where("id = ? AND device_only = ?", id, true).Delete(&models.OpsCredential{}).Error
}

// SaveConnectionInTransaction lets device creation and optional SSH settings
// commit together. No credentials enter collection snapshots or publications.
func (s *Store) SaveConnectionInTransaction(tx *gorm.DB, actor int64, ident string, f ConnectionInput) (ConnectionView, error) {
	f.Protocol = connectionProtocol(f.Protocol)
	var out models.OpsSSHConnection
	if err := access(tx, actor, f.GroupID, PermConfigure, true); err != nil {
		return ConnectionView{}, err
	}
	t, groups, err := target(tx, ident)
	if err != nil {
		return ConnectionView{}, err
	}
	if !slices.Contains(groups, f.GroupID) {
		return ConnectionView{}, denied()
	}
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&out, "target_ident = ?", ident).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return ConnectionView{}, err
	}
	if out.TargetIdent != "" && out.TargetID != t.Id {
		return ConnectionView{}, &Error{409, "target was replaced; remove the obsolete SSH binding first"}
	}
	if out.TargetIdent != "" && out.GroupID != f.GroupID {
		if err := access(tx, actor, out.GroupID, PermConfigure, true); err != nil {
			return ConnectionView{}, err
		}
	}
	if out.RecordVersion != f.RecordVersion {
		return ConnectionView{}, conflict()
	}
	if !f.Enabled && out.TargetIdent != "" {
		out.Enabled = false
		out.RecordVersion++
		out.UpdatedBy = actor
		out.UpdatedAt = time.Now().Unix()
		if err = tx.Save(&out).Error; err != nil {
			return ConnectionView{}, err
		}
		return view(out), audit(tx, actor, out.GroupID, ident, "connection_disabled")
	}
	if err = validateConnection(f); err != nil {
		return ConnectionView{}, err
	}
	previous := out
	saved := view(previous)
	if saved.Protocol != f.Protocol || saved.GroupID != f.GroupID {
		saved = ConnectionView{}
	}
	credential, err := s.endpoint(tx, actor, f.GroupID, f.Protocol+" device account", f.Target, saved.Target, f.deviceScoped)
	if err != nil {
		return ConnectionView{}, err
	}
	jumpID := ""
	if f.Jump != nil {
		var savedJump EndpointInput
		if saved.Jump != nil {
			savedJump = *saved.Jump
		}
		jumpID, err = s.endpoint(tx, actor, f.GroupID, "SSH jump account", *f.Jump, savedJump, f.deviceScoped)
		if err != nil {
			return ConnectionView{}, err
		}
	}
	b, _ := json.Marshal(f.LogPaths)
	out = models.OpsSSHConnection{TargetIdent: ident, TargetID: t.Id, GroupID: f.GroupID, Enabled: f.Enabled, Protocol: f.Protocol, Host: f.Target.Host, Port: f.Target.Port, Fingerprint: f.Target.Fingerprint, CredentialID: credential, LogPathsJSON: string(b), RecordVersion: out.RecordVersion + 1, UpdatedBy: actor, UpdatedAt: time.Now().Unix()}
	if f.Jump != nil {
		out.JumpHost = f.Jump.Host
		out.JumpPort = f.Jump.Port
		out.JumpFingerprint = f.Jump.Fingerprint
		out.JumpCredentialID = jumpID
	}
	if err = tx.Save(&out).Error; err != nil {
		return ConnectionView{}, err
	}
	if previous.CredentialID != out.CredentialID {
		if err = retireDeviceCredential(tx, previous.CredentialID); err != nil {
			return ConnectionView{}, err
		}
	}
	if previous.JumpCredentialID != out.JumpCredentialID {
		if err = retireDeviceCredential(tx, previous.JumpCredentialID); err != nil {
			return ConnectionView{}, err
		}
	}
	return view(out), audit(tx, actor, out.GroupID, ident, "connection_saved")
}
func (s *Store) SaveConnection(ctx context.Context, actor int64, ident string, f ConnectionInput) (ConnectionView, error) {
	var out ConnectionView
	err := s.write(ctx, func(tx *gorm.DB) error {
		var err error
		out, err = s.SaveConnectionInTransaction(tx, actor, ident, f)
		return err
	})
	return out, err
}
func view(c models.OpsSSHConnection) ConnectionView {
	f := ConnectionInput{Protocol: connectionProtocol(c.Protocol), GroupID: c.GroupID, Enabled: c.Enabled, Target: EndpointInput{Host: c.Host, Port: c.Port, Fingerprint: c.Fingerprint, CredentialID: c.CredentialID}, LogPaths: []string{}, RecordVersion: c.RecordVersion}
	_ = json.Unmarshal([]byte(c.LogPathsJSON), &f.LogPaths)
	if f.LogPaths == nil {
		f.LogPaths = []string{}
	}
	if c.JumpHost != "" {
		f.Jump = &EndpointInput{Host: c.JumpHost, Port: c.JumpPort, Fingerprint: c.JumpFingerprint, CredentialID: c.JumpCredentialID}
	}
	return ConnectionView{Ident: c.TargetIdent, ConnectionInput: f, Configured: true}
}
func (s *Store) GetConnection(ctx context.Context, actor int64, ident string) (ConnectionView, error) {
	db := s.DB.WithContext(ctx)
	t, groups, err := target(db, ident)
	if err != nil {
		return ConnectionView{}, err
	}
	u, err := models.UserGetById(ctxpkg.NewContext(ctx, db, true), actor)
	if err != nil {
		return ConnectionView{}, err
	}
	if u == nil || u.IsDisabled() {
		return ConnectionView{}, denied()
	}
	if !u.IsAdmin() {
		mine, err := models.MyBusiGroupIds(ctxpkg.NewContext(ctx, db, true), actor)
		if err != nil {
			return ConnectionView{}, err
		}
		allowed := false
		for _, id := range groups {
			if slices.Contains(mine, id) {
				allowed = true
			}
		}
		if !allowed {
			return ConnectionView{}, denied()
		}
	}
	var c models.OpsSSHConnection
	err = db.First(&c, "target_ident = ?", ident).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ConnectionView{Ident: ident, ConnectionInput: ConnectionInput{LogPaths: []string{}}}, nil
	}
	if err != nil {
		return ConnectionView{}, err
	}
	if !u.IsAdmin() {
		var group models.BusiGroup
		if err := db.First(&group, "id = ?", c.GroupID).Error; err != nil {
			return ConnectionView{}, denied()
		}
		ok, err := u.CanDoBusiGroup(ctxpkg.NewContext(ctx, db, true), &group)
		if err != nil || !ok {
			return ConnectionView{}, denied()
		}
	}
	out := view(c)
	out.Stale = c.TargetID != t.Id || !slices.Contains(groups, c.GroupID)
	if out.Stale && !u.IsAdmin() {
		return ConnectionView{}, denied()
	}
	return out, nil
}
func (s *Store) DeleteConnection(ctx context.Context, actor int64, ident string, version int64) error {
	return s.write(ctx, func(tx *gorm.DB) error {
		var c models.OpsSSHConnection
		if err := tx.First(&c, "target_ident = ?", ident).Error; err != nil {
			return missing()
		}
		authorizedGroup := c.GroupID
		if err := access(tx, actor, c.GroupID, PermConfigure, true); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "target_ident = ?", ident).Error; err != nil {
			return missing()
		}
		if c.GroupID != authorizedGroup {
			if err := access(tx, actor, c.GroupID, PermConfigure, true); err != nil {
				return err
			}
		}
		if c.RecordVersion != version {
			return conflict()
		}
		if err := tx.Delete(&c).Error; err != nil {
			return err
		}
		for _, id := range []string{c.CredentialID, c.JumpCredentialID} {
			if err := retireDeviceCredential(tx, id); err != nil {
				return err
			}
		}
		return audit(tx, actor, c.GroupID, ident, "connection_deleted")
	})
}

// Resolve is the only decrypting interface and is used solely by SSH transport.
// The authorization callback runs before any credential is opened.
func (s *Store) Resolve(ctx context.Context, ident string, authorize func(int64) error) (Connection, error) {
	return s.resolve(ctx, ident, ProtocolSSH, authorize)
}

func (s *Store) resolve(ctx context.Context, ident, protocol string, authorize func(int64) error) (Connection, error) {
	var out Connection
	if authorize == nil {
		return out, denied()
	}
	db := s.DB.WithContext(ctx)
	var c models.OpsSSHConnection
	if err := db.First(&c, "target_ident = ?", ident).Error; err != nil {
		return out, missing()
	}
	t, groups, err := target(db, ident)
	if err != nil {
		return out, err
	}
	if c.TargetID != t.Id || !slices.Contains(groups, c.GroupID) || !c.Enabled {
		return out, denied()
	}
	if connectionProtocol(c.Protocol) != protocol {
		return out, invalid("this protocol is not configured for the device")
	}
	if err = authorize(c.GroupID); err != nil {
		return out, err
	}
	open := func(id, host, fp string, port int) (Endpoint, error) {
		var credential models.OpsCredential
		if err := db.First(&credential, "id = ?", id).Error; err != nil {
			return Endpoint{}, missing()
		}
		if credential.GroupID != c.GroupID || credential.Disabled {
			return Endpoint{}, denied()
		}
		v, err := s.secret(credential)
		return Endpoint{Host: host, Port: port, Fingerprint: fp, Username: v.Username, Password: v.Password}, err
	}
	out = Connection{Ident: ident, Protocol: protocol, GroupID: c.GroupID, LogPaths: view(c).LogPaths}
	out.Target, err = open(c.CredentialID, c.Host, c.Fingerprint, c.Port)
	if err != nil {
		return Connection{}, err
	}
	if c.JumpHost != "" {
		jump, err := open(c.JumpCredentialID, c.JumpHost, c.JumpFingerprint, c.JumpPort)
		if err != nil {
			return Connection{}, err
		}
		out.Jump = &jump
	}
	return out, nil
}

// Connections includes obsolete bindings so operators can detach a deleted
// device without losing the ability to retire its referenced credential.
func (s *Store) Connections(ctx context.Context, actor int64, credentialID string) ([]ConnectionView, error) {
	db := s.DB.WithContext(ctx)
	var credential models.OpsCredential
	if err := db.First(&credential, "id = ?", credentialID).Error; err != nil {
		return nil, missing()
	}
	if err := access(db, actor, credential.GroupID, PermConfigure, true); err != nil {
		return nil, err
	}
	var rows []models.OpsSSHConnection
	if err := db.Where("credential_id = ? OR jump_credential_id = ?", credentialID, credentialID).Order("target_ident").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ConnectionView, 0, len(rows))
	for _, row := range rows {
		out = append(out, view(row))
	}
	return out, nil
}
