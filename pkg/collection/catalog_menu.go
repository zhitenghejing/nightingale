package collection

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const TypeBrandModelTaxonomy = "type_brand_model"

//go:embed catalog_seed_v1.json
var legacyCatalogSeed []byte

// Migrate only untouched seed locations, once. User-created/edited categories
// and immutable releases remain intact. Audit state prevents deleted seed
// directories or later intentional moves from being recreated on every read.
func (s *Store) migrateCatalogTaxonomy(tx *gorm.DB) error {
	const action = "catalog_taxonomy_type_brand_model"
	var done int64
	if err := tx.Model(&models.MonitorAudit{}).Where("action = ?", action).Count(&done).Error; err != nil || done > 0 {
		return err
	}
	var oldPaths, newPaths map[string][]string
	if err := json.Unmarshal(legacyCatalogSeed, &oldPaths); err != nil {
		return err
	}
	if err := json.Unmarshal(catalogSeed, &newPaths); err != nil {
		return err
	}
	var categories []models.MonitorCategory
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Find(&categories).Error; err != nil {
		return err
	}
	if err := tx.Model(&models.MonitorAudit{}).Where("action = ?", action).Count(&done).Error; err != nil || done > 0 {
		return err
	}
	byID := map[string]models.MonitorCategory{}
	for _, c := range categories {
		byID[c.ID] = c
	}
	untouched := map[string]int{}
	for _, path := range oldPaths {
		parent := ""
		for depth, name := range path {
			id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("nightingale-template-category:"+parent+":"+catalogLabel(name))).String()
			c, ok := byID[id]
			if !ok || c.ParentID != parent || c.Name != catalogLabel(name) || c.RecordVersion != 1 || c.Disabled || c.Sort != 0 {
				break
			}
			untouched[id] = depth + 1
			parent = id
		}
	}
	for _, name := range []string{"服务器", "数据库", "操作系统", "安全设备", "网络设备", "存储"} {
		if _, err := seedCategory(tx, []string{name}); err != nil {
			return err
		}
	}
	var rows []models.MonitorCatalog
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("deleted = ?", false).Order("component_id").Find(&rows).Error; err != nil {
		return err
	}
	var components []models.BuiltinComponent
	if err := tx.Select("id,ident").Find(&components).Error; err != nil {
		return err
	}
	idents := map[uint64]string{}
	for _, component := range components {
		idents[component.ID] = component.Ident
	}
	moved := 0
	for _, row := range rows {
		path := newPaths[idents[row.ComponentID]]
		oldPath := oldPaths[idents[row.ComponentID]]
		expected := ""
		for _, name := range oldPath {
			expected = uuid.NewSHA1(uuid.NameSpaceOID, []byte("nightingale-template-category:"+expected+":"+catalogLabel(name))).String()
		}
		if untouched[row.CategoryID] != 3 || len(path) != 3 || len(oldPath) != 3 || row.CategoryID != expected {
			continue
		}
		id, err := seedCategory(tx, path)
		if err != nil {
			return err
		}
		if id == row.CategoryID {
			continue
		}
		if err := tx.Model(&models.MonitorCatalog{}).Where("component_id = ?", row.ComponentID).Updates(map[string]interface{}{"category_id": id, "record_version": row.RecordVersion + 1}).Error; err != nil {
			return err
		}
		moved++
	}
	// Remove empty, untouched legacy locations from leaves upwards.
	for depth := 3; depth >= 1; depth-- {
		for id, level := range untouched {
			if level != depth {
				continue
			}
			var children, entries int64
			if err := tx.Model(&models.MonitorCategory{}).Where("parent_id = ?", id).Count(&children).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.MonitorCatalog{}).Where("category_id = ? AND deleted = ?", id, false).Count(&entries).Error; err != nil {
				return err
			}
			if children == 0 && entries == 0 {
				if err := tx.Delete(&models.MonitorCategory{}, "id = ?", id).Error; err != nil {
					return err
				}
			}
		}
	}
	return s.audit(tx, Scope{Admin: true}, 0, "catalog", action, "", map[string]interface{}{"moved_templates": moved})
}

type CategoryMoveInput struct {
	ParentID      string `json:"parent_id"`
	BeforeID      string `json:"before_id"`
	RecordVersion int64  `json:"record_version"`
}

func categoryDepth(byID map[string]models.MonitorCategory, id string) int {
	depth := 0
	for id != "" {
		c, ok := byID[id]
		if !ok || depth >= 3 {
			return -1
		}
		depth++
		id = c.ParentID
	}
	return depth
}

// Move within the same semantic level and persist sibling order atomically.
func (s *Store) MoveCategory(scope Scope, id string, f CategoryMoveInput) error {
	if !scope.Admin {
		return denied()
	}
	return s.write(func(tx *gorm.DB) error {
		var all []models.MonitorCategory
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Find(&all).Error; err != nil {
			return err
		}
		byID := map[string]models.MonitorCategory{}
		for _, c := range all {
			byID[c.ID] = c
		}
		source, ok := byID[id]
		if !ok {
			return missing()
		}
		if source.RecordVersion != f.RecordVersion {
			return conflict("category changed; reload before saving")
		}
		depth, parentDepth := categoryDepth(byID, id), categoryDepth(byID, f.ParentID)
		if depth < 1 || parentDepth < 0 || depth != parentDepth+1 {
			return bad("move categories within the same level")
		}
		if f.BeforeID == id && source.ParentID == f.ParentID {
			return nil
		}
		siblings := []models.MonitorCategory{}
		for _, c := range all {
			if c.ID != id && c.ParentID == f.ParentID {
				if c.Name == source.Name {
					return conflict("a sibling category already has this name")
				}
				siblings = append(siblings, c)
			}
		}
		sort.Slice(siblings, func(i, j int) bool {
			a, b := siblings[i], siblings[j]
			if a.Sort != b.Sort {
				return a.Sort < b.Sort
			}
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return a.ID < b.ID
		})
		at := len(siblings)
		if f.BeforeID != "" {
			at = -1
			for i, c := range siblings {
				if c.ID == f.BeforeID {
					at = i
					break
				}
			}
			if at < 0 {
				return bad("drop position is unavailable")
			}
		}
		siblings = append(siblings, models.MonitorCategory{})
		copy(siblings[at+1:], siblings[at:])
		source.ParentID = f.ParentID
		siblings[at] = source
		for i, c := range siblings {
			original := byID[c.ID]
			if original.ParentID == c.ParentID && original.Sort == (i+1)*10 {
				continue
			}
			if err := tx.Model(&models.MonitorCategory{}).Where("id = ?", c.ID).Updates(map[string]interface{}{"parent_id": c.ParentID, "sort": (i + 1) * 10, "record_version": original.RecordVersion + 1}).Error; err != nil {
				return err
			}
		}
		return s.audit(tx, scope, 0, id, "category_moved", "", f)
	})
}

type CatalogMoveInput struct {
	CategoryID    string `json:"category_id"`
	RecordVersion int64  `json:"record_version"`
}

func (s *Store) MoveCatalog(scope Scope, id uint64, f CatalogMoveInput) error {
	if !scope.Admin {
		return denied()
	}
	return s.write(func(tx *gorm.DB) error {
		row, err := lockCatalog(tx, id, f.RecordVersion)
		if err != nil {
			return err
		}
		if _, err = categoryPath(tx, f.CategoryID); err != nil {
			return err
		}
		if row.CategoryID == f.CategoryID {
			return nil
		}
		if err = tx.Model(&row).Updates(map[string]interface{}{"category_id": f.CategoryID, "record_version": row.RecordVersion + 1}).Error; err != nil {
			return err
		}
		return s.audit(tx, scope, 0, fmt.Sprint(id), "catalog_moved", "", f)
	})
}
