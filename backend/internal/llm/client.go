// Package llm provides a client for OpenAI-compatible chat APIs.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Client sends chat completion requests.
type Client struct {
	baseURL     string
	apiKey      string
	httpClient  *http.Client
	logger      *slog.Logger
	logPayloads bool
}

type chatRequest struct {
	Model           string          `json:"model"`
	Messages        []Message       `json:"messages"`
	ResponseFormat  *responseFormat `json:"response_format,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	Thinking        *thinkingMode   `json:"thinking,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
}

type thinkingMode struct {
	Type string `json:"type"`
}

type generationSettings struct {
	thinking        *thinkingMode
	reasoningEffort string
}

type responseFormat struct {
	Type string `json:"type"`
}

// Message is one OpenAI-compatible chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// message remains an alias for package-internal compatibility.
type message = Message

type chatResponse struct {
	Choices []choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type choice struct {
	Message Message `json:"message"`
}

// Usage contains token accounting returned by an OpenAI-compatible provider.
type Usage struct {
	PromptTokens         int                `json:"prompt_tokens"`
	CompletionTokens     int                `json:"completion_tokens"`
	TotalTokens          int                `json:"total_tokens"`
	CachedTokens         int                `json:"cached_tokens"`
	PromptCacheHitTokens int                `json:"prompt_cache_hit_tokens"`
	PromptTokensDetails  PromptTokenDetails `json:"prompt_tokens_details"`
}

// PromptTokenDetails contains the cached portion of the input token count.
type PromptTokenDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// Completion is a text answer together with provider token accounting.
type Completion struct {
	Answer string
	Usage  Usage
}

const maxAlgorithmResponseBytes = 1 << 20

// ChatErrorKind classifies errors from the traceable ChatMessages path.
type ChatErrorKind string

const (
	// ChatErrorPreSend means validation or request construction failed before Do.
	ChatErrorPreSend ChatErrorKind = "pre_send"
	// ChatErrorInvalidResponse means the provider returned an unreadable response.
	ChatErrorInvalidResponse ChatErrorKind = "invalid_response"
)

// ChatError retains a category without exposing provider details to callers.
type ChatError struct {
	Kind ChatErrorKind
	err  error
}

func (e *ChatError) Error() string { return e.err.Error() }
func (e *ChatError) Unwrap() error { return e.err }

// NewClient creates a Client with its own HTTP client and request timeout.
func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	return &Client{
		logger:      slog.Default(),
		logPayloads: strings.EqualFold(os.Getenv("LLM_LOG_PAYLOADS"), "true"),
		baseURL:     strings.TrimRight(baseURL, "/"),
		apiKey:      apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Chat sends one user message and returns the first completion.
func (c *Client) Chat(ctx context.Context, model, prompt string) (string, error) {
	return c.chat(ctx, model, []Message{{Role: "user", Content: prompt}}, nil)
}

// ChatWithTemperature sends exactly one user message with an explicit temperature.
func (c *Client) ChatWithTemperature(ctx context.Context, model, prompt string, temperature float64) (string, error) {
	if math.IsNaN(temperature) || math.IsInf(temperature, 0) || temperature < 0 || temperature > 2 {
		return "", fmt.Errorf("temperature должна быть конечным числом от 0 до 2")
	}
	return c.chatWithLimit(ctx, model, []Message{{Role: "user", Content: prompt}}, nil, &temperature, 0, false)
}

// ChatWithTemperatureMetrics sends one user message and requires provider usage accounting.
func (c *Client) ChatWithTemperatureMetrics(ctx context.Context, model, prompt string, temperature float64) (Completion, error) {
	if RequestID(ctx) == "" {
		ctx = WithRequestID(ctx, "")
	}
	if math.IsNaN(temperature) || math.IsInf(temperature, 0) || temperature < 0 || temperature > 2 {
		return Completion{}, fmt.Errorf("temperature должна быть конечным числом от 0 до 2")
	}
	requestTemperature := &temperature
	switch model {
	case "kimi-k3", "kimi-k2.7-code", "kimi-k2.6":
		if temperature != 1 {
			return Completion{}, fmt.Errorf("Kimi поддерживает только temperature=1 в thinking-режиме")
		}
		// Kimi requires default sampling parameters to be omitted.
		requestTemperature = nil
	}
	settings := generationSettings{}
	switch model {
	case "deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp":
		settings.thinking = &thinkingMode{Type: "disabled"}
	case "kimi-k3":
		settings.reasoningEffort = "low"
	}
	completion, err := c.chatCompletionWithLimit(ctx, model, []Message{{Role: "user", Content: prompt}}, nil, requestTemperature, 0, false, settings)
	if err != nil {
		return Completion{}, err
	}
	cachedTokens := max(completion.Usage.CachedTokens, completion.Usage.PromptCacheHitTokens, completion.Usage.PromptTokensDetails.CachedTokens)
	if completion.Usage.PromptTokens <= 0 || completion.Usage.CompletionTokens <= 0 || completion.Usage.CachedTokens < 0 || completion.Usage.PromptCacheHitTokens < 0 || completion.Usage.PromptTokensDetails.CachedTokens < 0 || cachedTokens > completion.Usage.PromptTokens {
		c.logger.Warn("llm.validation", "request_id", RequestID(ctx), "model", model, "reason", "invalid_usage",
			"input_tokens", completion.Usage.PromptTokens, "output_tokens", completion.Usage.CompletionTokens,
			"cached_tokens", completion.Usage.CachedTokens, "prompt_cache_hit_tokens", completion.Usage.PromptCacheHitTokens,
			"details_cached_tokens", completion.Usage.PromptTokensDetails.CachedTokens)
		return Completion{}, fmt.Errorf("API вернул некорректную статистику токенов")
	}
	return completion, nil
}

// ChatWithSystemPrompt sends a free-form completion with a system and user message.
func (c *Client) ChatWithSystemPrompt(ctx context.Context, model, systemPrompt, prompt string) (string, error) {
	if strings.TrimSpace(systemPrompt) == "" {
		return "", fmt.Errorf("системный prompt не должен быть пустым")
	}
	return c.chat(
		ctx,
		model,
		[]Message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: prompt}},
		nil,
	)
}

// ChatControlled sends a completion request constrained by a system prompt and JSON Schema.
func (c *Client) ChatControlled(
	ctx context.Context,
	model, prompt, systemPrompt string,
	schema json.RawMessage,
) (string, error) {
	if strings.TrimSpace(systemPrompt) == "" {
		return "", fmt.Errorf("системный prompt не должен быть пустым")
	}
	if !isJSONObject(schema) {
		return "", fmt.Errorf("JSON Schema должна быть JSON-объектом")
	}
	compiledSchema, schemaDocument, err := compileSchema(schema)
	if err != nil {
		return "", err
	}

	answer, err := c.chat(
		ctx,
		model,
		[]Message{
			{Role: "system", Content: controlledSystemMessage(systemPrompt, schema)},
			{Role: "user", Content: prompt},
		},
		&responseFormat{
			Type: "json_object",
		},
	)
	if err != nil {
		return "", err
	}
	if err := validateControlledAnswer(answer, compiledSchema, schemaDocument); err != nil {
		return "", err
	}

	return answer, nil
}

// ChatMessages sends the supplied ordered messages and preserves answer whitespace.
// It is intended for algorithm prompts that must expose their exact trace.
func (c *Client) ChatMessages(ctx context.Context, model string, messages []Message) (string, error) {
	return c.chatWithLimit(ctx, model, messages, nil, nil, maxAlgorithmResponseBytes, true)
}

func (c *Client) chat(
	ctx context.Context,
	model string,
	messages []Message,
	responseFormat *responseFormat,
) (string, error) {
	return c.chatWithLimit(ctx, model, messages, responseFormat, nil, 0, false)
}

func (c *Client) chatWithLimit(
	ctx context.Context,
	model string,
	messages []Message,
	responseFormat *responseFormat,
	temperature *float64,
	responseLimit int64,
	preserveWhitespace bool,
) (string, error) {
	completion, err := c.chatCompletionWithLimit(ctx, model, messages, responseFormat, temperature, responseLimit, preserveWhitespace)
	if err != nil {
		return "", err
	}
	return completion.Answer, nil
}

func (c *Client) chatCompletionWithLimit(
	ctx context.Context,
	model string,
	messages []Message,
	responseFormat *responseFormat,
	temperature *float64,
	responseLimit int64,
	preserveWhitespace bool,
	settings ...generationSettings,
) (result Completion, resultErr error) {
	if RequestID(ctx) == "" {
		ctx = WithRequestID(ctx, "")
	}
	startedAt := time.Now()
	status := 0
	reason := "validation"
	stage := "validation"
	logger := c.logger.With("request_id", RequestID(ctx), "model", model)
	logger.Info("llm.start", "timeout_ms", c.httpClient.Timeout.Milliseconds())
	defer func() {
		level := slog.LevelWarn
		if reason == "success" {
			level = slog.LevelInfo
		}
		attributes := []any{"duration_ms", time.Since(startedAt).Milliseconds(), "http_status", status, "reason", reason, "stage", stage}
		if c.logPayloads && resultErr != nil {
			attributes = append(attributes, "error", c.redactDiagnostic(resultErr.Error()))
		}
		logger.Log(ctx, level, "llm.finish", attributes...)
	}()
	if strings.TrimSpace(model) == "" {
		return Completion{}, c.traceableError(preserveWhitespace, ChatErrorPreSend, fmt.Errorf("модель не должна быть пустой"))
	}
	if len(messages) == 0 || strings.TrimSpace(messages[len(messages)-1].Content) == "" {
		return Completion{}, c.traceableError(preserveWhitespace, ChatErrorPreSend, fmt.Errorf("сообщение не должно быть пустым"))
	}

	generation := generationSettings{}
	if len(settings) > 0 {
		generation = settings[0]
	}
	body, err := json.Marshal(chatRequest{
		Model:           model,
		Messages:        messages,
		ResponseFormat:  responseFormat,
		Temperature:     temperature,
		Thinking:        generation.thinking,
		ReasoningEffort: generation.reasoningEffort,
	})
	if err != nil {
		return Completion{}, c.traceableError(preserveWhitespace, ChatErrorPreSend, fmt.Errorf("кодирование запроса: %w", err))
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return Completion{}, c.traceableError(preserveWhitespace, ChatErrorPreSend, fmt.Errorf("создание HTTP-запроса: %w", err))
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")

	stage = "await_headers"
	if c.logPayloads {
		logger.Info("llm.request", "body", c.redactDiagnostic(string(body)))
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		reason = transportFailure(err)
		return Completion{}, fmt.Errorf("вызов LLM API: %w", err)
	}
	status = response.StatusCode
	stage = "read_body"
	defer response.Body.Close()

	reader := io.Reader(response.Body)
	if responseLimit > 0 {
		reader = io.LimitReader(response.Body, responseLimit+1)
	}
	responseBody, err := io.ReadAll(reader)
	if c.logPayloads {
		complete := err == nil && (responseLimit == 0 || int64(len(responseBody)) <= responseLimit)
		logger.Info("llm.response", "http_status", status, "received_bytes", len(responseBody),
			"complete", complete, "body", c.redactDiagnostic(string(responseBody)))
	}
	if err != nil {
		reason = "read_error"
		if kind := transportFailure(err); kind == "timeout" || kind == "canceled" {
			reason = kind
		}
		return Completion{}, fmt.Errorf("чтение ответа API: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		reason = "http_error"
		return Completion{}, fmt.Errorf("API вернул %s: %s", response.Status, responseBody)
	}
	stage = "parse_response"
	reason = "invalid_response"
	if preserveWhitespace && !isJSONContentType(response.Header.Get("Content-Type")) {
		return Completion{}, c.traceableError(preserveWhitespace, ChatErrorInvalidResponse, fmt.Errorf("ответ API имеет некорректный Content-Type"))
	}
	if responseLimit > 0 && int64(len(responseBody)) > responseLimit {
		return Completion{}, c.traceableError(preserveWhitespace, ChatErrorInvalidResponse, fmt.Errorf("ответ API превышает допустимый размер"))
	}

	completion, err := parseCompletion(responseBody, preserveWhitespace)
	if err != nil {
		return Completion{}, c.traceableError(preserveWhitespace, ChatErrorInvalidResponse, err)
	}
	reason = "success"
	stage = "complete"
	return completion, nil
}

func (c *Client) traceableError(traceable bool, kind ChatErrorKind, err error) error {
	if !traceable {
		return err
	}
	return &ChatError{Kind: kind, err: err}
}

func isJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "application/json")
}

func isJSONObject(value json.RawMessage) bool {
	var object map[string]any
	return json.Unmarshal(value, &object) == nil && object != nil
}

func controlledSystemMessage(systemPrompt string, schema json.RawMessage) string {
	return strings.TrimSpace(systemPrompt) + "\n" + string(schema)
}

func compileSchema(schema json.RawMessage) (*jsonschema.Schema, map[string]any, error) {
	var schemaDocument map[string]any
	if err := json.Unmarshal(schema, &schemaDocument); err != nil {
		return nil, nil, fmt.Errorf("разбор JSON Schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("barista-response.schema.json", schemaDocument); err != nil {
		return nil, nil, fmt.Errorf("загрузка JSON Schema: %w", err)
	}
	compiledSchema, err := compiler.Compile("barista-response.schema.json")
	if err != nil {
		return nil, nil, fmt.Errorf("проверка JSON Schema: %w", err)
	}

	return compiledSchema, schemaDocument, nil
}

func validateControlledAnswer(answer string, compiledSchema *jsonschema.Schema, schemaDocument map[string]any) error {
	var response any
	if err := json.Unmarshal([]byte(answer), &response); err != nil {
		return fmt.Errorf("контролируемый ответ не является JSON: %w", err)
	}
	if err := compiledSchema.Validate(response); err != nil {
		return fmt.Errorf("контролируемый ответ не соответствует JSON Schema: %w", err)
	}
	if err := validateWordLimits(schemaDocument, response, "ответ"); err != nil {
		return fmt.Errorf("контролируемый ответ нарушает ограничение длины: %w", err)
	}

	return nil
}

func validateWordLimits(schema any, value any, path string) error {
	schemaObject, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	if maximum, ok := wordLimit(schemaObject, "x-max-words"); ok {
		text, ok := value.(string)
		if ok && len(strings.Fields(text)) > maximum {
			return fmt.Errorf("%s содержит больше %d слов", path, maximum)
		}
	}
	if maximum, ok := wordLimit(schemaObject, "x-max-total-string-words"); ok {
		if actual := countStringWords(value); actual > maximum {
			return fmt.Errorf("%s содержит %d слов, максимум %d", path, actual, maximum)
		}
	}

	switch typedValue := value.(type) {
	case map[string]any:
		properties, _ := schemaObject["properties"].(map[string]any)
		for property, propertyValue := range typedValue {
			if propertySchema, ok := properties[property]; ok {
				if err := validateWordLimits(propertySchema, propertyValue, path+"."+property); err != nil {
					return err
				}
			}
		}
	case []any:
		itemsSchema, ok := schemaObject["items"]
		if !ok {
			return nil
		}
		for index, item := range typedValue {
			if err := validateWordLimits(itemsSchema, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}

	return nil
}

func wordLimit(schema map[string]any, key string) (int, bool) {
	value, ok := schema[key]
	if !ok {
		return 0, false
	}
	limit, ok := value.(float64)
	if !ok || limit < 0 || limit != float64(int(limit)) {
		return 0, false
	}
	return int(limit), true
}

func countStringWords(value any) int {
	switch typedValue := value.(type) {
	case string:
		return len(strings.Fields(typedValue))
	case []any:
		total := 0
		for _, item := range typedValue {
			total += countStringWords(item)
		}
		return total
	case map[string]any:
		total := 0
		for _, item := range typedValue {
			total += countStringWords(item)
		}
		return total
	default:
		return 0
	}
}

func parseCompletion(responseBody []byte, preserveWhitespace bool) (Completion, error) {
	var result chatResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return Completion{}, fmt.Errorf("разбор ответа API: %w", err)
	}
	if len(result.Choices) == 0 {
		return Completion{}, fmt.Errorf("API не вернул вариантов ответа")
	}

	answer := result.Choices[0].Message.Content
	if strings.TrimSpace(answer) == "" {
		return Completion{}, fmt.Errorf("API вернул пустой ответ")
	}
	if !preserveWhitespace {
		answer = strings.TrimSpace(answer)
	}

	return Completion{Answer: answer, Usage: result.Usage}, nil
}
