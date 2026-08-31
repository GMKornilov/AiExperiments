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
)

// Client sends chat completion requests.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
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
	if strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("модель не должна быть пустой")
	}
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("сообщение не должно быть пустым")
	}

	body, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []message{{
			Role:    "user",
			Content: prompt,
		}},
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
