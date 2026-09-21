// Explicit boundary DTOs preserve the wire schema independently of domain models.
package statejson

import (
	"time"

	"aichallenge/week_1/task_1/internal/domain/model"
)

type diskMessage struct {
	ID            string    `json:"id"`
	ClientID      string    `json:"client_message_id,omitempty"`
	Role          string    `json:"role"`
	Text          string    `json:"text"`
	CreatedAt     time.Time `json:"created_at"`
	Status        string    `json:"status"`
	ErrorCategory string    `json:"error_category,omitempty"`
}
type diskTaskPlanItem struct {
	ID     string                   `json:"id"`
	Title  string                   `json:"title"`
	Status model.TaskPlanItemStatus `json:"status"`
	Stage  model.TaskStage          `json:"stage,omitempty"`
}
type diskTask struct {
	ID                 string               `json:"id"`
	Title              string               `json:"title"`
	Description        string               `json:"description"`
	Stage              model.TaskStage      `json:"stage"`
	CurrentStep        string               `json:"current_step"`
	ExpectedAction     string               `json:"expected_action"`
	Status             model.TaskStatus     `json:"status"`
	Plan               []diskTaskPlanItem   `json:"plan"`
	CurrentPlanItem    string               `json:"current_plan_item,omitempty"`
	ValidationResult   diskValidationResult `json:"validation_result,omitempty"`
	EquipmentConfirmed bool                 `json:"equipment_confirmed,omitempty"`
	CreatedAt          time.Time            `json:"created_at"`
	UpdatedAt          time.Time            `json:"updated_at"`
}
type diskValidationResult struct {
	Status            model.ValidationResultStatus `json:"status"`
	Summary           string                       `json:"summary,omitempty"`
	LegacyUnvalidated bool                         `json:"legacy_unvalidated,omitempty"`
}
type diskBrowser struct {
	Pending           *diskOperation                `json:"pending,omitempty"`
	GlobalFacts       []string                      `json:"global_facts"`
	Projects          map[string]*diskProjectState  `json:"projects"`
	SelectedProjectID string                        `json:"selected_project_id"`
	SelectedChatID    string                        `json:"selected_chat_id"`
	Profiles          map[string]*diskCustomProfile `json:"profiles"`
	ActiveProfileID   string                        `json:"active_profile_id"`
}
type diskCustomProfile struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Style             string `json:"style"`
	Constraints       string `json:"constraints"`
	AdditionalContext string `json:"additional_context"`
}
type diskProjectState struct {
	ID            string                    `json:"id"`
	Title         string                    `json:"title"`
	Facts         []string                  `json:"project_facts"`
	Chats         map[string]*diskChatState `json:"chats"`
	CreatedAt     time.Time                 `json:"created_at"`
	UpdatedAt     time.Time                 `json:"updated_at"`
	Status        string                    `json:"memory_status"`
	ErrorCategory string                    `json:"memory_error_category,omitempty"`
}
type diskChatState struct {
	TaskOperationIDs map[string]string         `json:"task_operation_ids,omitempty"`
	Operations       map[string]*diskOperation `json:"operations,omitempty"`
	ID               string                    `json:"id"`
	Title            string                    `json:"title"`
	TitleStatus      string                    `json:"title_status"`
	Messages         []diskMessage             `json:"messages"`
	CreatedAt        time.Time                 `json:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at"`
	Status           string                    `json:"memory_status"`
	ErrorCategory    string                    `json:"memory_error_category,omitempty"`
	Tasks            map[string]*diskTask      `json:"tasks"`
	TaskInputs       map[string]string         `json:"task_inputs,omitempty"`
}
type diskState struct {
	Sessions map[string]*diskBrowser `json:"sessions"`
}
type diskOperation struct {
	ID         string `json:"id"`
	Generation string `json:"generation"`
	ProjectID  string `json:"project_id"`
	ChatID     string `json:"chat_id"`
	TaskID     string `json:"task_id,omitempty"`
	Input      string `json:"input"`
	Status     string `json:"status"`
}

func fromMessage(v model.Message) diskMessage {
	var out diskMessage
	out.ID = v.ID
	out.ClientID = v.ClientID
	out.Role = v.Role
	out.Text = v.Text
	out.CreatedAt = v.CreatedAt
	out.Status = v.Status
	out.ErrorCategory = v.ErrorCategory
	return out
}
func toMessage(v diskMessage) model.Message {
	var out model.Message
	out.ID = v.ID
	out.ClientID = v.ClientID
	out.Role = v.Role
	out.Text = v.Text
	out.CreatedAt = v.CreatedAt
	out.Status = v.Status
	out.ErrorCategory = v.ErrorCategory
	return out
}
func fromTaskPlanItem(v model.TaskPlanItem) diskTaskPlanItem {
	var out diskTaskPlanItem
	out.ID = v.ID
	out.Title = v.Title
	out.Status = v.Status
	out.Stage = v.Stage
	return out
}
func toTaskPlanItem(v diskTaskPlanItem) model.TaskPlanItem {
	var out model.TaskPlanItem
	out.ID = v.ID
	out.Title = v.Title
	out.Status = v.Status
	out.Stage = v.Stage
	return out
}
func fromTask(v model.Task) diskTask {
	var out diskTask
	out.ID = v.ID
	out.Title = v.Title
	out.Description = v.Description
	out.Stage = v.Stage
	out.CurrentStep = v.CurrentStep
	out.ExpectedAction = v.ExpectedAction
	out.Status = v.Status
	if v.Plan != nil {
		out.Plan = make([]diskTaskPlanItem, len(v.Plan))
		for i := range v.Plan {
			out.Plan[i] = fromTaskPlanItem(v.Plan[i])
		}
	}
	out.CurrentPlanItem = v.CurrentPlanItem
	out.ValidationResult = diskValidationResult{Status: v.ValidationResult.Status, Summary: v.ValidationResult.Summary, LegacyUnvalidated: v.ValidationResult.LegacyUnvalidated}
	out.EquipmentConfirmed = v.EquipmentConfirmed
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	return out
}
func toTask(v diskTask) model.Task {
	var out model.Task
	out.ID = v.ID
	out.Title = v.Title
	out.Description = v.Description
	out.Stage = v.Stage
	out.CurrentStep = v.CurrentStep
	out.ExpectedAction = v.ExpectedAction
	out.Status = v.Status
	if v.Plan != nil {
		out.Plan = make([]model.TaskPlanItem, len(v.Plan))
		for i := range v.Plan {
			out.Plan[i] = toTaskPlanItem(v.Plan[i])
		}
	}
	out.CurrentPlanItem = v.CurrentPlanItem
	out.ValidationResult = model.ValidationResult{Status: v.ValidationResult.Status, Summary: v.ValidationResult.Summary, LegacyUnvalidated: v.ValidationResult.LegacyUnvalidated}
	out.EquipmentConfirmed = v.EquipmentConfirmed
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	return out
}
func fromBrowser(v model.Browser) diskBrowser {
	var out diskBrowser
	if v.Pending != nil {
		v := fromOperation(*v.Pending)
		out.Pending = &v
	}
	if v.GlobalFacts != nil {
		out.GlobalFacts = make([]string, len(v.GlobalFacts))
		for i := range v.GlobalFacts {
			out.GlobalFacts[i] = v.GlobalFacts[i]
		}
	}
	if v.Projects != nil {
		out.Projects = make(map[string]*diskProjectState, len(v.Projects))
		for k := range v.Projects {
			if v.Projects[k] != nil {
				v := fromProjectState(*v.Projects[k])
				out.Projects[k] = &v
			}
		}
	}
	out.SelectedProjectID = v.SelectedProjectID
	out.SelectedChatID = v.SelectedChatID
	if v.Profiles != nil {
		out.Profiles = make(map[string]*diskCustomProfile, len(v.Profiles))
		for k := range v.Profiles {
			if v.Profiles[k] != nil {
				v := fromCustomProfile(*v.Profiles[k])
				out.Profiles[k] = &v
			}
		}
	}
	out.ActiveProfileID = v.ActiveProfileID
	return out
}
func toBrowser(v diskBrowser) model.Browser {
	var out model.Browser
	if v.Pending != nil {
		v := toOperation(*v.Pending)
		out.Pending = &v
	}
	if v.GlobalFacts != nil {
		out.GlobalFacts = make([]string, len(v.GlobalFacts))
		for i := range v.GlobalFacts {
			out.GlobalFacts[i] = v.GlobalFacts[i]
		}
	}
	if v.Projects != nil {
		out.Projects = make(map[string]*model.ProjectState, len(v.Projects))
		for k := range v.Projects {
			if v.Projects[k] != nil {
				v := toProjectState(*v.Projects[k])
				out.Projects[k] = &v
			}
		}
	}
	out.SelectedProjectID = v.SelectedProjectID
	out.SelectedChatID = v.SelectedChatID
	if v.Profiles != nil {
		out.Profiles = make(map[string]*model.CustomProfile, len(v.Profiles))
		for k := range v.Profiles {
			if v.Profiles[k] != nil {
				v := toCustomProfile(*v.Profiles[k])
				out.Profiles[k] = &v
			}
		}
	}
	out.ActiveProfileID = v.ActiveProfileID
	return out
}
func fromCustomProfile(v model.CustomProfile) diskCustomProfile {
	var out diskCustomProfile
	out.ID = v.ID
	out.Name = v.Name
	out.Style = v.Style
	out.Constraints = v.Constraints
	out.AdditionalContext = v.AdditionalContext
	return out
}
func toCustomProfile(v diskCustomProfile) model.CustomProfile {
	var out model.CustomProfile
	out.ID = v.ID
	out.Name = v.Name
	out.Style = v.Style
	out.Constraints = v.Constraints
	out.AdditionalContext = v.AdditionalContext
	return out
}
func fromProjectState(v model.ProjectState) diskProjectState {
	var out diskProjectState
	out.ID = v.ID
	out.Title = v.Title
	if v.Facts != nil {
		out.Facts = make([]string, len(v.Facts))
		for i := range v.Facts {
			out.Facts[i] = v.Facts[i]
		}
	}
	if v.Chats != nil {
		out.Chats = make(map[string]*diskChatState, len(v.Chats))
		for k := range v.Chats {
			if v.Chats[k] != nil {
				v := fromChatState(*v.Chats[k])
				out.Chats[k] = &v
			}
		}
	}
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	out.Status = v.Status
	out.ErrorCategory = v.ErrorCategory
	return out
}
func toProjectState(v diskProjectState) model.ProjectState {
	var out model.ProjectState
	out.ID = v.ID
	out.Title = v.Title
	if v.Facts != nil {
		out.Facts = make([]string, len(v.Facts))
		for i := range v.Facts {
			out.Facts[i] = v.Facts[i]
		}
	}
	if v.Chats != nil {
		out.Chats = make(map[string]*model.ChatState, len(v.Chats))
		for k := range v.Chats {
			if v.Chats[k] != nil {
				v := toChatState(*v.Chats[k])
				out.Chats[k] = &v
			}
		}
	}
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	out.Status = v.Status
	out.ErrorCategory = v.ErrorCategory
	return out
}
func fromChatState(v model.ChatState) diskChatState {
	var out diskChatState
	if v.TaskOperationIDs != nil {
		out.TaskOperationIDs = make(map[string]string, len(v.TaskOperationIDs))
		for k := range v.TaskOperationIDs {
			out.TaskOperationIDs[k] = v.TaskOperationIDs[k]
		}
	}
	if v.Operations != nil {
		out.Operations = make(map[string]*diskOperation, len(v.Operations))
		for k := range v.Operations {
			if v.Operations[k] != nil {
				v := fromOperation(*v.Operations[k])
				out.Operations[k] = &v
			}
		}
	}
	out.ID = v.ID
	out.Title = v.Title
	out.TitleStatus = v.TitleStatus
	if v.Messages != nil {
		out.Messages = make([]diskMessage, len(v.Messages))
		for i := range v.Messages {
			out.Messages[i] = fromMessage(v.Messages[i])
		}
	}
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	out.Status = v.Status
	out.ErrorCategory = v.ErrorCategory
	if v.Tasks != nil {
		out.Tasks = make(map[string]*diskTask, len(v.Tasks))
		for k := range v.Tasks {
			if v.Tasks[k] != nil {
				v := fromTask(*v.Tasks[k])
				out.Tasks[k] = &v
			}
		}
	}
	if v.TaskInputs != nil {
		out.TaskInputs = make(map[string]string, len(v.TaskInputs))
		for k := range v.TaskInputs {
			out.TaskInputs[k] = v.TaskInputs[k]
		}
	}
	return out
}
func toChatState(v diskChatState) model.ChatState {
	var out model.ChatState
	if v.TaskOperationIDs != nil {
		out.TaskOperationIDs = make(map[string]string, len(v.TaskOperationIDs))
		for k := range v.TaskOperationIDs {
			out.TaskOperationIDs[k] = v.TaskOperationIDs[k]
		}
	}
	if v.Operations != nil {
		out.Operations = make(map[string]*model.Operation, len(v.Operations))
		for k := range v.Operations {
			if v.Operations[k] != nil {
				v := toOperation(*v.Operations[k])
				out.Operations[k] = &v
			}
		}
	}
	out.ID = v.ID
	out.Title = v.Title
	out.TitleStatus = v.TitleStatus
	if v.Messages != nil {
		out.Messages = make([]model.Message, len(v.Messages))
		for i := range v.Messages {
			out.Messages[i] = toMessage(v.Messages[i])
		}
	}
	out.CreatedAt = v.CreatedAt
	out.UpdatedAt = v.UpdatedAt
	out.Status = v.Status
	out.ErrorCategory = v.ErrorCategory
	if v.Tasks != nil {
		out.Tasks = make(map[string]*model.Task, len(v.Tasks))
		for k := range v.Tasks {
			if v.Tasks[k] != nil {
				v := toTask(*v.Tasks[k])
				out.Tasks[k] = &v
			}
		}
	}
	if v.TaskInputs != nil {
		out.TaskInputs = make(map[string]string, len(v.TaskInputs))
		for k := range v.TaskInputs {
			out.TaskInputs[k] = v.TaskInputs[k]
		}
	}
	return out
}
func fromState(v model.State) diskState {
	var out diskState
	if v.Sessions != nil {
		out.Sessions = make(map[string]*diskBrowser, len(v.Sessions))
		for k := range v.Sessions {
			if v.Sessions[k] != nil {
				v := fromBrowser(*v.Sessions[k])
				out.Sessions[k] = &v
			}
		}
	}
	return out
}
func toState(v diskState) model.State {
	var out model.State
	if v.Sessions != nil {
		out.Sessions = make(map[string]*model.Browser, len(v.Sessions))
		for k := range v.Sessions {
			if v.Sessions[k] != nil {
				v := toBrowser(*v.Sessions[k])
				out.Sessions[k] = &v
			}
		}
	}
	return out
}
func fromOperation(v model.Operation) diskOperation {
	var out diskOperation
	out.ID = v.ID
	out.Generation = v.Generation
	out.ProjectID = v.ProjectID
	out.ChatID = v.ChatID
	out.TaskID = v.TaskID
	out.Input = v.Input
	out.Status = v.Status
	return out
}
func toOperation(v diskOperation) model.Operation {
	var out model.Operation
	out.ID = v.ID
	out.Generation = v.Generation
	out.ProjectID = v.ProjectID
	out.ChatID = v.ChatID
	out.TaskID = v.TaskID
	out.Input = v.Input
	out.Status = v.Status
	return out
}
