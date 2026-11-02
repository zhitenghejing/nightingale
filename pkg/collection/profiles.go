package collection

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/pelletier/go-toml/v2"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const profileFence = "```snmp-collection\n"

// ProfileDefinition snapshots the native component, not a second user catalog.
// Native dashboard/rule instances are independent and stay editable afterwards.
type ProfileDefinition struct {
	ComponentID uint64                  `json:"component_id"`
	Name        string                  `json:"name"`
	Readme      string                  `json:"readme,omitempty"`
	Metrics     []models.BuiltinMetric  `json:"metrics"`
	Dashboards  []models.BuiltinPayload `json:"dashboards"`
	Alerts      []models.BuiltinPayload `json:"alerts"`
	Customized  bool                    `json:"customized"`
}
type ProfileCustomization struct {
	Config     *string                  `json:"config,omitempty"`
	Metrics    *[]models.BuiltinMetric  `json:"metrics,omitempty"`
	Dashboards *[]models.BuiltinPayload `json:"dashboards,omitempty"`
	Alerts     *[]models.BuiltinPayload `json:"alerts,omitempty"`
}
type ProfileResource struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	SourceID int64  `json:"source_id,omitempty"`
}
type profileState struct {
	Boards []ProfileResource `json:"boards"`
	Rules  []ProfileResource `json:"rules"`
	// Disabled states survive a device pause independently of template defaults.
	Suspended     map[int64]int `json:"suspended,omitempty"`
	Waiting       map[int64]int `json:"waiting,omitempty"`
	ArmedRevision string        `json:"armed_revision,omitempty"`
}
type ProfileView struct {
	SecretFields map[string]bool        `json:"secret_fields"`
	Catalog      *CatalogRelease        `json:"catalog,omitempty"`
	ComponentID  uint64                 `json:"component_id"`
	Name         string                 `json:"name"`
	Readme       string                 `json:"readme,omitempty"`
	Config       string                 `json:"config"`
	Metrics      []models.BuiltinMetric `json:"metrics"`
	Boards       []ProfileResource      `json:"boards"`
	Rules        []ProfileResource      `json:"rules"`
	Customized   bool                   `json:"customized"`
}
type ProfileOption struct {
	ComponentID    uint64                 `json:"component_id"`
	Name           string                 `json:"name"`
	Config         string                 `json:"config"`
	Metrics        []models.BuiltinMetric `json:"metrics"`
	DashboardCount int                    `json:"dashboard_count"`
	AlertCount     int                    `json:"alert_count"`
}

func collectionFromReadme(readme string) (TemplateConfig, error) {
	start := strings.Index(readme, profileFence)
	if start < 0 || strings.Count(readme, profileFence) != 1 {
		return TemplateConfig{}, bad("component needs one snmp-collection TOML block in its instructions")
	}
	text := readme[start+len(profileFence):]
	end := strings.Index(text, "\n```")
	if end < 0 {
		return TemplateConfig{}, bad("unclosed snmp-collection block")
	}
	return parseTemplate(text[:end])
}
func readCatalogResources(tx *gorm.DB, id uint64) (templateDocument, error) {
	var c models.BuiltinComponent
	if err := tx.First(&c, "id = ?", id).Error; err != nil {
		return templateDocument{}, bad("template component is unavailable")
	}
	var err error
	p := &ProfileDefinition{ComponentID: c.ID, Name: c.Ident, Readme: c.Readme, Metrics: []models.BuiltinMetric{}, Dashboards: []models.BuiltinPayload{}, Alerts: []models.BuiltinPayload{}}
	if err = tx.Where("typ = ?", c.Ident).Order("id").Find(&p.Metrics).Error; err != nil {
		return templateDocument{}, err
	}
	if err = tx.Where("component_id = ? AND type = ?", c.ID, "dashboard").Order("id").Find(&p.Dashboards).Error; err != nil {
		return templateDocument{}, err
	}
	if err = tx.Where("component_id = ? AND type = ?", c.ID, "alert").Order("id").Find(&p.Alerts).Error; err != nil {
		return templateDocument{}, err
	}
	return templateDocument{Component: c.Ident, Profile: p}, nil
}
func readProfileResources(tx *gorm.DB, id uint64) (templateDocument, error) {
	doc, err := readCatalogResources(tx, id)
	if err != nil {
		return doc, err
	}
	if err := validatePack(doc.Profile); err != nil {
		return doc, err
	}
	return doc, nil
}
func readProfile(tx *gorm.DB, id uint64) (templateDocument, error) {
	doc, err := readProfileResources(tx, id)
	if err != nil {
		return doc, err
	}
	var c models.BuiltinComponent
	if err = tx.First(&c, "id = ?", id).Error; err != nil {
		return doc, err
	}
	if c.Disabled != 0 {
		return doc, bad("template component is unavailable")
	}
	doc.TemplateConfig, err = collectionFromReadme(c.Readme)
	return doc, err
}
func (s *Store) Profiles() ([]ProfileOption, error) {
	var cs []models.BuiltinComponent
	if err := s.DB.Where("disabled = ?", 0).Order("ident").Find(&cs).Error; err != nil {
		return nil, err
	}
	out := []ProfileOption{}
	for _, c := range cs {
		if !strings.Contains(c.Readme, profileFence) {
			continue
		}
		p, err := readProfile(s.DB, c.ID)
		if err != nil {
			continue
		}
		b, _ := toml.Marshal(p.TemplateConfig)
		out = append(out, ProfileOption{c.ID, c.Ident, string(b), p.Profile.Metrics, len(p.Profile.Dashboards), len(p.Profile.Alerts)})
	}
	return out, nil
}
func (s *Store) DeviceProfile(d models.MonitorDevice) (*ProfileView, error) {
	p, err := loadTemplate(s.DB, d.TemplateVersionID)
	if err != nil {
		return nil, err
	}
	if p.Profile == nil {
		return nil, nil
	}
	state, err := deviceProfileState(d)
	if err != nil {
		return nil, err
	}
	b, err := toml.Marshal(p.TemplateConfig)
	if err != nil {
		return nil, err
	}
	metrics := append([]models.BuiltinMetric{}, p.Profile.Metrics...)
	for i := range metrics {
		// Documentation copied from a device should query that device too.
		// Preserve non-PromQL documentation rather than blocking device reads.
		if expression, scopeErr := scopeExpression(metrics[i].Expression, d); scopeErr == nil {
			metrics[i].Expression = expression
		}
	}
	secretFields := map[string]bool{}
	var credential models.MonitorCredential
	if err := s.DB.First(&credential, "id = ?", d.CredentialID).Error; err != nil {
		return nil, err
	}
	if strings.HasPrefix(credential.Kind, "parameters:") {
		secret, err := s.Keys.Open(string(credential.Ciphertext), credential.KeyID, "credential:"+credential.ID)
		if err != nil {
			return nil, err
		}
		var values map[string]interface{}
		if err := json.Unmarshal(secret, &values); err != nil {
			return nil, err
		}
		for key := range values {
			secretFields[key] = true
		}
	} else if credential.Kind == "snmp_v2c" {
		secretFields["community"] = true
	}
	config := string(b)
	if p.Catalog != nil {
		config = documentConfig(p)
		if p.Catalog.Definition.Adapter == "snmp_v2c" && p.Profile.Customized {
			config = string(b)
		}
		if p.Catalog.Definition.Pack == nil {
			p.Catalog.Definition.Pack = p.Profile
		}
	}
	return &ProfileView{SecretFields: secretFields, Catalog: p.Catalog, ComponentID: p.Profile.ComponentID, Name: p.Profile.Name, Readme: p.Profile.Readme, Config: config, Metrics: metrics, Boards: state.Boards, Rules: state.Rules, Customized: p.Profile.Customized}, nil
}
func deviceProfileState(d models.MonitorDevice) (profileState, error) {
	st := profileState{Boards: []ProfileResource{}, Rules: []ProfileResource{}, Suspended: map[int64]int{}}
	if d.ProfileState != "" && json.Unmarshal([]byte(d.ProfileState), &st) != nil {
		return st, errors.New("invalid device profile state")
	}
	if st.Suspended == nil {
		st.Suspended = map[int64]int{}
	}
	if st.Waiting == nil {
		st.Waiting = map[int64]int{}
	}
	return st, nil
}

// resolveDeviceProfile runs inside the device transaction after its locks and
// idempotency check. An ordinary edit keeps the snapshot; only an explicit
// template switch/refresh replaces native dashboard and alert customizations.
func resolveDeviceProfile(tx *gorm.DB, f *DeviceInput, old models.MonitorDevice) (bool, error) {
	if f.TemplateComponentID != 0 {
		var row models.MonitorCatalog
		err := tx.First(&row, "component_id = ?", f.TemplateComponentID).Error
		if err == nil {
			// Older clients may still select a component. Once it is catalogued,
			// resolve that selection through its release instead of mutable rows.
			if old.ID != "" && !f.RefreshTemplate {
				prior, e := loadTemplate(tx, old.TemplateVersionID)
				if e != nil {
					return false, e
				}
				if prior.Profile != nil && prior.Profile.ComponentID == f.TemplateComponentID {
					f.TemplateVersionID = old.TemplateVersionID
					f.TemplateComponentID = 0
					return resolveDeviceProfile(tx, f, old)
				}
			}
			if row.Disabled || row.Deleted || row.PublishedVersionID == "" {
				return false, bad("template is not published for device onboarding")
			}
			f.TemplateVersionID = row.PublishedVersionID
			f.TemplateComponentID = 0
			f.ResetResources = f.RefreshTemplate
			f.RefreshTemplate = false
			return resolveDeviceProfile(tx, f, old)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, err
		}
	}
	if f.TemplateComponentID == 0 {
		doc, err := loadTemplate(tx, f.TemplateVersionID)
		if err != nil {
			return false, err
		}
		changed := old.ID == "" || old.TemplateVersionID != f.TemplateVersionID
		apply := changed && doc.Profile != nil
		if doc.Catalog != nil && changed {
			var row models.MonitorCatalog
			if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "component_id = ?", doc.Profile.ComponentID).Error; err != nil {
				return false, bad("template is unavailable")
			}
			if row.Disabled || row.Deleted || row.PublishedVersionID != f.TemplateVersionID {
				return false, bad("select the current published template version")
			}
			if err := categoryEnabled(tx, row.CategoryID); err != nil {
				return false, err
			}
			if old.ID != "" && !f.ResetResources {
				prior, e := loadTemplate(tx, old.TemplateVersionID)
				if e != nil {
					return false, e
				}
				if prior.Profile != nil && prior.Profile.ComponentID == doc.Profile.ComponentID {
					apply = false
					if f.Customization == nil {
						custom, changes, mergeErr := mergeDeviceDefinition(prior, doc)
						if mergeErr != nil {
							return false, mergeErr
						}
						for _, change := range changes {
							if len(change.Conflicts) != 0 && f.UpgradeResolution != "keep_device" {
								return false, conflict("collection or metric overrides conflict with this release; review the template diff")
							}
						}
						f.Customization = custom
					}
				}
			}
		}
		f.ResetResources = false
		if f.Customization != nil {
			if doc.Profile == nil {
				return false, bad("template has no customizable profile")
			}
			if f.Customization.Config != nil {
				err = customizeConfig(&doc, *f.Customization.Config)
				if err != nil {
					return false, err
				}
				doc.Profile.Customized = true
			}
			if f.Customization.Metrics != nil {
				if len(*f.Customization.Metrics) > 256 {
					return false, bad("too many customized metrics")
				}
				doc.Profile.Metrics = *f.Customization.Metrics
				doc.Profile.Customized = true
			}
			if f.Customization.Dashboards != nil {
				doc.Profile.Dashboards = *f.Customization.Dashboards
				doc.Profile.Customized = true
				apply = true
			}
			if f.Customization.Alerts != nil {
				doc.Profile.Alerts = *f.Customization.Alerts
				doc.Profile.Customized = true
				apply = true
			}
			if err := validatePublishedResources(doc); err != nil {
				return false, err
			}
			b := canonical(doc)
			if len(b) > 2<<20 {
				return false, bad("component snapshot is too large")
			}
			hash := digest(b)
			id := "profile-" + hash[:56]
			row := models.MonitorTemplateVersion{ID: id, TemplateKey: id, Version: 1, Name: doc.Profile.Name, Definition: models.MonitorText(b), Digest: hash}
			if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
				return false, err
			}
			f.TemplateVersionID = id
			f.Customization = nil
		}
		return apply, nil
	} // historical API/version compatibility
	var d templateDocument
	apply := true
	if old.ID != "" && !f.RefreshTemplate {
		prior, err := loadTemplate(tx, old.TemplateVersionID)
		if err != nil {
			return false, err
		}
		if prior.Profile != nil && prior.Profile.ComponentID == f.TemplateComponentID {
			d = prior
			apply = false
		}
	}
	if d.Profile == nil {
		var err error
		d, err = readProfile(tx, f.TemplateComponentID)
		if err != nil {
			return false, err
		}
	}
	if f.Customization != nil {
		if f.Customization.Config != nil {
			c, err := parseTemplate(*f.Customization.Config)
			if err != nil {
				return false, err
			}
			d.TemplateConfig = c
			d.Profile.Customized = true
		}
		if f.Customization.Metrics != nil {
			if len(*f.Customization.Metrics) > 256 {
				return false, bad("too many customized metrics")
			}
			d.Profile.Metrics = *f.Customization.Metrics
			d.Profile.Customized = true
		}
	}
	if len(canonical(d)) > 2<<20 {
		return false, bad("component snapshot is too large")
	}
	b := canonical(d)
	hash := digest(b)
	id := "profile-" + hash[:56]
	name := []rune(d.Component)
	if len(name) > 128 {
		name = name[:128]
	}
	row := models.MonitorTemplateVersion{ID: id, TemplateKey: id, Version: 1, Name: string(name), Definition: models.MonitorText(b), Digest: hash}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return false, err
	}
	f.TemplateVersionID = id
	// These are selection commands, never collection execution parameters.
	f.TemplateComponentID = 0
	f.RefreshTemplate = false
	f.Customization = nil
	return apply, nil
}

// Scope every vector selector, including absent_over_time, to this exact
// managed revision. It prevents a stale or local Categraf series from matching.
func scopeExpression(expr string, d models.MonitorDevice) (string, error) {
	if strings.TrimSpace(expr) == "" {
		return expr, nil
	}
	// Native dashboard range macros are restored after parsing the expression.
	macros := [][2]string{{"$__rate_interval", "314159ms"}, {"$__interval", "314157ms"}}
	for _, m := range macros {
		expr = strings.ReplaceAll(expr, m[0], m[1])
	}
	tree, err := parser.ParseExpr(expr)
	if err != nil {
		return "", bad("invalid PromQL in device template or customization")
	}
	parser.Inspect(tree, func(n parser.Node, _ []parser.Node) error {
		v, ok := n.(*parser.VectorSelector)
		if !ok {
			return nil
		}
		out := []*labels.Matcher{}
		for _, m := range v.LabelMatchers {
			if !strings.HasPrefix(m.Name, "collection_") {
				out = append(out, m)
			}
		}
		for _, kv := range [][2]string{{"collection_device_id", d.ID}, {"collection_collector_id", d.CollectorID}, {"collection_revision", d.DesiredRevision}, {"collection_mode", "managed"}} {
			m, _ := labels.NewMatcher(labels.MatchEqual, kv[0], kv[1])
			out = append(out, m)
		}
		v.LabelMatchers = out
		return nil
	})
	result := tree.String()
	result = strings.ReplaceAll(result, "5m14s159ms", "$__rate_interval")
	result = strings.ReplaceAll(result, "5m14s157ms", "$__interval")
	return result, nil
}
func scopeJSON(raw string, d models.MonitorDevice, ds int64) (string, error) {
	var value interface{}
	if json.Unmarshal([]byte(raw), &value) != nil {
		return "", bad("invalid template JSON")
	}
	var walk func(interface{}) error
	walk = func(v interface{}) error {
		switch obj := v.(type) {
		case map[string]interface{}:
			for k, child := range obj {
				if k == "expr" || k == "prom_ql" {
					if s, ok := child.(string); ok {
						scoped, err := scopeExpression(s, d)
						if err != nil {
							return err
						}
						obj[k] = scoped
						continue
					}
				}
				if k == "datasourceValue" || k == "datasource_id" {
					obj[k] = ds
					continue
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []interface{}:
			for _, child := range obj {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(value); err != nil {
		return "", err
	}
	return string(canonical(value)), nil
}
func profileResourceName(d models.MonitorDevice, name string) string {
	name = strings.TrimSpace(name)
	chars := []rune(d.Name + " / " + name)
	if len(chars) > 135 {
		chars = chars[:135]
	}
	return string(chars) + " [" + d.ID[:8] + "]"
}

func syncDeviceProfile(tx *gorm.DB, d *models.MonitorDevice, old models.MonitorDevice, apply bool, ds int64, resolution ...bool) error {
	document, err := loadTemplate(tx, d.TemplateVersionID)
	if err != nil {
		return err
	}
	state, err := deviceProfileState(old)
	if err != nil {
		return err
	}
	ctx := &nctx.Context{DB: tx, IsCenter: true}
	stamp := time.Now().Unix()
	category := ""
	if document.Catalog != nil {
		category = strings.Join(document.Catalog.CategoryPath, " / ")
	}
	upgrading := !apply && old.ID != "" && old.TemplateVersionID != d.TemplateVersionID
	newRules := map[int64]bool{}
	if upgrading {
		diff, err := mergeDeviceResources(tx, *d, old, state, document, ds, false)
		if err != nil {
			return err
		}
		for _, resource := range diff.Resources {
			if len(resource.Conflicts) != 0 && (len(resolution) == 0 || !resolution[0]) {
				return conflict("template upgrade conflicts; review the diff and explicitly keep device overrides")
			}
		}
		if _, err := mergeDeviceResources(tx, *d, old, state, document, ds, true); err != nil {
			return err
		}
	}
	if apply || upgrading {
		if document.Profile == nil {
			document.Profile = &ProfileDefinition{}
		}
		// Retain historical instances and events, but retire the prior application.
		if apply {
			for _, ref := range state.Rules {
				if err := tx.Model(&models.AlertRule{}).Where("id = ?", ref.ID).Updates(map[string]interface{}{"disabled": 1, "update_at": stamp}).Error; err != nil {
					return err
				}
			}
			for _, ref := range state.Boards {
				if err := tx.Model(&models.Board{}).Where("id = ?", ref.ID).Update("hide", 1).Error; err != nil {
					return err
				}
			}
			state = profileState{Boards: []ProfileResource{}, Rules: []ProfileResource{}, Suspended: map[int64]int{}}
			state.Waiting = map[int64]int{}
		}
		for _, source := range document.Profile.Dashboards {
			if !apply && hasProfileResource(state.Boards, source) {
				continue
			}
			var spec struct {
				Name    string          `json:"name"`
				Configs json.RawMessage `json:"configs"`
			}
			if json.Unmarshal([]byte(source.Content), &spec) != nil || len(spec.Configs) == 0 {
				return bad("invalid dashboard template")
			}
			payload, err := scopeJSON(string(spec.Configs), *d, ds)
			if err != nil {
				return err
			}
			board := models.Board{GroupId: d.GroupID, Name: profileResourceName(*d, source.Name) + " v" + strconv.FormatInt(d.RecordVersion, 10), Ident: "snmp-" + d.ID + "-" + strconv.FormatInt(source.ID, 10) + "-" + strconv.FormatInt(d.RecordVersion, 10), CreateBy: "collection", UpdateBy: "collection"}
			if err := board.Add(ctx); err != nil {
				return err
			}
			if err := models.BoardPayloadSave(ctx, board.Id, payload); err != nil {
				return err
			}
			if err := models.BoardBusigroupAdd(tx, board.Id, []int64{d.GroupID}); err != nil {
				return err
			}
			state.Boards = append(state.Boards, ProfileResource{ID: board.Id, Name: source.Name, SourceID: source.ID})
		}
		for _, source := range document.Profile.Alerts {
			if !apply && hasProfileResource(state.Rules, source) {
				continue
			}
			content, err := scopeJSON(source.Content, *d, ds)
			if err != nil {
				return err
			}
			var rule models.AlertRule
			if json.Unmarshal([]byte(content), &rule) != nil {
				return bad("invalid alert template")
			}
			rule.Id = 0
			rule.GroupId = d.GroupID
			rule.Name = profileResourceName(*d, source.Name) + " v" + strconv.FormatInt(d.RecordVersion, 10)
			rule.CreateBy = "collection"
			rule.UpdateBy = "collection"
			rule.DatasourceQueries = []models.DatasourceQuery{{MatchType: 0, Op: "in", Values: []interface{}{ds}}}
			// Template adoption creates events; notification routing belongs to the
			// independent rule instance. Never inherit executable template callbacks.
			rule.NotifyGroupsJSON = []string{}
			rule.NotifyChannelsJSON = []string{}
			rule.NotifyRuleIds = []int64{}
			rule.PipelineConfigs = []models.PipelineConfig{}
			rule.CallbacksJSON = []string{}
			rule.NotifyVersion = 1
			applyNotification(&rule, *d, category)
			if err := rule.FE2DB(); err != nil {
				return err
			}
			if err := rule.Add(ctx); err != nil {
				return bad("invalid alert template: " + err.Error())
			}
			state.Rules = append(state.Rules, ProfileResource{ID: rule.Id, Name: source.Name, SourceID: source.ID})
			newRules[rule.Id] = true
		}
	}
	for _, ref := range state.Rules {
		var rule models.AlertRule
		if err := tx.First(&rule, "id = ?", ref.ID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		} else if err != nil {
			return err
		}
		notificationChanged := old.ID != "" && old.Notify != d.Notify && d.Notify != "" && d.Notify != "null"
		if notificationChanged {
			if err := rule.DB2FE(); err != nil {
				return err
			}
			rule.NotifyRuleIds = []int64{}
			applyNotification(&rule, *d, category)
			if err := rule.FE2DB(); err != nil {
				return err
			}
		}
		if !d.Enabled || d.Deleted {
			if _, ok := state.Suspended[ref.ID]; !ok || rule.UpdateBy != "collection" {
				state.Suspended[ref.ID] = rule.Disabled
				if desired, waiting := state.Waiting[ref.ID]; waiting && rule.UpdateBy == "collection" {
					state.Suspended[ref.ID] = desired
				}
				if _, waiting := state.Waiting[ref.ID]; waiting {
					state.Waiting[ref.ID] = state.Suspended[ref.ID]
				}
				// A lifecycle pause is the latest system write; a later native edit
				// changes this marker and remains protected during initial arming.
				rule.UpdateBy = "collection"
			}
			rule.Disabled = 1
		} else if disabled, ok := state.Suspended[ref.ID]; ok {
			if rule.UpdateBy == "collection" {
				rule.Disabled = disabled
			}
			delete(state.Suspended, ref.ID)
		}
		// Existing armed rules must keep detecting a bad new configuration. Only
		// initial adoption and genuinely new rules wait for their first samples.
		_, waiting := state.Waiting[ref.ID]
		if d.Enabled && !d.Deleted && document.Catalog != nil && (state.ArmedRevision == "" || newRules[ref.ID] || waiting) {
			if rule.UpdateBy != "collection" {
				// Explicit native edits supersede the pending system enable/disable
				// decision, even if another device save occurs before healthy data.
				delete(state.Waiting, ref.ID)
			} else {
				if !waiting {
					state.Waiting[ref.ID] = rule.Disabled
				}
				rule.Disabled = 1
			}
		}
		if old.DesiredRevision != d.DesiredRevision || old.GroupID != d.GroupID || apply {
			content, err := scopeJSON(rule.RuleConfig, *d, ds)
			if err != nil {
				return err
			}
			rule.RuleConfig = content
			if rule.PromQl != "" {
				rule.PromQl, err = scopeExpression(rule.PromQl, *d)
				if err != nil {
					return err
				}
			}
			rule.DatasourceQueries = []models.DatasourceQuery{{MatchType: 0, Op: "in", Values: []interface{}{ds}}}
		}
		rule.GroupId = d.GroupID
		rule.UpdateAt = max(stamp, rule.UpdateAt+1)
		fields := []string{"Disabled", "GroupId", "RuleConfig", "PromQl", "DatasourceQueries", "UpdateAt", "UpdateBy"}
		if notificationChanged {
			fields = append(fields, "NotifyRuleIds", "NotifyVersion", "Annotations", "Note")
		}
		if err := tx.Model(&rule).Select(fields).Updates(&rule).Error; err != nil {
			return err
		}
	}
	for _, ref := range state.Boards {
		var board models.Board
		if err := tx.First(&board, "id = ?", ref.ID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		} else if err != nil {
			return err
		}
		if old.DesiredRevision != d.DesiredRevision || old.GroupID != d.GroupID {
			payload, err := models.BoardPayloadGet(ctx, board.Id)
			if err != nil {
				return err
			}
			if payload != "" {
				payload, err = scopeJSON(payload, *d, ds)
				if err != nil {
					return err
				}
				if err = models.BoardPayloadSave(ctx, board.Id, payload); err != nil {
					return err
				}
			}
		}
		if old.GroupID != d.GroupID {
			if err := models.BoardBusigroupUpdate(ctx, board.Id, []int64{d.GroupID}); err != nil {
				return err
			}
		}
		if err := tx.Model(&board).Updates(map[string]interface{}{"group_id": d.GroupID, "hide": boolInt(d.Deleted), "update_at": max(stamp, board.UpdateAt+1)}).Error; err != nil {
			return err
		}
	}
	d.ProfileState = models.MonitorText(canonical(state))
	return tx.Model(d).Update("profile_state", d.ProfileState).Error
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
