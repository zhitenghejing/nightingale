package externalpush

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func labelPlan(endpoint string) Snapshot {
	s := simulation(endpoint)
	s.Params = map[string]string{"mapping_version": "event_labels_v1", "company_id": "TEST_CUSTOMER", "device_source": "event", "device_type_label": "kind", "device_name_label": "node", "device_tags_source": "event_labels", "device_tags_labels": `[{"label":"model","required":true},{"label":"business","required":false},{"label":"role","required":false}]`, "level_map": `{"1":"灾难","2":"严重","3":"警告"}`, "repeat_policy": "once_per_incident"}
	s.Event.TagsJSON = []string{"kind=服务器", "node=测试_192.0.2.8:9100(端口/1)", "model=R650", "role=数据库 + &=%"}
	return s
}
func TestLabelPlanOptionalTagsAndRuleSeverity(t *testing.T) {
	s := labelPlan("http://127.0.0.1:19091/mock")
	p, err := Build(s)
	if err != nil {
		t.Fatal(err)
	}
	if p.DeviceTags != "R650,数据库 + &=%" || p.DeviceName != "测试_192.0.2.8:9100(端口/1)" || p.Level != "灾难" {
		t.Fatalf("wrong mapping: %+v", p)
	}
	s.Event.TagsJSON = append(s.Event.TagsJSON, "business=HIS,门诊", "alarm_level=严重")
	p, err = Build(s)
	if err != nil || p.DeviceTags != "R650,HIS,门诊,数据库 + &=%" || p.Level != "灾难" {
		t.Fatalf("complete values and original Severity must be retained: %+v %v", p, err)
	}
	s.Event.TagsJSON = append(s.Event.TagsJSON, "role=另一值")
	if _, err = Build(s); err == nil || !strings.Contains(err.Error(), "role") {
		t.Fatal("optional does not permit ambiguous labels", err)
	}
}
func TestRuleControlledRemindersAndRecoverySnapshot(t *testing.T) {
	var calls atomic.Int32
	var received []Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" {
			t.Error("not POST")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		var p Payload
		if err := json.Unmarshal([]byte(r.PostForm.Get("json")), &p); err != nil {
			t.Error(err)
		}
		received = append(received, p)
		fmt.Fprint(w, `{"status":200}`)
	}))
	defer server.Close()
	db := testDB(t)
	s := labelPlan(server.URL)
	pid := enqueue(t, db, s)
	step(t, db)
	if state(t, db, pid).Task.State != Accepted {
		t.Fatal("first problem rejected", state(t, db, pid).Task.Reason)
	}
	repeat := s
	repeat.Event = *s.Event.DeepCopy()
	repeat.Event.DispatchID = uuid.NewString()
	repeat.Event.IncidentSequence = 3
	repID := enqueue(t, db, repeat)
	step(t, db)
	if calls.Load() != 2 || state(t, db, repID).Task.State != Accepted || state(t, db, repID).Task.Attempts != 1 {
		t.Fatal("legitimate reminder was blocked by the legacy once policy")
	}
	recovery := repeat
	recovery.Event = *repeat.Event.DeepCopy()
	recovery.Event.DispatchID = uuid.NewString()
	recovery.Event.IncidentSequence = 5
	recovery.Event.IsRecovered = true
	recovery.Event.RecoverTime = s.Event.FirstTriggerTime + 100
	recovery.Event.TagsJSON = nil
	rid := enqueue(t, db, recovery)
	step(t, db)
	if state(t, db, rid).Task.State != Accepted || calls.Load() != 3 {
		t.Fatal("recovery must not depend on current labels", state(t, db, rid).Task.Reason)
	}
	if received[2].DeviceTags != received[0].DeviceTags || received[2].DeviceName != received[0].DeviceName || received[2].ProblemTime != received[0].ProblemTime || received[2].AlarmType != "ok" {
		t.Fatal("recovery lost frozen problem mapping")
	}
	recovery.Event.DispatchID = uuid.NewString()
	recovery.Event.IncidentSequence = 6
	duplicate := enqueue(t, db, recovery)
	step(t, db)
	if calls.Load() != 3 || state(t, db, duplicate).Task.State != NotDelivered {
		t.Fatal("second recovery was sent")
	}
	newer := s
	newer.Event = *s.Event.DeepCopy()
	newer.Event.DispatchID = uuid.NewString()
	newer.Event.IncidentID = uuid.NewString()
	newer.Event.FirstTriggerTime += 200
	id := enqueue(t, db, newer)
	if err := (Worker{DB: db}).Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 || state(t, db, id).Task.State != Accepted {
		t.Fatal("new incident mistaken for reminder")
	}
}

func TestLabelPlanValidationAndFrozenOmissions(t *testing.T) {
	s := labelPlan("http://127.0.0.1:19091/mock")
	for _, raw := range []string{`[]`, `[{"label":"model","required":null}]`, `[{"label":"model","required":true,"required":false}]`, `[{"label":"model"}]`, `[{"label":"model","required":"false"}]`, `[{"label":"model","required":true},{"label":"model","required":false}]`, `[{"label":" model","required":false}]`, `[{"label":"model","required":false,"fallback":"fake"}]`, `[{"label":"model","required":false}] {}`} {
		t.Run(raw, func(t *testing.T) {
			candidate := s
			candidate.Params = map[string]string{}
			for k, v := range s.Params {
				candidate.Params[k] = v
			}
			candidate.Params["device_tags_labels"] = raw
			if _, err := Build(candidate); err == nil {
				t.Fatal("invalid source list accepted")
			}
		})
	}
	db := testDB(t)
	id := enqueue(t, db, s)
	detail := state(t, db, id)
	if len(detail.OmittedTags) != 1 || detail.OmittedTags[0].Label != "business" || detail.OmittedTags[0].Reason != "missing" {
		t.Fatalf("omissions not queryable: %+v", detail.OmittedTags)
	}
	s.Event.TagsJSON = append(s.Event.TagsJSON, "business=HIS")
	if len(state(t, db, id).OmittedTags) != 1 {
		t.Fatal("mutable event changed persisted omission explanation")
	}
	for _, raw := range []string{`[{"label":"absent","required":false}]`, `[{"label":"absent","required":true}]`} {
		candidate := s
		candidate.Params = map[string]string{}
		for k, v := range s.Params {
			candidate.Params[k] = v
		}
		candidate.Params["device_tags_labels"] = raw
		if _, err := Build(candidate); err == nil {
			t.Fatal("empty final tags accepted")
		}
	}
}

func TestMappingPreviewReadsDisabledChannelWithoutTasksOrSignature(t *testing.T) {
	db := testDB(t)
	s := labelPlan("http://127.0.0.1:19091/mock")
	// A real-mode draft does not need a key file to inspect field mapping; no
	// network call or signer is available to this read-only interface.
	s.Config.Mode = "platform10"
	s.Config.Endpoint = models.ExternalPushPlatform10Endpoint
	s.Config.SigningMode = "key_file"
	s.Config.TestSign = ""
	ch := models.NotifyChannelConfig{Name: "TEST preview only", RequestType: models.ExternalPushType, Enable: false, RequestConfig: &models.RequestConfig{ExternalPushConfig: &s.Config}}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	s.Event.Tags = strings.Join(s.Event.TagsJSON, ",,")
	h := s.Event.ToHis(nil)
	if err := db.Create(h).Error; err != nil {
		t.Fatal(err)
	}
	params := map[string]interface{}{}
	for k, v := range s.Params {
		params[k] = v
	}
	result, err := Preview(db, PreviewInput{ChannelID: ch.ID, EventID: h.Id, Params: params})
	if err != nil || result.Payload == nil || result.Payload.DeviceTags != "R650,数据库 + &=%" || result.Payload.Sign != "[REDACTED]" || len(result.OmittedTags) != 1 {
		t.Fatalf("read-only preview: %+v %v", result, err)
	}
	var count int64
	if err := db.Model(&models.ExternalPushTask{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("preview created delivery tasks")
	}
}

func TestUnknownHoldsLaterDispatchesWithoutRetryingProblem(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, "not JSON after saving") }))
	defer server.Close()
	db := testDB(t)
	s := labelPlan(server.URL)
	s.Event.IncidentSequence = 7 // Earlier source snapshots were legitimately filtered.
	pid := enqueue(t, db, s)
	step(t, db)
	if state(t, db, pid).Task.State != Unknown {
		t.Fatal("ambiguous response must remain unknown")
	}
	repeat := s
	repeat.Event = *s.Event.DeepCopy()
	repeat.Event.DispatchID = uuid.NewString()
	repeat.Event.IncidentSequence = 9
	rep := enqueue(t, db, repeat)
	step(t, db)
	if state(t, db, rep).Task.State != Waiting || calls.Load() != 1 {
		t.Fatal("unknown problem caused an automatic reminder request")
	}
	repeat.Event.DispatchID = uuid.NewString()
	repeat.Event.IncidentSequence = 10
	repeat.Event.IsRecovered = true
	repeat.Event.RecoverTime = s.Event.FirstTriggerTime + 99
	repeat.Event.TagsJSON = nil
	rid := enqueue(t, db, repeat)
	for i := 0; i < 3; i++ {
		step(t, db)
	}
	if calls.Load() != 1 || state(t, db, rid).Task.State != Waiting || state(t, db, pid).Task.Attempts != 1 {
		t.Fatal("recovery bypassed unknown problem or request was retried")
	}
}

func TestLegacyReminderSettingsNoLongerGateFaultDispatches(t *testing.T) {
	for _, policy := range []string{"", "once_per_incident", "review_repeats", "all_dispatches"} {
		t.Run(policy, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"status":200}`) }))
			defer server.Close()
			db := testDB(t)
			s := labelPlan(server.URL)
			s.Params["repeat_policy"] = policy
			for i := int64(1); i <= 3; i++ {
				s.Event.DispatchID = uuid.NewString()
				s.Event.IncidentSequence = i
				id := enqueue(t, db, s)
				// Re-registering this dispatch does not add another task/request.
				if again := enqueue(t, db, s); again != id {
					t.Fatal("same dispatch created another task")
				}
				step(t, db)
				if d := state(t, db, id); d.Task.State != Accepted || d.Task.Attempts != 1 {
					t.Fatalf("policy %q gated a valid dispatch: %s", policy, d.Task.Reason)
				}
			}
			if calls.Load() != 3 {
				t.Fatalf("got %d requests for 3 dispatches", calls.Load())
			}
		})
	}
}

func TestRetiredPolicyDoesNotConflictAcrossRoutesOrRewriteHistory(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"status":200}`) }))
	defer server.Close()
	db := testDB(t)
	s := labelPlan(server.URL)
	ch := models.NotifyChannelConfig{Name: "TEST policy compatibility", Enable: true, RequestType: models.ExternalPushType, RequestConfig: &models.RequestConfig{ExternalPushConfig: &s.Config}}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}
	s.Event.Tags = strings.Join(s.Event.TagsJSON, ",,")
	his := s.Event.ToHis(nil)
	if err := db.Create(his).Error; err != nil {
		t.Fatal(err)
	}
	s.Event.Id = his.Id
	routes := []Submission{}
	for _, policy := range []string{"once_per_incident", "review_repeats", ""} {
		params := map[string]interface{}{}
		for k, v := range s.Params {
			params[k] = v
		}
		params["repeat_policy"] = policy
		rule := models.NotifyRule{Name: "TEST legacy " + policy, Enable: true, NotifyConfigs: []models.NotifyConfig{{ChannelID: ch.ID, Params: params}}}
		if err := db.Create(&rule).Error; err != nil {
			t.Fatal(err)
		}
		routes = append(routes, Submission{ChannelID: ch.ID, NotifyRuleID: rule.ID, Params: params, Event: &s.Event})
	}
	ids, err := RegisterDispatch(nctx.NewContext(context.Background(), db, true), DispatchSubmission{Routes: routes})
	if err != nil || len(ids) != 1 {
		t.Fatalf("retired metadata caused a route conflict: %v %v", ids, err)
	}
	// Historical fingerprints are opaque stored values; compare the frozen
	// protocol inputs without rewriting the hash, payload or audit trail.
	id := ids[0]
	before := state(t, db, id)
	const historicalHash = "historical-policy-fingerprint"
	if err := db.Model(&models.ExternalPushTask{}).Where("id = ?", id).Update("fingerprint", historicalHash).Error; err != nil {
		t.Fatal(err)
	}
	again, err := RegisterDispatch(nctx.NewContext(context.Background(), db, true), DispatchSubmission{Routes: routes})
	if err != nil || len(again) != 1 || again[0] != id {
		t.Fatalf("historical dispatch registration failed: %v %v", again, err)
	}
	after := state(t, db, id)
	if after.Task.Fingerprint != historicalHash || after.Task.Snapshot != before.Task.Snapshot || len(after.Operations) != 0 {
		t.Fatal("historical metadata was rewritten")
	}
	step(t, db)
	if calls.Load() != 1 || state(t, db, id).Task.State != Accepted {
		t.Fatal("same dispatch was blocked or amplified")
	}
}
