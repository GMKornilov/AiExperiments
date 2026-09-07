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

type titleProvider struct {
	mu                    sync.Mutex
	chatCalls, titleCalls int
	titleStarted          chan struct{}
	titleRelease          chan struct{}
	titleErr              error
}

func (p *titleProvider) Complete(ctx context.Context, s agent.Snapshot, _ []llm.Message) (string, error) {
	if s.Model == "text" {
		p.mu.Lock()
		p.titleCalls++
		p.mu.Unlock()
		select {
		case p.titleStarted <- struct{}{}:
		default:
			{
			}
		}
		if p.titleRelease != nil {
			select {
			case <-p.titleRelease:
				return "Название\n диалога", p.titleErr
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return "Название", p.titleErr
	}
	p.mu.Lock()
	p.chatCalls++
	p.mu.Unlock()
	return "chat", nil
}
func titleSnapshot() agent.DialogSnapshot {
	return agent.DialogSnapshot{Chat: agent.Snapshot{BaseURL: "https://chat", APIKey: "chat-key", Model: "chat", SystemPrompt: "chat prompt", Timeout: time.Second}, Text: agent.Snapshot{BaseURL: "https://text", APIKey: "text-key", Model: "text", SystemPrompt: "title prompt", Timeout: time.Second}}
}

func TestSlowTitleDoesNotBlockChatOrSecondMessage(t *testing.T) {
	p := &titleProvider{titleStarted: make(chan struct{}, 1), titleRelease: make(chan struct{})}
	s := NewStore(p)
	d, _ := s.Create("s", titleSnapshot())
	if _, err := s.Send(context.Background(), "s", d.ID, "1", "first"); err != nil {
		t.Fatal(err)
	}
	<-p.titleStarted
	if _, err := s.Send(context.Background(), "s", d.ID, "2", "second"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	if p.chatCalls != 2 {
		t.Fatalf("chat=%d", p.chatCalls)
	}
	p.mu.Unlock()
	close(p.titleRelease)
}

func TestTitleGeneratedOnceAndFailureKeepsFallback(t *testing.T) {
	p := &titleProvider{titleStarted: make(chan struct{}, 1), titleErr: errors.New("down")}
	s := NewStore(p)
	d, _ := s.Create("s", titleSnapshot())
	got, err := s.Send(context.Background(), "s", d.ID, "1", "first title")
	if err != nil {
		t.Fatal(err)
	}
	<-p.titleStarted
	deadline := time.Now().Add(time.Second)
	for got.TitleStatus == "pending" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		got, _ = s.Get("s", d.ID)
	}
	if got.TitleStatus != "error" || got.Title != "first title" {
		t.Fatalf("dialog=%#v", got)
	}
	_, _ = s.Send(context.Background(), "s", d.ID, "1", "first title")
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.titleCalls != 1 {
		t.Fatalf("title calls=%d", p.titleCalls)
	}
}

func TestDeleteAndCloseCancelTitleWithoutResurrection(t *testing.T) {
	for _, closeStore := range []bool{false, true} {
		p := &titleProvider{titleStarted: make(chan struct{}, 1), titleRelease: make(chan struct{})}
		s := NewStore(p)
		d, _ := s.Create("s", titleSnapshot())
		go s.Send(context.Background(), "s", d.ID, "1", "first")
		<-p.titleStarted
		if closeStore {
			s.Close()
		} else {
			s.Delete("s", d.ID)
		}
		close(p.titleRelease)
		time.Sleep(10 * time.Millisecond)
		if !closeStore {
			if _, ok := s.Get("s", d.ID); ok {
				t.Fatal("resurrected")
			}
		}
	}
}

func TestTitleTimeoutReportsFinishWithoutCancellation(t *testing.T) {
	p := &titleProvider{titleStarted: make(chan struct{}, 1)}
	p.titleRelease = make(chan struct{})
	snapshot := titleSnapshot()
	snapshot.Text.Timeout = 10 * time.Millisecond
	s := NewStore(p)
	finished := make(chan string, 1)
	cancelled := make(chan struct{}, 1)
	s.SetAttemptObserver(AttemptObserver{
		TitleFinished: func(_ context.Context, _ string, _ string, _ time.Duration, _ Dialog, category string) {
			finished <- category
		},
		TitleCancelled: func(context.Context, string, string) { cancelled <- struct{}{} },
	})
	d, _ := s.Create("s", snapshot)
	if _, err := s.Send(context.Background(), "s", d.ID, "1", "first"); err != nil {
		t.Fatal(err)
	}
	select {
	case category := <-finished:
		if category != "timeout" {
			t.Fatalf("category=%q", category)
		}
	case <-time.After(time.Second):
		t.Fatal("title finish timeout")
	}
	select {
	case <-cancelled:
		t.Fatal("timeout classified as cancellation")
	default:
	}
	got, ok := s.Get("s", d.ID)
	if !ok || got.TitleStatus != "error" || got.Title != "first" {
		t.Fatalf("dialog=%#v", got)
	}
}
