package collection

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TemplateConfig is deliberately smaller than a Categraf instance: endpoints,
// credentials, ownership labels and execution settings are always server-owned.
type TemplateConfig struct {
	DisabledTables  []string    `json:"disabled_tables,omitempty" toml:"disabled_tables,omitempty"`
	ProducedMetrics []string    `json:"produced_metrics,omitempty" toml:"produced_metrics,omitempty"`
	RequiredMetrics []string    `json:"required_metrics" toml:"required_metrics"`
	Fields          []snmpField `json:"fields" toml:"field,omitempty"`
	Tables          []snmpTable `json:"tables" toml:"table,omitempty"`
}
type templateDocument struct {
	Catalog *CatalogRelease    `json:"catalog,omitempty"`
	Profile *ProfileDefinition `json:"profile,omitempty"`
	TemplateConfig
	Component       string `json:"component"`
	Description     string `json:"description"`
	ExecutionConfig string `json:"execution_config,omitempty"`
}
type TemplateInput struct {
	Name          string `json:"name"`
	Component     string `json:"component"`
	Description   string `json:"description"`
	Config        string `json:"config"`
	BaseVersionID string `json:"base_version_id"`
}
type TemplateView struct {
	ID          string `json:"id"`
	TemplateKey string `json:"template_key"`
	Version     int    `json:"version"`
	Name        string `json:"name"`
	Component   string `json:"component"`
	Description string `json:"description"`
	Config      string `json:"config"`
}

var numericOID = regexp.MustCompile(`^\.?[0-2](\.[0-9]+){2,127}$`)
var snmpName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

func validNumericOID(oid string) bool {
	if !numericOID.MatchString(oid) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(oid, "."), ".")
	for i, part := range parts {
		n, err := strconv.ParseUint(part, 10, 32)
		if err != nil || (i == 1 && parts[0] != "2" && n > 39) {
			return false
		}
	}
	return true
}

func templateDocumentFrom(t models.MonitorTemplateVersion) (templateDocument, error) {
	var d templateDocument
	if digest([]byte(t.Definition)) != t.Digest || json.Unmarshal([]byte(t.Definition), &d) != nil {
		return d, errors.New("collection template integrity failure")
	}
	if t.ID == WindowsTemplate {
		d.Component = "Windows"
	}
	return d, nil
}
func templateView(t models.MonitorTemplateVersion) (TemplateView, error) {
	d, err := templateDocumentFrom(t)
	if err != nil {
		return TemplateView{}, err
	}
	b, err := toml.Marshal(d.TemplateConfig)
	return TemplateView{t.ID, t.TemplateKey, t.Version, t.Name, d.Component, d.Description, string(b)}, err
}
func (s *Store) Templates() ([]TemplateView, error) {
	rows := []models.MonitorTemplateVersion{}
	if err := s.DB.Order("template_key, version DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := []TemplateView{}
	for _, row := range rows {
		v, err := templateView(row)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func loadTemplate(tx *gorm.DB, id string) (templateDocument, error) {
	var t models.MonitorTemplateVersion
	if err := tx.First(&t, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return templateDocument{}, bad("unknown immutable template version")
		}
		return templateDocument{}, err
	}
	return templateDocumentFrom(t)
}
func (s *Store) TemplateConfig(id string) (TemplateConfig, error) {
	d, err := loadTemplate(s.DB, id)
	return d.TemplateConfig, err
}
func (c TemplateConfig) MetricNames() []string {
	if len(c.ProducedMetrics) != 0 {
		return c.ProducedMetrics
	}
	names := []string{"snmp_uptime"}
	for _, f := range c.Fields {
		if !f.IsTag {
			names = append(names, "snmp_"+f.Name)
		}
	}
	for _, t := range c.Tables {
		for _, f := range t.Fields {
			if !f.IsTag {
				names = append(names, "snmp_"+t.Name+"_"+f.Name)
			}
		}
	}
	return sortedUnique(names)
}
func parseTemplate(config string) (TemplateConfig, error) {
	var c TemplateConfig
	if len(config) > 65536 || toml.NewDecoder(strings.NewReader(config)).DisallowUnknownFields().Decode(&c) != nil {
		return c, bad("invalid SNMP template TOML; only field, table, disabled_tables and required_metrics are allowed")
	}
	if len(c.ProducedMetrics) != 0 {
		return c, bad("SNMP metric names must be derived from OIDs")
	}
	if len(c.Fields)+len(c.Tables) == 0 || len(c.Fields) > 64 || len(c.Tables) > 32 {
		return c, bad("template needs fields or tables within the size limit")
	}
	names := map[string]bool{"uptime": true, "up": true, "health_state": true}
	checkFields := func(fields []snmpField, table string) error {
		seen := map[string]bool{}
		for _, f := range fields {
			name := f.Name
			if table != "" {
				name = table + "_" + name
			}
			if !snmpName.MatchString(f.Name) || !validNumericOID(f.OID) || seen[f.Name] || (!f.IsTag && names[name]) || (f.Conversion != "" && (f.Conversion != "hwaddr" || !f.IsTag)) {
				return bad("invalid or duplicate SNMP field name or numeric OID")
			}
			if f.IsTag && reservedLabel(f.Name) && f.Name != "type" {
				return bad("invalid or reserved label")
			}
			// Scalar tags become labels on every metric and must not shadow core metrics.
			if table == "" && (f.IsTag || reservedLabel(f.Name)) {
				return bad("scalar fields must be numeric metrics")
			}
			seen[f.Name] = true
			if !f.IsTag {
				names[name] = true
			}
		}
		return nil
	}
	if err := checkFields(c.Fields, ""); err != nil {
		return c, err
	}
	tables := map[string]bool{}
	for i := range c.Tables {
		t := &c.Tables[i]
		if !snmpName.MatchString(t.Name) || tables[t.Name] || len(t.Fields) == 0 || len(t.Fields) > 64 {
			return c, bad("invalid or duplicate SNMP table")
		}
		tables[t.Name] = true
		t.IndexAsTag = true
		if t.ErrorPolicy == "" {
			t.ErrorPolicy = "partial"
		}
		if t.ErrorPolicy != "partial" {
			return c, bad("SNMP tables require partial error policy")
		}
		if err := checkFields(t.Fields, t.Name); err != nil {
			return c, err
		}
	}
	c.RequiredMetrics = sortedUnique(append(c.RequiredMetrics, "snmp_uptime"))
	disabled := map[string]bool{}
	for _, name := range c.DisabledTables {
		if !tables[name] || disabled[name] {
			return c, bad("invalid disabled SNMP table")
		}
		disabled[name] = true
	}
	for _, table := range c.Tables {
		if disabled[table.Name] {
			for _, field := range table.Fields {
				for _, required := range c.RequiredMetrics {
					if !field.IsTag && required == "snmp_"+table.Name+"_"+field.Name {
						return c, bad("required SNMP metrics cannot be disabled")
					}
				}
			}
		}
	}
	available := map[string]bool{}
	for _, name := range c.MetricNames() {
		available[name] = true
	}
	for _, m := range c.RequiredMetrics {
		if !available[m] {
			return c, bad("required metric is not produced by this template")
		}
	}
	return c, nil
}

// SaveTemplate publishes a new immutable version. Existing devices and provider
// snapshots keep their exact version until explicitly updated by their owner.
func (s *Store) SaveTemplate(scope Scope, f TemplateInput) (TemplateView, error) {
	if !scope.Admin {
		return TemplateView{}, denied()
	}
	f.Name = strings.TrimSpace(f.Name)
	if !utf8.ValidString(f.Name) || len([]rune(f.Name)) < 1 || len([]rune(f.Name)) > 128 || len(f.Description) > 4096 || !utf8.ValidString(f.Description) {
		return TemplateView{}, bad("invalid template name or description")
	}
	cfg, err := parseTemplate(f.Config)
	if err != nil {
		return TemplateView{}, err
	}
	var out models.MonitorTemplateVersion
	err = s.write(func(tx *gorm.DB) error {
		var component models.BuiltinComponent
		if err := tx.Where("ident = ? AND disabled = ?", f.Component, 0).First(&component).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return bad("template component is unavailable")
			}
			return err
		}
		out = models.MonitorTemplateVersion{ID: uuid.NewString(), TemplateKey: uuid.NewString(), Version: 1, Name: f.Name}
		if f.BaseVersionID != "" {
			var base, latest models.MonitorTemplateVersion
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&base, "id = ?", f.BaseVersionID).Error; err != nil {
				return missing()
			}
			if err := tx.Where("template_key = ?", base.TemplateKey).Order("version DESC").First(&latest).Error; err != nil {
				return err
			}
			if latest.ID != base.ID {
				return conflict("template has a newer version; reload before editing")
			}
			old, err := templateDocumentFrom(base)
			if err != nil {
				return err
			}
			if old.Component != f.Component {
				return bad("template component is immutable")
			}
			out.TemplateKey = base.TemplateKey
			out.Version = base.Version + 1
		}
		b := canonical(templateDocument{TemplateConfig: cfg, Component: f.Component, Description: f.Description})
		out.Definition = models.MonitorText(b)
		out.Digest = digest(b)
		if err := tx.Create(&out).Error; err != nil {
			return err
		}
		return s.audit(tx, scope, 0, out.ID, "template_published", "", map[string]interface{}{"template_key": out.TemplateKey, "version": out.Version})
	})
	if err != nil {
		return TemplateView{}, err
	}
	return templateView(out)
}
