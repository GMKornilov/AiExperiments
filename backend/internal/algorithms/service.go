// Package algorithms builds and executes the prompt-comparison algorithm methods.
package algorithms

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"aichallenge/week_1/task_1/internal/llm"
)

const maxStatementRunes = 10000

const (
	promptDirectSystem               = "direct-system"
	promptStepByStepSystem           = "step-by-step-system"
	promptExpertsSystem              = "experts-system"
	promptMetaPromptGenerationSystem = "meta-prompt-generation-system"
	promptMetaSolutionSystem         = "meta-solution-system"
	promptMetaSolutionUser           = "meta-solution-user"
)

// PromptSources contains the algorithm prompt templates loaded at startup.
type PromptSources struct {
	DirectSystem               string
	StepByStepSystem           string
	ExpertsSystem              string
	MetaPromptGenerationSystem string
	MetaSolutionSystem         string
	MetaSolutionUser           string
}

// PromptData contains the explicit values allowed in algorithm templates.
type PromptData struct {
	Language        string
	InterfaceRule   string
	Statement       string
	GeneratedPrompt string
}

// Prompts is a validated, compiled set of algorithm templates.
type Prompts struct {
	templates map[string]*template.Template
}

// NewPrompts parses and validates the complete prompt set before requests begin.
func NewPrompts(sources PromptSources) (Prompts, error) {
	sourceByName := map[string]string{
		promptDirectSystem:               sources.DirectSystem,
		promptStepByStepSystem:           sources.StepByStepSystem,
		promptExpertsSystem:              sources.ExpertsSystem,
		promptMetaPromptGenerationSystem: sources.MetaPromptGenerationSystem,
		promptMetaSolutionSystem:         sources.MetaSolutionSystem,
		promptMetaSolutionUser:           sources.MetaSolutionUser,
	}
	prompts := Prompts{templates: make(map[string]*template.Template, len(sourceByName))}
	probe := PromptData{Language: "python", InterfaceRule: "interface", Statement: "statement", GeneratedPrompt: "generated"}
	for name, source := range sourceByName {
		if strings.TrimSpace(source) == "" {
			return Prompts{}, fmt.Errorf("algorithm prompt %s не должен быть пустым", name)
		}
		parsed, err := template.New(name).Option("missingkey=error").Parse(source)
		if err != nil {
			return Prompts{}, fmt.Errorf("разбор algorithm prompt %s: %w", name, err)
		}
		var rendered bytes.Buffer
		if err := parsed.Execute(&rendered, probe); err != nil {
			return Prompts{}, fmt.Errorf("проверка algorithm prompt %s: %w", name, err)
		}
		prompts.templates[name] = parsed
	}
	return prompts, nil
}

func (p Prompts) render(name string, data PromptData) (string, error) {
	parsed := p.templates[name]
	if parsed == nil {
		return "", fmt.Errorf("algorithm prompt %s не загружен", name)
	}
	var rendered bytes.Buffer
	if err := parsed.Execute(&rendered, data); err != nil {
		return "", fmt.Errorf("рендер algorithm prompt %s: %w", name, err)
	}
	return rendered.String(), nil
}

// Method identifies an algorithms prompt method.
type Method string

const (
	MethodDirect          Method = "direct"
	MethodStepByStep      Method = "step-by-step"
	MethodGeneratedPrompt Method = "generated-prompt"
	MethodExperts         Method = "experts"
)

// Language selects the generated code language.
type Language string

const (
	LanguagePython Language = "python"
	LanguageJava   Language = "java"
	LanguageCPP    Language = "cpp"
)

// Request is the immutable user snapshot for one method.
type Request struct {
	Statement string
	Language  Language
}

// Message records one exact LLM message in a trace.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// TraceStep records one actual LLM request and, when received, its raw response.
type TraceStep struct {
	Step     string    `json:"step"`
	Messages []Message `json:"messages"`
	Response *string   `json:"response,omitempty"`
}

// Result is the successful algorithm result. HTTP errors add an envelope around it.
type Result struct {
	Method Method      `json:"method"`
	Answer string      `json:"answer"`
	Trace  []TraceStep `json:"trace"`
}

// Client is the shared LLM capability needed by the algorithms service.
type Client interface {
	ChatMessages(context.Context, string, []llm.Message) (string, error)
}

// ErrorKind makes provider failures safe to classify at the HTTP boundary.
type ErrorKind string

const (
	ErrorInvalidRequest  ErrorKind = "invalid_request"
	ErrorUnavailable     ErrorKind = "unavailable"
	ErrorTimeout         ErrorKind = "timeout"
	ErrorInvalidResponse ErrorKind = "invalid_response"
)

// Error carries a safe category while retaining a wrapped operational error.
type Error struct {
	Kind ErrorKind
	err  error
}

func (e *Error) Error() string { return string(e.Kind) }
func (e *Error) Unwrap() error { return e.err }

// NewError returns a categorized service error.
func NewError(kind ErrorKind, err error) error { return &Error{Kind: kind, err: err} }

// Service executes algorithm methods with a per-call timeout.
type Service struct {
	client  Client
	model   string
	timeout time.Duration
	prompts Prompts
}

// NewService creates an algorithms service using its dedicated LLM client.
func NewService(client Client, model string, requestTimeout time.Duration, prompts Prompts) *Service {
	return &Service{client: client, model: model, timeout: requestTimeout, prompts: prompts}
}

// Solve executes exactly the calls required by method.
func (s *Service) Solve(ctx context.Context, method Method, request Request) (Result, error) {
	result := Result{Method: method, Trace: []TraceStep{}}
	if err := validateRequest(request); err != nil {
		return result, NewError(ErrorInvalidRequest, err)
	}
	if s.client == nil || strings.TrimSpace(s.model) == "" || s.timeout <= 0 {
		return result, NewError(ErrorUnavailable, errors.New("LLM service is not configured"))
	}
	if err := ctx.Err(); err != nil {
		return result, contextError(err)
	}

	switch method {
	case MethodDirect, MethodStepByStep, MethodExperts:
		messages, err := s.methodMessages(method, request)
		if err != nil {
			return result, NewError(ErrorUnavailable, err)
		}
		answer, sent, err := s.call(ctx, messages)
		if sent {
			trace := TraceStep{Step: "solution", Messages: traceMessages(messages)}
			result.Trace = append(result.Trace, trace)
		}
		if err != nil {
			return result, err
		}
		result.Trace[0].Response = &answer
		result.Answer = answer
		return result, nil
	case MethodGeneratedPrompt:
		return s.solveGeneratedPrompt(ctx, result, request)
	default:
		return result, NewError(ErrorInvalidRequest, fmt.Errorf("unsupported method %q", method))
	}
}

func (s *Service) solveGeneratedPrompt(ctx context.Context, result Result, request Request) (Result, error) {
	firstMessages, err := s.generatedPromptMessages(request)
	if err != nil {
		return result, NewError(ErrorUnavailable, err)
	}
	generated, sent, err := s.call(ctx, firstMessages)
	if sent {
		firstTrace := TraceStep{Step: "generate-prompt", Messages: traceMessages(firstMessages)}
		result.Trace = append(result.Trace, firstTrace)
	}
	if err != nil {
		return result, err
	}
	result.Trace[0].Response = &generated
	if err := ctx.Err(); err != nil {
		return result, contextError(err)
	}

	secondMessages, err := s.generatedSolutionMessages(request, generated)
	if err != nil {
		return result, NewError(ErrorUnavailable, err)
	}
	answer, sent, err := s.call(ctx, secondMessages)
	if sent {
		secondTrace := TraceStep{Step: "solution", Messages: traceMessages(secondMessages)}
		result.Trace = append(result.Trace, secondTrace)
	}
	if err != nil {
		return result, err
	}
	result.Trace[1].Response = &answer
	result.Answer = answer
	return result, nil
}

func (s *Service) call(parent context.Context, messages []llm.Message) (string, bool, error) {
	if err := parent.Err(); err != nil {
		return "", false, contextError(err)
	}
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	answer, err := s.client.ChatMessages(ctx, s.model, messages)
	if err == nil {
		return answer, true, nil
	}
	var chatErr *llm.ChatError
	if errors.As(err, &chatErr) {
		switch chatErr.Kind {
		case llm.ChatErrorPreSend:
			return "", false, NewError(ErrorUnavailable, err)
		case llm.ChatErrorInvalidResponse:
			return "", true, NewError(ErrorInvalidResponse, err)
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "", true, NewError(ErrorTimeout, err)
	}
	errorText := strings.ToLower(err.Error())
	if strings.Contains(errorText, "пуст") || strings.Contains(errorText, "разбор") || strings.Contains(errorText, "размер") || strings.Contains(errorText, "не вернул") {
		return "", true, NewError(ErrorInvalidResponse, err)
	}
	return "", true, NewError(ErrorUnavailable, err)
}

func contextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return NewError(ErrorTimeout, err)
	}
	return NewError(ErrorUnavailable, err)
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.Statement) == "" {
		return errors.New("statement must not be empty")
	}
	if utf8.RuneCountInString(request.Statement) > maxStatementRunes {
		return fmt.Errorf("statement must not exceed %d characters", maxStatementRunes)
	}
	switch request.Language {
	case LanguagePython, LanguageJava, LanguageCPP:
		return nil
	default:
		return errors.New("unsupported language")
	}
}

func promptData(request Request) PromptData {
	interfaceRule := "Для Python и C++ внешний интерфейс — одна вызываемая функция."
	if request.Language == LanguageJava {
		interfaceRule = "Для Java выведи минимальный класс-контейнер с одним вызываемым методом."
	}
	return PromptData{Language: string(request.Language), InterfaceRule: interfaceRule, Statement: request.Statement}
}

func (s *Service) methodMessages(method Method, request Request) ([]llm.Message, error) {
	data := promptData(request)
	name := promptDirectSystem
	switch method {
	case MethodStepByStep:
		name = promptStepByStepSystem
	case MethodExperts:
		name = promptExpertsSystem
	}
	system, err := s.prompts.render(name, data)
	if err != nil {
		return nil, err
	}
	return []llm.Message{{Role: "system", Content: system}, {Role: "user", Content: request.Statement}}, nil
}

func (s *Service) generatedPromptMessages(request Request) ([]llm.Message, error) {
	data := promptData(request)
	system, err := s.prompts.render(promptMetaPromptGenerationSystem, data)
	if err != nil {
		return nil, err
	}
	return []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: request.Statement},
	}, nil
}

func (s *Service) generatedSolutionMessages(request Request, generated string) ([]llm.Message, error) {
	data := promptData(request)
	data.GeneratedPrompt = generated
	system, err := s.prompts.render(promptMetaSolutionSystem, data)
	if err != nil {
		return nil, err
	}
	user, err := s.prompts.render(promptMetaSolutionUser, data)
	if err != nil {
		return nil, err
	}
	return []llm.Message{{Role: "system", Content: system}, {Role: "user", Content: user}}, nil
}

func traceMessages(messages []llm.Message) []Message {
	trace := make([]Message, len(messages))
	for index, message := range messages {
		trace[index] = Message{Role: message.Role, Content: message.Content}
	}
	return trace
}
