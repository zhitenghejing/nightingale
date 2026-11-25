package collection

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/remoteaccess"
	"github.com/stretchr/testify/require"
)

func TestDeviceAndSSHCredentialSaveAtomically(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	require.NoError(t, s.DB.AutoMigrate(models.OpsAccessTables()...))
	require.NoError(t, s.DB.Create(&models.User{Id: 1, Username: "root", Roles: "Admin"}).Error)
	s.RemoteAccess = &remoteaccess.Store{DB: s.DB, Keys: &remoteaccess.Keyring{Active: "test", Keys: map[string]string{"test": base64.StdEncoding.EncodeToString(make([]byte, 32))}}}
	f := deviceInput(c, cred, "192.0.2.22")
	ssh := remoteaccess.ConnectionInput{Enabled: true, GroupID: 1, Target: remoteaccess.EndpointInput{Host: f.Address, Port: 22, Fingerprint: "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", NewCredential: &remoteaccess.CredentialInput{Username: "readonly", Password: "SSH-synthetic-password-73892"}}, LogPaths: []string{"/var/log/test.log"}}
	f.RemoteAccess = &ssh
	d, e := s.SaveDevice(scope, "", "atomic-ssh-request", f)
	require.NoError(t, e)
	got, e := s.RemoteAccess.Resolve(context.Background(), d.Ident, func(int64) error { return nil })
	require.NoError(t, e)
	require.Equal(t, "SSH-synthetic-password-73892", got.Target.Password)
	// Same idempotency key returns the same device without duplicating secrets.
	again, e := s.SaveDevice(scope, "", "atomic-ssh-request", f)
	require.NoError(t, e)
	require.Equal(t, d.ID, again.ID)
	var count int64
	require.NoError(t, s.DB.Model(&models.OpsCredential{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
	var audits []models.MonitorAudit
	require.NoError(t, s.DB.Find(&audits).Error)
	b, e := json.Marshal(audits)
	require.NoError(t, e)
	require.NotContains(t, string(b), "SSH-synthetic-password")
	require.NotContains(t, string(b), "remote_access")
	require.NotContains(t, d.Parameters, "remote_access")
	// A rejected credential rolls back the device, Target, binding and publication.
	f.Address = "192.0.2.23"
	f.Ident = "192.0.2.23"
	f.RemoteAccess.Target.NewCredential.Password = ""
	_, e = s.SaveDevice(scope, "", "failed-ssh-request", f)
	require.ErrorContains(t, e, "password are required")
	require.NoError(t, s.DB.Model(&models.Target{}).Where("ident = ?", f.Ident).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, s.DB.Model(&models.MonitorDevice{}).Where("ident = ?", f.Ident).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, s.DB.Model(&models.OpsCredential{}).Count(&count).Error)
	require.Equal(t, int64(1), count)
}
