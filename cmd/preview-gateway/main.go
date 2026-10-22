//go:build linux

// preview-gateway owns a preview child and proxies HTTP, SSE and WebSockets.
// It is development infrastructure, never part of the production server.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type manifest struct {
	BuildID         string          `json:"buildId"`
	Source          json.RawMessage `json:"source"`
	Frontend        json.RawMessage `json:"frontend"`
	Profile         string          `json:"profile"`
	Role            string          `json:"role"`
	AppVersion      string          `json:"appVersion"`
	Port            int             `json:"port"`
	BackendPort     int             `json:"backendPort"`
	Binary          string          `json:"binary"`
	RunDir          string          `json:"runDir"`
	CredentialsFile string          `json:"credentialsFile"`
	TempDir         string          `json:"tempDir"`
}

var client = &http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{Proxy: nil}}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func check(base, path, expected string) error {
	response, err := client.Get(base + path)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil || response.StatusCode != 200 || !bytes.Contains(data, []byte(expected)) {
		return fmt.Errorf("probe failed: %s (HTTP %d)", path, response.StatusCode)
	}
	return nil
}

func checkLogin(base, credentialsFile string) error {
	var credentials map[string]string
	if err := readJSON(credentialsFile, &credentials); err != nil {
		return err
	}
	body, _ := json.Marshal(credentials)
	response, err := client.Post(base+"/api/n9e/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var data struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"dat"`
	}
	if err := json.NewDecoder(response.Body).Decode(&data); err != nil || data.Data.AccessToken == "" {
		return fmt.Errorf("preview account login failed (HTTP %d)", response.StatusCode)
	}
	request, _ := http.NewRequest(http.MethodGet, base+"/api/n9e/self/profile", nil)
	request.Header.Set("Authorization", "Bearer "+data.Data.AccessToken)
	profile, err := client.Do(request)
	if err != nil {
		return err
	}
	defer profile.Body.Close()
	content, _ := io.ReadAll(io.LimitReader(profile.Body, 2<<20))
	if profile.StatusCode != 200 || !bytes.Contains(content, []byte(`"username":"root"`)) {
		return fmt.Errorf("authenticated profile probe failed")
	}
	return nil
}

func health(base string, m manifest) error {
	version := m.AppVersion
	if version == "" {
		version = "preview-" + m.BuildID
	}
	for _, probe := range [][2]string{
		{"/api/n9e/version", version},
		{"/login", "<html"},
		{"/prometheus/api/v1/query?query=vector(1)", `"status":"success"`},
	} {
		if err := check(base, probe[0], probe[1]); err != nil {
			return err
		}
	}
	response, err := client.Get(base + "/api/n9e/self/profile")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("anonymous profile probe expected HTTP 401, got %d", response.StatusCode)
	}
	return nil
}

func checkReady(base string, m manifest) error {
	if err := health(base, m); err != nil {
		return err
	}
	// Only newly seeded databases require an initial account check. The marker
	// survives failed starts; once verified, user password changes are independent
	// of readiness. Existing instances without a marker need no credential migration.
	pending := filepath.Join(filepath.Dir(m.CredentialsFile), "login-check.pending")
	if _, err := os.Stat(pending); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := checkLogin(base, m.CredentialsFile); err != nil {
		return err
	}
	return os.Remove(pending)
}

func handler(m manifest, base string, ready *atomic.Bool) http.Handler {
	target, _ := url.Parse(base)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1 // Stream AI events immediately; also supports HTTP upgrades.
	proxy.ModifyResponse = func(response *http.Response) error {
		response.Header.Set("Cache-Control", "no-store")
		response.Header.Set("X-N9E-Preview-Build", m.BuildID)
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-N9E-Preview-Build", m.BuildID)
		if r.URL.Path == "/__preview/info" {
			ok := ready.Load() && health(base, m) == nil
			w.Header().Set("Content-Type", "application/json")
			if !ok {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			// Paths, process IDs and credentials never leave the host.
			_ = json.NewEncoder(w).Encode(map[string]any{"buildId": m.BuildID, "ready": ok,
				"source": m.Source, "frontend": m.Frontend, "profile": m.Profile, "role": m.Role,
				"appVersion": m.AppVersion})
			return
		}
		if !ready.Load() {
			http.Error(w, "Preview is starting; retry shortly.", http.StatusServiceUnavailable)
			return
		}
		// Internal service/agent APIs are never published by this gateway, even if
		// enabled in the backend. Loopback-only TSDB/debug endpoints stay private too.
		normalized := strings.ToLower(path.Clean("/" + r.URL.Path))
		for _, prefix := range []string{"/v1/n9e", "/prometheus", "/dumper", "/debug", "/metrics"} {
			if normalized == prefix || strings.HasPrefix(normalized, prefix+"/") {
				http.Error(w, "Use authenticated Nightingale APIs for preview data.", http.StatusForbidden)
				return
			}
		}
		if r.URL.Path == "/__preview/ready" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			label := "N9E preview"
			if m.Role == "stable" {
				label = "N9E stable"
			}
			// Clear this origin's storage to avoid stale tokens when a slot is reassigned.
			_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>%s</title><p>%s %s</p><a href="/login">Open Nightingale</a><script>localStorage.clear();sessionStorage.clear();if('serviceWorker' in navigator){navigator.serviceWorker.getRegistrations().then(rs=>Promise.all(rs.map(r=>r.unregister())))}if('caches' in window){caches.keys().then(ks=>Promise.all(ks.map(k=>caches.delete(k))))}</script>`, label, label, m.BuildID)
			return
		}
		// Forwarded origin is needed for OAuth callbacks and generated links.
		if r.Header.Get("X-Forwarded-Proto") == "" {
			r.Header.Set("X-Forwarded-Proto", "http")
		}
		proxy.ServeHTTP(w, r)
	})
}

func previewCommand(m manifest) *exec.Cmd {
	command := exec.Command(m.Binary, "--configs", "etc")
	command.Dir = m.RunDir
	// The config loader consumes CONFIGTYPE_* variables, and providers may read
	// credentials from the environment. Inherit only these runtime settings.
	command.Env = []string{"HOME=" + m.RunDir, "TMPDIR=" + m.TempDir}
	for _, name := range []string{"PATH", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value, ok := os.LookupEnv(name); ok {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	return command
}

func serve(path string) error {
	var m manifest
	if err := readJSON(path, &m); err != nil {
		return err
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", m.BackendPort)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	command := previewCommand(m)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	// A killed gateway must not leave an orphaned database/TSDB writer.
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	if err := command.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var ready atomic.Bool
	server := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", m.Port), Handler: handler(m, base, &ready),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		_ = server.Shutdown(shutdown)
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(25 * time.Second):
			_ = command.Process.Kill()
			<-done
		}
	}()
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Print(err)
			cancel()
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-done:
			done <- err // Keep completion available to the cleanup path.
			return fmt.Errorf("n9e child exited: %v", err)
		case <-ticker.C:
			if !ready.Load() {
				if err := checkReady(base, m); err != nil {
					log.Print(err)
					continue
				}
				ready.Store(true)
				log.Printf("preview ready: %s", strings.TrimSpace(m.BuildID))
			}
		}
	}
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: preview-gateway /absolute/path/to/manifest.json")
	}
	if err := serve(os.Args[1]); err != nil {
		log.Fatal(err)
	}
}
