package collection

import (
	_ "embed"
	"encoding/json"

	"github.com/ccfos/nightingale/v6/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed linux_x86_snmp_v2.json
var linuxX86SNMPDefinition []byte

const linuxX86SNMPIdent = "template_f757dec2151c4da6894ebf9e2f967b62"
const linuxX86SNMPName = "SNMP采集"
const linuxX86SNMPLogo = "/api/n9e/integrations/icon/Linux_SNMP/linux.png"

// EnsureBuiltinCatalog installs the bundled release once, preserving local
// edits, releases, moves and deletions on subsequent startups.
func (s *Store) EnsureBuiltinCatalog() error {
	return s.write(func(tx *gorm.DB) error {
		installed := func() (bool, error) {
			var count int64
			err := tx.Model(&models.BuiltinComponent{}).Where("ident = ?", linuxX86SNMPIdent).Count(&count).Error
			return count > 0, err
		}
		if exists, err := installed(); exists || err != nil {
			return err
		}

		path := []string{"主机系统", "Linux", "X86"}
		categoryID, err := seedCategory(tx, path)
		if err != nil {
			return err
		}
		// Lock the common leaf so simultaneous bootstraps cannot both install.
		var category models.MonitorCategory
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&category, "id = ?", categoryID).Error; err != nil {
			return err
		}
		if exists, err := installed(); exists || err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.MonitorCatalog{}).Where("category_id = ? AND name = ?", categoryID, linuxX86SNMPName).Count(&count).Error; err != nil || count > 0 {
			return err
		}

		var definition CatalogDefinition
		if err := json.Unmarshal(linuxX86SNMPDefinition, &definition); err != nil {
			return err
		}
		if err := validateCatalogDefinition(definition, true); err != nil {
			return err
		}
		doc, err := documentFromDraft(definition, 0)
		if err != nil {
			return err
		}
		if err := validatePublishedResources(doc); err != nil {
			return err
		}
		component := models.BuiltinComponent{Ident: linuxX86SNMPIdent, Logo: linuxX86SNMPLogo, Readme: definition.Pack.Readme}
		if err := addCatalogComponent(tx, &component); err != nil {
			return err
		}
		definition.Pack.ComponentID = component.ID
		row := models.MonitorCatalog{
			ComponentID: component.ID, Name: linuxX86SNMPName, Logo: models.MonitorText(linuxX86SNMPLogo),
			CategoryID: categoryID, Draft: models.MonitorText(canonical(definition)), RecordVersion: 1,
		}
		const versionNumber = 2
		doc.Catalog = &CatalogRelease{Version: versionNumber, Taxonomy: TypeBrandModelTaxonomy, CategoryPath: path, Definition: definition}
		version := catalogVersion(row, doc, versionNumber)
		if len(version.Definition) > 2<<20 {
			return bad("template snapshot is too large")
		}
		if err := tx.Create(&version).Error; err != nil {
			return err
		}
		row.PublishedVersionID = version.ID
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return s.audit(tx, Scope{}, 0, version.TemplateKey, "builtin_catalog_installed", "", map[string]interface{}{"version": versionNumber})
	})
}
