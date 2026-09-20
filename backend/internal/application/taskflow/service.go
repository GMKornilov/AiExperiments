// Package taskflow commits an autonomous proposal chain, conversation pair and memory snapshot.
package taskflow

import (
	"context"
	"errors"
	"strings"
	"time"

	"aichallenge/week_1/task_1/internal/application/completion"
	"aichallenge/week_1/task_1/internal/application/conversation"
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
}

func New(state TaskState, client CompletionClient, extractor Extractor, codec ProposalCodec, router Router, titles TitleStarter, settings conversation.Settings, id func() string, now func() time.Time) *Service {
	return &Service{state: state, client: client, extractor: extractor, codec: codec, router: router, titles: titles, settings: settings, id: id, now: now}
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
	var before model.State
	var out model.Chat
	var candidates []model.Task
	var previous model.Task
	var op model.Operation
	first, claim := false, false
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
		claim = conversation.ClaimTitle(c, input)
		before = root.Clone()
		return nil
	})
	if errors.Is(err, errReplay) || errors.Is(err, errCandidates) {
		return out, candidates, nil
	}
	if err != nil {
		return model.Chat{}, nil, err
	}
	if claim {
		s.titles.Start(ctx, sid, pid, cid, input)
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
	for step := 0; step < maxAutonomousSteps; step++ {
		messages := append([]completion.Message{}, baseMessages...)
		messages[0].Content += "\n\n" + s.codec.Instruction(next, first && step == 0, outputs, maxAutonomousSteps-step)
		var raw string
		raw, attemptErr = s.client.Complete(call, "task_step", messages)
		if attemptErr == nil {
			attemptErr = call.Err()
		}
		if attemptErr == nil {
			proposal, attemptErr = s.codec.Decode(raw)
		}
		if attemptErr == nil {
			next, attemptErr = model.ApplyProposal(next, proposal, first && step == 0)
			if attemptErr != nil {
				attemptErr = completion.Invalid()
			}
		}
		if attemptErr != nil {
			break
		}
		outputs = append(outputs, proposal.Output)
		if !model.ExpectsAgent(next) {
			break
		}
		if step == maxAutonomousSteps-1 {
			attemptErr = completion.Invalid()
		}
	}
	if attemptErr == nil {
		proposal.Output = strings.Join(outputs, "\n\n")
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
			operation.Status = "failed"
			return nil
		}
		now := s.now()
		next.UpdatedAt = now
		c.Tasks[next.ID] = &next
		operation.Status = "committed"
		c.Messages = append(c.Messages, model.Message{ID: s.id(), ClientID: clientID, Role: "user", Text: input, CreatedAt: now, Status: "success"}, model.Message{ID: s.id(), Role: "assistant", Text: proposal.Output, CreatedAt: now, Status: "success"})
		b.GlobalFacts = model.CloneFacts(facts.GlobalFacts)
		p.Facts = model.CloneFacts(facts.ProjectFacts)
		c.Status = "success"
		c.ErrorCategory = ""
		p.Status = "success"
		p.ErrorCategory = ""
		c.UpdatedAt = now
		p.UpdatedAt = now
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
	return out, nil, nil
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
