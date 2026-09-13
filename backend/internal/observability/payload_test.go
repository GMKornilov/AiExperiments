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
