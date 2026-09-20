package model

// Clone copies all mutable collections. Callers cannot change published state.
func (s State) Clone() State {
	out := State{Sessions: make(map[string]*Browser, len(s.Sessions))}
	for sid, b := range s.Sessions {
		v := *b
		v.GlobalFacts = CloneFacts(b.GlobalFacts)
		if b.Pending != nil {
			x := *b.Pending
			v.Pending = &x
		}
		v.Profiles = make(map[string]*CustomProfile, len(b.Profiles))
		for id, p := range b.Profiles {
			x := *p
			v.Profiles[id] = &x
		}
		v.Projects = make(map[string]*ProjectState, len(b.Projects))
		for pid, p := range b.Projects {
			x := *p
			x.Facts = CloneFacts(p.Facts)
			x.Chats = make(map[string]*ChatState, len(p.Chats))
			for cid, c := range p.Chats {
				y := *c
				y.Messages = append([]Message{}, c.Messages...)
				y.Tasks = make(map[string]*Task, len(c.Tasks))
				for tid, t := range c.Tasks {
					z := *t
					z.Plan = append([]TaskPlanItem{}, t.Plan...)
					y.Tasks[tid] = &z
				}
				y.TaskInputs = make(map[string]string, len(c.TaskInputs))
				for k, v := range c.TaskInputs {
					y.TaskInputs[k] = v
				}
				y.TaskOperationIDs = make(map[string]string, len(c.TaskOperationIDs))
				for k, v := range c.TaskOperationIDs {
					y.TaskOperationIDs[k] = v
				}
				y.Operations = make(map[string]*Operation, len(c.Operations))
				for k, v := range c.Operations {
					z := *v
					y.Operations[k] = &z
				}
				x.Chats[cid] = &y
			}
			v.Projects[pid] = &x
		}
		out.Sessions[sid] = &v
	}
	return out
}
func (s *State) Browser(sid string) *Browser {
	b := s.Sessions[sid]
	if b == nil {
		b = &Browser{GlobalFacts: []string{}, Projects: map[string]*ProjectState{}, Profiles: map[string]*CustomProfile{}, ActiveProfileID: BaristaProfileID}
		s.Sessions[sid] = b
	}
	return b
}
func (s State) Project(sid, pid string) *ProjectState {
	if s.Sessions[sid] == nil {
		return nil
	}
	return s.Sessions[sid].Projects[pid]
}
func (s State) Chat(sid, pid, cid string) *ChatState {
	p := s.Project(sid, pid)
	if p == nil {
		return nil
	}
	return p.Chats[cid]
}
func (s State) Owns(sid string, op Operation) bool {
	b := s.Sessions[sid]
	return b != nil && b.Pending != nil && b.Pending.Generation == op.Generation && b.Pending.ID == op.ID
}
