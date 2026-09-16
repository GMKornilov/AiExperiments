package memory

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
)

type scriptedProvider struct {
	mu          sync.Mutex
	calls       [][]llm.Message
	answers     []string
	err         error
	mainErr     bool
	block       chan struct{}
	titleCalls  int
	titleAnswer string
	titleErr    error
	titleBlock  chan struct{}
}

func (p *scriptedProvider) reset(answers []string, err error, mainErr bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = nil
	p.answers = answers
	p.err = err
	p.mainErr = mainErr
}

func (p *scriptedProvider) Complete(_ context.Context, snap agent.Snapshot, m []llm.Message) (string, error) {
	if snap.Model == "text" {
		p.mu.Lock()
		p.titleCalls++
		answer, err, block := p.titleAnswer, p.titleErr, p.titleBlock
		p.mu.Unlock()
		if block != nil {
			<-block
		}
		if answer == "" {
			answer = "Заголовок"
		}
		return answer, err
	}
	p.mu.Lock()
	p.calls = append(p.calls, append([]llm.Message{}, m...))
	i := len(p.calls) - 1
	p.mu.Unlock()
	if p.block != nil && i == 1 {
		<-p.block
	}
	if p.err != nil && (i == 1 || (p.mainErr && i == 0)) {
		return "", p.err
	}
	if i < len(p.answers) {
		return p.answers[i], nil
	}
	return "", nil
}
func snapshot() agent.DialogSnapshot {
	return agent.DialogSnapshot{Chat: agent.Snapshot{BaseURL: "https://example.test", APIKey: "key", Model: "chat", SystemPrompt: "BASE", Timeout: time.Second, Temperature: 1}, Text: agent.Snapshot{BaseURL: "https://example.test", APIKey: "key", Model: "text", SystemPrompt: "TEXT", Timeout: time.Second, Temperature: 1}, Memory: &agent.FactsConfig{Snapshot: agent.Snapshot{BaseURL: "https://example.test", APIKey: "key", Model: "memory", SystemPrompt: "EXTRACT", Timeout: time.Second, Temperature: 0}}, ContextWindowMessages: 3}
}
func setup(t *testing.T, p *scriptedProvider) (*Store, string, string, string) {
	t.Helper()
	s, e := New(p, snapshot())
	if e != nil {
		t.Fatal(e)
	}
	pr, e := s.CreateProject("a", "P")
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.CreateChat("a", pr.ID, "C")
	if e != nil {
		t.Fatal(e)
	}
	return s, "a", pr.ID, c.ID
}
func TestSendWaitsForExtractorAndUsesMemoryPrompt(t *testing.T) {
	p := &scriptedProvider{answers: []string{"answer", `{"global_facts":["global grinder"],"project_facts":["project beans"]}`}, block: make(chan struct{})}
	s, sid, pid, cid := setup(t, p)
	done := make(chan error, 1)
	go func() { _, e := s.Send(context.Background(), sid, pid, cid, "one", "I own a grinder"); done <- e }()
	deadline := time.After(time.Second)
	for {
		p.mu.Lock()
		n := len(p.calls)
		p.mu.Unlock()
		if n == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("extractor was not called")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if _, ok := s.GetChat(sid, pid, cid); !ok {
		t.Fatal("chat absent")
	}
	close(p.block)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	m, ok := s.ReadMemory(sid, pid)
	if !ok || len(m.GlobalFacts) != 1 || m.Status != "success" {
		t.Fatalf("memory=%+v", m)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls[0][0].Content == "BASE" {
		t.Fatal("prompt was not rebuilt")
	}
	if p.calls[0][0].Role != "system" || p.calls[0][1].Content != "I own a grinder" {
		t.Fatalf("main prompt=%+v", p.calls[0])
	}
}
func TestExtractorFailurePreservesSnapshotsAndPersistsPair(t *testing.T) {
	p := &scriptedProvider{answers: []string{"first", `{"global_facts":["g"],"project_facts":["p"]}`}}
	s, sid, pid, cid := setup(t, p)
	if _, e := s.Send(context.Background(), sid, pid, cid, "1", "beans"); e != nil {
		t.Fatal(e)
	}
	p.reset([]string{"second"}, errors.New("down"), false)
	c, e := s.Send(context.Background(), sid, pid, cid, "2", "more beans")
	if e != nil {
		t.Fatal(e)
	}
	if c.MemoryStatus != "error" {
		t.Fatalf("chat=%+v", c)
	}
	m, _ := s.ReadMemory(sid, pid)
	if len(m.GlobalFacts) != 1 || m.GlobalFacts[0] != "g" || m.Status != "error" {
		t.Fatalf("memory=%+v", m)
	}
	if len(c.Messages) != 4 {
		t.Fatalf("messages=%d", len(c.Messages))
	}
	if c.Messages[3].Text != "second" {
		t.Fatalf("assistant=%q", c.Messages[3].Text)
	}
}
func TestIsolationClearDeleteAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	p := &scriptedProvider{answers: []string{"a", `{"global_facts":["g"],"project_facts":["p"]}`}}
	s, e := New(p, snapshot())
	if e != nil {
		t.Fatal(e)
	}
	s.path = path
	pr, _ := s.CreateProject("one", "one")
	c, _ := s.CreateChat("one", pr.ID, "")
	if _, e = s.Send(context.Background(), "one", pr.ID, c.ID, "x", "beans"); e != nil {
		t.Fatal(e)
	}
	other, _ := s.CreateProject("two", "two")
	if _, ok := s.ReadMemory("two", pr.ID); ok {
		t.Fatal("cross-session leak")
	}
	if e = s.ClearGlobal("one"); e != nil {
		t.Fatal(e)
	}
	m, _ := s.ReadMemory("one", pr.ID)
	if len(m.GlobalFacts) != 0 || len(m.ProjectFacts) != 1 {
		t.Fatalf("clear=%+v", m)
	}
	if e = s.DeleteProject("one", pr.ID); e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(p, path, func() (agent.DialogSnapshot, error) { return snapshot(), nil })
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := reopened.GetProject("one", pr.ID); ok {
		t.Fatal("deleted project restored")
	}
	if _, ok := reopened.GetProject("two", other.ID); !ok {
		t.Fatal("other project missing")
	}
}

func TestMainFailurePersistsRetryableUserAndRetryDoesNotDuplicate(t *testing.T) {
	p := &scriptedProvider{answers: []string{"ignored"}, err: errors.New("main down"), mainErr: true}
	s, sid, pid, cid := setup(t, p)
	if _, err := s.Send(context.Background(), sid, pid, cid, "one", "beans"); err == nil {
		t.Fatal("expected main error")
	}
	c, ok := s.GetChat(sid, pid, cid)
	if !ok || len(c.Messages) != 1 || c.Messages[0].Status != "error" {
		t.Fatalf("chat=%+v", c)
	}
	p.reset([]string{"answer", `{"global_facts":[],"project_facts":[]}`}, nil, false)
	updated, err := s.Retry(context.Background(), sid, pid, cid, c.Messages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Messages) != 2 || updated.Messages[0].Role != "user" || updated.Messages[1].Role != "assistant" {
		t.Fatalf("retry=%+v", updated.Messages)
	}
	if updated.Messages[1].Text != "answer" {
		t.Fatalf("answer=%q", updated.Messages[1].Text)
	}
}

func TestInvalidExtractorJSONKeepsFactsAndReportsCategory(t *testing.T) {
	p := &scriptedProvider{answers: []string{"a", `{"global_facts":["g"],"project_facts":[]}`}}
	s, sid, pid, cid := setup(t, p)
	if _, err := s.Send(context.Background(), sid, pid, cid, "1", "beans"); err != nil {
		t.Fatal(err)
	}
	p.reset([]string{"b", "not json"}, nil, false)
	c, err := s.Send(context.Background(), sid, pid, cid, "2", "more")
	if err != nil {
		t.Fatal(err)
	}
	if c.MemoryErrorCategory != "invalid_response" {
		t.Fatalf("category=%q", c.MemoryErrorCategory)
	}
	m, _ := s.ReadMemory(sid, pid)
	if len(m.GlobalFacts) != 1 || m.GlobalFacts[0] != "g" {
		t.Fatalf("facts=%+v", m)
	}
}

func TestTitleRunsOnceAfterFirstSuccessfulPair(t *testing.T) {
	p := &scriptedProvider{answers: []string{"a", `{"global_facts":[],"project_facts":[]}`}}
	s, sid, pid, cid := setup(t, p)
	first, err := s.Send(context.Background(), sid, pid, cid, "one", "Первый запрос")
	if err != nil {
		t.Fatal(err)
	}
	if first.TitleStatus != "pending" && first.TitleStatus != "success" {
		t.Fatalf("status=%q", first.TitleStatus)
	}
	deadline := time.Now().Add(time.Second)
	for {
		c, _ := s.GetChat(sid, pid, cid)
		if c.TitleStatus == "success" {
			if c.Title != "Заголовок" {
				t.Fatalf("title=%q", c.Title)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("title did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	p.reset([]string{"b", `{"global_facts":[],"project_facts":[]}`}, nil, false)
	if _, err = s.Send(context.Background(), sid, pid, cid, "two", "Второй запрос"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	p.mu.Lock()
	calls := p.titleCalls
	p.mu.Unlock()
	if calls != 1 {
		t.Fatalf("title calls=%d", calls)
	}
}

func TestTitleDoesNotDelaySuccessfulResponse(t *testing.T) {
	p := &scriptedProvider{answers: []string{"a", `{"global_facts":[],"project_facts":[]}`}, titleBlock: make(chan struct{})}
	s, sid, pid, cid := setup(t, p)
	done := make(chan Chat, 1)
	go func() {
		c, err := s.Send(context.Background(), sid, pid, cid, "one", "beans")
		if err != nil {
			t.Error(err)
			return
		}
		done <- c
	}()
	select {
	case c := <-done:
		if c.TitleStatus != "pending" {
			t.Fatalf("status=%q", c.TitleStatus)
		}
	case <-time.After(time.Second):
		t.Fatal("send waited for title")
	}
	close(p.titleBlock)
}

func TestTitleFallbackUnicodeAndProjectRenamePersistence(t *testing.T) {
	p := &scriptedProvider{answers: []string{"a", `{"global_facts":["g"],"project_facts":["p"]}`}}
	s, sid, pid, cid := setup(t, p)
	s.path = filepath.Join(t.TempDir(), "state.json")
	p.mu.Lock()
	p.titleAnswer = "bad\ntitle"
	p.mu.Unlock()
	p.reset([]string{"a", `{"global_facts":[],"project_facts":[]}`}, nil, false)
	c2, err := s.CreateChat(sid, pid, "another")
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Repeat("😀", 61)
	if _, err = s.Send(context.Background(), sid, pid, c2.ID, "title", ""+input); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		c, _ := s.GetChat(sid, pid, c2.ID)
		if c.TitleStatus == "fallback" {
			if runeCount(c.Title) != 60 {
				t.Fatalf("fallback runes=%d", runeCount(c.Title))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("title fallback timeout")
		}
		time.Sleep(time.Millisecond)
	}
	_ = cid
	before, _ := s.ReadMemory(sid, pid)
	renamed, err := s.RenameProject(sid, pid, "  Новый проект  ")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Title != "Новый проект" {
		t.Fatalf("title=%q", renamed.Title)
	}
	after, _ := s.ReadMemory(sid, pid)
	if len(after.GlobalFacts) != len(before.GlobalFacts) || len(after.ProjectFacts) != len(before.ProjectFacts) {
		t.Fatal("rename mutated memory")
	}
	if _, err = s.RenameProject(sid, pid, strings.Repeat("я", 101)); err == nil {
		t.Fatal("expected long title validation")
	}
	if _, err = s.RenameProject("other", pid, "x"); err == nil {
		t.Fatal("expected isolation")
	}
	reopened, err := Open(p, s.path, func() (agent.DialogSnapshot, error) { return snapshot(), nil })
	if err != nil {
		t.Fatal(err)
	}
	persisted, ok := reopened.GetProject(sid, pid)
	if !ok || persisted.Title != "Новый проект" {
		t.Fatalf("project=%+v", persisted)
	}
}

func TestOpenConvertsPendingTitleToFallbackWithoutRerun(t *testing.T) {
	p := &scriptedProvider{}
	s, sid, pid, cid := setup(t, p)
	s.path = filepath.Join(t.TempDir(), "state.json")
	s.mu.Lock()
	c := s.sessions[sid].Projects[pid].Chats[cid]
	c.Messages = []Message{{ID: "u", ClientID: "one", Role: "user", Text: "Первое сообщение", Status: "success", CreatedAt: time.Now()}}
	c.TitleStatus = "pending"
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	reopened, err := Open(p, s.path, func() (agent.DialogSnapshot, error) { return snapshot(), nil })
	if err != nil {
		t.Fatal(err)
	}
	restored, ok := reopened.GetChat(sid, pid, cid)
	if !ok || restored.TitleStatus != "fallback" || restored.Title != "Первое сообщение" {
		t.Fatalf("chat=%+v", restored)
	}
	p.mu.Lock()
	calls := p.titleCalls
	p.mu.Unlock()
	if calls != 0 {
		t.Fatalf("title rerun=%d", calls)
	}
}
