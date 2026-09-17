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

func TestParseFlagsAcceptsMultipleEnvFiles(t *testing.T) {
	options, err := parseFlags([]string{"--config", "custom.yaml", "--env-file", ".env", "--env-file", "deploy/secrets.env"})
	if err != nil {
		t.Fatal(err)
	}
	if options.configPath != "custom.yaml" {
		t.Fatalf("configPath = %q", options.configPath)
	}
	if got, want := strings.Join(options.envFiles, ","), ".env,deploy/secrets.env"; got != want {
		t.Fatalf("envFiles = %q, want %q", got, want)
	}
}

func TestParseFlagsRejectsEmptyEnvFile(t *testing.T) {
	if _, err := parseFlags([]string{"--env-file", " "}); err == nil {
		t.Fatal("ожидалась ошибка пустого пути dotenv-файла")
	}
}
