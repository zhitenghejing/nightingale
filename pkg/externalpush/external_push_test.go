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
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/conf"
	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "push.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.AutoMigrate(&models.ExternalPushTask{}, &models.ExternalPushStream{}, &models.ExternalPushAttempt{}, &models.ExternalPushOperation{}, &models.NotifyChannelConfig{}, &models.NotifyRule{}, &models.AlertHisEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}
func simulation(endpoint string) Snapshot {
	return Snapshot{Config: models.ExternalPushConfig{Mode: "simulation", Endpoint: endpoint, Destination: "explicit-test-destination", TimeoutMS: 1000, ProblemType: "problem", RecoveryType: "ok", TestSign: "TEST_ONLY_+ &=%签名", TimeZone: "Asia/Shanghai"}, Params: map[string]string{"company_id": "测试客户+ &=%", "device_type": "TEST_virtualization", "level_map": "{\"1\":\"TEST_灾难\"}", "device_name_label": "ident", "device_tags": "['标签+ &=%', '单引号\\反斜线']", "repeat_policy": "all_dispatches"}, Event: models.AlertCurEvent{DispatchID: uuid.NewString(), IncidentID: uuid.NewString(), IncidentSequence: 1, FirstTriggerTime: 1786400002, TriggerTime: 1786400002, RuleName: "CPU>95% 中文+ &=%\"\n换行", Severity: 1, TagsJSON: []string{"ident=127.0.0.9"}}}
}

func TestPlatform10RequiresExplicitKnownLevelMapping(t *testing.T) {
	s := simulation("unused")
	s.Config.Mode, s.Config.Endpoint = "platform10", models.ExternalPushPlatform10Endpoint
	s.Config.SigningMode, s.Config.TestSign = "key_file", ""
	if _, err := Build(s); err == nil {
		t.Fatal("real mode accepted a simulation-only level")
	}
	for _, level := range []string{"灾难", "严重", "警告", "一般"} {
		s.Params["level_map"] = fmt.Sprintf(`{"1":%q}`, level)
		p, err := Build(s)
		if err != nil || p.Level != level {
			t.Fatal("explicit confirmed level rejected", err)
		}
	}
	s.Params["level_map"] = `{"2":"严重"}`
	if _, err := Build(s); err == nil {
		t.Fatal("missing severity mapping was inferred")
	}
}
func enqueue(t *testing.T, db *gorm.DB, s Snapshot) int64 {
	t.Helper()
	id, err := Enqueue(db, s, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func step(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := (Worker{DB: db}).Step(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func state(t *testing.T, db *gorm.DB, id int64) Detail {
	t.Helper()
	d, err := Get(db, id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSimulationFormAndLifecycle(t *testing.T) {
	var received []Payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded; charset=UTF-8" {
			t.Errorf("invalid request %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		if r.URL.RawQuery != "" {
			t.Error("payload must not be in URL query")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if len(r.PostForm) != 1 || len(r.PostForm["json"]) != 1 {
			t.Error("expected exactly one json form field")
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(r.PostForm.Get("json")), &obj); err != nil {
			t.Error(err)
		}
		if _, ok := obj["deviceTags"].(string); !ok {
			t.Error("deviceTags must remain a string")
		}
		var p Payload
		if err := json.Unmarshal([]byte(r.PostForm.Get("json")), &p); err != nil {
			t.Error(err)
		}
		received = append(received, p)
		fmt.Fprint(w, `{"status":200,"msg":"接受成功!"}`)
	}))
	defer srv.Close()
	db := testDB(t)
	s := simulation(srv.URL)
	want, _ := Build(s)
	// Recovery arrives before the first problem; it cannot synthesize a problem.
	recovered := s
	recovered.Event = *s.Event.DeepCopy()
	recovered.Event.DispatchID = uuid.NewString()
	recovered.Event.IncidentSequence = 3
	recovered.Event.IsRecovered = true
	recovered.Event.RecoverTime = s.Event.FirstTriggerTime + 120
	recovered.Event.RuleName = "renamed rule"
	rid := enqueue(t, db, recovered)
	step(t, db)
	if d := state(t, db, rid); d.Task.State != Waiting || len(d.Attempts) != 0 {
		t.Fatalf("orphan recovery: %+v", d)
	}
	pid := enqueue(t, db, s)
	repeated := s
	repeated.Event = *s.Event.DeepCopy()
	repeated.Event.DispatchID = uuid.NewString()
	repeated.Event.IncidentSequence = 2
	repeatID := enqueue(t, db, repeated)
	// Duplicate dispatches from rules/subscriptions do not amplify either phase.
	for i := 0; i < 4; i++ {
		if got := enqueue(t, db, s); got != pid {
			t.Fatal("duplicate task")
		}
	}
	step(t, db)
	step(t, db)
	if len(received) != 3 {
		t.Fatalf("expected first, repeated problem and recovery, got %d", len(received))
	}
	if received[0] != want {
		t.Fatalf("form JSON changed object: got %+v want %+v", received[0], want)
	}
	if received[2].AlarmType != "ok" || received[2].TriggerName != want.TriggerName || received[2].ProblemTime != want.ProblemTime {
		t.Fatalf("recovery did not pair with immutable first problem: %+v", received[2])
	}
	for _, id := range []int64{pid, repeatID, rid} {
		if d := state(t, db, id); d.Task.State != Accepted || len(d.Attempts) != 1 {
			t.Fatalf("not accepted: %+v", d)
		}
	}
	// Same hash and second timestamp are deliberately reused, but incident differs.
	next := s
	next.Event = *s.Event.DeepCopy()
	next.Event.DispatchID = uuid.NewString()
	next.Event.IncidentID = uuid.NewString()
	enqueue(t, db, next)
	step(t, db)
	if len(received) != 4 {
		t.Fatal("new incident was merged")
	}
}

func TestAcceptedThenTimeoutUnknownAndManualCAS(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(80 * time.Millisecond)
		fmt.Fprint(w, `{"status":200}`)
	}))
	defer srv.Close()
	db := testDB(t)
	s := simulation(srv.URL)
	s.Config.TimeoutMS = 20
	id := enqueue(t, db, s)
	step(t, db)
	step(t, db)
	d := state(t, db, id)
	if calls.Load() != 1 || d.Task.State != Unknown || len(d.Attempts) != 1 {
		t.Fatalf("ambiguous result was retried: %+v calls=%d", d, calls.Load())
	}
	recover := s
	recover.Event = *s.Event.DeepCopy()
	recover.Event.DispatchID = uuid.NewString()
	recover.Event.IncidentSequence = 2
	recover.Event.IsRecovered = true
	recover.Event.RecoverTime = s.Event.FirstTriggerTime + 60
	rid := enqueue(t, db, recover)
	step(t, db)
	if state(t, db, rid).Task.State != Waiting || calls.Load() != 1 {
		t.Fatal("recovery overtook unknown problem")
	}
	// Read-only queries never mutate state or retry.
	_ = state(t, db, id)
	_ = state(t, db, id)
	if calls.Load() != 1 {
		t.Fatal("query had side effects")
	}
	a := Action{Action: "confirm_received", Reason: "SIMULATION: checked mock receiver record", Version: d.Task.Version}
	if err := Act(db, id, "test-admin", a); err != nil {
		t.Fatal(err)
	}
	if err := Act(db, id, "test-admin", a); err == nil {
		t.Fatal("stale double click was accepted")
	}
	d = state(t, db, id)
	if d.Task.State != Confirmed || d.Attempts[0].State != Unknown || len(d.Operations) != 1 || d.Operations[0].Actor != "test-admin" {
		t.Fatalf("missing audit: %+v", d)
	}
	if err := Act(db, id, "test-admin", Action{Action: "authorize_resend", Reason: "must not resend confirmed receipt", Version: d.Task.Version}); err == nil {
		t.Fatal("manually confirmed receipt allowed another send")
	}
	step(t, db)
	if calls.Load() != 2 {
		t.Fatal("confirmed predecessor did not release recovery")
	}
}

func TestRestartDoesNotResendSending(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"status":200}`) }))
	defer srv.Close()
	db := testDB(t)
	s := simulation(srv.URL)
	id := enqueue(t, db, s)
	w := Worker{DB: db}
	task, attempt, err := w.claim(id)
	if err != nil || task == nil {
		t.Fatalf("claim: %v", err)
	}
	// Simulate receiver already saving followed by death before local completion.
	got, _ := send(context.Background(), *task)
	if got != Accepted {
		t.Fatal(got)
	}
	if err := db.Model(&models.ExternalPushTask{}).Where("id = ?", id).Update("lease_until", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.ExternalPushStream{}).Where("stream_key = ?", task.StreamKey).Update("lease_until", 1).Error; err != nil {
		t.Fatal(err)
	}
	// Close and reopen the file database to emulate a new process with no
	// in-memory tasks/locks retained.
	path := db.Dialector.(*sqlite.Dialector).DSN
	pool, _ := db.DB()
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	newPool, _ := db.DB()
	t.Cleanup(func() { newPool.Close() })
	step(t, db)
	step(t, db)
	d := state(t, db, id)
	if d.Task.State != Unknown || calls.Load() != 1 || d.Attempts[0].ID != attempt.ID || d.Attempts[0].State != Unknown {
		t.Fatalf("restart automatically resent: %+v calls=%d", d, calls.Load())
	}
	if err := Act(db, id, "admin", Action{Action: "authorize_resend", Reason: "TEST ONLY: intentionally authorize a second receiver record", Version: d.Task.Version}); err != nil {
		t.Fatal(err)
	}
	step(t, db)
	d = state(t, db, id)
	if calls.Load() != 2 || len(d.Attempts) != 2 || d.Task.State != Accepted || len(d.Operations) != 2 {
		t.Fatalf("explicit resend not audited: %+v", d)
	}
}

func TestResponsesNeverAssumeDownstreamSuccess(t *testing.T) {
	cases := []struct {
		name, body string
		http       int
		state      string
	}{
		{"accepted", `{"status":200}`, 200, Accepted}, {"number_decimal", `{"status":200.0}`, 201, Accepted},
		{"exact_exponent", `{"status":2e2}`, 200, Accepted}, {"fraction_exponent", `{"status":0.200E+3}`, 200, Accepted},
		{"near_exponent", `{"status":1.999999999999999999e2}`, 200, Unknown}, {"huge_exponent", `{"status":2e99999999999}`, 200, Unknown},
		{"business_failure", `{"status":500}`, 200, Unknown}, {"string_status", `{"status":"200"}`, 200, Unknown},
		{"http_failure", `{"status":200}`, 500, Unknown}, {"non_json", "oops", 200, Unknown}, {"empty", "", 200, Unknown},
		{"truncated", `{"status":200`, 200, Unknown}, {"trailing", `{"status":200}{}`, 200, Unknown}, {"array", `[{"status":200}]`, 200, Unknown},
		{"redirect", `{"status":200}`, 307, Unknown}, {"oversize", strings.Repeat("x", 65537), 200, Unknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "/again")
				w.WriteHeader(tc.http)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			db := testDB(t)
			id := enqueue(t, db, simulation(srv.URL))
			step(t, db)
			step(t, db)
			d := state(t, db, id)
			if d.Task.State != tc.state || calls.Load() != 1 {
				t.Fatalf("state=%s calls=%d", d.Task.State, calls.Load())
			}
			encoded, _ := json.Marshal(d)
			if strings.Contains(string(encoded), "TEST_ONLY") {
				t.Fatal("query exposed signature")
			}
		})
	}
}

func TestValidationRepeatPolicyAndConflict(t *testing.T) {
	db := testDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"status":200}`) }))
	defer srv.Close()
	s := simulation(srv.URL)
	s.Params["repeat_policy"] = "review_repeats"
	s.Event.IncidentSequence = 5
	first := enqueue(t, db, s)
	step(t, db)
	if state(t, db, first).Task.State != Accepted {
		t.Fatal("first actual delivery was mistaken for a repeat")
	}
	s.Event.DispatchID = uuid.NewString()
	s.Event.IncidentSequence = 6
	id := enqueue(t, db, s)
	step(t, db)
	if d := state(t, db, id); d.Task.State != Manual || d.Task.Attempts != 0 {
		t.Fatal("repeat reminder silently dropped or sent")
	}
	s.Event.DispatchID = uuid.NewString()
	s.Params["company_id"] = ""
	id = enqueue(t, db, s)
	step(t, db)
	if d := state(t, db, id); d.Task.State != Invalid || !strings.Contains(d.Task.Reason, "company_id") {
		t.Fatalf("missing mapping: %+v", d)
	}
	for _, endpoint := range []string{"https://admin.ztservice.cn/messageWarn!acceptWarn.act", "http://localhost:9000/mock", "http://127.0.0.1:9/?json=x", "http://127.0.0.1:9/#x"} {
		c := s.Config
		c.Endpoint = endpoint
		if ValidateConfig(&c) == nil {
			t.Fatalf("unsafe simulation endpoint allowed: %s", endpoint)
		}
	}
	s = simulation("http://127.0.0.1:1/mock")
	id = enqueue(t, db, s)
	changed := s
	changed.Params = map[string]string{}
	for k, v := range s.Params {
		changed.Params[k] = v
	}
	changed.Params["company_id"] = "different"
	if _, err := Enqueue(db, changed, 0, 99); err == nil {
		t.Fatal("conflicting route mappings silently merged")
	}
	d := state(t, db, id)
	if len(d.Operations) != 1 {
		t.Fatal("missing conflict audit")
	}
}

func TestConcurrentDuplicateRegistrationAndWorkers(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(time.Millisecond * 10)
		fmt.Fprint(w, `{"status":200}`)
	}))
	defer srv.Close()
	db := testDB(t)
	s := simulation(srv.URL)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Enqueue(db, s, 0, 99); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	var count int64
	db.Model(&models.ExternalPushTask{}).Count(&count)
	if count != 1 {
		t.Fatalf("created %d tasks", count)
	}
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := (Worker{DB: db}).Step(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("workers sent %d requests", calls.Load())
	}
}

func TestEdgeRegistersAtCenterWithoutSending(t *testing.T) {
	db := testDB(t)
	var sends, registrations atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends.Add(1); fmt.Fprint(w, `{"status":200}`) }))
	defer receiver.Close()
	s := simulation(receiver.URL)
	channel := models.NotifyChannelConfig{Name: "simulation", Ident: models.ExternalPushType, RequestType: models.ExternalPushType, Enable: true, RequestConfig: &models.RequestConfig{ExternalPushConfig: &s.Config}}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	centerCtx := nctx.NewContext(context.Background(), db, true)
	center := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		registrations.Add(1)
		if r.URL.Path != "/v1/n9e/external-push-tasks" {
			t.Error(r.URL.Path)
		}
		var in DispatchSubmission
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		ids, err := RegisterDispatch(centerCtx, in)
		if err != nil {
			t.Error(err)
		}
		data, _ := json.Marshal(ids)
		fmt.Fprintf(w, `{"dat":%s,"err":""}`, data)
	}))
	defer center.Close()
	edge := nctx.NewContext(context.Background(), nil, false, conf.CenterApi{Addrs: []string{center.URL}, Timeout: 1000})
	params := map[string]interface{}{}
	for k, v := range s.Params {
		params[k] = v
	}
	rule := models.NotifyRule{Name: "edge-simulation", Enable: true, NotifyConfigs: []models.NotifyConfig{{ChannelID: channel.ID, Params: params}}}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	source := s.Event
	source.Tags = "ident=127.0.0.9"
	source.LastEvalTime = source.FirstTriggerTime
	stored := source.ToHis(nil)
	if err := db.Create(stored).Error; err != nil {
		t.Fatal(err)
	}
	s.Event.Id = stored.Id
	in := Submission{ChannelID: channel.ID, NotifyRuleID: rule.ID, Params: params, Event: &s.Event}
	id, err := Register(edge, in)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := Register(edge, in)
	if err != nil || id != id2 {
		t.Fatalf("edge response retry not idempotent: %d %d %v", id, id2, err)
	}
	if sends.Load() != 0 || registrations.Load() != 2 {
		t.Fatal("edge sent externally")
	}
	step(t, db)
	if sends.Load() != 1 || state(t, db, id).Task.State != Accepted {
		t.Fatal("center did not send once")
	}
}

func TestSQLiteInitializationAndUpgradeSQL(t *testing.T) {
	// Validate the shipped full initializer in a fresh DB, not only AutoMigrate.
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "schema.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../docker/sqlite.sql")
	if err != nil {
		t.Fatal(err)
	}
	var clean []string
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			clean = append(clean, line)
		}
	}
	for _, stmt := range strings.Split(strings.Join(clean, "\n"), ";") {
		if strings.TrimSpace(stmt) != "" {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("initializer: %v", err)
			}
		}
	}
	for _, model := range []interface{}{&models.ExternalPushTask{}, &models.ExternalPushAttempt{}, &models.ExternalPushStream{}, &models.ExternalPushOperation{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatal("missing ledger table")
		}
	}
	for _, table := range []string{"alert_cur_event", "alert_his_event"} {
		for _, col := range []string{"dispatch_id", "incident_id", "incident_sequence"} {
			if !db.Migrator().HasColumn(table, col) {
				t.Fatalf("missing %s.%s", table, col)
			}
		}
	}
}

func TestBlockedQueueDoesNotStarveUnrelatedIncident(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"status":200}`) }))
	defer srv.Close()
	db := testDB(t)
	for i := 0; i < 100; i++ {
		s := simulation(srv.URL)
		s.Event.IsRecovered = true
		s.Event.RecoverTime = s.Event.FirstTriggerTime + 1
		s.Event.IncidentSequence = 2
		enqueue(t, db, s)
	}
	db.Model(&models.ExternalPushTask{}).Where("id <= ?", 100).Update("updated_at", 0)
	id := enqueue(t, db, simulation(srv.URL))
	step(t, db)
	step(t, db)
	if calls.Load() != 1 || state(t, db, id).Task.State != Accepted {
		t.Fatal("100 permanently blocked recoveries starved an unrelated problem")
	}
}

func TestApplicableDispatchConflictBlocksBeforeAnyRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{"status":200}`) }))
	defer srv.Close()
	db := testDB(t)
	s := simulation(srv.URL)
	channel := models.NotifyChannelConfig{Name: "simulation", Ident: models.ExternalPushType, RequestType: models.ExternalPushType, Enable: true, RequestConfig: &models.RequestConfig{ExternalPushConfig: &s.Config}}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	params := map[string]interface{}{}
	for k, v := range s.Params {
		params[k] = v
	}
	for i := 0; i < 2; i++ {
		copy := map[string]interface{}{}
		for k, v := range params {
			copy[k] = v
		}
		if i == 1 {
			copy["company_id"] = "conflicting-customer"
		}
		rule := models.NotifyRule{ID: int64(i + 1), Name: fmt.Sprint(i), Enable: true, NotifyConfigs: []models.NotifyConfig{{ChannelID: channel.ID, Params: copy}}}
		if err := db.Create(&rule).Error; err != nil {
			t.Fatal(err)
		}
	}
	source := s.Event
	source.Tags = "ident=127.0.0.9"
	source.LastEvalTime = source.FirstTriggerTime
	stored := source.ToHis(nil)
	if err := db.Create(stored).Error; err != nil {
		t.Fatal(err)
	}
	s.Event.Id = stored.Id
	other := map[string]interface{}{}
	for k, v := range params {
		other[k] = v
	}
	other["company_id"] = "conflicting-customer"
	ids, err := RegisterDispatch(nctx.NewContext(context.Background(), db, true), DispatchSubmission{Routes: []Submission{
		{ChannelID: channel.ID, NotifyRuleID: 1, Params: params, Event: &s.Event},
		{ChannelID: channel.ID, NotifyRuleID: 2, Params: other, Event: &s.Event},
	}})
	if err == nil || len(ids) != 1 {
		t.Fatalf("conflicting actual routes did not fail closed: ids=%v err=%v", ids, err)
	}
	id := ids[0]
	step(t, db)
	if d := state(t, db, id); calls.Load() != 0 || d.Task.State != Invalid || !strings.Contains(d.Task.Reason, "conflicting applicable") {
		t.Fatalf("ambiguous mapping escaped before fan-out finished: %+v", d.Task)
	}
}

func TestAmbiguousLevelMapAndSQLiteUpgrade(t *testing.T) {
	s := simulation("http://127.0.0.1:1/mock")
	s.Params["level_map"] = `{"1":"first","1":"different"}`
	if _, err := Build(s); err == nil {
		t.Fatal("ambiguous level mapping accepted")
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "upgrade.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"alert_cur_event", "alert_his_event"} {
		if err := db.Exec("CREATE TABLE " + table + " (id bigint PRIMARY KEY)").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO " + table + " (id) VALUES (1)").Error; err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../../docker/migratesql/external-push.sqlite.sql")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	for _, stmt := range strings.Split(strings.Join(lines, "\n"), ";") {
		if strings.TrimSpace(stmt) != "" {
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, table := range []string{"alert_cur_event", "alert_his_event"} {
		var row struct {
			DispatchID       string
			IncidentID       string
			IncidentSequence int64
		}
		if err := db.Table(table).Where("id = ?", 1).Scan(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.DispatchID != "" || row.IncidentID != "" || row.IncidentSequence != 0 {
			t.Fatal("migration fabricated a legacy lifecycle identity")
		}
	}
	if err := CheckSchema(db); err != nil {
		t.Fatal(err)
	}
}
