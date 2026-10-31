package collection

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// prepareParameters is the device write boundary: evaluate the published schema,
// discard hidden inputs, and seal secrets before any device or audit is stored.
func (s *Store) prepareParameters(tx *gorm.DB, scope Scope, f *DeviceInput, old models.MonitorDevice) error {
	if f.Parameters == nil {
		return nil
	} // historical SNMP clients
	doc, err := loadTemplate(tx, f.TemplateVersionID)
	if err != nil {
		return err
	}
	if doc.Catalog == nil {
		return bad("template has no device form")
	}
	d := doc.Catalog.Definition
	if d.Adapter != f.Protocol {
		return bad("selected template and device adapter differ")
	}
	input := map[string]interface{}{}
	for k, v := range f.Parameters {
		input[k] = v
	}
	input["port"] = float64(f.Port)
	input["address"] = f.Address
	input["protocol"] = f.Protocol
	if f.CredentialID != "" {
		input["credential_id"] = f.CredentialID
	}
	prior := map[string]interface{}{}
	if old.ID != "" && old.CredentialID != "" && old.GroupID == f.GroupID && old.Protocol == f.Protocol {
		previous, err := loadTemplate(tx, old.TemplateVersionID)
		if err != nil {
			return err
		}
		if previous.Profile != nil && doc.Profile != nil && previous.Profile.ComponentID == doc.Profile.ComponentID {
			cred, err := s.credential(tx, old.CredentialID, scope, old.GroupID)
			if err != nil {
				return err
			}
			if strings.HasPrefix(cred.Kind, "parameters:") {
				secret, err := s.Keys.Open(string(cred.Ciphertext), cred.KeyID, "credential:"+cred.ID)
				if err != nil {
					return err
				}
				if json.Unmarshal(secret, &prior) != nil {
					return bad("invalid encrypted device parameters")
				}
			} else if cred.Kind == "snmp_v2c" {
				secret, err := s.Keys.Open(string(cred.Ciphertext), cred.KeyID, "credential:"+cred.ID)
				if err != nil {
					return err
				}
				prior["community"] = string(secret)
			}
		}
	}
	for _, field := range d.Fields {
		if field.Secret || field.Type == "password" {
			if input[field.Key] == nil || input[field.Key] == "" {
				if v, ok := prior[field.Key]; ok {
					input[field.Key] = v
				}
			}
		}
	}
	values, err := validateParameters(d.Fields, input)
	if err != nil {
		return err
	}
	if timeout, ok := values["timeout"].(float64); ok {
		if timeout < 1 || timeout > 10 || timeout != float64(int(timeout)) {
			return bad("invalid SNMP timeout")
		}
		f.TimeoutSeconds = int(timeout)
	}
	if timeout, ok := values["connect_timeout"].(float64); ok {
		if timeout < 1 || timeout > 60 || timeout != float64(int(timeout)) {
			return bad("invalid database timeout")
		}
		f.TimeoutSeconds = int(timeout)
	}
	secrets := map[string]interface{}{}
	for _, field := range d.Fields {
		if field.Secret || field.Type == "password" {
			if v, ok := values[field.Key]; ok {
				secrets[field.Key] = v
				delete(values, field.Key)
			}
		}
	}
	if len(secrets) != 0 || f.CredentialID == "" {
		if old.ID != "" && old.GroupID == f.GroupID && string(canonical(secrets)) == string(canonical(prior)) && len(prior) != 0 {
			f.CredentialID = old.CredentialID
		} else {
			cred := models.MonitorCredential{ID: uuid.NewString(), Name: catalogLabel(f.Name + " / 设备凭据"), Kind: "parameters:" + f.Protocol, GroupID: f.GroupID, RecordVersion: 1, UpdatedAt: time.Now().Unix()}
			sealed, key, err := s.Keys.Seal(canonical(secrets), "credential:"+cred.ID)
			if err != nil {
				return err
			}
			cred.Ciphertext, cred.KeyID = models.MonitorText(sealed), key
			if err := tx.Create(&cred).Error; err != nil {
				return err
			}
			f.CredentialID = cred.ID
		}
	}
	f.Parameters = values
	delete(f.Parameters, "address")
	delete(f.Parameters, "port")
	delete(f.Parameters, "protocol")
	delete(f.Parameters, "credential_id")
	if len(f.Parameters) == 0 {
		f.Parameters = nil
	}
	return nil
}

func executionParameters(f DeviceInput, cred models.MonitorCredential, secret []byte) (map[string]interface{}, error) {
	params := map[string]interface{}{}
	for k, v := range f.Parameters {
		params[k] = v
	}
	if strings.HasPrefix(cred.Kind, "parameters:") {
		var private map[string]interface{}
		if json.Unmarshal(secret, &private) != nil {
			return nil, bad("invalid encrypted device parameters")
		}
		for k, v := range private {
			params[k] = v
		}
	} else {
		params["community"] = string(secret)
	}
	return params, nil
}
