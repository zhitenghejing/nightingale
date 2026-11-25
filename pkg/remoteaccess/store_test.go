package remoteaccess

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	db, e := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, e)
	pool, e := db.DB()
	require.NoError(t, e)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { pool.Close() })
	return seed(t, db)
}
func seed(t *testing.T, db *gorm.DB) *Store {
	require.NoError(t, db.AutoMigrate(models.OpsAccessTables()...))
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.BusiGroup{}, &models.Target{}, &models.TargetBusiGroup{}, &models.RoleOperation{}, &models.BusiGroupMember{}, &models.UserGroupMember{}))
	require.NoError(t, db.Create(&models.User{Id: 1, Username: "root", Roles: "Admin"}).Error)
	require.NoError(t, db.Create(&models.User{Id: 2, Username: "operator", Roles: "SSH"}).Error)
	for _, id := range []int64{1, 2} {
		require.NoError(t, db.Create(&models.BusiGroup{Id: id}).Error)
	}
	require.NoError(t, db.Create(&models.Target{Id: 1, Ident: "web-01"}).Error)
	require.NoError(t, db.Create(&models.TargetBusiGroup{TargetIdent: "web-01", GroupId: 1}).Error)
	return &Store{DB: db, Keys: &Keyring{Active: "test", Keys: map[string]string{"test": base64.StdEncoding.EncodeToString(make([]byte, 32))}}}
}
func credentialInput() CredentialInput {
	return CredentialInput{Name: "web read only", GroupID: 1, Username: "diagnostic-user", Password: "synthetic-secret-9812"}
}
func connectionInput(id string) ConnectionInput {
	return ConnectionInput{Enabled: true, GroupID: 1, Target: EndpointInput{Host: "192.0.2.12", Port: 22, Fingerprint: "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", CredentialID: id}, LogPaths: []string{"/var/log/test.log"}}
}
func TestCredentialLifecycleAndBinding(t *testing.T) { exerciseLifecycle(t, fixture(t)) }

func TestConnectionWithoutLogPathsReturnsEmptyArray(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	credential, err := s.SaveCredential(ctx, 1, "", credentialInput())
	require.NoError(t, err)
	input := connectionInput(credential.ID)
	input.LogPaths = nil
	saved, err := s.SaveConnection(ctx, 1, "web-01", input)
	require.NoError(t, err)
	require.Equal(t, []string{}, saved.LogPaths)
	loaded, err := s.GetConnection(ctx, 1, "web-01")
	require.NoError(t, err)
	require.Contains(t, string(mustJSON(t, loaded)), `"log_paths":[]`)
}

func exerciseLifecycle(t *testing.T, s *Store) {
	ctx := context.Background()
	c, e := s.SaveCredential(ctx, 1, "", credentialInput())
	require.NoError(t, e)
	var row models.OpsCredential
	require.NoError(t, s.DB.First(&row, "id = ?", c.ID).Error)
	for _, v := range []string{row.Ciphertext, string(mustJSON(t, c))} {
		require.NotContains(t, v, "synthetic-secret")
		require.NotContains(t, v, "diagnostic-user")
	}
	require.Equal(t, int64(1), c.RecordVersion)
	input := connectionInput(c.ID)
	conn, e := s.SaveConnection(ctx, 1, "web-01", input)
	require.NoError(t, e)
	require.Nil(t, conn.Target.NewCredential)
	called := false
	got, e := s.Resolve(ctx, "web-01", func(group int64) error { called = true; require.Equal(t, int64(1), group); return nil })
	require.NoError(t, e)
	require.True(t, called)
	require.Equal(t, credentialInput().Password, got.Target.Password)
	require.NotContains(t, string(mustJSON(t, got)), credentialInput().Password)
	// A denied caller cannot trigger even a failed decryption.
	s.Keys.Keys["test"] = base64.StdEncoding.EncodeToString(make([]byte, 31))
	_, e = s.Resolve(ctx, "web-01", func(int64) error { return errors.New("caller rejected") })
	require.EqualError(t, e, "caller rejected")
	s.Keys.Keys["test"] = base64.StdEncoding.EncodeToString(make([]byte, 32))
	_, e = s.SaveConnection(ctx, 1, "web-01", input)
	require.ErrorContains(t, e, "version changed")
	require.ErrorContains(t, s.DeleteCredential(ctx, 1, c.ID, 1), "referenced")
	// Blank values preserve secrets; disable revokes every new resolution.
	disabled := true
	c, e = s.SaveCredential(ctx, 1, c.ID, CredentialInput{Name: c.Name, GroupID: 1, RecordVersion: 1, Disabled: &disabled})
	require.NoError(t, e)
	_, e = s.Resolve(ctx, "web-01", func(int64) error { return nil })
	require.ErrorContains(t, e, "denied")
	disabled = false
	c, e = s.SaveCredential(ctx, 1, c.ID, CredentialInput{Name: c.Name, GroupID: 1, RecordVersion: c.RecordVersion, Disabled: &disabled})
	require.NoError(t, e)
	got, e = s.Resolve(ctx, "web-01", func(int64) error { return nil })
	require.NoError(t, e)
	require.Equal(t, credentialInput().Password, got.Target.Password)
	// AEAD binds ciphertext to credential identity and business group.
	tampered := row
	tampered.ID = uuid.NewString()
	_, e = s.secret(tampered)
	require.ErrorContains(t, e, "decryption failed")
	tampered = row
	tampered.GroupID = 2
	_, e = s.secret(tampered)
	require.ErrorContains(t, e, "decryption failed")
	// Target movement or recreation never inherits an old SSH credential.
	require.NoError(t, s.DB.Where("target_ident = ?", "web-01").Delete(&models.TargetBusiGroup{}).Error)
	_, e = s.Resolve(ctx, "web-01", func(int64) error { return nil })
	require.ErrorContains(t, e, "denied")
	require.NoError(t, s.DB.Create(&models.TargetBusiGroup{TargetIdent: "web-01", GroupId: 1}).Error)
	require.NoError(t, s.DB.Model(&models.Target{}).Where("ident = ?", "web-01").Update("id", 20).Error)
	_, e = s.Resolve(ctx, "web-01", func(int64) error { return nil })
	require.ErrorContains(t, e, "denied")
	stale, e := s.GetConnection(ctx, 1, "web-01")
	require.NoError(t, e)
	require.True(t, stale.Stale)
	require.NoError(t, s.DeleteConnection(ctx, 1, "web-01", conn.RecordVersion))
	require.NoError(t, s.DeleteCredential(ctx, 1, c.ID, c.RecordVersion))
	var audits []models.OpsAccessAudit
	require.NoError(t, s.DB.Find(&audits).Error)
	require.Greater(t, len(audits), 4)
	require.NotContains(t, string(mustJSON(t, audits)), credentialInput().Password)
}
func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	require.NoError(t, e)
	return b
}
func TestIndependentPermissionsAndCrossGroupIsolation(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	_, e := s.SaveCredential(ctx, 2, "", credentialInput())
	require.ErrorContains(t, e, "denied")
	for _, op := range []string{PermConfigure, PermCredentials, PermExecute} {
		require.NoError(t, s.DB.Create(&models.RoleOperation{RoleName: "SSH", Operation: op}).Error)
	}
	_, e = s.SaveCredential(ctx, 2, "", credentialInput())
	require.ErrorContains(t, e, "denied")
	require.NoError(t, s.DB.Create(&models.UserGroupMember{UserId: 2, GroupId: 9}).Error)
	require.NoError(t, s.DB.Create(&models.BusiGroupMember{BusiGroupId: 1, UserGroupId: 9, PermFlag: "ro"}).Error)
	_, e = s.SaveCredential(ctx, 2, "", credentialInput())
	require.ErrorContains(t, e, "denied")
	require.NoError(t, s.DB.Model(&models.BusiGroupMember{}).Where("user_group_id=9").Update("perm_flag", "rw").Error)
	c, e := s.SaveCredential(ctx, 2, "", credentialInput())
	require.NoError(t, e)
	f := credentialInput()
	f.GroupID = 2
	f.Name = "other"
	other, e := s.SaveCredential(ctx, 1, "", f)
	require.NoError(t, e)
	rows, e := s.Credentials(ctx, 2, 0)
	require.NoError(t, e)
	require.Len(t, rows, 1)
	_, e = s.SaveConnection(ctx, 2, "web-01", connectionInput(other.ID))
	require.ErrorContains(t, e, "denied")
	input := connectionInput(c.ID)
	input.Jump = &EndpointInput{Host: "192.0.2.13", Port: 22, Fingerprint: input.Target.Fingerprint, CredentialID: c.ID}
	_, e = s.SaveConnection(ctx, 2, "web-01", input)
	require.NoError(t, e)
	got, e := s.Resolve(ctx, "web-01", func(int64) error { return nil })
	require.NoError(t, e)
	require.NotNil(t, got.Jump)
	require.Equal(t, got.Target.Password, got.Jump.Password)
	require.NoError(t, s.DB.Where("role_name=? AND operation=?", "SSH", PermCredentials).Delete(&models.RoleOperation{}).Error)
	_, e = s.SaveCredential(ctx, 2, c.ID, CredentialInput{Name: c.Name, GroupID: 1, RecordVersion: 1})
	require.ErrorContains(t, e, "denied")
	require.NoError(t, s.DB.Model(&models.User{}).Where("id=2").Update("disabled", 1).Error)
	_, e = s.GetConnection(ctx, 2, "web-01")
	require.ErrorContains(t, e, "denied")
}
func TestKeyFileProtectionAndRotation(t *testing.T) {
	s := fixture(t)
	c, e := s.SaveCredential(context.Background(), 1, "", credentialInput())
	require.NoError(t, e)
	s.Keys.Keys["next"] = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("n", 32)))
	s.Keys.Active = "next"
	rotated, e := s.SaveCredential(context.Background(), 1, c.ID, CredentialInput{Name: c.Name, GroupID: 1, RecordVersion: 1})
	require.NoError(t, e)
	var row models.OpsCredential
	require.NoError(t, s.DB.First(&row, "id = ?", rotated.ID).Error)
	require.Equal(t, "next", row.KeyID)
	value, e := s.secret(row)
	require.NoError(t, e)
	require.Equal(t, credentialInput().Password, value.Password)
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")
	require.NoError(t, os.WriteFile(path, mustJSON(t, s.Keys), 0600))
	_, e = LoadKeyring(path)
	require.NoError(t, e)
	require.NoError(t, os.Chmod(path, 0644))
	_, e = LoadKeyring(path)
	require.ErrorContains(t, e, "private regular")
	require.NoError(t, os.Chmod(path, 0600))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(path, link))
	_, e = LoadKeyring(link)
	require.ErrorContains(t, e, "private regular")
}

func TestRevocationDoesNotRequireDecryptingTheSecret(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	c, e := s.SaveCredential(ctx, 1, "", credentialInput())
	require.NoError(t, e)
	_, e = s.SaveConnection(ctx, 1, "web-01", connectionInput(c.ID))
	require.NoError(t, e)
	delete(s.Keys.Keys, s.Keys.Active)
	disabled := true
	_, e = s.SaveCredential(ctx, 1, c.ID, CredentialInput{Name: c.Name, GroupID: 1, RecordVersion: c.RecordVersion, Disabled: &disabled})
	require.NoError(t, e)
	_, e = s.Resolve(ctx, "web-01", func(int64) error { return nil })
	require.ErrorContains(t, e, "denied")
}
func TestOrphanConnectionCanBeDetached(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	c, e := s.SaveCredential(ctx, 1, "", credentialInput())
	require.NoError(t, e)
	_, e = s.SaveConnection(ctx, 1, "web-01", connectionInput(c.ID))
	require.NoError(t, e)
	require.NoError(t, s.DB.Where("ident = ?", "web-01").Delete(&models.Target{}).Error)
	rows, e := s.Connections(ctx, 1, c.ID)
	require.NoError(t, e)
	require.Len(t, rows, 1)
	require.NoError(t, s.DeleteConnection(ctx, 1, rows[0].Ident, rows[0].RecordVersion))
	require.NoError(t, s.DeleteCredential(ctx, 1, c.ID, c.RecordVersion))
}
func exerciseConcurrentEdits(t *testing.T, s *Store) {
	ctx := context.Background()
	c, e := s.SaveCredential(ctx, 1, "", credentialInput())
	require.NoError(t, e)
	other := &Store{DB: s.DB, Keys: s.Keys}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, store := range []*Store{s, other} {
		go func(store *Store) {
			<-start
			_, err := store.SaveCredential(ctx, 1, c.ID, CredentialInput{Name: "concurrent rename", GroupID: 1, RecordVersion: 1})
			results <- err
		}(store)
	}
	close(start)
	one, two := <-results, <-results
	if one == nil {
		require.ErrorContains(t, two, "version changed")
	} else {
		require.NoError(t, two)
		require.ErrorContains(t, one, "version changed")
	}
}

func TestSQLiteInitializationSQL(t *testing.T) {
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, e)
	pool, e := db.DB()
	require.NoError(t, e)
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	b, e := os.ReadFile("../../doc/sql/remote-access.sqlite.sql")
	require.NoError(t, e)
	for i := 0; i < 2; i++ {
		for _, statement := range strings.Split(string(b), ";") {
			if strings.TrimSpace(statement) != "" {
				require.NoError(t, db.Exec(statement).Error)
			}
		}
	}
	exerciseLifecycle(t, seed(t, db))
}
