package extractjson

import (
	"reflect"
	"testing"
)

func TestProposalRejectsServerOwnedValidationResult(t *testing.T) {
	raw := `{"output":"x","understanding":"u","questions":[],"stage":"execution","current_step":"s","expected_action":"agent: s","status":"active","plan":[{"id":"p","title":"p","status":"current"}],"current_plan_item":"p","goal_confirmed":false,"positive_feedback":false,"validation_result":{"status":"passed","summary":"x"}}`
	if _, err := (ProposalDecoder{}).Decode(raw); err == nil {
		t.Fatal("accepted server-owned validation_result")
	}
}

func TestFactsStrictShape(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `[]`, `{"global_facts":[]}`, `{"project_facts":[]}`, `{"global_facts":null,"project_facts":[]}`, `{"global_facts":[],"project_facts":null}`, `{"global_facts":[],"project_facts":[],"other":[]}`, `{"global_facts":[1],"project_facts":[]}`, `{"global_facts":[" "],"project_facts":[]}`, `{"global_facts":["same","same"],"project_facts":[]}`, `{"global_facts":[],"global_facts":[],"project_facts":[]}`, `{"global_facts":[],"project_facts":[]} {}`, `{"global_facts":{},"project_facts":[]}`} {
		t.Run(raw, func(t *testing.T) {
			if _, err := DecodeFacts(raw); err == nil {
				t.Fatal("accepted invalid memory")
			}
		})
	}
	if v, e := DecodeFacts(`{"global_facts":[],"project_facts":[]}`); e != nil || v.GlobalFacts == nil || v.ProjectFacts == nil {
		t.Fatal("valid clear rejected")
	}
}

func TestFactsKeepNegativeGlobalAndExplicitProjectScope(t *testing.T) {
	facts, err := DecodeFacts(`{"global_facts":["Кофемолки нет","Зёрна закончились"],"project_facts":["Только для этого проекта есть отдельная пачка зёрен"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(facts.GlobalFacts, []string{"Кофемолки нет", "Зёрна закончились"}) {
		t.Fatalf("negative global facts changed: %#v", facts.GlobalFacts)
	}
	if !reflect.DeepEqual(facts.ProjectFacts, []string{"Только для этого проекта есть отдельная пачка зёрен"}) {
		t.Fatalf("explicit project fact changed: %#v", facts.ProjectFacts)
	}
}
