// Package session provides an in-memory, browser-session-scoped dialog store.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
)

// Dialog is the safe public representation of a dialog.
type Dialog struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	Messages  []agent.Message `json:"messages"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Listing is a browser session's dialog list and selected dialog ID.
type Listing struct {
	Dialogs          []Dialog `json:"dialogs"`
	SelectedDialogID string   `json:"selected_dialog_id"`
}

type storedDialog struct {
	dialog Dialog
	agent  *agent.Conversation
}

type browserSession struct {
	dialogs         map[string]*storedDialog
	selected        string
	pending         bool
	cancel          context.CancelFunc
	pendingDialogID string
}

// Store keeps all sessions solely in process memory.
type Store struct {
	mu       sync.Mutex
	sessions map[string]*browserSession
	provider agent.Provider
	now      func() time.Time
	observer AttemptObserver
}

// AttemptObserver observes actual provider calls after acceptance.
type AttemptObserver struct {
	Started  func(context.Context, string, string)
	Finished func(context.Context, string, string, time.Duration, Dialog)
}

// NewStore creates an empty store.
func NewStore(provider agent.Provider) *Store {
	return &Store{sessions: make(map[string]*browserSession), provider: provider, now: time.Now}
}

func (s *Store) SetAttemptObserver(observer AttemptObserver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observer = observer
}

// Close cancels every outstanding provider request during service shutdown.
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, browser := range s.sessions {
		if browser.cancel != nil {
			browser.cancel()
			browser.cancel = nil
			browser.pending = false
		}
	}
}

// Create adds and selects a new empty dialog in one browser session.
func (s *Store) Create(sessionID string, snapshot agent.Snapshot) (Dialog, error) {
	if strings.TrimSpace(sessionID) == "" {
		return Dialog{}, fmt.Errorf("session ID не должен быть пустым")
	}
	conversation, err := agent.NewConversation(snapshot)
	if err != nil {
		return Dialog{}, err
	}
	id, err := randomID()
	if err != nil {
		return Dialog{}, err
	}
	now := s.now()
	dialog := Dialog{ID: id, Title: "Новый диалог", CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.getOrCreateLocked(sessionID)
	session.dialogs[id] = &storedDialog{dialog: dialog, agent: conversation}
	session.selected = id
	return cloneDialog(dialog), nil
}

// List returns only dialogs belonging to sessionID, ordered by last activity.
func (s *Store) List(sessionID string) Listing {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[sessionID]
	if session == nil {
		return Listing{Dialogs: []Dialog{}}
	}
	listing := Listing{SelectedDialogID: session.selected}
	for _, stored := range session.dialogs {
		dialog := cloneDialog(stored.dialog)
		dialog.Messages = stored.agent.Messages()
		listing.Dialogs = append(listing.Dialogs, dialog)
	}
	sort.Slice(listing.Dialogs, func(i, j int) bool { return listing.Dialogs[i].UpdatedAt.After(listing.Dialogs[j].UpdatedAt) })
	return listing
}

// Get reads one dialog only if it belongs to sessionID.
func (s *Store) Get(sessionID, dialogID string) (Dialog, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.dialogLocked(sessionID, dialogID)
	if stored == nil {
		return Dialog{}, false
	}
	dialog := cloneDialog(stored.dialog)
	dialog.Messages = stored.agent.Messages()
	return dialog, true
}

// Exists reports whether a dialog remains in memory, regardless of browser session.
func (s *Store) Exists(dialogID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, browser := range s.sessions {
		if browser.dialogs[dialogID] != nil {
			return true
		}
	}
	return false
}

// Select changes the browser session's selected dialog.
func (s *Store) Select(sessionID, dialogID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[sessionID]
	if session == nil || session.dialogs[dialogID] == nil {
		return false
	}
	session.selected = dialogID
	return true
}

// Delete cancels a pending attempt, if any, and irreversibly removes its dialog.
func (s *Store) Delete(sessionID, dialogID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[sessionID]
	if session == nil || session.dialogs[dialogID] == nil {
		return false
	}
	delete(session.dialogs, dialogID)
	if session.selected == dialogID {
		session.selected = ""
	}
	if session.pending && session.pendingDialogID == dialogID && session.cancel != nil {
		session.cancel()
		session.pending = false
		session.cancel = nil
		session.pendingDialogID = ""
	}
	return true
}

// Send accepts a deduplicated client message and completes one agent attempt.
func (s *Store) Send(ctx context.Context, sessionID, dialogID, clientID, text string) (Dialog, error) {
	s.mu.Lock()
	if strings.TrimSpace(clientID) == "" {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("client message ID не должен быть пустым")
	}
	stored := s.dialogLocked(sessionID, dialogID)
	session := s.sessions[sessionID]
	if stored == nil || session == nil {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("диалог не найден")
	}
	if _, found := stored.agent.FindClientMessage(clientID); found {
		dialog := s.dialogCopyLocked(stored)
		s.mu.Unlock()
		return dialog, nil
	}
	if session.pending {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("в этом сеансе уже выполняется запрос")
	}
	message, err := stored.agent.Begin(clientID, text)
	if err != nil {
		s.mu.Unlock()
		return Dialog{}, err
	}
	if len(stored.agent.Messages()) == 1 {
		stored.dialog.Title = titleFrom(text)
	}
	return s.runAttempt(ctx, sessionID, dialogID, stored, session, message.ID)
}

// Retry reruns an errored user message without adding another user message.
func (s *Store) Retry(ctx context.Context, sessionID, dialogID, messageID string) (Dialog, error) {
	s.mu.Lock()
	stored := s.dialogLocked(sessionID, dialogID)
	session := s.sessions[sessionID]
	if stored == nil || session == nil {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("диалог не найден")
	}
	if session.pending {
		dialog := s.dialogCopyLocked(stored)
		s.mu.Unlock()
		return dialog, nil
	}
	message, err := stored.agent.Retry(messageID)
	if err != nil {
		s.mu.Unlock()
		return Dialog{}, err
	}
	return s.runAttempt(ctx, sessionID, dialogID, stored, session, message.ID)
}

func (s *Store) runAttempt(ctx context.Context, sessionID, dialogID string, stored *storedDialog, session *browserSession, messageID string) (Dialog, error) {
	// Keep correlation values, but detach the accepted work from request cancellation.
	attemptContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), stored.agent.Snapshot().Timeout)
	session.pending = true
	session.cancel = cancel
	session.pendingDialogID = dialogID
	observer := s.observer
	s.mu.Unlock()
	startedAt := time.Now()
	if observer.Started != nil {
		observer.Started(attemptContext, dialogID, messageID)
	}

	attemptErr := stored.agent.Attempt(attemptContext, s.provider, messageID)
	cancel()

	s.mu.Lock()
	currentSession := s.sessions[sessionID]
	if currentSession == nil || currentSession.dialogs[dialogID] != stored {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("диалог удалён")
	}
	currentSession.pending = false
	currentSession.cancel = nil
	currentSession.pendingDialogID = ""
	_ = attemptErr
	stored.dialog.UpdatedAt = s.now()
	stored.dialog.Messages = stored.agent.Messages()
	dialog := s.dialogCopyLocked(stored)
	observer = s.observer
	s.mu.Unlock()
	if observer.Finished != nil {
		observer.Finished(attemptContext, dialogID, messageID, time.Since(startedAt), dialog)
	}
	return dialog, nil
}

func (s *Store) dialogLocked(sessionID, dialogID string) *storedDialog {
	session := s.sessions[sessionID]
	if session == nil {
		return nil
	}
	return session.dialogs[dialogID]
}

func (s *Store) dialogCopyLocked(stored *storedDialog) Dialog {
	dialog := cloneDialog(stored.dialog)
	dialog.Messages = stored.agent.Messages()
	return dialog
}

func (s *Store) getOrCreateLocked(sessionID string) *browserSession {
	if session := s.sessions[sessionID]; session != nil {
		return session
	}
	session := &browserSession{dialogs: make(map[string]*storedDialog)}
	s.sessions[sessionID] = session
	return session
}

func cloneDialog(dialog Dialog) Dialog {
	dialog.Messages = append([]agent.Message(nil), dialog.Messages...)
	return dialog
}

func titleFrom(text string) string {
	runes := []rune(text)
	if len(runes) > 60 {
		runes = runes[:60]
	}
	return string(runes)
}

func randomID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("создание ID диалога: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
