package taskflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"aichallenge/week_1/task_1/internal/application/completion"
	"aichallenge/week_1/task_1/internal/application/invariant"
	"aichallenge/week_1/task_1/internal/brewmark"
	"aichallenge/week_1/task_1/internal/domain/model"
)

type toolLoopClient struct {
	results  []completion.Result
	messages [][]completion.Message
	tools    [][]completion.ToolDefinition
	plain    string
}

func (c *toolLoopClient) Complete(context.Context, string, []completion.Message) (string, error) {
	if c.plain == "" {
		return "", errors.New("unexpected plain completion")
	}
	return c.plain, nil
}

func (c *toolLoopClient) CompleteWithTools(_ context.Context, _ string, messages []completion.Message, tools []completion.ToolDefinition) (completion.Result, error) {
	c.messages = append(c.messages, append([]completion.Message{}, messages...))
	c.tools = append(c.tools, append([]completion.ToolDefinition{}, tools...))
	result := c.results[0]
	c.results = c.results[1:]
	return result, nil
}

type toolLoopMCP struct {
	mu     sync.Mutex
	calls  int
	name   string
	args   json.RawMessage
	result json.RawMessage
	err    error
}

func (m *toolLoopMCP) Call(_ context.Context, name string, args json.RawMessage, _ string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.name, m.args = name, append(json.RawMessage{}, args...)
	return m.result, m.err
}

func TestTaskStepPairsToolCallAndFullResult(t *testing.T) {
	call := completion.ToolCall{ID: "call-1", Name: "brewmark_list_grinders", Arguments: json.RawMessage(`{"brand":"Niche"}`)}
	client := &toolLoopClient{results: []completion.Result{{ToolCalls: []completion.ToolCall{call}}, {Text: `{"output":"ok"}`}}}
	mcp := &toolLoopMCP{result: json.RawMessage(`{"content":[{"type":"text","text":"full result"}],"structuredContent":{"items":[1]},"isError":false}`)}
	service := &Service{client: client, mcp: mcp}
	usedTool := false
	attempt := invariant.ResearchToolAttempt{Outcome: "no_tool"}
	raw, used, err := service.taskStep(context.Background(), []completion.Message{{Role: "system", Content: "s"}, {Role: "user", Content: "u"}}, "task-1", model.TaskStageResearchInputData, 8, &usedTool, &attempt)
	if err != nil || raw != `{"output":"ok"}` || used != 2 || mcp.calls != 1 || mcp.name != call.Name {
		t.Fatalf("raw=%q used=%d calls=%d err=%v", raw, used, mcp.calls, err)
	}
	if len(client.tools[0]) != 4 || len(client.tools[1]) != 0 {
		t.Fatal("tools must be present only on the first request")
	}
	if attempt.Tool != "brewmark_list_grinders" || attempt.Outcome != "success" {
		t.Fatalf("attempt = %+v", attempt)
	}
	if !strings.Contains(client.messages[0][0].Content, toolSafetyInstruction) || !strings.Contains(client.messages[1][0].Content, toolSafetyInstruction) {
		t.Fatal("tool safety instruction must be present in both paired requests")
	}
	for _, required := range []string{"tool results after assistant tool_calls", "tools are unavailable for this continuation", "do not emit DSML", "strict JSON proposal"} {
		if !strings.Contains(toolSafetyInstruction, required) {
			t.Fatalf("tool safety instruction misses %q: %q", required, toolSafetyInstruction)
		}
	}
	continuation := client.messages[1]
	if len(continuation) != 4 || continuation[2].Role != "assistant" || len(continuation[2].ToolCalls) != 1 || continuation[2].ToolCalls[0].ID != "call-1" || continuation[3].Role != "tool" || continuation[3].ToolCallID != "call-1" || continuation[3].Content != string(mcp.result) {
		t.Fatalf("continuation = %#v", continuation)
	}
}

func TestRepairPromptsDoNotRequireKeepingCurrentStage(t *testing.T) {
	messages := taskRepairPrompt([]completion.Message{{Role: "system", Content: "base"}}, []invariant.Violation{{
		InvariantID:       "task-transition",
		RepairInstruction: "Верни валидный следующий снимок: выбери текущий этап только для незавершённой работы либо ровно один разрешённый переход из TASK_STATE; не перескакивай этапы и не сохраняй текущий этап автоматически.",
	}})
	prompt := messages[0].Content
	for _, required := range []string{"ровно один разрешённый переход из TASK_STATE", "не сохраняй текущий этап автоматически"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("repair prompt misses %q: %q", required, prompt)
		}
	}
	if strings.Contains(prompt, "Сохрани текущий этап") {
		t.Fatalf("repair prompt preserves conflicting instruction: %q", prompt)
	}
}

type parallelBatchMCP struct {
	started chan string
	release chan struct{}
}

func (m *parallelBatchMCP) Call(_ context.Context, name string, _ json.RawMessage, _ string) (json.RawMessage, error) {
	m.started <- name
	<-m.release
	return json.RawMessage(`{"content":[{"type":"text","text":"catalogue"}],"structuredContent":{"matchStatus":"exact"},"isError":false}`), nil
}

func TestTaskStepExecutesBatchInParallelAndPreservesToolMessageOrder(t *testing.T) {
	calls := []completion.ToolCall{
		{ID: "grinder", Name: "brewmark_list_grinders", Arguments: json.RawMessage(`{"brand":"Niche","name":"Zero"}`)},
		{ID: "brewer", Name: "brewmark_list_brewers", Arguments: json.RawMessage(`{"brand":"DeLonghi","name":"EC685"}`)},
	}
	client := &toolLoopClient{results: []completion.Result{{ToolCalls: calls}, {Text: `{"output":"research complete"}`}}}
	mcp := &parallelBatchMCP{started: make(chan string, 2), release: make(chan struct{})}
	service := &Service{client: client, mcp: mcp}
	usedTool := false
	attempt := invariant.ResearchToolAttempt{Outcome: "no_tool"}
	done := make(chan error, 1)
	go func() {
		_, _, err := service.taskStep(context.Background(), []completion.Message{{Role: "system", Content: "s"}}, "task-1", model.TaskStageResearchInputData, 8, &usedTool, &attempt)
		done <- err
	}()
	<-mcp.started
	<-mcp.started
	close(mcp.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if attempt.Outcome != "success" || len(attempt.Calls) != 2 || attempt.Calls[0].Tool != calls[0].Name || attempt.Calls[1].Tool != calls[1].Name {
		t.Fatalf("attempt=%+v", attempt)
	}
	continuation := client.messages[1]
	if len(continuation) != 4 || continuation[1].Role != "assistant" || len(continuation[1].ToolCalls) != 2 || continuation[2].ToolCallID != "grinder" || continuation[3].ToolCallID != "brewer" {
		t.Fatalf("continuation=%#v", continuation)
	}
}

func TestTaskStepRejectsInvalidCallsWithoutMCP(t *testing.T) {
	fiveCalls := make([]completion.ToolCall, 5)
	for index := range fiveCalls {
		fiveCalls[index] = completion.ToolCall{ID: string(rune('a' + index)), Name: "brewmark_list_filters", Arguments: json.RawMessage(`{}`)}
	}
	for _, calls := range [][]completion.ToolCall{
		{{ID: "x", Name: "unknown", Arguments: json.RawMessage(`{}`)}},
		{{ID: "x", Name: "brewmark_list_filters", Arguments: json.RawMessage(`{"brand":"x"}`)}},
		{{ID: "x", Name: "brewmark_list_filters", Arguments: json.RawMessage(`{}`)}, {ID: "y", Name: "brewmark_list_filters", Arguments: json.RawMessage(`{}`)}},
		{{ID: "same", Name: "brewmark_list_filters", Arguments: json.RawMessage(`{}`)}, {ID: "same", Name: "brewmark_list_grinders", Arguments: json.RawMessage(`{"brand":"Niche"}`)}},
		fiveCalls,
	} {
		client := &toolLoopClient{results: []completion.Result{{ToolCalls: calls}}}
		mcp := &toolLoopMCP{}
		service := &Service{client: client, mcp: mcp}
		usedTool := false
		_, _, err := service.taskStep(context.Background(), []completion.Message{{Role: "system", Content: "s"}}, "task-1", model.TaskStageResearchInputData, 8, &usedTool, nil)
		if err == nil || mcp.calls != 0 {
			t.Fatalf("calls=%#v err=%v mcp=%d", calls, err, mcp.calls)
		}
	}
}

func TestTaskStepDoesNotExposeToolsOutsideResearchOrWithoutTwoSlots(t *testing.T) {
	for _, stage := range []model.TaskStage{model.TaskStageClarifyInput, model.TaskStageExecution} {
		client := &toolLoopClient{plain: `{"output":"text"}`}
		mcp := &toolLoopMCP{}
		service := &Service{client: client, mcp: mcp}
		usedTool := false
		_, used, err := service.taskStep(context.Background(), []completion.Message{{Role: "system", Content: "s"}}, "task-1", stage, 8, &usedTool, nil)
		if err != nil || used != 1 || len(client.tools) != 0 || mcp.calls != 0 {
			t.Fatalf("stage=%s used=%d tools=%d mcp=%d err=%v", stage, used, len(client.tools), mcp.calls, err)
		}
	}
	client := &toolLoopClient{plain: `{"output":"text"}`}
	usedTool := false
	_, used, err := (&Service{client: client}).taskStep(context.Background(), []completion.Message{{Role: "system", Content: "s"}}, "task-1", model.TaskStageResearchInputData, 1, &usedTool, nil)
	if err != nil || used != 1 || len(client.tools) != 0 {
		t.Fatalf("remaining=%d tools=%d err=%v", used, len(client.tools), err)
	}
}

func TestTaskStepRejectsSecondToolRound(t *testing.T) {
	requested := completion.ToolCall{ID: "x", Name: "brewmark_list_filters", Arguments: json.RawMessage(`{}`)}
	client := &toolLoopClient{results: []completion.Result{
		{ToolCalls: []completion.ToolCall{requested}},
		{ToolCalls: []completion.ToolCall{requested}},
		{ToolCalls: []completion.ToolCall{requested}},
		{Text: `{"output":"repair"}`},
	}}
	mcp := &toolLoopMCP{result: json.RawMessage(`{"content":[],"structuredContent":{},"isError":false}`)}
	service := &Service{client: client, mcp: mcp}
	usedTool := false
	_, _, err := service.taskStep(context.Background(), []completion.Message{{Role: "system", Content: "s"}}, "task-1", model.TaskStageResearchInputData, 8, &usedTool, nil)
	if !errors.Is(err, errInvalidToolCandidate) || mcp.calls != 1 || !usedTool {
		t.Fatalf("err=%v calls=%d used=%t", err, mcp.calls, usedTool)
	}
	_, calls, err := service.taskStep(context.Background(), []completion.Message{{Role: "system", Content: "s"}}, "task-1", model.TaskStageResearchInputData, 6, &usedTool, nil)
	if !errors.Is(err, errInvalidToolCandidate) || calls != 1 || mcp.calls != 1 || len(client.tools) != 3 || len(client.tools[2]) != 0 {
		t.Fatalf("second batch calls=%d mcp=%d tools=%d err=%v", calls, mcp.calls, len(client.tools), err)
	}
	text, calls, err := service.taskStep(context.Background(), []completion.Message{{Role: "system", Content: "s"}}, "task-1", model.TaskStageResearchInputData, 5, &usedTool, nil)
	if err != nil || text != `{"output":"repair"}` || calls != 1 || mcp.calls != 1 || len(client.tools) != 4 || len(client.tools[3]) != 0 {
		t.Fatalf("repair calls=%d mcp=%d tools=%d text=%q err=%v", calls, mcp.calls, len(client.tools), text, err)
	}
}

type outcomeBatchMCP struct {
	results map[string]json.RawMessage
	errors  map[string]error
}

func (m *outcomeBatchMCP) Call(_ context.Context, name string, _ json.RawMessage, _ string) (json.RawMessage, error) {
	return m.results[name], m.errors[name]
}

func TestBatchAttemptMetadataCoversPartialAndFullFailure(t *testing.T) {
	success := json.RawMessage(`{"content":[{"type":"text","text":"catalogue"}],"structuredContent":{"matchStatus":"exact"},"isError":false}`)
	calls := []completion.ToolCall{
		{ID: "grinder", Name: "brewmark_list_grinders", Arguments: json.RawMessage(`{"brand":"Niche","name":"Zero"}`)},
		{ID: "brewer", Name: "brewmark_list_brewers", Arguments: json.RawMessage(`{"brand":"DeLonghi","name":"EC685"}`)},
	}
	for _, testCase := range []struct {
		name          string
		mcp           *outcomeBatchMCP
		wantOutcome   string
		wantCallState []string
	}{
		{
			name:        "partial failure preserves success",
			mcp:         &outcomeBatchMCP{results: map[string]json.RawMessage{calls[0].Name: success}, errors: map[string]error{calls[1].Name: errors.New("private")}},
			wantOutcome: "partial_failure", wantCallState: []string{"success", "broker_error"},
		},
		{
			name:        "full failure",
			mcp:         &outcomeBatchMCP{errors: map[string]error{calls[0].Name: errors.New("private one"), calls[1].Name: errors.New("private two")}},
			wantOutcome: "failure", wantCallState: []string{"broker_error", "broker_error"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			attempt := invariant.ResearchToolAttempt{Outcome: "no_tool"}
			results := (&Service{mcp: testCase.mcp}).callBrewmarkBatch(context.Background(), "task-1", model.TaskStageResearchInputData, calls, &attempt)
			if attempt.Outcome != testCase.wantOutcome || len(attempt.Calls) != len(testCase.wantCallState) {
				t.Fatalf("attempt=%+v", attempt)
			}
			for index, want := range testCase.wantCallState {
				if attempt.Calls[index].Outcome != want {
					t.Fatalf("call[%d]=%+v want=%q", index, attempt.Calls[index], want)
				}
			}
			if testCase.wantOutcome == "partial_failure" && string(results[0]) != string(success) {
				t.Fatalf("successful MCP result was changed: %s", results[0])
			}
			for index, result := range results {
				if !json.Valid(result) {
					t.Fatalf("result[%d] is not a safe tool envelope: %q", index, result)
				}
			}
		})
	}
}

func TestTaskLLMDefinitionsMatchCanonicalToolContracts(t *testing.T) {
	definitions := brewmarkTools()
	contracts := brewmark.ToolContracts()
	if len(definitions) != len(contracts) {
		t.Fatalf("definitions=%d contracts=%d", len(definitions), len(contracts))
	}
	for index, contract := range contracts {
		definition := definitions[index]
		if definition.Name != contract.Name || definition.Description != contract.Description || string(definition.Schema) != string(contract.InputSchema) {
			t.Fatalf("definition[%d]=%+v contract=%+v", index, definition, contract)
		}
	}
}

func TestTaskStepBrokerFailureStillContinuesAndRejectsSecondToolCall(t *testing.T) {
	requested := completion.ToolCall{ID: "x", Name: "brewmark_list_filters", Arguments: json.RawMessage(`{}`)}
	client := &toolLoopClient{results: []completion.Result{{ToolCalls: []completion.ToolCall{requested}}, {ToolCalls: []completion.ToolCall{requested}}}}
	mcp := &toolLoopMCP{err: errors.New("private broker failure")}
	service := &Service{client: client, mcp: mcp}
	usedTool := false
	_, used, err := service.taskStep(context.Background(), []completion.Message{{Role: "system", Content: "s"}}, "task-1", model.TaskStageResearchInputData, 8, &usedTool, nil)
	if !errors.Is(err, errInvalidToolCandidate) || used != 2 || mcp.calls != 1 || !json.Valid([]byte(client.messages[1][2].Content)) {
		t.Fatalf("used=%d calls=%d err=%v messages=%#v", used, mcp.calls, err, client.messages)
	}
}
