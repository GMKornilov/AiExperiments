// Package temperature provides one-shot temperature-controlled completions.
package temperature

import (
	"context"
	"fmt"
	"math"
	"strings"

	"aichallenge/week_1/task_1/internal/config"
	"aichallenge/week_1/task_1/internal/llm"
)

// Client is the LLM capability required by Service.
type Client interface {
	ChatWithTemperature(context.Context, string, string, float64) (string, error)
}

// Service executes independent completions with a configured model.
type Service struct {
	model  string
	client Client
}

// NewService creates a temperature service using the configuration's LLM settings.
func NewService(cfg config.Config) *Service {
	return NewServiceWithClient(cfg, llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.RequestTimeout))
}

// NewServiceWithClient creates a temperature service with the supplied LLM client.
func NewServiceWithClient(cfg config.Config, client Client) *Service {
	return &Service{model: cfg.Model, client: client}
}

// Complete sends one user prompt with the chosen temperature.
func (s *Service) Complete(ctx context.Context, prompt string, value float64) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt не должен быть пустым")
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 2 {
		return "", fmt.Errorf("temperature должна быть конечным числом от 0 до 2")
	}
	if s.client == nil {
		return "", fmt.Errorf("LLM-клиент не настроен")
	}

	answer, err := s.client.ChatWithTemperature(ctx, s.model, prompt, value)
	if err != nil {
		return "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", fmt.Errorf("LLM вернул пустой ответ")
	}
	return answer, nil
}
