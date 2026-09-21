package openai

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/application/completion"
	"aichallenge/week_1/task_1/internal/llm"
)

// Client adapts the existing transport; each purpose has an independent timeout.
type Client struct {
	provider agent.Provider
	snapshot agent.DialogSnapshot
}

func New(provider agent.Provider, snapshot agent.DialogSnapshot) (*Client, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	if snapshot.Memory == nil {
		return nil, errors.New("memory extractor не настроен")
	}
	if snapshot.InvariantValidation == nil {
		return nil, errors.New("invariant validation не настроена")
	}
	return &Client{provider: provider, snapshot: snapshot}, nil
}
func (c *Client) Complete(ctx context.Context, purpose string, messages []completion.Message) (answer string, err error) {
	snap := c.snapshot.Chat
	switch purpose {
	case "title":
		snap = c.snapshot.Text
	case "memory_extractor":
		snap = c.snapshot.Memory.Snapshot
	case "invariant_equipment-availability", "invariant_beans-availability", "invariant_inventory-truth":
		snap = c.snapshot.InvariantValidation.Snapshot
	}
	call, cancel := context.WithTimeout(llm.WithPurpose(ctx, purpose), snap.Timeout)
	defer cancel()
	started := time.Now()
	defer func() {
		result, category := "success", ""
		if err != nil {
			result = "failure"
			category = completion.Category(err)
		}
		slog.Info("barista.completion", "source", "backend", "event", "completion", "purpose", purpose, "result", result, "error_category", category, "correlation_id", llm.RequestID(ctx), "duration_ms", time.Since(started).Milliseconds())
	}()
	input := make([]llm.Message, len(messages))
	for i, m := range messages {
		input[i] = llm.Message{Role: m.Role, Content: m.Content}
	}
	answer, err = c.provider.Complete(call, snap, input)
	if call.Err() != nil {
		err = call.Err()
	}
	if err == nil {
		return answer, nil
	}
	category := completion.Category(err)
	var old *agent.AttemptError
	if errors.As(err, &old) {
		category = string(old.Category)
	}
	return "", &completion.Error{Category: category, Cause: err}
}
