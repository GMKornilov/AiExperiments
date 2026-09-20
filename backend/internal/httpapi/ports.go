package httpapi

import (
	"context"

	"aichallenge/week_1/task_1/internal/domain/model"
)

type Projects interface {
	CreateProject(string, string) (model.Project, error)
	RenameProject(string, string, string) (model.Project, error)
	List(string) model.Listing
	GetProject(string, string) (model.Project, bool)
	SelectProject(string, string) error
	DeleteProject(string, string) error
}
type Chats interface {
	CreateChat(string, string, string) (model.Chat, error)
	GetChat(string, string, string) (model.Chat, bool)
	HasChat(string) bool
	SelectChat(string, string, string) error
	DeleteChat(string, string, string) error
}
type Memory interface {
	ReadMemory(string, string) (model.Memory, bool)
	ClearGlobal(string) error
	ClearProject(string, string) error
}
type Profiles interface {
	ListProfiles(string) model.ProfileListing
	CreateProfile(string, string, string, string, string) (model.ProfileListing, error)
	SelectProfile(string, string) (model.ProfileListing, error)
	DeleteProfile(string, string) (model.ProfileListing, error)
}
type Conversation interface {
	Send(context.Context, string, string, string, string, string) (model.Chat, error)
	Retry(context.Context, string, string, string, string) (model.Chat, error)
}
type Tasks interface {
	TaskInput(context.Context, string, string, string, string, string, string) (model.Chat, []model.Task, error)
	PauseTask(string, string, string, string) (model.Chat, error)
	Resume(context.Context, string, string, string, string, string, string) (model.Chat, []model.Task, error)
}
type Health interface{ StorageError() error }
type UseCases struct {
	Projects      Projects
	Chats         Chats
	Profiles      Profiles
	Memory        Memory
	Conversations Conversation
	Tasks         Tasks
	Health        Health
}

func responseDTO(v any) any {
	switch x := v.(type) {
	case model.Listing:
		return fromListing(x)
	case model.Project:
		return fromProject(x)
	case model.Chat:
		return fromChat(x)
	case model.ProfileListing:
		return fromProfileListing(x)
	case model.Memory:
		return fromMemory(x)
	case []model.Task:
		if x == nil {
			return nil
		}
		out := make([]viewTask, len(x))
		for i, t := range x {
			out[i] = fromTask(t)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = responseDTO(v)
		}
		return out
	default:
		return v
	}
}
