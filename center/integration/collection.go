package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/ccfos/nightingale/v6/models"
	nctx "github.com/ccfos/nightingale/v6/pkg/ctx"
	"gorm.io/gorm"
)

// Editable collection packs use the native four-part catalog. Files seed a
// component once; subsequent edits and deletions of its resources remain in DB.
// The collection method is a fenced TOML block in the native instructions.
func seedCollectionPack(ctx *nctx.Context, dir string, component models.BuiltinComponent) (bool, error) {
	if !strings.Contains(component.Readme, "```snmp-collection\n") && !strings.Contains(component.Readme, "```device-onboarding\n") {
		return false, nil
	}
	return true, models.DB(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.BuiltinComponent
		if err := tx.Where("ident = ?", component.Ident).First(&existing).Error; err == nil {
			return nil
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		c := &nctx.Context{DB: tx, IsCenter: true}
		const author = "template"
		if err := component.Add(c, author); err != nil {
			return err
		}
		for _, typ := range []string{"dashboard", "alert"} {
			folder := typ + "s"
			files, err := filepath.Glob(filepath.Join(dir, folder, "*.json"))
			if err != nil {
				return err
			}
			for _, file := range files {
				b, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				resources := []json.RawMessage{}
				if typ == "alert" {
					if err := json.Unmarshal(b, &resources); err != nil {
						return err
					}
				} else {
					resources = append(resources, b)
				}
				for _, content := range resources {
					var meta struct {
						Name string `json:"name"`
						UUID int64  `json:"uuid"`
						Note string `json:"note"`
					}
					if err := json.Unmarshal(content, &meta); err != nil {
						return err
					}
					cate := ""
					if typ == "alert" {
						cate = strings.TrimSuffix(filepath.Base(file), ".json")
					}
					p := models.BuiltinPayload{ComponentID: component.ID, Type: typ, Cate: cate, Name: meta.Name, UUID: meta.UUID, Note: meta.Note, Content: string(content)}
					if err := p.Add(c, author); err != nil {
						return err
					}
				}
			}
		}
		files, err := filepath.Glob(filepath.Join(dir, "metrics", "*.json"))
		if err != nil {
			return err
		}
		for _, file := range files {
			b, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			var metrics []models.BuiltinMetric
			if err := json.Unmarshal(b, &metrics); err != nil {
				return err
			}
			for _, m := range metrics {
				m.ID = 0
				m.Typ = component.Ident
				if err := m.Add(c, author); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
