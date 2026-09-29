// Package statejson owns the versioned disk schema and atomic file replacement.
package statejson

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aichallenge/week_1/task_1/internal/adapters/extractjson"
	"aichallenge/week_1/task_1/internal/application/state"
	"aichallenge/week_1/task_1/internal/domain/model"
)

type Repository struct {
	path    string
	version int
}
type envelope struct {
	Version  int                     `json:"version"`
	Sessions map[string]*diskBrowser `json:"sessions"`
}

func Open(path string) (*Repository, model.State, error) {
	r := &Repository{path: path, version: 9}
	empty := model.State{Sessions: map[string]*model.Browser{}}
	if path == "" {
		return r, empty, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, empty, r.Save(empty)
	}
	if err != nil {
		return nil, empty, state.ErrStorage
	}
	var header struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(data, &header) != nil {
		return nil, empty, state.ErrStorage
	}
	if header.Version == 1 || header.Version == 2 {
		return r.resetLegacy(data, header.Version)
	}
	if header.Version != 3 && header.Version != 4 && header.Version != 5 && header.Version != 7 && header.Version != 8 && header.Version != 9 {
		return nil, empty, state.ErrStorage
	}
	migratedV8 := header.Version == 8
	decodeData := data
	if migratedV8 {
		decodeData, err = migrateV8(data)
		if err != nil {
			return nil, empty, state.ErrStorage
		}
	}
	var d envelope
	if extractjson.Decode(decodeData, &d) != nil || d.Sessions == nil {
		return nil, empty, state.ErrStorage
	}
	// Validate pointer/map shape before conversions; malformed state never resets.
	for sid, b := range d.Sessions {
		if strings.TrimSpace(sid) == "" || b == nil || b.Projects == nil {
			return nil, empty, state.ErrStorage
		}
		for _, p := range b.Projects {
			if p == nil || p.Chats == nil {
				return nil, empty, state.ErrStorage
			}
			for _, c := range p.Chats {
				if c == nil {
					return nil, empty, state.ErrStorage
				}
				for _, t := range c.Tasks {
					if t == nil {
						return nil, empty, state.ErrStorage
					}
				}
				for _, o := range c.Operations {
					if o == nil {
						return nil, empty, state.ErrStorage
					}
				}
			}
		}
		for _, p := range b.Profiles {
			if p == nil {
				return nil, empty, state.ErrStorage
			}
		}
	}
	before, _ := json.Marshal(d)
	restored := toState(diskState{Sessions: d.Sessions})
	if migratedV8 {
		normalizeV8Clarifications(&restored)
	}
	if err := normalize(&restored); err != nil {
		return nil, empty, state.ErrStorage
	}
	if err := validate(restored); err != nil {
		return nil, empty, err
	}
	r.version = 9
	// Only normalized/recovered snapshots need a startup write. Archive the
	// exact source before any migration, retaining the original version for rollback.
	after, _ := json.Marshal(envelope{Version: r.version, Sessions: fromState(restored).Sessions})
	if migratedV8 || !bytes.Equal(before, after) {
		if err := archive(path+".backup-"+time.Now().UTC().Format("20060102T150405.000000000"), data); err != nil {
			return nil, empty, state.ErrStorage
		}
		if err := r.Save(restored); err != nil {
			return nil, empty, err
		}
	}
	return r, restored, nil
}

func normalizeV8Clarifications(stateValue *model.State) {
	for _, browser := range stateValue.Sessions {
		for _, project := range browser.Projects {
			for _, chat := range project.Chats {
				for _, task := range chat.Tasks {
					if task.Stage == model.TaskStageClarifyInput {
						task.CurrentStep = model.ClarificationStep
						task.ExpectedAction = model.ClarificationExpectedAction
						task.Plan = []model.TaskPlanItem{}
						task.CurrentPlanItem = ""
					}
				}
			}
		}
	}
}

// migrateV8 consumes the removed equipment domain only at the disk boundary.
// The remaining document is still decoded strictly by the v9 DTO afterwards.
func migrateV8(data []byte) ([]byte, error) {
	var value any
	if extractjson.Decode(data, &value) != nil {
		return nil, state.ErrStorage
	}
	if err := dropV8TaskEquipment(value); err != nil {
		return nil, err
	}
	result, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func dropV8TaskEquipment(value any) error {
	root, ok := value.(map[string]any)
	if !ok {
		return state.ErrStorage
	}
	sessions, ok := root["sessions"].(map[string]any)
	if !ok {
		return state.ErrStorage
	}
	for _, browserValue := range sessions {
		browser, ok := browserValue.(map[string]any)
		if !ok {
			return state.ErrStorage
		}
		projects, ok := browser["projects"].(map[string]any)
		if !ok {
			return state.ErrStorage
		}
		for _, projectValue := range projects {
			project, ok := projectValue.(map[string]any)
			if !ok {
				return state.ErrStorage
			}
			chats, ok := project["chats"].(map[string]any)
			if !ok {
				return state.ErrStorage
			}
			for _, chatValue := range chats {
				chat, ok := chatValue.(map[string]any)
				if !ok {
					return state.ErrStorage
				}
				tasks, ok := chat["tasks"].(map[string]any)
				if !ok {
					return state.ErrStorage
				}
				for _, taskValue := range tasks {
					task, ok := taskValue.(map[string]any)
					if !ok {
						return state.ErrStorage
					}
					if contextValue, exists := task["equipment_context"]; exists {
						if _, ok := contextValue.(map[string]any); !ok {
							return state.ErrStorage
						}
						delete(task, "equipment_context")
					}
					if confirmed, exists := task["equipment_confirmed"]; exists {
						if _, ok := confirmed.(bool); !ok {
							return state.ErrStorage
						}
						delete(task, "equipment_confirmed")
					}
				}
			}
		}
	}
	return nil
}
func (r *Repository) Save(value model.State) error {
	if r.path == "" {
		return nil
	}
	d := fromState(value)
	data, err := json.MarshalIndent(envelope{Version: r.version, Sessions: d.Sessions}, "", "  ")
	if err != nil {
		return state.ErrStorage
	}
	if err = replace(r.path, data); err != nil {
		return state.ErrStorage
	}
	return nil
}
func normalize(s *model.State) error {
	for _, b := range s.Sessions {
		b.GlobalFacts = model.CloneFacts(b.GlobalFacts)
		if !model.ValidFacts(b.GlobalFacts) {
			return state.ErrStorage
		}
		if b.Profiles == nil {
			b.Profiles = map[string]*model.CustomProfile{}
		}
		if !model.IsKnownProfile(b, b.ActiveProfileID) {
			b.ActiveProfileID = model.BaristaProfileID
		}
		for _, p := range b.Projects {
			p.Facts = model.CloneFacts(p.Facts)
			if !model.ValidFacts(p.Facts) {
				return state.ErrStorage
			}
			for _, c := range p.Chats {
				if c.Messages == nil {
					c.Messages = []model.Message{}
				}
				if c.Tasks == nil {
					c.Tasks = map[string]*model.Task{}
				}
				if c.TaskInputs == nil {
					c.TaskInputs = map[string]string{}
				}
				if c.Operations == nil {
					c.Operations = map[string]*model.Operation{}
				}
				for tid, t := range c.Tasks {
					if t.ID != tid || !t.Stage.Valid() || !t.Status.Valid() {
						return state.ErrStorage
					}
					if t.Stage == model.TaskStageClarifyInput && t.ExpectedAction == "agent" {
						t.CurrentStep = model.ClarificationStep
						t.ExpectedAction = model.ClarificationExpectedAction
					}
					if t.Plan == nil {
						t.Plan = []model.TaskPlanItem{}
					}
					if !t.ValidationResult.Status.Valid() {
						t.ValidationResult = model.ValidationResult{Status: model.ValidationNotValidated}
						if t.Stage == model.TaskStageUserFeedback || t.Status == model.TaskStatusDone {
							t.ValidationResult.LegacyUnvalidated = true
						}
					}
					// Older current versions may predate the plan field. Preserve their
					// confirmed step as the minimal recovery plan, never infer new work.
					if len(t.Plan) == 0 && t.Stage != model.TaskStageClarifyInput {
						title := strings.TrimSpace(t.CurrentStep)
						if title == "" {
							title = strings.TrimSpace(t.Title)
						}
						if title == "" {
							return state.ErrStorage
						}
						status := model.TaskPlanItemCurrent
						t.CurrentPlanItem = t.ID + "-recovered"
						if t.Stage == model.TaskStageUserFeedback || t.Status == model.TaskStatusDone {
							status = model.TaskPlanItemCompleted
							t.CurrentPlanItem = ""
						}
						t.Plan = []model.TaskPlanItem{{ID: t.ID + "-recovered", Title: title, Status: status}}
					}
					if err := model.ValidateTaskPlan(t); err != nil {
						return err
					}
				}
				if c.TitleStatus == "" {
					c.TitleStatus = "idle"
					if len(c.Messages) > 0 {
						c.TitleStatus = "pending"
					}
				}
				if c.TitleStatus == "pending" {
					for _, m := range c.Messages {
						if m.Role == "user" {
							c.Title = model.TitleFallback(m.Text)
							break
						}
					}
					c.TitleStatus = "fallback"
				}
			}
		}
		if op := b.Pending; op != nil {
			if p := b.Projects[op.ProjectID]; p != nil {
				if c := p.Chats[op.ChatID]; c != nil {
					if t := c.Tasks[op.TaskID]; t != nil {
						t.Status = model.TaskStatusPaused
					}
					if saved := c.Operations[op.ID]; saved != nil {
						saved.Status = "paused"
					}
				}
			}
			b.Pending = nil
		}
	}
	return nil
}
func replace(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".memory-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Recognized historical formats are archived before resetting. Renaming the
// dialog directory retains a recoverable backup without touching unrelated data.
func (r *Repository) resetLegacy(data []byte, version int) (*Repository, model.State, error) {
	empty := model.State{Sessions: map[string]*model.Browser{}}
	var legacy struct {
		Version  int                                   `json:"version"`
		Sessions map[string]map[string]json.RawMessage `json:"sessions"`
	}
	if extractjson.Decode(data, &legacy) != nil || legacy.Sessions == nil {
		return nil, empty, state.ErrStorage
	}
	for _, s := range legacy.Sessions {
		key := "dialogs"
		if version == 2 {
			key = "dialog_ids"
		}
		if len(s) > 2 || s[key] == nil {
			return nil, empty, state.ErrStorage
		}
		var list []json.RawMessage
		if json.Unmarshal(s[key], &list) != nil || list == nil {
			return nil, empty, state.ErrStorage
		}
		for _, item := range list {
			if version == 2 {
				var id string
				if json.Unmarshal(item, &id) != nil || len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
					return nil, empty, state.ErrStorage
				}
			} else {
				var dialog struct {
					Dialog              json.RawMessage `json:"dialog"`
					Snapshot            json.RawMessage `json:"snapshot"`
					AgentMessages       json.RawMessage `json:"agent_messages"`
					Branches            json.RawMessage `json:"branches"`
					FactsReadyMessageID string          `json:"facts_ready_message_id"`
				}
				if extractjson.Decode(item, &dialog) != nil || len(dialog.Dialog) == 0 || len(dialog.Snapshot) == 0 || string(dialog.Dialog) == "null" || string(dialog.Snapshot) == "null" {
					return nil, empty, state.ErrStorage
				}
			}
		}
		for key := range s {
			if key != "dialogs" && key != "dialog_ids" && key != "selected_dialog_id" {
				return nil, empty, state.ErrStorage
			}
		}
	}
	base := filepath.Base(r.path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if stem == "" || stem == "." || stem == ".." {
		return nil, empty, state.ErrStorage
	}
	backup := r.path + ".legacy-backup"
	f, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, empty, state.ErrStorage
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return nil, empty, state.ErrStorage
	}
	dir := strings.TrimSuffix(r.path, filepath.Ext(r.path))
	if dir == r.path {
		dir += ".dialogs"
	}
	moved := false
	if _, err := os.Stat(dir); err == nil {
		if _, err := os.Stat(dir + ".legacy-backup"); !errors.Is(err, os.ErrNotExist) {
			return nil, empty, state.ErrStorage
		}
		if os.Rename(dir, dir+".legacy-backup") != nil {
			return nil, empty, state.ErrStorage
		}
		moved = true
	}
	if err = r.Save(empty); err != nil {
		if moved {
			_ = os.Rename(dir+".legacy-backup", dir)
		}
		return nil, empty, err
	}
	return r, empty, nil
}

func archive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	return err
}
