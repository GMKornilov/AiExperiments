// Package session provides a browser-session-scoped dialog store with JSON persistence.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
)

// Dialog is the safe public representation of a dialog.
type Dialog struct {
	AccountedTokens int64           `json:"accounted_tokens"`
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	TitleStatus     string          `json:"title_status"`
	Messages        []agent.Message `json:"messages"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// Listing is a browser session's dialog list and selected dialog ID.
type Listing struct {
	Dialogs          []Dialog `json:"dialogs"`
	SelectedDialogID string   `json:"selected_dialog_id"`
}

type storedDialog struct {
	dialog       Dialog
	agent        *agent.Conversation
	textSnapshot agent.Snapshot
	titleCancel  context.CancelFunc
}

type browserSession struct {
	dialogs         map[string]*storedDialog
	selected        string
	pending         bool
	cancel          context.CancelFunc
	pendingDialogID string
}

// Store serializes session mutations and optionally persists them to JSON.
type Store struct {
	mu               sync.Mutex
	sessions         map[string]*browserSession
	provider         agent.Provider
	now              func() time.Time
	observer         AttemptObserver
	titleWG          sync.WaitGroup
	attemptWG        sync.WaitGroup
	closed           bool
	path             string
	storageErr       error
	persistedDialogs map[string][]byte
	persistedIndex   []byte
}

// AttemptObserver observes actual provider calls after acceptance.
type AttemptObserver struct {
	Started        func(context.Context, string, string)
	Finished       func(context.Context, string, string, time.Duration, Dialog)
	TitleStarted   func(context.Context, string, string)
	TitleFinished  func(context.Context, string, string, time.Duration, Dialog, string)
	TitleCancelled func(context.Context, string, string)
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
	s.closed = true
	for _, browser := range s.sessions {
		if browser.cancel != nil {
			browser.cancel()
			browser.cancel = nil
			browser.pending = false
		}
		for _, dialog := range browser.dialogs {
			if dialog.titleCancel != nil {
				dialog.titleCancel()
				dialog.titleCancel = nil
			}
		}
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.titleWG.Wait(); s.attemptWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

// Create adds and selects a new empty dialog in one browser session.
func (s *Store) Create(sessionID string, snapshot agent.DialogSnapshot) (Dialog, error) {
	if strings.TrimSpace(sessionID) == "" {
		return Dialog{}, fmt.Errorf("session ID не должен быть пустым")
	}
	if err := snapshot.Validate(); err != nil {
		return Dialog{}, err
	}
	conversation, err := agent.NewConversation(snapshot.Chat)
	if err != nil {
		return Dialog{}, err
	}
	id, err := randomID()
	if err != nil {
		return Dialog{}, err
	}
	now := s.now()
	dialog := Dialog{ID: id, Title: "Новый диалог", TitleStatus: "idle", CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Dialog{}, fmt.Errorf("хранилище закрыто")
	}
	if s.storageErr != nil {
		return Dialog{}, s.storageErr
	}
	session := s.getOrCreateLocked(sessionID)
	session.dialogs[id] = &storedDialog{dialog: dialog, agent: conversation, textSnapshot: snapshot.Text}
	session.selected = id
	if err := s.saveLocked(context.Background()); err != nil {
		return Dialog{}, err
	}
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
		dialog.AccountedTokens = agent.AccountedTokens(dialog.Messages)
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
	dialog.AccountedTokens = agent.AccountedTokens(dialog.Messages)
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
func (s *Store) Select(sessionID, dialogID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storageErr != nil {
		return false, s.storageErr
	}
	session := s.sessions[sessionID]
	if session == nil || session.dialogs[dialogID] == nil {
		return false, nil
	}
	session.selected = dialogID
	return true, s.saveLocked(context.Background())
}

// Delete cancels a pending attempt, if any, and irreversibly removes its dialog.
func (s *Store) Delete(sessionID, dialogID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storageErr != nil {
		return false, s.storageErr
	}
	session := s.sessions[sessionID]
	if session == nil || session.dialogs[dialogID] == nil {
		return false, nil
	}
	stored := session.dialogs[dialogID]
	if stored.titleCancel != nil {
		stored.titleCancel()
		stored.titleCancel = nil
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
	return true, s.saveLocked(context.Background())
}

// Send accepts a deduplicated client message and completes one agent attempt.
func (s *Store) Send(ctx context.Context, sessionID, dialogID, clientID, text string) (Dialog, error) {
	s.mu.Lock()
	if s.storageErr != nil {
		s.mu.Unlock()
		return Dialog{}, s.storageErr
	}
	if s.closed {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("хранилище закрыто")
	}
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
	startTitle := len(stored.agent.Messages()) == 1 && stored.dialog.TitleStatus == "idle"
	if startTitle {
		stored.dialog.Title = titleFrom(text)
		stored.dialog.TitleStatus = "pending"
	}
	if err := s.saveLocked(ctx); err != nil {
		s.mu.Unlock()
		return Dialog{}, err
	}
	if startTitle {
		titleContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), stored.textSnapshot.Timeout)
		stored.titleCancel = cancel
		observer := s.observer
		s.titleWG.Add(1)
		go s.runTitle(titleContext, cancel, sessionID, dialogID, stored, message.ID, text, observer)
	}
	return s.runAttempt(ctx, sessionID, dialogID, stored, session, message.ID)
}

func (s *Store) runTitle(ctx context.Context, cancel context.CancelFunc, sessionID, dialogID string, stored *storedDialog, messageID, text string, observer AttemptObserver) {
	defer s.titleWG.Done()
	defer cancel()
	started := time.Now()
	if observer.TitleStarted != nil {
		observer.TitleStarted(ctx, dialogID, messageID)
	}
	answer, err := s.provider.Complete(ctx, stored.textSnapshot, []llm.Message{{Role: "system", Content: stored.textSnapshot.SystemPrompt}, {Role: "user", Content: text}})
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	s.mu.Lock()
	browser := s.sessions[sessionID]
	if browser == nil || browser.dialogs[dialogID] != stored {
		s.mu.Unlock()
		if observer.TitleCancelled != nil {
			observer.TitleCancelled(ctx, dialogID, messageID)
		}
		return
	}
	if stored.titleCancel != nil {
		stored.titleCancel = nil
	}
	if err != nil || ctx.Err() != nil {
		stored.dialog.TitleStatus = "error"
	} else {
		stored.dialog.Title = normalizeTitle(answer, text)
		stored.dialog.TitleStatus = "success"
	}
	dialog := s.dialogCopyLocked(stored)
	if saveErr := s.saveLocked(ctx); saveErr != nil {
		err = saveErr
	}
	s.mu.Unlock()
	if errors.Is(ctx.Err(), context.Canceled) {
		if observer.TitleCancelled != nil {
			observer.TitleCancelled(ctx, dialogID, messageID)
		}
		return
	}
	if observer.TitleFinished != nil {
		observer.TitleFinished(ctx, dialogID, messageID, time.Since(started), dialog, titleErrorCategory(err))
	}
}

func titleErrorCategory(err error) string {
	if err == nil {
		return ""
	}
	var typed *agent.AttemptError
	if errors.As(err, &typed) {
		return string(typed.Category)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "provider"
}

func normalizeTitle(answer, fallback string) string {
	answer = strings.Join(strings.Fields(answer), " ")
	if answer == "" {
		return titleFrom(fallback)
	}
	return titleFrom(answer)
}

// Retry reruns an errored user message without adding another user message.
func (s *Store) Retry(ctx context.Context, sessionID, dialogID, messageID string) (Dialog, error) {
	s.mu.Lock()
	if s.storageErr != nil || s.closed {
		s.mu.Unlock()
		return Dialog{}, ErrStorage
	}
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
	if err := s.saveLocked(ctx); err != nil {
		s.mu.Unlock()
		return Dialog{}, err
	}
	return s.runAttempt(ctx, sessionID, dialogID, stored, session, message.ID)
}

func (s *Store) runAttempt(ctx context.Context, sessionID, dialogID string, stored *storedDialog, session *browserSession, messageID string) (Dialog, error) {
	s.attemptWG.Add(1)
	defer s.attemptWG.Done()
	// Keep correlation values, but detach the accepted work from request cancellation.
	attemptContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), stored.agent.Snapshot().Timeout)
	for _, message := range stored.agent.Messages() {
		if message.ID == messageID {
			attemptContext = llm.WithAttemptID(attemptContext, message.Attempts[len(message.Attempts)-1].ID)
		}
	}
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
	saveErr := s.saveLocked(ctx)
	s.mu.Unlock()
	if observer.Finished != nil {
		observer.Finished(attemptContext, dialogID, messageID, time.Since(startedAt), dialog)
	}
	return dialog, saveErr
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
	dialog.AccountedTokens = agent.AccountedTokens(dialog.Messages)
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
	dialog.Messages = agent.CloneMessages(dialog.Messages)
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
