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
	result, err := (OpenAIProvider{}).CompleteWithUsage(ctx, snapshot, messages)
	return result.Text, err
}
func (OpenAIProvider) CompleteWithUsage(ctx context.Context, snapshot Snapshot, messages []llm.Message) (llm.Completion, error) {
	answer, err := llm.NewClient(snapshot.BaseURL, snapshot.APIKey, snapshot.Timeout).ChatCompletion(ctx, snapshot.Model, messages, snapshot.Temperature)
	if err != nil {
		var upstream *llm.Error
		if errors.As(err, &upstream) {
			return answer, &AttemptError{Category: ErrorCategory(upstream.Kind), Err: err}
		}
		return answer, &AttemptError{Category: ErrorProvider, Err: err}
	}
	return answer, nil
}
