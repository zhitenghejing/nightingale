package cmdb

import (
	"encoding/json"
	"errors"
	"strings"
)

// AlertDevice exposes only the inventory fields needed by the center's event
// push adapter. It does not associate or modify Nightingale Target records.
type AlertDevice struct {
	ID       string
	Version  int
	Type     string
	Hostname string
	Model    string
	Business string
}

func (s *Store) LookupAlertDevice(field, value string) (AlertDevice, error) {
	var out AlertDevice
	if (field != "hostname" && field != "businessIp" && field != "managementIp") || strings.TrimSpace(value) == "" {
		return out, errors.New("CMDB match field and an exact nonempty event label are required")
	}
	// Asset documents have no indexed hostname/IP columns. Scan only active
	// documents without loading rack/disk data or relying on dialect-specific JSON.
	var rows []Asset
	if err := s.DB.Select("id", "version", "payload").Where("deleted = ?", false).Find(&rows).Error; err != nil {
		return out, errors.New("CMDB inventory could not be read")
	}
	count := 0
	for _, row := range rows {
		var data map[string]interface{}
		if json.Unmarshal([]byte(row.Payload), &data) != nil {
			return AlertDevice{}, errors.New("CMDB inventory contains an invalid document")
		}
		if match, _ := data[field].(string); match != value {
			continue
		}
		count++
		out.ID, out.Version = row.ID, row.Version
		out.Type, _ = data["type"].(string)
		out.Hostname, _ = data["hostname"].(string)
		out.Model, _ = data["model"].(string)
		out.Business, _ = data["business"].(string)
	}
	if count != 1 {
		return AlertDevice{}, errors.New("CMDB lookup must match exactly one active asset; missing or ambiguous device blocks delivery")
	}
	if strings.TrimSpace(out.Type) == "" || strings.TrimSpace(out.Hostname) == "" {
		return AlertDevice{}, errors.New("matched CMDB asset is missing device type or hostname")
	}
	return out, nil
}
