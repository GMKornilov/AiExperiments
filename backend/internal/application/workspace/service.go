// Package workspace implements owner-scoped projects, chats, profiles and memory.
package workspace

import (
	"sort"
	"strings"
	"time"

	"aichallenge/week_1/task_1/internal/domain/model"
)

type StateReader interface{ Snapshot() model.State }
type StateCommitter interface {
	Update(func(*model.State) error) error
}
type Service struct {
	reader StateReader
	writer StateCommitter
	id     func() string
	now    func() time.Time
}

func New(reader StateReader, writer StateCommitter, id func() string, now func() time.Time) *Service {
	return &Service{reader: reader, writer: writer, id: id, now: now}
}
func (s *Service) CreateProject(sid, title string) (out model.Project, err error) {
	err = s.writer.Update(func(root *model.State) error {
		if strings.TrimSpace(title) == "" {
			title = "Новый проект"
		}
		now := s.now()
		id := s.id()
		p := &model.ProjectState{ID: id, Title: title, Facts: []string{}, Chats: map[string]*model.ChatState{}, CreatedAt: now, UpdatedAt: now, Status: "idle"}
		b := root.Browser(sid)
		b.Projects[id] = p
		b.SelectedProjectID = id
		b.SelectedChatID = ""
		out = model.CopyProject(p)
		return nil
	})
	if err != nil {
		out = model.Project{}
	}
	return
}
func (s *Service) RenameProject(sid, pid, title string) (out model.Project, err error) {
	title = strings.TrimSpace(title)
	if title == "" || model.RuneCount(title) > 100 {
		return out, model.ErrValidation
	}
	err = s.writer.Update(func(root *model.State) error {
		p := root.Project(sid, pid)
		if p == nil {
			return model.ErrNotFound
		}
		p.Title = title
		p.UpdatedAt = s.now()
		out = model.CopyProject(p)
		return nil
	})
	if err != nil {
		out = model.Project{}
	}
	return
}
func (s *Service) List(sid string) model.Listing {
	root := s.reader.Snapshot()
	out := model.Listing{Projects: []model.Project{}}
	b := root.Sessions[sid]
	if b == nil {
		return out
	}
	out.SelectedProjectID = b.SelectedProjectID
	out.SelectedChatID = b.SelectedChatID
	for _, p := range b.Projects {
		out.Projects = append(out.Projects, model.CopyProject(p))
	}
	sort.Slice(out.Projects, func(i, j int) bool { return out.Projects[i].UpdatedAt.After(out.Projects[j].UpdatedAt) })
	return out
}
func (s *Service) GetProject(sid, pid string) (model.Project, bool) {
	p := s.reader.Snapshot().Project(sid, pid)
	if p == nil {
		return model.Project{}, false
	}
	return model.CopyProject(p), true
}
func (s *Service) SelectProject(sid, pid string) error {
	return s.writer.Update(func(root *model.State) error {
		if root.Project(sid, pid) == nil {
			return model.ErrNotFound
		}
		b := root.Sessions[sid]
		b.SelectedProjectID = pid
		b.SelectedChatID = ""
		return nil
	})
}
func (s *Service) DeleteProject(sid, pid string) error {
	return s.writer.Update(func(root *model.State) error {
		if root.Project(sid, pid) == nil {
			return model.ErrNotFound
		}
		b := root.Sessions[sid]
		if b.Pending != nil && b.Pending.ProjectID == pid {
			b.Pending = nil
		}
		delete(b.Projects, pid)
		if b.SelectedProjectID == pid {
			b.SelectedProjectID = ""
			b.SelectedChatID = ""
		}
		return nil
	})
}
func (s *Service) CreateChat(sid, pid, title string) (out model.Chat, err error) {
	err = s.writer.Update(func(root *model.State) error {
		p := root.Project(sid, pid)
		if p == nil {
			return model.ErrNotFound
		}
		if strings.TrimSpace(title) == "" {
			title = "Новый чат"
		}
		now := s.now()
		id := s.id()
		c := &model.ChatState{ID: id, Title: title, TitleStatus: "idle", Messages: []model.Message{}, Tasks: map[string]*model.Task{}, TaskInputs: map[string]string{}, Operations: map[string]*model.Operation{}, CreatedAt: now, UpdatedAt: now, Status: "idle"}
		p.Chats[id] = c
		p.UpdatedAt = now
		b := root.Sessions[sid]
		b.SelectedProjectID = pid
		b.SelectedChatID = id
		out = model.CopyChat(c, pid)
		return nil
	})
	if err != nil {
		out = model.Chat{}
	}
	return
}
func (s *Service) GetChat(sid, pid, cid string) (model.Chat, bool) {
	root := s.reader.Snapshot()
	c := root.Chat(sid, pid, cid)
	if c == nil {
		return model.Chat{}, false
	}
	out := model.CopyChat(c, pid)
	if p := root.Sessions[sid].Pending; p != nil && p.ChatID == cid {
		out.MemoryStatus = "updating"
	}
	return out, true
}
func (s *Service) HasChat(cid string) bool {
	if strings.TrimSpace(cid) == "" {
		return false
	}
	for _, b := range s.reader.Snapshot().Sessions {
		for _, p := range b.Projects {
			if p.Chats[cid] != nil {
				return true
			}
		}
	}
	return false
}
func (s *Service) SelectChat(sid, pid, cid string) error {
	return s.writer.Update(func(root *model.State) error {
		if root.Chat(sid, pid, cid) == nil {
			return model.ErrNotFound
		}
		b := root.Sessions[sid]
		b.SelectedProjectID = pid
		b.SelectedChatID = cid
		return nil
	})
}
func (s *Service) DeleteChat(sid, pid, cid string) error {
	return s.writer.Update(func(root *model.State) error {
		p := root.Project(sid, pid)
		if p == nil || p.Chats[cid] == nil {
			return model.ErrNotFound
		}
		b := root.Sessions[sid]
		if b.Pending != nil && b.Pending.ChatID == cid {
			b.Pending = nil
		}
		delete(p.Chats, cid)
		if b.SelectedChatID == cid {
			b.SelectedChatID = ""
		}
		p.UpdatedAt = s.now()
		return nil
	})
}
func (s *Service) ReadMemory(sid, pid string) (model.Memory, bool) {
	root := s.reader.Snapshot()
	p := root.Project(sid, pid)
	if p == nil {
		return model.Memory{}, false
	}
	b := root.Sessions[sid]
	out := model.Memory{GlobalFacts: model.CloneFacts(b.GlobalFacts), ProjectFacts: model.CloneFacts(p.Facts), Status: p.Status, ErrorCategory: p.ErrorCategory}
	if b.Pending != nil && b.Pending.ProjectID == pid {
		out.Status = "updating"
	}
	return out, true
}
func (s *Service) ClearGlobal(sid string) error {
	return s.writer.Update(func(root *model.State) error {
		b := root.Browser(sid)
		if b.Pending != nil {
			return model.ErrBusy
		}
		b.GlobalFacts = []string{}
		return nil
	})
}
func (s *Service) ClearProject(sid, pid string) error {
	return s.writer.Update(func(root *model.State) error {
		p := root.Project(sid, pid)
		if p == nil {
			return model.ErrNotFound
		}
		b := root.Sessions[sid]
		if b.Pending != nil && b.Pending.ProjectID == pid {
			return model.ErrBusy
		}
		p.Facts = []string{}
		p.Status = "success"
		p.ErrorCategory = ""
		return nil
	})
}
func (s *Service) ListProfiles(sid string) model.ProfileListing {
	root := s.reader.Snapshot()
	return model.ProfilesFor(root.Browser(sid))
}
func (s *Service) CreateProfile(sid, name, style, constraints, additional string) (out model.ProfileListing, err error) {
	name = strings.TrimSpace(name)
	style = strings.TrimSpace(style)
	constraints = strings.TrimSpace(constraints)
	additional = strings.TrimSpace(additional)
	if name == "" || model.RuneCount(name) > 60 || style == "" || constraints == "" || additional == "" {
		return out, model.ErrValidation
	}
	err = s.writer.Update(func(root *model.State) error {
		b := root.Browser(sid)
		if model.ProfileNameExists(b, name) {
			return model.ErrValidation
		}
		id := s.id()
		b.Profiles[id] = &model.CustomProfile{ID: id, Name: name, Style: style, Constraints: constraints, AdditionalContext: additional}
		b.ActiveProfileID = id
		out = model.ProfilesFor(b)
		return nil
	})
	if err != nil {
		out = model.ProfileListing{}
	}
	return
}
func (s *Service) SelectProfile(sid, id string) (out model.ProfileListing, err error) {
	err = s.writer.Update(func(root *model.State) error {
		b := root.Browser(sid)
		if !model.IsKnownProfile(b, id) {
			return model.ErrNotFound
		}
		b.ActiveProfileID = id
		out = model.ProfilesFor(b)
		return nil
	})
	if err != nil {
		out = model.ProfileListing{}
	}
	return
}
func (s *Service) DeleteProfile(sid, id string) (out model.ProfileListing, err error) {
	err = s.writer.Update(func(root *model.State) error {
		b := root.Browser(sid)
		if b.Profiles[id] == nil {
			return model.ErrNotFound
		}
		delete(b.Profiles, id)
		if b.ActiveProfileID == id {
			b.ActiveProfileID = model.BaristaProfileID
		}
		out = model.ProfilesFor(b)
		return nil
	})
	if err != nil {
		out = model.ProfileListing{}
	}
	return
}
