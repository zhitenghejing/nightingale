package victorialogs

import (
	"context"
	"testing"
	"time"

	"github.com/ccfos/nightingale/v6/internal/testenv"
)

func integrationVictoriaLogs(t *testing.T) *VictoriaLogs {
	t.Helper()
	address := testenv.Require(t, "N9E_TEST_VICTORIALOGS_URL")
	return &VictoriaLogs{VictorialogsAddr: address, Headers: make(map[string]string), Timeout: 5000}
}

func TestVictoriaLogs_InitHTTPClient(t *testing.T) {
	v := &VictoriaLogs{VictorialogsAddr: "http://example.invalid", Timeout: 5000}
	if err := v.InitHTTPClient(); err != nil {
		t.Fatalf("InitHTTPClient failed: %v", err)
	}
	if v.HTTPClient == nil {
		t.Fatal("HTTPClient should not be nil after initialization")
	}
}

func TestVictoriaLogs_Query(t *testing.T) {
	v := integrationVictoriaLogs(t)
	ctx := context.Background()
	if err := v.InitHTTPClient(); err != nil {
		t.Fatalf("InitHTTPClient failed: %v", err)
	}

	// Query logs with basic query
	now := time.Now().UnixMilli()
	start := now - time.Hour.Milliseconds() // 1 hour ago
	end := now

	logs, err := v.Query(ctx, "*", start, end, 10)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	t.Logf("Query returned %d log entries", len(logs))
	for i, log := range logs {
		t.Logf("Log[%d]: %v", i, log)
	}
}

func TestVictoriaLogs_StatsQuery(t *testing.T) {
	v := integrationVictoriaLogs(t)
	ctx := context.Background()
	if err := v.InitHTTPClient(); err != nil {
		t.Fatalf("InitHTTPClient failed: %v", err)
	}

	// Stats query with count
	now := time.Now().UnixMilli()
	result, err := v.StatsQuery(ctx, "* | stats count() as total", now)
	if err != nil {
		t.Fatalf("StatsQuery failed: %v", err)
	}
	if result.Status != "success" {
		t.Fatalf("query status = %q", result.Status)
	}
	t.Logf("StatsQuery result: status=%s, resultType=%s", result.Status, result.Data.ResultType)
	for i, item := range result.Data.Result {
		t.Logf("Result[%d]: metric=%v, value=%v", i, item.Metric, item.Value)
	}
}

func TestVictoriaLogs_StatsQueryRange(t *testing.T) {
	v := integrationVictoriaLogs(t)
	ctx := context.Background()
	if err := v.InitHTTPClient(); err != nil {
		t.Fatalf("InitHTTPClient failed: %v", err)
	}

	// Stats query range
	now := time.Now().UnixMilli()
	start := now - time.Hour.Milliseconds() // 1 hour ago
	end := now

	result, err := v.StatsQueryRange(ctx, "* | stats count() as total", start, end, "5m")
	if err != nil {
		t.Fatalf("StatsQueryRange failed: %v", err)
	}
	if result.Status != "success" {
		t.Fatalf("query status = %q", result.Status)
	}
	t.Logf("StatsQueryRange result: status=%s, resultType=%s", result.Status, result.Data.ResultType)
	for i, item := range result.Data.Result {
		t.Logf("Result[%d]: metric=%v, values count=%d", i, item.Metric, len(item.Values))
	}
}

func TestVictoriaLogs_HitsLogs(t *testing.T) {
	v := integrationVictoriaLogs(t)
	ctx := context.Background()
	if err := v.InitHTTPClient(); err != nil {
		t.Fatalf("InitHTTPClient failed: %v", err)
	}

	// Get total hits count
	now := time.Now().UnixMilli()
	start := now - time.Hour.Milliseconds() // 1 hour ago
	end := now

	count, err := v.HitsLogs(ctx, "*", start, end)
	if err != nil {
		t.Fatalf("HitsLogs failed: %v", err)
	}
	t.Logf("HitsLogs total count: %d", count)
}

func TestVictoriaLogs_QueryWithFilter(t *testing.T) {
	v := integrationVictoriaLogs(t)
	ctx := context.Background()
	if err := v.InitHTTPClient(); err != nil {
		t.Fatalf("InitHTTPClient failed: %v", err)
	}

	// Query with a filter condition
	now := time.Now().UnixMilli()
	start := now - time.Hour.Milliseconds()
	end := now

	logs, err := v.Query(ctx, "_stream:{app=\"test\"}", start, end, 5)
	if err != nil {
		t.Fatalf("Query with filter failed: %v", err)
	}
	t.Logf("Query with filter returned %d log entries", len(logs))
}

func TestVictoriaLogs_StatsQueryByField(t *testing.T) {
	v := integrationVictoriaLogs(t)
	ctx := context.Background()
	if err := v.InitHTTPClient(); err != nil {
		t.Fatalf("InitHTTPClient failed: %v", err)
	}

	// Stats query grouped by field
	now := time.Now().UnixMilli()
	result, err := v.StatsQuery(ctx, "* | stats by (level) count() as cnt", now)
	if err != nil {
		t.Fatalf("StatsQuery by field failed: %v", err)
	}
	if result.Status != "success" {
		t.Fatalf("query status = %q", result.Status)
	}
	t.Logf("StatsQuery by field result: status=%s", result.Status)
	for i, item := range result.Data.Result {
		t.Logf("Result[%d]: metric=%v, value=%v", i, item.Metric, item.Value)
	}
}
