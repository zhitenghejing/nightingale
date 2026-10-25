//go:build linux

package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/conf"
	"github.com/ccfos/nightingale/v6/pkg/cfg"
)

func TestFingerprintAssetsAreCompressedAndCacheable(t *testing.T) {
	asset := strings.Repeat("window.example = 'static frontend';\n", 10000)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		io.WriteString(w, asset)
	}))
	defer backend.Close()
	var ready atomic.Bool
	ready.Store(true)
	gateway := httptest.NewServer(handler(manifest{BuildID: "assets"}, backend.URL, &ready))
	defer gateway.Close()
	for _, test := range []struct{ path, cachePolicy string }{
		{"/assets/index-431594d8.js", "public, max-age=31536000, immutable"},
		{"/js/placement.min.js", "no-store"},
		{"/font/iconfont.js", "no-store"},
	} {
		t.Run(test.path, func(t *testing.T) {
			request, _ := http.NewRequest("GET", gateway.URL+test.path, nil)
			request.Header.Set("Accept-Encoding", "gzip")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.Header.Get("Content-Encoding") != "gzip" {
				t.Fatalf("static asset not compressed: %v", response.Header)
			}
			if values := response.Header.Values("Cache-Control"); len(values) != 1 || values[0] != test.cachePolicy {
				t.Fatalf("fingerprinted asset cannot be reused: %v", values)
			}
			if !strings.Contains(response.Header.Get("Vary"), "Accept-Encoding") {
				t.Fatal("compressed and identity responses share a cache key")
			}
			data, err := io.ReadAll(response.Body)
			if err != nil || len(data) >= len(asset)/4 {
				t.Fatalf("asset transfer was not reduced: %d bytes, %v", len(data), err)
			}
			reader, err := gzip.NewReader(strings.NewReader(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			decoded, err := io.ReadAll(reader)
			if err != nil || string(decoded) != asset {
				t.Fatalf("compressed asset changed: %v", err)
			}
		})
	}
}

func TestOnlySuccessfulStaticFingerprintsCanBeCached(t *testing.T) {
	for _, test := range []struct {
		path, contentType, encoding string
		status                      int
	}{
		{"/login", "text/html", "gzip", 200},
		{"/api/n9e/self/profile", "application/json", "gzip", 200},
		{"/api/chat", "text/event-stream", "gzip", 200},
		{"/assets/settings.js", "text/javascript", "gzip", 200},
		{"/assets/missing-1234abcd.js", "text/html", "gzip", 200},
		{"/assets/private-1234abcd.js", "text/javascript", "gzip", 401},
		{"/assets/error-1234abcd.js", "text/javascript", "gzip", 500},
		{"/assets/index-1234abcd.js", "application/json", "gzip", 200},
	} {
		t.Run(test.path, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(test.status)
				io.WriteString(w, "response")
			}))
			defer backend.Close()
			var ready atomic.Bool
			ready.Store(true)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", test.path, nil)
			r.Header.Set("Accept-Encoding", test.encoding)
			handler(manifest{}, backend.URL, &ready).ServeHTTP(w, r)
			if values := w.Header().Values("Cache-Control"); len(values) != 1 || values[0] != "no-store" {
				t.Fatalf("dynamic/error response cache policy: %v", values)
			}
			if w.Header().Get("Content-Encoding") != "" || w.Body.String() != "response" {
				t.Fatal("dynamic/error response was transformed")
			}
		})
	}
}

func TestStaticAssetsWithCookiesStayPrivate(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Header().Set("Set-Cookie", "session=private")
		io.WriteString(w, "private")
	}))
	defer backend.Close()
	var ready atomic.Bool
	ready.Store(true)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/assets/index-1234abcd.js", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	handler(manifest{}, backend.URL, &ready).ServeHTTP(w, r)
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Encoding") != "" {
		t.Fatal("response setting a session cookie became public")
	}
}

func TestAssetCompressionRespectsNegotiationAndRanges(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		http.ServeContent(w, r, "style.css", time.Time{}, strings.NewReader(strings.Repeat("body {}\n", 100)))
	}))
	defer backend.Close()
	var ready atomic.Bool
	ready.Store(true)
	for _, test := range []struct{ method, encoding, byteRange string }{
		{"GET", "", ""}, {"GET", "gzip;q=0", ""}, {"GET", "br", ""},
		{"GET", "gzip", "bytes=0-19"}, {"HEAD", "gzip", ""},
	} {
		t.Run(test.method+test.encoding+test.byteRange, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(test.method, "/assets/index-1234abcd.css", nil)
			r.Header.Set("Accept-Encoding", test.encoding)
			r.Header.Set("Range", test.byteRange)
			handler(manifest{}, backend.URL, &ready).ServeHTTP(w, r)
			if w.Header().Get("Content-Encoding") != "" {
				t.Fatal("compressed a range, HEAD or unaccepted encoding")
			}
			if test.byteRange != "" && (w.Code != 206 || w.Body.Len() != 20) {
				t.Fatalf("range changed: status %d, bytes %d", w.Code, w.Body.Len())
			}
		})
	}
}

func TestPreviewChildEnvironment(t *testing.T) {
	directory := t.TempDir()
	for name, value := range map[string]string{
		"CONFIGTYPE_HTTP_APIFORSERVICE_ENABLE": "true",
		"CONFIGTYPE_HTTP_APIFORAGENT_ENABLE":   "true",
		"CONFIGTYPE_HTTP_PROXYAUTH_ENABLE":     "true",
		"CONFIGTYPE_CENTER_DEMOLOGIN_ENABLE":   "true",
		"N9E_CONFIGS":                          "/host/config",
		"AWS_SECRET_ACCESS_KEY":                "test-only-secret",
		"BASH_ENV":                             "/host/shell-hook",
		"HTTP_PROXY":                           "http://host-proxy.invalid:8080",
		"HOME":                                 "/host/home",
		"TMPDIR":                               "/host/tmp",
		"LANG":                                 "C.UTF-8",
		"TZ":                                   "UTC",
	} {
		t.Setenv(name, value)
	}
	if err := os.Mkdir(filepath.Join(directory, "etc"), 0700); err != nil {
		t.Fatal(err)
	}
	// Omit the auth switches so the real config loader detects environment leaks,
	// independently of the explicit switches in the generated preview config.
	if err := os.WriteFile(filepath.Join(directory, "etc/config.toml"), []byte("[HTTP]\nHost = '127.0.0.1'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m := manifest{Binary: executable, RunDir: directory, TempDir: filepath.Join(directory, "tmp")}
	command := previewCommand(m)
	command.Args = []string{executable, "-test.run=^TestPreviewChildEnvironmentHelper$"}
	command.Env = append(command.Env, "N9E_PREVIEW_ENV_HELPER=1")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("child config check failed: %v", err)
	}
	var result struct {
		Service, Agent, Proxy, Demo bool
		Leaked                      []string
		Home, Temp, Path, Lang, TZ  string
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode child config check: %v", err)
	}
	if result.Service || result.Agent || result.Proxy || result.Demo {
		t.Errorf("host environment enabled child auth settings: service=%v agent=%v proxy=%v demo=%v",
			result.Service, result.Agent, result.Proxy, result.Demo)
	}
	if len(result.Leaked) != 0 {
		t.Errorf("host variables leaked into child: %v", result.Leaked)
	}
	if result.Home != m.RunDir || result.Temp != m.TempDir {
		t.Error("child HOME/TMPDIR do not belong to this instance")
	}
	if result.Path != os.Getenv("PATH") || result.Lang != "C.UTF-8" || result.TZ != "UTC" {
		t.Error("required runtime environment was not preserved")
	}
}

func TestPreviewChildEnvironmentHelper(t *testing.T) {
	if os.Getenv("N9E_PREVIEW_ENV_HELPER") != "1" {
		return
	}
	var config conf.ConfigType
	if err := cfg.LoadConfigByDir("etc", &config); err != nil {
		t.Fatal(err)
	}
	leaked := []string{}
	for _, name := range []string{"N9E_CONFIGS", "AWS_SECRET_ACCESS_KEY", "BASH_ENV", "HTTP_PROXY"} {
		if _, ok := os.LookupEnv(name); ok {
			leaked = append(leaked, name)
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"Service": config.HTTP.APIForService.Enable, "Agent": config.HTTP.APIForAgent.Enable,
		"Proxy": config.HTTP.ProxyAuth.Enable, "Demo": config.Center.DemoLogin.Enable,
		"Leaked": leaked, "Home": os.Getenv("HOME"), "Temp": os.Getenv("TMPDIR"),
		"Path": os.Getenv("PATH"), "Lang": os.Getenv("LANG"), "TZ": os.Getenv("TZ"),
	})
	os.Exit(0)
}

func TestReadinessAfterPasswordChange(t *testing.T) {
	directory := t.TempDir()
	credentials := filepath.Join(directory, "credentials.json")
	pending := filepath.Join(directory, "login-check.pending")
	if err := os.WriteFile(credentials, []byte(`{"username":"root","password":"initial"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	var passwordChanged atomic.Bool
	var logins atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/n9e/version":
			fmt.Fprint(w, "preview-new")
		case "/login":
			fmt.Fprint(w, "<html></html>")
		case "/prometheus/api/v1/query":
			fmt.Fprint(w, `{"status":"success"}`)
		case "/api/n9e/auth/login":
			logins.Add(1)
			if passwordChanged.Load() {
				fmt.Fprint(w, `{"err":"Username or password invalid"}`)
				return
			}
			fmt.Fprint(w, `{"dat":{"access_token":"test"}}`)
		case "/api/n9e/self/profile":
			if r.Header.Get("Authorization") != "Bearer test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{"dat":{"username":"root"}}`)
		default:
			t.Errorf("unexpected probe: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer backend.Close()
	m := manifest{BuildID: "new", CredentialsFile: credentials}
	// A failed first login must remain pending, including across another attempt.
	passwordChanged.Store(true)
	if err := checkReady(backend.URL, m); err == nil {
		t.Fatal("failed initial login accepted as ready")
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("failed login lost pending check: %v", err)
	}
	passwordChanged.Store(false)
	if err := checkReady(backend.URL, m); err != nil {
		t.Fatalf("initial readiness failed: %v", err)
	}
	passwordChanged.Store(true)
	if err := checkReady(backend.URL, m); err != nil {
		t.Fatalf("refresh after password change failed: %v", err)
	}
	if logins.Load() != 2 {
		t.Fatalf("refresh retried initial credentials: %d logins", logins.Load())
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("successful login did not clear pending check: %v", err)
	}
	// Old instances also have no pending marker and must not need initial credentials.
	if err := os.Remove(credentials); err != nil {
		t.Fatal(err)
	}
	if err := checkReady(backend.URL, m); err != nil {
		t.Fatalf("existing instance required initial credentials: %v", err)
	}
}

func TestSharedPrometheusReadinessRequiresActualHealthyMetrics(t *testing.T) {
	for _, payload := range []string{
		`{"status":"success","data":{"result":[]}}`,
		`{"status":"success","data":{"result":[{"value":[1,"0"]}]}}`,
		`{"status":"error"}`,
		`not-json`,
	} {
		t.Run(payload, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, payload)
			}))
			defer server.Close()
			if err := prometheusHealth(server.URL); err == nil {
				t.Fatal("unhealthy or empty metrics accepted")
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") != `up{job="prometheus"}` {
			t.Error("wrong shared fixture probe")
		}
		fmt.Fprint(w, `{"status":"success","data":{"result":[{"value":[1,"1"]}]}}`)
	}))
	defer server.Close()
	if err := prometheusHealth(server.URL); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if err := prometheusHealth(server.URL); err == nil {
		t.Fatal("unavailable dependency accepted")
	}
}

func TestPublicInfoDoesNotExposeDatabaseCredentials(t *testing.T) {
	var ready atomic.Bool
	ready.Store(true)
	w := httptest.NewRecorder()
	handler(manifest{DatabaseDSN: "private-secret", PrometheusURL: "http://private-address", Storage: json.RawMessage(`{"database":"postgres"}`)}, "http://127.0.0.1:1", &ready).
		ServeHTTP(w, httptest.NewRequest("GET", "/__preview/info", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("database failure did not block readiness")
	}
	if strings.Contains(w.Body.String(), "private-secret") || strings.Contains(w.Body.String(), "private-address") {
		t.Fatal("dependency credentials or private endpoints leaked")
	}
}

func TestStartingPreviewDoesNotForwardRequests(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request reached backend before readiness")
	}))
	defer backend.Close()
	var ready atomic.Bool
	w := httptest.NewRecorder()
	handler(manifest{BuildID: "new"}, backend.URL, &ready).ServeHTTP(w, httptest.NewRequest("GET", "/login", nil))
	if w.Code != 503 {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestUnavailableBackendResponseCannotBeCached(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	backend.Close()
	var ready atomic.Bool
	ready.Store(true)
	w := httptest.NewRecorder()
	handler(manifest{BuildID: "unavailable"}, backend.URL, &ready).
		ServeHTTP(w, httptest.NewRequest("GET", "/assets/index-1234abcd.js", nil))
	if w.Code != http.StatusBadGateway || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-N9E-Preview-Build") != "unavailable" {
		t.Fatalf("backend failure lost cache/build policy: status %d, headers %v", w.Code, w.Header())
	}
}

func TestInfoChecksBackendBuildAndHealth(t *testing.T) {
	for _, test := range []struct {
		version         string
		anonymousStatus int
		ready           bool
	}{
		{"preview-new", http.StatusUnauthorized, true},
		{"preview-old", http.StatusUnauthorized, false},
		{"preview-new", http.StatusOK, false},
		{"preview-new", http.StatusInternalServerError, false},
	} {
		t.Run(fmt.Sprintf("%s/anonymous-%d", test.version, test.anonymousStatus), func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/n9e/version":
					fmt.Fprint(w, test.version)
				case "/login":
					fmt.Fprint(w, "<html></html>")
				case "/api/n9e/self/profile":
					w.WriteHeader(test.anonymousStatus)
				default:
					fmt.Fprint(w, `{"status":"success"}`)
				}
			}))
			defer backend.Close()
			var ready atomic.Bool
			ready.Store(true)
			w := httptest.NewRecorder()
			handler(manifest{BuildID: "new", RunDir: "/private", CredentialsFile: "/secret"}, backend.URL, &ready).
				ServeHTTP(w, httptest.NewRequest("GET", "/__preview/info", nil))
			var info map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
				t.Fatal(err)
			}
			if info["ready"] != test.ready {
				t.Fatalf("wrong readiness: %s", w.Body.String())
			}
			if strings.Contains(w.Body.String(), "/private") || strings.Contains(w.Body.String(), "/secret") {
				t.Fatal("private paths leaked")
			}
		})
	}
}

func TestProxyPreservesAPIQueryAndStreamsBeforeCompletion(t *testing.T) {
	finish := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "q=a%2Bb" || r.Header.Get("Authorization") != "Bearer test" {
			t.Error("request query/auth changed")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-finish
		fmt.Fprint(w, "data: done\n\n")
	}))
	defer backend.Close()
	defer close(finish)
	var ready atomic.Bool
	ready.Store(true)
	gateway := httptest.NewServer(handler(manifest{BuildID: "stream"}, backend.URL, &ready))
	defer gateway.Close()
	request, _ := http.NewRequest("GET", gateway.URL+"/api/chat?q=a%2Bb", nil)
	request.Header.Set("Authorization", "Bearer test")
	streamClient := &http.Client{Timeout: 2 * time.Second}
	response, err := streamClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatalf("stream buffered: %q, %v", line, err)
	}
	if response.Header.Get("X-N9E-Preview-Build") != "stream" {
		t.Fatal("missing build identity")
	}
}

func TestBackendFailureIsNotReady(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		io.WriteString(w, "preview-new")
	}))
	defer backend.Close()
	if err := health(backend.URL, manifest{BuildID: "new"}); err == nil {
		t.Fatal("backend HTTP 500 accepted as healthy")
	}
}

func TestLocalOnlyEndpointsNeverForwarded(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("local-only endpoint reached backend")
	}))
	defer backend.Close()
	var ready atomic.Bool
	ready.Store(true)
	for _, route := range []string{
		"/prometheus/api/v1/write", "/dumper/sync", "/debug/pprof/", "/metrics",
		"/datadog/api/v1/series", "/datadog/api/v1/check_run", "/opentsdb/put",
		"/openfalcon/push", "/proxy/v1/write", "/x/../datadog/api/v1/series",
		"/%70rometheus/api/v1/query", "/x/../prometheus/api/v1/query",
		"/v1/n9e", "/v1/n9e/", "/v1/n9e/users", "/v1/n9e/user-token",
		"/v1/%6e9e/users", "/v1%2fn9e/users", "//v1//n9e/users", "/x/../v1/n9e/users",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			w := httptest.NewRecorder()
			handler(manifest{BuildID: "new"}, backend.URL, &ready).ServeHTTP(w, httptest.NewRequest(method, route, nil))
			if w.Code != http.StatusForbidden {
				t.Errorf("%s %s: status = %d", method, route, w.Code)
			}
		}
	}
}
