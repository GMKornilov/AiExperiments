package extractjson

import (
	"aichallenge/week_1/task_1/internal/llm"
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"aichallenge/week_1/task_1/internal/application/completion"
	"aichallenge/week_1/task_1/internal/domain/model"
)

type CompletionClient interface {
	Complete(context.Context, string, []completion.Message) (string, error)
}
type Extractor struct {
	client CompletionClient
	prompt string
}

func NewExtractor(client CompletionClient, prompt string) *Extractor {
	return &Extractor{client: client, prompt: prompt}
}
func DecodeFacts(raw string) (completion.Facts, error) {
	var wire struct {
		GlobalFacts  []string `json:"global_facts"`
		ProjectFacts []string `json:"project_facts"`
	}
	if Decode([]byte(raw), &wire) != nil || wire.GlobalFacts == nil || wire.ProjectFacts == nil || !model.ValidFacts(wire.GlobalFacts) || !model.ValidFacts(wire.ProjectFacts) {
		return completion.Facts{}, completion.Invalid()
	}
	return completion.Facts{GlobalFacts: wire.GlobalFacts, ProjectFacts: wire.ProjectFacts}, nil
}
func (e *Extractor) Extract(ctx context.Context, in completion.MemoryInput) (facts completion.Facts, err error) {
	started := time.Now()
	defer func() {
		result, category := "success", ""
		if err != nil {
			result = "failure"
			category = completion.Category(err)
		}
		slog.Info("barista.memory_extractor", "source", "backend", "event", "memory_extractor", "result", result, "error_category", category, "correlation_id", llm.RequestID(ctx), "duration_ms", time.Since(started).Milliseconds())
	}()
	type message struct {
		Role string `json:"role"`
		Text string `json:"text"`
	}
	wire := struct {
		GlobalFacts  []string  `json:"global_facts"`
		ProjectFacts []string  `json:"project_facts"`
		Messages     []message `json:"messages"`
	}{GlobalFacts: in.GlobalFacts, ProjectFacts: in.ProjectFacts, Messages: []message{}}
	for _, m := range in.Messages {
		wire.Messages = append(wire.Messages, message{Role: m.Role, Text: m.Text})
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return completion.Facts{}, completion.Invalid()
	}
	raw, err := e.client.Complete(ctx, "memory_extractor", []completion.Message{{Role: "system", Content: e.prompt}, {Role: "user", Content: string(data)}})
	if err != nil {
		return completion.Facts{}, err
	}
	return DecodeFacts(raw)
}
