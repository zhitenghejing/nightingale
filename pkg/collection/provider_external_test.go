package collection

import (
	"bytes"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
	"github.com/stretchr/testify/require"
)

// This gate runs the actual pinned collector, not a reimplementation of its provider.
// It uses only loopback endpoints and synthetic credentials; no deployed collector is touched.
func TestCategrafHTTPProvider(t *testing.T) {
	binary := os.Getenv("N9E_TEST_CATEGRAF_BINARY")
	if binary == "" {
		t.Skip("set N9E_TEST_CATEGRAF_BINARY to the pinned v0.5.18 binary")
	}
	binary, err := filepath.Abs(binary)
	require.NoError(t, err)
	v, err := exec.Command(binary, "-version").CombinedOutput()
	require.NoError(t, err)
	require.Contains(t, string(v), "v0.5.18-")
	t.Logf("binary version: %s", strings.TrimSpace(string(v)))
	data, err := os.ReadFile(binary)
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	t.Logf("binary sha256: %x", sum)
	fixture := newSNMPFixture(t)
	var mu sync.Mutex
	version := "initial-empty"
	configs := map[string]map[string]map[string]string{}
	status := 200
	var raw []byte
	var requests []url.Values
	type received struct {
		at     time.Time
		labels map[string]string
		value  float64
	}
	var samples []received
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			body, e := io.ReadAll(r.Body)
			if e != nil {
				w.WriteHeader(400)
				return
			}
			body, e = snappy.Decode(nil, body)
			if e != nil {
				w.WriteHeader(400)
				return
			}
			var req prompb.WriteRequest
			if req.Unmarshal(body) != nil {
				w.WriteHeader(400)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, ts := range req.Timeseries {
				labels := map[string]string{}
				for _, l := range ts.Labels {
					labels[l.Name] = l.Value
				}
				for _, s := range ts.Samples {
					samples = append(samples, received{time.UnixMilli(s.Timestamp), labels, s.Value})
				}
			}
			w.WriteHeader(204)
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "fixture" || pass != "fixture-config-token" {
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, r.URL.Query())
		if status == -1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			if conn != nil {
				_ = conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if raw != nil {
			_, _ = w.Write(raw)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"version": version, "configs": configs})
	}))
	defer server.Close()
	dir := t.TempDir()
	config := fmt.Sprintf(`[global]
hostname = "protocol-fixture"
interval = 1
providers = ["local", "http"]
print_configs = false
[log]
file_name = "stdout"
[http_provider]
remote_url = %q
basic_auth_user = "fixture"
basic_auth_pass = "fixture-config-token"
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
`, server.URL+"/configs", server.URL+"/write")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "input.system"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "input.system/system.toml"), []byte("interval = 1\n"), 0600))
	var proc *exec.Cmd
	var logs bytes.Buffer
	start := func() {
		proc = exec.Command(binary, "-configs", dir)
		proc.Stdout = &logs
		proc.Stderr = &logs
		require.NoError(t, proc.Start())
	}
	stop := func() {
		if proc != nil {
			_ = proc.Process.Kill()
			_ = proc.Wait()
			proc = nil
		}
	}
	defer stop()
	defer func() {
		stop()
		if t.Failed() {
			t.Logf("collector output (synthetic credentials only):\n%s", logs.String())
		}
	}()
	wait := func(name string, f func() bool) {
		t.Helper()
		require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return f() }, 15*time.Second, 100*time.Millisecond, name)
	}
	has := func(id, rev string, since time.Time) bool {
		for _, s := range samples {
			if s.at.After(since) && s.labels["__name__"] == "snmp_uptime" && s.labels["collection_device_id"] == id && s.labels["collection_revision"] == rev {
				return true
			}
		}
		return false
	}
	clientSeen := func(v string) bool {
		for _, q := range requests {
			if q.Get("version") == v {
				return true
			}
		}
		return false
	}
	makeConfig := func(id, rev string) (string, map[string]string) {
		host, port, err := net.SplitHostPort(fixture.LocalAddr().String())
		require.NoError(t, err)
		p, err := strconv.Atoi(port)
		require.NoError(t, err)
		cfg, err := parseTemplate(`required_metrics=["snmp_cpu_load"]
[[table]]
name="cpu"
[[table.field]]
name="load"
oid=".1.3.6.1.2.1.25.3.3.1.2"
`)
		require.NoError(t, err)
		content, err := compile(DeviceInput{Address: host, Port: p, Ident: id, CollectorID: "fixture", TemplateVersionID: "editable-template-fixture", IntervalSeconds: 1, TimeoutSeconds: 1}, id, rev, "fixture-community", "managed", cfg)
		require.NoError(t, err)
		c := content.Config
		h := sha256.Sum256([]byte(c))
		return hex.EncodeToString(h[:]), map[string]string{"format": "toml", "config": c}
	}

	publish := func(v string, entries map[string]map[string]string) {
		mu.Lock()
		defer mu.Unlock()
		version = v
		configs = map[string]map[string]map[string]string{"snmp": entries}
		raw = nil
		status = 200
	}
	start()
	wait("initial empty snapshot is fetched", func() bool { return len(requests) > 0 })
	wait("local provider remains active", func() bool {
		for _, s := range samples {
			if s.labels["__name__"] == "system_uptime" {
				return true
			}
		}
		return false
	})
	k1, c1 := makeConfig("device-one", "revision-one")
	publish("added", map[string]map[string]string{k1: c1})
	wait("remote SNMP task produces samples", func() bool { return has("device-one", "revision-one", time.Time{}) })
	wait("table metrics carry device labels", func() bool {
		for _, s := range samples {
			if s.labels["__name__"] == "snmp_cpu_load" && s.labels["index"] == "8" {
				return true
			}
		}
		return false
	})
	wait("client caches published version", func() bool { return clientSeen("added") })
	mu.Lock()
	initialSamples := append([]received(nil), samples...)
	mu.Unlock()
	for _, s := range initialSamples {
		if s.labels["__name__"] == "snmp_up" || s.labels["__name__"] == "snmp_uptime" || s.labels["__name__"] == "snmp_cpu_load" {
			require.Equal(t, "device-one", s.labels["ident"])
			require.Equal(t, "fixture", s.labels["collection_collector_id"])
			require.Equal(t, "revision-one", s.labels["collection_revision"])
			require.Equal(t, "managed", s.labels["collection_mode"])
			require.Equal(t, "127.0.0.1", s.labels["snmp_target"])
		}
	}
	// Keeping a checksum while changing bytes must not be mistaken for a working update.
	_, changed := makeConfig("device-one", "ignored-revision")
	publish("same-key", map[string]map[string]string{k1: changed})
	wait("same key response reached the client", func() bool {
		for _, q := range requests {
			if q.Get("version") == "added" {
				return len(requests) > 4
			}
		}
		return false
	})
	time.Sleep(2200 * time.Millisecond)
	mu.Lock()
	ignored := has("device-one", "ignored-revision", time.Time{})
	seenWrongKey := clientSeen("same-key")
	mu.Unlock()
	require.False(t, ignored)
	require.False(t, seenWrongKey)
	k2, c2 := makeConfig("device-one", "revision-two")
	k3, c3 := makeConfig("device-two", "revision-three")
	publish("modified", map[string]map[string]string{k2: c2, k3: c3})
	wait("changed checksum replaces the first task", func() bool {
		return has("device-one", "revision-two", time.Time{}) && has("device-two", "revision-three", time.Time{})
	})
	for _, failure := range []struct {
		name string
		code int
		body string
	}{{"unauthorized", 401, `{"error":"unauthorized"}`}, {"forbidden", 403, `{"error":"forbidden"}`}, {"database", 503, `{"error":"unavailable"}`}, {"empty", 200, ""}, {"malformed", 200, "{"}, {"connection-reset", -1, ""}} {
		mu.Lock()
		status = failure.code
		raw = []byte(failure.body)
		before := len(requests)
		mu.Unlock()
		wait(failure.name+" was fetched", func() bool { return len(requests) > before })
		since := time.Now()
		wait(failure.name+" preserves running tasks", func() bool { return has("device-one", "revision-two", since) })
	}
	publish("partial-delete", map[string]map[string]string{k2: c2})
	wait("partial deletion acknowledged", func() bool { return clientSeen("partial-delete") })
	since := time.Now().Add(5 * time.Second)
	wait("remaining task continues", func() bool { return has("device-one", "revision-two", since) })
	mu.Lock()
	partialStillRunning := has("device-two", "revision-three", since)
	mu.Unlock()
	require.False(t, partialStillRunning)
	publish("all-deleted", map[string]map[string]string{})
	wait("last deletion acknowledged with nonempty version", func() bool { return clientSeen("all-deleted") })
	time.Sleep(5200 * time.Millisecond)
	since = time.Now()
	time.Sleep(3200 * time.Millisecond)
	mu.Lock()
	deletedStillRunning := has("device-one", "revision-two", since)
	mu.Unlock()
	require.False(t, deletedStillRunning)
	_, invalid := makeConfig("device-one", "initialization-failed")
	invalid["config"] = strings.Replace(invalid["config"], "community =", "translator = \"fixture-invalid\"\ncommunity =", 1)
	invalidKey := digest([]byte(invalid["config"]))
	publish("invalid-init", map[string]map[string]string{invalidKey: invalid})
	wait("provider caches version before initialization succeeds", func() bool { return clientSeen("invalid-init") })
	time.Sleep(2200 * time.Millisecond)
	mu.Lock()
	invalidProducedSamples := has("device-one", "initialization-failed", time.Time{})
	mu.Unlock()
	require.False(t, invalidProducedSamples)
	publish("restored", map[string]map[string]string{k2: c2})
	wait("task restored", func() bool { return clientSeen("restored") })
	mu.Lock()
	status = 503
	raw = []byte(`{"error":"offline"}`)
	mu.Unlock()
	since = time.Now()
	wait("running task survives endpoint outage", func() bool { return has("device-one", "revision-two", since) })
	stop()
	mu.Lock()
	samples = nil
	requests = nil
	mu.Unlock()
	logs.Reset()
	start()
	wait("cold start still runs local provider", func() bool {
		for _, s := range samples {
			if s.labels["__name__"] == "system_uptime" {
				return true
			}
		}
		return false
	})
	time.Sleep(2200 * time.Millisecond)
	mu.Lock()
	coldStartedRemote := has("device-one", "revision-two", time.Time{})
	mu.Unlock()
	require.False(t, coldStartedRemote)
	publish("restored", map[string]map[string]string{k2: c2})
	wait("remote task recovers when endpoint returns", func() bool { return has("device-one", "revision-two", time.Time{}) })
	stop()
	leaked := strings.Contains(logs.String(), "fixture-community")
	t.Logf("collector logs contain community: %v", leaked)
	if os.Getenv("N9E_TEST_CATEGRAF_REDACTED") == "1" {
		require.False(t, leaked, "patched collector must not log community")
	}
}

// A minimal loopback SNMP v2c fixture. All OIDs are numeric and all values synthetic.
func newSNMPFixture(t *testing.T) *net.UDPConn {
	t.Helper()
	bind := net.IPv4(127, 0, 0, 1)
	if value := os.Getenv("N9E_TEST_SNMP_FIXTURE_BIND"); value != "" {
		bind = net.ParseIP(value)
		require.NotNil(t, bind)
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: bind})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 65536)
		for {
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var packet struct {
				Version   int
				Community []byte
				PDU       asn1.RawValue
			}
			if _, err = asn1.Unmarshal(buf[:n], &packet); err != nil {
				continue
			}
			if string(packet.Community) != "fixture-community" {
				continue
			}
			var pdu struct {
				RequestID int
				Error     int
				Index     int
				Bindings  []struct {
					OID   asn1.ObjectIdentifier
					Value asn1.RawValue
				}
			}
			full := append([]byte(nil), packet.PDU.FullBytes...)
			full[0] = 0x30
			if _, err = asn1.Unmarshal(full, &pdu); err != nil {
				continue
			}
			values := fixtureSNMPValues()
			// Optional, private acceptance controls for real native alert events.
			// The normal HTTP Provider gate keeps its fixed synthetic values.
			if path := os.Getenv("N9E_TEST_SNMP_FIXTURE_STATE_FILE"); path != "" {
				var state struct {
					CPU       *int  `json:"cpu"`
					Memory    *int  `json:"memory"`
					Disk      *int  `json:"disk"`
					Available *bool `json:"available"`
				}
				b, e := os.ReadFile(path)
				if e == nil && json.Unmarshal(b, &state) == nil {
					if state.Available != nil && !*state.Available {
						continue
					}
					integer := func(v int) asn1.RawValue { b, _ := asn1.Marshal(v); return asn1.RawValue{FullBytes: b} }
					if state.CPU != nil {
						for _, i := range []string{"8", "17"} {
							values["1.3.6.1.2.1.25.3.3.1.2."+i] = integer(*state.CPU)
						}
					}
					if state.Memory != nil {
						values["1.3.6.1.2.1.25.2.3.1.6.17"] = integer(2 * *state.Memory)
					}
					if state.Disk != nil {
						values["1.3.6.1.2.1.25.2.3.1.6.2"] = integer(2 * *state.Disk)
					}
				}
			}

			for i := range pdu.Bindings {
				requested := pdu.Bindings[i].OID
				if packet.PDU.Tag == 5 || packet.PDU.Tag == 1 {
					var candidates []asn1.ObjectIdentifier
					for oid := range values {
						parsed := parseFixtureOID(oid)
						if compareFixtureOID(parsed, requested) > 0 {
							candidates = append(candidates, parsed)
						}
					}
					sort.Slice(candidates, func(i, j int) bool { return compareFixtureOID(candidates[i], candidates[j]) < 0 })
					if len(candidates) > 0 {
						pdu.Bindings[i].OID = candidates[0]
						pdu.Bindings[i].Value = values[candidates[0].String()]
					} else {
						pdu.Bindings[i].Value = asn1.RawValue{Class: 2, Tag: 2}
					}
				} else if v, ok := values[requested.String()]; ok {
					pdu.Bindings[i].Value = v
				} else {
					pdu.Bindings[i].Value = asn1.RawValue{Class: 2, Tag: 0}
				}
			}
			pdu.Error = 0
			pdu.Index = 0
			encoded, err := asn1.Marshal(pdu)
			if err != nil {
				continue
			}
			encoded[0] = 0xa2
			packet.PDU = asn1.RawValue{FullBytes: encoded}
			response, err := asn1.Marshal(packet)
			if err == nil {
				_, _ = conn.WriteToUDP(response, addr)
			}
		}
	}()
	return conn
}

func parseFixtureOID(s string) asn1.ObjectIdentifier {
	out := asn1.ObjectIdentifier{}
	for _, part := range strings.Split(strings.TrimPrefix(s, "."), ".") {
		n, _ := strconv.Atoi(part)
		out = append(out, n)
	}
	return out
}
func compareFixtureOID(a, b asn1.ObjectIdentifier) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}
func fixtureSNMPValues() map[string]asn1.RawValue {
	integer := func(v int) asn1.RawValue { b, _ := asn1.Marshal(v); return asn1.RawValue{FullBytes: b} }
	oid := func(v string) asn1.RawValue {
		b, _ := asn1.Marshal(parseFixtureOID(v))
		return asn1.RawValue{FullBytes: b}
	}
	out := map[string]asn1.RawValue{"1.3.6.1.2.1.1.1.0": {Tag: 4, Bytes: []byte("protocol-fixture")}, "1.3.6.1.2.1.1.3.0": {Class: 1, Tag: 3, Bytes: []byte{0x30, 0x39}}}
	for index, value := range map[int]int{8: 25, 17: 50} {
		out[fmt.Sprint("1.3.6.1.2.1.25.3.3.1.2.", index)] = integer(value)
	}
	for _, index := range []int{2, 17} {
		prefix := "1.3.6.1.2.1.25.2.3.1."
		typ := "1.3.6.1.2.1.25.2.1.4"
		if index == 17 {
			typ = "1.3.6.1.2.1.25.2.1.2"
		}
		out[fmt.Sprint(prefix, "2.", index)] = oid(typ)
		out[fmt.Sprint(prefix, "4.", index)] = integer(4096)
		out[fmt.Sprint(prefix, "5.", index)] = integer(200)
		out[fmt.Sprint(prefix, "6.", index)] = integer(50)
	}
	for _, index := range []int{11, 13} {
		for _, column := range []int{3, 7, 8, 10, 16} {
			v := 1
			if column == 3 {
				v = 6
			}
			if column == 10 || column == 16 {
				v = int(time.Now().Unix()%100000) * 1000
			}
			out[fmt.Sprint("1.3.6.1.2.1.2.2.1.", column, ".", index)] = integer(v)
		}
	}
	return out
}

// This opt-in fixture is used only by the disposable-preview browser acceptance run.
func TestCollectionPreviewFixture(t *testing.T) {
	path := os.Getenv("N9E_TEST_COLLECTION_PREVIEW_FIXTURE_FILE")
	if path == "" {
		t.Skip("preview SNMP fixture server not requested")
	}
	conn := newSNMPFixture(t)
	require.NoError(t, os.WriteFile(path, []byte(conn.LocalAddr().String()), 0600))
	defer os.Remove(path)
	stop := path + ".stop"
	deadline := time.NewTimer(30 * time.Minute)
	defer deadline.Stop()
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-deadline.C:
			return
		case <-timer.C:
			if _, err := os.Stat(stop); err == nil {
				_ = os.Remove(stop)
				return
			}
		}
	}
}
