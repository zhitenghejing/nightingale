package collection

import (
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/pelletier/go-toml/v2"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/require"
)

func agentPingSeries(t *testing.T, content ConfigContent) prompb.TimeSeries {
	t.Helper()
	var cfg pingConfig
	require.NoError(t, toml.Unmarshal([]byte(content.Config), &cfg))
	labels := []prompb.Label{{Name: "__name__", Value: "ping_result_code"}}
	for k, v := range cfg.Instances[0].Labels {
		labels = append(labels, prompb.Label{Name: k, Value: v})
	}
	return prompb.TimeSeries{Labels: labels, Samples: []prompb.Sample{{Value: 0, Timestamp: time.Now().UnixMilli()}}}
}
func TestAgentICMPFollowsRegistrationAddressAndDeletion(t *testing.T) {
	s, scope, c, _, token := testStore(t)
	initial, _, err := s.Provider(c.ID, token, "")
	require.NoError(t, err)
	host := models.Target{Ident: c.AgentIdent, HostIp: "192.0.2.40", AgentVersion: "v0.5.18", OS: "linux"}
	require.NoError(t, s.DB.Create(&host).Error)
	snap, _, err := s.Provider(c.ID, token, initial.Version)
	require.NoError(t, err)
	require.NotEqual(t, initial.Version, snap.Version)
	require.Len(t, snap.Configs["ping"], 1)
	var ping prompb.TimeSeries
	for _, content := range snap.Configs["ping"] {
		ping = agentPingSeries(t, content)
	}
	accepted, err := s.FilterSamples(c.WriterUser, []prompb.TimeSeries{ping})
	require.NoError(t, err)
	require.Len(t, accepted, 1)
	for _, change := range []struct{ k, v string }{{"collection_revision", "old"}, {"target", "192.0.2.41"}, {"collection_device_id", "forged"}, {"probe_id", "forged"}, {"ident", "another"}, {"__name__", "cpu_usage_idle"}} {
		bad := ping
		bad.Labels = append([]prompb.Label(nil), ping.Labels...)
		found := false
		for i := range bad.Labels {
			if bad.Labels[i].Name == change.k {
				bad.Labels[i].Value = change.v
				found = true
			}
		}
		if !found {
			bad.Labels = append(bad.Labels, prompb.Label{Name: change.k, Value: change.v})
		}
		accepted, err = s.FilterSamples(c.WriterUser, []prompb.TimeSeries{bad})
		require.NoError(t, err)
		require.Empty(t, accepted, change.k)
	}
	accepted, err = s.FilterSamples("other-writer", []prompb.TimeSeries{ping})
	require.NoError(t, err)
	require.Empty(t, accepted)
	unchanged, same, err := s.Provider(c.ID, token, snap.Version)
	require.NoError(t, err)
	require.True(t, same)
	require.Equal(t, snap.Version, unchanged.Version)
	require.NoError(t, s.DB.Model(&host).Update("host_ip", "2001:db8::40").Error)
	changed, _, err := s.Provider(c.ID, token, snap.Version)
	require.NoError(t, err)
	require.NotEqual(t, snap.Version, changed.Version)
	accepted, err = s.FilterSamples(c.WriterUser, []prompb.TimeSeries{ping})
	require.NoError(t, err)
	require.Empty(t, accepted, "old-address tasks cannot affect current reachability")
	for _, content := range changed.Configs["ping"] {
		var cfg pingConfig
		require.NoError(t, toml.Unmarshal([]byte(content.Config), &cfg))
		require.True(t, cfg.Instances[0].IPv6)
	}
	require.NoError(t, s.DB.Delete(&host).Error)
	withdrawn, _, err := s.Provider(c.ID, token, changed.Version)
	require.NoError(t, err)
	require.Empty(t, withdrawn.Configs)
	require.Error(t, s.DeleteCollector(scope, c.ID, c.RecordVersion), "an empty snapshot is insufficient before its acknowledgement")
	accepted, err = s.FilterSamples(c.WriterUser, []prompb.TimeSeries{ping})
	require.NoError(t, err)
	require.Empty(t, accepted, "deleted hosts cannot be registered by old reserved ping samples")
	var n int64
	require.NoError(t, s.DB.Model(&models.MonitorDevice{}).Count(&n).Error)
	require.Zero(t, n)
}
func TestAgentICMPAssignmentsRespectGroupsAndPreferOwnCollector(t *testing.T) {
	s, scope, c, _, _ := testStore(t)
	second, _, err := s.CreateCollector(scope, CollectorInput{Name: "second", AgentIdent: "own-agent", DatasourceID: 1, GroupIDs: []int64{2}, WriterUser: "other-writer"})
	require.NoError(t, err)
	for _, host := range []models.Target{{Ident: "own-agent", HostIp: "192.0.2.40", AgentVersion: "v1"}, {Ident: "group-agent", HostIp: "192.0.2.41", AgentVersion: "v1"}, {Ident: "unassigned", HostIp: "192.0.2.42", AgentVersion: "v1"}, {Ident: "not-an-agent", HostIp: "192.0.2.43"}, {Ident: "managed", HostIp: "192.0.2.44", AgentVersion: "v1"}} {
		require.NoError(t, s.DB.Create(&host).Error)
	}
	for _, ident := range []string{"own-agent", "group-agent", "not-an-agent", "managed"} {
		require.NoError(t, s.DB.Create(&models.TargetBusiGroup{TargetIdent: ident, GroupId: 1}).Error)
	}
	require.NoError(t, s.DB.Create(&models.MonitorDevice{ID: "reserved", Ident: "managed", RequestKey: "reserved"}).Error)
	owners, err := AgentPingOwners(s.DB, nil)
	require.NoError(t, err)
	require.Len(t, owners, 2)
	require.Equal(t, second.ID, owners["own-agent"].CollectorID, "explicit Agent binding takes priority over fallback group")
	require.Equal(t, c.ID, owners["group-agent"].CollectorID)
	scoped, err := AgentPingOwners(s.DB, []string{"group-agent"})
	require.NoError(t, err)
	require.Len(t, scoped, 1)
	require.NoError(t, s.DB.Where("target_ident = ?", "group-agent").Delete(&models.TargetBusiGroup{}).Error)
	owners, err = AgentPingOwners(s.DB, nil)
	require.NoError(t, err)
	require.NotContains(t, owners, "group-agent")
}
func TestAgentICMPCollectorDeletionWithdrawsBeforeRevocation(t *testing.T) {
	s, scope, c, _, token := testStore(t)
	require.NoError(t, s.DB.Create(&models.Target{Ident: c.AgentIdent, HostIp: "192.0.2.40", AgentVersion: "v1"}).Error)
	snap, _, err := s.Provider(c.ID, token, "")
	require.NoError(t, err)
	require.Len(t, snap.Configs["ping"], 1)
	err = s.DeleteCollector(scope, c.ID, c.RecordVersion)
	require.Error(t, err)
	require.Equal(t, 409, err.(*Error).Code)
	require.NoError(t, s.SyncReachability())
	empty, _, err := s.Provider(c.ID, token, snap.Version)
	require.NoError(t, err)
	require.Empty(t, empty.Configs)
	require.Error(t, s.DeleteCollector(scope, c.ID, c.RecordVersion))
	_, same, err := s.Provider(c.ID, token, empty.Version)
	require.NoError(t, err)
	require.True(t, same)
	require.NoError(t, s.DB.Model(&models.MonitorCollector{}).Where("id = ?", c.ID).Update("client_seen_at", time.Now().Add(-91*time.Second).Unix()).Error)
	require.NoError(t, s.DeleteCollector(scope, c.ID, c.RecordVersion))
	_, _, err = s.Provider(c.ID, token, "")
	require.Error(t, err)
	require.Equal(t, 401, err.(*Error).Code)
}
func TestAgentICMPRollbackRebuildsCurrentNativeHostWithoutDuplicatingIt(t *testing.T) {
	s, scope, c, cred, token := testStore(t)
	require.NoError(t, s.DB.Create(&models.Target{Ident: c.AgentIdent, HostIp: "192.0.2.40", AgentVersion: "v1"}).Error)
	_, err := s.SaveDevice(scope, "", "native-rollback", deviceInput(c, cred, "192.0.2.10"))
	require.NoError(t, err)
	snap, _, err := s.Provider(c.ID, token, "")
	require.NoError(t, err)
	var publication models.MonitorPublication
	require.NoError(t, s.DB.First(&publication, "version = ?", snap.Version).Error)
	require.NoError(t, s.DB.Model(&models.Target{}).Where("ident = ?", c.AgentIdent).Update("host_ip", "192.0.2.41").Error)
	require.NoError(t, s.Rollback(scope, publication.ID))
	current, _, err := s.Provider(c.ID, token, "")
	require.NoError(t, err)
	require.Len(t, current.Configs["ping"], 2)
	for _, content := range current.Configs["ping"] {
		var cfg pingConfig
		require.NoError(t, toml.Unmarshal([]byte(content.Config), &cfg))
		if cfg.Instances[0].Labels["collection_mode"] == "agent" {
			require.Equal(t, []string{"192.0.2.41"}, cfg.Instances[0].Targets)
		}
	}
	var n int64
	require.NoError(t, s.DB.Model(&models.MonitorDevice{}).Count(&n).Error)
	require.EqualValues(t, 1, n)
}
