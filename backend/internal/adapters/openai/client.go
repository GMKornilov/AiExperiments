package openai

import (
	"context"
	"encoding/json"
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

// CompleteWithTools executes one ephemeral tool-aware task completion.
func (c *Client) CompleteWithTools(ctx context.Context, purpose string, messages []completion.Message, tools []completion.ToolDefinition) (completion.Result, error) {
	if purpose != "task_step" {
		return completion.Result{}, completion.Invalid()
	}
	snap := c.snapshot.Chat
	call, cancel := context.WithTimeout(llm.WithPurpose(ctx, purpose), snap.Timeout)
	defer cancel()
	input := make([]llm.Message, len(messages))
	for index, message := range messages {
		input[index] = llm.Message{Role: message.Role, Content: message.Content, ToolCallID: message.ToolCallID}
		for _, requested := range message.ToolCalls {
			toolCall := llm.ToolCall{ID: requested.ID, Type: "function"}
			toolCall.Function.Name = requested.Name
			toolCall.Function.Arguments = string(requested.Arguments)
			input[index].ToolCalls = append(input[index].ToolCalls, toolCall)
		}
	}
	providerTools := make([]llm.Tool, len(tools))
	for index, tool := range tools {
		providerTools[index].Type = "function"
		providerTools[index].Function.Name = tool.Name
		providerTools[index].Function.Description = tool.Description
		providerTools[index].Function.Parameters = tool.Schema
	}
	result, calls, err := llm.NewClient(snap.BaseURL, snap.APIKey, snap.Timeout).ChatCompletionWithTools(call, snap.Model, input, snap.Temperature, providerTools)
	if err != nil {
		category := "provider"
		var upstream *llm.Error
		if errors.As(err, &upstream) {
			category = string(upstream.Kind)
		}
		return completion.Result{}, &completion.Error{Category: category, Cause: err}
	}
	if call.Err() != nil {
		return completion.Result{}, &completion.Error{Category: completion.Category(call.Err()), Cause: call.Err()}
	}
	out := completion.Result{Text: result.Text}
	for _, toolCall := range calls {
		out.ToolCalls = append(out.ToolCalls, completion.ToolCall{ID: toolCall.ID, Name: toolCall.Function.Name, Arguments: json.RawMessage(toolCall.Function.Arguments)})
	}
	return out, nil
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
	case "invariant_equipment-availability", "invariant_beans-availability", "invariant_inventory-truth", "invariant_research-sufficiency":
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
