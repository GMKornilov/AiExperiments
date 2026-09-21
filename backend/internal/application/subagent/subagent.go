// Package subagent contains the stateless, one-call boundary used by active LLM roles.
package subagent

import (
	"context"
	"strings"

	"aichallenge/week_1/task_1/internal/application/completion"
)

type Client interface {
	Complete(context.Context, string, []completion.Message) (string, error)
}

// Run performs exactly one provider call and decodes its result. Retry and state
// ownership deliberately stay with the caller.
func Run[T any](ctx context.Context, client Client, purpose string, messages []completion.Message, decode func(string) (T, error)) (T, error) {
	var zero T
	if client == nil || strings.TrimSpace(purpose) == "" || len(messages) == 0 {
		return zero, completion.Invalid()
	}
	raw, err := client.Complete(ctx, purpose, messages)
	if err != nil {
		return zero, err
	}
	return decode(raw)
}

func Text(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", completion.Invalid()
	}
	return raw, nil
}
