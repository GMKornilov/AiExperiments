// Package llm provides a client for OpenAI-compatible chat APIs.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Client sends chat completion requests.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []message       `json:"messages"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []choice `json:"choices"`
}

type choice struct {
	Message message `json:"message"`
}

// NewClient creates a Client with its own HTTP client and request timeout.
func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Chat sends one user message and returns the first completion.
func (c *Client) Chat(ctx context.Context, model, prompt string) (string, error) {
	return c.chat(ctx, model, []message{{Role: "user", Content: prompt}}, nil)
}

// ChatWithSystemPrompt sends a free-form completion with a system and user message.
func (c *Client) ChatWithSystemPrompt(ctx context.Context, model, systemPrompt, prompt string) (string, error) {
	if strings.TrimSpace(systemPrompt) == "" {
		return "", fmt.Errorf("системный prompt не должен быть пустым")
	}
	return c.chat(
		ctx,
		model,
		[]message{{Role: "system", Content: systemPrompt}, {Role: "user", Content: prompt}},
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
		[]message{
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

func (c *Client) chat(
	ctx context.Context,
	model string,
	messages []message,
	responseFormat *responseFormat,
) (string, error) {
	if strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("модель не должна быть пустой")
	}
	if len(messages) == 0 || strings.TrimSpace(messages[len(messages)-1].Content) == "" {
		return "", fmt.Errorf("сообщение не должно быть пустым")
	}

	body, err := json.Marshal(chatRequest{
		Model:          model,
		Messages:       messages,
		ResponseFormat: responseFormat,
	})
	if err != nil {
		return "", fmt.Errorf("кодирование запроса: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("создание HTTP-запроса: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("вызов LLM API: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("чтение ответа API: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("API вернул %s: %s", response.Status, responseBody)
	}

	return parseAnswer(responseBody)
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

func parseAnswer(responseBody []byte) (string, error) {
	var result chatResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return "", fmt.Errorf("разбор ответа API: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("API не вернул вариантов ответа")
	}

	answer := strings.TrimSpace(result.Choices[0].Message.Content)
	if answer == "" {
		return "", fmt.Errorf("API вернул пустой ответ")
	}

	return answer, nil
}
