package model

import (
	"sort"
	"strings"
)

func ProfilesFor(b *Browser) ProfileListing {
	profiles := make([]Profile, 0, len(builtInProfiles)+len(b.Profiles))
	profiles = append(profiles, builtInProfiles...)
	for _, value := range b.Profiles {
		profiles = append(profiles, Profile{ID: value.ID, Name: value.Name, Style: value.Style, Constraints: value.Constraints, AdditionalContext: value.AdditionalContext})
	}
	sort.Slice(profiles[len(builtInProfiles):], func(i, j int) bool {
		return strings.ToLower(profiles[len(builtInProfiles)+i].Name) < strings.ToLower(profiles[len(builtInProfiles)+j].Name)
	})
	return ProfileListing{Profiles: profiles, ActiveProfileID: b.ActiveProfileID}
}

func IsKnownProfile(b *Browser, profileID string) bool {
	if profileID == BaristaProfileID || profileID == EquipmentProfileID {
		return true
	}
	return b != nil && b.Profiles[profileID] != nil
}

func ProfileNameExists(b *Browser, name string) bool {
	for _, value := range builtInProfiles {
		if strings.EqualFold(value.Name, name) {
			return true
		}
	}
	for _, value := range b.Profiles {
		if strings.EqualFold(value.Name, name) {
			return true
		}
	}
	return false
}

func ActiveProfile(b *Browser) Profile {
	for _, value := range builtInProfiles {
		if value.ID == b.ActiveProfileID {
			return value
		}
	}
	if value := b.Profiles[b.ActiveProfileID]; value != nil {
		return Profile{ID: value.ID, Name: value.Name, Style: value.Style, Constraints: value.Constraints, AdditionalContext: value.AdditionalContext}
	}
	return builtInProfiles[0]
}
func ValidTitle(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n`") || strings.Contains(value, "**") || strings.Contains(value, "__") || strings.Contains(value, "](") || strings.HasPrefix(value, "#") || RuneCount(value) > 60 {
		return "", false
	}
	return value, true
}
func TitleFallback(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return TruncateRunes(value, 60)
}
func RuneCount(value string) int { return len([]rune(value)) }
func TruncateRunes(value string, limit int) string {
	r := []rune(value)
	if len(r) > limit {
		r = r[:limit]
	}
	return string(r)
}

func ValidateTaskPlan(task *Task) error {
	return validateTaskPlan(task, false)
}

// validateTaskPlan permits one in-memory execution→feedback candidate before
// taskflow has run the mandatory semantic gate. It must never be persisted in
// that form.
func validateTaskPlan(task *Task, allowUngatedFeedback bool) error {
	if task == nil || !task.Stage.Valid() || !task.Status.Valid() || (task.Status == TaskStatusDone && task.Stage != TaskStageUserFeedback) {
		return ErrValidation
	}
	if !task.ValidationResult.Status.Valid() || (task.ValidationResult.Status == ValidationPassed && strings.TrimSpace(task.ValidationResult.Summary) == "") || (task.ValidationResult.Status == ValidationNotValidated && (task.ValidationResult.Summary != "" || task.ValidationResult.LegacyUnvalidated && task.Stage != TaskStageUserFeedback && task.Status != TaskStatusDone)) {
		return ErrValidation
	}
	if (task.Stage == TaskStageUserFeedback || task.Status == TaskStatusDone) && !allowUngatedFeedback && !task.ValidationResult.LegacyUnvalidated && task.ValidationResult.Status != ValidationPassed {
		return ErrValidation
	}
	if len(task.Plan) == 0 {
		if task.CurrentPlanItem != "" || task.Stage != TaskStageClarifyInput {
			return ErrValidation
		}
		return nil
	}
	ids := make(map[string]struct{}, len(task.Plan))
	current := ""
	for _, item := range task.Plan {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Title) == "" || !item.Status.Valid() || (item.Stage != "" && !item.Stage.Valid()) {
			return ErrValidation
		}
		if _, exists := ids[item.ID]; exists {
			return ErrValidation
		}
		ids[item.ID] = struct{}{}
		if item.Status == TaskPlanItemCurrent {
			if current != "" {
				return ErrValidation
			}
			current = item.ID
		}
	}
	if task.Status == TaskStatusDone || task.Stage == TaskStageUserFeedback {
		if current != "" || task.CurrentPlanItem != "" {
			return ErrValidation
		}
		for _, item := range task.Plan {
			if item.Status != TaskPlanItemCompleted {
				return ErrValidation
			}
		}
		return nil
	}
	if task.Stage == TaskStageClarifyInput {
		return ErrValidation
	}
	if current == "" || task.CurrentPlanItem != current {
		return ErrValidation
	}
	return nil
}

func CloneFacts(v []string) []string { return append([]string{}, v...) }
func CopyChat(c *ChatState, pid string) Chat {
	tasks := make([]Task, 0, len(c.Tasks))
	for _, task := range c.Tasks {
		copy := *task
		copy.Plan = append([]TaskPlanItem{}, task.Plan...)
		tasks = append(tasks, copy)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].UpdatedAt.After(tasks[j].UpdatedAt) })
	return Chat{ID: c.ID, ProjectID: pid, Title: c.Title, TitleStatus: c.TitleStatus, Messages: append([]Message{}, c.Messages...), Tasks: tasks, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, MemoryStatus: c.Status, MemoryErrorCategory: c.ErrorCategory}
}
func CopyProject(p *ProjectState) Project {
	o := Project{ID: p.ID, Title: p.Title, Chats: []Chat{}, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
	for _, c := range p.Chats {
		o.Chats = append(o.Chats, CopyChat(c, p.ID))
	}
	sort.Slice(o.Chats, func(i, j int) bool { return o.Chats[i].UpdatedAt.After(o.Chats[j].UpdatedAt) })
	return o
}
func ValidFacts(f []string) bool {
	seen := map[string]bool{}
	for _, x := range f {
		if strings.TrimSpace(x) == "" || seen[x] {
			return false
		}
		seen[x] = true
	}
	return true
}
