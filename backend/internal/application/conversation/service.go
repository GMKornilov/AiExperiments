package conversation

import (
	"context"
	"errors"
	"strings"
	"time"

	"aichallenge/week_1/task_1/internal/application/completion"
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
}

func New(state ConversationState, client CompletionClient, extractor Extractor, titles TitleStarter, settings Settings, id func() string, now func() time.Time) *Service {
	return &Service{state: state, client: client, extractor: extractor, titles: titles, settings: settings, id: id, now: now}
}

var errReplay = errors.New("already accepted")

func (s *Service) Send(ctx context.Context, sid, pid, cid, clientID, input string) (model.Chat, error) {
	return s.send(ctx, sid, pid, cid, clientID, input, false)
}
func (s *Service) send(ctx context.Context, sid, pid, cid, clientID, input string, retry bool) (model.Chat, error) {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(input) == "" {
		return model.Chat{}, model.ErrValidation
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
		claim = ClaimTitle(c, input)
		before = root.Clone()
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
	call, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	s.state.Attach(sid, op, cancel)
	defer s.state.Detach(op)
	b := before.Sessions[sid]
	p := before.Project(sid, pid)
	c := before.Chat(sid, pid, cid)
	answer, mainErr := s.client.Complete(call, "chat", BuildPrompt(s.settings, b, p, c, input))
	if mainErr == nil {
		mainErr = call.Err()
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
	return out, nil
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
