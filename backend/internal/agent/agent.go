// Package agent implements the state and context of one barista conversation.
package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"aichallenge/week_1/task_1/internal/llm"
)

const MaxUserRunes = 4000

// ErrorCategory is a safe public category for an unsuccessful LLM attempt.
type ErrorCategory string

const (
	ErrorTimeout         ErrorCategory = "timeout"
	ErrorNetwork         ErrorCategory = "network"
	ErrorProvider        ErrorCategory = "provider"
	ErrorInvalidResponse ErrorCategory = "invalid_response"
)

// AttemptError lets a provider expose a safe error category without its details.
type AttemptError struct {
	Category ErrorCategory
	Err      error
}

func (e *AttemptError) Error() string { return e.Err.Error() }
func (e *AttemptError) Unwrap() error { return e.Err }

// Status describes the delivery state of a user message.
type Status string

const (
	StatusPending Status = "pending"
	StatusError   Status = "error"
	StatusSuccess Status = "success"
)

// Snapshot is the immutable LLM setup captured when a dialog is created.
type Snapshot struct {
	BaseURL      string
	APIKey       string
	Model        string
	SystemPrompt string
	Timeout      time.Duration
	Temperature  float64
}

// DialogSnapshot captures immutable configurations for chat and title generation.
type DialogSnapshot struct {
	Chat Snapshot
	Text Snapshot
}

func (s DialogSnapshot) Validate() error {
	if err := s.Chat.Validate(); err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	if err := s.Text.Validate(); err != nil {
		return fmt.Errorf("text: %w", err)
	}
	return nil
}

// Validate checks the part of an LLM setup used by a conversation.
func (s Snapshot) Validate() error {
	if strings.TrimSpace(s.BaseURL) == "" {
		return fmt.Errorf("URL провайдера не должен быть пустым")
	}
	if strings.TrimSpace(s.APIKey) == "" {
		return fmt.Errorf("ключ провайдера не должен быть пустым")
	}
	if strings.TrimSpace(s.Model) == "" {
		return fmt.Errorf("модель не должна быть пустой")
	}
	if strings.TrimSpace(s.SystemPrompt) == "" {
		return fmt.Errorf("system prompt не должен быть пустым")
	}
	if s.Timeout <= 0 {
		return fmt.Errorf("таймаут LLM должен быть больше нуля")
	}
	if math.IsNaN(s.Temperature) || math.IsInf(s.Temperature, 0) || s.Temperature < 0 || s.Temperature > 2 {
		return fmt.Errorf("temperature должна быть конечным числом от 0 до 2")
	}
	return nil
}

// Message is a persisted dialog message. ClientID exists only on user messages.
type Message struct {
	ID            string    `json:"id"`
	ClientID      string    `json:"client_message_id,omitempty"`
	Role          string    `json:"role"`
	Text          string    `json:"text"`
	Status        Status    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	ErrorCategory string    `json:"error_category,omitempty"`
}

// Provider performs exactly one upstream completion.
type Provider interface {
	Complete(context.Context, Snapshot, []llm.Message) (string, error)
}

// Conversation owns a snapshot and message history for one dialog.
type Conversation struct {
	mu       sync.Mutex
	snapshot Snapshot
	messages []Message
	nextID   uint64
}

// NewConversation creates an empty conversation using snapshot.
func NewConversation(snapshot Snapshot) (*Conversation, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	return &Conversation{snapshot: snapshot}, nil
}

// Snapshot returns the dialog's immutable setup.
func (c *Conversation) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshot
}

// Messages returns a copy of history.
func (c *Conversation) Messages() []Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Message(nil), c.messages...)
}

// FindClientMessage finds a user message accepted under a client-generated ID.
func (c *Conversation) FindClientMessage(clientID string) (Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, message := range c.messages {
		if message.Role == "user" && message.ClientID == clientID {
			return message, true
		}
	}
	return Message{}, false
}

// Begin accepts a new user message and marks it pending.
func (c *Conversation) Begin(clientID, text string) (Message, error) {
	if strings.TrimSpace(clientID) == "" {
		return Message{}, fmt.Errorf("client message ID не должен быть пустым")
	}
	if strings.TrimSpace(text) == "" {
		return Message{}, fmt.Errorf("сообщение не должно быть пустым")
	}
	if utf8.RuneCountInString(text) > MaxUserRunes {
		return Message{}, fmt.Errorf("сообщение не должно превышать %d символов", MaxUserRunes)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, existing := range c.messages {
		if existing.Role == "user" && existing.Status != StatusSuccess {
			return Message{}, fmt.Errorf("предыдущее сообщение не завершено")
		}
	}
	c.nextID++
	message := Message{ID: fmt.Sprintf("m-%d", c.nextID), ClientID: clientID, Role: "user", Text: text, Status: StatusPending, CreatedAt: time.Now()}
	c.messages = append(c.messages, message)
	return message, nil
}

// Retry marks an errored user message pending again.
func (c *Conversation) Retry(messageID string) (Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for index := range c.messages {
		message := &c.messages[index]
		if message.ID != messageID {
			continue
		}
		if message.Role != "user" || message.Status != StatusError {
			return Message{}, fmt.Errorf("сообщение нельзя повторить")
		}
		message.Status = StatusPending
		message.ErrorCategory = ""
		return *message, nil
	}
	return Message{}, fmt.Errorf("сообщение не найдено")
}

// Context builds the exact context for a pending user message.
func (c *Conversation) Context(messageID string) ([]llm.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	messages := []llm.Message{{Role: "system", Content: c.snapshot.SystemPrompt}}
	for index := range c.messages {
		message := c.messages[index]
		if message.Role == "user" && message.ID == messageID {
			if message.Status != StatusPending {
				return nil, fmt.Errorf("сообщение не ожидает ответа")
			}
			messages = append(messages, llm.Message{Role: "user", Content: message.Text})
			return messages, nil
		}
		if message.Role == "user" && message.Status == StatusSuccess && index+1 < len(c.messages) {
			assistant := c.messages[index+1]
			if assistant.Role == "assistant" && assistant.Status == StatusSuccess {
				messages = append(messages, llm.Message{Role: "user", Content: message.Text}, llm.Message{Role: "assistant", Content: assistant.Text})
			}
		}
	}
	return nil, fmt.Errorf("сообщение не найдено")
}

// Finish stores exactly one assistant message on success, or marks the user errored.
func (c *Conversation) Finish(messageID, answer string, attemptErr error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for index := range c.messages {
		message := &c.messages[index]
		if message.ID != messageID {
			continue
		}
		if message.Role != "user" || message.Status != StatusPending {
			return fmt.Errorf("попытка больше не активна")
		}
		if attemptErr != nil || strings.TrimSpace(answer) == "" {
			message.Status = StatusError
			message.ErrorCategory = errorCategory(attemptErr)
			return nil
		}
		message.Status = StatusSuccess
		c.nextID++
		c.messages = append(c.messages, Message{ID: fmt.Sprintf("m-%d", c.nextID), Role: "assistant", Text: answer, Status: StatusSuccess, CreatedAt: time.Now()})
		return nil
	}
	return fmt.Errorf("сообщение не найдено")
}

// Attempt builds context and performs exactly one provider call for a pending message.
func (c *Conversation) Attempt(ctx context.Context, provider Provider, messageID string) error {
	if provider == nil {
		return c.Finish(messageID, "", fmt.Errorf("провайдер LLM не настроен"))
	}
	messages, err := c.Context(messageID)
	if err != nil {
		return err
	}
	answer, attemptErr := provider.Complete(ctx, c.Snapshot(), messages)
	return c.Finish(messageID, answer, attemptErr)
}

func errorCategory(err error) string {
	if err == nil {
		return "invalid_response"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var typed *AttemptError
	if errors.As(err, &typed) && typed.Category != "" {
		return string(typed.Category)
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return string(ErrorNetwork)
	}
	return string(ErrorProvider)
}
