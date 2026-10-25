//go:build linux

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func privateTestHost(t *testing.T) string {
	t.Helper()
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil && ip.IsPrivate() {
			return ip.String()
		}
	}
	t.Skip("LAN listener test requires a host private IPv4 interface")
	return ""
}

func freePreviewPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func TestLANManifestRejectsWildcardPublicAndStable(t *testing.T) {
	for _, host := range []string{"0.0.0.0", "127.0.0.1", "8.8.8.8", "localhost", "::", "fd00::1", "::ffff:192.168.0.1"} {
		t.Run(host, func(t *testing.T) {
			if _, err := listenPreview(manifest{Port: freePreviewPort(t), LANHost: host}); err == nil {
				t.Fatal("unsafe LAN manifest accepted")
			}
		})
	}
	if _, err := listenPreview(manifest{Role: "stable", LANHost: "192.168.0.1"}); err == nil {
		t.Fatal("stable instance acquired a development LAN entry")
	}
}

func TestLANBindFailureReleasesLoopbackAndDoesNotStartChild(t *testing.T) {
	host := privateTestHost(t)
	occupied, err := net.Listen("tcp4", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port
	directory := t.TempDir()
	marker := filepath.Join(directory, "child-started")
	binary := filepath.Join(directory, "child")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\ntouch child-started\n"), 0700); err != nil {
		t.Fatal(err)
	}
	m := manifest{Port: port, LANHost: host, Binary: binary, RunDir: directory}
	data, _ := json.Marshal(m)
	path := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := serve(context.Background(), path); err == nil || !strings.Contains(err.Error(), host) {
		t.Fatalf("LAN conflict was not reported: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("child started despite failed LAN bind")
	}
	loopback, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("failed setup leaked loopback listener: %v", err)
	}
	_ = loopback.Close()
}

func TestPreviewOwnsBothEntriesAndChildUntilStop(t *testing.T) {
	for _, host := range []string{"", privateTestHost(t)} {
		t.Run("lan="+host, func(t *testing.T) {
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/n9e/version":
					fmt.Fprint(w, "preview-lan-test")
				case "/login":
					fmt.Fprint(w, "<html>LAN login</html>")
				case "/api/n9e/self/profile":
					w.WriteHeader(http.StatusUnauthorized)
				case "/prometheus/api/v1/query":
					fmt.Fprint(w, `{"status":"success"}`)
				case "/api/stream":
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: first\n\n")
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				default:
					t.Errorf("unexpected backend request: %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer backend.Close()
			_, rawPort, _ := net.SplitHostPort(strings.TrimPrefix(backend.URL, "http://"))
			backendPort, _ := strconv.Atoi(rawPort)
			directory := t.TempDir()
			binary := filepath.Join(directory, "child")
			if err := os.WriteFile(binary, []byte("#!/bin/sh\necho $$ > child.pid\nexec sleep 60\n"), 0700); err != nil {
				t.Fatal(err)
			}
			m := manifest{Port: freePreviewPort(t), BackendPort: backendPort, LANHost: host,
				BuildID: "lan-test", Role: "development", RunDir: directory, Binary: binary,
				CredentialsFile: filepath.Join(directory, "credentials.json")}
			data, _ := json.Marshal(m)
			path := filepath.Join(directory, "manifest.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- serve(ctx, path) }()
			defer func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(10 * time.Second):
					t.Error("preview did not stop")
				}
				for _, address := range []string{"127.0.0.1", host} {
					if address == "" {
						continue
					}
					connection, err := net.DialTimeout("tcp4", net.JoinHostPort(address, fmt.Sprint(m.Port)), time.Second)
					if err == nil {
						_ = connection.Close()
						t.Error("stop leaked listener", address)
					}
				}
				pidBytes, err := os.ReadFile(filepath.Join(directory, "child.pid"))
				if err != nil {
					t.Error(err)
					return
				}
				pid, _ := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
				if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
					t.Error("stop left a child process", pid, err)
				}
			}()
			for _, address := range []string{"127.0.0.1", host} {
				if address == "" {
					continue
				}
				base := "http://" + net.JoinHostPort(address, fmt.Sprint(m.Port))
				deadline := time.Now().Add(8 * time.Second)
				for {
					if err := check(base, "/__preview/info", `"ready":true`); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("entry did not become ready", base)
					}
					time.Sleep(20 * time.Millisecond)
				}
				for _, check := range []struct {
					path string
					code int
				}{{"/login", 200}, {"/api/n9e/self/profile", 401}, {"/v1/n9e/users", 403}, {"/metrics", 403}} {
					response, err := client.Get(base + check.path)
					if err != nil {
						t.Fatal(err)
					}
					_ = response.Body.Close()
					if response.StatusCode != check.code || response.Header.Get("X-N9E-Preview-Build") != m.BuildID {
						t.Fatalf("%s: status=%d, build=%s", check.path, response.StatusCode, response.Header.Get("X-N9E-Preview-Build"))
					}
				}
				response, err := client.Get(base + "/api/stream")
				if err != nil {
					t.Fatal(err)
				}
				line, err := bufio.NewReader(response.Body).ReadString('\n')
				_ = response.Body.Close()
				if err != nil || line != "data: first\n" {
					t.Fatal("entry buffered the stream", line, err)
				}
			}
		})
	}
}
