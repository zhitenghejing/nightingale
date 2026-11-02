package aop

import "testing"

func TestCollectionBodiesCannotBeEnabledForLogging(t *testing.T) {
	paths := []string{"/api/n9e/collection/credentials", "/api/n9e/collection/collectors", "/api/n9e/collection/devices/device-id", "/v1/n9e/collection/configs"}
	configured := map[string]struct{}{}
	for _, p := range paths {
		configured[p] = struct{}{}
	}
	config := LoggerConfig{PrintBodyPaths: func() map[string]struct{} { return configured }}
	for _, p := range paths {
		if config.ContainsPath(p) {
			t.Errorf("collection body logging enabled for %s", p)
		}
	}
	configured["/api/n9e/ordinary"] = struct{}{}
	if !config.ContainsPath("/api/n9e/ordinary") {
		t.Fatal("unrelated body logging behavior changed")
	}
}
