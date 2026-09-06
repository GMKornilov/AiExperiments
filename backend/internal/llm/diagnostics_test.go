package llm

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLLMLogsAreCorrelatedAndExcludeSecrets(t *testing.T) {
	t.Setenv("LLM_LOG_PAYLOADS", "false")
	for _, test := range []struct {
		name   string
		status int
		body   string
		reason string
	}{
		{"success", 200, `{"choices":[{"message":{"content":"private-answer"}}]}`, "success"},
		{"auth", 401, `{"error":{"message":"private-key private-prompt"}}`, "http_error"},
		{"malformed", 200, "private-answer", "invalid_response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			var logs bytes.Buffer
			client := NewClient(server.URL, "private-key", time.Second)
			client.logger = slog.New(slog.NewJSONHandler(&logs, nil))
			ctx := WithRequestID(context.Background(), "1234567890abcdef1234567890abcdef")
			_, _ = client.Chat(ctx, "test-model", "private-prompt")
			value := logs.String()
			for _, expected := range []string{"llm.start", "llm.finish", "1234567890abcdef1234567890abcdef", test.reason, "http_status", "duration_ms"} {
				if !strings.Contains(value, expected) {
					t.Errorf("missing %q in logs: %s", expected, value)
				}
			}
			for _, secret := range []string{"private-key", "private-prompt", "private-answer", server.URL} {
				if strings.Contains(value, secret) {
					t.Errorf("logs contain sensitive data")
				}
			}
		})
	}
}

func TestFullPayloadDiagnostics(t *testing.T) {
	t.Setenv("LLM_LOG_PAYLOADS", "true")
	for _, test := range []struct {
		name    string
		status  int
		partial bool
	}{
		{"success", 200, false},
		{"provider_error", 400, false},
		{"partial_timeout", 200, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"full-answer private-key"}}]}`))
				if test.partial {
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				}
			}))
			defer server.Close()
			var logs bytes.Buffer
			client := NewClient(server.URL, "private-key", 100*time.Millisecond)
			client.logger = slog.New(slog.NewJSONHandler(&logs, nil))
			_, _ = client.Chat(context.Background(), "test-model", "full-prompt private-key")
			value := logs.String()
			for _, expected := range []string{"llm.request", "llm.response", "full-prompt", "full-answer", "[REDACTED]"} {
				if !strings.Contains(value, expected) {
					t.Errorf("missing %q", expected)
				}
			}
			if strings.Contains(value, "private-key") || strings.Contains(value, server.URL) || strings.Contains(value, "Authorization") {
				t.Fatal("sensitive transport data in logs")
			}
			if test.partial && (!strings.Contains(value, `"complete":false`) || !strings.Contains(value, `"reason":"timeout"`) || !strings.Contains(value, `"stage":"read_body"`)) {
				t.Errorf("missing partial response diagnostics: %s", value)
			}
			if (test.partial || test.status == 400) && !strings.Contains(value, `"error":`) {
				t.Fatal("missing concrete error")
			}
		})
	}
}

func TestTransportFailure(t *testing.T) {
	if transportFailure(context.DeadlineExceeded) != "timeout" || transportFailure(context.Canceled) != "canceled" || transportFailure(errors.New("secret")) != "network" {
		t.Fatal("incorrect transport error classification")
	}
	ctx := WithRequestID(context.Background(), "untrusted\nsecret")
	if len(RequestID(ctx)) != 32 || strings.Contains(RequestID(ctx), "secret") {
		t.Fatal("untrusted request ID was not replaced")
	}
}
