package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
	"aichallenge/week_1/task_1/internal/observability"
	"aichallenge/week_1/task_1/internal/session"
)

func TestLargePromptReachesProviderUnchanged(t *testing.T) {
	t.Run("success", func(t *testing.T) { testLargePrompt(t, false) })
	t.Run("DeepSeek context overflow", func(t *testing.T) { testLargePrompt(t, true) })
}

func testLargePrompt(t *testing.T, overflow bool) {
	prompt := strings.Repeat("123456 ", 2_000_000)
	received := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string        `json:"model"`
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body.Model == "chat" {
			received <- body.Messages[len(body.Messages)-1].Content
		}
		w.Header().Set("Content-Type", "application/json")
		if overflow && body.Model == "chat" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"This model's maximum context length is 1048576 tokens. However, you requested 6000348 tokens (6000348 in the messages, 0 in the completion). Please reduce the length of the messages or completion.","type":"invalid_request_error","code":"invalid_request_error"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer upstream.Close()
	snapshot := agent.Snapshot{BaseURL: upstream.URL, APIKey: "fixture", Model: "chat", SystemPrompt: "system", Timeout: 30 * time.Second}
	title := snapshot
	title.Model = "title"
	store := session.NewStore(agent.OpenAIProvider{})
	defer store.Close()
	h := New(store, func() (agent.DialogSnapshot, error) { return agent.DialogSnapshot{Chat: snapshot, Text: title}, nil }, observability.NewJournal(false, nil))
	id := createDialog(t, h, "large")
	body, err := json.Marshal(map[string]string{"client_message_id": "large-1", "text": prompt})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/dialogs/"+id+"/messages", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Session-ID", "large")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d", w.Code)
	}
	if overflow && !strings.Contains(w.Body.String(), `"error_category":"context_limit"`) {
		t.Fatal("provider context overflow did not reach the message DTO")
	}
	select {
	case got := <-received:
		if got != prompt {
			t.Fatal("prompt changed in transit")
		}
	default:
		t.Fatal("provider did not receive prompt")
	}
}
