package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aichallenge/week_1/task_1/internal/agent"
)

func TestOldDialogReceivesSummaryConfigurationOnRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	original := testSnapshot()
	provider := &fakeProvider{ignoreTitle: true, complete: func(context.Context) (string, error) { return "answer", nil }}
	before, err := OpenStore(provider, path, func() (agent.DialogSnapshot, error) { return original, nil })
	if err != nil {
		t.Fatal(err)
	}
	d, err := before.Create("browser", original)
	if err != nil {
		t.Fatal(err)
	}
	if d.Compression.Available {
		t.Fatal("legacy dialog unexpectedly supports summary")
	}
	if _, err := before.Send(context.Background(), "browser", d.ID, "first", "18 grams"); err != nil {
		t.Fatal(err)
	}
	before.Close()
	saved, _ := before.Get("browser", d.ID)
	current := original
	current.Chat.SystemPrompt = "new prompt must not replace old one"
	current.Summary = &agent.SummaryConfig{Snapshot: original.Text, KeepLastMessages: 3, BatchSize: 2}
	after, err := OpenStore(provider, path, func() (agent.DialogSnapshot, error) { return current, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close()
	restored, ok := after.Get("browser", d.ID)
	if !ok || !restored.Compression.Available || restored.Compression.Enabled {
		t.Fatal("legacy dialog remains unavailable or enabled itself")
	}
	restoredJSON, err := json.Marshal(restored.Messages)
	if err != nil {
		t.Fatal(err)
	}
	savedJSON, err := json.Marshal(saved.Messages)
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredJSON) != string(savedJSON) {
		t.Fatal("migration altered transcript")
	}
	if after.sessions["browser"].dialogs[d.ID].agent.Snapshot().SystemPrompt != original.Chat.SystemPrompt {
		t.Fatal("migration altered chat prompt")
	}
	enabled, err := after.SetCompression(context.Background(), "browser", d.ID, true)
	if err != nil || !enabled.Compression.Enabled {
		t.Fatalf("old dialog cannot enable compression: %v", err)
	}
}

func TestCompressionPersistsAndRemainsSessionScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	snapshot := testSnapshot()
	summary := snapshot.Text
	summary.Model, summary.APIKey = "summary-model", "summary-secret"
	snapshot.Summary = &agent.SummaryConfig{Snapshot: summary, KeepLastMessages: 2, BatchSize: 2}
	provider := &fakeProvider{ignoreTitle: true, complete: func(context.Context) (string, error) { return "saved summary", nil }}
	open := func() *Store {
		s, err := OpenStore(provider, path, func() (agent.DialogSnapshot, error) { return snapshot, nil })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		return s
	}
	s := open()
	d, err := s.Create("browser", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetCompression(context.Background(), "other", d.ID, true); err == nil {
		t.Fatal("cross-session mutation")
	}
	if _, err := s.SetCompression(context.Background(), "browser", d.ID, true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Send(context.Background(), "browser", d.ID, fmt.Sprint(i), fmt.Sprintf("unique-question-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	saved, _ := s.Get("browser", d.ID)
	if saved.Compression.CoveredMessages != 3 || saved.Compression.Summary == "" {
		t.Fatalf("missing summary: %+v", saved.Compression)
	}
	data, err := os.ReadFile(filepath.Join(dialogDirectory(path), d.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "summary-secret") {
		t.Fatal("persisted summary credential")
	}
	var disk diskChat
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	if len(disk.Dialog.Messages) != 6 || disk.AgentMessages == nil || len(*disk.AgentMessages) != 3 {
		t.Fatal("UI and agent history not separated")
	}
	for _, m := range *disk.AgentMessages {
		if strings.Contains(m.Text, "unique-question-0") || strings.Contains(m.Text, "unique-question-1") {
			t.Fatal("archive leaked into persisted agent memory")
		}
	}
	restored := open()
	after, ok := restored.Get("browser", d.ID)
	if !ok || !after.Compression.Enabled || after.Compression.Summary != saved.Compression.Summary || after.Compression.CoveredMessages != 3 || len(after.Messages) != 6 {
		t.Fatal("lost summary or original messages")
	}
	if len(restored.sessions["browser"].dialogs[d.ID].agent.Messages()) != 3 {
		t.Fatal("archive restored into agent")
	}
	off, err := restored.SetCompression(context.Background(), "browser", d.ID, false)
	if err != nil || off.Compression.Enabled || len(off.Messages) != 6 {
		t.Fatal("cannot turn compression off")
	}
}

func TestLegacySummaryPrunesOriginalHistory(t *testing.T) {
	snapshot := testSnapshot()
	c, err := agent.NewConversation(snapshot.Chat)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		m, err := c.Begin(fmt.Sprint(i), fmt.Sprintf("old-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Finish(m.ID, "answer", nil); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &agent.SummaryConfig{Snapshot: snapshot.Text, KeepLastMessages: 3, BatchSize: 2}
	if err := c.ConfigureCompression(cfg, agent.CompressionState{Summary: "memory", CoveredMessages: 3}); err != nil {
		t.Fatal(err)
	}
	if len(c.Messages()) != 3 || c.Messages()[0].ID != "m-4" {
		t.Fatal("legacy prefix not pruned")
	}
	restored, err := agent.RestoreCompactedConversation(snapshot.Chat, c.Messages(), c.Compression())
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.ConfigureCompression(cfg, c.Compression()); err != nil {
		t.Fatal(err)
	}
	m, err := restored.Begin("next", "next question")
	if err != nil || m.ID != "m-7" {
		t.Fatalf("IDs not preserved: %+v %v", m, err)
	}
	contextMessages, err := restored.Context(m.ID)
	if err != nil || len(contextMessages) != 5 || contextMessages[1].Role != "assistant" {
		t.Fatal("restored tail corrupted")
	}
}
