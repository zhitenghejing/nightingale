package collection

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
	"github.com/prometheus/prometheus/promql/parser"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed catalog_seed.json
var catalogSeed []byte

// DeviceField is declarative. Bindings are restricted to typed compiler inputs;
// a catalog author cannot inject Javascript, arbitrary TOML or secret defaults.
type DeviceField struct {
	Key      string           `json:"key"`
	Label    string           `json:"label"`
	Type     string           `json:"type"`
	Required bool             `json:"required"`
	Default  interface{}      `json:"default,omitempty"`
	Options  []string         `json:"options,omitempty"`
	Help     string           `json:"help,omitempty"`
	Secret   bool             `json:"secret,omitempty"`
	Group    string           `json:"group,omitempty"`
	Min      *float64         `json:"min,omitempty"`
	Max      *float64         `json:"max,omitempty"`
	When     []FieldCondition `json:"when,omitempty"`
}
type CatalogDefinition struct {
	Reachability  string              `json:"reachability"`
	Adapter       string              `json:"adapter"`
	Config        string              `json:"config"`
	Fields        []DeviceField       `json:"fields"`
	Notification  string              `json:"notification"`
	Compatibility string              `json:"compatibility,omitempty"`
	Execution     string              `json:"execution,omitempty"`
	Pack          *ProfileDefinition  `json:"pack,omitempty"`
	Notify        *NotificationPreset `json:"notify,omitempty"`
}
type CatalogRelease struct {
	Version      int               `json:"version"`
	Taxonomy     string            `json:"taxonomy,omitempty"`
	CategoryPath []string          `json:"category_path"`
	Definition   CatalogDefinition `json:"definition"`
}
type CatalogEntry struct {
	models.MonitorCatalog
	Logo             string                 `json:"logo"`
	Ident            string                 `json:"ident"`
	Draft            CatalogDefinition      `json:"draft"`
	Published        *CatalogRelease        `json:"published"`
	PublishedMetrics []models.BuiltinMetric `json:"published_metrics"`
	Onboarding       CatalogOnboarding      `json:"onboarding"`
}
type CatalogView struct {
	Categories []models.MonitorCategory `json:"categories"`
	Entries    []CatalogEntry           `json:"entries"`
}
type CatalogMetadata struct {
	Name          string `json:"name"`
	Logo          string `json:"logo"`
	CategoryID    string `json:"category_id"`
	Disabled      bool   `json:"disabled"`
	RecordVersion int64  `json:"record_version"`
}
type CatalogDraftInput struct {
	Definition    CatalogDefinition `json:"definition"`
	RecordVersion int64             `json:"record_version"`
}

func defaultDeviceFields() []DeviceField {
	return []DeviceField{
		{Key: "protocol", Label: "SNMP 版本", Type: "select", Required: true, Default: "snmp_v2c", Options: []string{"snmp_v2c"}},
		{Key: "port", Label: "SNMP 端口", Type: "number", Required: true, Default: 161},
		{Key: "credential_id", Label: "SNMP 团体字凭据", Type: "credential", Required: true, Help: "凭据加密保存，可从下方创建；一个采集器可采集多台设备。"},
	}
}
func seedCategory(tx *gorm.DB, path []string) (string, error) {
	parent := ""
	for _, name := range path {
		name = catalogLabel(name)
		var existing models.MonitorCategory
		if err := tx.Where("parent_id = ? AND name = ?", parent, name).First(&existing).Error; err == nil {
			parent = existing.ID
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", err
		}
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("nightingale-template-category:"+parent+":"+name)).String()
		// A user may have renamed/moved the old deterministic seed ID.
		if err := tx.First(&existing, "id = ?", id).Error; err == nil {
			id = uuid.NewString()
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", err
		}
		c := models.MonitorCategory{ID: id, ParentID: parent, Name: name, RecordVersion: 1}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&c).Error; err != nil {
			return "", err
		}
		parent = id
	}
	return parent, nil
}

// EnsureCatalog imports only missing entries. Source refreshes never overwrite
// user metadata, drafts, category moves or immutable release selections.
func (s *Store) EnsureCatalog() error {
	return s.write(func(tx *gorm.DB) error {
		if err := s.migrateCatalogTaxonomy(tx); err != nil {
			return err
		}
		var components []models.BuiltinComponent
		if err := tx.Where("id NOT IN (?)", tx.Model(&models.MonitorCatalog{}).Select("component_id")).Find(&components).Error; err != nil {
			return err
		}
		var paths map[string][]string
		if err := json.Unmarshal(catalogSeed, &paths); err != nil {
			return err
		}
		for _, c := range components {
			path := paths[c.Ident]
			if len(path) != 3 {
				path = []string{"待归类", "未知品牌", catalogLabel(c.Ident)}
			}
			category, err := seedCategory(tx, path)
			if err != nil {
				return err
			}
			d := CatalogDefinition{Adapter: "documentation", Fields: []DeviceField{}, Notification: "events_only"}
			name := catalogLabel(c.Ident)
			var doc templateDocument
			if strings.Contains(c.Readme, profileFence) {
				doc, err = readProfile(tx, c.ID)
				if err == nil {
					b, _ := toml.Marshal(doc.TemplateConfig)
					d = CatalogDefinition{Adapter: "snmp_v2c", Config: string(b), Fields: defaultDeviceFields(), Notification: "events_only"}
				}
			}
			if doc.Profile == nil {
				doc, err = readCatalogResources(tx, c.ID)
				if err != nil {
					return err
				}
			}
			d.Pack = doc.Profile
			if imported, ok := definitionFromReadme(c.Readme); ok {
				imported.Pack = doc.Profile
				if err := validateCatalogDefinition(imported, true); err == nil {
					if compiled, err := documentFromDraft(imported, c.ID); err == nil {
						d = imported
						doc = compiled
						name = catalogLabel(strings.TrimPrefix(strings.SplitN(c.Readme, "\n", 2)[0], "# "))
					}
				}
			}
			row := models.MonitorCatalog{ComponentID: c.ID, Name: name, Logo: models.MonitorText(c.Logo), CategoryID: category, Disabled: c.Disabled != 0, Draft: models.MonitorText(canonical(d)), RecordVersion: 1}
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
			if created.Error != nil {
				return created.Error
			}
			if created.RowsAffected == 0 {
				continue
			}
			if d.Adapter == "snmp_v2c" || d.Adapter == "mysql" {
				if err := validatePublishedResources(doc); err != nil {
					continue
				}
				doc.Catalog = &CatalogRelease{Version: 1, Taxonomy: TypeBrandModelTaxonomy, CategoryPath: path, Definition: d}
				version := catalogVersion(row, doc, 1)
				if len(version.Definition) > 2<<20 {
					continue
				}
				if err = tx.Create(&version).Error; err != nil {
					return err
				}
				if err = tx.Model(&row).Update("published_version_id", version.ID).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
func catalogVersion(row models.MonitorCatalog, doc templateDocument, version int) models.MonitorTemplateVersion {
	if doc.Profile != nil {
		doc.Profile.Name = row.Name
	}
	b := canonical(doc)
	return models.MonitorTemplateVersion{ID: uuid.NewString(), TemplateKey: fmt.Sprintf("catalog:%d", row.ComponentID), Version: version, Name: row.Name, Definition: models.MonitorText(b), Digest: digest(b)}
}
func (s *Store) Catalog() (CatalogView, error) {
	out := CatalogView{Categories: []models.MonitorCategory{}, Entries: []CatalogEntry{}}
	if err := s.EnsureCatalog(); err != nil {
		return out, err
	}
	if err := s.DB.Order("sort, name, id").Find(&out.Categories).Error; err != nil {
		return out, err
	}
	var rows []models.MonitorCatalog
	if err := s.DB.Where("deleted = ?", false).Order("name, component_id").Find(&rows).Error; err != nil {
		return out, err
	}
	var components []models.BuiltinComponent
	if err := s.DB.Find(&components).Error; err != nil {
		return out, err
	}
	byID := map[uint64]models.BuiltinComponent{}
	for _, component := range components {
		byID[component.ID] = component
	}
	for _, row := range rows {
		c, ok := byID[row.ComponentID]
		if !ok {
			continue
		}
		e := CatalogEntry{MonitorCatalog: row, Logo: string(row.Logo), Ident: c.Ident}
		if err := json.Unmarshal([]byte(row.Draft), &e.Draft); err != nil {
			return out, err
		}
		// Upgrade the old partial draft once. Subsequent reads never pull mutable
		// resource rows into an existing draft or release.
		if e.Draft.Pack == nil {
			if err := s.completeCatalogDraft(row.ComponentID); err != nil {
				return out, err
			}
			if err := s.DB.First(&row, "component_id = ?", row.ComponentID).Error; err != nil {
				return out, err
			}
			e.MonitorCatalog = row
			if err := json.Unmarshal([]byte(row.Draft), &e.Draft); err != nil {
				return out, err
			}
		}
		if row.PublishedVersionID != "" {
			doc, err := loadTemplate(s.DB, row.PublishedVersionID)
			if err != nil {
				return out, err
			}
			e.Published = doc.Catalog
			if doc.Profile != nil {
				e.PublishedMetrics = doc.Profile.Metrics
				if e.Published != nil && e.Published.Definition.Pack == nil {
					e.Published.Definition.Pack = doc.Profile
				}
			}
		}
		if err := s.initializeOnboarding(&e); err != nil {
			return out, err
		}
		e.Draft.Reachability = DefaultReachability
		if e.Published != nil {
			e.Published.Definition.Reachability = DefaultReachability
		}
		e.Onboarding = catalogOnboarding(e, out.Categories)
		out.Entries = append(out.Entries, e)
	}
	return out, nil
}

// The browser tree needs metadata and the published form schema. Large native
// dashboards and instructions are fetched only when a package is opened.
func (s *Store) CatalogSummary() (CatalogView, error) {
	view, err := s.Catalog()
	if err != nil {
		return view, err
	}
	for i := range view.Entries {
		view.Entries[i].Draft.Pack = nil
		view.Entries[i].PublishedMetrics = nil
		if view.Entries[i].Published != nil {
			view.Entries[i].Published.Definition.Pack = nil
		}
	}
	return view, nil
}
func (s *Store) CatalogEntry(id uint64) (CatalogEntry, error) {
	view, err := s.Catalog()
	if err != nil {
		return CatalogEntry{}, err
	}
	for _, entry := range view.Entries {
		if entry.ComponentID == id {
			return entry, nil
		}
	}
	return CatalogEntry{}, missing()
}
func lockCatalog(tx *gorm.DB, id uint64, version int64) (models.MonitorCatalog, error) {
	var row models.MonitorCatalog
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "component_id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return row, missing()
		}
		return row, err
	}
	if row.Deleted {
		return row, missing()
	}
	if row.RecordVersion != version {
		return row, conflict("template changed; reload before saving")
	}
	return row, nil
}
func categoryPath(tx *gorm.DB, id string) ([]string, error) {
	path := []string{}
	seen := map[string]bool{}
	for id != "" {
		if seen[id] || len(path) >= 3 {
			return nil, bad("category must have exactly three levels")
		}
		seen[id] = true
		var c models.MonitorCategory
		if err := tx.First(&c, "id = ?", id).Error; err != nil {
			return nil, bad("category is unavailable")
		}
		path = append([]string{c.Name}, path...)
		id = c.ParentID
	}
	if len(path) != 3 {
		return nil, bad("select a third-level device category")
	}
	return path, nil
}
func validLogo(value string) bool {
	if len(value) > 3<<20 {
		return false
	}
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "data:image/") {
		for _, prefix := range []string{"data:image/png;base64,", "data:image/jpeg;base64,", "data:image/svg+xml;base64,", "data:image/webp;base64,"} {
			if strings.HasPrefix(value, prefix) {
				decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, prefix))
				return err == nil && len(decoded) > 0 && len(decoded) <= 2<<20
			}
		}
		return false
	}
	u, err := url.Parse(value)
	return err == nil && ((u.Scheme == "http" || u.Scheme == "https") && u.Host != "" || u.Scheme == "" && strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//"))
}
func (s *Store) SaveCatalogMetadata(scope Scope, id uint64, f CatalogMetadata) error {
	if !scope.Admin {
		return denied()
	}
	f.Name = strings.TrimSpace(f.Name)
	if !utf8.ValidString(f.Name) || len([]rune(f.Name)) < 1 || len([]rune(f.Name)) > 128 || !validLogo(f.Logo) {
		return bad("invalid template name or icon")
	}
	return s.write(func(tx *gorm.DB) error {
		row, err := lockCatalog(tx, id, f.RecordVersion)
		if err != nil {
			return err
		}
		if _, err = categoryPath(tx, f.CategoryID); err != nil {
			return err
		}
		row.Name = f.Name
		row.Logo = models.MonitorText(f.Logo)
		row.CategoryID = f.CategoryID
		row.Disabled = f.Disabled
		row.RecordVersion++
		if err = tx.Save(&row).Error; err != nil {
			return err
		}
		return s.audit(tx, scope, 0, fmt.Sprint(id), "catalog_metadata_saved", "", nil)
	})
}
func validateCatalogDefinition(d CatalogDefinition, publishing bool) error {
	return validateDefinition(d, publishing)
}
func (s *Store) SaveCatalogDraft(scope Scope, id uint64, f CatalogDraftInput, publish bool) error {
	if !scope.Admin {
		return denied()
	}
	// Normalize numeric defaults even for callers using Go values.
	var normalized CatalogDefinition
	if err := json.Unmarshal(canonical(f.Definition), &normalized); err != nil {
		return bad("invalid device definition")
	}
	f.Definition = normalized
	if err := validateCatalogDefinition(f.Definition, publish); err != nil {
		return err
	}
	return s.write(func(tx *gorm.DB) error {
		row, err := lockCatalog(tx, id, f.RecordVersion)
		if err != nil {
			return err
		}
		if f.Definition.Pack == nil {
			// Compatibility for pre-bundle clients: retain the saved draft, never
			// resnapshot the mutable native library at publication time.
			var previous CatalogDefinition
			if err := json.Unmarshal([]byte(row.Draft), &previous); err != nil {
				return err
			}
			f.Definition.Pack = previous.Pack
			if f.Definition.Pack == nil {
				return bad("reload the complete template draft before publishing")
			}
		}
		row.Draft = models.MonitorText(canonical(f.Definition))
		row.RecordVersion++
		if publish {
			if err := categoryEnabled(tx, row.CategoryID); err != nil {
				return err
			}
			path, err := categoryPath(tx, row.CategoryID)
			if err != nil {
				return err
			}
			if row.Disabled {
				return bad("enable the template before publishing")
			}
			doc, err := documentFromDraft(f.Definition, id)
			if err != nil {
				return err
			}
			next := 1
			if row.PublishedVersionID != "" {
				var prior models.MonitorTemplateVersion
				if err = tx.First(&prior, "id = ?", row.PublishedVersionID).Error; err != nil {
					return err
				}
				next = prior.Version + 1
			}
			doc.Catalog = &CatalogRelease{Version: next, Taxonomy: TypeBrandModelTaxonomy, CategoryPath: path, Definition: f.Definition}
			if err = validatePublishedResources(doc); err != nil {
				return err
			}
			version := catalogVersion(row, doc, next)
			if len(version.Definition) > 2<<20 {
				return bad("template snapshot is too large")
			}
			if err = tx.Create(&version).Error; err != nil {
				return err
			}
			row.PublishedVersionID = version.ID
		}
		if err = tx.Save(&row).Error; err != nil {
			return err
		}
		return s.audit(tx, scope, 0, fmt.Sprint(id), "catalog_draft_saved", row.PublishedVersionID, map[string]bool{"published": publish})
	})
}
func validatePublishedResources(doc templateDocument) error {
	if err := validatePack(doc.Profile); err != nil {
		return err
	}
	d := models.MonitorDevice{ID: "validation-device", CollectorID: "validation-collector", DesiredRevision: "validation-revision"}
	available := map[string]bool{"snmp_up": true, "snmp_health_state": true}
	for _, name := range doc.MetricNames() {
		available[name] = true
	}
	check := func(expr string) error {
		expr = strings.ReplaceAll(strings.ReplaceAll(expr, "$__rate_interval", "5m"), "$__interval", "1m")
		if strings.TrimSpace(expr) == "" {
			return nil
		}
		tree, err := parser.ParseExpr(expr)
		if err != nil {
			return bad("invalid PromQL in device template or customization")
		}
		var missingMetric bool
		parser.Inspect(tree, func(node parser.Node, _ []parser.Node) error {
			if vector, ok := node.(*parser.VectorSelector); ok && vector.Name != "" && !available[vector.Name] {
				missingMetric = true
			}
			return nil
		})
		if missingMetric {
			return bad("dashboard or alert references a metric not produced by this template")
		}
		return nil
	}
	for _, m := range doc.Profile.Metrics {
		if _, err := scopeExpression(m.Expression, d); err != nil {
			return err
		}
		if err := check(m.Expression); err != nil {
			return err
		}
	}
	for _, resources := range [][]models.BuiltinPayload{doc.Profile.Dashboards, doc.Profile.Alerts} {
		for _, r := range resources {
			if _, err := scopeJSON(r.Content, d, 1); err != nil {
				return err
			}
			var value interface{}
			if err := json.Unmarshal([]byte(r.Content), &value); err != nil {
				return bad("invalid template JSON")
			}
			var walk func(interface{}) error
			walk = func(v interface{}) error {
				switch o := v.(type) {
				case map[string]interface{}:
					for k, child := range o {
						if k == "expr" || k == "prom_ql" {
							if text, ok := child.(string); ok {
								if err := check(text); err != nil {
									return err
								}
							}
						}
						if err := walk(child); err != nil {
							return err
						}
					}
				case []interface{}:
					for _, child := range o {
						if err := walk(child); err != nil {
							return err
						}
					}
				}
				return nil
			}
			if err := walk(value); err != nil {
				return err
			}
		}
	}
	for _, resource := range doc.Profile.Dashboards {
		var board struct {
			Configs map[string]interface{} `json:"configs"`
		}
		if json.Unmarshal([]byte(resource.Content), &board) != nil || board.Configs == nil {
			return bad("dashboard requires native configs")
		}
		if _, ok := board.Configs["panels"].([]interface{}); !ok {
			return bad("dashboard requires native panels")
		}
	}
	for _, resource := range doc.Profile.Alerts {
		var rule models.AlertRule
		if json.Unmarshal([]byte(resource.Content), &rule) != nil {
			return bad("invalid native alert rule")
		}
		if rule.Cate != "" && rule.Cate != "prometheus" {
			return bad("this collection adapter requires Prometheus alert rules")
		}
		if err := rule.FE2DB(); err != nil {
			return bad("invalid native alert rule: " + err.Error())
		}
		if err := rule.Verify(); err != nil {
			return bad("invalid native alert rule: " + err.Error())
		}
	}
	return nil
}
func (s *Store) SaveCategory(scope Scope, id string, f models.MonitorCategory) error {
	if !scope.Admin {
		return denied()
	}
	f.Name = strings.TrimSpace(f.Name)
	if f.Name == "" || len([]rune(f.Name)) > 128 || !utf8.ValidString(f.Name) {
		return bad("invalid category name")
	}
	return s.write(func(tx *gorm.DB) error {
		// Lock the root-level category set before checking duplicate siblings and
		// parent changes; the process mutex additionally covers SQLite.
		var all []models.MonitorCategory
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Find(&all).Error; err != nil {
			return err
		}
		if id == "" {
			id = uuid.NewString()
			f.RecordVersion = 0
		} else {
			found := false
			for _, c := range all {
				if c.ID == id {
					found = true
					if c.RecordVersion != f.RecordVersion {
						return conflict("category changed; reload before saving")
					}
				}
			}
			if !found {
				return missing()
			}
		}
		f.ID = id
		byID := map[string]models.MonitorCategory{}
		for _, c := range all {
			if c.ID != id && c.ParentID == f.ParentID && c.Name == f.Name {
				return conflict("a sibling category already has this name")
			}
			byID[c.ID] = c
		}
		byID[id] = f
		for key := range byID {
			seen := map[string]bool{}
			node := key
			depth := 0
			for node != "" {
				if seen[node] || depth >= 3 {
					return bad("categories cannot contain a cycle or more than three levels")
				}
				seen[node] = true
				c, ok := byID[node]
				if !ok {
					return bad("parent category is unavailable")
				}
				node = c.ParentID
				depth++
			}
		}
		// Existing templates must remain at leaf depth three after a move.
		var entries []models.MonitorCatalog
		if err := tx.Where("deleted = ?", false).Find(&entries).Error; err != nil {
			return err
		}
		for _, e := range entries {
			depth := 0
			for node := e.CategoryID; node != ""; {
				c, ok := byID[node]
				if !ok {
					return bad("category is unavailable")
				}
				depth++
				node = c.ParentID
			}
			if depth != 3 {
				return bad("move templates before changing this category depth")
			}
		}
		f.RecordVersion++
		return tx.Save(&f).Error
	})
}
func (s *Store) DeleteCategory(scope Scope, id string, version int64) error {
	if !scope.Admin {
		return denied()
	}
	return s.write(func(tx *gorm.DB) error {
		var c models.MonitorCategory
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&c, "id = ?", id).Error; err != nil {
			return missing()
		}
		if c.RecordVersion != version {
			return conflict("category changed; reload before deleting")
		}
		var n int64
		if err := tx.Model(&models.MonitorCategory{}).Where("parent_id = ?", id).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return conflict("move or delete child categories first")
		}
		if err := tx.Model(&models.MonitorCatalog{}).Where("category_id = ? AND deleted = ?", id, false).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return conflict("move templates out of this category first")
		}
		return tx.Delete(&c).Error
	})
}

func (s *Store) DeleteCatalog(scope Scope, id uint64, version int64) error {
	if !scope.Admin {
		return denied()
	}
	return s.write(func(tx *gorm.DB) error {
		row, err := lockCatalog(tx, id, version)
		if err != nil {
			return err
		}
		var versions []models.MonitorTemplateVersion
		if err = tx.Joins("JOIN monitor_device ON monitor_device.template_version_id = monitor_template_version.id").Find(&versions).Error; err != nil {
			return err
		}
		for _, v := range versions {
			doc, err := templateDocumentFrom(v)
			if err != nil {
				return err
			}
			if doc.Profile != nil && doc.Profile.ComponentID == id {
				return conflict("template is referenced by a device; disable it instead")
			}
		}
		// Retain native resources and releases for historical links. The tombstone
		// also prevents startup seeding from resurrecting a deleted builtin card.
		row.Deleted = true
		row.Disabled = true
		row.RecordVersion++
		if err = tx.Save(&row).Error; err != nil {
			return err
		}
		return s.audit(tx, scope, 0, fmt.Sprint(id), "catalog_deleted", "", nil)
	})
}

func catalogLabel(value string) string {
	chars := []rune(value)
	if len(chars) > 128 {
		return string(chars[:119]) + "…" + digest([]byte(value))[:8]
	}
	return value
}
