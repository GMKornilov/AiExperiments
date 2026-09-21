package model

import "time"

type Message struct {
	ID            string
	ClientID      string
	Role          string
	Text          string
	CreatedAt     time.Time
	Status        string
	ErrorCategory string
}
type Chat struct {
	ID                  string
	ProjectID           string
	Title               string
	TitleStatus         string
	Messages            []Message
	CreatedAt           time.Time
	UpdatedAt           time.Time
	MemoryStatus        string
	MemoryErrorCategory string
	Tasks               []Task
}

// TaskStage is a deliberately closed set of task workflow stages.
type TaskStage string

const (
	TaskStageClarifyInput      TaskStage = "clarify_input"
	TaskStageResearchInputData TaskStage = "research_input_data"
	TaskStageExecution         TaskStage = "execution"
	TaskStageUserFeedback      TaskStage = "user_feedback"
)

// TaskStatus is a lifecycle state, distinct from TaskStage.
type TaskStatus string

const (
	TaskStatusActive TaskStatus = "active"
	TaskStatusPaused TaskStatus = "paused"
	TaskStatusDone   TaskStatus = "done"

	ClarificationStep           = "Сформулировать понимание задачи и запросить подтверждение или недостающие данные"
	ClarificationExpectedAction = "user: подтвердить цель или ответить на вопросы доуточнения"
)

// TaskPlanItemStatus is the deliberately closed lifecycle of one subject-level
// item in a task plan. It is independent from TaskStage: a single stage may
// require several plan items and one item may require several task steps.
type TaskPlanItemStatus string

const (
	TaskPlanItemPending   TaskPlanItemStatus = "pending"
	TaskPlanItemCurrent   TaskPlanItemStatus = "current"
	TaskPlanItemCompleted TaskPlanItemStatus = "completed"
)

// TaskPlanItem is a durable, user-visible unit of work. Stage is explanatory
// only; an empty value intentionally does not affect the state machine.
type TaskPlanItem struct {
	ID     string
	Title  string
	Status TaskPlanItemStatus
	Stage  TaskStage
}

// ValidationResult is server-owned evidence that the completed result passed
// the mandatory validation gate. It is never supplied by a task proposal.
type ValidationResultStatus string

const (
	ValidationNotValidated ValidationResultStatus = "not_validated"
	ValidationPassed       ValidationResultStatus = "passed"
)

type ValidationResult struct {
	Status            ValidationResultStatus
	Summary           string
	LegacyUnvalidated bool
}

func (s ValidationResultStatus) Valid() bool {
	return s == ValidationNotValidated || s == ValidationPassed
}

// Task is a browser-session and chat-scoped durable unit of work.
type Task struct {
	ID               string
	Title            string
	Description      string
	Stage            TaskStage
	CurrentStep      string
	ExpectedAction   string
	Status           TaskStatus
	Plan             []TaskPlanItem
	CurrentPlanItem  string
	ValidationResult ValidationResult
	// EquipmentConfirmed is durable server-side evidence for the current
	// clarification round. It intentionally has no public API representation.
	EquipmentConfirmed bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (s TaskStage) Valid() bool {
	return s == TaskStageClarifyInput || s == TaskStageResearchInputData || s == TaskStageExecution || s == TaskStageUserFeedback
}

func (s TaskStatus) Valid() bool {
	return s == TaskStatusActive || s == TaskStatusPaused || s == TaskStatusDone
}

func (s TaskPlanItemStatus) Valid() bool {
	return s == TaskPlanItemPending || s == TaskPlanItemCurrent || s == TaskPlanItemCompleted
}

type Project struct {
	ID        string
	Title     string
	Chats     []Chat
	CreatedAt time.Time
	UpdatedAt time.Time
}
type Memory struct {
	GlobalFacts   []string
	ProjectFacts  []string
	Status        string
	ErrorCategory string
}
type Listing struct {
	Projects          []Project
	SelectedProjectID string
	SelectedChatID    string
}

// Profile is a browser-session-scoped set of response rules. Its free-text
// fields are deliberately not treated as memory facts.
type Profile struct {
	ID                string
	Name              string
	Style             string
	Constraints       string
	AdditionalContext string
	BuiltIn           bool
}

type ProfileListing struct {
	Profiles        []Profile
	ActiveProfileID string
}

const (
	BaristaProfileID   = "barista"
	EquipmentProfileID = "coffee-equipment"
)

var builtInProfiles = []Profile{
	{ID: BaristaProfileID, Name: "Бариста", Style: "Дружелюбный, практичный и пошаговый.", Constraints: "Не выдумывай оборудование и ингредиенты; уточняй недостающие параметры рецепта.", AdditionalContext: "Ассистент отвечает как специалист по приготовлению кофе и помогает с выбором напитка, рецептом и техникой заваривания.", BuiltIn: true},
	{ID: EquipmentProfileID, Name: "Специалист по кофейному оборудованию", Style: "Технический, структурированный и диагностический.", Constraints: "Не обещай исправить неисправность без данных; предупреждай о рисках при работе с электрическим оборудованием.", AdditionalContext: "Ассистент отвечает как специалист по кофейному оборудованию и фокусируется на подборе, настройке, уходе и диагностике оборудования.", BuiltIn: true},
}

type Browser struct {
	Pending           *Operation
	GlobalFacts       []string
	Projects          map[string]*ProjectState
	SelectedProjectID string
	SelectedChatID    string
	Profiles          map[string]*CustomProfile
	ActiveProfileID   string
}
type CustomProfile struct {
	ID                string
	Name              string
	Style             string
	Constraints       string
	AdditionalContext string
}
type ProjectState struct {
	ID            string
	Title         string
	Facts         []string
	Chats         map[string]*ChatState
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Status        string
	ErrorCategory string
}
type ChatState struct {
	TaskOperationIDs map[string]string
	Operations       map[string]*Operation
	ID               string
	Title            string
	TitleStatus      string
	Messages         []Message
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Status           string
	ErrorCategory    string
	Tasks            map[string]*Task
	TaskInputs       map[string]string
}
type State struct{ Sessions map[string]*Browser }
type Operation struct {
	ID         string
	Generation string
	ProjectID  string
	ChatID     string
	TaskID     string
	Input      string
	Status     string
}
