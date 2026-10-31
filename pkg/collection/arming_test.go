package collection

import (
	"context"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestPublishedPackageWaitsForCompleteRevisionBeforeArming(t *testing.T) {
	s, scope, c, cred, _ := testStore(t)
	e := catalogFixture(t, s)
	f := deviceInput(c, cred, "192.0.2.125")
	f.TemplateVersionID = e.PublishedVersionID
	d, err := s.SaveDevice(scope, "", "package-arming-fixture", f)
	require.NoError(t, err)
	var rules []models.AlertRule
	require.NoError(t, s.DB.Find(&rules).Error)
	for _, rule := range rules {
		require.Equal(t, 1, rule.Disabled, "no absent alerts before first successful sample contract")
	}
	service := &StatusService{Store: s, Query: func(context.Context, int64, string, time.Time) (model.Value, error) { return model.Matrix{}, nil }}
	_, err = service.States(context.Background(), []models.MonitorDevice{d})
	require.NoError(t, err)
	state, err := deviceProfileState(d)
	require.NoError(t, err)
	require.NotEmpty(t, state.Waiting)
	config, err := s.TemplateConfig(d.TemplateVersionID)
	require.NoError(t, err)
	matrix := model.Matrix{}
	for _, name := range append(config.RequiredMetrics, "snmp_up") {
		matrix = append(matrix, &model.SampleStream{Metric: model.Metric{"__name__": model.LabelValue(name), "collection_device_id": model.LabelValue(d.ID), "collection_collector_id": model.LabelValue(d.CollectorID), "collection_revision": model.LabelValue(d.DesiredRevision), "collection_mode": "managed"}, Values: []model.SamplePair{{Timestamp: model.Time(time.Now().UnixMilli()), Value: 1}}})
	}
	service = &StatusService{Store: s, Query: func(context.Context, int64, string, time.Time) (model.Value, error) { return matrix, nil }}
	states, err := service.States(context.Background(), []models.MonitorDevice{d})
	require.NoError(t, err)
	require.Equal(t, "healthy", states[d.ID].Health)
	require.NoError(t, s.DB.Find(&rules).Error)
	for _, rule := range rules {
		require.Zero(t, rule.Disabled)
	}
	updated, err := s.Device(scope, d.ID)
	require.NoError(t, err)
	state, err = deviceProfileState(updated)
	require.NoError(t, err)
	require.Empty(t, state.Waiting)
	require.Equal(t, d.DesiredRevision, state.ArmedRevision)
}
