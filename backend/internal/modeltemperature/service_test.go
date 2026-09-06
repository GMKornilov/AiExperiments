package modeltemperature

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/llm"
)

type fakeClient struct {
	calls       int
	model       string
	prompt      string
	temperature float64
	completion  llm.Completion
	err         error
}

func (c *fakeClient) ChatWithTemperatureMetrics(_ context.Context, model, prompt string, temperature float64) (llm.Completion, error) {
	c.calls++
	c.model = model
	c.prompt = prompt
	c.temperature = temperature
	return c.completion, c.err
}

func TestCompleteRoutesProviderAndReturnsMetrics(t *testing.T) {
	deepSeek := &fakeClient{}
	kimi := &fakeClient{completion: llm.Completion{Answer: "  ответ  ", Usage: llm.Usage{
		PromptTokens: 1000, CompletionTokens: 2000, CachedTokens: 100,
	}}}
	service := NewService(deepSeek, kimi)
	times := []time.Time{
		time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC),
		time.Date(2026, time.September, 4, 12, 0, 1, 250_000_000, time.UTC),
	}
	service.now = func() time.Time { value := times[0]; times = times[1:]; return value }

	result, err := service.Complete(context.Background(), "  prompt  ", 1, ProviderKimi, ModelKimiK3)
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "ответ" || result.Metrics.DurationMilliseconds != 1250 || result.Metrics.InputTokens != 1000 || result.Metrics.OutputTokens != 2000 {
		t.Errorf("result = %#v", result)
	}
	wantCost := (900*3 + 100*0.30 + 2000*15) / 1_000_000.0
	if math.Abs(result.Metrics.CostUSD-wantCost) > 1e-12 {
		t.Errorf("cost = %.12f, want %.12f", result.Metrics.CostUSD, wantCost)
	}
	if deepSeek.calls != 0 || kimi.calls != 1 || kimi.model != string(ModelKimiK3) || kimi.prompt != "prompt" || kimi.temperature != 1 {
		t.Errorf("routing = deepseek:%d kimi:%d/%q/%q/%v", deepSeek.calls, kimi.calls, kimi.model, kimi.prompt, kimi.temperature)
	}
}

func TestKimiModelRates(t *testing.T) {
	for _, test := range []struct {
		model Model
		want  float64
	}{
		{ModelKimiK3, (500*3 + 500*0.30 + 1000*15) / 1_000_000},
		{ModelKimiK27Code, (500*0.95 + 500*0.19 + 1000*4) / 1_000_000},
		{ModelKimiK26, (500*0.95 + 500*0.16 + 1000*4) / 1_000_000},
	} {
		got := requestCostUSD(ProviderKimi, test.model, llm.Usage{PromptTokens: 1000, CompletionTokens: 1000, CachedTokens: 500}, time.Now())
		if math.Abs(got-test.want) > 1e-12 {
			t.Errorf("%s cost=%f, want %f", test.model, got, test.want)
		}
	}
}

func TestCompleteUsesDeepSeekPeakAndOffPeakRates(t *testing.T) {
	usage := llm.Usage{PromptTokens: 1000, CompletionTokens: 1000, PromptCacheHitTokens: 500}
	client := &fakeClient{completion: llm.Completion{Answer: "answer", Usage: usage}}
	for _, test := range []struct {
		name string
		hour int
		want float64
	}{
		{name: "peak", hour: 6, want: (500*0.44 + 500*0.014 + 1000*1.32) / 1_000_000},
		{name: "off peak", hour: 12, want: (500*0.22 + 500*0.007 + 1000*0.66) / 1_000_000},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(client, nil)
			now := time.Date(2026, time.September, 4, test.hour, 0, 0, 0, time.UTC)
			service.now = func() time.Time { return now }
			result, err := service.Complete(context.Background(), "x", 1.2, ProviderDeepSeek, ModelV4Flash)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(result.Metrics.CostUSD-test.want) > 1e-12 {
				t.Errorf("cost = %.12f, want %.12f", result.Metrics.CostUSD, test.want)
			}
		})
	}
}

func TestCompleteRejectsInvalidInputWithoutClientCall(t *testing.T) {
	tests := []struct {
		name        string
		prompt      string
		temperature float64
		provider    Provider
		model       Model
	}{
		{name: "empty prompt", prompt: " ", temperature: 0.7, provider: ProviderDeepSeek, model: ModelV4Flash},
		{name: "deepseek temperature", prompt: "x", temperature: 2.1, provider: ProviderDeepSeek, model: ModelV4Flash},
		{name: "kimi temperature", prompt: "x", temperature: 1.2, provider: ProviderKimi, model: ModelKimiK3},
		{name: "kimi lower temperature", prompt: "x", temperature: 0.7, provider: ProviderKimi, model: ModelKimiK3},
		{name: "provider", prompt: "x", temperature: 0.7, provider: "other", model: ModelV4Flash},
		{name: "mismatched model", prompt: "x", temperature: 0.7, provider: ProviderKimi, model: ModelV4Flash},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeClient{completion: llm.Completion{Answer: "unexpected"}}
			if _, err := NewService(client, client).Complete(context.Background(), test.prompt, test.temperature, test.provider, test.model); err == nil {
				t.Fatal("expected error")
			}
			if client.calls != 0 {
				t.Errorf("client calls = %d, want 0", client.calls)
			}
		})
	}
}

func TestCompleteReturnsConfigurationAndClientErrors(t *testing.T) {
	if _, err := NewService(&fakeClient{}, nil).Complete(context.Background(), "x", 1, ProviderKimi, ModelKimiK26); err == nil {
		t.Fatal("expected missing Kimi configuration error")
	}
	upstream := errors.New("upstream")
	if _, err := NewService(&fakeClient{err: upstream}, nil).Complete(context.Background(), "x", 0, ProviderDeepSeek, ModelV4FlashVisionExp); !errors.Is(err, upstream) {
		t.Errorf("error = %v, want upstream", err)
	}
}
