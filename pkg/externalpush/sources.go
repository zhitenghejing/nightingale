package externalpush

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/ccfos/nightingale/v6/pkg/cmdb"
	"gorm.io/gorm"
)

// ResolvedSources is frozen when a task is first registered, never at send time.
// It contains no credentials or unrelated inventory fields.
type ResolvedSources struct {
	Error      string `json:"error,omitempty"`
	AssetID    string `json:"asset_id,omitempty"`
	Version    int    `json:"asset_version,omitempty"`
	DeviceType string `json:"device_type,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
	Model      string `json:"model,omitempty"`
	Business   string `json:"business,omitempty"`
	AppendTags string `json:"append_tags"`
}

func reuseSources(s *Snapshot, old Snapshot) bool {
	if old.Sources == nil || !reflect.DeepEqual(old.Config, s.Config) || !reflect.DeepEqual(old.Params, s.Params) || old.Event.RuleId != s.Event.RuleId {
		return false
	}
	label := s.Params["cmdb_match_label"]
	nowValue, nowOK := eventLabel(s.Event, label)
	oldValue, oldOK := eventLabel(old.Event, label)
	if nowValue != oldValue || nowOK != oldOK {
		return false
	}
	s.Sources = old.Sources
	return true
}

func resolveSources(db *gorm.DB, s *Snapshot) {
	if s.InputError != "" || (s.Params["device_source"] != "cmdb" && s.Params["device_tags_source"] != "rule_append_tags" && s.Params["device_tags_source"] != "cmdb_model_business") {
		return
	}
	if s.Config.Verify() != nil || !validID(s.Event.DispatchID) || !validID(s.Event.IncidentID) {
		return
	}
	s.Sources = &ResolvedSources{}
	// Retries of the same dispatch must reuse the first inventory/rule snapshot,
	// including validation failure. Configuration changes still conflict normally.
	var same models.ExternalPushTask
	err := db.Where("dedupe_key = ?", digest(s.Config.Destination+"\x00"+s.Event.DispatchID)).First(&same).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		s.Sources.Error = "existing source snapshot could not be read"
		return
	}
	if err == nil {
		var old Snapshot
		if json.Unmarshal([]byte(same.Snapshot), &old) == nil && reuseSources(s, old) {
			return
		}
	}
	if s.Event.IsRecovered {
		var problem models.ExternalPushTask
		err := db.Where("stream_key = ? AND kind = ? AND sequence < ? AND attempts > 0 AND state IN ?", digest(s.Config.Destination+"\x00"+s.Event.IncidentID), "problem", s.Event.IncidentSequence, []string{Accepted, Confirmed}).Order("sequence").First(&problem).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			s.Sources.Error = "predecessor source snapshot could not be read"
			return
		}
		if err == nil {
			var original Snapshot
			if json.Unmarshal([]byte(problem.Snapshot), &original) == nil && original.Sources != nil && original.Config.Mode == s.Config.Mode {
				s.Sources = original.Sources
				return
			}
		}
	}
	if s.Params["device_source"] == "cmdb" || s.Params["device_tags_source"] == "cmdb_model_business" {
		value, ok := eventLabel(s.Event, s.Params["cmdb_match_label"])
		if !ok {
			s.Sources.Error = "CMDB match label is missing or ambiguous; no asset inferred"
			return
		}
		asset, err := (&cmdb.Store{DB: db}).LookupAlertDevice(s.Params["cmdb_match_field"], value)
		if err != nil {
			s.Sources.Error = err.Error()
			return
		}
		s.Sources.AssetID, s.Sources.Version = asset.ID, asset.Version
		s.Sources.DeviceType, s.Sources.DeviceName = asset.Type, asset.Hostname
		s.Sources.Model, s.Sources.Business = asset.Model, asset.Business
	}
	if s.Params["device_tags_source"] == "rule_append_tags" {
		var rule models.AlertRule
		if s.Event.RuleId <= 0 || db.Select("id", "append_tags").First(&rule, s.Event.RuleId).Error != nil {
			s.Sources.Error = "source alert rule is missing; append tags cannot be snapshotted"
			return
		}
		// Preserve the saved field verbatim as one JSON string, not an array and
		// not the union of metric labels and appended labels.
		s.Sources.AppendTags = rule.AppendTags
	}
}

func cmdbValues(s Snapshot) (string, string, bool) {
	if s.Sources == nil || strings.TrimSpace(s.Sources.AssetID) == "" || strings.TrimSpace(s.Sources.DeviceType) == "" || strings.TrimSpace(s.Sources.DeviceName) == "" {
		return "", "", false
	}
	return s.Sources.DeviceType, s.Sources.DeviceName, true
}
