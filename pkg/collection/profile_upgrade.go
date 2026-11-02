package collection

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/pelletier/go-toml/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ResourceDiff struct {
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Changed   []string `json:"changed"`
	Conflicts []string `json:"conflicts"`
}
type TemplateDiff struct {
	Resources []ResourceDiff `json:"resources"`
}

// A missing member is distinct from a JSON null. Maps merge by property;
// arrays are kept as a device override when their structure differs.
type missingValue struct{}

func mergeTemplateValue(base, current, next interface{}, path string, diff *ResourceDiff) interface{} {
	if reflect.DeepEqual(base, next) {
		return current
	}
	if reflect.DeepEqual(base, current) {
		diff.Changed = append(diff.Changed, path)
		return next
	}
	if reflect.DeepEqual(current, next) {
		return current
	}
	b, bok := base.(map[string]interface{})
	c, cok := current.(map[string]interface{})
	n, nok := next.(map[string]interface{})
	if bok && cok && nok {
		keys := map[string]bool{}
		for key := range b {
			keys[key] = true
		}
		for key := range c {
			keys[key] = true
		}
		for key := range n {
			keys[key] = true
		}
		ordered := []string{}
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		out := map[string]interface{}{}
		get := func(m map[string]interface{}, k string) interface{} {
			if v, ok := m[k]; ok {
				return v
			}
			return missingValue{}
		}
		for _, key := range ordered {
			value := mergeTemplateValue(get(b, key), get(c, key), get(n, key), strings.TrimPrefix(path+"."+key, "."), diff)
			if _, missing := value.(missingValue); !missing {
				out[key] = value
			}
		}
		return out
	}
	ba, bok := base.([]interface{})
	ca, cok := current.([]interface{})
	na, nok := next.([]interface{})
	if bok && cok && nok && len(ba) == len(ca) && len(ba) == len(na) {
		for i := range ba {
			bm, bok := ba[i].(map[string]interface{})
			cm, cok := ca[i].(map[string]interface{})
			nm, nok := na[i].(map[string]interface{})
			if bok && cok && nok && bm["id"] != nil && (!reflect.DeepEqual(bm["id"], cm["id"]) || !reflect.DeepEqual(bm["id"], nm["id"])) {
				diff.Conflicts = append(diff.Conflicts, path)
				return current
			}
		}
		out := make([]interface{}, len(ba))
		for i := range ba {
			out[i] = mergeTemplateValue(ba[i], ca[i], na[i], fmt.Sprintf("%s[%d]", path, i), diff)
		}
		return out
	}
	diff.Conflicts = append(diff.Conflicts, path)
	return current
}

func resourceSource(items []models.BuiltinPayload, id int64, name string) *models.BuiltinPayload {
	for i := range items {
		if id != 0 && items[i].ID == id {
			return &items[i]
		}
	}
	for i := range items {
		if items[i].Name == name {
			return &items[i]
		}
	}
	return nil
}
func hasProfileResource(refs []ProfileResource, source models.BuiltinPayload) bool {
	for _, ref := range refs {
		if ref.SourceID != 0 && ref.SourceID == source.ID || ref.SourceID == 0 && ref.Name == source.Name {
			return true
		}
	}
	return false
}

func ruleFields(rule models.AlertRule, d models.MonitorDevice, ds int64) (map[string]interface{}, error) {
	if err := rule.DB2FE(); err != nil {
		return nil, err
	}
	content, err := scopeJSON(rule.RuleConfig, d, ds)
	if err != nil {
		return nil, err
	}
	var config interface{}
	if err := json.Unmarshal([]byte(content), &config); err != nil {
		return nil, err
	}
	fields := map[string]interface{}{"name": rule.Name, "rule_config": config, "prom_for_duration": float64(rule.PromForDuration), "prom_eval_interval": float64(rule.PromEvalInterval), "severity": float64(rule.Severity), "disabled": float64(rule.Disabled), "annotations": rule.AnnotationsJSON}
	var normalized map[string]interface{}
	err = json.Unmarshal(canonical(fields), &normalized)
	return normalized, err
}

func sourceRuleFields(source models.BuiltinPayload, d models.MonitorDevice, ds int64) (map[string]interface{}, error) {
	content, err := scopeJSON(source.Content, d, ds)
	if err != nil {
		return nil, err
	}
	var rule models.AlertRule
	if err := json.Unmarshal([]byte(content), &rule); err != nil {
		return nil, err
	}
	rule.Name = profileResourceName(d, source.Name) + " v" + strconv.FormatInt(d.RecordVersion, 10)
	if err := rule.FE2DB(); err != nil {
		return nil, err
	}
	if err := rule.Verify(); err != nil {
		return nil, err
	}
	return ruleFields(rule, d, ds)
}

// Same-source upgrades update independent instances in place. Comparing the
// old package, current instance and new package preserves only actual overrides.
func mergeDeviceResources(tx *gorm.DB, d models.MonitorDevice, old models.MonitorDevice, state profileState, next templateDocument, ds int64, write bool) (TemplateDiff, error) {
	out := TemplateDiff{Resources: []ResourceDiff{}}
	previous, err := loadTemplate(tx, old.TemplateVersionID)
	if err != nil {
		return out, err
	}
	if previous.Profile == nil || next.Profile == nil || previous.Profile.ComponentID != next.Profile.ComponentID {
		return out, nil
	}
	ctx := &nctx.Context{DB: tx, IsCenter: true}
	for index, ref := range state.Rules {
		baseSource := resourceSource(previous.Profile.Alerts, ref.SourceID, ref.Name)
		if baseSource == nil {
			continue
		}
		nextSource := resourceSource(next.Profile.Alerts, baseSource.ID, baseSource.Name)
		if nextSource == nil {
			out.Resources = append(out.Resources, ResourceDiff{Kind: "alert", Name: ref.Name, Changed: []string{}, Conflicts: []string{"模板已删除此规则；保留设备实例"}})
			continue
		}
		var rule models.AlertRule
		query := tx
		if write {
			query = tx.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&rule, "id = ?", ref.ID).Error; err != nil {
			return out, err
		}
		current, err := ruleFields(rule, d, ds)
		if err != nil {
			return out, err
		}
		if desired, waiting := state.Waiting[ref.ID]; waiting {
			current["disabled"] = float64(desired)
		}
		base, err := sourceRuleFields(*baseSource, old, ds)
		if err != nil {
			return out, err
		}
		// Compare PromQL at the same revision; routing labels are system-owned.
		base["rule_config"], err = reScopeValue(base["rule_config"], d, ds)
		if err != nil {
			return out, err
		}
		incoming, err := sourceRuleFields(*nextSource, d, ds)
		if err != nil {
			return out, err
		}
		if old.Name == d.Name && baseSource.Name == nextSource.Name {
			incoming["name"] = base["name"]
		}
		if strings.HasPrefix(rule.Name, profileResourceName(old, baseSource.Name)+" v") {
			base["name"] = current["name"]
		}
		diff := ResourceDiff{Kind: "alert", Name: ref.Name, Changed: []string{}, Conflicts: []string{}}
		merged := mergeTemplateValue(base, current, incoming, "", &diff)
		out.Resources = append(out.Resources, diff)
		if write {
			state.Rules[index].SourceID = nextSource.ID
			state.Rules[index].Name = nextSource.Name
			if err := rule.DB2FE(); err != nil {
				return out, err
			}
			if err := json.Unmarshal(canonical(merged), &rule); err != nil {
				return out, err
			}
			if err := rule.FE2DB(); err != nil {
				return out, err
			}
			if _, waiting := state.Waiting[ref.ID]; waiting {
				state.Waiting[ref.ID] = rule.Disabled
				rule.Disabled = 1
			}
			if err := rule.Verify(); err != nil {
				return out, bad(err.Error())
			}
			rule.UpdateAt = max(time.Now().Unix(), rule.UpdateAt+1)
			if err := tx.Model(&rule).Select("Name", "RuleConfig", "PromForDuration", "PromEvalInterval", "Severity", "Disabled", "Annotations", "UpdateAt").Updates(&rule).Error; err != nil {
				return out, err
			}
		}
	}
	for index, ref := range state.Boards {
		baseSource := resourceSource(previous.Profile.Dashboards, ref.SourceID, ref.Name)
		if baseSource == nil {
			continue
		}
		nextSource := resourceSource(next.Profile.Dashboards, baseSource.ID, baseSource.Name)
		if nextSource == nil {
			out.Resources = append(out.Resources, ResourceDiff{Kind: "dashboard", Name: ref.Name, Changed: []string{}, Conflicts: []string{"模板已删除此仪表盘；保留设备实例"}})
			continue
		}
		get := func(raw string) (interface{}, error) {
			var value struct {
				Configs json.RawMessage `json:"configs"`
			}
			if err := json.Unmarshal([]byte(raw), &value); err != nil {
				return nil, err
			}
			return reScopeValue(value.Configs, d, ds)
		}
		base, err := get(baseSource.Content)
		if err != nil {
			return out, err
		}
		incoming, err := get(nextSource.Content)
		if err != nil {
			return out, err
		}
		raw, err := models.BoardPayloadGet(ctx, ref.ID)
		if err != nil {
			return out, err
		}
		current, err := reScopeValue(json.RawMessage(raw), d, ds)
		if err != nil {
			return out, err
		}
		diff := ResourceDiff{Kind: "dashboard", Name: ref.Name, Changed: []string{}, Conflicts: []string{}}
		merged := mergeTemplateValue(base, current, incoming, "", &diff)
		out.Resources = append(out.Resources, diff)
		if write {
			state.Boards[index].SourceID = nextSource.ID
			state.Boards[index].Name = nextSource.Name
			if err := models.BoardPayloadSave(ctx, ref.ID, string(canonical(merged))); err != nil {
				return out, err
			}
		}
	}
	for kind, sources := range map[string][]models.BuiltinPayload{"dashboard": next.Profile.Dashboards, "alert": next.Profile.Alerts} {
		refs := state.Boards
		if kind == "alert" {
			refs = state.Rules
		}
		for _, source := range sources {
			if !hasProfileResource(refs, source) {
				out.Resources = append(out.Resources, ResourceDiff{Kind: kind, Name: source.Name, Changed: []string{"新增资源"}, Conflicts: []string{}})
			}
		}
	}
	return out, nil
}

func reScopeValue(value interface{}, d models.MonitorDevice, ds int64) (interface{}, error) {
	raw, err := scopeJSON(string(canonical(value)), d, ds)
	if err != nil {
		return nil, err
	}
	var out interface{}
	err = json.Unmarshal([]byte(raw), &out)
	return out, err
}

func (s *Store) DeviceTemplateDiff(scope Scope, id, version string) (TemplateDiff, error) {
	d, err := s.Device(scope, id)
	if err != nil {
		return TemplateDiff{}, err
	}
	next, err := loadTemplate(s.DB, version)
	if err != nil {
		return TemplateDiff{}, err
	}
	state, err := deviceProfileState(d)
	if err != nil {
		return TemplateDiff{}, err
	}
	var collector models.MonitorCollector
	if err := s.DB.First(&collector, "id = ?", d.CollectorID).Error; err != nil {
		return TemplateDiff{}, err
	}
	current := d
	current.TemplateVersionID = version
	out, err := mergeDeviceResources(s.DB, current, d, state, next, collector.DatasourceID, false)
	if err != nil {
		return out, err
	}
	previous, err := loadTemplate(s.DB, d.TemplateVersionID)
	if err != nil {
		return out, err
	}
	_, changes, err := mergeDeviceDefinition(previous, next)
	out.Resources = append(out.Resources, changes...)
	return out, err
}

func jsonValue(v interface{}) interface{} {
	var out interface{}
	_ = json.Unmarshal(canonical(v), &out)
	return out
}

func mergeDeviceDefinition(previous, next templateDocument) (*ProfileCustomization, []ResourceDiff, error) {
	changes := []ResourceDiff{}
	if previous.Catalog == nil || next.Catalog == nil || previous.Profile == nil || next.Profile == nil || previous.Profile.ComponentID != next.Profile.ComponentID || previous.Catalog.Definition.Pack == nil {
		return nil, changes, nil
	}
	base := previous
	base.TemplateConfig = TemplateConfig{}
	if err := customizeConfig(&base, previous.Catalog.Definition.Config); err != nil {
		return nil, changes, err
	}
	configDiff := ResourceDiff{Kind: "collection", Name: "采集配置", Changed: []string{}, Conflicts: []string{}}
	baseConfig, currentConfig, nextConfig := jsonValue(base.TemplateConfig), jsonValue(previous.TemplateConfig), jsonValue(next.TemplateConfig)
	if previous.Catalog.Definition.Adapter == "mysql" && next.Catalog.Definition.Adapter == "mysql" {
		b, err := parseMySQL(previous.Catalog.Definition.Config)
		if err != nil {
			return nil, changes, err
		}
		c, err := parseMySQL(documentConfig(previous))
		if err != nil {
			return nil, changes, err
		}
		n, err := parseMySQL(documentConfig(next))
		if err != nil {
			return nil, changes, err
		}
		baseConfig, currentConfig, nextConfig = jsonValue(b), jsonValue(c), jsonValue(n)
	}
	mergedConfig := mergeTemplateValue(baseConfig, currentConfig, nextConfig, "", &configDiff)
	metricsDiff := ResourceDiff{Kind: "metrics", Name: "指标说明", Changed: []string{}, Conflicts: []string{}}
	mergedMetrics := mergeTemplateValue(jsonValue(previous.Catalog.Definition.Pack.Metrics), jsonValue(previous.Profile.Metrics), jsonValue(next.Profile.Metrics), "", &metricsDiff)
	changes = append(changes, configDiff, metricsDiff)
	custom := &ProfileCustomization{}
	if !reflect.DeepEqual(mergedConfig, nextConfig) {
		if next.Catalog.Definition.Adapter != "snmp_v2c" {
			var config mysqlRecipe
			if err := json.Unmarshal(canonical(mergedConfig), &config); err != nil {
				return nil, changes, err
			}
			text, err := toml.Marshal(config)
			if err != nil {
				return nil, changes, err
			}
			value := string(text)
			custom.Config = &value
		} else {
			var config TemplateConfig
			if err := json.Unmarshal(canonical(mergedConfig), &config); err != nil {
				return nil, changes, err
			}
			text, err := toml.Marshal(config)
			if err != nil {
				return nil, changes, err
			}
			value := string(text)
			custom.Config = &value
		}
	}
	if !reflect.DeepEqual(mergedMetrics, jsonValue(next.Profile.Metrics)) {
		var metrics []models.BuiltinMetric
		if err := json.Unmarshal(canonical(mergedMetrics), &metrics); err != nil {
			return nil, changes, err
		}
		custom.Metrics = &metrics
	}
	if custom.Config == nil && custom.Metrics == nil {
		return nil, changes, nil
	}
	return custom, changes, nil
}
