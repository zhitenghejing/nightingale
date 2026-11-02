package collection

import "gorm.io/gorm"

// Immutable form defaults precede legacy input normalization and request hashing.
// This keeps a database package from accidentally receiving the SNMP port default.
func inputDefaults(tx *gorm.DB, f *DeviceInput) error {
	if f.Parameters == nil || f.TemplateVersionID == "" || f.TemplateVersionID == WindowsTemplate {
		return nil
	}
	doc, err := loadTemplate(tx, f.TemplateVersionID)
	if err != nil {
		return err
	}
	if doc.Catalog == nil {
		return nil
	}
	if f.Protocol == "" {
		f.Protocol = doc.Catalog.Definition.Adapter
	}
	if f.Port == 0 {
		for _, field := range doc.Catalog.Definition.Fields {
			if field.Key == "port" {
				if value, ok := field.Default.(float64); ok {
					if value < 1 || value > 65535 || value != float64(int(value)) {
						return bad("invalid template port default")
					}
					f.Port = int(value)
				}
			}
		}
	}
	return nil
}
