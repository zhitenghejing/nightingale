package externalpush

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
)

func TestFileKeySigningThroughPersistentDelivery(t *testing.T) {
	const secret = "TEST_ONLY_FIXED_KEY"
	const expected = "134CD8F89958CBFEED40918C040FCE9D"
	path := filepath.Join(t.TempDir(), "test-only.key")
	if err := os.WriteFile(path, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	var got []Payload
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var p Payload
		if err := json.Unmarshal([]byte(r.PostForm.Get("json")), &p); err != nil {
			t.Error(err)
		}
		got = append(got, p)
		fmt.Fprint(w, `{"status":200}`)
	}))
	defer receiver.Close()
	db := testDB(t)
	s := simulation(receiver.URL)
	s.Config.TestSign = ""
	s.Config.SigningMode = "key_file"
	s.Config.SigningKeyRef = "platform10-v1"
	// Literal vector was calculated independently with Python hashlib/UTF-8.
	s.Event.FirstTriggerTime = 1767294245
	id := enqueue(t, db, s)
	worker := Worker{DB: db, Keys: NewFileKeys(map[string]string{"platform10-v1": path})}
	if err := worker.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Sign != expected {
		t.Fatalf("file signature not applied to final payload: %+v", got)
	}
	recovery := s
	recovery.Event.DispatchID = uuid.NewString()
	recovery.Event.IsRecovered = true
	recovery.Event.IncidentSequence = 2
	recovery.Event.RecoverTime = s.Event.FirstTriggerTime + 722
	rid := enqueue(t, db, recovery)
	if err := worker.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Sign != expected || got[1].ProblemTime != got[0].ProblemTime || got[1].AlarmType != "ok" {
		t.Fatal("recovery must use the original signing input")
	}
	for _, id := range []int64{id, rid} {
		d := state(t, db, id)
		raw, _ := json.Marshal(d)
		if d.Task.State != Accepted || strings.Contains(string(raw), secret) || strings.Contains(d.Task.Snapshot, secret) || strings.Contains(d.Task.Payload, secret) {
			t.Fatal("key leaked or task not accepted")
		}
	}
}

func TestMissingFileKeyBlocksOnlyItsOwnTask(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{"status":200}`) }))
	defer srv.Close()
	db := testDB(t)
	s := simulation(srv.URL)
	s.Config.TestSign = ""
	s.Config.SigningMode = "key_file"
	id := enqueue(t, db, s)
	legacy := simulation(srv.URL)
	enqueue(t, db, legacy)
	worker := Worker{DB: db, Keys: NewFileKeys(map[string]string{"platform10-v1": filepath.Join(t.TempDir(), "missing.key")})}
	if err := worker.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := state(t, db, id)
	if d.Task.State != Invalid || d.Task.Attempts != 0 || !strings.Contains(d.Task.Reason, "missing on center") || calls != 1 {
		t.Fatalf("missing key must block only file-key channel: state=%s attempts=%d calls=%d reason=%s", d.Task.State, d.Task.Attempts, calls, d.Task.Reason)
	}
}

func TestSimulationProblemCannotAnchorRealReceiverRecovery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"status":200}`) }))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("TEST_ONLY_KEY"), 0600); err != nil {
		t.Fatal(err)
	}
	db := testDB(t)
	s := simulation(srv.URL)
	s.Config.SigningMode, s.Config.TestSign = "key_file", ""
	s.Params["level_map"] = `{"1":"严重"}`
	id := enqueue(t, db, s)
	worker := Worker{DB: db, Keys: NewFileKeys(map[string]string{"platform10-v1": path})}
	if err := worker.Step(context.Background()); err != nil || state(t, db, id).Task.State != Accepted {
		t.Fatal("local problem not accepted", err)
	}
	s.Event.DispatchID = uuid.NewString()
	s.Event.IncidentSequence, s.Event.IsRecovered = 2, true
	s.Event.RecoverTime = s.Event.FirstTriggerTime + 60
	s.Config.Mode, s.Config.Endpoint = "platform10", models.ExternalPushPlatform10Endpoint
	rid := enqueue(t, db, s)
	// Exercise the claim/attempt boundary only: this test must never call send,
	// perform DNS, or connect to the real receiver even when the guard is broken.
	claimed, _, err := worker.claim(rid)
	if err != nil {
		t.Fatal(err)
	}
	d := state(t, db, rid)
	if claimed != nil || d.Task.State != Invalid || d.Task.Attempts != 0 || !strings.Contains(d.Task.Reason, "receiver mode") {
		t.Fatalf("simulation problem used for real recovery: claimed=%t state=%s attempts=%d reason=%s", claimed != nil, d.Task.State, d.Task.Attempts, d.Task.Reason)
	}
}

func TestSigningModeCannotChangeWithinDeliveredIncident(t *testing.T) {
	for _, firstMode := range []string{"key_file", "test_value"} {
		for _, recovery := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/recovery=%t", firstMode, recovery), func(t *testing.T) {
				var calls int
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{"status":200}`) }))
				defer srv.Close()
				path := filepath.Join(t.TempDir(), "key")
				if err := os.WriteFile(path, []byte("TEST_ONLY_KEY"), 0600); err != nil {
					t.Fatal(err)
				}
				db := testDB(t)
				s := simulation(srv.URL)
				s.Config.SigningMode = firstMode
				if firstMode == "key_file" {
					s.Config.TestSign = ""
				}
				id := enqueue(t, db, s)
				worker := Worker{DB: db, Keys: NewFileKeys(map[string]string{"platform10-v1": path})}
				if err := worker.Step(context.Background()); err != nil || state(t, db, id).Task.State != Accepted {
					t.Fatal("original problem must be accepted", err)
				}
				s.Event.DispatchID = uuid.NewString()
				s.Event.IncidentSequence = 2
				s.Event.IsRecovered = recovery
				if recovery {
					s.Event.RecoverTime = s.Event.FirstTriggerTime + 60
				}
				if firstMode == "key_file" {
					s.Config.SigningMode, s.Config.TestSign = "test_value", "TEST_CHANGED_MODE"
				} else {
					s.Config.SigningMode, s.Config.TestSign = "key_file", ""
				}
				next := enqueue(t, db, s)
				if err := worker.Step(context.Background()); err != nil {
					t.Fatal(err)
				}
				d := state(t, db, next)
				if calls != 1 || d.Task.State != Invalid || d.Task.Attempts != 0 || !strings.Contains(d.Task.Reason, "signing") {
					t.Fatalf("mode change must not send: calls=%d state=%s attempts=%d reason=%s", calls, d.Task.State, d.Task.Attempts, d.Task.Reason)
				}
			})
		}
	}
}

func TestFileKeyValidationAndExactWhitespace(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		mode          os.FileMode
		ready         bool
	}{
		{"valid", "TEST_ONLY_FIXED_KEY", 0600, true},
		{"key_spaces_are_significant", " TEST_ONLY_FIXED_KEY ", 0600, true},
		{"empty", "", 0600, false}, {"spaces_only", "  ", 0600, false},
		{"newline", "TEST_ONLY_FIXED_KEY\n", 0600, false},
		{"world_readable", "TEST_ONLY_FIXED_KEY", 0644, false},
		{"invalid_utf8", string([]byte{255}), 0600, false},
		{"oversized", strings.Repeat("X", 65537), 0600, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "key")
			if err := os.WriteFile(path, []byte(tc.content), tc.mode); err != nil {
				t.Fatal(err)
			}
			keys := NewFileKeys(map[string]string{"TEST-v1": path})
			status := keys.Status("TEST-v1")
			if status.Ready != tc.ready {
				t.Fatalf("unexpected readiness: %+v", status)
			}
			encoded, _ := json.Marshal(status)
			if strings.Contains(string(encoded), "TEST_ONLY_FIXED_KEY") || strings.Contains(string(encoded), path) {
				t.Fatal("readiness leaks material or path")
			}
			if tc.name == "key_spaces_are_significant" {
				value, err := keys.Sign("2026-01-02 03:04:05", "TEST-v1")
				if err != nil || value != "E1EF396EB25FDDC58724AFBB26949460" {
					t.Fatal("key was trimmed or timestamp was changed")
				}
			}
		})
	}
	dir := t.TempDir()
	keys := NewFileKeys(map[string]string{"directory": dir, "relative": "key"})
	if keys.Status("directory").Ready || keys.Status("relative").Ready || keys.Status("unknown").Ready {
		t.Fatal("invalid references accepted")
	}
}

func TestFileKeyResendKeepsFrozenSignatureAndRequiresOriginalVersion(t *testing.T) {
	var bodies []string
	var bodyMu sync.Mutex
	readBodies := func() []string { bodyMu.Lock(); defer bodyMu.Unlock(); return append([]string(nil), bodies...) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		bodyMu.Lock()
		bodies = append(bodies, r.PostForm.Get("json"))
		first := len(bodies) == 1
		bodyMu.Unlock()
		if first {
			time.Sleep(70 * time.Millisecond)
		}
		fmt.Fprint(w, `{"status":200}`)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "key")
	os.WriteFile(path, []byte("TEST_KEY_V1"), 0600)
	db := testDB(t)
	s := simulation(srv.URL)
	s.Config.SigningMode = "key_file"
	s.Config.TestSign = ""
	s.Config.TimeoutMS = 20
	id := enqueue(t, db, s)
	worker := Worker{DB: db, Keys: NewFileKeys(map[string]string{"platform10-v1": path})}
	worker.Step(context.Background())
	d := state(t, db, id)
	if d.Task.State != Unknown || d.Task.Attempts != 1 {
		t.Fatal("expected unknown first attempt")
	}
	time.Sleep(80 * time.Millisecond)
	os.WriteFile(path, []byte("TEST_REPLACED_KEY"), 0600)
	if err := Act(db, id, "test-admin", Action{Action: "authorize_resend", Version: d.Task.Version, Reason: "TEST receiver reconciled and old request ended"}); err != nil {
		t.Fatal(err)
	}
	worker.Step(context.Background())
	d = state(t, db, id)
	if d.Task.State != Manual || d.Task.Attempts != 1 || len(readBodies()) != 1 {
		t.Fatal("changed key sent a replacement signature")
	}
	os.WriteFile(path, []byte("TEST_KEY_V1"), 0600)
	if err := Act(db, id, "test-admin", Action{Action: "authorize_resend", Version: d.Task.Version, Reason: "TEST original key restored, resend explicitly authorized"}); err != nil {
		t.Fatal(err)
	}
	worker.Step(context.Background())
	if len(readBodies()) != 2 || readBodies()[0] != readBodies()[1] || state(t, db, id).Task.State != Accepted {
		t.Fatal("resend changed frozen bytes")
	}
}
