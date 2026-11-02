package externalpush

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestCMDBSourcesFreezeAtRegistrationAndPairRecovery(t *testing.T) {
	for _, source := range []string{"rule_append_tags", "cmdb_model_business"} {
		t.Run(source, func(t *testing.T) {
			var got []Payload
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.ParseForm()
				var p Payload
				if err := json.Unmarshal([]byte(r.PostForm.Get("json")), &p); err != nil {
					t.Error(err)
				}
				got = append(got, p)
				fmt.Fprint(w, `{"status":200}`)
			}))
			defer srv.Close()
			db := testDB(t)
			store := &cmdb.Store{DB: db}
			if err := store.Migrate(); err != nil {
				t.Fatal(err)
			}
			if err := db.AutoMigrate(&models.AlertRule{}); err != nil {
				t.Fatal(err)
			}
			const name = "TEST_NODE_192.0.2.8:1443(TESTPORT2/3-)"
			var asset cmdb.Document
			if err := store.Write(func(tx *gorm.DB) error {
				var err error
				asset, err = store.Save(tx, cmdb.Document{"customer": "TEST_COMPANY", "serial": "TEST_SERIAL", "hostname": name, "type": "TEST_存储", "businessIp": "192.0.2.8", "model": "TEST_R650&+型", "business": "TEST_业务/系统"}, 1, true)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			alertRule := models.AlertRule{AppendTags: "service=TEST_app team=TEST_运维"}
			if err := db.Create(&alertRule).Error; err != nil {
				t.Fatal(err)
			}
			s := simulation(srv.URL)
			s.Event.RuleId = alertRule.Id
			s.Event.TagsJSON = []string{"ident=" + name, "metric_label=DO_NOT_EXPORT"}
			channel := models.NotifyChannelConfig{Name: "TEST_CMDB", Ident: models.ExternalPushType, RequestType: models.ExternalPushType, Enable: true, RequestConfig: &models.RequestConfig{ExternalPushConfig: &s.Config}}
			if err := db.Create(&channel).Error; err != nil {
				t.Fatal(err)
			}
			params := map[string]interface{}{"company_id": "TEST_COMPANY", "device_source": "cmdb", "cmdb_match_field": "hostname", "cmdb_match_label": "ident", "device_tags_source": source, "level_map": `{"1":"严重"}`, "repeat_policy": "all_dispatches"}
			rule := models.NotifyRule{Name: "TEST_CMDB_ROUTE", Enable: true, NotifyConfigs: []models.NotifyConfig{{ChannelID: channel.ID, Params: params}}}
			if err := db.Create(&rule).Error; err != nil {
				t.Fatal(err)
			}
			ctx := nctx.NewContext(context.Background(), db, true)
			register := func(e *models.AlertCurEvent) int64 {
				if e.Id == 0 {
					e.LastEvalTime = e.FirstTriggerTime
					if e.IsRecovered {
						e.LastEvalTime = e.RecoverTime
					}
					e.Tags = strings.Join(e.TagsJSON, ",,")
					h := e.ToHis(nil)
					if err := db.Create(h).Error; err != nil {
						t.Fatal(err)
					}
					e.Id = h.Id
				}
				ids, err := RegisterDispatch(ctx, DispatchSubmission{Routes: []Submission{{ChannelID: channel.ID, NotifyRuleID: rule.ID, Params: params, Event: e}}})
				if err != nil || len(ids) != 1 {
					t.Fatalf("register: %v %v", ids, err)
				}
				return ids[0]
			}
			id := register(&s.Event)
			asset["hostname"], asset["type"] = "TEST_RENAMED", "TEST_CHANGED"
			asset["model"], asset["business"] = "TEST_NEW_MODEL", "TEST_NEW_BUSINESS"
			if err := store.Write(func(tx *gorm.DB) error { _, err := store.Save(tx, asset, 1, false); return err }); err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&alertRule).Update("append_tags", "changed=TEST_LATER").Error; err != nil {
				t.Fatal(err)
			}
			if again := register(&s.Event); again != id {
				t.Fatal("duplicate registration created another task")
			}
			step(t, db)
			if len(got) != 1 || state(t, db, id).Task.State != Accepted || got[0].DeviceType != "TEST_存储" || got[0].DeviceName != name || got[0].DeviceTags == "" {
				t.Fatalf("wrong frozen fields: %+v; task=%+v", got, state(t, db, id).Task)
			}
			wantTags := "service=TEST_app team=TEST_运维"
			if source == "cmdb_model_business" {
				wantTags = "['TEST_R650&+型', 'TEST_业务/系统']"
			}
			if got[0].DeviceTags != wantTags {
				t.Fatalf("deviceTags: got %q want %q", got[0].DeviceTags, wantTags)
			}
			s.Event.Id = 0
			s.Event.DispatchID = uuid.NewString()
			s.Event.IncidentSequence = 2
			s.Event.IsRecovered = true
			s.Event.RecoverTime = s.Event.FirstTriggerTime + 60
			rid := register(&s.Event)
			step(t, db)
			if len(got) != 2 || state(t, db, rid).Task.State != Accepted || got[1].DeviceType != got[0].DeviceType || got[1].DeviceName != got[0].DeviceName || got[1].DeviceTags != got[0].DeviceTags || got[1].ProblemTime != got[0].ProblemTime {
				t.Fatalf("recovery lost original CMDB/rule fields: %+v; task=%+v", got, state(t, db, rid).Task)
			}
			for _, serial := range []string{"TEST_DUP1", "TEST_DUP2"} {
				if err := store.Write(func(tx *gorm.DB) error {
					_, err := store.Save(tx, cmdb.Document{"customer": "TEST_COMPANY", "serial": serial, "hostname": "TEST_DUPLICATE", "type": "TEST_主机"}, 1, true)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			for _, label := range []string{"TEST_MISSING", "TEST_DUPLICATE"} {
				e := s.Event
				e.Id, e.IsRecovered, e.IncidentSequence = 0, false, 1
				e.DispatchID, e.IncidentID = uuid.NewString(), uuid.NewString()
				e.TagsJSON = []string{"ident=" + label}
				bad := register(&e)
				step(t, db)
				d := state(t, db, bad)
				if d.Task.State != Invalid || d.Task.Attempts != 0 || !strings.Contains(d.Task.Reason, "exactly one active asset") || len(got) != 2 {
					t.Fatalf("missing/ambiguous CMDB lookup sent: %+v", d.Task)
				}
			}

			if source == "cmdb_model_business" {
				for _, field := range []string{"model", "business"} {
					incomplete := cmdb.Document{"customer": "TEST", "serial": "TEST_MISSING_" + field, "hostname": "TEST_INCOMPLETE_" + field, "type": "TEST_主机", "model": "TEST_MODEL", "business": "TEST_BUSINESS"}
					incomplete[field] = " "
					if err := store.Write(func(tx *gorm.DB) error { _, err := store.Save(tx, incomplete, 1, true); return err }); err != nil {
						t.Fatal(err)
					}
					e := s.Event
					e.Id, e.IsRecovered, e.IncidentSequence = 0, false, 1
					e.DispatchID, e.IncidentID = uuid.NewString(), uuid.NewString()
					e.TagsJSON = []string{"ident=TEST_INCOMPLETE_" + field}
					bad := register(&e)
					step(t, db)
					d := state(t, db, bad)
					if d.Task.State != Invalid || d.Task.Attempts != 0 || !strings.Contains(d.Task.Reason, "model and business") || len(got) != 2 {
						t.Fatalf("incomplete tags sent: %+v", d.Task)
					}
				}
			}
		})
	}
}

func TestCMDBTagsAreListTextInsideJSONString(t *testing.T) {
	for _, tc := range []struct{ model, business, want string }{
		{"R650", "HIS", "['R650', 'HIS']"},
		{"R'650", "HIS\\DB", "['R\\'650', 'HIS\\\\DB']"},
		{"R650\n中文", "HIS\t系统", "['R650\\u000a中文', 'HIS\\u0009系统']"},
	} {
		s := simulation("http://127.0.0.1:19091")
		s.Params["device_tags_source"] = "cmdb_model_business"
		delete(s.Params, "device_tags")
		s.Sources = &ResolvedSources{AssetID: "TEST_ASSET", Model: tc.model, Business: tc.business}
		p, err := Build(s)
		if err != nil {
			t.Fatal(err)
		}
		if p.DeviceTags != tc.want {
			t.Fatalf("got %q want %q", p.DeviceTags, tc.want)
		}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["deviceTags"] != tc.want {
			t.Fatalf("not the confirmed string type: %s", raw)
		}
	}
}
