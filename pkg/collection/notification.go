package collection

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"gorm.io/gorm"
)

func (s *Store) prepareNotification(tx *gorm.DB, scope Scope, f *DeviceInput, old models.MonitorDevice) error {
	if f.Notify == nil {
		if old.Notify != "" && old.Notify != "null" {
			return json.Unmarshal([]byte(old.Notify), &f.Notify)
		}
		doc, err := loadTemplate(tx, f.TemplateVersionID)
		if err != nil {
			return err
		}
		if doc.Catalog != nil {
			f.Notify = doc.Catalog.Definition.Notify
		}
		if f.Notify == nil {
			return nil
		}
	}
	// Never mutate the shared release while resolving its defaults.
	var preset NotificationPreset
	if err := json.Unmarshal(canonical(f.Notify), &preset); err != nil {
		return err
	}
	f.Notify = &preset
	if preset.Mode == "inherit" {
		doc, err := loadTemplate(tx, f.TemplateVersionID)
		if err != nil {
			return err
		}
		f.Notify.RuleIDs = []int64{}
		if doc.Catalog != nil && doc.Catalog.Definition.Notify != nil {
			f.Notify.RuleIDs = append(f.Notify.RuleIDs, doc.Catalog.Definition.Notify.RuleIDs...)
		}
	}
	if preset.Mode != "events_only" && preset.Mode != "inherit" && preset.Mode != "rules" {
		return bad("invalid notification mode")
	}
	if len([]rune(preset.Message)) > 1024 || len(preset.RuleIDs) > 16 {
		return bad("notification preset is too large")
	}
	if preset.Mode == "events_only" {
		f.Notify.RuleIDs = []int64{}
		return nil
	}
	if preset.Mode == "rules" && len(preset.RuleIDs) == 0 {
		return bad("select at least one notification rule")
	}
	if len(preset.RuleIDs) == 0 {
		return nil
	} // inherit with no mapping: events only, explicitly shown in UI
	var groups []int64
	if !scope.Admin {
		var err error
		groups, err = models.MyGroupIds(&nctx.Context{DB: tx, IsCenter: true}, scope.ActorID)
		if err != nil {
			return err
		}
	}
	for _, id := range preset.RuleIDs {
		var rule models.NotifyRule
		if err := tx.First(&rule, "id = ?", id).Error; err != nil || !rule.Enable {
			return bad("notification rule is missing or disabled")
		}
		if !scope.Admin {
			allowed := false
			for _, own := range groups {
				for _, assigned := range rule.UserGroupIds {
					allowed = allowed || own == assigned
				}
			}
			if !allowed {
				return denied()
			}
		}
		if len(rule.NotifyConfigs) == 0 {
			return bad("notification rule has no channels")
		}
		for _, config := range rule.NotifyConfigs {
			var count int64
			if err := tx.Model(&models.NotifyChannelConfig{}).Where("id = ? AND enable = ?", config.ChannelID, true).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return bad("notification channel is missing or disabled")
			}
			if config.TemplateID > 0 {
				if err := tx.Model(&models.NotifyTpl{}).Where("id = ?", config.TemplateID).Count(&count).Error; err != nil {
					return err
				}
				if count != 1 {
					return bad("notification message template is unavailable")
				}
			}
		}
	}
	return nil
}

func applyNotification(rule *models.AlertRule, d models.MonitorDevice, category ...string) {
	var preset NotificationPreset
	if json.Unmarshal([]byte(d.Notify), &preset) != nil {
		return
	}
	if preset.Mode != "events_only" {
		rule.NotifyRuleIds = append([]int64{}, preset.RuleIDs...)
	}
	rule.NotifyVersion = 1
	if preset.Message != "" {
		if rule.AnnotationsJSON == nil {
			rule.AnnotationsJSON = map[string]string{}
		}
		path := ""
		if len(category) != 0 {
			path = category[0]
		}
		// Quote static values so device names cannot inject template expressions.
		rule.AnnotationsJSON["collection_message"] = strings.NewReplacer("{{设备名称}}", fmt.Sprintf("{{ %q }}", d.Name), "{{设备分类}}", fmt.Sprintf("{{ %q }}", path), "{{规则名称}}", "{{ .RuleName }}", "{{当前值}}", "{{ $value }}", "{{发生时间}}", "{{ .TriggerTime }}", "{{告警级别}}", "{{ .Severity }}").Replace(preset.Message)
		// Native media templates already render RuleNote. Referencing the parsed
		// annotation keeps media-specific payloads and credentials in native rules.
		rule.Note = "{{ $annotations.collection_message }}"
	}
}
