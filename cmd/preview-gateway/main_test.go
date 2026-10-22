//go:build linux

package main

import (
	"bufio"
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
