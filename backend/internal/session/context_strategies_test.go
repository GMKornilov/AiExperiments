package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
)

type contextCall struct {
	model    string
	messages []llm.Message
}

type contextProvider struct {
	mu        sync.Mutex
	calls     []contextCall
	facts     []string
	chatError int
}

func (p *contextProvider) Complete(ctx context.Context, snapshot agent.Snapshot, messages []llm.Message) (string, error) {
	completion, err := p.CompleteWithUsage(ctx, snapshot, messages)
	return completion.Text, err
}

func (p *contextProvider) CompleteWithUsage(_ context.Context, snapshot agent.Snapshot, messages []llm.Message) (llm.Completion, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, contextCall{model: snapshot.Model, messages: append([]llm.Message(nil), messages...)})
	if snapshot.Model == "title-model" {
		return llm.Completion{Text: "title"}, nil
	}
	if snapshot.Model == "facts-model" {
		if len(p.facts) == 0 {
			return llm.Completion{Text: `{}`, Usage: &llm.Usage{PromptTokens: 7, CompletionTokens: 3}}, nil
		}
		result := p.facts[0]
		p.facts = p.facts[1:]
		return llm.Completion{Text: result, Usage: &llm.Usage{PromptTokens: 7, CompletionTokens: 3}}, nil
	}
	if p.chatError > 0 {
		p.chatError--
		return llm.Completion{Usage: &llm.Usage{PromptTokens: 11, CompletionTokens: 5}}, fmt.Errorf("controlled chat failure")
	}
	return llm.Completion{Text: "answer", Usage: &llm.Usage{PromptTokens: 11, CompletionTokens: 5}}, nil
}

func (p *contextProvider) callsFor(model string) []contextCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]contextCall, 0)
	for _, call := range p.calls {
		if call.model == model {
			result = append(result, call)
		}
	}
	return result
}

func contextStrategySnapshot() agent.DialogSnapshot {
	snapshot := testSnapshot()
	snapshot.ContextWindowMessages = 3
	facts := snapshot.Chat
	facts.Model = "facts-model"
	facts.SystemPrompt = "facts system"
	snapshot.Facts = &agent.FactsConfig{Snapshot: facts}
	summary := snapshot.Chat
	summary.Model = "summary-model"
	snapshot.Summary = &agent.SummaryConfig{Snapshot: summary, KeepLastMessages: 1, BatchSize: 1}
	return snapshot
}

func TestParseFactsRejectsAnythingButOneFlatUniqueStringObject(t *testing.T) {
	t.Parallel()
	invalid := []string{
		``, `null`, `[]`, `{"a":null}`, `{"a":[]}`, `{"a":{}}`, `{"a":""}`,
		`{"a":"   "}`, `{"Bad_key":"value"}`, "```json\\n{}\\n```", `{}` + " trailing",
		`{"a":"first","a":"second"}`,
	}
	for _, payload := range invalid {
		t.Run(payload, func(t *testing.T) {
			if _, err := parseFacts(payload); err == nil {
				t.Fatalf("accepted invalid facts response %q", payload)
			}
		})
	}
	facts, err := parseFacts(`{"dose":"17 g","preference":"без молока"}`)
	if err != nil || facts["dose"] != "17 g" || len(facts) != 2 {
		t.Fatalf("valid facts rejected: %#v, %v", facts, err)
	}
}

func TestSlidingWindowSendsCurrentInputOnceAndNeverSendsArchive(t *testing.T) {
	p := &contextProvider{}
	s := NewStore(p)
	dialog, err := s.CreateWithStrategy("browser", contextStrategySnapshot(), agent.StrategySlidingWindow)
	if err != nil {
		t.Fatal(err)
	}
	for index, text := range []string{"one", "two", "three", "four"} {
		if _, err := s.Send(context.Background(), "browser", dialog.ID, fmt.Sprintf("client-%d", index), text); err != nil {
			t.Fatal(err)
		}
	}
	calls := p.callsFor("model")
	if len(calls) != 4 {
		t.Fatalf("chat call count = %d, want 4", len(calls))
	}
	last := calls[len(calls)-1].messages
	if len(last) != 4 {
		t.Fatalf("window has %d messages, want system + current + two predecessors: %#v", len(last), last)
	}
	if last[0].Role != "system" || last[1].Content != "three" || last[2].Content != "answer" || last[3].Content != "four" {
		t.Fatalf("unexpected N=3 request: %#v", last)
	}
	for _, message := range last {
		if message.Content == "one" || message.Content == "two" {
			t.Fatalf("archived message leaked into request: %#v", last)
		}
	}
}

func TestFactsCorrectionPrecedesChatAndChatRetryDoesNotReextract(t *testing.T) {
	p := &contextProvider{facts: []string{`{"dose":"18 g"}`, `{"dose":"17 g"}`}, chatError: 1}
	s := NewStore(p)
	dialog, err := s.CreateWithStrategy("browser", contextStrategySnapshot(), agent.StrategyFacts)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := s.Send(context.Background(), "browser", dialog.ID, "one", "initial dose")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Messages[0].Status != agent.StatusError || failed.Facts["dose"] != "18 g" {
		t.Fatalf("facts were not committed before chat failure: %#v", failed)
	}
	retried, err := s.Retry(context.Background(), "browser", dialog.ID, failed.Messages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Messages[0].Status != agent.StatusSuccess || len(p.callsFor("facts-model")) != 1 || len(p.callsFor("model")) != 2 {
		t.Fatalf("retry unexpectedly re-extracted facts or did not retry chat: %#v", p.calls)
	}
	updated, err := s.Send(context.Background(), "browser", dialog.ID, "two", "correction: dose 17")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Facts["dose"] != "17 g" || updated.FactsTokens != 20 {
		t.Fatalf("facts correction or usage ledger lost: %#v", updated)
	}
	chats := p.callsFor("model")
	last := chats[len(chats)-1].messages
	if len(last) < 2 || !strings.Contains(last[1].Content, `"dose":"17 g"`) {
		t.Fatalf("corrected facts were not explicitly passed before tail: %#v", last)
	}
}

func TestInvalidFactsResponseKeepsPriorSnapshotAndDoesNotCallChat(t *testing.T) {
	p := &contextProvider{facts: []string{`{"dose":"18 g"}`, `{"dose":null}`}}
	s := NewStore(p)
	dialog, err := s.CreateWithStrategy("browser", contextStrategySnapshot(), agent.StrategyFacts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), "browser", dialog.ID, "one", "first"); err != nil {
		t.Fatal(err)
	}
	failed, err := s.Send(context.Background(), "browser", dialog.ID, "two", "bad extraction")
	if err != nil {
		t.Fatal(err)
	}
	if failed.Facts["dose"] != "18 g" || failed.Messages[len(failed.Messages)-1].Status != agent.StatusError {
		t.Fatalf("invalid extraction changed facts or was accepted: %#v", failed)
	}
	if got := len(p.callsFor("facts-model")); got != 2 {
		t.Fatalf("facts calls = %d, want 2", got)
	}
	if got := len(p.callsFor("model")); got != 1 {
		t.Fatalf("invalid extraction called chat %d times", got)
	}
}

func TestFactsFailureRemainsRetryableAfterRestart(t *testing.T) {
	path := t.TempDir() + "/history.json"
	p := &contextProvider{facts: []string{`{"dose":null}`, `{"dose":"17 g"}`}}
	loader := func() (agent.DialogSnapshot, error) { return contextStrategySnapshot(), nil }
	s, err := OpenStore(p, path, loader)
	if err != nil {
		t.Fatal(err)
	}
	dialog, err := s.CreateWithStrategy("browser", contextStrategySnapshot(), agent.StrategyFacts)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := s.Send(context.Background(), "browser", dialog.ID, "one", "recoverable extraction")
	if err != nil || failed.Messages[0].Status != agent.StatusError {
		t.Fatalf("facts failure did not create retryable message: %#v, %v", failed, err)
	}
	s.Close()
	restored, err := OpenStore(p, path, loader)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got, ok := restored.Get("browser", dialog.ID)
	if !ok || got.Messages[0].Status != agent.StatusError {
		t.Fatalf("error was not restored: %#v", got)
	}
	if _, err := restored.Retry(context.Background(), "browser", dialog.ID, got.Messages[0].ID); err != nil {
		t.Fatal(err)
	}
	if len(p.callsFor("facts-model")) != 2 || len(p.callsFor("model")) != 1 {
		t.Fatalf("retry did not perform exactly extraction then chat: %#v", p.calls)
	}
}

func TestSlidingAndFactsRestartAfterPruningRetainTranscriptAndBoundedMemory(t *testing.T) {
	for _, strategy := range []agent.ContextStrategy{agent.StrategySlidingWindow, agent.StrategyFacts} {
		t.Run(string(strategy), func(t *testing.T) {
			path := t.TempDir() + "/history.json"
			provider := &contextProvider{facts: []string{
				`{"turn":"one"}`, `{"turn":"two"}`, `{"turn":"three"}`, `{"turn":"four"}`, `{"turn":"five"}`,
			}}
			loader := func() (agent.DialogSnapshot, error) { return contextStrategySnapshot(), nil }
			store, err := OpenStore(provider, path, loader)
			if err != nil {
				t.Fatal(err)
			}
			dialog, err := store.CreateWithStrategy("browser", contextStrategySnapshot(), strategy)
			if err != nil {
				t.Fatal(err)
			}
			for turn := 1; turn <= 4; turn++ {
				if _, err := store.Send(context.Background(), "browser", dialog.ID, fmt.Sprintf("turn-%d", turn), fmt.Sprintf("turn %d", turn)); err != nil {
					t.Fatal(err)
				}
			}
			store.Close()

			restored, err := OpenStore(provider, path, loader)
			if err != nil {
				t.Fatalf("OpenStore after pruned %s transcript: %v", strategy, err)
			}
			defer restored.Close()
			before, ok := restored.Get("browser", dialog.ID)
			if !ok || len(before.Messages) != 8 {
				t.Fatalf("UI transcript after restart = %#v", before.Messages)
			}
			stored := restored.sessions["browser"].dialogs[dialog.ID]
			if tail := stored.agent.Messages(); len(tail) > 3 {
				t.Fatalf("agent memory has %d messages after pruning, want <= 3", len(tail))
			}
			after, err := restored.Send(context.Background(), "browser", dialog.ID, "turn-5", "turn 5")
			if err != nil {
				t.Fatal(err)
			}
			if len(after.Messages) != 10 || after.AccountedTokens != 80 {
				t.Fatalf("post-restart UI transcript or usage = %#v", after)
			}
			if calls := provider.callsFor("model"); len(calls) != 5 {
				t.Fatalf("chat calls = %d, want exactly 5", len(calls))
			}
			if strategy == agent.StrategyFacts && (after.FactsTokens != 50 || after.FactsUsageMissing) {
				t.Fatalf("facts usage lost after restart: %#v", after)
			}
		})
	}
}

func TestSummaryCannotChangeStrategyAfterCompactEmptiesAgentMemory(t *testing.T) {
	provider := &contextProvider{}
	store := NewStore(provider)
	dialog, err := store.CreateWithStrategy("browser", contextStrategySnapshot(), agent.StrategySummary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Send(context.Background(), "browser", dialog.ID, "one", "coffee"); err != nil {
		t.Fatal(err)
	}
	compacted, err := store.Compact(context.Background(), "browser", dialog.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(compacted.Messages) != 2 {
		t.Fatalf("compact lost UI transcript: %#v", compacted.Messages)
	}
	if tail := store.sessions["browser"].dialogs[dialog.ID].agent.Messages(); len(tail) != 0 {
		t.Fatalf("expected compacted agent memory to be empty, got %#v", tail)
	}
	if _, err := store.UpdateStrategy(context.Background(), "browser", dialog.ID, agent.StrategySlidingWindow); err == nil {
		t.Fatal("strategy changed after compacted dialog had prior messages")
	}
}

func TestBranchChildrenAreIsolatedAndChargeSharedPrefixOnce(t *testing.T) {
	p := &contextProvider{}
	s := NewStore(p)
	dialog, err := s.CreateWithStrategy("browser", contextStrategySnapshot(), agent.StrategyBranching)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), "browser", dialog.ID, "root", "shared prefix"); err != nil {
		t.Fatal(err)
	}
	root, ok := s.Get("browser", dialog.ID)
	if !ok || root.ActiveBranchID == "" {
		t.Fatalf("missing main branch: %#v", root)
	}
	first, err := s.AddBranch(context.Background(), "browser", dialog.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstID := first.ActiveBranchID
	if _, err := s.Send(context.Background(), "browser", dialog.ID, "first", "first child only"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectBranch(context.Background(), "browser", dialog.ID, root.ActiveBranchID); err != nil {
		t.Fatal(err)
	}
	second, err := s.AddBranch(context.Background(), "browser", dialog.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondID := second.ActiveBranchID
	if firstID == secondID || firstID == root.ActiveBranchID {
		t.Fatalf("branch IDs are not unique: root=%q first=%q second=%q", root.ActiveBranchID, firstID, secondID)
	}
	secondResult, err := s.Send(context.Background(), "browser", dialog.ID, "second", "second child only")
	if err != nil {
		t.Fatal(err)
	}
	if secondResult.AccountedTokens != 48 { // 3 independent chat completions at synthetic 11+5.
		t.Fatalf("shared prefix was double counted: %d", secondResult.AccountedTokens)
	}
	if _, err := s.SelectBranch(context.Background(), "browser", dialog.ID, firstID); err != nil {
		t.Fatal(err)
	}
	firstResult, ok := s.Get("browser", dialog.ID)
	if !ok {
		t.Fatal("first branch disappeared")
	}
	var firstUserID string
	for _, message := range firstResult.Messages {
		if message.Text == "second child only" {
			t.Fatalf("second child leaked into first branch: %#v", firstResult.Messages)
		}
		if message.Text == "first child only" {
			firstUserID = message.ID
		}
	}
	if firstUserID == "" || !strings.Contains(firstUserID, firstID[:8]) {
		t.Fatalf("child message ID is not branch-unique: %q", firstUserID)
	}
	calls := p.callsFor("model")
	if len(calls) != 3 {
		t.Fatalf("chat calls=%d, want 3", len(calls))
	}
	last := calls[len(calls)-1].messages
	for _, message := range last {
		if message.Content == "first child only" {
			t.Fatalf("first child entered second child context: %#v", last)
		}
	}
}

func TestBranchSelectRejectsConcurrentAttemptWithoutLeakingAnswer(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	p := &contextProvider{}
	p.chatError = 0
	original := p.CompleteWithUsage
	_ = original
	// A tiny dedicated provider keeps the root response synchronous and blocks
	// only the child turn so the test observes the public busy boundary.
	blocking := &branchBlockingProvider{started: started, release: release}
	s := NewStore(blocking)
	dialog, err := s.CreateWithStrategy("browser", contextStrategySnapshot(), agent.StrategyBranching)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), "browser", dialog.ID, "root", "root"); err != nil {
		t.Fatal(err)
	}
	root, _ := s.Get("browser", dialog.ID)
	child, err := s.AddBranch(context.Background(), "browser", dialog.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, sendErr := s.Send(context.Background(), "browser", dialog.ID, "child", "in flight child")
		done <- sendErr
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("child chat did not start")
	}
	if _, err := s.SelectBranch(context.Background(), "browser", dialog.ID, root.ActiveBranchID); err == nil {
		t.Fatal("branch switch accepted while child attempt was pending")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get("browser", dialog.ID)
	if !ok || got.ActiveBranchID != child.ActiveBranchID {
		t.Fatalf("attempt answer moved active branch: %#v", got)
	}
	if !strings.Contains(strings.Join(messageTexts(got.Messages), " "), "in flight child") {
		t.Fatalf("child answer missing from source branch: %#v", got.Messages)
	}
}

func TestRestartRestoresActiveBranchWithoutSiblingTranscript(t *testing.T) {
	path := t.TempDir() + "/history.json"
	p := &contextProvider{}
	loader := func() (agent.DialogSnapshot, error) { return contextStrategySnapshot(), nil }
	s, err := OpenStore(p, path, loader)
	if err != nil {
		t.Fatal(err)
	}
	dialog, err := s.CreateWithStrategy("browser", contextStrategySnapshot(), agent.StrategyBranching)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), "browser", dialog.ID, "root", "shared"); err != nil {
		t.Fatal(err)
	}
	root, _ := s.Get("browser", dialog.ID)
	child, err := s.AddBranch(context.Background(), "browser", dialog.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), "browser", dialog.ID, "child", "only child"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	restored, err := OpenStore(p, path, loader)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	active, ok := restored.Get("browser", dialog.ID)
	if !ok || active.ActiveBranchID != child.ActiveBranchID || len(active.Branches) != 2 {
		t.Fatalf("active branch was not restored: %#v", active)
	}
	if !strings.Contains(strings.Join(messageTexts(active.Messages), " "), "only child") {
		t.Fatalf("active child transcript lost: %#v", active.Messages)
	}
	if _, err := restored.SelectBranch(context.Background(), "browser", dialog.ID, root.ActiveBranchID); err != nil {
		t.Fatal(err)
	}
	main, _ := restored.Get("browser", dialog.ID)
	if strings.Contains(strings.Join(messageTexts(main.Messages), " "), "only child") {
		t.Fatalf("sibling transcript restored into main branch: %#v", main.Messages)
	}
}

func messageTexts(messages []agent.Message) []string {
	texts := make([]string, 0, len(messages))
	for _, message := range messages {
		texts = append(texts, message.Text)
	}
	return texts
}

type branchBlockingProvider struct {
	started chan<- struct{}
	release <-chan struct{}
	calls   int
	mu      sync.Mutex
}

func (p *branchBlockingProvider) Complete(ctx context.Context, snapshot agent.Snapshot, messages []llm.Message) (string, error) {
	p.mu.Lock()
	if snapshot.Model == "title-model" {
		p.mu.Unlock()
		return "title", nil
	}
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 2 {
		p.started <- struct{}{}
		select {
		case <-p.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "answer", nil
}

func TestContextStrategySnapshotsHaveUsableTimeout(t *testing.T) {
	// Keep the fixture explicit so accidental zero timeout does not make these
	// behavioral tests hang when a provider is intentionally blocked.
	if contextStrategySnapshot().Chat.Timeout != time.Second {
		t.Fatal("unexpected test snapshot timeout")
	}
}
