package collection

import (
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/pelletier/go-toml/v2"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/require"
)

func TestICMPDefaultPublicationLifecycle(t *testing.T) {
	s, scope, c, cred, token := testStore(t)
	d, err := s.SaveDevice(scope, "", "icmp-device", deviceInput(c, cred, "192.0.2.10"))
	require.NoError(t, err)
	snapshot, _, err := s.Provider(c.ID, token, "")
	require.NoError(t, err)
	require.Len(t, snapshot.Configs["ping"], 1)
	for hash, content := range snapshot.Configs["ping"] {
		require.Equal(t, digest([]byte(content.Config)), hash)
		var cfg pingConfig
		require.NoError(t, toml.Unmarshal([]byte(content.Config), &cfg))
		require.Equal(t, []string{d.Address}, cfg.Instances[0].Targets)
		require.Equal(t, d.DesiredRevision, cfg.Instances[0].Labels["collection_revision"])
		require.Equal(t, d.ID, cfg.Instances[0].Labels["collection_device_id"])
		require.Equal(t, "native", cfg.Instances[0].Method)
		require.NotContains(t, content.Config, "community")
	}
	require.NoError(t, s.SyncReachability())
	after, _, err := s.Provider(c.ID, token, "")
	require.NoError(t, err)
	require.Equal(t, snapshot.Version, after.Version, "startup is idempotent")
	_, err = s.SetDeviceState(scope, d.ID, d.RecordVersion, false, false)
	require.NoError(t, err)
	after, _, err = s.Provider(c.ID, token, "")
	require.NoError(t, err)
	require.Empty(t, after.Configs, "disable withdraws both protocol and ICMP tasks")
}
func TestICMPSamplesAreOwnedAndCannotEstablishProtocolHealth(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	d, err := s.SaveDevice(scope, "", "icmp-ingest", deviceInput(c, cred, "192.0.2.10"))
	require.NoError(t, err)
	ping := metric(d, "ping_result_code", 0, map[string]string{"target": d.Address})
	out, err := s.FilterSamples(c.WriterUser, []prompb.TimeSeries{ping})
	require.NoError(t, err)
	require.Len(t, out, 1)
	var current models.MonitorDevice
	require.NoError(t, s.DB.First(&current, "id = ?", d.ID).Error)
	require.Zero(t, current.LastReceivedAt, "ping alone cannot establish SNMP/MySQL collection success")
	for _, bad := range []prompb.TimeSeries{metric(d, "ping_result_code", 0, map[string]string{"target": "192.0.2.11"}), metric(d, "ping_invented_metric", 1, map[string]string{"target": d.Address})} {
		out, err = s.FilterSamples(c.WriterUser, []prompb.TimeSeries{bad})
		require.NoError(t, err)
		require.Empty(t, out)
	}
	out, err = s.FilterSamples("other-writer", []prompb.TimeSeries{ping})
	require.NoError(t, err)
	require.Empty(t, out)
	stale := ping
	stale.Labels = append([]prompb.Label(nil), ping.Labels...)
	for i := range stale.Labels {
		if stale.Labels[i].Name == "collection_revision" {
			stale.Labels[i].Value = "old-revision"
		}
	}
	out, err = s.FilterSamples(c.WriterUser, []prompb.TimeSeries{stale})
	require.NoError(t, err)
	require.Empty(t, out)
	require.NoError(t, s.DB.First(&current, "id = ?", d.ID).Error)
	require.NotZero(t, current.LastRejectedAt)
}
func TestICMPCompilerIPv6AndReservedTarget(t *testing.T) {
	f := DeviceInput{Address: "2001:db8::1", Ident: "ipv6-device", CollectorID: "collector", IntervalSeconds: 15, Labels: map[string]string{"target": "forged"}}
	content, err := compilePing(f, "device", "revision")
	require.NoError(t, err)
	var cfg pingConfig
	require.NoError(t, toml.Unmarshal([]byte(content.Config), &cfg))
	require.True(t, cfg.Instances[0].IPv6)
	require.Equal(t, f.Address, cfg.Instances[0].Labels["target"])
	require.Equal(t, 3, cfg.Instances[0].Count)
	require.Equal(t, float64(3), cfg.Instances[0].Timeout)
}
func TestICMPUpgradeKeepsPrimaryConfigurationAndReleaseBytes(t *testing.T) {
	s, scope, c, cred, token := testStore(t)
	d, err := s.SaveDevice(scope, "", "icmp-upgrade", deviceInput(c, cred, "192.0.2.10"))
	require.NoError(t, err)
	snap, _, err := s.Provider(c.ID, token, "")
	require.NoError(t, err)
	delete(snap.Configs, "ping")
	var publication models.MonitorPublication
	require.NoError(t, s.DB.First(&publication, "version = ?", snap.Version).Error)
	data := canonical(snap.Configs)
	sealed, key, err := s.Keys.Seal(data, "publication:"+publication.ID)
	require.NoError(t, err)
	require.NoError(t, s.DB.Model(&publication).Updates(map[string]interface{}{"ciphertext": sealed, "key_id": key, "digest": digest(data)}).Error)
	require.NoError(t, s.SyncReachability())
	after, _, err := s.Provider(c.ID, token, "")
	require.NoError(t, err)
	require.NotEqual(t, snap.Version, after.Version)
	require.Equal(t, snap.Configs["snmp"], after.Configs["snmp"])
	require.Len(t, after.Configs["ping"], 1)
	var current models.MonitorDevice
	require.NoError(t, s.DB.First(&current, "id = ?", d.ID).Error)
	require.Equal(t, d.DesiredRevision, current.DesiredRevision)
	require.Equal(t, d.RecordVersion, current.RecordVersion)
	require.Equal(t, d.CredentialID, current.CredentialID)
	require.NoError(t, s.SyncReachability())
	again, _, err := s.Provider(c.ID, token, "")
	require.NoError(t, err)
	require.Equal(t, after.Version, again.Version)
	require.LessOrEqual(t, current.UpdatedAt, time.Now().Unix())
}
