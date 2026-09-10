// Package agent implements the state and context of one barista conversation.
package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
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
	ErrorContextLimit    ErrorCategory = "context_limit"
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
	BaseURL      string        `json:"base_url"`
	APIKey       string        `json:"-"`
	Model        string        `json:"model"`
	SystemPrompt string        `json:"system_prompt"`
	Timeout      time.Duration `json:"timeout"`
	Temperature  float64       `json:"temperature"`
}

// DialogSnapshot captures immutable configurations for chat and title generation.
type DialogSnapshot struct {
	Chat Snapshot `json:"chat"`
	Text Snapshot `json:"text"`
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
type Attempt struct {
	ID            string     `json:"id"`
	Usage         *llm.Usage `json:"usage,omitempty"`
	ErrorCategory string     `json:"error_category,omitempty"`
}
type Message struct {
	Usage         *llm.Usage `json:"usage,omitempty"`
	Attempts      []Attempt  `json:"attempts,omitempty"`
	ID            string     `json:"id"`
	ClientID      string     `json:"client_message_id,omitempty"`
	Role          string     `json:"role"`
	Text          string     `json:"text"`
	Status        Status     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	ErrorCategory string     `json:"error_category,omitempty"`
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

// RestoreConversation validates saved history and makes interrupted work retryable.
func RestoreConversation(snapshot Snapshot, messages []Message) (*Conversation, error) {
	c, err := NewConversation(snapshot)
	if err != nil {
		return nil, err
	}
	c.messages = CloneMessages(messages)
	var accounted int64
	clients := make(map[string]bool)
	for i := range c.messages {
		m := &c.messages[i]
		if m.Usage != nil && (!m.Usage.Valid() || m.Role != "assistant") {
			return nil, fmt.Errorf("некорректная статистика сообщения")
		}
		for j, attempt := range m.Attempts {
			if m.Role != "user" || attempt.ID != fmt.Sprintf("%s-a-%d", m.ID, j+1) {
				return nil, fmt.Errorf("некорректный учёт попыток")
			}
			if attempt.Usage != nil {
				if !attempt.Usage.Valid() || attempt.Usage.PromptTokens+attempt.Usage.CompletionTokens > llm.MaxSafeTokens-accounted {
					return nil, fmt.Errorf("некорректный расход токенов")
				}
				accounted += attempt.Usage.PromptTokens + attempt.Usage.CompletionTokens
			}
		}
		id, err := strconv.ParseUint(strings.TrimPrefix(m.ID, "m-"), 10, 64)
		if err != nil || m.ID != fmt.Sprintf("m-%d", id) || id != uint64(i+1) || strings.TrimSpace(m.Text) == "" || m.CreatedAt.IsZero() {
			return nil, fmt.Errorf("некорректная история сообщений")
		}
		if i%2 == 0 {
			if m.Role != "user" || m.ClientID == "" || clients[m.ClientID] {
				return nil, fmt.Errorf("некорректная user-реплика")
			}
			clients[m.ClientID] = true
			if m.Status == StatusPending {
				m.Status = StatusError
				m.ErrorCategory = "cancelled"
				if len(m.Attempts) > 0 {
					m.Attempts[len(m.Attempts)-1].ErrorCategory = "cancelled"
				}
			}
			if (m.Status == StatusError && i != len(messages)-1) || (m.Status == StatusSuccess && i+1 >= len(messages)) || (m.Status != StatusError && m.Status != StatusSuccess) {
				return nil, fmt.Errorf("некорректный статус user-реплики")
			}
		} else if m.Role != "assistant" || m.Status != StatusSuccess || m.ClientID != "" {
			return nil, fmt.Errorf("некорректная assistant-реплика")
		}
		if m.Role == "assistant" {
			attempts := c.messages[i-1].Attempts
			if len(attempts) > 0 {
				last := attempts[len(attempts)-1]
				if last.ErrorCategory != "" || (last.Usage == nil) != (m.Usage == nil) || (m.Usage != nil && *m.Usage != *last.Usage) {
					return nil, fmt.Errorf("статистика ответа не соответствует попытке")
				}
			} else if m.Usage != nil {
				return nil, fmt.Errorf("отсутствует попытка для статистики ответа")
			}
		}
		c.nextID = id
	}
	return c, nil
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
	return CloneMessages(c.messages)
}

// FindClientMessage finds a user message accepted under a client-generated ID.
func (c *Conversation) FindClientMessage(clientID string) (Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, message := range c.messages {
		if message.Role == "user" && message.ClientID == clientID {
			return CloneMessages([]Message{message})[0], true
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
	message.Attempts = []Attempt{{ID: message.ID + "-a-1"}}
	c.messages = append(c.messages, message)
	return CloneMessages([]Message{message})[0], nil
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
		message.Attempts = append(message.Attempts, Attempt{ID: fmt.Sprintf("%s-a-%d", message.ID, len(message.Attempts)+1)})
		return CloneMessages([]Message{*message})[0], nil
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
	return c.finishCompletion(messageID, llm.Completion{Text: answer}, attemptErr)
}
func (c *Conversation) finishCompletion(messageID string, result llm.Completion, attemptErr error) error {
	answer := result.Text
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
		usage := result.Usage
		if usage != nil {
			copied := *usage
			usage = &copied
			if !usage.Valid() || usage.PromptTokens+usage.CompletionTokens > llm.MaxSafeTokens-c.accountedTokensLocked() {
				usage = nil
			}
		}
		attempt := Attempt{ID: message.Attempts[len(message.Attempts)-1].ID, Usage: usage}
		if attemptErr != nil || strings.TrimSpace(answer) == "" {
			attempt.ErrorCategory = errorCategory(attemptErr)
		}
		message.Attempts[len(message.Attempts)-1] = attempt
		if attemptErr != nil || strings.TrimSpace(answer) == "" {
			message.Status = StatusError
			message.ErrorCategory = errorCategory(attemptErr)
			return nil
		}
		message.Status = StatusSuccess
		c.nextID++
		c.messages = append(c.messages, Message{ID: fmt.Sprintf("m-%d", c.nextID), Role: "assistant", Text: answer, Usage: usage, Status: StatusSuccess, CreatedAt: time.Now()})
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
	if metered, ok := provider.(interface {
		CompleteWithUsage(context.Context, Snapshot, []llm.Message) (llm.Completion, error)
	}); ok {
		result, attemptErr := metered.CompleteWithUsage(ctx, c.Snapshot(), messages)
		return c.finishCompletion(messageID, result, attemptErr)
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

// AccountedTokens derives the total from completed chat attempts, never reads.
func (c *Conversation) AccountedTokens() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accountedTokensLocked()
}
func (c *Conversation) accountedTokensLocked() int64 {
	return AccountedTokens(c.messages)
}

// AccountedTokens sums the confirmed ledger entries in a validated history snapshot.
func AccountedTokens(messages []Message) int64 {
	var total int64
	for _, message := range messages {
		for _, attempt := range message.Attempts {
			if attempt.Usage != nil {
				total += attempt.Usage.PromptTokens + attempt.Usage.CompletionTokens
			}
		}
	}
	return total
}

// CloneMessages protects the mutable history and its nested usage records.
func CloneMessages(messages []Message) []Message {
	result := append([]Message(nil), messages...)
	for i := range result {
		if result[i].Usage != nil {
			usage := *result[i].Usage
			result[i].Usage = &usage
		}
		result[i].Attempts = append([]Attempt(nil), result[i].Attempts...)
		for j := range result[i].Attempts {
			if result[i].Attempts[j].Usage != nil {
				usage := *result[i].Attempts[j].Usage
				result[i].Attempts[j].Usage = &usage
			}
		}
	}
	return result
}
