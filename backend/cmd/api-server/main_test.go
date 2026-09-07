package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestConfigFailureLogIsStructuredAndSafe(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	logConfig(logger, "request-id", "failure", time.Millisecond, "config")
	got := output.String()
	for _, field := range []string{`"source":"backend"`, `"event":"config_read"`, `"result":"failure"`, `"correlation_id":"request-id"`, `"duration_ms":1`, `"error_category":"config"`} {
		if !strings.Contains(got, field) {
			t.Fatalf("log misses %s: %s", field, got)
		}
	}
	if strings.Contains(got, "api_key") || strings.Contains(got, "config.yaml") {
		t.Fatalf("unsafe detail in log: %s", got)
	}
}
