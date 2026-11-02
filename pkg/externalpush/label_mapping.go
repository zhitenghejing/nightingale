package externalpush

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const LabelMappingVersion = "event_labels_v1"

type tagLabel struct {
	Label    string `json:"label"`
	Required *bool  `json:"required"`
}

// Reject repeated members as well as duplicate source labels. A repeated
// required flag must not silently weaken a source from required to optional.
func (item *tagLabel) UnmarshalJSON(raw []byte) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("invalid label source object")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return errors.New("duplicate label source member")
		}
		seen[key] = true
		switch key {
		case "label":
			if err := d.Decode(&item.Label); err != nil {
				return err
			}
		case "required":
			var value *bool
			if err := d.Decode(&value); err != nil {
				return err
			}
			if value == nil {
				return errors.New("required must be boolean")
			}
			item.Required = value
		default:
			return errors.New("unknown label source member")
		}
	}
	_, err = d.Token()
	return err
}

// TagOmission describes an optional source omitted from a frozen request. It
// contains source keys and reason codes, never signing material or tag values.
type TagOmission struct {
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

func tagLabels(raw string) ([]tagLabel, error) {
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	var labels []tagLabel
	if err := d.Decode(&labels); err != nil {
		return nil, errors.New("deviceTags source list must contain label and explicit required boolean")
	}
	if err := d.Decode(new(interface{})); err != io.EOF {
		return nil, errors.New("deviceTags source list has trailing content")
	}
	if len(labels) == 0 || len(labels) > 64 {
		return nil, errors.New("deviceTags requires 1 to 64 source labels")
	}
	seen := map[string]bool{}
	for _, item := range labels {
		if item.Label == "" || strings.TrimSpace(item.Label) != item.Label || item.Required == nil {
			return nil, errors.New("deviceTags source label and explicit required boolean are required")
		}
		if seen[item.Label] {
			return nil, fmt.Errorf("deviceTags source label %s is configured more than once", item.Label)
		}
		seen[item.Label] = true
	}
	return labels, nil
}

func validateLabelMapping(params map[string]string) error {
	if params["mapping_version"] != "" && params["mapping_version"] != LabelMappingVersion {
		return errors.New("unsupported external push mapping version")
	}
	if params["mapping_version"] != LabelMappingVersion {
		return nil
	}
	if params["device_source"] != "event" || params["device_tags_source"] != "event_labels" || params["device_type"] != "" || params["device_tags"] != "" || params["device_tags_label"] != "" || params["cmdb_match_field"] != "" || params["cmdb_match_label"] != "" {
		return errors.New("event label mapping must not retain CMDB or literal device mappings")
	}
	for _, key := range []string{"device_type_label", "device_name_label"} {
		if strings.TrimSpace(params[key]) == "" {
			return fmt.Errorf("%s is required", key)
		}
	}
	if strings.TrimSpace(params["company_id"]) != "" && strings.TrimSpace(params["company_id_label"]) != "" {
		return errors.New("companyId must select exactly one fixed or label source")
	}
	if strings.TrimSpace(params["company_id"]) == "" && strings.TrimSpace(params["company_id_label"]) == "" {
		return errors.New("companyId requires a fixed or label source")
	}
	_, err := tagLabels(params["device_tags_labels"])
	return err
}

func joinedTagLabels(s Snapshot) (string, []TagOmission, error) {
	labels, err := tagLabels(s.Params["device_tags_labels"])
	if err != nil {
		return "", nil, err
	}
	values := []string{}
	omissions := []TagOmission{}
	for _, item := range labels {
		candidates := map[string]bool{}
		present := false
		if value, ok := s.Event.TagsMap[item.Label]; ok {
			present = true
			if strings.TrimSpace(value) != "" {
				candidates[value] = true
			}
		}
		for _, tag := range s.Event.TagsJSON {
			pair := strings.SplitN(tag, "=", 2)
			if len(pair) == 2 && pair[0] == item.Label {
				present = true
				if strings.TrimSpace(pair[1]) != "" {
					candidates[pair[1]] = true
				}
			}
		}
		if len(candidates) > 1 {
			return "", nil, fmt.Errorf("deviceTags label %s has ambiguous values", item.Label)
		}
		if len(candidates) == 0 {
			if *item.Required {
				return "", nil, fmt.Errorf("deviceTags required label %s is missing or empty", item.Label)
			}
			reason := "missing"
			if present {
				reason = "empty"
			}
			omissions = append(omissions, TagOmission{Label: item.Label, Reason: reason})
			continue
		}
		for value := range candidates {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return "", omissions, errors.New("deviceTags has no nonempty values after optional labels were omitted")
	}
	return strings.Join(values, ","), omissions, nil
}
