// Package barista provides shared routing for barista chat modes.
package barista

import (
	"context"
	"fmt"
	"strings"

	"aichallenge/week_1/task_1/internal/config"
	"aichallenge/week_1/task_1/internal/llm"
)

// Mode selects the response contract for a chat request.
type Mode string

const (
	// ModeFree returns a free-form barista answer.
	ModeFree Mode = "free"
	// ModeControlled returns a schema-validated JSON barista answer.
	ModeControlled Mode = "controlled"
)

// Service routes requests to the configured LLM client.
type Service struct {
	config config.Config
	client *llm.Client
}

// NewService creates a service with the configuration's API settings.
func NewService(cfg config.Config) *Service {
	return NewServiceWithClient(cfg, llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.RequestTimeout))
}

// NewServiceWithClient creates a service with a shared LLM client.
func NewServiceWithClient(cfg config.Config, client *llm.Client) *Service {
	return &Service{
		config: cfg,
		client: client,
	}
}

// Chat sends one prompt using the requested mode.
func (s *Service) Chat(ctx context.Context, mode Mode, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("сообщение не должно быть пустым")
	}

	switch mode {
	case ModeFree:
		return s.client.ChatWithSystemPrompt(ctx, s.config.Model, s.config.FreeSystemPrompt, prompt)
	case ModeControlled:
		return s.client.ChatControlled(ctx, s.config.Model, prompt, s.config.ControlledSystemPrompt, s.config.ResponseSchema)
	default:
		return "", fmt.Errorf("неподдерживаемый режим %q", mode)
	}
}
