// Package llm provides a minimal OpenAI-compatible chat-completions client.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
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
	ErrorContextLimit    ErrorKind = "context_limit"
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
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// ChatMessages sends the supplied messages in order and returns the first answer.
func (c *Client) ChatMessages(ctx context.Context, model string, messages []Message, temperature float64) (string, error) {
	result, err := c.ChatCompletion(ctx, model, messages, temperature)
	return result.Text, err
}
func (c *Client) ChatCompletion(ctx context.Context, model string, messages []Message, temperature float64) (completion Completion, completionErr error) {
	started := time.Now()
	status := 0
	defer func() {
		if completionErr == nil {
			return
		}
		var typed *Error
		category := ErrorProvider
		if errors.As(completionErr, &typed) {
			category = typed.Kind
		}
		slog.Warn("llm_request_failed", "correlation_id", RequestID(ctx), "attempt_id", AttemptID(ctx), "http_status", status, "error_category", category, "result", "failure", "duration_ms", time.Since(started).Milliseconds())
	}()
	if strings.TrimSpace(c.baseURL) == "" || strings.TrimSpace(c.apiKey) == "" || strings.TrimSpace(model) == "" || len(messages) == 0 {
		return Completion{}, &Error{Kind: ErrorInvalidResponse, err: errors.New("invalid completion configuration")}
	}
	if math.IsNaN(temperature) || math.IsInf(temperature, 0) || temperature < 0 || temperature > 2 {
		return Completion{}, &Error{Kind: ErrorInvalidResponse, err: errors.New("invalid temperature")}
	}
	for _, message := range messages {
		if (message.Role != "system" && message.Role != "user" && message.Role != "assistant") || strings.TrimSpace(message.Content) == "" {
			return Completion{}, &Error{Kind: ErrorInvalidResponse, err: errors.New("invalid message")}
		}
	}
	body, err := json.Marshal(chatRequest{Model: model, Messages: messages, Temperature: temperature})
	if err != nil {
		return Completion{}, &Error{Kind: ErrorInvalidResponse, err: err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Completion{}, &Error{Kind: ErrorInvalidResponse, err: err}
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return Completion{}, &Error{Kind: errorKind(err), err: err}
	}
	defer response.Body.Close()
	status = response.StatusCode
	if !strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "application/json") {
		kind := ErrorInvalidResponse
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			kind = ErrorProvider
		}
		return Completion{}, &Error{Kind: kind, err: errors.New("response is not JSON")}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return Completion{}, &Error{Kind: errorKind(err), err: err}
	}
	if len(data) > maxResponseBytes {
		return Completion{}, &Error{Kind: ErrorInvalidResponse, err: errors.New("response exceeds limit")}
	}
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
		Error struct {
			Code    string `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &envelope)
	result := Completion{Usage: parseUsage(envelope.Usage)}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		kind := ErrorProvider
		if isContextLimit(envelope.Error.Code, envelope.Error.Type, envelope.Error.Message) {
			kind = ErrorContextLimit
		}
		return result, &Error{Kind: kind, err: fmt.Errorf("provider returned HTTP %d", response.StatusCode)}
	}
	var decoded chatResponse
	if err := json.Unmarshal(data, &decoded); err != nil || len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return result, &Error{Kind: ErrorInvalidResponse, err: errors.New("invalid completion response")}
	}
	result.Text = decoded.Choices[0].Message.Content
	return result, nil
}

func isContextLimit(code, errorType, message string) bool {
	for _, value := range []string{code, errorType} {
		if value == "context_length_exceeded" || value == "context_window_exceeded" {
			return true
		}
	}
	// DeepSeek uses invalid_request_error even for context overflow.
	message = strings.ToLower(message)
	return strings.Contains(message, "maximum context length is") &&
		strings.Contains(message, "however, you requested") &&
		strings.Contains(message, "please reduce the length")
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
