package externalpush

import (
	"encoding/json"
	"errors"
	"reflect"

	"github.com/ccfos/nightingale/v6/models"
	"gorm.io/gorm"
)

var protocolParams = []string{"company_id", "device_type", "level_map", "device_name_label", "device_tags", "repeat_policy"}

func routeParams(in map[string]interface{}) map[string]string {
	out := map[string]string{}
	for _, k := range protocolParams {
		out[k], _ = in[k].(string)
	}
	// Omit absent optional keys so persisted literal-only snapshots retain their
	// existing fingerprint and route-consensus representation.
	for _, k := range []string{"company_id_label", "device_type_label", "device_tags_label", "device_source", "cmdb_match_field", "cmdb_match_label", "device_tags_source", "mapping_version", "device_tags_labels"} {
		if value, ok := in[k].(string); ok && value != "" {
			out[k] = value
		}
	}
	return out
}

// routeConsensus checks only references in this sealed dispatch plan. Different
// events may use different business mappings at the same destination/endpoint.
func routeConsensus(db *gorm.DB, s Snapshot, ruleID int64) error {
	refs := s.Routes
	if len(refs) == 0 {
		return errors.New("persisted task has no complete dispatch route plan; manual reconciliation required")
	}
	for _, ref := range refs {
		var channel models.NotifyChannelConfig
		if err := db.First(&channel, ref.ChannelID).Error; err != nil {
			return errors.New("selected saved channel is missing")
		}
		if !channel.Enable || channel.RequestType != models.ExternalPushType || channel.RequestConfig == nil || channel.RequestConfig.ExternalPushConfig == nil || !reflect.DeepEqual(*channel.RequestConfig.ExternalPushConfig, s.Config) {
			return errors.New("selected channel was disabled or its protocol configuration changed")
		}
		var rule models.NotifyRule
		if err := db.First(&rule, ref.NotifyRuleID).Error; err != nil || !rule.Enable {
			return errors.New("selected saved notification rule is missing or disabled")
		}
		matched := false
		for _, config := range rule.NotifyConfigs {
			if config.ChannelID == ref.ChannelID && reflect.DeepEqual(effectiveProtocolParams(routeParams(config.Params)), effectiveProtocolParams(s.Params)) {
				matched = true
			}
		}
		if !matched {
			return errors.New("selected saved notification route no longer matches the dispatch snapshot")
		}
	}
	return nil
}

func canonicalEvent(db *gorm.DB, e *models.AlertCurEvent) (models.AlertCurEvent, error) {
	// The primary key is only an indexed locator. UUID/incident/sequence below
	// are the identity; repeated persistence may yield different locator IDs.
	var original models.AlertHisEvent
	if err := db.Where("id = ? AND dispatch_id = ?", e.Id, e.DispatchID).First(&original).Error; err != nil {
		return models.AlertCurEvent{}, errors.New("original persisted dispatch was not found; refusing to infer lifecycle identity")
	}
	if original.IncidentID != e.IncidentID || original.IncidentSequence != e.IncidentSequence || (original.IsRecovered == 1) != e.IsRecovered {
		return models.AlertCurEvent{}, errors.New("dispatch identity conflicts with original persisted incident")
	}
	clean := original.ToCur()
	// ToCur fills label arrays; do not parse unrelated legacy rule JSON here.
	clean.TagsMap = nil
	return models.AlertCurEvent{Id: clean.Id, RuleId: clean.RuleId, DispatchID: clean.DispatchID, IncidentID: clean.IncidentID, IncidentSequence: clean.IncidentSequence, IsRecovered: clean.IsRecovered, FirstTriggerTime: clean.FirstTriggerTime, RecoverTime: clean.RecoverTime, RuleName: clean.RuleName, Severity: clean.Severity, TagsJSON: clean.TagsJSON}, nil
}

// Keep the immutable payload byte representation independent of extra event
// fields that a subscription or a pipeline adds to a local notification copy.
func protocolFingerprint(s Snapshot, p Payload) string {
	b, _ := json.Marshal(struct {
		Config    models.ExternalPushConfig
		Params    map[string]string
		Payload   Payload
		Incident  string
		Sequence  int64
		Recovered bool
	}{s.Config, effectiveProtocolParams(s.Params), p, s.Event.IncidentID, s.Event.IncidentSequence, s.Event.IsRecovered})
	return digest(string(b))
}

// Retain the retired field in stored snapshots, but never let its old value
// gate reminders or create a false conflict between otherwise equal routes.
func effectiveProtocolParams(params map[string]string) map[string]string {
	out := make(map[string]string, len(params)+1)
	for k, v := range params {
		out[k] = v
	}
	out["repeat_policy"] = "all_dispatches"
	return out
}
