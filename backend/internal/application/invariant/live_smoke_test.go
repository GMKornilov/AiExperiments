package invariant_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"aichallenge/week_1/task_1/internal/adapters/configsnapshot"
	"aichallenge/week_1/task_1/internal/adapters/openai"
	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/application/completion"
	"aichallenge/week_1/task_1/internal/application/invariant"
)

// TestLiveInvariantValidationSmoke is an opt-in acceptance gate. It uses the
// same invariant_validation endpoint and environment-expanded llm.yaml as the
// server, makes exactly 4 contexts × 3 checkers, and therefore never exceeds
// the documented twelve external calls.
func TestLiveInvariantValidationSmoke(t *testing.T) {
	if os.Getenv("RUN_LIVE_INVARIANT_SMOKE") != "1" {
		t.Skip("set RUN_LIVE_INVARIANT_SMOKE=1 with runtime LLM credentials")
	}
	configPath := os.Getenv("LIVE_LLM_CONFIG")
	if configPath == "" {
		configPath = filepath.Clean("../../../llm.yaml")
	}
	snapshot, err := configsnapshot.Loader(configPath)()
	if err != nil || snapshot.InvariantValidation == nil {
		t.Skipf("runtime invariant_validation config is unavailable: %v", err)
	}
	client, err := openai.New(agent.OpenAIProvider{}, snapshot)
	if err != nil {
		t.Skipf("runtime invariant_validation credentials are unavailable: %v", err)
	}
	counted := &countingClient{client: client}
	pipeline := invariant.NewSet(counted)
	cases := []struct {
		name       string
		text       string
		facts      []string
		expectedID []string
	}{
		{name: "unavailable equipment", text: "Используйте сломанную кофемолку для помола подтверждённых зёрен Colombia.", facts: []string{"Кофемолка сломана", "Есть зёрна Colombia"}, expectedID: []string{"equipment-availability"}},
		{name: "unavailable beans", text: "Заварьте подтверждённые зёрна Ethiopia в имеющейся воронке V60.", facts: []string{"Есть воронка V60", "Зёрна Ethiopia закончились"}, expectedID: []string{"beans-availability"}},
		{name: "unconfirmed inventory", text: "У вас есть эспрессо-машина, поэтому настройте её на 93 °C.", facts: []string{"Есть воронка V60", "Есть зёрна Colombia"}, expectedID: []string{"inventory-truth"}},
		{name: "conditional variant", text: "Если у вас есть V60 и подходящие зёрна, можно использовать рецепт для V60.", expectedID: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations, validateErr := pipeline.Validate(context.Background(), invariant.Input{Subject: invariant.ChatCandidate, Text: tc.text, Facts: tc.facts, Phase: "post"})
			if validateErr != nil {
				t.Fatal(validateErr)
			}
			got := make([]string, 0, len(violations))
			for _, violation := range violations {
				got = append(got, violation.InvariantID)
			}
			sort.Strings(got)
			sort.Strings(tc.expectedID)
			if len(got) != len(tc.expectedID) {
				t.Fatalf("verdicts=%v, want %v", got, tc.expectedID)
			}
			for i := range got {
				if got[i] != tc.expectedID[i] {
					t.Fatalf("verdicts=%v, want %v", got, tc.expectedID)
				}
			}
		})
	}
	if got := counted.Count(); got != 12 {
		t.Fatalf("live validator calls=%d, want 12", got)
	}
}

type countingClient struct {
	client interface {
		Complete(context.Context, string, []completion.Message) (string, error)
	}
	mu    sync.Mutex
	calls int
}

func (c *countingClient) Complete(ctx context.Context, purpose string, messages []completion.Message) (string, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.client.Complete(ctx, purpose, messages)
}

func (c *countingClient) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}
