package extractjson

import (
	"reflect"
	"strings"
	"testing"

	"aichallenge/week_1/task_1/internal/domain/model"
)

func TestProposalInstructionUsesBrewMarkNameLookup(t *testing.T) {
	instruction := (ProposalDecoder{}).Instruction(model.Task{Stage: model.TaskStageResearchInputData}, false, nil, 2)
	if !strings.Contains(instruction, "BrewMark lookup с name и brand") {
		t.Fatalf("BrewMark lookup contract is missing name and brand: %q", instruction)
	}
	obsoleteArgument := "mo" + "del"
	if strings.Contains(instruction, "BrewMark lookup с "+obsoleteArgument) || strings.Contains(instruction, obsoleteArgument+"=") {
		t.Fatalf("instruction contains obsolete model input contract: %q", instruction)
	}
}

func TestProposalInstructionPreservesCatalogHandoffForExecution(t *testing.T) {
	instruction := (ProposalDecoder{}).Instruction(model.Task{Stage: model.TaskStageResearchInputData}, false, []string{"catalog handoff"}, 2)
	for _, required := range []string{
		"компактный catalog handoff",
		"без округления",
		"minSetting, maxSetting, settingUnit",
		"относящийся к способу anchor",
		"exact match",
		"не заменяй доступную числовую опору общим советом",
		"стартовая каталожная настройка",
	} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("catalog handoff contract is missing %q: %q", required, instruction)
		}
	}
}

func TestProposalInstructionContinuesAnsweredClarification(t *testing.T) {
	instruction := (ProposalDecoder{}).Instruction(model.Task{Stage: model.TaskStageClarifyInput}, false, nil, 2)
	for _, required := range []string{
		"ФОРМАТ PROPOSAL/REPAIR/SYNTHESIS СТРОГИЙ",
		"первый символ текстового ответа — {, последний — }",
		"Markdown fences",
		"XML, DSML или текстовый tool-call syntax",
		"Единственное исключение: в первом research-вызове",
		"только native tool call(s), без текста, JSON, Markdown или DSML",
		"после tool results снова верни один строгий JSON object",
		"TASK_STATE.stage=clarify_input и TASK_STATE.first=false",
		"последним assistant clarification и историей",
		"включая нумерованный список",
		"ОБЯЗАТЕЛЬНО перейди ровно в research_input_data",
		"единственный допустимый следующий stage — research_input_data",
		"не переходи в execution в этом случае",
		"plan непустой",
		"Не задавай повторный clarify-вопрос",
		"конкретный недостающий gap",
		"семантическое решение task LLM",
	} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("clarify continuation contract is missing %q: %q", required, instruction)
		}
	}
	if !strings.Contains(instruction, `"first":false`) {
		t.Fatalf("instruction must include non-first state: %q", instruction)
	}
	if !strings.Contains(instruction, "Правило «конкретная модель не названа» применяется только в этом research-этапе") || !strings.Contains(instruction, "никогда не разрешает пропустить research") {
		t.Fatalf("instruction must keep no-model branch inside research: %q", instruction)
	}
}

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
