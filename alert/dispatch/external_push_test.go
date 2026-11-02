package dispatch

import (
	"context"
	"fmt"
	"github.com/ccfos/nightingale/v6/alert/astats"
	"github.com/prometheus/client_golang/prometheus"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/alert/sender"
	"github.com/ccfos/nightingale/v6/memsto"
	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/ccfos/nightingale/v6/pkg/externalpush"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestDedicatedDispatchUsesOnePersistentTaskNotRecipients(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "dispatch.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.NotifyChannelConfig{}, &models.ExternalPushTask{}, &models.ExternalPushStream{}, &models.ExternalPushOperation{}, &models.ExternalPushAttempt{}, &models.NotifyRule{}, &models.AlertHisEvent{}); err != nil {
		t.Fatal(err)
	}
	ctx := nctx.NewContext(context.Background(), db, true)
	channel := &models.NotifyChannelConfig{Name: "explicit simulation", Ident: models.ExternalPushType, Enable: true, RequestType: models.ExternalPushType, RequestConfig: &models.RequestConfig{ExternalPushConfig: &models.ExternalPushConfig{Mode: "simulation", Destination: "same-target", Endpoint: "http://127.0.0.1:1/mock", TimeoutMS: 100, ProblemType: "problem", RecoveryType: "ok", TestSign: "TEST_ONLY", TimeZone: "UTC"}}}
	if err := db.Create(channel).Error; err != nil {
		t.Fatal(err)
	}
	params := map[string]interface{}{"company_id": "TEST_company", "device_type": "TEST_device", "level_map": "{\"1\":\"TEST_level\"}", "device_name_label": "ident", "device_tags": "TEST string tags", "repeat_policy": "all_dispatches", "user_ids": []int64{1, 2, 3}, "user_group_ids": []int64{1, 2}}
	config := &models.NotifyConfig{ChannelID: channel.ID, Params: params, Severities: []int{1}}
	event := &models.AlertCurEvent{DispatchID: uuid.NewString(), IncidentID: uuid.NewString(), IncidentSequence: 1, FirstTriggerTime: 100, RuleName: "test", Severity: 1, TagsJSON: []string{"ident=one-device"}}
	source := event.DeepCopy()
	source.Tags = "ident=one-device"
	source.LastEvalTime = 100
	for i := 0; i < 3; i++ {
		stored := source.ToHis(nil)
		if err := db.Create(stored).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{1, 2, 3} {
		rule := models.NotifyRule{ID: id, Name: fmt.Sprint(id), Enable: true, NotifyConfigs: []models.NotifyConfig{*config}}
		if err := db.Create(&rule).Error; err != nil {
			t.Fatal(err)
		}
	}
	defer sender.NotifyRecordQueue.RemoveAll()
	sender.NotifyRecordQueue.RemoveAll()
	rules := &memsto.NotifyRuleCacheType{}
	ruleMap := map[int64]*models.NotifyRule{}
	for _, id := range []int64{1, 2, 3} {
		ruleMap[id] = &models.NotifyRule{ID: id, Enable: true, NotifyConfigs: []models.NotifyConfig{*config}}
	}
	rules.Set(ruleMap, 3, 1)
	channels := memsto.NewNotifyChannelCache(ctx, externalTestStats())
	d := &Dispatch{ctx: ctx, notifyRuleCache: rules, notifyChannelCache: channels, messageTemplateCache: &memsto.MessageTemplateCacheType{}}
	event.Id = 1
	event.NotifyRuleIds = []int64{1, 2, 3}
	// Real rule matching seals the complete route set before task registration.
	d.HandleEventWithNotifyRule(event)
	d.HandleEventWithNotifyRule(event.DeepCopy())
	var tasks []models.ExternalPushTask
	if err := db.Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Attempts != 0 || tasks[0].State != "pending" {
		t.Fatalf("fanout duplicated or sent: %+v", tasks)
	}
	if sender.NotifyRecordQueue.Len() != 0 {
		t.Fatal("registration created a misleading generic notification success")
	}
}

var externalStatsOnce sync.Once
var externalStats *memsto.Stats

func externalTestStats() *memsto.Stats {
	externalStatsOnce.Do(func() { externalStats = memsto.NewSyncStats() })
	return externalStats
}

func TestActualApplicableRulesConflictBeforeAnyWorkerRequest(t *testing.T) {
	var requests atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); fmt.Fprint(w, `{"status":200}`) }))
	defer receiver.Close()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "batch.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	pool.SetMaxOpenConns(1)
	defer pool.Close()
	if err := db.AutoMigrate(&models.NotifyChannelConfig{}, &models.NotifyRule{}, &models.AlertHisEvent{}, &models.ExternalPushTask{}, &models.ExternalPushStream{}, &models.ExternalPushAttempt{}, &models.ExternalPushOperation{}); err != nil {
		t.Fatal(err)
	}
	c := nctx.NewContext(context.Background(), db, true)
	channel := &models.NotifyChannelConfig{Name: "TEST mock", Ident: models.ExternalPushType, Enable: true, RequestType: models.ExternalPushType, RequestConfig: &models.RequestConfig{ExternalPushConfig: &models.ExternalPushConfig{Mode: "simulation", Destination: "one-destination", Endpoint: receiver.URL, TimeoutMS: 1000, ProblemType: "problem", RecoveryType: "ok", TestSign: "TEST_ONLY", TimeZone: "UTC"}}}
	if err := db.Create(channel).Error; err != nil {
		t.Fatal(err)
	}
	ruleMap := map[int64]*models.NotifyRule{}
	for i := int64(1); i <= 2; i++ {
		params := map[string]interface{}{"company_id": "TEST_company", "device_type": fmt.Sprintf("TEST_type_%d", i), "level_map": "{\"1\":\"TEST_level\"}", "device_name_label": "ident", "device_tags": "TEST_ONLY", "repeat_policy": "all_dispatches"}
		rule := &models.NotifyRule{ID: i, Name: fmt.Sprint(i), Enable: true, NotifyConfigs: []models.NotifyConfig{{ChannelID: channel.ID, Params: params, Severities: []int{1}}}}
		if err := db.Create(rule).Error; err != nil {
			t.Fatal(err)
		}
		ruleMap[i] = rule
	}
	event := &models.AlertCurEvent{DispatchID: uuid.NewString(), IncidentID: uuid.NewString(), IncidentSequence: 1, FirstTriggerTime: 100, LastEvalTime: 100, RuleName: "test", Severity: 1, Tags: "ident=TEST_device", TagsJSON: []string{"ident=TEST_device"}, NotifyRuleIds: []int64{1, 2}}
	history := event.ToHis(nil)
	if err := db.Create(history).Error; err != nil {
		t.Fatal(err)
	}
	event.Id = history.Id
	rules := &memsto.NotifyRuleCacheType{}
	rules.Set(ruleMap, 2, 1)
	d := &Dispatch{ctx: c, notifyRuleCache: rules, notifyChannelCache: memsto.NewNotifyChannelCache(c, externalTestStats()), messageTemplateCache: &memsto.MessageTemplateCacheType{}}
	stop := externalpush.Start(context.Background(), db)
	defer stop()
	d.HandleEventWithNotifyRule(event)
	var tasks []models.ExternalPushTask
	if err := db.Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one conflicting task: %+v", tasks)
	}
	detail, err := externalpush.Get(db, tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.State != externalpush.Invalid || detail.Task.Attempts != 0 || requests.Load() != 0 {
		t.Fatalf("applicable conflict escaped: state=%s attempts=%d requests=%d", detail.Task.State, detail.Task.Attempts, requests.Load())
	}
}

// Exercise the public root entry point, including the subscription's independent
// notification goroutine and its real event-drop pipeline before batch sealing.
func TestRootAndSubscriptionShareOneApplicableDispatchPlan(t *testing.T) {
	for _, dropSubscription := range []bool{false, true} {
		t.Run(fmt.Sprintf("subscription_dropped_%v", dropSubscription), func(t *testing.T) {
			var requests atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); fmt.Fprint(w, `{"status":200}`) }))
			defer receiver.Close()
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "subscription.db")), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			pool, _ := db.DB()
			pool.SetMaxOpenConns(1)
			defer pool.Close()
			if err := db.AutoMigrate(&models.EventPipelineExecution{}, &models.NotifyChannelConfig{}, &models.NotifyRule{}, &models.AlertHisEvent{}, &models.ExternalPushTask{}, &models.ExternalPushStream{}, &models.ExternalPushAttempt{}, &models.ExternalPushOperation{}); err != nil {
				t.Fatal(err)
			}
			c := nctx.NewContext(context.Background(), db, true)
			channel := &models.NotifyChannelConfig{Name: "TEST mock", Ident: models.ExternalPushType, Enable: true, RequestType: models.ExternalPushType, RequestConfig: &models.RequestConfig{ExternalPushConfig: &models.ExternalPushConfig{Mode: "simulation", Destination: "one-target", Endpoint: receiver.URL, TimeoutMS: 1000, ProblemType: "problem", RecoveryType: "ok", TestSign: "TEST_ONLY", TimeZone: "UTC"}}}
			if err := db.Create(channel).Error; err != nil {
				t.Fatal(err)
			}
			ruleMap := map[int64]*models.NotifyRule{}
			for i := int64(1); i <= 2; i++ {
				rule := &models.NotifyRule{ID: i, Name: fmt.Sprint(i), Enable: true, NotifyConfigs: []models.NotifyConfig{{ChannelID: channel.ID, Severities: []int{1}, Params: map[string]interface{}{"company_id": "TEST_company", "device_type": fmt.Sprintf("TEST_type_%d", i), "level_map": "{\"1\":\"TEST_level\"}", "device_name_label": "ident", "device_tags": "TEST_ONLY", "repeat_policy": "all_dispatches"}}}}
				if i == 2 && dropSubscription {
					rule.PipelineConfigs = []models.PipelineConfig{{PipelineId: 1, Enable: true}}
				}
				if err := db.Create(rule).Error; err != nil {
					t.Fatal(err)
				}
				ruleMap[i] = rule
			}
			rules := &memsto.NotifyRuleCacheType{}
			rules.Set(ruleMap, 2, 1)
			alerts := &memsto.AlertRuleCacheType{}
			alerts.Set(map[int64]*models.AlertRule{91: {Id: 91}}, 1, 1)
			subs := &memsto.AlertSubscribeCacheType{}
			subs.Set(map[int64][]*models.AlertSubscribe{91: {{Id: 1, NotifyRuleIds: []int64{2}}}}, 1, 1)
			pipelines := &memsto.EventProcessorCacheType{}
			pipelines.Set(map[int64]*models.EventPipeline{1: {ID: 1, ProcessorConfigs: []models.ProcessorConfig{{Typ: "event_drop", Config: map[string]interface{}{"content": "true"}}}}}, 1, 1)
			d := &Dispatch{ctx: c, alertRuleCache: alerts, alertSubscribeCache: subs, notifyRuleCache: rules, notifyChannelCache: memsto.NewNotifyChannelCache(c, externalTestStats()), messageTemplateCache: &memsto.MessageTemplateCacheType{}, eventProcessorCache: pipelines, userCache: &memsto.UserCacheType{}, userGroupCache: &memsto.UserGroupCacheType{}, notifyConfigCache: &memsto.NotifyConfigCacheType{}, BeforeSenderHook: func(*models.AlertCurEvent) bool { return true }, Astats: &astats.Stats{CounterSubEventTotal: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_external_subscriptions", Help: "Test subscription count"}, []string{"group"})}}
			event := &models.AlertCurEvent{RuleId: 91, DispatchID: uuid.NewString(), IncidentID: uuid.NewString(), IncidentSequence: 1, FirstTriggerTime: 100, TriggerTime: 100, LastEvalTime: 100, RuleName: "test", Severity: 1, Tags: "ident=TEST_device", TagsJSON: []string{"ident=TEST_device"}, NotifyRuleIds: []int64{1}}
			history := event.ToHis(nil)
			if err := db.Create(history).Error; err != nil {
				t.Fatal(err)
			}
			event.Id = history.Id
			// Initialize the pre-existing lazy script default before concurrent legacy sends.
			d.notifyConfigCache.GetNotifyScript()
			d.HandleEventNotify(event, false)
			var tasks []models.ExternalPushTask
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if err := db.Find(&tasks).Error; err != nil {
					t.Fatal(err)
				}
				if len(tasks) > 0 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if len(tasks) != 1 {
				t.Fatalf("expected one sealed task: %+v", tasks)
			}
			worker := externalpush.Worker{DB: db}
			for i := 0; i < 3; i++ {
				if err := worker.Step(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			detail, err := externalpush.Get(db, tasks[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			if dropSubscription {
				if detail.Task.State != externalpush.Accepted || requests.Load() != 1 {
					t.Fatalf("filtered route caused false conflict: %+v requests=%d", detail.Task, requests.Load())
				}
			} else if detail.Task.State != externalpush.Invalid || detail.Task.Attempts != 0 || requests.Load() != 0 {
				t.Fatalf("root/subscription conflict escaped: %+v requests=%d", detail.Task, requests.Load())
			}
		})
	}
}
