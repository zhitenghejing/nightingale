package externalpush

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/google/uuid"
)

// Source sequences include snapshots that did not match this destination. The
// observable delivery stream must contain only the registered events, no fillers.
func TestSourceSnapshotGapsDoNotBlockActualDeliveries(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		first, reminder, recovery int64
	}{
		{"first problem muted then unmuted", 2, 3, 4},
		{"intermediate reminder filtered", 1, 4, 5},
		{"channel enabled mid incident", 5, 7, 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var received []Payload
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.ParseForm()
				var value Payload
				if err := json.Unmarshal([]byte(r.Form.Get("json")), &value); err != nil {
					t.Error(err)
				}
				received = append(received, value)
				fmt.Fprint(w, `{"status":200}`)
			}))
			defer server.Close()
			db := testDB(t)
			s := simulation(server.URL)
			s.Event.IncidentSequence = tc.first
			first := enqueue(t, db, s)
			step(t, db)
			if d := state(t, db, first); d.Task.State != Accepted {
				t.Fatalf("legitimate first delivery blocked: %s: %s", d.Task.State, d.Task.Reason)
			}
			s.Event.DispatchID = uuid.NewString()
			s.Event.IncidentSequence = tc.reminder
			reminder := enqueue(t, db, s)
			step(t, db)
			if d := state(t, db, reminder); d.Task.State != Accepted {
				t.Fatalf("legitimate reminder blocked: %s: %s", d.Task.State, d.Task.Reason)
			}
			s.Event.DispatchID = uuid.NewString()
			s.Event.IncidentSequence = tc.recovery
			s.Event.IsRecovered = true
			s.Event.RecoverTime = s.Event.FirstTriggerTime + 100
			recovery := enqueue(t, db, s)
			step(t, db)
			if d := state(t, db, recovery); d.Task.State != Accepted {
				t.Fatalf("legitimate recovery blocked: %s: %s", d.Task.State, d.Task.Reason)
			}
			if len(received) != 3 || received[0].AlarmType != "problem" || received[1].AlarmType != "problem" || received[2].AlarmType != "ok" {
				t.Fatalf("fabricated or misordered events: %+v", received)
			}
		})
	}
}

func TestSameEndpointAllowsIndependentApplicableRules(t *testing.T) {
	var received []Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var value Payload
		json.Unmarshal([]byte(r.Form.Get("json")), &value)
		received = append(received, value)
		fmt.Fprint(w, `{"status":200}`)
	}))
	defer server.Close()
	db := testDB(t)
	s := simulation(server.URL)
	ch := models.NotifyChannelConfig{Name: "SIMULATION ONLY shared receiver", Ident: models.ExternalPushType, RequestType: models.ExternalPushType, Enable: true, RequestConfig: &models.RequestConfig{ExternalPushConfig: &s.Config}}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	var routes []Submission
	for i, name := range []string{"TEST_HEADQUARTERS", "TEST_BRANCH"} {
		event := *s.Event.DeepCopy()
		event.DispatchID = uuid.NewString()
		event.IncidentID = uuid.NewString()
		event.Tags = "ident=SIMULATION_device_" + name
		his := event.ToHis(nil)
		if err := db.Create(his).Error; err != nil {
			t.Fatal(err)
		}
		event.Id = his.Id
		params := map[string]interface{}{}
		for k, v := range s.Params {
			params[k] = v
		}
		// Tags carry a test-only site distinction, not an agreed 1.0 attribution protocol.
		params["device_tags"] = "SIMULATION_ONLY_" + name
		params["device_type"] = []string{"TEST_virtualization", "TEST_router"}[i]
		rule := models.NotifyRule{Name: name, Enable: true, NotifyConfigs: []models.NotifyConfig{{ChannelID: ch.ID, Params: params}}}
		if err := db.Create(&rule).Error; err != nil {
			t.Fatal(err)
		}
		routes = append(routes, Submission{ChannelID: ch.ID, NotifyRuleID: rule.ID, Params: params, Event: &event})
	}
	for _, route := range routes {
		id, err := Register(nctx.NewContext(context.Background(), db, true), route)
		if err != nil {
			t.Fatal(err)
		}
		step(t, db)
		if d := state(t, db, id); d.Task.State != Accepted {
			t.Fatalf("unrelated rule blocked shared endpoint: %s: %s", d.Task.State, d.Task.Reason)
		}
	}
	if len(received) != 2 || received[0].DeviceType != "TEST_virtualization" || received[1].DeviceType != "TEST_router" || received[0].DeviceTags == received[1].DeviceTags {
		t.Fatalf("incorrect independent mappings: %+v", received)
	}
}

func TestRecoveryDoesNotRequireNeverSentReminder(t *testing.T) {
	var received []Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		var value Payload
		json.Unmarshal([]byte(r.Form.Get("json")), &value)
		received = append(received, value)
		fmt.Fprint(w, `{"status":200}`)
	}))
	defer server.Close()
	db := testDB(t)
	s := simulation(server.URL)
	s.Params["repeat_policy"] = "review_repeats"
	first := enqueue(t, db, s)
	step(t, db)
	if state(t, db, first).Task.State != Accepted {
		t.Fatal("first problem not delivered")
	}
	s.Event.DispatchID = uuid.NewString()
	s.Event.IncidentSequence = 3
	reminder := enqueue(t, db, s)
	d := state(t, db, reminder)
	if err := Act(db, reminder, "test-admin", Action{Action: "cancel_unsent", Reason: "TEST explicit cancellation of unsent reminder", Version: d.Task.Version}); err != nil {
		t.Fatal(err)
	}
	s.Event.DispatchID = uuid.NewString()
	s.Event.IncidentSequence = 5
	s.Event.IsRecovered = true
	s.Event.RecoverTime = s.Event.FirstTriggerTime + 100
	recovery := enqueue(t, db, s)
	step(t, db)
	if d := state(t, db, recovery); d.Task.State != Accepted {
		t.Fatalf("unattempted reminder blocked recovery: %s: %s", d.Task.State, d.Task.Reason)
	}
	if d := state(t, db, reminder); d.Task.Attempts != 0 || d.Task.State == Accepted || d.Task.State == Confirmed {
		t.Fatal("unattempted reminder was sent or falsely confirmed")
	}
	if len(received) != 2 || received[1].AlarmType != "ok" {
		t.Fatalf("unexpected deliveries: %+v", received)
	}
}

func TestStrictBusinessStatusCannotRoundOrChooseDuplicate(t *testing.T) {
	for _, body := range []string{`{"status":200.00000000000000001}`, `{"status":500,"status":200}`, `{"status":200,"status":200}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer server.Close()
			db := testDB(t)
			id := enqueue(t, db, simulation(server.URL))
			step(t, db)
			if d := state(t, db, id); d.Task.State != Unknown {
				t.Fatalf("ambiguous/non-200 business status accepted: %s", d.Task.State)
			}
		})
	}
}

func TestCancelUnsentDoesNotPretendReceiptOrBypassUnknown(t *testing.T) {
	db := testDB(t)
	s := simulation("http://127.0.0.1:1/mock")
	id := enqueue(t, db, s)
	d := state(t, db, id)
	if err := Act(db, id, "test-admin", Action{Action: "cancel_unsent", Reason: "TEST explicit decision not to deliver", Version: d.Task.Version}); err != nil {
		t.Fatal(err)
	}
	d = state(t, db, id)
	if d.Task.State != "not_delivered" || d.Task.Attempts != 0 || len(d.Operations) != 1 || d.Operations[0].Actor != "test-admin" {
		t.Fatalf("cancellation was not retained distinctly: %+v", d)
	}
	s.Event.DispatchID = uuid.NewString()
	s.Event.IncidentSequence = 4
	s.Event.IsRecovered = true
	s.Event.RecoverTime = s.Event.FirstTriggerTime + 100
	recovery := enqueue(t, db, s)
	step(t, db)
	if state(t, db, recovery).Task.State != Waiting {
		t.Fatal("cancelled problem was treated as received")
	}
	s = simulation("http://127.0.0.1:1/mock")
	attempted := enqueue(t, db, s)
	step(t, db)
	d = state(t, db, attempted)
	if d.Task.State != Unknown {
		t.Fatal("transport failure not unknown")
	}
	if err := Act(db, attempted, "test-admin", Action{Action: "cancel_unsent", Reason: "cannot erase an ambiguous request", Version: d.Task.Version}); err == nil {
		t.Fatal("cancellation bypassed unknown protection")
	}
}

func TestSameDispatchCannotAliasOneEndpointWithDifferentDestinations(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); fmt.Fprint(w, `{"status":200}`) }))
	defer server.Close()
	db := testDB(t)
	s := simulation(server.URL)
	history := s.Event.ToHis(nil)
	if err := db.Create(history).Error; err != nil {
		t.Fatal(err)
	}
	s.Event.Id = history.Id
	params := map[string]interface{}{}
	for k, v := range s.Params {
		params[k] = v
	}
	var routes []Submission
	for _, destination := range []string{"TEST_alias_A", "TEST_alias_B"} {
		config := s.Config
		config.Destination = destination
		ch := models.NotifyChannelConfig{Name: destination, Ident: models.ExternalPushType, RequestType: models.ExternalPushType, Enable: true, RequestConfig: &models.RequestConfig{ExternalPushConfig: &config}}
		if err := db.Create(&ch).Error; err != nil {
			t.Fatal(err)
		}
		rule := models.NotifyRule{Name: destination, Enable: true, NotifyConfigs: []models.NotifyConfig{{ChannelID: ch.ID, Params: params}}}
		if err := db.Create(&rule).Error; err != nil {
			t.Fatal(err)
		}
		routes = append(routes, Submission{ChannelID: ch.ID, NotifyRuleID: rule.ID, Params: params, Event: &s.Event})
	}
	ids, err := RegisterDispatch(nctx.NewContext(context.Background(), db, true), DispatchSubmission{Routes: routes})
	if err == nil || len(ids) != 2 {
		t.Fatalf("endpoint aliases not explicitly rejected: ids=%v err=%v", ids, err)
	}
	step(t, db)
	for _, id := range ids {
		d := state(t, db, id)
		if d.Task.State != Invalid || d.Task.Attempts != 0 || !strings.Contains(d.Task.Reason, "destination") {
			t.Fatalf("alias escaped: %+v", d.Task)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("same endpoint received aliased dispatch")
	}
}
