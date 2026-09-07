package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
)

type fakeProvider struct {
	mu       sync.Mutex
	requests [][]llm.Message
	complete func(context.Context) (string, error)
}

func (p *fakeProvider) Complete(ctx context.Context, _ agent.Snapshot, messages []llm.Message) (string, error) {
	p.mu.Lock()
	p.requests = append(p.requests, append([]llm.Message(nil), messages...))
	p.mu.Unlock()
	return p.complete(ctx)
}

func testSnapshot() agent.Snapshot {
	return agent.Snapshot{BaseURL: "https://llm.example", APIKey: "secret", Model: "model", SystemPrompt: "Ты бариста", Timeout: time.Second}
}

func TestStoreBuildsHistoryAndDeduplicatesClientID(t *testing.T) {
	provider := &fakeProvider{complete: func(context.Context) (string, error) { return "answer", nil }}
	store := NewStore(provider)
	dialog, err := store.Create("browser", testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Send(context.Background(), "browser", dialog.ID, "c1", "Первый вопрос")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Send(context.Background(), "browser", dialog.ID, "c2", "Второй вопрос")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := store.Send(context.Background(), "browser", dialog.ID, "c2", "изменённый текст")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Messages) != 2 || len(second.Messages) != 4 || len(duplicate.Messages) != 4 {
		t.Fatalf("history lengths = %d, %d, %d", len(first.Messages), len(second.Messages), len(duplicate.Messages))
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.requests))
	}
	got := provider.requests[1]
	if len(got) != 4 || got[0].Role != "system" || got[1].Content != "Первый вопрос" || got[2].Role != "assistant" || got[3].Content != "Второй вопрос" {
		t.Fatalf("unexpected context: %#v", got)
	}
}

func TestStoreRetryKeepsOneUserMessage(t *testing.T) {
	calls := 0
	provider := &fakeProvider{complete: func(context.Context) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("offline")
		}
		return "готово", nil
	}}
	store := NewStore(provider)
	dialog, _ := store.Create("browser", testSnapshot())
	failed, err := store.Send(context.Background(), "browser", dialog.ID, "c1", "кофе")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Messages[0].Status != agent.StatusError || failed.Messages[0].ErrorCategory != "provider" {
		t.Fatalf("failure = %#v", failed.Messages[0])
	}
	retried, err := store.Retry(context.Background(), "browser", dialog.ID, failed.Messages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(retried.Messages) != 2 || retried.Messages[0].Status != agent.StatusSuccess || retried.Messages[1].Role != "assistant" {
		t.Fatalf("retry result = %#v", retried.Messages)
	}
}

func TestDeletePendingDialogReleasesSessionLockAndIgnoresLateAnswer(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls int
	var callsMu sync.Mutex
	provider := &fakeProvider{complete: func(context.Context) (string, error) {
		callsMu.Lock()
		calls++
		call := calls
		callsMu.Unlock()
		if call > 1 {
			return "second", nil
		}
		close(started)
		<-release // Deliberately ignore cancellation, like an uncooperative upstream.
		return "late", nil
	}}
	store := NewStore(provider)
	first, _ := store.Create("browser", testSnapshot())
	second, _ := store.Create("browser", testSnapshot())
	done := make(chan error, 1)
	go func() {
		_, err := store.Send(context.Background(), "browser", first.ID, "c1", "ожидание")
		done <- err
	}()
	<-started
	if !store.Delete("browser", first.ID) {
		t.Fatal("Delete() = false")
	}
	if _, err := store.Send(context.Background(), "browser", second.ID, "c2", "новый запрос"); err != nil {
		t.Fatalf("new request after delete: %v", err)
	}
	close(release)
	if err := <-done; err == nil {
		t.Fatal("deleted pending request returned nil error")
	}
	if _, found := store.Get("browser", first.ID); found {
		t.Fatal("deleted dialog was restored")
	}
}
