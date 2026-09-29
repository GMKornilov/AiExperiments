package invariant

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"aichallenge/week_1/task_1/internal/application/completion"
)

type scriptedClient struct {
	purposes []string
	answers  map[string]string
	errors   map[string]error
	messages map[string][]completion.Message
}

func (c *scriptedClient) Complete(_ context.Context, purpose string, messages []completion.Message) (string, error) {
	c.purposes = append(c.purposes, purpose)
	if c.messages == nil {
		c.messages = make(map[string][]completion.Message)
	}
	c.messages[purpose] = append([]completion.Message{}, messages...)
	if err := c.errors[purpose]; err != nil {
		return "", err
	}
	return c.answers[purpose], nil
}

func TestRulePromptsKeepCheckerBoundaries(t *testing.T) {
	client := &scriptedClient{answers: map[string]string{
		"invariant_equipment-availability": `{"status":"allow"}`,
		"invariant_beans-availability":     `{"status":"allow"}`,
		"invariant_inventory-truth":        `{"status":"allow"}`,
	}}
	if _, err := NewSet(client).Validate(context.Background(), Input{Subject: ChatCandidate, Text: "candidate", Phase: "post"}); err != nil {
		t.Fatal(err)
	}
	assertPromptContains := func(purpose string, phrases ...string) {
		t.Helper()
		prompt := client.messages[purpose][0].Content
		for _, phrase := range phrases {
			if !strings.Contains(prompt, phrase) {
				t.Fatalf("%s prompt misses %q: %s", purpose, phrase, prompt)
			}
		}
	}
	assertPromptContains("invariant_equipment-availability", "ONLY the equipment-availability", "Ignore all beans", "Missing or unknown inventory")
	assertPromptContains("invariant_beans-availability", "ONLY the beans-availability", "Ignore all equipment", "Missing or unknown beans")
	assertPromptContains("invariant_inventory-truth", "ONLY the inventory-truth", "top-level coffee resource", "broken, unavailable, or finished also confirms", "technical characteristics, configuration, settings, burrs, parts, or catalog attributes", "Explicit conditional")
}

func TestSetRunsAllSemanticCheckersSequentiallyAndAggregates(t *testing.T) {
	client := &scriptedClient{answers: map[string]string{
		"invariant_equipment-availability": `{"status":"violation","reason":"equipment","repair_instruction":"alternative"}`,
		"invariant_beans-availability":     `{"status":"allow"}`,
		"invariant_inventory-truth":        `{"status":"violation","reason":"inventory","repair_instruction":"conditional"}`,
	}}
	violations, err := NewSet(client).Validate(context.Background(), Input{Subject: ChatCandidate, Text: "candidate", Phase: "post"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := client.purposes, []string{"invariant_equipment-availability", "invariant_beans-availability", "invariant_inventory-truth"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order=%v, want %v", got, want)
	}
	if got, want := []string{violations[0].InvariantID, violations[1].InvariantID}, []string{"equipment-availability", "inventory-truth"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("violations=%v", got)
	}
}

func TestSetContinuesAfterCheckerTechnicalError(t *testing.T) {
	client := &scriptedClient{answers: map[string]string{
		"invariant_beans-availability": `{"status":"allow"}`,
		"invariant_inventory-truth":    `{"status":"allow"}`,
	}, errors: map[string]error{"invariant_equipment-availability": errors.New("timeout")}}
	_, err := NewSet(client).Validate(context.Background(), Input{Subject: ChatCandidate, Text: "candidate", Phase: "post"})
	if completion.Category(err) != "invariant_validation" {
		t.Fatalf("error=%v", err)
	}
	if got, want := client.purposes, []string{"invariant_equipment-availability", "invariant_beans-availability", "invariant_inventory-truth"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order=%v, want %v", got, want)
	}
}

func TestResearchSufficiencyIsPrivateAndReceivesConfirmedContext(t *testing.T) {
	client := &scriptedClient{answers: map[string]string{
		"invariant_equipment-availability": `{"status":"allow"}`,
		"invariant_beans-availability":     `{"status":"allow"}`,
		"invariant_inventory-truth":        `{"status":"allow"}`,
		"invariant_research-sufficiency":   `{"status":"violation","reason":"нужна модель кофемолки","repair_instruction":"уточни модель через каталог"}`,
	}}
	set := NewSet(client)
	if got := len(set.Public()); got != 3 {
		t.Fatalf("public rules=%d, want 3", got)
	}
	input := Input{Subject: TaskProposal, Text: `{"stage":"execution","output":"данных достаточно"}`, Snapshot: `{"stage":"research_input_data","plan":[]}`, Request: "У меня Niche Zero", Facts: []string{"У пользователя есть Niche Zero"}, Research: ResearchToolAttempt{Outcome: "no_tool"}, Phase: "post"}
	violations, err := set.Validate(context.Background(), input)
	if err != nil || len(violations) != 1 || violations[0].InvariantID != "research-sufficiency" {
		t.Fatalf("violations=%+v error=%v", violations, err)
	}
	request := client.messages["invariant_research-sufficiency"]
	if len(request) != 2 || !strings.Contains(request[0].Content, "proposed_stage") || !strings.Contains(request[0].Content, "semantic_suspicion") || !strings.Contains(request[1].Content, `"proposed_stage":"execution"`) || !strings.Contains(request[1].Content, `"user_request":"У меня Niche Zero"`) || !strings.Contains(request[1].Content, `"outcome":"no_tool"`) || !strings.Contains(request[1].Content, "У пользователя есть Niche Zero") {
		t.Fatalf("research checker input=%+v", request)
	}
	before := len(client.purposes)
	input.Research = ResearchToolAttempt{}
	if _, err := set.Validate(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if got := len(client.purposes) - before; got != 3 {
		t.Fatalf("non-research checks=%d, want 3", got)
	}
}

func TestResearchExecutionSuspicionIsNotAViolation(t *testing.T) {
	snapshot := `{"stage":"research_input_data","plan":[{"id":"parameters","title":"Подобрать стартовые параметры","status":"current"}]}`
	for _, candidate := range []string{
		`{"output":"Рецепт: 18 г кофе, 36 г напитка за 28 секунд.","plan":[{"id":"parameters","title":"Подобрать стартовые параметры","status":"current"}]}`,
		`{"output":"Сведения собраны.","plan":[{"id":"parameters","title":"Подобрать стартовые параметры","status":"completed"}]}`,
	} {
		if !researchExecutionSuspicion(candidate, snapshot) {
			t.Fatalf("candidate did not raise suspicion: %s", candidate)
		}
	}
	if researchExecutionSuspicion(`{"output":"Данных об оборудовании достаточно.","plan":[{"id":"parameters","title":"Подобрать стартовые параметры","status":"current"}]}`, snapshot) {
		t.Fatal("valid research raised suspicion")
	}
}

func TestResearchSuspicionAlwaysDefersFinalVerdictToLLM(t *testing.T) {
	base := Input{
		Subject:  TaskProposal,
		Snapshot: `{"stage":"research_input_data","plan":[{"id":"equipment","status":"current"},{"id":"recipe","status":"pending"}]}`,
		Research: ResearchToolAttempt{Outcome: "success"},
	}
	for _, tc := range []struct {
		name    string
		text    string
		answer  string
		wantBad bool
	}{
		{name: "future recipe is allowed", text: `{"stage":"research_input_data","output":"Сведения об оборудовании собраны; рецепт подготовлю на следующем execution-шаге.","plan":[{"id":"equipment","title":"Уточнить оборудование","status":"completed"},{"id":"recipe","title":"Подобрать рецепт","stage":"execution","status":"current"}]}`, answer: `{"status":"allow"}`},
		{name: "actual recipe is rejected", text: `{"stage":"research_input_data","output":"Рецепт: 18 г кофе, 36 г напитка за 28 секунд.","plan":[{"id":"equipment","title":"Уточнить оборудование","status":"completed"},{"id":"recipe","title":"Подобрать рецепт","stage":"execution","status":"current"}]}`, answer: `{"status":"violation","reason":"Рецепт уже выдан","repair_instruction":"Оставь рецепт для execution."}`, wantBad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &scriptedClient{answers: map[string]string{"invariant_research-sufficiency": tc.answer}}
			input := base
			input.Text = tc.text
			outcome := NewChecker(Metadata{ID: "research-sufficiency", Internal: true, Subjects: []Subject{TaskProposal}}, client, researchPrompt).Check(context.Background(), input)
			if outcome.Category != "" {
				t.Fatalf("checker category=%q", outcome.Category)
			}
			if (outcome.Violation != nil) != tc.wantBad {
				t.Fatalf("outcome=%+v, want violation=%t", outcome, tc.wantBad)
			}
			if got, want := client.purposes, []string{"invariant_research-sufficiency"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("purposes=%v, want %v", got, want)
			}
			payload := client.messages["invariant_research-sufficiency"][1].Content
			if !strings.Contains(payload, `"proposed_stage":"research_input_data"`) || !strings.Contains(payload, `"semantic_suspicion":true`) {
				t.Fatalf("payload misses suspicion: %s", payload)
			}
			if tc.wantBad && (!strings.Contains(payload, `"execution_result_present":true`) || strings.Contains(payload, `"completed_execution_item":true`)) {
				t.Fatalf("parameter violation control data=%s", payload)
			}
		})
	}
}

func TestResearchCatalogHandoffIsAllowedButRecipeIsNot(t *testing.T) {
	handoff := `{"stage":"execution","output":"Catalog handoff: brewmark_list_grinders; matchStatus=exact; DF64 Gen 2; minSetting=0; maxSetting=90; settingUnit=NUMBER; espressoAnchor=10; burrType=FLAT. Это каталожная стартовая опора, не рецепт.","plan":[{"id":"equipment","title":"Проверить кофемолку","status":"completed"},{"id":"recipe","title":"Подобрать рецепт","stage":"execution","status":"current"}],"expected_action":"agent: подобрать рецепт"}`
	client := &scriptedClient{answers: map[string]string{"invariant_research-sufficiency": `{"status":"allow"}`}}
	checker := NewChecker(Metadata{ID: "research-sufficiency", Internal: true, Subjects: []Subject{TaskProposal}}, client, researchPrompt)
	input := Input{
		Subject:  TaskProposal,
		Text:     handoff,
		Snapshot: `{"stage":"research_input_data","plan":[{"id":"equipment","status":"current"},{"id":"recipe","status":"pending"}]}`,
		Research: ResearchToolAttempt{Outcome: "success", Calls: []ResearchToolCallAttempt{{Tool: "brewmark_list_grinders", Outcome: "success"}}},
	}
	if outcome := checker.Check(context.Background(), input); !outcome.Allowed() {
		t.Fatalf("handoff outcome=%+v, want allow", outcome)
	}
	prompt := client.messages["invariant_research-sufficiency"][0].Content
	for _, required := range []string{"compact BrewMark catalog handoff", "research-versus-recipe boundary, not verification of catalog accuracy", "do not reject a handoff value solely because it is absent from facts or payload evidence", "do not demand, advise, or infer an absent catalog value", "completed mandatory attempt without catalog values"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("research prompt misses %q: %q", required, prompt)
		}
	}
	for _, forbidden := range []string{"espressoAnchor=10", "range 0–90 NUMBER", "DF64 Gen 2"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("research prompt retains hardcoded catalogue value %q: %q", forbidden, prompt)
		}
	}

	client.answers["invariant_research-sufficiency"] = `{"status":"violation","reason":"recipe","repair_instruction":"leave recipe for execution"}`
	input.Text = strings.Replace(handoff, "Это каталожная стартовая опора, не рецепт.", "Рецепт: 18 г кофе, 36 г напитка за 28 секунд.", 1)
	if outcome := checker.Check(context.Background(), input); outcome.Violation == nil {
		t.Fatalf("recipe outcome=%+v, want violation", outcome)
	}
}

func TestResearchEmptyCatalogResultsDoNotInventExecutionTransition(t *testing.T) {
	client := &scriptedClient{answers: map[string]string{"invariant_research-sufficiency": `{"status":"allow"}`}}
	input := Input{
		Subject:  TaskProposal,
		Text:     `{"stage":"research_input_data","output":"Каталог не дал совпадений. Владение подтверждено; рецепт подготовлю на будущем execution-шаге.","plan":[{"id":"equipment","title":"Проверить оборудование","stage":"research_input_data","status":"current"},{"id":"recipe","title":"Подобрать рецепт","stage":"execution","status":"pending"}]}`,
		Snapshot: `{"stage":"research_input_data","plan":[{"id":"equipment","status":"current"},{"id":"recipe","status":"pending"}]}`,
		Request:  "У меня кофемолка DF64 Gen2 и De'Longhi EC685. Подбери рецепт эспрессо.",
		Facts: []string{
			"У пользователя есть кофемолка DF64 Gen2",
			"У пользователя есть De'Longhi EC685",
		},
		Research: ResearchToolAttempt{Outcome: "success", Calls: []ResearchToolCallAttempt{
			{Tool: "brewmark_list_grinders", Outcome: "empty"},
			{Tool: "brewmark_list_brewers", Outcome: "empty"},
		}},
	}
	checker := NewChecker(Metadata{ID: "research-sufficiency", Internal: true, Subjects: []Subject{TaskProposal}}, client, researchPrompt)
	if outcome := checker.Check(context.Background(), input); !outcome.Allowed() {
		t.Fatalf("outcome=%+v, want allow", outcome)
	}
	if got, want := client.purposes, []string{"invariant_research-sufficiency"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("purposes=%v, want %v", got, want)
	}
	var payload struct {
		ProposedStage          string              `json:"proposed_stage"`
		SemanticSuspicion      bool                `json:"semantic_suspicion"`
		ExecutionResultPresent bool                `json:"execution_result_present"`
		CompletedExecutionItem bool                `json:"completed_execution_item"`
		Research               ResearchToolAttempt `json:"research_tool_attempt"`
	}
	if err := json.Unmarshal([]byte(client.messages["invariant_research-sufficiency"][1].Content), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ProposedStage != "research_input_data" || !payload.SemanticSuspicion || payload.ExecutionResultPresent || payload.CompletedExecutionItem {
		t.Fatalf("control payload=%+v", payload)
	}
	if len(payload.Research.Calls) != 2 || payload.Research.Calls[0].Outcome != "empty" || payload.Research.Calls[1].Outcome != "empty" {
		t.Fatalf("research status=%+v", payload.Research)
	}
}
