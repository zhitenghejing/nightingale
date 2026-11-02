package collection

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/prometheus/common/model"
	"golang.org/x/sync/singleflight"
)

type State struct {
	DeviceID           string   `json:"device_id"`
	Kind               string   `json:"kind"`
	ConfigurationState string   `json:"configuration_state"`
	Health             string   `json:"health"`
	CollectorID        string   `json:"collector_id"`
	Revision           string   `json:"revision"`
	LastSampleAt       int64    `json:"last_sample_at"`
	CheckedAt          int64    `json:"checked_at"`
	CPUPercent         *float64 `json:"cpu_percent"`
	MemoryPercent      *float64 `json:"memory_percent"`
	MissingMetrics     []string `json:"missing_metrics"`
	CounterBits        int      `json:"counter_bits"`
}
type QueryFunc func(context.Context, int64, string, time.Time) (model.Value, error)
type statusResult struct {
	at     time.Time
	matrix model.Matrix
	err    error
}
type StatusService struct {
	Store         *Store
	Query         QueryFunc
	StaleSeconds  int
	FutureSeconds int
	mu            sync.Mutex
	cache         map[string]statusResult
	previous      map[string]State
	calls         singleflight.Group
}

func (s *StatusService) fetch(ctx context.Context, ds int64, devices []models.MonitorDevice, now time.Time) (model.Matrix, error) {
	ids := []string{}
	revisions := []string{}
	metricNames := []string{"snmp_up", "snmp_health_state", "mysql_up"}
	for _, d := range devices {
		ids = append(ids, regexp.QuoteMeta(d.ID))
		revisions = append(revisions, d.ID+":"+d.CollectorID+":"+d.DesiredRevision)
		cfg, err := s.Store.TemplateConfig(d.TemplateVersionID)
		if err != nil {
			return nil, err
		}
		metricNames = append(metricNames, cfg.MetricNames()...)
	}
	sort.Strings(ids)
	sort.Strings(revisions)
	key := fmt.Sprint(ds) + ":" + digest(canonical(revisions)) + ":" + fmt.Sprint(now.Unix()/10)
	value, _, _ := s.calls.Do(key, func() (interface{}, error) {
		s.mu.Lock()
		if r, ok := s.cache[key]; ok && now.Sub(r.at) < 10*time.Second {
			s.mu.Unlock()
			return r, nil
		}
		s.mu.Unlock()
		q := fmt.Sprintf(`{__name__=~%q,collection_device_id=~%q,collection_mode="managed"}[10m]`, strings.Join(sortedUnique(metricNames), "|"), strings.Join(ids, "|"))
		var matrix model.Matrix
		var err error
		if s.Query == nil {
			err = errors.New("collection datasource unavailable")
		} else {
			var v model.Value
			timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			v, err = s.Query(timeout, ds, q, now)
			if err == nil {
				var ok bool
				matrix, ok = v.(model.Matrix)
				if !ok {
					err = errors.New("collection datasource returned an unexpected result")
				}
			}
		}
		result := statusResult{now, matrix, err}
		s.mu.Lock()
		if s.cache == nil || len(s.cache) > 200 {
			s.cache = map[string]statusResult{}
		}
		s.cache[key] = result
		s.mu.Unlock()
		return result, nil
	})
	r := value.(statusResult)
	return r.matrix, r.err
}
func (s *StatusService) States(ctx context.Context, devices []models.MonitorDevice) (map[string]State, error) {
	now := time.Now()
	out := map[string]State{}
	if len(devices) == 0 {
		return out, nil
	}
	ids := []string{}
	for _, d := range devices {
		ids = append(ids, d.CollectorID)
	}
	var collectors []models.MonitorCollector
	if err := s.Store.DB.Where("id IN ?", sortedUnique(ids)).Find(&collectors).Error; err != nil {
		return nil, err
	}
	byID := map[string]models.MonitorCollector{}
	for _, c := range collectors {
		byID[c.ID] = c
	}
	groups := map[int64][]models.MonitorDevice{}
	for _, d := range devices {
		c, ok := byID[d.CollectorID]
		if !ok {
			state := baseState(d, c, now.Unix())
			state.Health = "query_error"
			out[d.ID] = state
			continue
		}
		if !d.Enabled {
			out[d.ID] = baseState(d, c, now.Unix())
			continue
		}
		groups[c.DatasourceID] = append(groups[c.DatasourceID], d)
	}
	for ds, items := range groups {
		for start := 0; start < len(items); start += 50 {
			batch := items[start:min(start+50, len(items))]
			matrix, err := s.fetch(ctx, ds, batch, now)
			for _, d := range batch {
				c := byID[d.CollectorID]
				cfg, templateErr := s.Store.TemplateConfig(d.TemplateVersionID)
				state := evaluate(d, c, matrix, now.Unix(), s.StaleSeconds, s.FutureSeconds, cfg.RequiredMetrics)
				queryErr := err
				if templateErr != nil {
					queryErr = templateErr
				}
				s.mu.Lock()
				if s.previous == nil {
					s.previous = map[string]State{}
				}
				if queryErr != nil {
					if old, ok := s.previous[d.ID+":"+d.DesiredRevision]; ok {
						state.LastSampleAt = old.LastSampleAt
						state.CPUPercent = old.CPUPercent
						state.MemoryPercent = old.MemoryPercent
					}
					state.Health = "query_error"
				} else {
					if len(s.previous) > 10000 {
						s.previous = map[string]State{}
					}
					s.previous[d.ID+":"+d.DesiredRevision] = state
				}
				s.mu.Unlock()
				out[d.ID] = state
				if queryErr == nil && state.Health == "healthy" {
					if err := s.Store.armDevice(d); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	return out, nil
}

// DatasourceUnavailableState exposes configuration metadata without consulting
// metric caches. UI readers denied a datasource must not inherit cached values.
func DatasourceUnavailableState(d models.MonitorDevice, c models.MonitorCollector) State {
	state := baseState(d, c, time.Now().Unix())
	state.Health = "query_error"
	return state
}

func baseState(d models.MonitorDevice, c models.MonitorCollector, now int64) State {
	state := State{DeviceID: d.ID, Kind: "snmp", ConfigurationState: "pending", Health: "waiting", CollectorID: d.CollectorID, Revision: d.DesiredRevision, CheckedAt: now, MissingMetrics: []string{}}
	if d.Protocol == "mysql" {
		state.Kind = "mysql"
	}
	if !d.Enabled {
		state.ConfigurationState = "withdraw_pending"
		state.Health = "withdraw_pending"
		if withdrawn(d, c, now) {
			state.ConfigurationState = "withdraw_observed"
			state.Health = "disabled"
		}
		return state
	}
	if c.LastServedVersion == c.DesiredVersion && c.DesiredVersion != "" {
		state.ConfigurationState = "served"
	}
	if c.LastClientVersion == c.DesiredVersion && c.DesiredVersion != "" {
		state.ConfigurationState = "client_seen"
	}
	return state
}
func evaluate(d models.MonitorDevice, c models.MonitorCollector, matrix model.Matrix, now int64, stale, future int, required ...[]string) State {
	state := baseState(d, c, now)
	if !d.Enabled {
		return state
	}
	if stale <= 0 {
		stale = 90
	}
	threshold := int64(max(3*d.IntervalSeconds, stale))
	if future <= 0 {
		future = 30
	}
	type reading struct {
		value     float64
		timestamp int64
		labels    model.Metric
	}
	readings := map[string][]reading{}
	clockError := d.LastClockErrorAt > 0 && now-d.LastClockErrorAt < threshold
	for _, stream := range matrix {
		m := stream.Metric
		if string(m["collection_device_id"]) != d.ID || string(m["collection_collector_id"]) != d.CollectorID || string(m["collection_revision"]) != d.DesiredRevision || string(m["collection_mode"]) != "managed" {
			continue
		}
		if len(stream.Values) == 0 {
			continue
		}
		last := stream.Values[len(stream.Values)-1]
		timestamp := int64(last.Timestamp) / 1000
		if math.IsNaN(float64(last.Value)) || math.IsInf(float64(last.Value), 0) {
			continue
		}
		state.LastSampleAt = max(state.LastSampleAt, timestamp)
		if timestamp > now+int64(future) {
			clockError = true
			continue
		}
		if now-timestamp > threshold || math.IsNaN(float64(last.Value)) || math.IsInf(float64(last.Value), 0) {
			continue
		}
		name := string(m[model.MetricNameLabel])
		readings[name] = append(readings[name], reading{float64(last.Value), timestamp, m})
	}
	if clockError {
		state.Health = "clock_error"
		return state
	}
	if state.LastSampleAt > 0 {
		state.ConfigurationState = "observed"
	}
	if d.Protocol == "mysql" {
		up, down := false, false
		for _, r := range readings["mysql_up"] {
			up = up || r.value == 1
			down = down || r.value == 0
		}
		if !up {
			if down {
				state.Health = "failed"
			} else if state.LastSampleAt > 0 || now-max(d.CreatedAt, d.UpdatedAt) > threshold {
				state.Health = "interrupted"
			}
			return state
		}
		if len(required) != 0 {
			for _, name := range required[0] {
				if len(readings[name]) == 0 {
					state.MissingMetrics = append(state.MissingMetrics, name)
				}
			}
		}
		state.Health = "healthy"
		if len(state.MissingMetrics) != 0 {
			state.Health = "partial"
		}
		return state
	}
	up := false
	down := false
	for _, r := range readings["snmp_up"] {
		up = up || r.value == 1
		down = down || r.value == 0
	}
	for _, r := range readings["snmp_health_state"] {
		down = down || r.value == 0
	}
	if !up && len(readings["snmp_uptime"]) == 0 {
		if down {
			state.Health = "failed"
		} else if state.LastSampleAt > 0 || now-max(d.CreatedAt, d.UpdatedAt) > threshold {
			state.Health = "interrupted"
		}
		return state
	}
	if len(readings["snmp_uptime"]) == 0 {
		state.MissingMetrics = append(state.MissingMetrics, "uptime")
	}
	cpu := 0.0
	count := 0
	for _, r := range readings["snmp_cpu_load"] {
		if r.value >= 0 && r.value <= 100 {
			cpu += r.value
			count++
		}
	}
	if count > 0 {
		v := cpu / float64(count)
		state.CPUPercent = &v
	} else {
		state.MissingMetrics = append(state.MissingMetrics, "cpu")
	}
	sizes := map[string]float64{}
	for _, r := range readings["snmp_storage_size"] {
		if strings.TrimPrefix(string(r.labels["type"]), ".") == "1.3.6.1.2.1.25.2.1.2" && r.value > 0 {
			sizes[string(r.labels["index"])] = r.value
		}
	}
	for _, r := range readings["snmp_storage_used"] {
		if size, ok := sizes[string(r.labels["index"])]; ok && r.value >= 0 && r.value <= size {
			v := r.value / size * 100
			state.MemoryPercent = &v
			break
		}
	}
	if state.MemoryPercent == nil {
		state.MissingMetrics = append(state.MissingMetrics, "memory")
	}
	if len(readings["snmp_traffic64_in_octets"]) > 0 && len(readings["snmp_traffic64_out_octets"]) > 0 {
		state.CounterBits = 64
	} else if len(readings["snmp_traffic32_in_octets"]) > 0 && len(readings["snmp_traffic32_out_octets"]) > 0 {
		state.CounterBits = 32
	}
	if len(required) > 0 {
		state.MissingMetrics = []string{}
		for _, name := range required[0] {
			if len(readings[name]) == 0 || (name == "snmp_cpu_load" && state.CPUPercent == nil) || ((name == "snmp_storage_size" || name == "snmp_storage_used") && d.TemplateVersionID == WindowsTemplate && state.MemoryPercent == nil) {
				state.MissingMetrics = append(state.MissingMetrics, name)
			}
		}
	}
	state.Health = "healthy"
	if len(state.MissingMetrics) > 0 {
		state.Health = "partial"
	}
	return state
}
