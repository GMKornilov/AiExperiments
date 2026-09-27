package extractjson

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"aichallenge/week_1/task_1/internal/domain/model"
)

func TestProposalInstructionUsesBrewMarkNameLookup(t *testing.T) {
	instruction := (ProposalDecoder{}).Instruction(model.Task{Stage: model.TaskStageResearchInputData}, false, nil, 2)
	if !strings.Contains(instruction, "lookup с name и brand") {
		t.Fatalf("BrewMark lookup contract is missing name and brand: %q", instruction)
	}
	obsoleteArgument := "mo" + "del"
	if strings.Contains(instruction, "BrewMark lookup с "+obsoleteArgument) || strings.Contains(instruction, obsoleteArgument+"=") {
		t.Fatalf("instruction contains obsolete model input contract: %q", instruction)
	}
}

func TestPromptBuilderUsesOnlyCurrentStageStrategy(t *testing.T) {
	builder := NewTaskPromptBuilder()
	stageRules := map[model.TaskStage]string{
		model.TaskStageClarifyInput:      "ЭТАП clarify_input",
		model.TaskStageResearchInputData: "ЭТАП research_input_data",
		model.TaskStageExecution:         "ЭТАП execution",
		model.TaskStageUserFeedback:      "ЭТАП user_feedback",
	}
	for stage, ownRule := range stageRules {
		t.Run(string(stage), func(t *testing.T) {
			prompt, err := builder.Build(model.Task{Stage: stage}, false, nil, 2)
			if err != nil || !strings.Contains(prompt, ownRule) {
				t.Fatalf("stage=%s err=%v prompt=%q", stage, err, prompt)
			}
			for otherStage, otherRule := range stageRules {
				if otherStage != stage && strings.Contains(prompt, otherRule) {
					t.Fatalf("stage=%s leaked rule from %s: %q", stage, otherStage, prompt)
				}
			}
			for _, common := range []string{"ФОРМАТ PROPOSAL/REPAIR/SYNTHESIS СТРОГИЙ", `"stage":"`, "TASK_STATE:"} {
				if !strings.Contains(prompt, common) {
					t.Fatalf("stage=%s misses common contract %q", stage, common)
				}
			}
		})
	}
}

func TestPromptBuilderRejectsUnknownStage(t *testing.T) {
	_, err := NewTaskPromptBuilder().Build(model.Task{Stage: model.TaskStage("unknown")}, false, nil, 1)
	if !errors.Is(err, ErrUnknownTaskStage) {
		t.Fatalf("err=%v", err)
	}
}

func TestProposalInstructionPreservesCatalogHandoffForExecution(t *testing.T) {
	instruction := (ProposalDecoder{}).Instruction(model.Task{Stage: model.TaskStageResearchInputData}, false, []string{"catalog handoff"}, 2)
	for _, required := range []string{
		"компактный catalog handoff",
		"без округления",
		"minSetting, maxSetting, settingUnit",
		"относящийся к способу anchor",
		"стартовые каталожные опоры",
	} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("catalog handoff contract is missing %q: %q", required, instruction)
		}
	}
}

func TestExecutionPromptPreservesExactCatalogAnchor(t *testing.T) {
	prompt, err := NewTaskPromptBuilder().Build(model.Task{Stage: model.TaskStageExecution}, false, []string{"catalog handoff"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"используй exact match",
		"не заменяй доступную числовую опору общим советом настроить по времени",
		"финальный output должен быть понятен вместе с prior_outputs",
		"ровно один current, согласованный с current_plan_item",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("execution prompt misses %q: %q", required, prompt)
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
		"ЭТАП clarify_input",
		"TASK_STATE.first=false",
		"последним assistant clarification и историей",
		"включая нумерованный список",
		"перейди ровно в research_input_data",
		"единственный допустимый следующий stage — research_input_data",
		"не переходи в execution",
		"plan непустой",
		"Не задавай повторный clarify-вопрос",
		"конкретно названном gap",
		"семантическое решение task LLM",
	} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("clarify continuation contract is missing %q: %q", required, instruction)
		}
	}
	if !strings.Contains(instruction, `"first":false`) {
		t.Fatalf("instruction must include non-first state: %q", instruction)
	}
	if strings.Contains(instruction, "BrewMark lookup") || strings.Contains(instruction, "ЭТАП research_input_data") {
		t.Fatalf("clarify prompt leaked research rules: %q", instruction)
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
