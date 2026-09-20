package statejson

import (
	"strings"

	"aichallenge/week_1/task_1/internal/application/state"
	"aichallenge/week_1/task_1/internal/domain/model"
)

// Validate before a recovery write, so malformed current state remains intact.
func validate(value model.State) error {
	for _, b := range value.Sessions {
		names := map[string]bool{}
		for id, p := range b.Profiles {
			name := strings.ToLower(strings.TrimSpace(p.Name))
			if id == "" || id != p.ID || name == "" || model.RuneCount(p.Name) > 60 || strings.TrimSpace(p.Style) == "" || strings.TrimSpace(p.Constraints) == "" || strings.TrimSpace(p.AdditionalContext) == "" || names[name] {
				return state.ErrStorage
			}
			names[name] = true
		}
		for pid, p := range b.Projects {
			if pid == "" || p.ID != pid || strings.TrimSpace(p.Title) == "" {
				return state.ErrStorage
			}
			for cid, c := range p.Chats {
				if cid == "" || c.ID != cid || strings.TrimSpace(c.Title) == "" {
					return state.ErrStorage
				}
				switch c.TitleStatus {
				case "idle", "pending", "success", "fallback":
				default:
					return state.ErrStorage
				}
				for _, m := range c.Messages {
					if m.ID == "" || (m.Role != "user" && m.Role != "assistant") || (m.Status != "success" && m.Status != "error") {
						return state.ErrStorage
					}
				}
				for _, t := range c.Tasks {
					if strings.TrimSpace(t.Title) == "" || strings.TrimSpace(t.CurrentStep) == "" || strings.TrimSpace(t.ExpectedAction) == "" {
						return state.ErrStorage
					}
				}
				for id, op := range c.Operations {
					if id == "" || op.ID != id || op.ProjectID != pid || op.ChatID != cid || op.Generation == "" || op.TaskID == "" || c.Tasks[op.TaskID] == nil {
						return state.ErrStorage
					}
					switch op.Status {
					case "pending", "paused", "failed", "committed":
					default:
						return state.ErrStorage
					}
				}
				for tid, id := range c.TaskOperationIDs {
					if c.Tasks[tid] == nil || c.Operations[id] == nil || c.Operations[id].TaskID != tid {
						return state.ErrStorage
					}
				}
			}
		}
	}
	return nil
}
