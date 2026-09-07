package agent

import (
	"context"
	"errors"

	"aichallenge/week_1/task_1/internal/llm"
)

// OpenAIProvider calls the OpenAI-compatible endpoint stored in each snapshot.
// A client is created for each call to preserve snapshot credentials and timeout.
type OpenAIProvider struct{}

func (OpenAIProvider) Complete(ctx context.Context, snapshot Snapshot, messages []llm.Message) (string, error) {
	answer, err := llm.NewClient(snapshot.BaseURL, snapshot.APIKey, snapshot.Timeout).ChatMessages(ctx, snapshot.Model, messages)
	if err != nil {
		var upstream *llm.Error
		if errors.As(err, &upstream) {
			return "", &AttemptError{Category: ErrorCategory(upstream.Kind), Err: err}
		}
		return "", &AttemptError{Category: ErrorProvider, Err: err}
	}
	return answer, nil
}
