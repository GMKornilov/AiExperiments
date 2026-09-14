package httpapi

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/observability"
	"aichallenge/week_1/task_1/internal/session"
)

func TestJournalCapturesChatTitleAndSummary(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"answer","reasoning_content":"details"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
	}))
	defer upstream.Close()
	endpoint := agent.Snapshot{BaseURL: upstream.URL, APIKey: "secret", Model: "model", SystemPrompt: "system", Timeout: time.Second}
	snapshot := agent.DialogSnapshot{Chat: endpoint, Text: endpoint, Summary: &agent.SummaryConfig{Snapshot: endpoint, KeepLastMessages: 1, BatchSize: 1}}
	store := session.NewStore(agent.OpenAIProvider{})
	defer store.Close()
	journal := observability.NewJournal(true, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	h := New(store, func() (agent.DialogSnapshot, error) { return snapshot, nil }, journal)
	d, err := store.Create("browser", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-Session-ID", "browser")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
		}
	}
	for i := 0; i < 2; i++ {
		request("POST", "/api/dialogs/"+d.ID+"/messages", fmt.Sprintf(`{"client_message_id":"c%d","text":"question"}`, i))
	}
	store.Close()
	calls := map[string][]observability.Record{}
	purposes := map[string]int{}
	for _, r := range journal.Logs(d.ID) {
		if r.CallID != "" {
			calls[r.CallID] = append(calls[r.CallID], r)
		}
	}
	if len(calls) != 4 {
		t.Fatalf("expected 2 chat, title, summary; got %d calls", len(calls))
	}
	for _, pair := range calls {
		if len(pair) != 2 || pair[0].Event != "llm_request" || pair[1].Event != "llm_response" || pair[0].CorrelationID == "" || pair[0].Purpose != pair[1].Purpose {
			t.Fatalf("broken trace pair: %+v", pair)
		}
		if !strings.Contains(pair[0].Payload, "messages") {
			t.Fatal("missing input")
		}
		if pair[1].Result == "success" && !strings.Contains(pair[1].Payload, "reasoning_content") {
			t.Fatal("missing raw response")
		}
		purposes[pair[0].Purpose]++
	}
	if purposes["chat"] != 2 || purposes["title"] != 1 || purposes["summary"] != 1 {
		t.Fatalf("wrong purpose routing: %v", purposes)
	}
}
