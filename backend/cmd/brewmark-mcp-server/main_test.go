package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestTimeout(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"0", "-1s", "invalid"} {
		if _, err := requestTimeout(value); err == nil {
			t.Fatalf("requestTimeout(%q) succeeded", value)
		}
	}
	if timeout, err := requestTimeout(""); err != nil || timeout.String() != "10s" {
		t.Fatalf("default = %v, %v", timeout, err)
	}
}

func TestEmitEventRetriesOnNonNoContentAndOmitsInapplicableFields(t *testing.T) {
	var calls atomic.Int32
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"http_status", "error_category", "tool"} {
			if _, found := payload[field]; found {
				t.Fatalf("unexpected optional field %q in %#v", field, payload)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer collector.Close()

	started := time.Now()
	emitEvent(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), collector.URL, "collector-secret", "brewmark", "brewmark_request", "brewmark_request", "success", "", 0, "trace-1", time.Millisecond, "")
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("publisher exceeded deadline: %s", elapsed)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("collector calls = %d, want 2", got)
	}
}

func TestEmitEventCollectorFailureLogsWithoutSecrets(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	var calls atomic.Int32
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer collector.Close()
	const token = "collector-secret"
	started := time.Now()
	emitEvent(logger, collector.URL, token, "brewmark", "brewmark_request", "brewmark_request", "success", "", 0, "trace-1", time.Millisecond, "")
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("publisher exceeded deadline: %s", elapsed)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("collector calls = %d, want 2", got)
	}
	output := logs.String()
	if !bytes.Contains([]byte(output), []byte("collector_unavailable")) {
		t.Fatalf("missing failure log: %s", output)
	}
	if bytes.Contains([]byte(output), []byte(collector.URL)) || bytes.Contains([]byte(output), []byte(token)) {
		t.Fatalf("failure log contains secret: %s", output)
	}
}
