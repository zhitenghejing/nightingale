package models

import (
	"encoding/json"
	"testing"
)

func TestExternalPushValidationWithoutAlertEngine(t *testing.T) {
	previous := VerifyByProvider
	VerifyByProvider = nil
	defer func() { VerifyByProvider = previous }()
	c := NotifyChannelConfig{Name: "simulation draft", Ident: ExternalPushType, RequestType: ExternalPushType}
	if err := c.Verify(); err != nil {
		t.Fatal("disabled draft should be savable", err)
	}
	c.Enable = true
	c.RequestConfig = &RequestConfig{ExternalPushConfig: &ExternalPushConfig{Mode: "production", Endpoint: "https://admin.ztservice.cn/messageWarn!acceptWarn.act"}}
	if err := c.Verify(); err == nil {
		t.Fatal("production channel enabled while alert-engine hook is absent")
	}
	c.RequestConfig.ExternalPushConfig = &ExternalPushConfig{Mode: "simulation", Endpoint: "http://127.0.0.1:19091/mock", Destination: "TEST", TimeoutMS: 100, ProblemType: "problem", RecoveryType: "ok", TestSign: "TEST_ONLY", TimeZone: "UTC"}
	if err := c.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestExternalPushUsesConfirmedProblemAndOKTypes(t *testing.T) {
	config := ExternalPushConfig{Mode: "simulation", Endpoint: "http://127.0.0.1:19091/mock", Destination: "TEST_ONLY", TimeoutMS: 100, ProblemType: "problem", RecoveryType: "ok", TestSign: "TEST_ONLY_NOT_A_CUSTOMER_SIGNATURE", TimeZone: "UTC"}
	if err := config.Verify(); err != nil {
		t.Fatalf("confirmed event types rejected: %v", err)
	}
	for _, types := range [][2]string{{"problem", "TEST_recovery"}, {"TEST_problem", "TEST_recovery"}, {"ok", "problem"}} {
		config.ProblemType, config.RecoveryType = types[0], types[1]
		if err := config.Verify(); err == nil {
			t.Fatalf("unconfirmed/legacy event types accepted: %v", types)
		}
	}
}

func TestConfirmedEventTypesDoNotProvideASignatureDefault(t *testing.T) {
	config := ExternalPushConfig{Mode: "simulation", Endpoint: "http://127.0.0.1:19091/mock", Destination: "TEST_ONLY", TimeoutMS: 100, ProblemType: "problem", RecoveryType: "ok", TimeZone: "UTC"}
	for _, value := range []string{"", " \t "} {
		config.TestSign = value
		if err := config.Verify(); err == nil {
			t.Fatal("test signer value was inferred or empty")
		}
	}
}

func TestExternalPushFileSigningConfiguration(t *testing.T) {
	var c ExternalPushConfig
	raw := `{"mode":"simulation","endpoint":"http://127.0.0.1:19091/mock","destination":"TEST","timeout_ms":1000,"problem_type":"problem","recovery_type":"ok","time_zone":"Asia/Shanghai","signing_mode":"key_file","signing_key_ref":"platform10-v1"}`
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(); err != nil {
		t.Fatalf("file-key mode must validate without embedding a password or test sign: %v", err)
	}
}

func TestExternalPushFileSigningRejectsMixedSourcesAndPaths(t *testing.T) {
	var c ExternalPushConfig
	json.Unmarshal([]byte(`{"mode":"simulation","endpoint":"http://127.0.0.1:19091/mock","destination":"TEST","timeout_ms":1000,"problem_type":"problem","recovery_type":"ok","time_zone":"UTC","signing_mode":"key_file"}`), &c)
	for _, ref := range []string{"/tmp/key", "../key", "TEST key"} {
		c.SigningKeyRef = ref
		if c.Verify() == nil {
			t.Fatal("path/password-like reference allowed")
		}
	}
	c.SigningKeyRef = "TEST-v1"
	c.TestSign = "TEST_VALUE"
	if c.Verify() == nil {
		t.Fatal("mixed key and literal signing accepted")
	}
	c.SigningMode = "test_value"
	if c.Verify() == nil {
		t.Fatal("test mode retained a key reference")
	}
}

func TestPlatform10AllowsOnlyConfirmedEndpointAndFileSigning(t *testing.T) {
	c := ExternalPushConfig{Mode: "platform10", Endpoint: "https://admin.ztservice.cn/messageWarn!acceptWarn.act", Destination: "platform10", TimeoutMS: 1000,
		ProblemType: "problem", RecoveryType: "ok", SigningMode: "key_file", SigningKeyRef: "platform10-v1", TimeZone: "Asia/Shanghai"}
	// Configuration validation must not load keys, resolve DNS, or send requests.
	if err := c.Verify(); err != nil {
		t.Fatalf("confirmed endpoint with file signing rejected: %v", err)
	}
	for _, endpoint := range []string{"http://admin.ztservice.cn/messageWarn!acceptWarn.act", "https://zt.ztservice.cn/messageWarn!acceptWarn.act", "https://admin.ztservice.cn/messageWarn!acceptWarn.act?json=x", "https://admin.ztservice.cn/other", "http://127.0.0.1:19999/mock"} {
		c.Endpoint = endpoint
		if c.Verify() == nil {
			t.Fatalf("unconfirmed real-mode endpoint allowed: %s", endpoint)
		}
	}
	c.Endpoint = "https://admin.ztservice.cn/messageWarn!acceptWarn.act"
	c.SigningMode, c.SigningKeyRef, c.TestSign = "test_value", "", "TEST_ONLY"
	if c.Verify() == nil {
		t.Fatal("explicit fake test signature allowed at real endpoint")
	}
	c.Mode = "simulation"
	if c.Verify() == nil {
		t.Fatal("simulation can reach the real receiver")
	}
}
