package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
	"aichallenge/week_1/task_1/internal/observability"
	"aichallenge/week_1/task_1/internal/session"
)

type blockingProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	calls   int
	mu      sync.Mutex
}

func (p *blockingProvider) Complete(ctx context.Context, _ agent.Snapshot, _ []llm.Message) (string, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	p.once.Do(func() { close(p.started) })
	select {
	case <-p.release:
		return "done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestHTTPConversationContract(t *testing.T) {
	for _, logText := range []bool{false, true} {
		t.Run(map[bool]string{false: "hidden", true: "visible"}[logText], func(t *testing.T) {
			var mu sync.Mutex
			var calls []struct {
				Model    string        `json:"model"`
				Messages []llm.Message `json:"messages"`
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var c struct {
					Model    string        `json:"model"`
					Messages []llm.Message `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				calls = append(calls, c)
				mu.Unlock()
				_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "answer-" + c.Model}}}})
			}))
			defer upstream.Close()
			dir := t.TempDir()
			prompt := filepath.Join(dir, "prompt.txt")
			cfg := filepath.Join(dir, "llm.yaml")
			writeFixture(t, prompt, "SYSTEM SECRET-PROMPT")
			writeFixture(t, cfg, "base_url: "+upstream.URL+"\napi_key: KEY-ONE\nmodel: one\nsystem_prompt_path: prompt.txt\n")
			journal := observability.NewJournal(logText, nil)
			store := session.NewStore(agent.OpenAIProvider{})
			h := New(store, SnapshotLoader(cfg), journal)
			a := createDialog(t, h, "a")
			sendMessage(t, h, "a", a, "c1", "first")
			writeFixture(t, prompt, "SYSTEM TWO")
			writeFixture(t, cfg, "base_url: "+upstream.URL+"\napi_key: KEY-TWO\nmodel: two\nsystem_prompt_path: prompt.txt\n")
			b := createDialog(t, h, "a")
			sendMessage(t, h, "a", a, "c2", "second")
			sendMessage(t, h, "a", b, "c3", "other")
			mu.Lock()
			got := append([]struct {
				Model    string        `json:"model"`
				Messages []llm.Message `json:"messages"`
			}{}, calls...)
			mu.Unlock()
			if len(got) != 3 || got[0].Model != "one" || got[1].Model != "one" || got[2].Model != "two" {
				t.Fatalf("calls=%#v", got)
			}
			if len(got[1].Messages) != 4 || got[1].Messages[0].Content != "SYSTEM SECRET-PROMPT" || got[1].Messages[1].Content != "first" || got[1].Messages[2].Role != "assistant" || got[1].Messages[3].Content != "second" {
				t.Fatalf("context=%#v", got[1].Messages)
			}
			writeFixture(t, cfg, "bad: [")
			request := httptest.NewRequest(http.MethodPost, "/api/dialogs", nil)
			request.Header.Set("X-Session-ID", "a")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, request)
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("bad config status=%d", w.Code)
			}
			sendMessage(t, h, "a", a, "c4", "still-old")
			logs := journal.Logs(a)
			raw, _ := json.Marshal(logs)
			if bytes.Contains(raw, []byte("KEY-ONE")) || bytes.Contains(raw, []byte("SYSTEM SECRET-PROMPT")) {
				t.Fatalf("private log=%s", raw)
			}
			if logText && !bytes.Contains(raw, []byte("first")) {
				t.Fatalf("message missing %s", raw)
			}
			if !logText && bytes.Contains(raw, []byte("first")) {
				t.Fatalf("message leaked %s", raw)
			}
		})
	}
}

func TestAcceptedDisconnectReconcilesWithoutDuplicate(t *testing.T) {
	p := &blockingProvider{started: make(chan struct{}), release: make(chan struct{})}
	store := session.NewStore(p)
	journal := observability.NewJournal(false, nil)
	snap := agent.Snapshot{BaseURL: "http://provider", APIKey: "key", Model: "model", SystemPrompt: "system", Timeout: time.Second}
	h := New(store, func() (agent.Snapshot, error) { return snap, nil }, journal)
	id := createDialog(t, h, "s")
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodPost, "/api/dialogs/"+id+"/messages", bytes.NewBufferString(`{"client_message_id":"same","text":"hello"}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Session-ID", "s")
	r.Header.Set("X-Request-ID", "1234567890abcdef1234567890abcdef")
	done := make(chan struct{})
	go func() { h.ServeHTTP(httptest.NewRecorder(), r); close(done) }()
	<-p.started
	cancel()
	if d, ok := store.Get("s", id); !ok || len(d.Messages) != 1 || d.Messages[0].Status != agent.StatusPending {
		t.Fatalf("pending=%#v", d)
	}
	dup := httptest.NewRequest(http.MethodPost, "/api/dialogs/"+id+"/messages", bytes.NewBufferString(`{"client_message_id":"same","text":"hello"}`))
	dup.Header.Set("Content-Type", "application/json")
	dup.Header.Set("X-Session-ID", "s")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, dup)
	if w.Code != http.StatusOK {
		t.Fatalf("dedup=%d %s", w.Code, w.Body.String())
	}
	close(p.release)
	<-done
	d, _ := store.Get("s", id)
	if len(d.Messages) != 2 || d.Messages[1].Status != agent.StatusSuccess {
		t.Fatalf("final=%#v", d.Messages)
	}
	p.mu.Lock()
	calls := p.calls
	p.mu.Unlock()
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	logs := journal.Logs(id)
	starts, finishes := 0, 0
	for _, l := range logs {
		if l.Event == "llm_start" {
			starts++
			if l.CorrelationID != "1234567890abcdef1234567890abcdef" {
				t.Fatal(l.CorrelationID)
			}
		}
		if l.Event == "llm_finish" {
			finishes++
		}
	}
	if starts != 1 || finishes != 1 {
		t.Fatalf("logs=%#v", logs)
	}
}

func writeFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
func createDialog(t *testing.T, h http.Handler, sid string) string {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/dialogs", nil)
	r.Header.Set("X-Session-ID", sid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var d struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d.ID
}
func sendMessage(t *testing.T, h http.Handler, sid, id, cid, text string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/dialogs/"+id+"/messages", bytes.NewBufferString(`{"client_message_id":"`+cid+`","text":"`+text+`"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Session-ID", sid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("send %d %s", w.Code, w.Body.String())
	}
}
