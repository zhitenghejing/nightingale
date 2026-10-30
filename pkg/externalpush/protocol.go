// Package externalpush owns durable external notification delivery. The first
// modes separate loopback simulation from the explicitly confirmed 1.0 receiver.
package externalpush

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
)

const (
	Pending      = "pending"
	Sending      = "sending"
	Accepted     = "accepted"
	Confirmed    = "confirmed_received"
	Invalid      = "validation_failed"
	Unknown      = "unknown"
	Waiting      = "waiting_predecessor"
	Manual       = "manual"
	NotDelivered = "not_delivered"
)

type Payload struct {
	AlarmType    string `json:"alarmType"`
	CompanyID    string `json:"companyId"`
	DeviceType   string `json:"deviceType"`
	DeviceName   string `json:"deviceName"`
	ProblemTime  string `json:"problemTime"`
	RecoveryTime string `json:"recoveryTime"`
	TriggerName  string `json:"triggerName"`
	Level        string `json:"level"`
	DeviceTags   string `json:"deviceTags"` // Confirmed string, including list-like text for CMDB model/business.
	Sign         string `json:"sign"`
}

type RouteReference struct {
	ChannelID    int64 `json:"channel_id"`
	NotifyRuleID int64 `json:"notify_rule_id"`
}

type Snapshot struct {
	Routes     []RouteReference          `json:"routes,omitempty"`
	InputError string                    `json:"validation_error,omitempty"`
	Config     models.ExternalPushConfig `json:"config"`
	Params     map[string]string         `json:"params"`
	Event      models.AlertCurEvent      `json:"event"`
	Sources    *ResolvedSources          `json:"sources,omitempty"`
}

// ValidateConfig is also used at save time; disabled incomplete drafts may be
// saved, but never produce a request. There is no production fallback.
func ValidateConfig(c *models.ExternalPushConfig) error { return c.Verify() }

func Build(s Snapshot) (Payload, error) {
	var p Payload
	if s.InputError != "" {
		return p, errors.New(s.InputError)
	}
	if s.Sources != nil && s.Sources.Error != "" {
		return p, errors.New(s.Sources.Error)
	}
	if err := ValidateConfig(&s.Config); err != nil {
		return p, err
	}
	e := s.Event
	if !validID(e.DispatchID) {
		return p, errors.New("missing or invalid dispatch UUID; no identity inferred from event ID or hash")
	}
	if !validID(e.IncidentID) || e.IncidentSequence < 1 {
		return p, errors.New("missing incident UUID/sequence; legacy or external events require explicit lifecycle identity")
	}
	if e.IsRecovered && e.IncidentSequence < 2 {
		return p, errors.New("recovery has no preceding problem sequence")
	}
	for _, k := range []string{"level_map", "repeat_policy"} {
		if strings.TrimSpace(s.Params[k]) == "" {
			return p, fmt.Errorf("required rule parameter %s is missing", k)
		}
	}
	policy := s.Params["repeat_policy"]
	if policy != "all_dispatches" && policy != "review_repeats" {
		return p, errors.New("repeat_policy must explicitly be all_dispatches or review_repeats (simulation assumptions)")
	}
	levels, err := decodeLevelMap(s.Params["level_map"])
	if err != nil {
		return p, errors.New("level_map must be an unambiguous JSON object of severity to text")
	}
	level := levels[strconv.Itoa(e.Severity)]
	if strings.TrimSpace(level) == "" {
		return p, errors.New("severity is not mapped; no production level is assumed")
	}
	if s.Config.Mode == "platform10" && level != "灾难" && level != "严重" && level != "警告" && level != "一般" {
		return p, errors.New("platform 1.0 level must explicitly map to one of the four confirmed receiver levels")
	}
	company, err := ruleValue(s, "company_id")
	if err != nil {
		return p, err
	}
	var deviceType, name string
	if s.Params["device_source"] == "cmdb" {
		if s.Params["device_type"] != "" || s.Params["device_type_label"] != "" || s.Params["device_name_label"] != "" {
			return p, errors.New("CMDB source cannot also configure literal or label device mappings")
		}
		var ok bool
		deviceType, name, ok = cmdbValues(s)
		if !ok {
			return p, errors.New("CMDB device values have not been snapshotted; configure an unambiguous asset match")
		}
	} else {
		if s.Params["device_source"] != "" && s.Params["device_source"] != "event" {
			return p, errors.New("device source must be event or cmdb")
		}
		deviceType, err = ruleValue(s, "device_type")
		if err != nil {
			return p, err
		}
		var ok bool
		name, ok = eventLabel(e, s.Params["device_name_label"])
		if !ok {
			return p, errors.New("device_name_label is missing or ambiguous; exactly one device value is required")
		}
	}
	var tags string
	if s.Params["device_tags_source"] == "cmdb_model_business" {
		if s.Params["device_tags"] != "" || s.Params["device_tags_label"] != "" {
			return p, errors.New("CMDB tags source cannot also configure literal or label tags")
		}
		if s.Sources == nil || s.Sources.AssetID == "" || strings.TrimSpace(s.Sources.Model) == "" || strings.TrimSpace(s.Sources.Business) == "" {
			return p, errors.New("matched CMDB asset must have model and business for deviceTags")
		}
		tags = "[" + singleQuotedTag(s.Sources.Model) + ", " + singleQuotedTag(s.Sources.Business) + "]"
	} else if s.Params["device_tags_source"] == "rule_append_tags" {
		if s.Params["device_tags"] != "" || s.Params["device_tags_label"] != "" {
			return p, errors.New("rule append tags source cannot also configure literal or label tags")
		}
		if s.Sources == nil {
			return p, errors.New("alert rule append tags have not been snapshotted")
		}
		tags = s.Sources.AppendTags
	} else {
		if s.Params["device_tags_source"] != "" && s.Params["device_tags_source"] != "mapping" {
			return p, errors.New("device tags source must be mapping, rule_append_tags or cmdb_model_business")
		}
		tags, err = ruleValue(s, "device_tags")
		if err != nil {
			return p, err
		}
	}
	if e.FirstTriggerTime <= 0 || e.RuleName == "" {
		return p, errors.New("problem time and trigger name are required")
	}
	loc, _ := time.LoadLocation(s.Config.TimeZone)
	p = Payload{AlarmType: s.Config.ProblemType, CompanyID: company, DeviceType: deviceType, DeviceName: name, ProblemTime: time.Unix(e.FirstTriggerTime, 0).In(loc).Format("2006-01-02 15:04:05"), TriggerName: e.RuleName, Level: level, DeviceTags: tags, Sign: s.Config.TestSign}
	if e.IsRecovered {
		if e.RecoverTime <= 0 || e.RecoverTime < e.FirstTriggerTime {
			return Payload{}, errors.New("valid recovery time is required")
		}
		p.AlarmType = s.Config.RecoveryType
		p.RecoveryTime = time.Unix(e.RecoverTime, 0).In(loc).Format("2006-01-02 15:04:05")
	}
	return p, nil
}

// A rule selects one explicit source; label values are not rendered as templates
// or parsed as addresses, JSON arrays, customer IDs or signatures.
func ruleValue(s Snapshot, field string) (string, error) {
	literal, label := s.Params[field], s.Params[field+"_label"]
	if strings.TrimSpace(literal) != "" && strings.TrimSpace(label) != "" {
		return "", fmt.Errorf("%s mapping selects both literal and label; choose exactly one source", field)
	}
	if strings.TrimSpace(label) != "" {
		value, ok := eventLabel(s.Event, label)
		if !ok {
			return "", fmt.Errorf("%s label mapping is missing or ambiguous in the original event", field)
		}
		return value, nil
	}
	if strings.TrimSpace(literal) == "" {
		return "", fmt.Errorf("required rule parameter %s is missing", field)
	}
	return literal, nil
}

func eventLabel(e models.AlertCurEvent, label string) (string, bool) {
	values := map[string]bool{}
	if value := e.TagsMap[label]; strings.TrimSpace(value) != "" {
		values[value] = true
	}
	for _, tag := range e.TagsJSON {
		pair := strings.SplitN(tag, "=", 2)
		if len(pair) == 2 && pair[0] == label && strings.TrimSpace(pair[1]) != "" {
			values[pair[1]] = true
		}
	}
	if len(values) != 1 {
		return "", false
	}
	for value := range values {
		return value, true
	}
	return "", false
}

func validID(s string) bool {
	id, err := uuid.Parse(s)
	return err == nil && id != uuid.Nil && id.String() == s
}

func decodeLevelMap(raw string) (map[string]string, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("object required")
	}
	result := map[string]string{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("invalid mapping key")
		}
		if _, exists := result[key]; exists {
			return nil, errors.New("duplicate severity mapping")
		}
		var value string
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return nil, errors.New("trailing mapping data")
	}
	return result, nil
}

// Keep the receiver's single-quoted list text inside a JSON string. Escape
// delimiters and control characters so an asset value cannot add list entries.
func singleQuotedTag(value string) string {
	var out strings.Builder
	out.WriteByte(39)
	for _, r := range value {
		switch r {
		case 39, 92:
			out.WriteByte(92)
			out.WriteRune(r)
		default:
			if r < 32 || r == 127 {
				fmt.Fprintf(&out, "\\u%04x", r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte(39)
	return out.String()
}
