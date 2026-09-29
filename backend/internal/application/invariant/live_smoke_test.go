package invariant

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
)

// TestLiveInvariantValidationSmoke is an opt-in acceptance gate. It uses the
// same invariant_validation endpoint and environment-expanded llm.yaml as the
// server: four contexts through three public checkers, then two research-only
// fixtures through the private checker, for at most fourteen external calls.
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
	pipeline := NewSet(counted)
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
			violations, validateErr := pipeline.Validate(context.Background(), Input{Subject: ChatCandidate, Text: tc.text, Facts: tc.facts, Phase: "post"})
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
	var research Invariant
	for _, rule := range pipeline.all {
		if rule.Metadata().ID == "research-sufficiency" {
			research = rule
			break
		}
	}
	if research == nil {
		t.Fatal("private research-sufficiency checker is missing")
	}
	researchCases := []struct {
		name      string
		input     Input
		violation bool
	}{
		{
			name: "research sufficient facts without tool",
			input: Input{
				Subject:  TaskProposal,
				Text:     `{"Output":"Данные об оборудовании достаточны для следующего шага.","Stage":"execution","ExpectedAction":"agent: подобрать стартовый рецепт","Plan":[{"ID":"equipment","Title":"Уточнить оборудование","Status":"completed"},{"ID":"recipe","Title":"Подобрать рецепт","Status":"current"}]}`,
				Snapshot: `{"stage":"research_input_data","current_step":"Проверить достаточность данных об оборудовании","plan":[{"id":"equipment","title":"Уточнить оборудование","status":"current"},{"id":"recipe","title":"Подобрать рецепт","status":"pending"}]}`,
				Request:  "У меня исправная кофемолка Niche Zero, эспрессо-машина Lelit Bianca и корзина 18 г. Подбери стартовый рецепт эспрессо.",
				Facts:    []string{"У пользователя есть исправная кофемолка Niche Zero", "У пользователя есть исправная эспрессо-машина Lelit Bianca", "У пользователя есть корзина 18 г"},
				Research: ResearchToolAttempt{Outcome: "no_tool"},
				Phase:    "post",
			},
		},
		{
			name: "research missing catalog fact without tool",
			input: Input{
				Subject:  TaskProposal,
				Text:     `{"Output":"Данные об оборудовании достаточны для следующего шага.","Stage":"execution","ExpectedAction":"agent: подобрать рецепт","Plan":[{"ID":"equipment","Title":"Проверить характеристики кофемолки в BrewMark","Status":"completed"},{"ID":"recipe","Title":"Подобрать рецепт","Status":"current"}]}`,
				Snapshot: `{"stage":"research_input_data","current_step":"Проверить характеристики кофемолки Timemore Chestnut C3 в BrewMark","plan":[{"id":"equipment","title":"Проверить характеристики кофемолки в BrewMark","status":"current"},{"id":"recipe","title":"Подобрать рецепт","status":"pending"}]}`,
				Request:  "У меня Timemore Chestnut C3 и V60. Проверь характеристики кофемолки по каталогу BrewMark, затем подбери рецепт.",
				Facts:    []string{"У пользователя есть Timemore Chestnut C3", "У пользователя есть V60", "Характеристики Timemore Chestnut C3 по BrewMark ещё не получены"},
				Research: ResearchToolAttempt{Outcome: "no_tool"},
				Phase:    "post",
			},
			violation: true,
		},
	}
	for _, tc := range researchCases {
		t.Run(tc.name, func(t *testing.T) {
			before := counted.Count()
			outcome := research.Check(context.Background(), tc.input)
			if outcome.Category != "" {
				t.Fatalf("research validator error: %s", outcome.Category)
			}
			if tc.violation {
				if outcome.Violation == nil || outcome.Violation.InvariantID != "research-sufficiency" {
					t.Fatalf("research verdict=%+v, want research-sufficiency violation", outcome)
				}
			} else if outcome.Violation != nil {
				t.Fatalf("research verdict=%+v, want allow", outcome)
			}
			if got := counted.Count() - before; got != 1 {
				t.Fatalf("research validator calls=%d, want 1", got)
			}
		})
	}
	if got := counted.Count(); got != 14 {
		t.Fatalf("live validator calls=%d, want 14 or fewer with all six fixtures checked", got)
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
