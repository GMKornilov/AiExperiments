package completion

import (
	"context"
	"errors"

	"aichallenge/week_1/task_1/internal/domain/model"
)

type Message struct {
	Role    string
	Content string
}
type Facts struct {
	GlobalFacts  []string
	ProjectFacts []string
}
type MemoryInput struct {
	GlobalFacts  []string
	ProjectFacts []string
	Messages     []model.Message
}
type Error struct {
	Category string
	Cause    error
}

func (e *Error) Error() string { return e.Category }
func (e *Error) Unwrap() error { return e.Cause }
func Category(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Category
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "provider"
}
func Invalid() error { return &Error{Category: "invalid_response"} }

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}
func RequestID(ctx context.Context) string { id, _ := ctx.Value(requestIDKey{}).(string); return id }
