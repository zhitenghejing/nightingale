// Package testenv gates tests that require explicitly provisioned external services.
package testenv

import (
	"os"
	"strings"
	"testing"
)

// Require skips only absent configuration. Once configured, callers must fail
// on connection or assertion errors. CI can require every external dependency.
func Require(t testing.TB, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if strings.TrimSpace(value) == "" {
		if os.Getenv("N9E_REQUIRE_EXTERNAL_TESTS") == "1" {
			t.Fatalf("external integration test requires %s", key)
		}
		t.Skipf("external integration test: set %s to enable", key)
	}
	return value
}
