package conversation

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"aichallenge/week_1/task_1/internal/application/completion"
	"aichallenge/week_1/task_1/internal/domain/model"
)

type CompletionClient interface {
	Complete(context.Context, string, []completion.Message) (string, error)
}
type TitleState interface {
	Update(func(*model.State) error) error
	AttachTitle(string, string, string, context.CancelFunc)
}
type Titles struct {
	state  TitleState
	client CompletionClient
	prompt string
	now    func() time.Time
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func NewTitles(state TitleState, client CompletionClient, prompt string, now func() time.Time) *Titles {
	return &Titles{state: state, client: client, prompt: prompt, now: now}
}

// ClaimTitle runs inside the input acceptance transaction.
func ClaimTitle(c *model.ChatState, input string) bool {
	if c.TitleStatus != "idle" {
		return false
	}
	c.TitleStatus = "pending"
	c.Title = model.TitleFallback(input)
	return true
}
func (t *Titles) Start(ctx context.Context, sid, pid, cid, input string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.wg.Add(1)
	call, cancel := context.WithCancel(context.WithoutCancel(ctx))
	t.state.AttachTitle(sid, pid, cid, cancel)
	go func() {
		defer t.wg.Done()
		defer cancel()
		started := time.Now()
		raw, err := t.client.Complete(call, "title", []completion.Message{{Role: "system", Content: t.prompt}, {Role: "user", Content: input}})
		title, valid := model.ValidTitle(raw)
		status := "success"
		if err != nil || !valid {
			title = model.TitleFallback(input)
			status = "fallback"
		}
		if call.Err() != nil {
			return
		}
		saveErr := t.state.Update(func(root *model.State) error {
			c := root.Chat(sid, pid, cid)
			if c == nil || c.TitleStatus != "pending" {
				return model.ErrNotFound
			}
			c.Title = title
			c.TitleStatus = status
			c.UpdatedAt = t.now()
			root.Project(sid, pid).UpdatedAt = c.UpdatedAt
			return nil
		})
		result, category := status, ""
		if saveErr != nil {
			result = "failure"
			category = "storage"
		} else if err != nil {
			category = completion.Category(err)
		} else if !valid {
			category = "invalid_response"
		}
		slog.Info("barista.chat_title", "source", "backend", "event", "chat_title", "result", result, "error_category", category, "correlation_id", completion.RequestID(ctx), "project_id", pid, "chat_id", cid, "duration_ms", time.Since(started).Milliseconds())
	}()
}
func (t *Titles) Close() { t.mu.Lock(); t.closed = true; t.mu.Unlock(); t.wg.Wait() }
