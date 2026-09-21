package conversation

import (
	"context"
	"errors"
	"strings"
	"time"

	"aichallenge/week_1/task_1/internal/application/completion"
	"aichallenge/week_1/task_1/internal/application/invariant"
	"aichallenge/week_1/task_1/internal/application/subagent"
	"aichallenge/week_1/task_1/internal/domain/model"
)

type ConversationState interface {
	Snapshot() model.State
	Update(func(*model.State) error) error
	Attach(string, model.Operation, context.CancelFunc)
	Detach(model.Operation)
}
type Extractor interface {
	Extract(context.Context, completion.MemoryInput) (completion.Facts, error)
}
type TitleStarter interface {
	Start(context.Context, string, string, string, string)
}
type Service struct {
	state     ConversationState
	client    CompletionClient
	extractor Extractor
	titles    TitleStarter
	settings  Settings
	id        func() string
	now       func() time.Time
	validator invariant.Pipeline
}

func New(state ConversationState, client CompletionClient, extractor Extractor, titles TitleStarter, settings Settings, id func() string, now func() time.Time, validator invariant.Pipeline) *Service {
	if validator == nil {
		panic("conversation invariant pipeline is required")
	}
	return &Service{state: state, client: client, extractor: extractor, titles: titles, settings: settings, id: id, now: now, validator: validator}
}

var errReplay = errors.New("already accepted")

func (s *Service) Send(ctx context.Context, sid, pid, cid, clientID, input string) (model.Chat, error) {
	return s.send(ctx, sid, pid, cid, clientID, input, false)
}
func (s *Service) send(ctx context.Context, sid, pid, cid, clientID, input string, retry bool) (model.Chat, error) {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(input) == "" {
		return model.Chat{}, model.ErrValidation
	}
	beforeValidation := s.state.Snapshot()
	if c := beforeValidation.Chat(sid, pid, cid); c == nil {
		return model.Chat{}, model.ErrNotFound
	} else {
		b, p := beforeValidation.Sessions[sid], beforeValidation.Project(sid, pid)
		violations, validationErr := s.validator.Validate(ctx, invariant.Input{Subject: invariant.UserInput, Text: input, Facts: append(model.CloneFacts(b.GlobalFacts), p.Facts...), Phase: "pre", ProjectID: pid, ChatID: cid})
		if validationErr != nil {
			return model.Chat{}, validationErr
		}
		if len(violations) != 0 {
			return s.refuse(ctx, sid, pid, cid, clientID, input, violations)
		}
	}
	var before model.State
	var out model.Chat
	claim := false
	op := model.Operation{ID: clientID, Generation: s.id(), ProjectID: pid, ChatID: cid, Input: input, Status: "pending"}
	err := s.state.Update(func(root *model.State) error {
		c := root.Chat(sid, pid, cid)
		if c == nil {
			return model.ErrNotFound
		}
		b := root.Sessions[sid]
		for _, m := range c.Messages {
			if m.ClientID == clientID {
				if m.Text != input {
					return model.ErrValidation
				}
				if m.Status == "success" || !retry {
					out = model.CopyChat(c, pid)
					return errReplay
				}
			}
		}
		if b.Pending != nil {
			return model.ErrBusy
		}
		b.Pending = &op
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
	b := before.Sessions[sid]
	p := before.Project(sid, pid)
	c := before.Chat(sid, pid, cid)
	prompt := BuildPrompt(s.settings, b, p, c, input)
	answer, mainErr := subagent.Run(call, s.client, "chat", prompt, subagent.Text)
	if mainErr == nil {
		mainErr = call.Err()
	}
	for index := 0; mainErr == nil && index < 3; index++ {
		violations, validationErr := s.validator.Validate(call, invariant.Input{Subject: invariant.ChatCandidate, Text: answer, Facts: append(model.CloneFacts(b.GlobalFacts), p.Facts...), Phase: "post", Index: index, ProjectID: pid, ChatID: cid})
		if validationErr != nil {
			mainErr = validationErr
			break
		}
		if len(violations) == 0 {
			break
		}
		if index == 2 {
			answer = refusal(violations)
			break
		}
		prompt = repairPrompt(prompt, violations)
		answer, mainErr = subagent.Run(call, s.client, "chat", prompt, subagent.Text)
		if mainErr == nil {
			mainErr = call.Err()
		}
	}
	var facts completion.Facts
	var extractErr error
	if mainErr == nil {
		facts, extractErr = s.extractor.Extract(call, MemoryInput(s.settings.Window, b, p, c, input, answer))
		if extractErr == nil {
			extractErr = call.Err()
		}
	}
	err = s.state.Update(func(root *model.State) error {
		c := root.Chat(sid, pid, cid)
		if c == nil {
			return model.ErrNotFound
		}
		if !root.Owns(sid, op) {
			return context.Canceled
		}
		b := root.Sessions[sid]
		p := root.Project(sid, pid)
		b.Pending = nil
		// Validator failures are fail-closed. Unlike a main-provider failure,
		// they must not turn the unaccepted input into a durable error message.
		if completion.Category(mainErr) == "invariant_validation" {
			out = model.CopyChat(c, pid)
			return nil
		}
		// Replace an explicit failed retry only in the final durable commit.
		messages := c.Messages[:0]
		for _, m := range c.Messages {
			if m.ClientID != clientID {
				messages = append(messages, m)
			}
		}
		c.Messages = messages
		now := s.now()
		c.UpdatedAt = now
		p.UpdatedAt = now
		user := model.Message{ID: s.id(), ClientID: clientID, Role: "user", Text: input, CreatedAt: now, Status: "success"}
		c.Status = "success"
		c.ErrorCategory = ""
		p.Status = "success"
		p.ErrorCategory = ""
		if mainErr != nil {
			user.Status = "error"
			user.ErrorCategory = completion.Category(mainErr)
			c.Status = "error"
			c.ErrorCategory = user.ErrorCategory
			p.Status = "error"
			p.ErrorCategory = user.ErrorCategory
			c.Messages = append(c.Messages, user)
		} else {
			c.Messages = append(c.Messages, user, model.Message{ID: s.id(), Role: "assistant", Text: answer, CreatedAt: now, Status: "success"})
			if extractErr == nil {
				b.GlobalFacts = model.CloneFacts(facts.GlobalFacts)
				p.Facts = model.CloneFacts(facts.ProjectFacts)
			} else {
				c.Status = "error"
				c.ErrorCategory = completion.Category(extractErr)
				p.Status = "error"
				p.ErrorCategory = c.ErrorCategory
			}
			claim = ClaimTitle(c, input)
		}
		out = model.CopyChat(c, pid)
		return nil
	})
	if err != nil {
		return model.Chat{}, err
	}
	if mainErr != nil {
		return model.Chat{}, mainErr
	}
	if claim {
		s.titles.Start(ctx, sid, pid, cid, input)
	}
	return out, nil
}

func (s *Service) refuse(ctx context.Context, sid, pid, cid, clientID, input string, violations []invariant.Violation) (model.Chat, error) {
	answer := refusal(violations)
	op := model.Operation{ID: clientID, Generation: s.id(), ProjectID: pid, ChatID: cid, Input: input, Status: "pending"}
	var before model.State
	var out model.Chat
	claim := false
	err := s.state.Update(func(root *model.State) error {
		chat := root.Chat(sid, pid, cid)
		if chat == nil {
			return model.ErrNotFound
		}
		for _, m := range chat.Messages {
			if m.ClientID == clientID {
				if m.Text != input {
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
	facts, extractErr := s.extractor.Extract(call, MemoryInput(s.settings.Window, b, p, c, input, answer))
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
		claim = ClaimTitle(chat, input)
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

func refusal(violations []invariant.Violation) string {
	names := make([]string, 0, len(violations))
	for _, v := range violations {
		names = append(names, v.InvariantID)
	}
	return "Я не могу безопасно выполнить этот запрос: сработали правила " + strings.Join(names, ", ") + ". Назовите доступное оборудование и зёрна либо уточните инвентарь — предложу безопасный вариант."
}
func repairPrompt(prompt []completion.Message, violations []invariant.Violation) []completion.Message {
	out := append([]completion.Message{}, prompt...)
	parts := make([]string, 0, len(violations))
	for _, v := range violations {
		parts = append(parts, v.InvariantID+": "+v.RepairInstruction)
	}
	out[0].Content += "\n\nREPAIR REQUIREMENTS (data):\n" + strings.Join(parts, "\n")
	return out
}
func (s *Service) Retry(ctx context.Context, sid, pid, cid, messageID string) (model.Chat, error) {
	c := s.state.Snapshot().Chat(sid, pid, cid)
	if c == nil {
		return model.Chat{}, model.ErrNotFound
	}
	for _, m := range c.Messages {
		if m.ID == messageID && m.Role == "user" && m.Status == "error" {
			return s.send(ctx, sid, pid, cid, m.ClientID, m.Text, true)
		}
	}
	return model.Chat{}, model.ErrValidation
}
