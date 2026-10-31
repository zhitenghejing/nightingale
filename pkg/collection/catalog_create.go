package collection

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CatalogCreateInput struct {
	Name            string `json:"name"`
	Logo            string `json:"logo"`
	CategoryID      string `json:"category_id"`
	BaseComponentID uint64 `json:"base_component_id"`
}

func (s *Store) CreateCatalog(scope Scope, f CatalogCreateInput) (uint64, error) {
	if !scope.Admin {
		return 0, denied()
	}
	f.Name = strings.TrimSpace(f.Name)
	if !utf8.ValidString(f.Name) || len([]rune(f.Name)) < 1 || len([]rune(f.Name)) > 128 || !validLogo(f.Logo) {
		return 0, bad("invalid template name or icon")
	}
	var id uint64
	err := s.write(func(tx *gorm.DB) error {
		if _, err := categoryPath(tx, f.CategoryID); err != nil {
			return err
		}
		definition := CatalogDefinition{Adapter: "documentation", Fields: []DeviceField{}, Notification: "events_only", Pack: &ProfileDefinition{Metrics: []models.BuiltinMetric{}, Dashboards: []models.BuiltinPayload{}, Alerts: []models.BuiltinPayload{}}}
		if f.BaseComponentID != 0 {
			var original models.MonitorCatalog
			if err := tx.First(&original, "component_id = ? AND deleted = ?", f.BaseComponentID, false).Error; err != nil {
				return missing()
			}
			if err := json.Unmarshal([]byte(original.Draft), &definition); err != nil {
				return err
			}
			if definition.Pack == nil {
				return bad("reload the complete source draft before cloning")
			}
			if f.Logo == "" {
				f.Logo = string(original.Logo)
			}
		}
		component := models.BuiltinComponent{Ident: "template_" + strings.ReplaceAll(uuid.NewString(), "-", ""), Logo: f.Logo, Readme: definition.Pack.Readme}
		// The native component's explicit BIGINT type is not an SQLite rowid.
		if tx.Dialector.Name() == "sqlite" {
			var maximum uint64
			if err := tx.Model(&models.BuiltinComponent{}).Select("coalesce(max(id),0)").Scan(&maximum).Error; err != nil {
				return err
			}
			component.ID = maximum + 1
		}
		if err := component.Add(&nctx.Context{DB: tx, IsCenter: true}, "collection"); err != nil {
			return err
		}
		id = component.ID
		definition.Pack.ComponentID, definition.Pack.Name = id, f.Name
		row := models.MonitorCatalog{ComponentID: id, Name: f.Name, Logo: models.MonitorText(f.Logo), CategoryID: f.CategoryID, Draft: models.MonitorText(canonical(definition)), RecordVersion: 1}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return s.audit(tx, scope, 0, fmt.Sprint(component.ID), "catalog_created", "", map[string]interface{}{"cloned_from": f.BaseComponentID})
	})
	return id, err
}

func categoryEnabled(tx *gorm.DB, id string) error {
	for id != "" {
		var category models.MonitorCategory
		if err := tx.First(&category, "id = ?", id).Error; err != nil {
			return bad("category is unavailable")
		}
		if category.Disabled {
			return bad("category is disabled for new device bindings")
		}
		id = category.ParentID
	}
	return nil
}
