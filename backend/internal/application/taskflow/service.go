// Package taskflow commits an autonomous proposal chain, conversation pair and memory snapshot.
package taskflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"aichallenge/week_1/task_1/internal/application/completion"
	"aichallenge/week_1/task_1/internal/application/conversation"
	"aichallenge/week_1/task_1/internal/application/invariant"
	"aichallenge/week_1/task_1/internal/application/subagent"
	"aichallenge/week_1/task_1/internal/domain/model"
)

type TaskState interface {
	Snapshot() model.State
	Update(func(*model.State) error) error
	Attach(string, model.Operation, context.CancelFunc)
	Detach(model.Operation)
}
type CompletionClient interface {
	Complete(context.Context, string, []completion.Message) (string, error)
}
type Extractor interface {
	Extract(context.Context, completion.MemoryInput) (completion.Facts, error)
}
type ProposalCodec interface {
	Decode(string) (model.Proposal, error)
	Instruction(model.Task, bool, []string, int) string
}
type Router interface {
	Match([]model.Task, string) []model.Task
}
type TitleStarter interface {
	Start(context.Context, string, string, string, string)
}
type Service struct {
	state     TaskState
	client    CompletionClient
	extractor Extractor
	codec     ProposalCodec
	router    Router
	titles    TitleStarter
	settings  conversation.Settings
	id        func() string
	now       func() time.Time
	validator invariant.Pipeline
}

func New(state TaskState, client CompletionClient, extractor Extractor, codec ProposalCodec, router Router, titles TitleStarter, settings conversation.Settings, id func() string, now func() time.Time, validator invariant.Pipeline) *Service {
	if validator == nil {
		panic("task invariant pipeline is required")
	}
	return &Service{state: state, client: client, extractor: extractor, codec: codec, router: router, titles: titles, settings: settings, id: id, now: now, validator: validator}
}

var errReplay = errors.New("already accepted")
var errCandidates = errors.New("choose task")

const maxAutonomousSteps = 8

func (s *Service) TaskInput(ctx context.Context, sid, pid, cid, input, candidateID, clientID string) (model.Chat, []model.Task, error) {
	if strings.TrimSpace(input) == "" || (clientID != "" && strings.TrimSpace(clientID) == "") {
		return model.Chat{}, nil, model.ErrValidation
	}
	if clientID == "" {
		clientID = s.id()
	}
	// Pre-checks deliberately happen before task creation. A validator error is
	// fail-closed and therefore leaves the authoritative task snapshot intact.
	{
		snapshot := s.state.Snapshot()
		if chat := snapshot.Chat(sid, pid, cid); chat == nil {
			return model.Chat{}, nil, model.ErrNotFound
		}
		b, p := snapshot.Sessions[sid], snapshot.Project(sid, pid)
		violations, validationErr := s.validator.Validate(ctx, invariant.Input{Subject: invariant.UserInput, Text: input, Facts: append(model.CloneFacts(b.GlobalFacts), p.Facts...), Phase: "pre", ProjectID: pid, ChatID: cid})
		if validationErr != nil {
			return model.Chat{}, nil, validationErr
		}
		if len(violations) != 0 {
			chat, refusalErr := s.refuse(ctx, sid, pid, cid, clientID, input, violations)
			return chat, nil, refusalErr
		}
	}
	var before model.State
	var out model.Chat
	var candidates []model.Task
	var previous model.Task
	var op model.Operation
	var previousOperation *model.Operation
	var previousOperationID string
	var previousTaskInput string
	var hadOperationID, hadTaskInput bool
	first, claim, createdTask := false, false, false
	err := s.state.Update(func(root *model.State) error {
		c := root.Chat(sid, pid, cid)
		if c == nil {
			return model.ErrNotFound
		}
		b := root.Sessions[sid]
		if old := c.Operations[clientID]; old != nil {
			if old.Input != input || (candidateID != "" && candidateID != old.TaskID) {
				return model.ErrValidation
			}
			if old.Status == "committed" {
				out = model.CopyChat(c, pid)
				return errReplay
			}
			candidateID = old.TaskID
		}
		if b.Pending != nil {
			return model.ErrBusy
		}
		var selected *model.Task
		created := false
		if candidateID != "" {
			selected = c.Tasks[candidateID]
			if selected == nil {
				return model.ErrNotFound
			}
			if selected.Status == model.TaskStatusDone {
				return model.ErrValidation
			}
		} else {
			tasks := model.CopyChat(c, pid).Tasks
			matches := s.router.Match(tasks, input)
			if len(matches) > 1 {
				out = model.CopyChat(c, pid)
				candidates = matches
				return errCandidates
			}
			if len(matches) == 1 {
				selected = c.Tasks[matches[0].ID]
			}
		}
		if selected == nil {
			created = true
			createdTask = true
			now := s.now()
			id := s.id()
			selected = &model.Task{ID: id, Title: model.TitleFallback(input), Description: model.TruncateRunes(strings.Join(strings.Fields(input), " "), 160), Stage: model.TaskStageClarifyInput, CurrentStep: model.ClarificationStep, ExpectedAction: model.ClarificationExpectedAction, Status: model.TaskStatusActive, Plan: []model.TaskPlanItem{}, CreatedAt: now, UpdatedAt: now}
			c.Tasks[id] = selected
		}
		first = true
		for _, old := range c.Operations {
			if old.TaskID == selected.ID && old.Status == "committed" {
				first = false
			}
		}
		// Existing v4 tasks already have confirmed history but no operation ledger.
		if !created && len(c.Operations) == 0 && len(c.Messages) > 0 {
			first = false
		}
		previous = *selected
		previous.Plan = append([]model.TaskPlanItem{}, selected.Plan...)
		if saved := c.Operations[clientID]; saved != nil {
			copy := *saved
			previousOperation = &copy
		}
		previousOperationID, hadOperationID = c.TaskOperationIDs[selected.ID]
		previousTaskInput, hadTaskInput = c.TaskInputs[selected.ID]
		selected.Status = model.TaskStatusActive
		op = model.Operation{ID: clientID, Generation: s.id(), ProjectID: pid, ChatID: cid, TaskID: selected.ID, Input: input, Status: "pending"}
		if c.Operations == nil {
			c.Operations = map[string]*model.Operation{}
		}
		if c.TaskOperationIDs == nil {
			c.TaskOperationIDs = map[string]string{}
		}
		c.TaskOperationIDs[selected.ID] = clientID
		saved := op
		c.Operations[clientID] = &saved
		c.TaskInputs[selected.ID] = input
		b.Pending = &op
		before = root.Clone()
		return nil
	})
	if errors.Is(err, errReplay) || errors.Is(err, errCandidates) {
		return out, candidates, nil
	}
	if err != nil {
		return model.Chat{}, nil, err
	}
	call, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	s.state.Attach(sid, op, cancel)
	defer s.state.Detach(op)
	b := before.Sessions[sid]
	p := before.Project(sid, pid)
	c := before.Chat(sid, pid, cid)
	baseMessages := conversation.BuildPrompt(s.settings, b, p, c, input)
	var proposal model.Proposal
	next := previous
	var facts completion.Facts
	var attemptErr error
	outputs := make([]string, 0, maxAutonomousSteps)
	refusalNeeded := false
	callBudget := 0
	for step := 0; step < maxAutonomousSteps; step++ {
		messages := append([]completion.Message{}, baseMessages...)
		messages[0].Content += "\n\n" + s.codec.Instruction(next, first && step == 0, outputs, maxAutonomousSteps-step)
		allowed := false
		for candidate := 0; candidate < 3; candidate++ {
			if callBudget >= maxAutonomousSteps {
				refusalNeeded = true
				break
			}
			callBudget++
			raw, callErr := subagent.Run(call, s.client, "task_step", messages, subagent.Text)
			if callErr != nil {
				attemptErr = callErr
				break
			}
			if call.Err() != nil {
				attemptErr = call.Err()
				break
			}
			proposal, attemptErr = s.codec.Decode(raw)
			if attemptErr != nil {
				break
			}
			candidateNext, transitionErr := model.ApplyProposal(next, proposal, first && step == 0)
			violations := []invariant.Violation{}
			if transitionErr != nil {
				violations = append(violations, invariant.Violation{InvariantID: "task-transition", Reason: "Недопустимый переход задачи", RepairInstruction: "Сохрани текущий этап и верни валидный следующий снимок задачи."})
			}
			{
				proposalPayload, marshalErr := json.Marshal(proposal)
				if marshalErr != nil {
					attemptErr = completion.Invalid()
					break
				}
				snapshotPayload, snapshotErr := taskSnapshot(next)
				if snapshotErr != nil {
					attemptErr = completion.Invalid()
					break
				}
				semantic, validationErr := s.validator.Validate(call, invariant.Input{Subject: invariant.TaskProposal, Text: string(proposalPayload), Facts: append(model.CloneFacts(b.GlobalFacts), p.Facts...), Snapshot: snapshotPayload, Phase: "post", Index: candidate, ProjectID: pid, ChatID: cid})
				if validationErr != nil {
					attemptErr = validationErr
					break
				}
				violations = append(violations, semantic...)
			}
			if len(violations) == 0 {
				next = candidateNext
				allowed = true
				break
			}
			if candidate == 2 {
				refusalNeeded = true
				break
			}
			messages = taskRepairPrompt(messages, violations)
		}
		if attemptErr != nil {
			break
		}
		if refusalNeeded {
			break
		}
		if !allowed {
			attemptErr = completion.Invalid()
			break
		}
		outputs = append(outputs, proposal.Output)
		if !model.ExpectsAgent(next) {
			break
		}
		if step == maxAutonomousSteps-1 {
			refusalNeeded = true
		}
	}
	if attemptErr == nil {
		if refusalNeeded {
			proposal.Output = taskRefusal()
		} else {
			proposal.Output = strings.Join(outputs, "\n\n")
		}
		facts, attemptErr = s.extractor.Extract(call, conversation.MemoryInput(s.settings.Window, b, p, c, input, proposal.Output))
		if attemptErr == nil {
			attemptErr = call.Err()
		}
	}
	// A failed task attempt never uses the ordinary-chat partial-success rule.
	err = s.state.Update(func(root *model.State) error {
		c := root.Chat(sid, pid, cid)
		if c == nil {
			return model.ErrNotFound
		}
		if !root.Owns(sid, op) {
			out = model.CopyChat(c, pid)
			return errReplay
		}
		b := root.Sessions[sid]
		p := root.Project(sid, pid)
		b.Pending = nil
		operation := c.Operations[clientID]
		if attemptErr != nil {
			if completion.Category(attemptErr) == "invariant_validation" {
				s.restoreProvisionalTask(c, op, createdTask, previous, previousOperation, previousOperationID, hadOperationID, previousTaskInput, hadTaskInput)
			} else if operation != nil {
				operation.Status = "failed"
			}
			return nil
		}
		now := s.now()
		if refusalNeeded {
			s.restoreProvisionalTask(c, op, createdTask, previous, previousOperation, previousOperationID, hadOperationID, previousTaskInput, hadTaskInput)
		} else {
			next.UpdatedAt = now
			c.Tasks[next.ID] = &next
			operation.Status = "committed"
		}
		c.Messages = append(c.Messages, model.Message{ID: s.id(), ClientID: clientID, Role: "user", Text: input, CreatedAt: now, Status: "success"}, model.Message{ID: s.id(), Role: "assistant", Text: proposal.Output, CreatedAt: now, Status: "success"})
		b.GlobalFacts = model.CloneFacts(facts.GlobalFacts)
		p.Facts = model.CloneFacts(facts.ProjectFacts)
		c.Status = "success"
		c.ErrorCategory = ""
		p.Status = "success"
		p.ErrorCategory = ""
		c.UpdatedAt = now
		p.UpdatedAt = now
		claim = conversation.ClaimTitle(c, input)
		out = model.CopyChat(c, pid)
		return nil
	})
	if errors.Is(err, errReplay) {
		return out, nil, nil
	}
	if err != nil {
		return model.Chat{}, nil, err
	}
	if attemptErr != nil {
		return model.Chat{}, nil, attemptErr
	}
	if claim {
		s.titles.Start(ctx, sid, pid, cid, input)
	}
	return out, nil, nil
}

func taskSnapshot(task model.Task) (string, error) {
	payload, err := json.Marshal(struct {
		ID              string               `json:"id"`
		Title           string               `json:"title"`
		Description     string               `json:"description"`
		Stage           model.TaskStage      `json:"stage"`
		CurrentStep     string               `json:"current_step"`
		ExpectedAction  string               `json:"expected_action"`
		Status          model.TaskStatus     `json:"status"`
		Plan            []model.TaskPlanItem `json:"plan"`
		CurrentPlanItem string               `json:"current_plan_item"`
	}{
		ID: task.ID, Title: task.Title, Description: task.Description, Stage: task.Stage,
		CurrentStep: task.CurrentStep, ExpectedAction: task.ExpectedAction, Status: task.Status,
		Plan: task.Plan, CurrentPlanItem: task.CurrentPlanItem,
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// restoreProvisionalTask rolls back bookkeeping before a failed validation or
// a template refusal. Ordinary provider failures keep the historical retry
// record for manual retry.
func (s *Service) restoreProvisionalTask(c *model.ChatState, op model.Operation, created bool, previous model.Task, previousOperation *model.Operation, previousOperationID string, hadOperationID bool, previousTaskInput string, hadTaskInput bool) {
	if created {
		delete(c.Tasks, op.TaskID)
	} else {
		restored := previous
		restored.Plan = append([]model.TaskPlanItem{}, previous.Plan...)
		c.Tasks[op.TaskID] = &restored
	}
	if hadTaskInput {
		c.TaskInputs[op.TaskID] = previousTaskInput
	} else {
		delete(c.TaskInputs, op.TaskID)
	}
	if hadOperationID {
		c.TaskOperationIDs[op.TaskID] = previousOperationID
	} else {
		delete(c.TaskOperationIDs, op.TaskID)
	}
	if previousOperation != nil {
		restored := *previousOperation
		c.Operations[op.ID] = &restored
	} else {
		delete(c.Operations, op.ID)
	}
}

func taskRepairPrompt(messages []completion.Message, violations []invariant.Violation) []completion.Message {
	out := append([]completion.Message{}, messages...)
	details := make([]string, 0, len(violations))
	for _, violation := range violations {
		details = append(details, violation.InvariantID+": "+violation.RepairInstruction)
	}
	out[0].Content += "\n\nREPAIR REQUIREMENTS (data):\n" + strings.Join(details, "\n")
	return out
}
func taskRefusal() string {
	return "Я не смог безопасно подготовить следующий шаг задачи. Уточните доступное оборудование, зёрна или инвентарь — и я продолжу с безопасного варианта."
}

// refuse persists an ordinary pair without creating or changing a task.
func (s *Service) refuse(ctx context.Context, sid, pid, cid, clientID, input string, violations []invariant.Violation) (model.Chat, error) {
	ids := make([]string, 0, len(violations))
	for _, violation := range violations {
		ids = append(ids, violation.InvariantID)
	}
	answer := "Я не могу безопасно выполнить этот запрос: сработали правила " + strings.Join(ids, ", ") + ". Назовите доступное оборудование и зёрна либо уточните инвентарь — предложу безопасный вариант."
	op := model.Operation{ID: clientID, Generation: s.id(), ProjectID: pid, ChatID: cid, Input: input, Status: "pending"}
	var before model.State
	var out model.Chat
	claim := false
	err := s.state.Update(func(root *model.State) error {
		chat := root.Chat(sid, pid, cid)
		if chat == nil {
			return model.ErrNotFound
		}
		for _, message := range chat.Messages {
			if message.ClientID == clientID {
				if message.Text != input {
					return model.ErrValidation
				}
				out = model.CopyChat(chat, pid)
				return errReplay
			}
		}
		browser := root.Sessions[sid]
		if browser.Pending != nil {
			return model.ErrBusy
		}
		browser.Pending = &op
		before = root.Clone()
		return nil
	})
	if errors.Is(err, errReplay) {
		return out, nil
	}
	if err != nil {
		return model.Chat{}, err
	}
	call, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	s.state.Attach(sid, op, cancel)
	defer s.state.Detach(op)
	b, p, c := before.Sessions[sid], before.Project(sid, pid), before.Chat(sid, pid, cid)
	facts, extractErr := s.extractor.Extract(call, conversation.MemoryInput(s.settings.Window, b, p, c, input, answer))
	if extractErr == nil {
		extractErr = call.Err()
	}
	err = s.state.Update(func(root *model.State) error {
		chat := root.Chat(sid, pid, cid)
		if chat == nil {
			return model.ErrNotFound
		}
		if !root.Owns(sid, op) {
			return context.Canceled
		}
		browser := root.Sessions[sid]
		project := root.Project(sid, pid)
		browser.Pending = nil
		now := s.now()
		chat.Messages = append(chat.Messages, model.Message{ID: s.id(), ClientID: clientID, Role: "user", Text: input, CreatedAt: now, Status: "success"}, model.Message{ID: s.id(), Role: "assistant", Text: answer, CreatedAt: now, Status: "success"})
		chat.UpdatedAt = now
		project.UpdatedAt = now
		chat.Status = "success"
		chat.ErrorCategory = ""
		project.Status = "success"
		project.ErrorCategory = ""
		if extractErr == nil {
			browser.GlobalFacts = model.CloneFacts(facts.GlobalFacts)
			project.Facts = model.CloneFacts(facts.ProjectFacts)
		} else {
			chat.Status = "error"
			chat.ErrorCategory = completion.Category(extractErr)
			project.Status = "error"
			project.ErrorCategory = chat.ErrorCategory
		}
		claim = conversation.ClaimTitle(chat, input)
		out = model.CopyChat(chat, pid)
		return nil
	})
	if errors.Is(err, errReplay) {
		return out, nil
	}
	if err != nil {
		return model.Chat{}, err
	}
	if claim {
		s.titles.Start(ctx, sid, pid, cid, input)
	}
	return out, nil
}
func (s *Service) PauseTask(sid, pid, cid, tid string) (out model.Chat, err error) {
	err = s.state.Update(func(root *model.State) error {
		c := root.Chat(sid, pid, cid)
		if c == nil || c.Tasks[tid] == nil {
			return model.ErrNotFound
		}
		t := c.Tasks[tid]
		if t.Status == model.TaskStatusDone {
			return model.ErrValidation
		}
		t.Status = model.TaskStatusPaused
		t.UpdatedAt = s.now()
		b := root.Sessions[sid]
		if op := b.Pending; op != nil && op.ChatID == cid && op.TaskID == tid {
			if saved := c.Operations[op.ID]; saved != nil {
				saved.Status = "paused"
			}
			b.Pending = nil
		}
		out = model.CopyChat(c, pid)
		return nil
	})
	if err != nil {
		out = model.Chat{}
	}
	return
}
func (s *Service) Resume(ctx context.Context, sid, pid, cid, tid, input, clientID string) (model.Chat, []model.Task, error) {
	c := s.state.Snapshot().Chat(sid, pid, cid)
	if c == nil || c.Tasks[tid] == nil {
		return model.Chat{}, nil, model.ErrNotFound
	}
	wasPaused := c.Tasks[tid].Status == model.TaskStatusPaused
	if strings.TrimSpace(input) == "" {
		input = c.TaskInputs[tid]
		if saved := c.Operations[clientID]; clientID != "" && saved != nil {
			// A retry can arrive after this operation completed the task. Resolve
			// its original input before TaskInput checks replay versus new work.
			input = saved.Input
		} else if saved := c.Operations[c.TaskOperationIDs[tid]]; saved != nil && (saved.Status == "paused" || saved.Status == "failed") {
			input = saved.Input
			if clientID == "" {
				clientID = saved.ID
			}
		}

		if input == "" {
			input = c.Tasks[tid].CurrentStep
		}
	}
	out, candidates, err := s.TaskInput(ctx, sid, pid, cid, input, tid, clientID)
	if err == nil || !wasPaused {
		return out, candidates, err
	}
	restoreErr := s.state.Update(func(root *model.State) error {
		chat := root.Chat(sid, pid, cid)
		if chat == nil || chat.Tasks[tid] == nil {
			return model.ErrNotFound
		}
		if chat.Tasks[tid].Status != model.TaskStatusDone {
			chat.Tasks[tid].Status = model.TaskStatusPaused
			chat.Tasks[tid].UpdatedAt = s.now()
		}
		return nil
	})
	if restoreErr != nil {
		return model.Chat{}, nil, restoreErr
	}
	return out, candidates, err
}
