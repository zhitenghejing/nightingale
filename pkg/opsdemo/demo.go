// Package opsdemo supplies an isolated, bounded test workload. Its quota metric
// describes its own cache directory, never the host filesystem's free space.
package opsdemo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const quota = 8 << 20

type State struct {
	Expected       []string       `json:"expected"`
	Delivered      []string       `json:"delivered"`
	BusinessBroken bool           `json:"business_broken"`
	DataBroken     bool           `json:"data_broken"`
	Operations     map[string]int `json:"operations"`
	FaultNumber    int            `json:"fault_number"`
}
type Demo struct {
	mu          sync.Mutex
	root, ident string
	state       State
}

func New(root, ident string) (*Demo, error) {
	if root == "" || ident == "" || len(ident) > 100 {
		return nil, fmt.Errorf("private data directory and bounded target identity are required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if root == "/" {
		return nil, fmt.Errorf("root filesystem is not a demo data directory")
	}
	if info, e := os.Lstat(root); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("demo directory must not be a symlink")
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	marker := filepath.Join(root, ".ops-demo-owned")
	if _, e := os.Stat(marker); os.IsNotExist(e) {
		entries, e := os.ReadDir(root)
		if e != nil || len(entries) > 0 {
			return nil, fmt.Errorf("existing non-demo directory is not accepted")
		}
		if err = os.WriteFile(marker, []byte(ident), 0600); err != nil {
			return nil, err
		}
	} else {
		data, e := os.ReadFile(marker)
		if e != nil || string(data) != ident {
			return nil, fmt.Errorf("demo directory belongs to another identity")
		}
	}
	d := &Demo{root: root, ident: ident, state: State{Expected: []string{}, Delivered: []string{}, Operations: map[string]int{}}}
	if raw, e := os.ReadFile(filepath.Join(root, "state.json")); e == nil {
		if err = json.Unmarshal(raw, &d.state); err != nil {
			return nil, err
		}
	}
	return d, nil
}
func (d *Demo) save() error {
	raw, err := json.Marshal(d.state)
	if err != nil {
		return err
	}
	tmp := filepath.Join(d.root, "state.json.tmp")
	if err = os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(d.root, "state.json"))
}
func (d *Demo) used() int64 {
	var n int64
	entries, _ := os.ReadDir(d.root)
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasPrefix(entry.Name(), "cache-") {
			if info, e := entry.Info(); e == nil {
				n += info.Size()
			}
		}
	}
	return n
}
func (d *Demo) businessOK() bool { return d.used() < quota*8/10 && !d.state.BusinessBroken }
func (d *Demo) dataOK() bool {
	if d.state.DataBroken || len(d.state.Expected) != len(d.state.Delivered) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range d.state.Delivered {
		if seen[id] {
			return false
		}
		seen[id] = true
	}
	for _, id := range d.state.Expected {
		if !seen[id] {
			return false
		}
	}
	return true
}
func (d *Demo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost {
		ScriptedModel(w, r)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet && r.URL.Path == "/metrics" {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		up := 0
		if d.businessOK() {
			up = 1
		}
		fmt.Fprintf(w, "ops_demo_quota_used_ratio{ident=%s,environment=\"test\",system_version=\"1\"} %f\nops_demo_business_up{ident=%s} %d\nops_demo_queue_size{ident=%s} %d\n", strconv.Quote(d.ident), float64(d.used())/quota, strconv.Quote(d.ident), up, strconv.Quote(d.ident), len(d.state.Expected)-len(d.state.Delivered))
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		Kind         string `json:"kind"`
		Ident        string `json:"ident"`
		OperationKey string `json:"operation_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if input.Ident != "" && input.Ident != d.ident {
		http.Error(w, "wrong test target", 403)
		return
	}
	out := map[string]interface{}{"ident": d.ident, "test_only": true}
	switch r.URL.Path {
	case "/info":
		out["quota_bytes"] = quota
		out["used_bytes"] = d.used()
		out["fault_number"] = d.state.FaultNumber
	case "/fault":
		switch input.Kind {
		case "storage":
			d.state.FaultNumber++
			name := fmt.Sprintf("cache-%06d", d.state.FaultNumber)
			if d.used() > quota {
				http.Error(w, "quota limit exceeded", 409)
				return
			}
			f, err := os.OpenFile(filepath.Join(d.root, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				http.Error(w, "cache creation failed", 500)
				return
			}
			err = f.Truncate(quota*95/100 - d.used())
			_ = f.Close()
			if err != nil {
				http.Error(w, "cache allocation failed", 500)
				return
			}
			for i := 0; i < 5; i++ {
				d.state.Expected = append(d.state.Expected, fmt.Sprintf("test-%d-%d", d.state.FaultNumber, i))
			}
		case "business":
			d.state.BusinessBroken = true
		case "data":
			d.state.DataBroken = true
		case "clear":
			d.state.BusinessBroken = false
			d.state.DataBroken = false
			if d.businessOK() {
				d.state.Delivered = append([]string{}, d.state.Expected...)
			}
		default:
			http.Error(w, "unsupported test fault", 422)
			return
		}
		out["fault"] = input.Kind
	case "/actions/reclaim":
		if input.OperationKey == "" || len(input.OperationKey) > 64 || input.Ident != d.ident {
			http.Error(w, "operation identity required", 422)
			return
		}
		if n, ok := d.state.Operations[input.OperationKey]; ok {
			out["execution_count"] = n
			out["replayed"] = true
			break
		}
		entries, err := os.ReadDir(d.root)
		if err != nil {
			http.Error(w, "cache read failed", 500)
			return
		}
		for _, e := range entries {
			if e.Type().IsRegular() && strings.HasPrefix(e.Name(), "cache-") {
				if err = os.Remove(filepath.Join(d.root, e.Name())); err != nil {
					http.Error(w, "cache removal failed", 500)
					return
				}
			}
		}
		if d.businessOK() {
			d.state.Delivered = append([]string{}, d.state.Expected...)
		}
		d.state.Operations[input.OperationKey] = 1
		out["execution_count"] = 1
		out["replayed"] = false
	case "/probe":
		switch input.Kind {
		case "demo_business":
			out["passed"] = d.businessOK()
			out["queue_size"] = len(d.state.Expected) - len(d.state.Delivered)
		case "demo_data":
			out["passed"] = d.dataOK()
			out["expected_count"] = len(d.state.Expected)
			out["delivered_count"] = len(d.state.Delivered)
		default:
			http.Error(w, "unsupported probe", 422)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err := d.save(); err != nil {
		http.Error(w, "state persistence failed", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
