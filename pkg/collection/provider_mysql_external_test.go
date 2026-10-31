package collection

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ccfos/nightingale/v6/models"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/golang/snappy"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/require"
)

// A real pinned collector consumes the package publication and writes actual
// samples from the explicitly configured disposable test MySQL service.
func TestCategrafMySQLPackage(t *testing.T) {
	binary, dsn := os.Getenv("N9E_TEST_CATEGRAF_BINARY"), os.Getenv("N9E_TEST_MYSQL_DSN")
	if binary == "" || dsn == "" {
		t.Skip("set N9E_TEST_CATEGRAF_BINARY and N9E_TEST_MYSQL_DSN")
	}
	config, err := mysqldriver.ParseDSN(dsn)
	require.NoError(t, err)
	host, port, err := net.SplitHostPort(config.Addr)
	require.NoError(t, err)
	number, err := strconv.Atoi(port)
	require.NoError(t, err)
	s, scope, c, _, token := testStore(t)
	componentID := profileFixture(t, s, "MySQL_84_Remote")
	e, err := s.CatalogEntry(componentID)
	require.NoError(t, err)
	require.NotNil(t, e.Published)
	require.Equal(t, "mysql", e.Published.Definition.Adapter)
	t.Log("published native MySQL 8.4 package -> authenticated HTTP Provider -> actual pinned collector -> managed Remote Write -> complete required metric contract")
	f := DeviceInput{Name: "provider MySQL fixture", Address: host, Port: number, GroupID: 1, CollectorID: c.ID, Protocol: "mysql", TemplateVersionID: e.PublishedVersionID, IntervalSeconds: 15, Enabled: true, Parameters: map[string]interface{}{"username": config.User, "password": config.Passwd, "tls": "off"}}
	d, err := s.SaveDevice(scope, "", "provider-mysql-fixture", f)
	require.NoError(t, err)
	var mu sync.Mutex
	matrix := map[string]*model.SampleStream{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			user, secret, _ := r.BasicAuth()
			snapshot, _, err := s.Provider(user, secret, r.URL.Query().Get("version"))
			if err != nil {
				w.WriteHeader(403)
				return
			}
			_ = json.NewEncoder(w).Encode(snapshot)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		body, err = snappy.Decode(nil, body)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		var request prompb.WriteRequest
		if request.Unmarshal(body) != nil {
			w.WriteHeader(400)
			return
		}
		filtered, err := s.FilterSamples("fixture-writer", request.Timeseries)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, series := range filtered {
			labels := model.Metric{}
			for _, label := range series.Labels {
				labels[model.LabelName(label.Name)] = model.LabelValue(label.Value)
			}
			if labels["collection_device_id"] != model.LabelValue(d.ID) {
				continue
			}
			stream := &model.SampleStream{Metric: labels}
			for _, sample := range series.Samples {
				stream.Values = append(stream.Values, model.SamplePair{Timestamp: model.Time(sample.Timestamp), Value: model.SampleValue(sample.Value)})
			}
			matrix[string(labels["__name__"])] = stream
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	dir := t.TempDir()
	text := fmt.Sprintf(`[global]
hostname = "mysql-package-fixture"
interval = 1
providers = ["http"]
print_configs = false
[log]
file_name = "stdout"
[http_provider]
remote_url = %q
basic_auth_user = %q
basic_auth_pass = %q
timeout = 1
reload_interval = 1
[writer_opt]
batch = 1
chan_size = 10000
[[writers]]
url = %q
timeout = 1000
[http]
enable = false
[heartbeat]
enable = false
[ibex]
enable = false
`, server.URL+"/configs", c.ID, token, server.URL+"/write")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(text), 0600))
	var logs bytes.Buffer
	process := exec.Command(binary, "-configs", dir)
	process.Stdout = &logs
	process.Stderr = &logs
	require.NoError(t, process.Start())
	defer func() { _ = process.Process.Kill(); _ = process.Wait() }()
	contract, err := s.TemplateConfig(d.TemplateVersionID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, name := range contract.RequiredMetrics {
			stream := matrix[name]
			if stream == nil || len(stream.Values) == 0 {
				return false
			}
		}
		return matrix["mysql_up"].Values[0].Value == 1
	}, 30*time.Second, 100*time.Millisecond, "collector must deliver the actual required MySQL metrics with device ownership")
	values := model.Matrix{}
	mu.Lock()
	for _, stream := range matrix {
		values = append(values, stream)
	}
	mu.Unlock()
	status := &StatusService{Store: s, Query: func(context.Context, int64, string, time.Time) (model.Value, error) { return values, nil }}
	states, err := status.States(context.Background(), []models.MonitorDevice{d})
	require.NoError(t, err)
	require.Equal(t, "healthy", states[d.ID].Health)
	probeInput := inputFromDevice(d)
	probeInput.ProbeDeviceID = d.ID
	probe, err := s.CreateProbe(scope, probeInput)
	require.NoError(t, err)
	require.Eventually(t, func() bool { current, err := s.Probe(scope, probe.ID); return err == nil && current.Result == "passed" }, 20*time.Second, 100*time.Millisecond, "MySQL connection probe must use the existing encrypted password and return real up samples")
	var targets int64
	require.NoError(t, s.DB.Model(&models.Target{}).Count(&targets).Error)
	require.EqualValues(t, 1, targets, "temporary probes never register a Target")
	require.NoError(t, s.CancelProbe(scope, probe.ID))
}
