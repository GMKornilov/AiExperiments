// Explicit boundary DTOs preserve the wire schema independently of domain models.
package httpapi

import (
	"time"

	"aichallenge/week_1/task_1/internal/domain/model"
)

type viewMessage struct {
	ID            string    `json:"id"`
	ClientID      string    `json:"client_message_id,omitempty"`
	Role          string    `json:"role"`
	Text          string    `json:"text"`
	CreatedAt     time.Time `json:"created_at"`
	Status        string    `json:"status"`
	ErrorCategory string    `json:"error_category,omitempty"`
}
type viewChat struct {
	ID                  string        `json:"id"`
	ProjectID           string        `json:"project_id"`
	Title               string        `json:"title"`
	TitleStatus         string        `json:"title_status"`
	Messages            []viewMessage `json:"messages"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
	MemoryStatus        string        `json:"memory_status"`
	MemoryErrorCategory string        `json:"memory_error_category,omitempty"`
	Tasks               []viewTask    `json:"tasks"`
}
type viewTaskPlanItem struct {
	ID     string                   `json:"id"`
	Title  string                   `json:"title"`
	Status model.TaskPlanItemStatus `json:"status"`
	Stage  model.TaskStage          `json:"stage,omitempty"`
}
type viewTask struct {
	ID               string               `json:"id"`
	Title            string               `json:"title"`
	Description      string               `json:"description"`
	Stage            model.TaskStage      `json:"stage"`
	CurrentStep      string               `json:"current_step"`
	ExpectedAction   string               `json:"expected_action"`
	Status           model.TaskStatus     `json:"status"`
	Plan             []viewTaskPlanItem   `json:"plan"`
	CurrentPlanItem  string               `json:"current_plan_item,omitempty"`
	ValidationResult viewValidationResult `json:"validation_result"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}
type viewValidationResult struct {
	Status            model.ValidationResultStatus `json:"status"`
	Summary           string                       `json:"summary,omitempty"`
	LegacyUnvalidated bool                         `json:"legacy_unvalidated,omitempty"`
}
type viewProject struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Chats     []viewChat `json:"chats"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
type viewMemory struct {
	GlobalFacts   []string `json:"global_facts"`
	ProjectFacts  []string `json:"project_facts"`
	Status        string   `json:"status"`
	ErrorCategory string   `json:"error_category,omitempty"`
}
type viewListing struct {
	Projects          []viewProject `json:"projects"`
	SelectedProjectID string        `json:"selected_project_id,omitempty"`
	SelectedChatID    string        `json:"selected_chat_id,omitempty"`
}
type viewProfile struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Style             string `json:"style"`
	Constraints       string `json:"constraints"`
	AdditionalContext string `json:"additional_context"`
	BuiltIn           bool   `json:"built_in"`
}
type viewProfileListing struct {
	Profiles        []viewProfile `json:"profiles"`
	ActiveProfileID string        `json:"active_profile_id"`
}

func fromMessage(v model.Message) viewMessage {
	var out viewMessage
	out.ID = v.ID
	out.ClientID = v.ClientID
	out.Role = v.Role
	out.Text = v.Text
	out.CreatedAt = v.CreatedAt
	out.Status = v.Status
	out.ErrorCategory = v.ErrorCategory
	return out
}
func fromChat(v model.Chat) viewChat {
	var out viewChat
	out.ID = v.ID
	out.ProjectID = v.ProjectID
	out.Title = v.Title
	out.TitleStatus = v.TitleStatus
	if v.Messages != nil {
		out.Messages = make([]viewMessage, len(v.Messages))
		for i := range v.Messages {
			out.Messages[i] = fromMessage(v.Messages[i])
		}
	}
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	out.MemoryStatus = v.MemoryStatus
	out.MemoryErrorCategory = v.MemoryErrorCategory
	if v.Tasks != nil {
		out.Tasks = make([]viewTask, len(v.Tasks))
		for i := range v.Tasks {
			out.Tasks[i] = fromTask(v.Tasks[i])
		}
	}
	return out
}
func fromTaskPlanItem(v model.TaskPlanItem) viewTaskPlanItem {
	var out viewTaskPlanItem
	out.ID = v.ID
	out.Title = v.Title
	out.Status = v.Status
	out.Stage = v.Stage
	return out
}
func fromTask(v model.Task) viewTask {
	var out viewTask
	out.ID = v.ID
	out.Title = v.Title
	out.Description = v.Description
	out.Stage = v.Stage
	out.CurrentStep = v.CurrentStep
	out.ExpectedAction = v.ExpectedAction
	out.Status = v.Status
	if v.Plan != nil {
		out.Plan = make([]viewTaskPlanItem, len(v.Plan))
		for i := range v.Plan {
			out.Plan[i] = fromTaskPlanItem(v.Plan[i])
		}
	}
	out.CurrentPlanItem = v.CurrentPlanItem
	out.ValidationResult = viewValidationResult{Status: v.ValidationResult.Status, Summary: v.ValidationResult.Summary, LegacyUnvalidated: v.ValidationResult.LegacyUnvalidated}
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	return out
}
func fromProject(v model.Project) viewProject {
	var out viewProject
	out.ID = v.ID
	out.Title = v.Title
	if v.Chats != nil {
		out.Chats = make([]viewChat, len(v.Chats))
		for i := range v.Chats {
			out.Chats[i] = fromChat(v.Chats[i])
		}
	}
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	return out
}
func fromMemory(v model.Memory) viewMemory {
	var out viewMemory
	if v.GlobalFacts != nil {
		out.GlobalFacts = make([]string, len(v.GlobalFacts))
		for i := range v.GlobalFacts {
			out.GlobalFacts[i] = v.GlobalFacts[i]
		}
	}
	if v.ProjectFacts != nil {
		out.ProjectFacts = make([]string, len(v.ProjectFacts))
		for i := range v.ProjectFacts {
			out.ProjectFacts[i] = v.ProjectFacts[i]
		}
	}
	out.Status = v.Status
	out.ErrorCategory = v.ErrorCategory
	return out
}
func fromListing(v model.Listing) viewListing {
	var out viewListing
	if v.Projects != nil {
		out.Projects = make([]viewProject, len(v.Projects))
		for i := range v.Projects {
			out.Projects[i] = fromProject(v.Projects[i])
		}
	}
	out.SelectedProjectID = v.SelectedProjectID
	out.SelectedChatID = v.SelectedChatID
	return out
}
func fromProfile(v model.Profile) viewProfile {
	var out viewProfile
	out.ID = v.ID
	out.Name = v.Name
	out.Style = v.Style
	out.Constraints = v.Constraints
	out.AdditionalContext = v.AdditionalContext
	out.BuiltIn = v.BuiltIn
	return out
}
func fromProfileListing(v model.ProfileListing) viewProfileListing {
	var out viewProfileListing
	if v.Profiles != nil {
		out.Profiles = make([]viewProfile, len(v.Profiles))
		for i := range v.Profiles {
			out.Profiles[i] = fromProfile(v.Profiles[i])
		}
	}
	out.ActiveProfileID = v.ActiveProfileID
	return out
}
