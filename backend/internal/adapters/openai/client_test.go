package openai

import (
	"context"
	"sync"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/application/completion"
	"aichallenge/week_1/task_1/internal/llm"
)

type snapshotProvider struct {
	mu        sync.Mutex
	calls     int
	snapshots []agent.Snapshot
}

func (p *snapshotProvider) Complete(_ context.Context, snapshot agent.Snapshot, _ []llm.Message) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.snapshots = append(p.snapshots, snapshot)
	return `{"status":"allow"}`, nil
}

func TestInvariantValidationUsesDedicatedSixtySecondSnapshotOnce(t *testing.T) {
	chat := agent.Snapshot{BaseURL: "https://example.test", APIKey: "key", Model: "chat", SystemPrompt: "chat", Timeout: 30 * time.Second, Temperature: 1}
	validator := chat
	validator.Model = "validator"
	validator.SystemPrompt = "validator"
	validator.Timeout = 60 * time.Second
	provider := &snapshotProvider{}
	client, err := New(provider, agent.DialogSnapshot{
		Chat:                chat,
		Text:                chat,
		Memory:              &agent.FactsConfig{Snapshot: chat},
		InvariantValidation: &agent.FactsConfig{Snapshot: validator},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(context.Background(), "invariant_research-sufficiency", []completion.Message{{Role: "user", Content: "candidate"}}); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls=%d, want 1", provider.calls)
	}
	if got, want := provider.snapshots[0].Timeout, 60*time.Second; got != want {
		t.Fatalf("validator timeout=%s, want %s", got, want)
	}
}
