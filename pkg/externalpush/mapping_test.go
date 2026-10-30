package externalpush

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/google/uuid"
)

// Rule-level label mappings resolve against the original persisted event. No
// customer identifier or presentation vocabulary is inferred from local IDs.
func TestLabelMappingsThroughSavedRulesAndRecovery(t *testing.T) {
	var got []Payload
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		var p Payload
		if err := json.Unmarshal([]byte(r.PostForm.Get("json")), &p); err != nil {
			t.Error(err)
		}
		got = append(got, p)
		fmt.Fprint(w, `{"status":200}`)
	}))
	defer receiver.Close()
	db := testDB(t)
	ctx := nctx.NewContext(context.Background(), db, true)
	s := simulation(receiver.URL)
	channel := models.NotifyChannelConfig{Name: "TEST label mapping", RequestType: models.ExternalPushType, Enable: true, RequestConfig: &models.RequestConfig{ExternalPushConfig: &s.Config}}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	params := map[string]interface{}{"company_id_label": "external_company", "device_type_label": "external_type", "device_tags_label": "external_tags", "device_name_label": "ident", "level_map": "{\"1\":\"TEST_严重\"}", "repeat_policy": "all_dispatches"}
	rule := models.NotifyRule{Name: "TEST label route", Enable: true, NotifyConfigs: []models.NotifyConfig{{ChannelID: channel.ID, Params: params}}}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	register := func(e models.AlertCurEvent) int64 {
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
		// A notification copy must not override the source event's business values.
		e.TagsJSON = []string{"external_company=TEST tampered copy"}
		ids, err := RegisterDispatch(ctx, DispatchSubmission{Routes: []Submission{{ChannelID: channel.ID, NotifyRuleID: rule.ID, Params: params, Event: &e}}})
		if err != nil || len(ids) != 1 {
			t.Fatalf("register: %v %v", ids, err)
		}
		return ids[0]
	}
	for _, site := range []string{"TEST_总部", "TEST_分部"} {
		event := s.Event
		event.DispatchID = uuid.NewString()
		event.IncidentID = uuid.NewString()
		deviceType := "TEST_主机"
		if site == "TEST_分部" {
			deviceType = "TEST_网络设备"
		}
		event.TagsJSON = []string{"ident=TEST_NODE_192.0.2.8:1443(TESTPORT2/3-)", "external_company=" + site, "external_type=" + deviceType, "external_tags=['TEST_db', 'TEST_业务等级B']"}
		pid := register(event)
		step(t, db)
		if state(t, db, pid).Task.State != Accepted {
			t.Fatalf("label mapping rejected: %+v", state(t, db, pid).Task)
		}
		event.DispatchID = uuid.NewString()
		event.IncidentSequence = 2
		event.IsRecovered = true
		event.RecoverTime = event.FirstTriggerTime + 722
		event.TagsJSON = []string{"ident=TEST_CHANGED", "external_company=TEST_CHANGED", "external_type=TEST_CHANGED", "external_tags=TEST_CHANGED"}
		rid := register(event)
		step(t, db)
		if state(t, db, rid).Task.State != Accepted {
			t.Fatal("recovery rejected")
		}
	}
	if len(got) != 4 {
		t.Fatalf("got %d requests", len(got))
	}
	for i := 0; i < 4; i += 2 {
		if got[i].CompanyID != []string{"TEST_总部", "TEST_分部"}[i/2] || got[i].DeviceName != "TEST_NODE_192.0.2.8:1443(TESTPORT2/3-)" {
			t.Fatalf("wrong original source: %+v", got[i])
		}
		p, r := got[i], got[i+1]
		p.AlarmType = "ok"
		p.RecoveryTime = r.RecoveryTime
		if p != r || r.ProblemTime != got[i].ProblemTime || r.RecoveryTime == "" {
			t.Fatal("recovery did not preserve original business fields")
		}
	}
	if path := os.Getenv("N9E_EXTERNAL_PUSH_FIELD_EVIDENCE"); path != "" {
		raw, err := json.MarshalIndent(map[string]interface{}{"scope": "LOCAL SYNTHETIC VALUES ONLY", "normalization": "10 JSON string fields; deviceTags string is a local assumption; fake signature is not a production signer", "endpoint": receiver.URL, "response_http_status": 200, "response_body": `{"status":200}`, "decoded_form_json": got}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuleMappingRejectsMissingConflictingAndAmbiguousLabels(t *testing.T) {
	s := simulation("http://127.0.0.1:19091/mock")
	for _, field := range []string{"company_id", "device_type", "device_tags"} {
		t.Run(field, func(t *testing.T) {
			snap := s
			snap.Params = map[string]string{}
			for k, v := range s.Params {
				snap.Params[k] = v
			}
			snap.Params[field+"_label"] = "source"
			if _, err := Build(snap); err == nil {
				t.Fatal("literal and label conflict accepted")
			}
			delete(snap.Params, field)
			if _, err := Build(snap); err == nil || !strings.Contains(err.Error(), field) {
				t.Fatal("missing label must identify the target mapping")
			}
			snap.Event.TagsJSON = append(snap.Event.TagsJSON, "source=TEST_A", "source=TEST_B")
			if _, err := Build(snap); err == nil {
				t.Fatal("ambiguous label accepted")
			}
		})
	}
}
