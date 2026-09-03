package algorithms

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/llm"
)

type recordedClient struct {
	calls   [][]llm.Message
	answers []string
	errors  []error
}

func testPrompts(t *testing.T) Prompts {
	t.Helper()
	prompts, err := NewPrompts(PromptSources{
		DirectSystem:               "direct {{.Language}} {{.InterfaceRule}}",
		StepByStepSystem:           "решай пошагово краткое публичное объяснение",
		ExpertsSystem:              "Теоретик Практик Скептик собственную функцию",
		MetaPromptGenerationSystem: "generate {{.Language}} {{.InterfaceRule}}",
		MetaSolutionSystem:         "solution {{.Language}} {{.InterfaceRule}}",
		MetaSolutionUser:           "Исходный statement:\n{{.Statement}}\n\nНеизменённый generated prompt:\n{{.GeneratedPrompt}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	return prompts
}

type deadlineClient struct {
	deadlines []time.Time
	answers   []string
	hangAt    int
}

func (c *deadlineClient) ChatMessages(ctx context.Context, _ string, _ []llm.Message) (string, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return "", errors.New("missing deadline")
	}
	c.deadlines = append(c.deadlines, deadline)
	if c.hangAt == len(c.deadlines) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return c.answers[len(c.deadlines)-1], nil
}

func (c *recordedClient) ChatMessages(_ context.Context, _ string, messages []llm.Message) (string, error) {
	copyOfMessages := append([]llm.Message(nil), messages...)
	c.calls = append(c.calls, copyOfMessages)
	index := len(c.calls) - 1
	if index < len(c.errors) && c.errors[index] != nil {
		return "", c.errors[index]
	}
	return c.answers[index], nil
}

func TestServiceSolveUsesIndependentPromptContracts(t *testing.T) {
	statement := "  Найдите два числа.\\nОграничение: n <= 10.\\nПример: [2,7], 9 -> [0,1]  "
	client := &recordedClient{answers: []string{"direct", "steps", "generated prompt", "generated solution", "experts"}}
	service := NewService(client, "test-model", time.Second, testPrompts(t))

	for _, method := range []Method{MethodDirect, MethodStepByStep, MethodGeneratedPrompt, MethodExperts} {
		result, err := service.Solve(context.Background(), method, Request{Statement: statement, Language: LanguagePython})
		if err != nil {
			t.Fatalf("Solve(%q) error = %v", method, err)
		}
		if result.Method != method || result.Answer == "" {
			t.Errorf("Solve(%q) result = %#v", method, result)
		}
	}

	if len(client.calls) != 5 {
		t.Fatalf("LLM calls = %d, want 5", len(client.calls))
	}
	for index, call := range client.calls {
		if len(call) != 2 {
			t.Errorf("call %d message count = %d, want 2", index, len(call))
		}
	}

	direct, step, generate, generatedSolution, experts := client.calls[0], client.calls[1], client.calls[2], client.calls[3], client.calls[4]
	if strings.Contains(strings.ToLower(direct[0].Content), "пошагово") {
		t.Error("direct prompt contains step-by-step instruction")
	}
	for _, required := range []string{"решай пошагово", "краткое публичное объяснение"} {
		if !strings.Contains(step[0].Content, required) {
			t.Errorf("step prompt does not contain %q", required)
		}
	}
	for _, required := range []string{"Теоретик", "Практик", "Скептик", "собственную функцию"} {
		if !strings.Contains(experts[0].Content, required) {
			t.Errorf("expert prompt does not contain %q", required)
		}
	}
	for _, messages := range [][]llm.Message{direct, step, generate, experts} {
		if messages[1].Content != statement {
			t.Errorf("raw statement changed: %q", messages[1].Content)
		}
	}
	if !strings.Contains(generate[0].Content, "generate") || !strings.Contains(generatedSolution[0].Content, "solution") {
		t.Error("meta prompts do not use their separate templates")
	}
	if !strings.Contains(generatedSolution[1].Content, statement) || !strings.Contains(generatedSolution[1].Content, "generated prompt") {
		t.Errorf("second meta prompt lacks raw snapshot or generated prompt: %q", generatedSolution[1].Content)
	}
	if !strings.Contains(generatedSolution[1].Content, "generated prompt") || !strings.Contains(generatedSolution[1].Content, client.answers[2]) {
		t.Errorf("second meta prompt changes generated prompt: %q", generatedSolution[1].Content)
	}
}

func TestServiceSolveLanguageInterfaceRules(t *testing.T) {
	tests := []struct {
		language Language
		want     string
	}{
		{LanguagePython, "одна вызываемая функция"},
		{LanguageCPP, "одна вызываемая функция"},
		{LanguageJava, "минимальный класс-контейнер с одним вызываемым методом"},
	}
	for _, test := range tests {
		t.Run(string(test.language), func(t *testing.T) {
			client := &recordedClient{answers: []string{"answer"}}
			_, err := NewService(client, "model", time.Second, testPrompts(t)).Solve(context.Background(), MethodDirect, Request{Statement: "условие", Language: test.language})
			if err != nil {
				t.Fatal(err)
			}
			prompt := client.calls[0][0].Content
			if !strings.Contains(prompt, test.want) {
				t.Errorf("prompt = %q, want language contract", prompt)
			}
		})
	}
}

func TestPromptTemplatesRejectUnknownPlaceholderAndChangeRenderedMessages(t *testing.T) {
	invalid := PromptSources{
		DirectSystem:               "{{.Unknown}}",
		StepByStepSystem:           "steps",
		ExpertsSystem:              "experts",
		MetaPromptGenerationSystem: "generate {{.Language}}",
		MetaSolutionSystem:         "solution {{.Language}}",
		MetaSolutionUser:           "{{.Statement}} {{.GeneratedPrompt}}",
	}
	if _, err := NewPrompts(invalid); err == nil {
		t.Fatal("NewPrompts() error = nil for unknown placeholder")
	}

	sources := PromptSources{
		DirectSystem:               "changed {{.Language}} {{.InterfaceRule}}",
		StepByStepSystem:           "steps",
		ExpertsSystem:              "experts",
		MetaPromptGenerationSystem: "generate {{.Language}}",
		MetaSolutionSystem:         "solution {{.Language}}",
		MetaSolutionUser:           "statement=<{{.Statement}}> generated=<{{.GeneratedPrompt}}>",
	}
	prompts, err := NewPrompts(sources)
	if err != nil {
		t.Fatal(err)
	}
	statement := "  raw\n statement  "
	client := &recordedClient{answers: []string{"answer"}}
	_, err = NewService(client, "model", time.Second, prompts).Solve(context.Background(), MethodDirect, Request{Statement: statement, Language: LanguageCPP})
	if err != nil {
		t.Fatal(err)
	}
	if got := client.calls[0][0].Content; !strings.HasPrefix(got, "changed cpp") {
		t.Errorf("rendered prompt = %q, want modified fixture content", got)
	}
	if client.calls[0][1].Content != statement {
		t.Errorf("statement = %q, want exact %q", client.calls[0][1].Content, statement)
	}
}

func TestServiceSolvePreservesExactTraceAndPartialMetaFailure(t *testing.T) {
	firstAnswer := "  generated prompt\n{{.Statement}}  "
	client := &recordedClient{answers: []string{firstAnswer}, errors: []error{nil, errors.New("provider down")}}
	statement := "  raw\n{{.Language}}  "
	result, err := NewService(client, "model", time.Second, testPrompts(t)).Solve(context.Background(), MethodGeneratedPrompt, Request{Statement: statement, Language: LanguageJava})
	if err == nil {
		t.Fatal("Solve() error = nil, want provider failure")
	}
	var algorithmError *Error
	if !errors.As(err, &algorithmError) || algorithmError.Kind != ErrorUnavailable {
		t.Fatalf("error = %v, want unavailable Error", err)
	}
	if result.Answer != "" || len(result.Trace) != 2 {
		t.Fatalf("partial result = %#v, want two trace steps without answer", result)
	}
	if result.Trace[0].Response == nil || *result.Trace[0].Response != firstAnswer {
		t.Errorf("first response = %#v, want exact %q", result.Trace[0].Response, firstAnswer)
	}
	if result.Trace[1].Response != nil {
		t.Errorf("second response = %#v, want absent", result.Trace[1].Response)
	}
	if !reflect.DeepEqual(result.Trace[0].Messages, traceMessages(client.calls[0])) || !reflect.DeepEqual(result.Trace[1].Messages, traceMessages(client.calls[1])) {
		t.Error("trace messages do not exactly equal actual provider messages")
	}
	wantUser := "Исходный statement:\n" + statement + "\n\nНеизменённый generated prompt:\n" + firstAnswer
	if got := client.calls[1][1].Content; got != wantUser {
		t.Errorf("second provider request = %q, want exact %q", got, wantUser)
	}
}

func TestServiceSolveStopsMetaAfterFirstFailureAndRejectsInvalidInput(t *testing.T) {
	client := &recordedClient{errors: []error{errors.New("provider down")}}
	service := NewService(client, "model", time.Second, testPrompts(t))
	result, err := service.Solve(context.Background(), MethodGeneratedPrompt, Request{Statement: "condition", Language: LanguagePython})
	if err == nil || len(client.calls) != 1 || len(result.Trace) != 1 || result.Trace[0].Response != nil {
		t.Errorf("first meta failure result/calls = %#v/%d", result, len(client.calls))
	}

	for _, request := range []Request{{Statement: " ", Language: LanguagePython}, {Statement: "condition", Language: "go"}, {Statement: strings.Repeat("я", maxStatementRunes+1), Language: LanguagePython}} {
		client.calls = nil
		_, err := service.Solve(context.Background(), MethodDirect, request)
		var algorithmError *Error
		if !errors.As(err, &algorithmError) || algorithmError.Kind != ErrorInvalidRequest || len(client.calls) != 0 {
			t.Errorf("invalid request %#v: err=%v calls=%d", request, err, len(client.calls))
		}
	}
}

type cancellingClient struct {
	calls  int
	cancel context.CancelFunc
}

func (c *cancellingClient) ChatMessages(ctx context.Context, _ string, _ []llm.Message) (string, error) {
	c.calls++
	if c.calls == 1 {
		c.cancel()
		return "generated", nil
	}
	return "", ctx.Err()
}

func TestServiceSolveDoesNotStartSecondMetaCallAfterParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &cancellingClient{cancel: cancel}
	result, err := NewService(client, "model", time.Second, testPrompts(t)).Solve(ctx, MethodGeneratedPrompt, Request{Statement: "condition", Language: LanguagePython})
	if err == nil {
		t.Fatal("Solve() error = nil, want cancelled request error")
	}
	if client.calls != 1 || len(result.Trace) != 1 || result.Trace[0].Response == nil {
		t.Errorf("calls/trace after cancellation = %d/%#v, want first completed call only", client.calls, result.Trace)
	}
}

func TestServiceUsesConfiguredBudgetWithoutLegacyMinuteClamp(t *testing.T) {
	client := &deadlineClient{answers: []string{"answer"}}
	budget := 90 * time.Second
	before := time.Now()
	_, err := NewService(client, "model", budget, testPrompts(t)).Solve(context.Background(), MethodExperts, Request{Statement: "condition", Language: LanguagePython})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.deadlines) != 1 {
		t.Fatalf("calls = %d, want 1", len(client.deadlines))
	}
	if remaining := time.Until(client.deadlines[0]); remaining < budget-time.Second || remaining > budget {
		t.Errorf("deadline remaining = %v, want approximately %v after %v", remaining, budget, time.Since(before))
	}
}

func TestServiceCancelsHangingProviderAtOwnTimeout(t *testing.T) {
	client := &deadlineClient{hangAt: 1}
	result, err := NewService(client, "model", 10*time.Millisecond, testPrompts(t)).Solve(context.Background(), MethodDirect, Request{Statement: "condition", Language: LanguagePython})
	var algorithmError *Error
	if !errors.As(err, &algorithmError) || algorithmError.Kind != ErrorTimeout {
		t.Fatalf("error = %v, want timeout", err)
	}
	if len(result.Trace) != 1 || len(client.deadlines) != 1 {
		t.Errorf("result/calls = %#v/%d, want one sent call", result, len(client.deadlines))
	}
}

func TestServiceGivesEachMetaCallItsFullBudgetAndPreservesPartialTimeout(t *testing.T) {
	client := &deadlineClient{answers: []string{"generated prompt", "solution"}}
	budget := 90 * time.Second
	service := NewService(client, "model", budget, testPrompts(t))

	result, err := service.Solve(context.Background(), MethodGeneratedPrompt, Request{Statement: "condition", Language: LanguagePython})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.deadlines) != 2 || len(result.Trace) != 2 {
		t.Fatalf("calls/trace = %d/%d, want 2/2", len(client.deadlines), len(result.Trace))
	}
	for index, deadline := range client.deadlines {
		if remaining := time.Until(deadline); remaining < budget-time.Second || remaining > budget {
			t.Errorf("call %d remaining = %v, want approximately %v", index, remaining, budget)
		}
	}

	client = &deadlineClient{answers: []string{"generated prompt"}, hangAt: 2}
	result, err = NewService(client, "model", 10*time.Millisecond, testPrompts(t)).Solve(context.Background(), MethodGeneratedPrompt, Request{Statement: "condition", Language: LanguagePython})
	var algorithmError *Error
	if !errors.As(err, &algorithmError) || algorithmError.Kind != ErrorTimeout {
		t.Fatalf("second timeout error = %v, want timeout", err)
	}
	if len(result.Trace) != 2 || result.Trace[0].Response == nil || result.Trace[1].Response != nil {
		t.Fatalf("partial trace = %#v, want successful first and sent second trace", result.Trace)
	}
}

func TestServiceSolveDoesNotTraceLLMFailureBeforeRequestIsSent(t *testing.T) {
	client := llm.NewClient("://invalid", "test-key", time.Second)
	service := NewService(client, "model", time.Second, testPrompts(t))
	for _, method := range []Method{MethodDirect, MethodGeneratedPrompt} {
		t.Run(string(method), func(t *testing.T) {
			result, err := service.Solve(context.Background(), method, Request{Statement: "condition", Language: LanguagePython})
			if err == nil {
				t.Fatal("Solve() error = nil, want pre-send failure")
			}
			if len(result.Trace) != 0 {
				t.Errorf("pre-send failure trace = %#v, want no actual provider calls", result.Trace)
			}
		})
	}
}

func TestServiceClassifiesInvalidProviderMIMEAsInvalidResponse(t *testing.T) {
	providerCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerCalls++
		if request.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/jsonp")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"answer"}}]}`))
	}))
	defer server.Close()

	client := llm.NewClient(server.URL, "test-key", time.Second)
	result, err := NewService(client, "model", time.Second, testPrompts(t)).Solve(context.Background(), MethodDirect, Request{Statement: "condition", Language: LanguagePython})
	var algorithmError *Error
	if !errors.As(err, &algorithmError) || algorithmError.Kind != ErrorInvalidResponse {
		t.Fatalf("error = %v, want invalid_response", err)
	}
	if providerCalls != 1 || len(result.Trace) != 1 || result.Trace[0].Response != nil {
		t.Errorf("provider calls/trace = %d/%#v, want one sent request without response", providerCalls, result.Trace)
	}
}
