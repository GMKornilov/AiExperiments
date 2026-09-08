package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
)

// ErrStorage indicates that durable state could not be read or written.
var ErrStorage = errors.New("хранилище истории недоступно")

type diskState struct {
	Version  int                    `json:"version"`
	Sessions map[string]diskSession `json:"sessions"`
}

type diskSession struct {
	Selected string       `json:"selected_dialog_id"`
	Dialogs  []diskDialog `json:"dialogs"`
}

type diskDialog struct {
	Dialog   Dialog               `json:"dialog"`
	Snapshot agent.DialogSnapshot `json:"snapshot"`
}

type diskIndex struct {
	Version  int                         `json:"version"`
	Sessions map[string]diskIndexSession `json:"sessions"`
}

type diskIndexSession struct {
	Selected  string   `json:"selected_dialog_id"`
	DialogIDs []string `json:"dialog_ids"`
}

type diskChat struct {
	Version  int                  `json:"version"`
	Dialog   Dialog               `json:"dialog"`
	Snapshot agent.DialogSnapshot `json:"snapshot"`
}

func dialogDirectory(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path + ".dialogs"
	}
	return strings.TrimSuffix(path, ext)
}

func validDialogID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, char := range id {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func (s *Store) readState(data []byte) (diskState, error) {
	var header struct {
		Version int `json:"version"`
	}
	if json.Unmarshal(data, &header) != nil {
		return diskState{}, ErrStorage
	}
	if header.Version == 1 {
		var legacy diskState
		if json.Unmarshal(data, &legacy) != nil || legacy.Sessions == nil {
			return diskState{}, ErrStorage
		}
		return legacy, nil
	}
	var index diskIndex
	if header.Version != 2 || json.Unmarshal(data, &index) != nil || index.Sessions == nil {
		return diskState{}, ErrStorage
	}
	state := diskState{Version: 1, Sessions: make(map[string]diskSession)}
	for sid, session := range index.Sessions {
		if session.DialogIDs == nil {
			return diskState{}, ErrStorage
		}
		saved := diskSession{Selected: session.Selected, Dialogs: []diskDialog{}}
		for _, id := range session.DialogIDs {
			if !validDialogID(id) {
				return diskState{}, ErrStorage
			}
			body, err := os.ReadFile(filepath.Join(dialogDirectory(s.path), id+".json"))
			if err != nil {
				return diskState{}, ErrStorage
			}
			var chat diskChat
			if json.Unmarshal(body, &chat) != nil || chat.Version != 1 || chat.Dialog.ID != id {
				return diskState{}, ErrStorage
			}
			saved.Dialogs = append(saved.Dialogs, diskDialog{Dialog: chat.Dialog, Snapshot: chat.Snapshot})
			s.persistedDialogs[id] = body
		}
		state.Sessions[sid] = saved
	}
	s.persistedIndex = data
	return state, nil
}

// OpenStore loads durable conversations. The loader supplies credentials only;
// prompts and model parameters always come from the saved snapshot.
func OpenStore(provider agent.Provider, path string, loader func() (agent.DialogSnapshot, error)) (_ *Store, err error) {
	started := time.Now()
	defer func() { logStorage(context.Background(), "history_load", started, err) }()
	if strings.TrimSpace(path) == "" {
		return nil, ErrStorage
	}
	s := NewStore(provider)
	s.path = path
	s.persistedDialogs = make(map[string][]byte)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		entries, dirErr := os.ReadDir(dialogDirectory(path))
		if (dirErr != nil && !errors.Is(dirErr, os.ErrNotExist)) || len(entries) != 0 {
			return nil, fmt.Errorf("%w: missing index", ErrStorage)
		}
		if err := s.saveLocked(context.Background()); err != nil {
			return nil, err
		}
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read", ErrStorage)
	}
	state, err := s.readState(data)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid format", ErrStorage)
	}
	var credentials agent.DialogSnapshot
	loaded := false
	ids := make(map[string]bool)
	for sid, saved := range state.Sessions {
		if strings.TrimSpace(sid) == "" || saved.Dialogs == nil {
			return nil, ErrStorage
		}
		browser := s.getOrCreateLocked(sid)
		browser.selected = saved.Selected
		for _, entry := range saved.Dialogs {
			if !loaded {
				if loader == nil {
					return nil, ErrStorage
				}
				credentials, err = loader()
				if err != nil {
					return nil, fmt.Errorf("%w: credentials", ErrStorage)
				}
				loaded = true
			}
			if entry.Snapshot.Chat.BaseURL != credentials.Chat.BaseURL || entry.Snapshot.Text.BaseURL != credentials.Text.BaseURL {
				return nil, fmt.Errorf("%w: credential endpoint mismatch", ErrStorage)
			}
			entry.Snapshot.Chat.APIKey = credentials.Chat.APIKey
			entry.Snapshot.Text.APIKey = credentials.Text.APIKey
			if entry.Snapshot.Validate() != nil || !validDialogID(entry.Dialog.ID) || ids[entry.Dialog.ID] || entry.Dialog.CreatedAt.IsZero() || entry.Dialog.UpdatedAt.IsZero() {
				return nil, ErrStorage
			}
			ids[entry.Dialog.ID] = true
			conversation, err := agent.RestoreConversation(entry.Snapshot.Chat, entry.Dialog.Messages)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid conversation", ErrStorage)
			}
			switch entry.Dialog.TitleStatus {
			case "pending":
				entry.Dialog.TitleStatus = "error"
			case "idle", "error", "success":
			default:
				return nil, ErrStorage
			}
			browser.dialogs[entry.Dialog.ID] = &storedDialog{dialog: entry.Dialog, agent: conversation, textSnapshot: entry.Snapshot.Text}
		}
		if browser.selected != "" && browser.dialogs[browser.selected] == nil {
			return nil, ErrStorage
		}
	}
	// Commit recovery or migration only after validating every dialog.
	if err := s.saveLocked(context.Background()); err != nil {
		return nil, err
	}
	if err := s.removeOrphanFiles(); err != nil {
		return nil, ErrStorage
	}
	return s, nil
}

// StorageError reports a write failure; a failed store stays unavailable until restart.
func (s *Store) StorageError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.storageErr
}

// RegisterSecrets restores redaction for diagnostic logs of loaded dialogs.
func (s *Store) RegisterSecrets(register func(string, ...string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, browser := range s.sessions {
		for id, dialog := range browser.dialogs {
			register(id, dialog.agent.Snapshot().APIKey, dialog.textSnapshot.APIKey)
		}
	}
}

// saveLocked serializes mutations under the store mutex. No credentials are encoded.
func (s *Store) saveLocked(ctx context.Context) error {
	if s.storageErr != nil {
		return s.storageErr
	}
	if s.path == "" {
		return nil
	}
	started := time.Now()
	err := s.writeStateLocked()
	if err != nil {
		s.storageErr = ErrStorage
	}
	logStorage(ctx, "history_save", started, s.storageErr)
	return s.storageErr
}

func (s *Store) writeStateLocked() error {
	index := diskIndex{Version: 2, Sessions: make(map[string]diskIndexSession)}
	current := make(map[string]bool)
	for sid, browser := range s.sessions {
		saved := diskIndexSession{Selected: browser.selected, DialogIDs: []string{}}
		for id, dialog := range browser.dialogs {
			if !validDialogID(id) {
				return ErrStorage
			}
			current[id] = true
			saved.DialogIDs = append(saved.DialogIDs, id)
			chat := diskChat{Version: 1, Dialog: s.dialogCopyLocked(dialog), Snapshot: agent.DialogSnapshot{Chat: dialog.agent.Snapshot(), Text: dialog.textSnapshot}}
			data, err := json.MarshalIndent(chat, "", "  ")
			if err != nil {
				return err
			}
			if !bytes.Equal(data, s.persistedDialogs[id]) {
				if err := replaceFile(filepath.Join(dialogDirectory(s.path), id+".json"), data); err != nil {
					return err
				}
				s.persistedDialogs[id] = data
			}
		}
		sort.Strings(saved.DialogIDs)
		index.Sessions[sid] = saved
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	if !bytes.Equal(data, s.persistedIndex) {
		if err := replaceFile(s.path, data); err != nil {
			return err
		}
		s.persistedIndex = data
	}
	// Remove only after the index no longer references the dialog.
	for id := range s.persistedDialogs {
		if current[id] {
			continue
		}
		if err := os.Remove(filepath.Join(dialogDirectory(s.path), id+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		delete(s.persistedDialogs, id)
	}
	return nil
}

func (s *Store) removeOrphanFiles() error {
	entries, err := os.ReadDir(dialogDirectory(s.path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if entry.IsDir() || entry.Name() != id+".json" || !validDialogID(id) {
			continue
		}
		if _, referenced := s.persistedDialogs[id]; referenced {
			continue
		}
		if err := os.Remove(filepath.Join(dialogDirectory(s.path), entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func replaceFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".history-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func logStorage(ctx context.Context, event string, started time.Time, err error) {
	result, category := "success", ""
	if err != nil {
		result, category = "failure", "storage"
	}
	ctx = llm.WithRequestID(ctx, llm.RequestID(ctx))
	slog.Info("barista.event", "source", "backend", "event", event, "result", result, "error_category", category,
		"correlation_id", llm.RequestID(ctx), "duration_ms", time.Since(started).Milliseconds())
}
