package model

import "strings"

// Proposal is the typed result of one task-agent completion, not an HTTP DTO.
type Proposal struct {
	Output           string
	Understanding    string
	Questions        []string
	Stage            TaskStage
	CurrentStep      string
	ExpectedAction   string
	Status           TaskStatus
	Plan             []TaskPlanItem
	CurrentPlanItem  string
	GoalConfirmed    bool
	PositiveFeedback bool
}

func ApplyProposal(previous Task, p Proposal, first bool) (Task, error) {
	if strings.TrimSpace(p.Output) == "" || strings.TrimSpace(p.CurrentStep) == "" || strings.TrimSpace(p.ExpectedAction) == "" || !p.Stage.Valid() || !p.Status.Valid() || p.Status == TaskStatusPaused {
		return Task{}, ErrValidation
	}
	expected := strings.ToLower(strings.TrimSpace(p.ExpectedAction))
	if p.Status == TaskStatusActive && !strings.HasPrefix(expected, "user:") && !strings.HasPrefix(expected, "agent:") {
		return Task{}, ErrValidation
	}
	if first && (p.Stage != TaskStageClarifyInput || strings.TrimSpace(p.Understanding) == "" || len(p.Questions) == 0) {
		return Task{}, ErrValidation
	}
	for _, q := range p.Questions {
		if strings.TrimSpace(q) == "" {
			return Task{}, ErrValidation
		}
	}
	if !legalStageTransition(previous.Stage, p.Stage) || previous.Status == TaskStatusDone {
		return Task{}, ErrValidation
	}
	if previous.Stage == TaskStageUserFeedback && p.Stage == TaskStageUserFeedback && p.Status != TaskStatusDone {
		return Task{}, ErrValidation
	}
	if previous.Stage == TaskStageClarifyInput && p.Stage != previous.Stage && !previous.EquipmentConfirmed {
		return Task{}, ErrValidation
	}
	if p.Stage == TaskStageClarifyInput && (!strings.HasPrefix(expected, "user:") || len(p.Questions) == 0) {
		return Task{}, ErrValidation
	}
	if p.Stage == TaskStageUserFeedback && p.Status == TaskStatusActive && !strings.HasPrefix(expected, "user:") {
		return Task{}, ErrValidation
	}
	if previous.Stage == TaskStageExecution && p.Stage == TaskStageUserFeedback {
		for _, item := range p.Plan {
			if item.Status != TaskPlanItemCompleted {
				return Task{}, ErrValidation
			}
		}
	}
	if p.Status == TaskStatusDone && (previous.Stage != TaskStageUserFeedback || p.Stage != TaskStageUserFeedback || !p.PositiveFeedback || p.ExpectedAction != "none") {
		return Task{}, ErrValidation
	}
	if previous.Stage == TaskStageUserFeedback && p.Stage != TaskStageUserFeedback && p.PositiveFeedback {
		return Task{}, ErrValidation
	}
	for _, old := range previous.Plan {
		if old.Status != TaskPlanItemCompleted {
			continue
		}
		found := false
		for _, item := range p.Plan {
			if item.ID == old.ID && item.Title == old.Title && item.Status == TaskPlanItemCompleted {
				found = true
			}
		}
		if !found {
			return Task{}, ErrValidation
		}
	}
	next := previous
	next.Stage = p.Stage
	next.Status = p.Status
	next.CurrentStep = p.CurrentStep
	next.ExpectedAction = p.ExpectedAction
	next.Plan = append([]TaskPlanItem{}, p.Plan...)
	next.CurrentPlanItem = p.CurrentPlanItem
	if !next.ValidationResult.Status.Valid() {
		next.ValidationResult = ValidationResult{Status: ValidationNotValidated}
	}
	if previous.Stage == TaskStageUserFeedback && p.Stage != previous.Stage {
		next.ValidationResult = ValidationResult{Status: ValidationNotValidated}
		if p.Stage == TaskStageClarifyInput {
			next.EquipmentConfirmed = false
		}
	}
	if p.Status == TaskStatusDone {
		next.CurrentStep = previous.CurrentStep
	}
	if err := validateTaskPlan(&next, previous.Stage == TaskStageExecution && p.Stage == TaskStageUserFeedback); err != nil {
		return Task{}, err
	}
	return next, nil
}

func ExpectsAgent(task Task) bool {
	return task.Status == TaskStatusActive && strings.HasPrefix(strings.ToLower(strings.TrimSpace(task.ExpectedAction)), "agent:")
}

func legalStageTransition(previous, next TaskStage) bool {
	if previous == next {
		return true
	}
	switch previous {
	case TaskStageClarifyInput:
		return next == TaskStageResearchInputData
	case TaskStageResearchInputData:
		return next == TaskStageExecution
	case TaskStageExecution:
		return next == TaskStageUserFeedback
	case TaskStageUserFeedback:
		return next == TaskStageClarifyInput || next == TaskStageResearchInputData || next == TaskStageExecution
	default:
		return false
	}
}
