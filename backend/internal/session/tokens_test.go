package session

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
)

func TestTokenAccountingRetryRestartAndContext(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string        `json:"model"`
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if req.Model == "title-model" {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"title"}}],"usage":{"prompt_tokens":999,"completion_tokens":999}}`)
			return
		}
		n := calls.Add(1)
		switch n {
		case 1:
			fmt.Fprint(w, `{"choices":[{"message":{"content":"answer"}}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`)
		case 2:
			fmt.Fprint(w, `{"choices":[],"usage":{"prompt_tokens":150,"completion_tokens":30}}`)
		case 3:
			if len(req.Messages) != 4 || req.Messages[1].Content != "first" || req.Messages[2].Content != "answer" || req.Messages[3].Content != "second" {
				t.Errorf("lost restart context: %+v", req.Messages)
			}
			fmt.Fprint(w, `{"choices":[{"message":{"content":"retried"}}],"usage":{"prompt_tokens":150,"completion_tokens":40}}`)
		default:
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":{"code":"context_length_exceeded"}}`)
		}
	}))
	defer server.Close()
	snapshot := testSnapshot()
	snapshot.Chat.BaseURL = server.URL
	snapshot.Text.BaseURL = server.URL
	path := filepath.Join(t.TempDir(), "history.json")
	open := func() *Store {
		s, err := OpenStore(agent.OpenAIProvider{}, path, func() (agent.DialogSnapshot, error) { return snapshot, nil })
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := open()
	d, err := s.Create("browser", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Send(context.Background(), "browser", d.ID, "c1", "first")
	if err != nil || first.AccountedTokens != 120 || first.Messages[1].Usage.PromptTokens != 100 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	failed, err := s.Send(context.Background(), "browser", d.ID, "c2", "second")
	if err != nil || failed.AccountedTokens != 300 || failed.Messages[2].Status != agent.StatusError {
		t.Fatalf("failed=%+v err=%v", failed, err)
	}
	s.Close()
	s = open()
	defer s.Close()
	restored, _ := s.Get("browser", d.ID)
	if restored.AccountedTokens != 300 || calls.Load() != 2 {
		t.Fatal("restart changed accounting")
	}
	retry, err := s.Retry(context.Background(), "browser", d.ID, failed.Messages[2].ID)
	if err != nil || retry.AccountedTokens != 490 || len(retry.Messages[2].Attempts) != 2 {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
	duplicate, err := s.Send(context.Background(), "browser", d.ID, "c2", "second")
	if err != nil || duplicate.AccountedTokens != 490 || calls.Load() != 3 {
		t.Fatal("duplicate counted")
	}
	overflow, err := s.Send(context.Background(), "browser", d.ID, "c3", "third")
	if err != nil || overflow.AccountedTokens != 490 || overflow.Messages[4].ErrorCategory != "context_limit" || calls.Load() != 4 {
		t.Fatalf("overflow=%+v err=%v", overflow, err)
	}
	if _, found := s.Get("other", d.ID); found {
		t.Fatal("session leak")
	}
	// Public snapshots cannot mutate the persisted source of truth.
	retry.Messages[1].Usage.PromptTokens = 999
	retry.Messages[2].Attempts[0].Usage.PromptTokens = 999
	unchanged, _ := s.Get("browser", d.ID)
	if unchanged.AccountedTokens != 490 || unchanged.Messages[1].Usage.PromptTokens != 100 {
		t.Fatal("snapshot alias")
	}
}
