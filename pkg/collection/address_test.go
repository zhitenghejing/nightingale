package collection

import (
	"fmt"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/stretchr/testify/require"
)

func TestDeviceAddressesRequireOnlyIPSyntax(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	for i, address := range []string{"198.51.100.10", "192.0.2.0", "192.0.2.255", "127.0.0.1", "169.254.1.2", "0.0.0.0", "224.0.0.1", "255.255.255.255", "::1", "2001:db8::10"} {
		f := deviceInput(c, cred, address)
		f.Enabled = false
		_, err := s.SaveDevice(scope, "", fmt.Sprintf("valid-address-%d", i), f)
		require.NoError(t, err, address)
	}
	for _, address := range []string{"", "host.example", "192.0.2.256", "192.0.2", "192.000.2.1", "2001:::1"} {
		f := deviceInput(c, cred, address)
		require.Error(t, f.normalize(), address)
	}
}

func TestRepeatedDeviceAddressesHaveIndependentIdentities(t *testing.T) {
	s, scope, c, cred, secret := testStore(t)
	f := deviceInput(c, cred, "192.0.2.10")
	first, err := s.SaveDevice(scope, "", "repeated-address-first", f)
	require.NoError(t, err)
	second, err := s.SaveDevice(scope, "", "repeated-address-second", f)
	require.NoError(t, err)
	require.Equal(t, first.Address, second.Address)
	require.NotEqual(t, first.ID, second.ID)
	require.NotEqual(t, first.Ident, second.Ident)
	again, err := s.SaveDevice(scope, "", "repeated-address-second", f)
	require.NoError(t, err)
	require.Equal(t, second.ID, again.ID)
	updated := inputFromDevice(second)
	updated.Address = "198.51.100.10"
	second, err = s.SaveDevice(scope, second.ID, "", updated)
	require.NoError(t, err)
	updated = inputFromDevice(second)
	updated.Address = first.Address
	second, err = s.SaveDevice(scope, second.ID, "", updated)
	require.NoError(t, err)
	snapshot, _, err := s.Provider(c.ID, secret, "")
	require.NoError(t, err)
	require.Len(t, snapshot.Configs["snmp"], 2)
	first, err = s.SetDeviceState(scope, first.ID, first.RecordVersion, false, true)
	require.NoError(t, err)
	second, err = s.Device(scope, second.ID)
	require.NoError(t, err)
	require.True(t, second.Enabled)
	require.False(t, second.Deleted)
	_, err = s.SaveDevice(scope, "", "repeated-address-after-delete", f)
	require.NoError(t, err)
}

func TestAddressMatchingAgentDoesNotTakeOverItsTarget(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	const address = "192.0.2.20"
	require.NoError(t, s.DB.Create(&models.Target{Ident: address, Note: "Existing agent", AgentVersion: "original"}).Error)
	d, err := s.SaveDevice(scope, "", "agent-address-repeated", deviceInput(c, cred, address))
	require.NoError(t, err)
	require.NotEqual(t, address, d.Ident)
	var agent models.Target
	require.NoError(t, s.DB.First(&agent, "ident = ?", address).Error)
	require.Equal(t, "original", agent.AgentVersion)
	require.Equal(t, "Existing agent", agent.Note)
}

func TestProbeAcceptsAnAddressOutsideLegacyCollectorNetworks(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	probe, err := s.CreateProbe(scope, deviceInput(c, cred, "198.51.100.22"))
	require.NoError(t, err)
	require.Equal(t, "waiting", probe.State)
}

func TestCollectorRegistrationNeedsNoAddressAllowlist(t *testing.T) {
	s, scope, _, _, _ := testStore(t)
	c, _, err := s.CreateCollector(scope, CollectorInput{Name: "No address allowlist", AgentIdent: "new-agent", DatasourceID: 1, GroupIDs: []int64{1}, WriterUser: "other-writer"})
	require.NoError(t, err)
	_, err = s.UpdateCollector(scope, c.ID, CollectorInput{Name: c.Name, AgentIdent: c.AgentIdent, DatasourceID: c.DatasourceID, GroupIDs: []int64{1}, WriterUser: c.WriterUser, RecordVersion: c.RecordVersion})
	require.NoError(t, err)
}

type legacyEndpointDevice struct {
	models.MonitorDevice
	GroupEndpoint     string `gorm:"size:64;unique"`
	CollectorEndpoint string `gorm:"size:64;unique"`
}

func (legacyEndpointDevice) TableName() string { return "monitor_device" }

func verifyLegacyAddressUpgrade(t *testing.T, s *Store, scope Scope, c models.MonitorCollector, cred models.MonitorCredential) {
	t.Helper()
	require.NoError(t, s.DB.AutoMigrate(&legacyEndpointDevice{}))
	input := deviceInput(c, cred, "192.0.2.90")
	input.Enabled = false
	before, err := s.SaveDevice(scope, "", "legacy-address-before", input)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		require.NoError(t, s.Migrate())
	}
	var preserved models.MonitorDevice
	require.NoError(t, s.DB.First(&preserved, "id = ?", before.ID).Error)
	require.Equal(t, before.Ident, preserved.Ident)
	require.Equal(t, before.CredentialID, preserved.CredentialID)
	require.Equal(t, before.DesiredRevision, preserved.DesiredRevision)
	after, err := s.SaveDevice(scope, "", "legacy-address-after", input)
	require.NoError(t, err)
	require.NotEqual(t, before.Ident, after.Ident)
	require.Equal(t, before.GroupEndpoint, after.GroupEndpoint)
	require.Equal(t, before.CollectorEndpoint, after.CollectorEndpoint)
}

func TestLegacyAddressConstraintsMigrateWithoutLosingRecords(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	verifyLegacyAddressUpgrade(t, s, scope, c, cred)
}
