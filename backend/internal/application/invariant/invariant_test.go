package invariant

import (
	"context"
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
	assertPromptContains("invariant_inventory-truth", "ONLY the inventory-truth", "broken, unavailable, or finished also confirms", "Explicit conditional")
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
