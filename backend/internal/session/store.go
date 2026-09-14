// Package session provides a browser-session-scoped dialog store with JSON persistence.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
)

var factsKey = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Dialog is the safe public representation of a dialog.
type Dialog struct {
	Compression           agent.CompressionState `json:"compression"`
	AccountedTokens       int64                  `json:"accounted_tokens"`
	ID                    string                 `json:"id"`
	Title                 string                 `json:"title"`
	TitleStatus           string                 `json:"title_status"`
	Messages              []agent.Message        `json:"messages"`
	CreatedAt             time.Time              `json:"created_at"`
	UpdatedAt             time.Time              `json:"updated_at"`
	ContextStrategy       agent.ContextStrategy  `json:"context_strategy"`
	ContextWindowMessages int                    `json:"context_window_messages"`
	Facts                 map[string]string      `json:"facts"`
	FactsTokens           int64                  `json:"facts_tokens"`
	FactsUsageMissing     bool                   `json:"facts_usage_missing"`
	ActiveBranchID        string                 `json:"active_branch_id,omitempty"`
	Branches              []Branch               `json:"branches,omitempty"`
}

// Branch is a safe description of a branch; its transcript remains private to the store.
type Branch struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	ParentBranchID      string `json:"parent_branch_id,omitempty"`
	CheckpointMessageID string `json:"checkpoint_message_id,omitempty"`
	MessageCount        int    `json:"message_count"`
}

// Listing is a browser session's dialog list and selected dialog ID.
type Listing struct {
	Dialogs          []Dialog `json:"dialogs"`
	SelectedDialogID string   `json:"selected_dialog_id"`
}

type storedDialog struct {
	dialog              Dialog
	agent               *agent.Conversation
	textSnapshot        agent.Snapshot
	factsSnapshot       *agent.Snapshot
	titleCancel         context.CancelFunc
	branches            map[string]*storedBranch
	activeBranch        string
	factsReadyMessageID string
}

type storedBranch struct {
	meta               Branch
	agent              *agent.Conversation
	parentMessageCount int
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
	strategy := agent.StrategySlidingWindow
	if snapshot.Summary != nil {
		strategy = agent.StrategySummary
	}
	return s.CreateWithStrategy(sessionID, snapshot, strategy)
}

// CreateWithStrategy creates a dialog with the explicitly selected policy.
func (s *Store) CreateWithStrategy(sessionID string, snapshot agent.DialogSnapshot, strategy agent.ContextStrategy) (Dialog, error) {
	if strings.TrimSpace(sessionID) == "" {
		return Dialog{}, fmt.Errorf("session ID не должен быть пустым")
	}
	if err := snapshot.Validate(); err != nil {
		return Dialog{}, err
	}
	if !strategy.Valid() {
		return Dialog{}, fmt.Errorf("некорректная стратегия контекста")
	}
	if snapshot.ContextWindowMessages == 0 {
		snapshot.ContextWindowMessages = 10
	}
	if strategy == agent.StrategyFacts && snapshot.Facts == nil {
		return Dialog{}, fmt.Errorf("facts не настроен")
	}
	if strategy == agent.StrategySummary && snapshot.Summary == nil {
		return Dialog{}, fmt.Errorf("summary не настроен")
	}
	conversation, err := agent.NewConversation(snapshot.Chat)
	if err != nil {
		return Dialog{}, err
	}
	if err := conversation.ConfigureContext(strategy, snapshot.ContextWindowMessages); err != nil {
		return Dialog{}, err
	}
	compression := agent.CompressionState{Enabled: strategy == agent.StrategySummary}
	if err := conversation.ConfigureCompression(snapshot.Summary, compression); err != nil {
		return Dialog{}, err
	}
	id, err := randomID()
	if err != nil {
		return Dialog{}, err
	}
	now := s.now()
	dialog := Dialog{Compression: conversation.Compression(), ID: id, Title: "Новый диалог", TitleStatus: "idle", CreatedAt: now, UpdatedAt: now, ContextStrategy: strategy, ContextWindowMessages: snapshot.ContextWindowMessages, Facts: map[string]string{}}
	stored := &storedDialog{dialog: dialog, agent: conversation, textSnapshot: snapshot.Text}
	if snapshot.Facts != nil {
		facts := snapshot.Facts.Snapshot
		stored.factsSnapshot = &facts
	}
	if strategy == agent.StrategyBranching {
		branchID, branchErr := randomID()
		if branchErr != nil {
			return Dialog{}, branchErr
		}
		stored.activeBranch = branchID
		stored.branches = map[string]*storedBranch{branchID: {meta: Branch{ID: branchID, Name: "Основная ветка"}, agent: conversation}}
		stored.dialog.ActiveBranchID = branchID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Dialog{}, fmt.Errorf("хранилище закрыто")
	}
	if s.storageErr != nil {
		return Dialog{}, s.storageErr
	}
	session := s.getOrCreateLocked(sessionID)
	session.dialogs[id] = stored
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
		dialog := s.dialogCopyLocked(stored)
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
	return s.dialogCopyLocked(stored), true
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

// UpdateStrategy changes an explicit selection only while its active line is empty.
func (s *Store) UpdateStrategy(ctx context.Context, sessionID, dialogID string, strategy agent.ContextStrategy) (Dialog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Dialog{}, fmt.Errorf("хранилище закрыто")
	}
	if s.storageErr != nil {
		return Dialog{}, s.storageErr
	}
	stored := s.dialogLocked(sessionID, dialogID)
	if stored == nil {
		return Dialog{}, fmt.Errorf("диалог не найден")
	}
	if !strategy.Valid() || len(stored.dialog.Messages) != 0 {
		return Dialog{}, fmt.Errorf("стратегию можно изменить только в пустом диалоге")
	}
	if strategy == agent.StrategyFacts && stored.factsSnapshot == nil {
		return Dialog{}, fmt.Errorf("facts не настроен")
	}
	if strategy == agent.StrategySummary && currentAgent(stored).SummaryConfig() == nil {
		return Dialog{}, fmt.Errorf("summary не настроен")
	}
	conversation := currentAgent(stored)
	if strategy == agent.StrategyBranching && stored.dialog.ContextStrategy != agent.StrategyBranching {
		branchID, idErr := randomID()
		if idErr != nil {
			return Dialog{}, idErr
		}
		stored.branches = map[string]*storedBranch{branchID: {meta: Branch{ID: branchID, Name: "Основная ветка"}, agent: conversation}}
		stored.activeBranch = branchID
		stored.dialog.ActiveBranchID = branchID
	}
	if strategy != agent.StrategyBranching && stored.dialog.ContextStrategy == agent.StrategyBranching {
		stored.agent = conversation
		stored.branches = nil
		stored.activeBranch = ""
		stored.dialog.ActiveBranchID = ""
		stored.dialog.Branches = nil
	}
	if err := conversation.ConfigureContext(strategy, stored.dialog.ContextWindowMessages); err != nil {
		return Dialog{}, err
	}
	if stored.dialog.ContextStrategy == agent.StrategySummary && strategy != agent.StrategySummary {
		state := conversation.Compression()
		state.Enabled = false
		state.Summary = ""
		state.PrunedMessages = 0
		state.ArchivedTokens = 0
		state.CoveredMessages = 0
		state.FullEstimate = 0
		state.SentEstimate = 0
		state.LastInputTokens = nil
		if err := conversation.ConfigureCompression(conversation.SummaryConfig(), state); err != nil {
			return Dialog{}, err
		}
	}
	if strategy == agent.StrategySummary {
		if err := conversation.SetCompression(true); err != nil {
			return Dialog{}, err
		}
	} else if err := conversation.SetCompression(false); err != nil {
		return Dialog{}, err
	}
	stored.dialog.ContextStrategy = strategy
	stored.dialog.UpdatedAt = s.now()
	return s.dialogCopyLocked(stored), s.saveLocked(ctx)
}

// AddBranch clones the successful active line and selects the new child.
func (s *Store) AddBranch(ctx context.Context, sessionID, dialogID string) (Dialog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.storageErr != nil {
		return Dialog{}, ErrStorage
	}
	stored := s.dialogLocked(sessionID, dialogID)
	browser := s.sessions[sessionID]
	if stored == nil {
		return Dialog{}, fmt.Errorf("диалог не найден")
	}
	if stored.dialog.ContextStrategy != agent.StrategyBranching {
		return Dialog{}, fmt.Errorf("ветки доступны только для Branching")
	}
	if browser.pending {
		return Dialog{}, fmt.Errorf("уже выполняется запрос")
	}
	parent := stored.branches[stored.activeBranch]
	if parent == nil {
		return Dialog{}, fmt.Errorf("активная ветка не найдена")
	}
	messages, state := parent.agent.Memory()
	if len(messages) == 0 || messages[len(messages)-1].Role != "assistant" || messages[len(messages)-1].Status != agent.StatusSuccess {
		return Dialog{}, fmt.Errorf("ветку можно создать только после успешного ответа")
	}
	childID, err := randomID()
	if err != nil {
		return Dialog{}, err
	}
	child, err := agent.RestoreCompactedConversation(parent.agent.Snapshot(), messages, state)
	if err != nil {
		return Dialog{}, err
	}
	if err := child.ConfigureContext(agent.StrategyBranching, stored.dialog.ContextWindowMessages); err != nil {
		return Dialog{}, err
	}
	if err := child.ConfigureCompression(parent.agent.SummaryConfig(), state); err != nil {
		return Dialog{}, err
	}
	child.SetBranchSuffix(childID[:8])
	stored.branches[childID] = &storedBranch{meta: Branch{ID: childID, Name: fmt.Sprintf("Ветка %d", len(stored.branches)), ParentBranchID: parent.meta.ID, CheckpointMessageID: messages[len(messages)-1].ID}, agent: child, parentMessageCount: len(messages)}
	stored.activeBranch = childID
	stored.dialog.UpdatedAt = s.now()
	return s.dialogCopyLocked(stored), s.saveLocked(ctx)
}

// SelectBranch switches the active line without contacting an LLM.
func (s *Store) SelectBranch(ctx context.Context, sessionID, dialogID, branchID string) (Dialog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.storageErr != nil {
		return Dialog{}, ErrStorage
	}
	stored := s.dialogLocked(sessionID, dialogID)
	if stored == nil {
		return Dialog{}, fmt.Errorf("диалог не найден")
	}
	if stored.dialog.ContextStrategy != agent.StrategyBranching || stored.branches[branchID] == nil {
		return Dialog{}, fmt.Errorf("ветка не найдена")
	}
	if s.sessions[sessionID].pending {
		return Dialog{}, fmt.Errorf("уже выполняется запрос")
	}
	stored.activeBranch = branchID
	stored.dialog.UpdatedAt = s.now()
	return s.dialogCopyLocked(stored), s.saveLocked(ctx)
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
	conversation := currentAgent(stored)
	if conversation == nil {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("активная ветка не найдена")
	}
	if _, found := conversation.FindClientMessage(clientID); found {
		dialog := s.dialogCopyLocked(stored)
		s.mu.Unlock()
		return dialog, nil
	}
	for _, accepted := range stored.dialog.Messages {
		if accepted.ClientID == clientID {
			dialog := s.dialogCopyLocked(stored)
			s.mu.Unlock()
			return dialog, nil
		}
	}
	if session.pending {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("в этом сеансе уже выполняется запрос")
	}
	message, err := conversation.Begin(clientID, text)
	if err != nil {
		s.mu.Unlock()
		return Dialog{}, err
	}
	startTitle := len(conversation.Messages()) == 1 && stored.dialog.TitleStatus == "idle"
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
	ctx = llm.WithPurpose(llm.WithAttemptID(ctx, messageID+"-title"), "title")
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
	conversation := currentAgent(stored)
	if conversation == nil {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("активная ветка не найдена")
	}
	message, err := conversation.Retry(messageID)
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
	conversation := currentAgent(stored)
	if conversation == nil {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("активная ветка не найдена")
	}
	timeout := conversation.Snapshot().Timeout
	if cfg := conversation.SummaryConfig(); cfg != nil {
		timeout += cfg.Snapshot.Timeout
	}
	if stored.dialog.ContextStrategy == agent.StrategyFacts {
		if cfg := stored.dialogFactsConfig(); cfg != nil {
			timeout += cfg.Timeout
		}
	}
	attemptContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	for _, message := range conversation.Messages() {
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

	attemptErr := s.attemptConversation(attemptContext, stored, conversation, messageID)
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
	// Capture the complete UI transcript before discarding old agent memory.
	_ = s.dialogCopyLocked(stored)
	if messages := conversation.Messages(); len(messages) > 0 && messages[len(messages)-1].Role == "assistant" {
		conversation.PruneContextMemory()
	}
	dialog := s.dialogCopyLocked(stored)
	observer = s.observer
	saveErr := s.saveLocked(ctx)
	s.mu.Unlock()
	if observer.Finished != nil {
		observer.Finished(attemptContext, dialogID, messageID, time.Since(startedAt), dialog)
	}
	return dialog, saveErr
}

func (s *Store) attemptConversation(ctx context.Context, stored *storedDialog, conversation *agent.Conversation, messageID string) error {
	if stored.dialog.ContextStrategy != agent.StrategyFacts {
		return conversation.Attempt(ctx, s.provider, messageID)
	}
	if stored.factsReadyMessageID != messageID {
		facts, usage, err := s.extractFacts(ctx, stored, conversation, messageID)
		if err != nil {
			s.mu.Lock()
			recordFactsUsage(&stored.dialog, usage)
			_ = s.saveLocked(ctx)
			s.mu.Unlock()
			return conversation.Finish(messageID, "", err)
		}
		s.mu.Lock()
		stored.dialog.Facts = facts
		recordFactsUsage(&stored.dialog, usage)
		stored.factsReadyMessageID = messageID
		// Persist extraction success before starting chat: retry/restart must not repeat it.
		err = s.saveLocked(ctx)
		s.mu.Unlock()
		if err != nil {
			return conversation.Finish(messageID, "", err)
		}
	}
	return conversation.AttemptWithFacts(ctx, s.provider, messageID, cloneFacts(stored.dialog.Facts))
}

func recordFactsUsage(dialog *Dialog, usage *llm.Usage) {
	if usage != nil && usage.Valid() && usage.PromptTokens+usage.CompletionTokens <= llm.MaxSafeTokens-dialog.FactsTokens {
		dialog.FactsTokens += usage.PromptTokens + usage.CompletionTokens
		return
	}
	dialog.FactsUsageMissing = true
}

func (s *Store) extractFacts(ctx context.Context, stored *storedDialog, conversation *agent.Conversation, messageID string) (map[string]string, *llm.Usage, error) {
	snapshot := stored.dialogFactsConfig()
	if snapshot == nil {
		return nil, nil, fmt.Errorf("facts не настроен")
	}
	contextMessages, err := conversation.Context(messageID)
	if err != nil {
		return nil, nil, err
	}
	payload := struct {
		PreviousFacts map[string]string `json:"previous_facts"`
		Messages      []llm.Message     `json:"messages"`
	}{PreviousFacts: cloneFacts(stored.dialog.Facts), Messages: contextMessages[1:]}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, fmt.Errorf("кодирование facts: %w", err)
	}
	input := []llm.Message{{Role: "system", Content: snapshot.SystemPrompt}, {Role: "user", Content: string(data)}}
	factsCtx, cancel := context.WithTimeout(llm.WithPurpose(ctx, "facts"), snapshot.Timeout)
	defer cancel()
	var result llm.Completion
	if metered, ok := s.provider.(interface {
		CompleteWithUsage(context.Context, agent.Snapshot, []llm.Message) (llm.Completion, error)
	}); ok {
		result, err = metered.CompleteWithUsage(factsCtx, *snapshot, input)
	} else {
		result.Text, err = s.provider.Complete(factsCtx, *snapshot, input)
	}
	if err == nil && factsCtx.Err() != nil {
		err = factsCtx.Err()
	}
	if err != nil {
		return nil, result.Usage, err
	}
	facts, err := parseFacts(result.Text)
	if err != nil {
		return nil, result.Usage, &agent.AttemptError{Category: agent.ErrorInvalidResponse, Err: err}
	}
	return facts, result.Usage, nil
}

func parseFacts(text string) (map[string]string, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	opening, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("facts должен быть JSON object")
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return nil, fmt.Errorf("facts должен быть JSON object")
	}
	facts := make(map[string]string)
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		if tokenErr != nil {
			return nil, fmt.Errorf("facts должен быть JSON object")
		}
		key, ok := keyToken.(string)
		if !ok || facts[key] != "" {
			return nil, fmt.Errorf("дублирующийся ключ facts")
		}
		if !factsKey.MatchString(key) {
			return nil, fmt.Errorf("некорректный ключ facts")
		}
		var textValue any
		if decoder.Decode(&textValue) != nil {
			return nil, fmt.Errorf("некорректное значение facts")
		}
		value, ok := textValue.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("некорректное значение facts")
		}
		facts[key] = value
	}
	closing, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("facts должен быть JSON object")
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return nil, fmt.Errorf("facts должен быть JSON object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("facts должен быть единственным JSON object")
	}
	return facts, nil
}

func (s *Store) dialogLocked(sessionID, dialogID string) *storedDialog {
	session := s.sessions[sessionID]
	if session == nil {
		return nil
	}
	return session.dialogs[dialogID]
}

func currentAgent(stored *storedDialog) *agent.Conversation {
	if stored == nil || stored.dialog.ContextStrategy != agent.StrategyBranching {
		if stored == nil {
			return nil
		}
		return stored.agent
	}
	branch := stored.branches[stored.activeBranch]
	if branch == nil {
		return nil
	}
	return branch.agent
}

func (stored *storedDialog) dialogFactsConfig() *agent.Snapshot {
	if stored.factsSnapshot == nil {
		return nil
	}
	copy := *stored.factsSnapshot
	return &copy
}

func (s *Store) dialogCopyLocked(stored *storedDialog) Dialog {
	// The transcript belongs to the UI; it is never passed back into the agent.
	conversation := currentAgent(stored)
	if conversation == nil {
		return Dialog{}
	}
	tail, state := conversation.Memory()
	if stored.dialog.ContextStrategy == agent.StrategyBranching {
		stored.dialog.Messages = tail
		stored.dialog.ActiveBranchID = stored.activeBranch
		stored.dialog.Branches = make([]Branch, 0, len(stored.branches))
		for _, branch := range stored.branches {
			meta := branch.meta
			meta.MessageCount = len(branch.agent.Messages())
			stored.dialog.Branches = append(stored.dialog.Branches, meta)
		}
		sort.Slice(stored.dialog.Branches, func(i, j int) bool {
			return branchOrder(stored.dialog.Branches[i]) < branchOrder(stored.dialog.Branches[j])
		})
	}
	positions := make(map[string]int, len(stored.dialog.Messages))
	for i, message := range stored.dialog.Messages {
		positions[message.ID] = i
	}
	for _, message := range tail {
		if i, ok := positions[message.ID]; ok {
			stored.dialog.Messages[i] = message
		} else {
			positions[message.ID] = len(stored.dialog.Messages)
			stored.dialog.Messages = append(stored.dialog.Messages, message)
		}
	}
	dialog := cloneDialog(stored.dialog)
	if stored.dialog.ContextStrategy == agent.StrategyBranching {
		dialog.AccountedTokens = branchAccountedTokens(stored)
	} else {
		dialog.AccountedTokens = state.ArchivedTokens + agent.AccountedTokens(tail)
	}
	dialog.Compression = state
	return dialog
}

func branchOrder(branch Branch) int {
	if branch.Name == "Основная ветка" {
		return 0
	}
	const prefix = "Ветка "
	if value, err := strconv.Atoi(strings.TrimPrefix(branch.Name, prefix)); err == nil && value > 0 {
		return value
	}
	return int(^uint(0) >> 1)
}

func branchAccountedTokens(stored *storedDialog) int64 {
	var total int64
	for _, branch := range stored.branches {
		messages := branch.agent.Messages()
		start := branch.parentMessageCount
		if start < 0 || start > len(messages) {
			continue
		}
		total += agent.AccountedTokens(messages[start:])
	}
	return total
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
	dialog.Facts = cloneFacts(dialog.Facts)
	dialog.Branches = append([]Branch(nil), dialog.Branches...)
	return dialog
}

func cloneFacts(facts map[string]string) map[string]string {
	if facts == nil {
		return map[string]string{}
	}
	copy := make(map[string]string, len(facts))
	for key, value := range facts {
		copy[key] = value
	}
	return copy
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

func (s *Store) SetCompression(ctx context.Context, sid, id string, enabled bool) (Dialog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Dialog{}, fmt.Errorf("хранилище закрыто")
	}
	if s.storageErr != nil {
		return Dialog{}, s.storageErr
	}
	stored := s.dialogLocked(sid, id)
	if stored == nil {
		return Dialog{}, fmt.Errorf("диалог не найден")
	}
	if s.sessions[sid].pending {
		return Dialog{}, fmt.Errorf("уже выполняется запрос")
	}
	conversation := currentAgent(stored)
	if conversation == nil {
		return Dialog{}, fmt.Errorf("активная ветка не найдена")
	}
	if err := conversation.SetCompression(enabled); err != nil {
		return Dialog{}, err
	}
	stored.dialog.UpdatedAt = s.now()
	return s.dialogCopyLocked(stored), s.saveLocked(ctx)
}

// Compact runs a standalone summary without adding a transcript message.
func (s *Store) Compact(ctx context.Context, sid, id string) (Dialog, error) {
	s.mu.Lock()
	if s.closed || s.storageErr != nil {
		s.mu.Unlock()
		return Dialog{}, ErrStorage
	}
	stored := s.dialogLocked(sid, id)
	if stored == nil {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("диалог не найден")
	}
	browser := s.sessions[sid]
	if browser.pending {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("уже выполняется запрос")
	}
	if stored.dialog.ContextStrategy != agent.StrategySummary {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("/compact доступен только для Summary")
	}
	conversation := currentAgent(stored)
	if conversation == nil || conversation.SummaryConfig() == nil {
		s.mu.Unlock()
		return Dialog{}, fmt.Errorf("суммаризация не настроена")
	}
	compactCtx, cancel := context.WithCancel(ctx)
	browser.pending, browser.cancel, browser.pendingDialogID = true, cancel, id
	s.attemptWG.Add(1)
	s.mu.Unlock()
	defer s.attemptWG.Done()
	err := conversation.Compact(compactCtx, s.provider)
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dialogLocked(sid, id) != stored {
		return Dialog{}, fmt.Errorf("диалог не найден")
	}
	browser.pending, browser.cancel, browser.pendingDialogID = false, nil, ""
	stored.dialog.UpdatedAt = s.now()
	dialog := s.dialogCopyLocked(stored)
	if saveErr := s.saveLocked(ctx); saveErr != nil {
		return Dialog{}, saveErr
	}
	return dialog, err
}
