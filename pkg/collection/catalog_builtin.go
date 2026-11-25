package collection

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/ccfos/nightingale/v6/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed linux_x86_snmp_v2.json
var linuxX86SNMPDefinition []byte

const linuxX86SNMPIdent = "template_f757dec2151c4da6894ebf9e2f967b62"
const linuxSNMPName = "SNMP采集"
const linuxSNMPLogo = "/api/n9e/integrations/icon/Linux_SNMP/linux.png"
const linuxAgentLogo = "/api/n9e/integrations/icon/Linux/linux.png"

func linuxX86SNMP() (CatalogDefinition, error) {
	var definition CatalogDefinition
	err := json.Unmarshal(linuxX86SNMPDefinition, &definition)
	return definition, err
}

type builtinCatalogTemplate struct {
	ident      string
	name       string
	logo       string
	path       []string
	version    int // Zero for a manual onboarding package kept as a draft.
	definition func() (CatalogDefinition, error)
}

// EnsureBuiltinCatalog installs each bundled template once, preserving local
// edits, releases, moves and deletions on subsequent startups.
func (s *Store) EnsureBuiltinCatalog() error {
	arm := []string{"主机系统", "Linux", "ARM"}
	x86 := []string{"主机系统", "Linux", "X86"}
	templates := []builtinCatalogTemplate{
		{linuxX86SNMPIdent, linuxSNMPName, linuxSNMPLogo, x86, 2, linuxX86SNMP},
		{linuxX86AgentIdent, "Agent采集", linuxAgentLogo, x86, 0, linuxX86Agent},
		{linuxARMSNMPIdent, linuxSNMPName, linuxSNMPLogo, arm, 1, linuxARMSNMP},
		{linuxARMAgentIdent, "Agent采集", linuxAgentLogo, arm, 0, linuxARMAgent},
	}
	return s.write(func(tx *gorm.DB) error {
		for _, template := range templates {
			if err := s.installBuiltinCatalog(tx, template); err != nil {
				return fmt.Errorf("%s: %w", template.ident, err)
			}
		}
		return nil
	})
}

func (s *Store) installBuiltinCatalog(tx *gorm.DB, template builtinCatalogTemplate) error {
	installed := func() (bool, error) {
		var count int64
		err := tx.Model(&models.BuiltinComponent{}).Where("ident = ?", template.ident).Count(&count).Error
		return count > 0, err
	}
	if exists, err := installed(); exists || err != nil {
		return err
	}

	categoryID, err := seedCategory(tx, template.path)
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
	if err := tx.Model(&models.MonitorCatalog{}).Where("category_id = ? AND name = ?", categoryID, template.name).Count(&count).Error; err != nil || count > 0 {
		return err
	}

	definition, err := template.definition()
	if err != nil {
		return err
	}
	publish := template.version > 0
	if err := validateCatalogDefinition(definition, publish); err != nil {
		return err
	}
	if !publish {
		if err := validatePack(definition.Pack); err != nil {
			return err
		}
	}
	component := models.BuiltinComponent{Ident: template.ident, Logo: template.logo, Readme: definition.Pack.Readme}
	if err := addCatalogComponent(tx, &component); err != nil {
		return err
	}
	definition.Pack.ComponentID = component.ID
	row := models.MonitorCatalog{
		ComponentID: component.ID, Name: template.name, Logo: models.MonitorText(template.logo),
		CategoryID: categoryID, RecordVersion: 1,
	}
	if publish {
		doc, err := documentFromDraft(definition, component.ID)
		if err != nil {
			return err
		}
		doc.Catalog = &CatalogRelease{Version: template.version, Taxonomy: TypeBrandModelTaxonomy, CategoryPath: template.path, Definition: definition}
		if err := validatePublishedResources(doc); err != nil {
			return err
		}
		version := catalogVersion(row, doc, template.version)
		if len(version.Definition) > 2<<20 {
			return bad("template snapshot is too large")
		}
		if err := tx.Create(&version).Error; err != nil {
			return err
		}
		row.PublishedVersionID = version.ID
	}
	row.Draft = models.MonitorText(canonical(definition))
	if err := tx.Create(&row).Error; err != nil {
		return err
	}
	return s.audit(tx, Scope{}, 0, fmt.Sprintf("catalog:%d", component.ID), "builtin_catalog_installed", "", map[string]int{"version": template.version})
}
