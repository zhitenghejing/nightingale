package collection

import (
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	"gorm.io/gorm"
)

// A package draft owns the complete bundle. Publishing consumes only this value;
// importing the native library is a one-time operation, not a publication hook.
type FieldCondition struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

type NotificationPreset struct {
	Mode    string  `json:"mode"`
	Message string  `json:"message"`
	RuleIDs []int64 `json:"rule_ids"`
}

func definitionFromReadme(readme string) (CatalogDefinition, bool) {
	const fence = "```device-onboarding\n"
	start := strings.Index(readme, fence)
	if start < 0 || strings.Count(readme, fence) != 1 {
		return CatalogDefinition{}, false
	}
	text := readme[start+len(fence):]
	end := strings.Index(text, "\n```")
	if end < 0 {
		return CatalogDefinition{}, false
	}
	var definition CatalogDefinition
	err := json.Unmarshal([]byte(text[:end]), &definition)
	return definition, err == nil
}

func validateDefinition(d CatalogDefinition, publishing bool) error {
	if d.Reachability != "" && d.Reachability != DefaultReachability {
		return bad("device templates require ICMP reachability")
	}
	switch d.Adapter {
	case "documentation", "snmp_v2c", "mysql", "sqlserver", "oracle", "pgsql", "redfish", "redis", "http":
	default:
		return bad("unknown collection adapter")
	}
	if d.Notification != "" && d.Notification != "events_only" && d.Notification != "inherit" && d.Notification != "rules" {
		return bad("invalid notification mode")
	}
	if d.Notify != nil {
		if d.Notify.Mode != "events_only" && d.Notify.Mode != "inherit" && d.Notify.Mode != "rules" {
			return bad("invalid notification mode")
		}
		if len([]rune(d.Notify.Message)) > 1024 || len(d.Notify.RuleIDs) > 16 {
			return bad("notification preset is too large")
		}
	}
	if len(d.Fields) > 64 || len(d.Config) > 65536 || len(canonical(d)) > 8<<20 {
		return bad("template draft is too large")
	}
	seen := map[string]int{}
	for i, f := range d.Fields {
		if !labelName.MatchString(f.Key) || strings.HasPrefix(f.Key, "__") || len(f.Key) > 64 || f.Label == "" || len([]rune(f.Label)) > 128 || len(f.Help) > 2048 {
			return bad("invalid device field")
		}
		if _, ok := seen[f.Key]; ok {
			return bad("duplicate device field")
		}
		seen[f.Key] = i
		switch f.Type {
		case "text", "password", "credential", "number", "switch", "select", "multiselect", "ip", "url", "endpoints":
		default:
			return bad("unsupported device field control")
		}
		if (f.Secret || f.Type == "password" || f.Type == "credential") && f.Default != nil {
			return bad("secret fields cannot have default values")
		}
		if f.Secret && f.Type != "password" {
			return bad("secret fields must use a password control")
		}
		if len(f.Options) > 64 || len(f.When) > 8 {
			return bad("too many field options or conditions")
		}
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			return bad("invalid field range")
		}
		if f.Default != nil {
			if err := validateFieldValue(f, f.Default); err != nil {
				return err
			}
		}
	}
	for i, f := range d.Fields {
		for _, condition := range f.When {
			parent, ok := seen[condition.Key]
			// Earlier dependencies make evaluation deterministic and exclude cycles.
			if !ok || parent >= i || len(condition.Values) == 0 || d.Fields[parent].Secret || d.Fields[parent].Type == "password" {
				return bad("conditions must refer to earlier non-secret fields")
			}
		}
	}
	if d.Adapter == "snmp_v2c" && publishing {
		for _, f := range d.Fields {
			switch f.Key {
			case "protocol":
				if f.Type != "select" || len(f.Options) != 1 || f.Options[0] != "snmp_v2c" {
					return bad("SNMP protocol field must select the supported version")
				}
			case "port", "timeout":
				if f.Type != "number" {
					return bad("SNMP port and timeout fields must be numbers")
				}
			case "address":
				if f.Type != "ip" {
					return bad("device address must use an IP control")
				}
			case "credential_id":
				if f.Type != "credential" {
					return bad("credential references must use a credential control")
				}
			case "community":
				if f.Type != "password" {
					return bad("community must use an encrypted password control")
				}
			default:
				return bad("field does not bind to an SNMP adapter input")
			}
		}
		if _, ok := seen["port"]; !ok {
			return bad("SNMP form requires port")
		}
		_, credential := seen["credential_id"]
		_, community := seen["community"]
		if !credential && !community {
			return bad("SNMP form requires a community or credential field")
		}
		_, err := parseTemplate(d.Config)
		return err
	}
	if d.Adapter == "mysql" && publishing {
		for _, f := range d.Fields {
			switch f.Key {
			case "username":
				if f.Type != "text" {
					return bad("MySQL username must be a text field")
				}
			case "password":
				if f.Type != "password" {
					return bad("MySQL password must use an encrypted password control")
				}
			case "port":
				if f.Type != "number" {
					return bad("MySQL port must be a number")
				}
			case "connect_timeout":
				if f.Type != "number" {
					return bad("MySQL timeout must be a number")
				}
			case "address":
				if f.Type != "ip" {
					return bad("device address must use an IP control")
				}
			case "tls":
				if f.Type != "select" {
					return bad("MySQL TLS must be a select field")
				}
				for _, option := range f.Options {
					if option != "off" && option != "verify" {
						return bad("unsupported MySQL TLS mode")
					}
				}
			default:
				return bad("field does not bind to a MySQL adapter input")
			}
		}
		if _, ok := seen["username"]; !ok {
			return bad("MySQL form requires a username")
		}
		if _, ok := seen["password"]; !ok {
			return bad("MySQL form requires a password")
		}
		_, err := mysqlContract(d.Config)
		return err
	}
	return nil
}

func fieldActive(f DeviceField, values map[string]interface{}, active map[string]bool) bool {
	for _, condition := range f.When {
		if !active[condition.Key] {
			return false
		}
		match := false
		for _, v := range condition.Values {
			switch actual := values[condition.Key].(type) {
			case []interface{}:
				for _, a := range actual {
					match = match || fmt.Sprint(a) == v
				}
			case []string:
				for _, a := range actual {
					match = match || a == v
				}
			default:
				match = match || fmt.Sprint(actual) == v
			}
		}
		if !match {
			return false
		}
	}
	return true
}

func validateFieldValue(f DeviceField, value interface{}) error {
	fail := func() error { return bad("invalid device parameter: " + f.Key) }
	text, isText := value.(string)
	if isText && (len(text) > 8192 || strings.ContainsRune(text, 0)) {
		return fail()
	}
	switch f.Type {
	case "number":
		n, ok := value.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || f.Min != nil && n < *f.Min || f.Max != nil && n > *f.Max {
			return fail()
		}
	case "switch":
		if _, ok := value.(bool); !ok {
			return fail()
		}
	case "select":
		if !isText {
			return fail()
		}
		valid := false
		for _, option := range f.Options {
			valid = valid || option == text
		}
		if !valid {
			return fail()
		}
	case "multiselect", "endpoints":
		var list []interface{}
		switch v := value.(type) {
		case []interface{}:
			list = v
		case []string:
			for _, s := range v {
				list = append(list, s)
			}
		default:
			return fail()
		}
		if len(list) > 32 {
			return fail()
		}
		for _, item := range list {
			itemText, ok := item.(string)
			if !ok || len(itemText) > 2048 {
				return fail()
			}
			if f.Type == "multiselect" {
				valid := false
				for _, option := range f.Options {
					valid = valid || option == itemText
				}
				if !valid {
					return fail()
				}
			}
		}
	case "ip":
		if _, err := netip.ParseAddr(text); !isText || err != nil {
			return fail()
		}
	case "url":
		u, err := url.Parse(text)
		if !isText || err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return fail()
		}
	default:
		if !isText {
			return fail()
		}
	}
	return nil
}

// Hidden values are dropped before validation and before credentials are sealed.
func validateParameters(fields []DeviceField, input map[string]interface{}) (map[string]interface{}, error) {
	values := map[string]interface{}{}
	active := map[string]bool{}
	for _, f := range fields {
		if !fieldActive(f, values, active) {
			continue
		}
		active[f.Key] = true
		value, ok := input[f.Key]
		if !ok {
			value = f.Default
		}
		if value == nil || value == "" {
			if f.Required {
				return nil, bad("missing device parameter: " + f.Key)
			}
			continue
		}
		if err := validateFieldValue(f, value); err != nil {
			return nil, err
		}
		values[f.Key] = value
	}
	return values, nil
}

func validatePack(p *ProfileDefinition) error {
	if p == nil || len(p.Metrics) == 0 || len(p.Dashboards) == 0 || len(p.Alerts) == 0 || len(p.Dashboards) > 10 || len(p.Alerts) > 64 || len(p.Metrics) > 512 {
		return bad("template requires metrics, dashboards and alert rules within size limits")
	}
	for _, metric := range p.Metrics {
		if strings.TrimSpace(metric.Name) == "" || strings.TrimSpace(metric.Expression) == "" || len(metric.Name) > 191 || len(metric.Expression) > 4096 || len(metric.Note) > 4096 {
			return bad("invalid metric documentation")
		}
	}
	for _, items := range [][]models.BuiltinPayload{p.Dashboards, p.Alerts} {
		seen := map[int64]bool{}
		for _, item := range items {
			if item.ID != 0 {
				if seen[item.ID] {
					return bad("duplicate template resource identity")
				}
				seen[item.ID] = true
			}
			if strings.TrimSpace(item.Name) == "" || len([]rune(item.Name)) > 191 {
				return bad("invalid template resource name")
			}
		}
		next := int64(-1)
		for i := range items {
			if items[i].ID == 0 {
				for seen[next] {
					next--
				}
				items[i].ID = next
				seen[next] = true
				next--
			}
		}
	}
	return nil
}

func documentFromDraft(d CatalogDefinition, id uint64) (templateDocument, error) {
	if err := validatePack(d.Pack); err != nil {
		return templateDocument{}, err
	}
	if d.Adapter != "snmp_v2c" && d.Adapter != "mysql" {
		return templateDocument{}, bad("this adapter has no verified execution compiler; the complete package can be saved as a draft")
	}
	doc := templateDocument{Profile: d.Pack}
	doc.Profile.ComponentID = id
	var err error
	if d.Adapter == "snmp_v2c" {
		doc.TemplateConfig, err = parseTemplate(d.Config)
	} else {
		doc.TemplateConfig, err = mysqlContract(d.Config)
	}
	return doc, err
}

func documentConfig(doc templateDocument) string {
	if doc.ExecutionConfig != "" {
		return doc.ExecutionConfig
	}
	if doc.Catalog != nil {
		return doc.Catalog.Definition.Config
	}
	return ""
}
func customizeConfig(doc *templateDocument, text string) error {
	var config TemplateConfig
	var err error
	if doc.Catalog != nil && doc.Catalog.Definition.Adapter == "mysql" {
		config, err = mysqlContract(text)
		doc.ExecutionConfig = text
	} else {
		config, err = parseTemplate(text)
	}
	if err == nil {
		doc.TemplateConfig = config
	}
	return err
}

func (s *Store) completeCatalogDraft(id uint64) error {
	return s.write(func(tx *gorm.DB) error {
		var row models.MonitorCatalog
		if err := tx.First(&row, "component_id = ?", id).Error; err != nil {
			return err
		}
		var definition CatalogDefinition
		if err := json.Unmarshal([]byte(row.Draft), &definition); err != nil {
			return err
		}
		if definition.Pack != nil {
			return nil
		}
		doc, err := readCatalogResources(tx, id)
		if err != nil {
			return err
		}
		definition.Pack = doc.Profile
		return tx.Model(&row).Updates(map[string]interface{}{"draft": models.MonitorText(canonical(definition)), "record_version": row.RecordVersion + 1}).Error
	})
}
