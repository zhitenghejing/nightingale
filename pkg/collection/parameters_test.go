package collection

import (
	"encoding/json"
	"testing"

	"github.com/ccfos/nightingale/v6/models"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

func TestTemplateParametersSealInlineSecretsAndPreserveOnEdit(t *testing.T) {
	s, scope, c, _, token := testStore(t)
	e := catalogFixture(t, s)
	e.Draft.Fields = []DeviceField{{Key: "protocol", Label: "版本", Type: "select", Required: true, Default: "snmp_v2c", Options: []string{"snmp_v2c"}}, {Key: "port", Label: "端口", Type: "number", Required: true, Default: 161}, {Key: "community", Label: "团体字", Type: "password", Secret: true, Required: true}}
	require.NoError(t, s.SaveCatalogDraft(scope, e.ComponentID, CatalogDraftInput{Definition: e.Draft, RecordVersion: e.RecordVersion}, true))
	view, err := s.Catalog()
	require.NoError(t, err)
	secret := "fixture-inline-\"\n@secret"
	f := DeviceInput{Name: "inline fixture", Address: "2001:db8::123", GroupID: 1, CollectorID: c.ID, Protocol: "snmp_v2c", TemplateVersionID: view.Entries[0].PublishedVersionID, Parameters: map[string]interface{}{"community": secret}, Enabled: true}
	d, err := s.SaveDevice(scope, "", "inline-secret-fixture", f)
	require.NoError(t, err)
	require.NotContains(t, string(d.Parameters), secret)
	require.NotContains(t, string(canonical(d)), secret)
	var credential models.MonitorCredential
	require.NoError(t, s.DB.First(&credential, "id = ?", d.CredentialID).Error)
	require.NotContains(t, string(credential.Ciphertext), secret)
	provider, _, err := s.Provider(c.ID, token, "")
	require.NoError(t, err)
	for _, content := range provider.Configs["snmp"] {
		var config snmpConfig
		require.NoError(t, toml.Unmarshal([]byte(content.Config), &config))
		require.Equal(t, secret, config.Instances[0].Community)
		require.Equal(t, "udp://[2001:db8::123]:161", config.Instances[0].Agents[0])
	}
	profile, err := s.DeviceProfile(d)
	require.NoError(t, err)
	require.True(t, profile.SecretFields["community"])
	f = inputFromDevice(d)
	f.Name = "renamed fixture"
	edited, err := s.SaveDevice(scope, d.ID, "", f)
	require.NoError(t, err)
	require.Equal(t, d.CredentialID, edited.CredentialID)
	require.Equal(t, d.DesiredRevision, edited.DesiredRevision)
}

func TestConditionalParametersExcludeHiddenCredentials(t *testing.T) {
	var fields []DeviceField
	require.NoError(t, json.Unmarshal([]byte(`[{"key":"tls","label":"TLS","type":"select","required":true,"options":["verify","ca"],"default":"verify"},{"key":"ca_password","label":"CA password","type":"password","required":true,"secret":true,"when":[{"key":"tls","values":["ca"]}]}]`), &fields))
	values, err := validateParameters(fields, map[string]interface{}{"tls": "verify", "ca_password": "must-not-be-delivered"})
	require.NoError(t, err)
	require.NotContains(t, values, "ca_password")
	_, err = validateParameters(fields, map[string]interface{}{"tls": "ca"})
	require.ErrorContains(t, err, "ca_password")
	definition := CatalogDefinition{Adapter: "redfish", Fields: fields, Notification: "events_only"}
	require.NoError(t, validateCatalogDefinition(definition, false))
	fields[1].Default = "must-not-be-saved"
	definition.Fields = fields
	require.ErrorContains(t, validateCatalogDefinition(definition, false), "default")
}
