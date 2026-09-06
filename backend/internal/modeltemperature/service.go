// Package modeltemperature provides one-shot provider-, model- and temperature-controlled completions.
package modeltemperature

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"aichallenge/week_1/task_1/internal/llm"
)

// Provider identifies an LLM API provider configured on the backend.
type Provider string

const (
	ProviderDeepSeek Provider = "deepseek"
	ProviderKimi     Provider = "kimi"
)

// Model is a supported provider model identifier.
type Model string

const (
	ModelV4Flash          Model = "deepseek-v4-flash"
	ModelV4Pro            Model = "deepseek-v4-pro"
	ModelV4FlashVisionExp Model = "deepseek-v4-flash-vision-exp"
	ModelKimiK3           Model = "kimi-k3"
	ModelKimiK27Code      Model = "kimi-k2.7-code"
	ModelKimiK26          Model = "kimi-k2.6"
)

// Metrics contains observable timing, token usage and calculated request cost.
type Metrics struct {
	DurationMilliseconds int64   `json:"duration_ms"`
	InputTokens          int     `json:"input_tokens"`
	OutputTokens         int     `json:"output_tokens"`
	CostUSD              float64 `json:"cost_usd"`
}

// Result is a completed answer and its request metrics.
type Result struct {
	Answer  string
	Metrics Metrics
}

// IsSupportedProvider reports whether provider is exposed by this application.
func IsSupportedProvider(provider Provider) bool {
	return provider == ProviderDeepSeek || provider == ProviderKimi
}

// IsSupportedModel reports whether model belongs to the selected provider.
func IsSupportedModel(provider Provider, model Model) bool {
	switch provider {
	case ProviderDeepSeek:
		return model == ModelV4Flash || model == ModelV4Pro || model == ModelV4FlashVisionExp
	case ProviderKimi:
		return model == ModelKimiK3 || model == ModelKimiK27Code || model == ModelKimiK26
	default:
		return false
	}
}

// IsSupportedTemperature respects fixed sampling parameters of Kimi thinking models.
func IsSupportedTemperature(provider Provider, temperature float64) bool {
	if math.IsNaN(temperature) || math.IsInf(temperature, 0) {
		return false
	}
	if provider == ProviderKimi {
		return temperature == 1
	}
	return provider == ProviderDeepSeek && temperature >= 0 && temperature <= 2
}

// Client is the LLM capability required by Service.
type Client interface {
	ChatWithTemperatureMetrics(context.Context, string, string, float64) (llm.Completion, error)
}

// Service executes independent completions against provider-specific clients.
type Service struct {
	deepSeekClient Client
	kimiClient     Client
	now            func() time.Time
}

// NewService creates a model-and-temperature completion service.
func NewService(deepSeekClient, kimiClient Client) *Service {
	return &Service{deepSeekClient: deepSeekClient, kimiClient: kimiClient, now: time.Now}
}

// Complete sends one user prompt with the chosen provider, model and temperature.
func (s *Service) Complete(ctx context.Context, prompt string, temperature float64, provider Provider, model Model) (Result, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Result{}, fmt.Errorf("prompt не должен быть пустым")
	}
	if !IsSupportedProvider(provider) {
		return Result{}, fmt.Errorf("провайдер не поддерживается")
	}
	if !IsSupportedTemperature(provider, temperature) {
		return Result{}, fmt.Errorf("temperature вне диапазона провайдера")
	}
	if !IsSupportedModel(provider, model) {
		return Result{}, fmt.Errorf("модель не поддерживается провайдером")
	}

	client := s.deepSeekClient
	if provider == ProviderKimi {
		client = s.kimiClient
	}
	if client == nil {
		slog.Warn("llm.configuration", "request_id", llm.RequestID(ctx), "provider", provider, "model", model, "reason", "provider_not_configured")
		return Result{}, fmt.Errorf("провайдер не настроен")
	}

	startedAt := s.now()
	completion, err := client.ChatWithTemperatureMetrics(ctx, string(model), prompt, temperature)
	finishedAt := s.now()
	if err != nil {
		return Result{}, err
	}
	answer := strings.TrimSpace(completion.Answer)
	if answer == "" {
		return Result{}, fmt.Errorf("LLM вернул пустой ответ")
	}

	return Result{
		Answer: answer,
		Metrics: Metrics{
			DurationMilliseconds: max(finishedAt.Sub(startedAt).Milliseconds(), 0),
			InputTokens:          completion.Usage.PromptTokens,
			OutputTokens:         completion.Usage.CompletionTokens,
			CostUSD:              requestCostUSD(provider, model, completion.Usage, startedAt),
		},
	}, nil
}

type tokenRates struct {
	input  float64
	cached float64
	output float64
}

func requestCostUSD(provider Provider, model Model, usage llm.Usage, requestedAt time.Time) float64 {
	rates := ratesFor(provider, model, requestedAt)
	cached := max(usage.CachedTokens, usage.PromptCacheHitTokens, usage.PromptTokensDetails.CachedTokens)
	cached = min(max(cached, 0), usage.PromptTokens)
	uncached := usage.PromptTokens - cached
	return (float64(uncached)*rates.input + float64(cached)*rates.cached + float64(usage.CompletionTokens)*rates.output) / 1_000_000
}

func ratesFor(provider Provider, model Model, requestedAt time.Time) tokenRates {
	if provider == ProviderDeepSeek {
		multiplier := 0.5
		if isDeepSeekPeak(requestedAt) {
			multiplier = 1
		}
		if model == ModelV4Pro {
			return tokenRates{input: 1.32 * multiplier, cached: 0.044 * multiplier, output: 3.96 * multiplier}
		}
		return tokenRates{input: 0.44 * multiplier, cached: 0.014 * multiplier, output: 1.32 * multiplier}
	}

	if model == ModelKimiK3 {
		return tokenRates{input: 3, cached: 0.30, output: 15}
	}
	if model == ModelKimiK27Code {
		return tokenRates{input: 0.95, cached: 0.19, output: 4}
	}
	return tokenRates{input: 0.95, cached: 0.16, output: 4}
}

func isDeepSeekPeak(value time.Time) bool {
	hour := value.UTC().Hour()
	return (hour >= 1 && hour < 4) || (hour >= 6 && hour < 10)
}
