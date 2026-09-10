package agent

import (
	"aichallenge/week_1/task_1/internal/llm"
	"testing"
	"time"
)

func TestUsageSnapshotAndInterruptedAttemptRestoration(t *testing.T) {
	snapshot := Snapshot{BaseURL: "https://example.com", APIKey: "secret", Model: "model", SystemPrompt: "system", Timeout: time.Second}
	conversation, err := NewConversation(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	message, err := conversation.Begin("c1", "coffee")
	if err != nil {
		t.Fatal(err)
	}
	pending := conversation.Messages()
	restored, err := RestoreConversation(snapshot, pending)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := restored.Retry(message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(retried.Attempts) != 2 || retried.Attempts[1].ID != "m-1-a-2" {
		t.Fatalf("attempt reused: %+v", retried.Attempts)
	}
	before := restored.Messages()
	if err := restored.finishCompletion(message.ID, llm.Completion{Text: "answer", Usage: &llm.Usage{PromptTokens: 100, CompletionTokens: 20}}, nil); err != nil {
		t.Fatal(err)
	}
	if AccountedTokens(before) != 0 || AccountedTokens(restored.Messages()) != 120 {
		t.Fatal("captured snapshot changed after completion")
	}
	corrupted := restored.Messages()
	corrupted[1].Usage.PromptTokens = 101
	if _, err := RestoreConversation(snapshot, corrupted); err == nil {
		t.Fatal("mismatched persisted row/ledger accepted")
	}
}
