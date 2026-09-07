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
			writeFixture(t, cfg, nestedConfig(upstream.URL, "KEY-ONE", "one", "prompt.txt"))
			journal := observability.NewJournal(logText, nil)
			store := session.NewStore(agent.OpenAIProvider{})
			h := New(store, SnapshotLoader(cfg), journal)
			a := createDialog(t, h, "a")
			sendMessage(t, h, "a", a, "c1", "first")
			writeFixture(t, prompt, "SYSTEM TWO")
			writeFixture(t, cfg, nestedConfig(upstream.URL, "KEY-TWO", "two", "prompt.txt"))
			b := createDialog(t, h, "a")
			sendMessage(t, h, "a", a, "c2", "second")
			sendMessage(t, h, "a", b, "c3", "other")
			mu.Lock()
			got := append([]struct {
				Model    string        `json:"model"`
				Messages []llm.Message `json:"messages"`
			}{}, calls...)
			mu.Unlock()
			if len(got) != 5 {
				t.Fatalf("calls=%#v", got)
			}
			var history []llm.Message
			for _, call := range got {
				if len(call.Messages) == 4 {
					history = call.Messages
				}
			}
			if len(history) != 4 || history[0].Content != "SYSTEM SECRET-PROMPT" || history[1].Content != "first" || history[2].Role != "assistant" || history[3].Content != "second" {
				t.Fatalf("context=%#v", history)
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
	base := agent.Snapshot{BaseURL: "http://provider", APIKey: "key", Model: "model", SystemPrompt: "system", Timeout: time.Second}
	snap := agent.DialogSnapshot{Chat: base, Text: base}
	h := New(store, func() (agent.DialogSnapshot, error) { return snap, nil }, journal)
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
	if calls != 2 {
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

func TestTemperatureSnapshotsReachChatAndText(t *testing.T) {
	var mu sync.Mutex
	var calls []struct {
		Model       string        `json:"model"`
		Temperature *float64      `json:"temperature"`
		Messages    []llm.Message `json:"messages"`
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model       string        `json:"model"`
			Temperature *float64      `json:"temperature"`
			Messages    []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		calls = append(calls, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "ok"}}}})
	}))
	defer upstream.Close()
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "chat.txt"), "chat")
	writeFixture(t, filepath.Join(dir, "text.txt"), "title")
	cfg := filepath.Join(dir, "llm.yaml")
	writeFixture(t, cfg, "chat:\n  base_url: "+upstream.URL+"\n  api_key: chat-key\n  model: chat\n  temperature: 0.7\n  system_prompt_path: chat.txt\ntext:\n  base_url: "+upstream.URL+"\n  api_key: text-key\n  model: text\n  temperature: 0\n  system_prompt_path: text.txt\n")
	h := New(session.NewStore(agent.OpenAIProvider{}), SnapshotLoader(cfg), observability.NewJournal(false, nil))
	id := createDialog(t, h, "s")
	writeFixture(t, cfg, "chat:\n  base_url: "+upstream.URL+"\n  api_key: chat-key-2\n  model: chat\n  temperature: 0.3\n  system_prompt_path: chat.txt\ntext:\n  base_url: "+upstream.URL+"\n  api_key: text-key-2\n  model: text\n  temperature: 1.2\n  system_prompt_path: text.txt\n")
	newID := createDialog(t, h, "s")
	sendMessage(t, h, "s", id, "old", "old-question")
	sendMessage(t, h, "s", newID, "new", "new-question")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(calls)
		mu.Unlock()
		if n == 4 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 4 {
		t.Fatalf("calls=%v", calls)
	}
	seen := map[string]float64{}
	for _, v := range calls {
		if v.Temperature == nil {
			t.Fatalf("temperature omitted: %#v", v)
		}
		question := v.Messages[len(v.Messages)-1].Content
		seen[question+"/"+v.Model] = *v.Temperature
	}
	if seen["old-question/chat"] != 0.7 || seen["old-question/text"] != 0 || seen["new-question/chat"] != 0.3 || seen["new-question/text"] != 1.2 {
		t.Fatalf("calls=%v", calls)
	}
}

func writeFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
func nestedConfig(url, key, model, prompt string) string {
	return "chat:\n  base_url: " + url + "\n  api_key: " + key + "\n  model: " + model + "\n  system_prompt_path: " + prompt + "\ntext:\n  base_url: " + url + "\n  api_key: " + key + "\n  model: title-" + model + "\n  system_prompt_path: " + prompt + "\n"
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
