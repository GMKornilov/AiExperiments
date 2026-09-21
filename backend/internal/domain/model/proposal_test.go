package model

import (
	"strings"
	"testing"
)

func TestProposalInvariants(t *testing.T) {
	previous := Task{ID: "t", Stage: TaskStageExecution, Status: TaskStatusActive, Plan: []TaskPlanItem{{ID: "done", Title: "Узнать модель кофемолки", Status: TaskPlanItemCompleted}, {ID: "now", Title: "Подобрать рецепт", Status: TaskPlanItemCurrent}}, CurrentPlanItem: "now"}
	valid := Proposal{Output: "Результат одного шага", Stage: TaskStageExecution, Status: TaskStatusActive, CurrentStep: "Проверить дозу", ExpectedAction: "agent: проверить дозу", Plan: append([]TaskPlanItem{}, previous.Plan...), CurrentPlanItem: "now"}
	if _, e := ApplyProposal(previous, valid, false); e != nil {
		t.Fatal("same-stage step rejected", e)
	}
	tests := map[string]func(*Proposal){
		"skip":             func(p *Proposal) { p.Stage = TaskStageClarifyInput },
		"completed-title":  func(p *Proposal) { p.Plan[0].Title = "changed" },
		"completed-status": func(p *Proposal) { p.Plan[0].Status = TaskPlanItemPending },
		"duplicate-id":     func(p *Proposal) { p.Plan[1].ID = p.Plan[0].ID },
		"two-current":      func(p *Proposal) { p.Plan[0].Status = TaskPlanItemCurrent },
		"blank-title":      func(p *Proposal) { p.Plan[1].Title = " " },
		"unknown-status":   func(p *Proposal) { p.Plan[1].Status = "other" },
		"wrong-current":    func(p *Proposal) { p.CurrentPlanItem = "missing" },
		"empty-plan":       func(p *Proposal) { p.Plan = nil },
		"premature-done":   func(p *Proposal) { p.Status = TaskStatusDone },
	}
	for name, edit := range tests {
		t.Run(name, func(t *testing.T) {
			p := valid
			p.Plan = append([]TaskPlanItem{}, valid.Plan...)
			edit(&p)
			if _, e := ApplyProposal(previous, p, false); e == nil {
				t.Fatal("invalid proposal accepted")
			}
		})
	}
	research := Task{ID: "t", Stage: TaskStageResearchInputData, Status: TaskStatusActive, ValidationResult: ValidationResult{Status: ValidationNotValidated}, Plan: []TaskPlanItem{{ID: "research", Title: "Собрать данные", Status: TaskPlanItemCurrent}, {ID: "result", Title: "Подготовить результат", Status: TaskPlanItemPending}}, CurrentPlanItem: "research"}
	execution := Proposal{Output: "Данные собраны", Stage: TaskStageExecution, Status: TaskStatusActive, CurrentStep: "Подготовить результат", ExpectedAction: "agent: подготовить результат", Plan: []TaskPlanItem{{ID: "research", Title: "Собрать данные", Status: TaskPlanItemCompleted}, {ID: "result", Title: "Подготовить результат", Status: TaskPlanItemCurrent}}, CurrentPlanItem: "result"}
	if _, e := ApplyProposal(research, execution, false); e != nil {
		t.Fatal("valid adjacent transition rejected", e)
	}
	execTask, _ := ApplyProposal(research, execution, false)
	feedback := Proposal{Output: "Результат подготовлен", Stage: TaskStageUserFeedback, Status: TaskStatusActive, CurrentStep: "Получить отзыв", ExpectedAction: "user: оценить результат", Plan: []TaskPlanItem{{ID: "research", Title: "Собрать данные", Status: TaskPlanItemCompleted}, {ID: "result", Title: "Подготовить результат", Status: TaskPlanItemCompleted}}}
	if _, e := ApplyProposal(execTask, feedback, false); e != nil {
		t.Fatal("valid forward stage transition rejected", e)
	}
	ungated, _ := ApplyProposal(execTask, feedback, false)
	if ungated.ValidationResult.Status != ValidationNotValidated {
		t.Fatal("proposal fabricated validation evidence")
	}
}

func TestStageGraphRejectsJumpsAndFeedbackSelfTransition(t *testing.T) {
	base := Task{ID: "t", Status: TaskStatusActive, ValidationResult: ValidationResult{Status: ValidationNotValidated}, Stage: TaskStageResearchInputData, Plan: []TaskPlanItem{{ID: "p", Title: "Работа", Status: TaskPlanItemCurrent}}, CurrentPlanItem: "p"}
	proposal := Proposal{Output: "x", Stage: TaskStageUserFeedback, Status: TaskStatusActive, CurrentStep: "Отзыв", ExpectedAction: "user: отзыв", Plan: []TaskPlanItem{{ID: "p", Title: "Работа", Status: TaskPlanItemCompleted}}}
	if _, err := ApplyProposal(base, proposal, false); err == nil {
		t.Fatal("research to feedback jump accepted")
	}
	base.Stage = TaskStageUserFeedback
	base.ValidationResult = ValidationResult{Status: ValidationPassed, Summary: "ok"}
	base.Plan[0].Status = TaskPlanItemCompleted
	base.CurrentPlanItem = ""
	if _, err := ApplyProposal(base, proposal, false); err == nil {
		t.Fatal("feedback self transition accepted")
	}
}
func TestTitleAndProfileUnicode(t *testing.T) {
	if _, ok := ValidTitle(strings.Repeat("я", 61)); ok {
		t.Fatal("overlong title")
	}
	for _, text := range []string{"", "one\ntwo", "one\rtwo"} {
		if _, ok := ValidTitle(text); ok {
			t.Fatal("invalid title")
		}
	}
	if got := TitleFallback(strings.Repeat("я", 70)); len([]rune(got)) != 60 {
		t.Fatal("fallback rune truncation")
	}
}
