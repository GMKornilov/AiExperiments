package llm

import "context"

// Trace contains transport metadata and a body, never authorization headers.
type Trace struct {
	CallID        string
	Purpose       string
	Event         string
	Payload       string
	HTTPStatus    int
	DurationMS    int64
	ErrorCategory string
	Truncated     bool
}
type traceKey struct{}
type purposeKey struct{}

func WithTrace(ctx context.Context, observer func(context.Context, Trace)) context.Context {
	return context.WithValue(ctx, traceKey{}, observer)
}
func WithPurpose(ctx context.Context, purpose string) context.Context {
	return context.WithValue(ctx, purposeKey{}, purpose)
}
func emitTrace(ctx context.Context, trace Trace) {
	observer, _ := ctx.Value(traceKey{}).(func(context.Context, Trace))
	if observer == nil {
		return
	}
	trace.Purpose, _ = ctx.Value(purposeKey{}).(string)
	if trace.Purpose == "" {
		trace.Purpose = "chat"
	}
	observer(ctx, trace)
}
