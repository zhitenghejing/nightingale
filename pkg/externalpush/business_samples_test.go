package externalpush

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/google/uuid"
)

type businessSample struct {
	Ref     string  `json:"ref"`
	Pair    string  `json:"pair"`
	Payload Payload `json:"payload"`
}

type sampleRequest struct {
	Method      string  `json:"method"`
	ContentType string  `json:"content_type"`
	Form        string  `json:"form_body"`
	Payload     Payload `json:"decoded_json"`
	HTTPStatus  int     `json:"response_http_status"`
	Response    string  `json:"response_body"`
}

// Characterization at the persistent task/HTTP boundary. These fixtures are
// synthetic business-value samples, not a parser or a confirmed wire protocol.
func TestSanitizedBusinessSamplesThroughPersistentDelivery(t *testing.T) {
	raw, err := os.ReadFile("testdata/business_samples.sanitized.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Assumptions map[string]string `json:"normalization_assumptions"`
		Records     []businessSample  `json:"records"`
		Boundaries  []businessSample  `json:"additional_boundary_cases"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures.Records) != 9 || len(fixtures.Boundaries) != 2 {
		t.Fatal("expected nine redacted source records and one separate synthetic boundary pair")
	}
	var received []sampleRequest
	var receiveMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded; charset=UTF-8" || len(form) != 1 || len(form["json"]) != 1 {
			t.Errorf("invalid form request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		var value Payload
		if err := json.Unmarshal([]byte(form.Get("json")), &value); err != nil {
			t.Error(err)
		}
		var fields map[string]interface{}
		if err := json.Unmarshal([]byte(form.Get("json")), &fields); err != nil {
			t.Error(err)
		}
		if len(fields) != 10 {
			t.Errorf("expected exactly ten protocol fields, got %d", len(fields))
		}
		if _, ok := fields["deviceTags"].(string); !ok {
			t.Error("normalization assumption violated: deviceTags must remain a JSON string")
		}
		const response = `{"status":200,"msg":"TEST_ACCEPTED_LOCAL_ONLY"}`
		receiveMu.Lock()
		received = append(received, sampleRequest{r.Method, r.Header.Get("Content-Type"), string(body), value, 200, response})
		receiveMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, response)
	}))
	defer server.Close()
	db := testDB(t)
	ctx := nctx.NewContext(context.Background(), db, true)
	config := models.ExternalPushConfig{Mode: "simulation", Endpoint: server.URL, Destination: "TEST_ONE_LOGICAL_RECEIVER", TimeoutMS: 1000, ProblemType: "problem", RecoveryType: "ok", TestSign: "TEST_ONLY_NOT_A_CUSTOMER_SIGNATURE", TimeZone: "Asia/Shanghai"}
	channel := models.NotifyChannelConfig{Name: "TEST same independent deployment", Ident: models.ExternalPushType, RequestType: models.ExternalPushType, Enable: true, RequestConfig: &models.RequestConfig{ExternalPushConfig: &config}}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	records := append(fixtures.Records, fixtures.Boundaries...)
	incidents := map[string]string{}
	tasks := map[string]int64{}
	loc, err := time.LoadLocation(config.TimeZone)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		p := record.Payload
		if p.Sign != config.TestSign {
			t.Fatal("fixture must never contain a customer signature")
		}
		if p.AlarmType != "problem" && p.AlarmType != "ok" {
			t.Fatal("unconfirmed alarm type")
		}
		params := map[string]interface{}{"company_id": p.CompanyID, "device_type": p.DeviceType, "device_name_label": "device_name", "device_tags": p.DeviceTags, "level_map": "{\"1\":\"严重\",\"2\":\"警告\"}", "repeat_policy": "all_dispatches"}
		rule := models.NotifyRule{Name: "TEST " + record.Ref, Enable: true, NotifyConfigs: []models.NotifyConfig{{ChannelID: channel.ID, Params: params, Severities: []int{1, 2}}}}
		if err := db.Create(&rule).Error; err != nil {
			t.Fatal(err)
		}
		problem, err := time.ParseInLocation("2006-01-02 15:04:05", p.ProblemTime, loc)
		if err != nil {
			t.Fatal(err)
		}
		pair := record.Pair
		if pair == "" {
			pair = "orphan-" + record.Ref
		}
		if incidents[pair] == "" {
			incidents[pair] = uuid.NewString()
		}
		event := models.AlertCurEvent{DispatchID: uuid.NewString(), IncidentID: incidents[pair], IncidentSequence: 1, FirstTriggerTime: problem.Unix(), TriggerTime: problem.Unix(), LastEvalTime: problem.Unix(), RuleName: p.TriggerName, RuleConfig: "{}", Severity: 1, Tags: "device_name=" + p.DeviceName, TagsJSON: []string{"device_name=" + p.DeviceName}}
		if p.Level == "警告" {
			event.Severity = 2
		}
		if p.AlarmType == "ok" {
			recovery, err := time.ParseInLocation("2006-01-02 15:04:05", p.RecoveryTime, loc)
			if err != nil {
				t.Fatal(err)
			}
			event.IsRecovered = true
			event.IncidentSequence = 2
			event.RecoverTime = recovery.Unix()
			event.LastEvalTime = recovery.Unix()
		}
		history := event.ToHis(nil)
		if err := db.Create(history).Error; err != nil {
			t.Fatal(err)
		}
		event.Id = history.Id
		route := Submission{ChannelID: channel.ID, NotifyRuleID: rule.ID, Params: params, Event: &event}
		for duplicate := 0; duplicate < 2; duplicate++ {
			ids, err := RegisterDispatch(ctx, DispatchSubmission{Routes: []Submission{route, route}})
			if err != nil {
				t.Fatalf("%s registration: %v", record.Ref, err)
			}
			if len(ids) != 1 {
				t.Fatalf("%s fanout amplified tasks: %v", record.Ref, ids)
			}
			if previous := tasks[record.Ref]; previous != 0 && previous != ids[0] {
				t.Fatal("repeated dispatch created another task")
			}
			tasks[record.Ref] = ids[0]
		}
		step(t, db)
	}
	for i := 0; i < 3; i++ {
		step(t, db)
	}
	receiveMu.Lock()
	defer receiveMu.Unlock()
	if len(received) != 8 {
		t.Fatalf("expected six source-pair requests plus two synthetic boundaries; got %d", len(received))
	}
	uniqueTasks := map[int64]bool{}
	var taskEvidence []map[string]interface{}
	for _, record := range records {
		id := tasks[record.Ref]
		if uniqueTasks[id] {
			t.Fatal("different events were merged, possibly using sign or business time")
		}
		uniqueTasks[id] = true
		detail := state(t, db, id)
		taskEvidence = append(taskEvidence, map[string]interface{}{"ref": record.Ref, "task_id": id, "state": detail.Task.State, "attempts": detail.Task.Attempts, "reason": detail.Task.Reason})
		if record.Pair == "" {
			if detail.Task.State != Waiting || detail.Task.Attempts != 0 || !strings.Contains(detail.Task.Reason, "no problem has been delivered") {
				t.Fatalf("%s orphan was sent, fabricated or mis-associated: %+v", record.Ref, detail.Task)
			}
			continue
		}
		if detail.Task.State != Accepted || len(detail.Attempts) != 1 {
			t.Fatalf("%s not accepted once: %+v", record.Ref, detail.Task)
		}
		found := 0
		for _, request := range received {
			if request.Payload == record.Payload {
				found++
			}
		}
		if found != 1 {
			t.Fatalf("%s fields changed or request multiplied: matched %d times", record.Ref, found)
		}
	}
	for _, pair := range []string{"P1", "P2", "P3", "B1"} {
		problemIndex, recoveryIndex := -1, -1
		for _, record := range records {
			if record.Pair != pair {
				continue
			}
			for i, request := range received {
				if request.Payload == record.Payload {
					if record.Payload.AlarmType == "problem" {
						problemIndex = i
					} else {
						recoveryIndex = i
					}
				}
			}
		}
		if problemIndex < 0 || recoveryIndex <= problemIndex || received[problemIndex].Payload.ProblemTime != received[recoveryIndex].Payload.ProblemTime || received[recoveryIndex].Payload.RecoveryTime == "" {
			t.Fatalf("%s failed local dependency/time preservation", pair)
		}
	}
	if path := os.Getenv("N9E_EXTERNAL_PUSH_SAMPLE_EVIDENCE"); path != "" {
		evidence := map[string]interface{}{"scope": "LOCAL SIMULATION ONLY; no production or personnel notification", "normalization_assumptions": fixtures.Assumptions, "endpoint": server.URL, "request_count": len(received), "requests": received, "tasks": taskEvidence}
		raw, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("local sample adaptation: 3 source pairs + 1 synthetic boundary pair accepted once; 3 orphan recoveries retained; 11 distinct dispatch tasks share one explicit test signature")
}

func TestLegacySimulationSnapshotIsNotSilentlyConvertedOrSent(t *testing.T) {
	var requests int
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; fmt.Fprint(w, `{"status":200}`) }))
	defer receiver.Close()
	db := testDB(t)
	snapshot := simulation(receiver.URL)
	snapshot.Config.ProblemType = "TEST_problem"
	snapshot.Config.RecoveryType = "TEST_recovery"
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the persisted pre-confirmation shape, rather than registering a new
	// invalid configuration through today's validation gate.
	stream := models.ExternalPushStream{Key: uuid.NewString()}
	if err := db.Create(&stream).Error; err != nil {
		t.Fatal(err)
	}
	task := models.ExternalPushTask{DedupeKey: uuid.NewString(), StreamKey: stream.Key, Destination: snapshot.Config.Destination, DispatchID: snapshot.Event.DispatchID, IncidentID: snapshot.Event.IncidentID, Sequence: 1, Kind: "problem", State: Pending, Reason: "TEST legacy pending snapshot", Snapshot: string(raw), Payload: "{}", Version: 1}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	step(t, db)
	detail := state(t, db, task.ID)
	if detail.Task.State != Invalid || detail.Task.Attempts != 0 || !strings.Contains(detail.Task.Reason, "confirmed event types") || requests != 0 {
		t.Fatalf("legacy snapshot changed or sent: %+v requests=%d", detail.Task, requests)
	}
}
