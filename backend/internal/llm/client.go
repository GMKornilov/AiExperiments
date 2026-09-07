// Package llm provides a minimal OpenAI-compatible chat-completions client.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

// Message is one ordered OpenAI-compatible chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ErrorKind is a safe category for one upstream attempt.
type ErrorKind string

const (
	ErrorNetwork         ErrorKind = "network"
	ErrorTimeout         ErrorKind = "timeout"
	ErrorProvider        ErrorKind = "provider"
	ErrorInvalidResponse ErrorKind = "invalid_response"
)

// Error retains a category while keeping provider details private to this package.
type Error struct {
	Kind ErrorKind
	err  error
}

func (e *Error) Error() string { return string(e.Kind) }
func (e *Error) Unwrap() error { return e.err }

// Client issues one completion request without retries.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a client whose timeout is taken from an immutable snapshot.
func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, httpClient: &http.Client{Timeout: timeout}}
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// ChatMessages sends the supplied messages in order and returns the first answer.
func (c *Client) ChatMessages(ctx context.Context, model string, messages []Message) (string, error) {
	if strings.TrimSpace(c.baseURL) == "" || strings.TrimSpace(c.apiKey) == "" || strings.TrimSpace(model) == "" || len(messages) == 0 {
		return "", &Error{Kind: ErrorInvalidResponse, err: errors.New("invalid completion configuration")}
	}
	for _, message := range messages {
		if (message.Role != "system" && message.Role != "user" && message.Role != "assistant") || strings.TrimSpace(message.Content) == "" {
			return "", &Error{Kind: ErrorInvalidResponse, err: errors.New("invalid message")}
		}
	}
	body, err := json.Marshal(chatRequest{Model: model, Messages: messages})
	if err != nil {
		return "", &Error{Kind: ErrorInvalidResponse, err: err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", &Error{Kind: ErrorInvalidResponse, err: err}
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", &Error{Kind: errorKind(err), err: err}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", &Error{Kind: ErrorProvider, err: fmt.Errorf("provider returned HTTP %d", response.StatusCode)}
	}
	if !strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "application/json") {
		return "", &Error{Kind: ErrorInvalidResponse, err: errors.New("response is not JSON")}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return "", &Error{Kind: errorKind(err), err: err}
	}
	if len(data) > maxResponseBytes {
		return "", &Error{Kind: ErrorInvalidResponse, err: errors.New("response exceeds limit")}
	}
	var decoded chatResponse
	if err := json.Unmarshal(data, &decoded); err != nil || len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return "", &Error{Kind: ErrorInvalidResponse, err: errors.New("invalid completion response")}
	}
	return decoded.Choices[0].Message.Content, nil
}

func errorKind(err error) ErrorKind {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorTimeout
	}
	if errors.Is(err, context.Canceled) {
		return ErrorNetwork
	}
	return ErrorNetwork
}
