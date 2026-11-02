package collection

import (
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/require"
)

func TestDeviceNameEnrichmentUsesOwnedCurrentNameWithoutChangingPublication(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	input := deviceInput(c, cred, "192.0.2.41")
	input.Name = "北京 Windows 服务器"
	d, err := s.SaveDevice(scope, "", "device-name-owned", input)
	require.NoError(t, err)
	for _, name := range []string{"snmp_up", "snmp_cpu_load", "ping_result_code"} {
		incoming := metric(d, name, 1, map[string]string{"device_name": "forged label"})
		if name == "ping_result_code" {
			incoming.Labels = append(incoming.Labels, prompb.Label{Name: "target", Value: d.Address})
		}
		filtered, err := s.FilterSamples(c.WriterUser, []prompb.TimeSeries{incoming})
		require.NoError(t, err)
		require.Len(t, filtered, 1)
		got := map[string]string{}
		for _, l := range filtered[0].Labels {
			got[l.Name] = l.Value
		}
		require.Equal(t, d.Name, got["device_name"])
		require.Equal(t, d.Ident, got["ident"])
		for _, l := range incoming.Labels {
			if l.Name == "device_name" {
				require.Equal(t, "forged label", l.Value, "caller input must remain unchanged")
			}
		}
	}
	beforePublication := d.PublicationVersion
	beforeRevision := d.DesiredRevision
	update := inputFromDevice(d)
	update.Name = "改名后的 Windows"
	renamed, err := s.SaveDevice(scope, d.ID, "", update)
	require.NoError(t, err)
	require.Equal(t, beforeRevision, renamed.DesiredRevision)
	require.Equal(t, beforePublication, renamed.PublicationVersion)
	filtered, err := s.FilterSamples(c.WriterUser, []prompb.TimeSeries{metric(d, "snmp_up", 1, nil)})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Contains(t, filtered[0].Labels, prompb.Label{Name: "device_name", Value: update.Name})
	bad := metric(d, "snmp_up", 1, nil)
	for i := range bad.Labels {
		if bad.Labels[i].Name == "collection_revision" {
			bad.Labels[i].Value = "stale"
		}
	}
	filtered, err = s.FilterSamples(c.WriterUser, []prompb.TimeSeries{bad})
	require.NoError(t, err)
	require.Empty(t, filtered)
	filtered, err = s.FilterSamples("other-writer", []prompb.TimeSeries{metric(d, "snmp_up", 1, nil)})
	require.NoError(t, err)
	require.Empty(t, filtered)
	ordinary := prompb.TimeSeries{Labels: []prompb.Label{{Name: "__name__", Value: "cpu_usage"}, {Name: "ident", Value: "ordinary-host"}}}
	filtered, err = s.FilterSamples(c.WriterUser, []prompb.TimeSeries{ordinary})
	require.NoError(t, err)
	require.Equal(t, []prompb.TimeSeries{ordinary}, filtered)
	input.Labels = map[string]string{"device_name": "manual override"}
	_, err = s.SaveDevice(scope, "", "reserved-device-name", input)
	require.Error(t, err)
	// MySQL samples use the same authoritative enrichment path.
	require.NoError(t, s.DB.Model(&models.MonitorDevice{}).Where("id = ?", d.ID).Update("protocol", "mysql").Error)
	filtered, err = s.FilterSamples(c.WriterUser, []prompb.TimeSeries{metric(d, "mysql_up", 1, nil)})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Contains(t, filtered[0].Labels, prompb.Label{Name: "device_name", Value: update.Name})
}

func TestDeviceNameForAuthorizedNativeAgentICMP(t *testing.T) {
	s, _, c, _, token := testStore(t)
	host := models.Target{Ident: c.AgentIdent, HostIp: "192.0.2.40", AgentVersion: "v0.5.18", OS: "linux"}
	require.NoError(t, s.DB.Create(&host).Error)
	snap, _, err := s.Provider(c.ID, token, "")
	require.NoError(t, err)
	require.Len(t, snap.Configs["ping"], 1)
	for _, content := range snap.Configs["ping"] {
		incoming := agentPingSeries(t, content)
		incoming.Labels = append(incoming.Labels, prompb.Label{Name: "device_name", Value: "forged"})
		accepted, err := s.FilterSamples(c.WriterUser, []prompb.TimeSeries{incoming})
		require.NoError(t, err)
		require.Len(t, accepted, 1)
		require.Contains(t, accepted[0].Labels, prompb.Label{Name: "device_name", Value: host.Ident})
	}
}
