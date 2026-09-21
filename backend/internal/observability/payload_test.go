package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestPayloadRedactionAndDisabledLogging(t *testing.T) {
	if got := redactPayload(`{"ok":true} trailing provider diagnostics`, "key"); got != `{"ok":true} trailing provider diagnostics` {
		t.Fatal("invalid JSON response was silently shortened")
	}
	for _, enabled := range []bool{true, false} {
		var out bytes.Buffer
		j := NewJournal(enabled, slog.New(slog.NewJSONHandler(&out, nil)))
		j.SetSecrets("dialog", "secret-key")
		j.Log(Record{DialogID: "dialog", Event: "llm_response", Payload: `{"choices":[{"content":"secret-key","reasoning_content":"\u0073ecret-key"}],"usage":{"prompt_tokens":9007199254740991}}`}, "")
		logs := j.Logs("dialog")
		if got := j.LogTextPayloads(); got != enabled {
			t.Fatalf("LogTextPayloads() = %t, want %t", got, enabled)
		}
		if strings.Contains(out.String(), "secret-key") || strings.Contains(logs[0].Payload, "secret-key") {
			t.Fatal("credential leaked")
		}
		if !enabled && (logs[0].Payload != "" || strings.Contains(out.String(), "reasoning_content")) {
			t.Fatal("disabled payload logged")
		}
		if enabled && (!json.Valid([]byte(logs[0].Payload)) || !strings.Contains(logs[0].Payload, "9007199254740991") || !strings.Contains(logs[0].Payload, "[REDACTED]")) {
			t.Fatal("payload structure lost")
		}
	}
}

func TestJournalRedactsRegisteredAndPerCallCredentialsInMemoryAndConsole(t *testing.T) {
	var out bytes.Buffer
	j := NewJournal(true, slog.New(slog.NewJSONHandler(&out, nil)))
	j.SetSecrets("dialog", "chat-secret", "title-secret")
	j.Log(Record{
		DialogID: "dialog",
		Event:    "llm_request",
		Text:     "chat-secret title-secret call-secret",
		Payload:  `{"chat-secret":"title-secret","nested":["call-secret"]}`,
	}, "call-secret")

	logs := j.Logs("dialog")
	if len(logs) != 1 {
		t.Fatalf("logs count = %d, want 1", len(logs))
	}
	for _, secret := range []string{"chat-secret", "title-secret", "call-secret"} {
		if strings.Contains(logs[0].Text, secret) || strings.Contains(logs[0].Payload, secret) || strings.Contains(out.String(), secret) {
			t.Fatalf("secret %q leaked", secret)
		}
	}
	if logs[0].Text != "[REDACTED] [REDACTED] [REDACTED]" || !json.Valid([]byte(logs[0].Payload)) {
		t.Fatalf("redacted record = %#v", logs[0])
	}

	var console map[string]any
	if err := json.Unmarshal(out.Bytes(), &console); err != nil {
		t.Fatalf("decode structured console log: %v", err)
	}
	if _, ok := console["payload"]; !ok {
		t.Fatalf("console log has no payload: %#v", console)
	}
	if console["text"] != "[REDACTED] [REDACTED] [REDACTED]" {
		t.Fatalf("console text = %#v", console["text"])
	}
}
