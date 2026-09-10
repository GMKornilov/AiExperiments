// Package httpapi exposes the barista browser contract.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/config"
	"aichallenge/week_1/task_1/internal/llm"
	"aichallenge/week_1/task_1/internal/observability"
	"aichallenge/week_1/task_1/internal/session"
)

const maxBody = 64 << 10

type Store interface {
	Create(string, agent.DialogSnapshot) (session.Dialog, error)
	List(string) session.Listing
	Get(string, string) (session.Dialog, bool)
	Exists(string) bool
	Select(string, string) (bool, error)
	Delete(string, string) (bool, error)
	Send(context.Context, string, string, string, string) (session.Dialog, error)
	Retry(context.Context, string, string, string) (session.Dialog, error)
}

type Handler struct {
	store        Store
	loadSnapshot func() (agent.DialogSnapshot, error)
	journal      *observability.Journal
}
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func New(store Store, loadSnapshot func() (agent.DialogSnapshot, error), journal *observability.Journal) http.Handler {
	h := &Handler{store: store, loadSnapshot: loadSnapshot, journal: journal}
	if restored, ok := store.(interface{ RegisterSecrets(func(string, ...string)) }); ok {
		restored.RegisterSecrets(journal.SetSecrets)
	}
	if observed, ok := store.(interface{ SetAttemptObserver(session.AttemptObserver) }); ok {
		observed.SetAttemptObserver(session.AttemptObserver{
			Started: func(ctx context.Context, dialogID, messageID string) {
				h.logContext(ctx, "backend", "llm_start", "success", dialogID, messageID, 0, "", "")
			},
			Finished: func(ctx context.Context, dialogID, messageID string, elapsed time.Duration, dialog session.Dialog) {
				result, category, answer := attemptOutcome(dialog)
				record := observability.Record{Source: "backend", Event: "llm_finish", Result: result, CorrelationID: llm.RequestID(ctx), DialogID: dialogID, MessageID: messageID, DurationMS: elapsed.Milliseconds(), ErrorCategory: category, Text: answer}
				for _, message := range dialog.Messages {
					if message.ID == messageID && len(message.Attempts) > 0 {
						attempt := message.Attempts[len(message.Attempts)-1]
						record.AttemptID, record.Usage = attempt.ID, attempt.Usage
					}
				}
				h.journal.Log(record, "")
			},
			TitleStarted: func(ctx context.Context, dialogID, messageID string) {
				h.logContext(ctx, "backend", "title_start", "success", dialogID, messageID, 0, "", "")
			},
			TitleFinished: func(ctx context.Context, dialogID, messageID string, elapsed time.Duration, dialog session.Dialog, category string) {
				event, result := "title_finish", "success"
				if category != "" {
					event, result = "title_error", "failure"
				}
				h.logContext(ctx, "backend", event, result, dialogID, messageID, elapsed, category, dialog.Title)
			},
			TitleCancelled: func(ctx context.Context, dialogID, messageID string) {
				h.logContext(ctx, "backend", "title_cancelled", "success", dialogID, messageID, 0, "", "")
			},
		})
	}
	return h
}

func SnapshotLoader(path string) func() (agent.DialogSnapshot, error) {
	return func() (agent.DialogSnapshot, error) {
		cfg, err := config.LoadLLM(path)
		if err != nil {
			return agent.DialogSnapshot{}, err
		}
		return agent.DialogSnapshot{Chat: agent.Snapshot{BaseURL: cfg.Chat.BaseURL, APIKey: cfg.Chat.APIKey, Model: cfg.Chat.Model, SystemPrompt: cfg.Chat.SystemPrompt, Timeout: cfg.Chat.RequestTimeout, Temperature: cfg.Chat.Temperature}, Text: agent.Snapshot{BaseURL: cfg.Text.BaseURL, APIKey: cfg.Text.APIKey, Model: cfg.Text.Model, SystemPrompt: cfg.Text.SystemPrompt, Timeout: cfg.Text.RequestTimeout, Temperature: cfg.Text.Temperature}}, nil
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	ownedDialogID := ""
	tracked := r.URL.Path != "/healthz" && !(r.URL.Path == "/api/admin/logs" && r.URL.Query().Get("action") == "poll")
	writer := &statusWriter{ResponseWriter: w}
	defer func() {
		if !tracked {
			return
		}
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		result := "success"
		category := ""
		if status >= 400 {
			result = "failure"
			switch status {
			case http.StatusBadRequest:
				category = "validation"
			case http.StatusNotFound:
				category = "not_found"
			case http.StatusConflict:
				category = "busy"
			case http.StatusServiceUnavailable:
				category = "config"
				if durable, ok := h.store.(interface{ StorageError() error }); ok && durable.StorageError() != nil {
					category = "storage"
				}
			default:
				category = "provider"
			}
		}
		h.logContext(r.Context(), "backend", "http_request", result, ownedDialogID, "", time.Since(started), category, "")
	}()
	w = writer
	ctx := llm.WithRequestID(r.Context(), r.Header.Get("X-Request-ID"))
	r = r.WithContext(ctx)
	w.Header().Set("X-Request-ID", llm.RequestID(ctx))
	w.Header().Set("Cache-Control", "no-store")
	if durable, ok := h.store.(interface{ StorageError() error }); ok && durable.StorageError() != nil {
		h.fail(w, http.StatusServiceUnavailable, "storage")
		return
	}
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			method(w, http.MethodGet)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/api/admin/logs" {
		h.admin(w, r)
		return
	}
	if r.URL.Path == "/api/events" {
		h.events(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/dialogs") {
		http.NotFound(w, r)
		return
	}
	sessionID := strings.TrimSpace(r.Header.Get("X-Session-ID"))
	if sessionID == "" {
		h.fail(w, http.StatusBadRequest, "validation")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/dialogs"), "/")
	if len(parts) == 1 && parts[0] == "" {
		h.dialogs(w, r, sessionID)
		return
	}
	if len(parts) < 2 || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	dialogID := parts[1]
	if _, ok := h.store.Get(sessionID, dialogID); ok {
		ownedDialogID = dialogID
	}
	if len(parts) == 2 {
		h.dialog(w, r, sessionID, dialogID)
		return
	}
	if len(parts) == 3 && parts[2] == "select" {
		h.selectDialog(w, r, sessionID, dialogID)
		return
	}
	if len(parts) == 3 && parts[2] == "messages" {
		h.send(w, r, sessionID, dialogID)
		return
	}
	if len(parts) == 5 && parts[2] == "messages" && parts[4] == "retry" {
		h.retry(w, r, sessionID, dialogID, parts[3])
		return
	}
	http.NotFound(w, r)
}

func (h *Handler) dialogs(w http.ResponseWriter, r *http.Request, sid string) {
	switch r.Method {
	case http.MethodGet:
		h.ok(w, listingDTO(h.store.List(sid)))
	case http.MethodPost:
		started := time.Now()
		snap, err := h.loadSnapshot()
		if err != nil {
			h.log(r, "backend", "config_read", "failure", "", "", time.Since(started), "config", "")
			h.fail(w, http.StatusServiceUnavailable, "config")
			return
		}
		d, err := h.store.Create(sid, snap)
		if err != nil {
			if errors.Is(err, session.ErrStorage) {
				h.fail(w, http.StatusServiceUnavailable, "storage")
				return
			}
			h.fail(w, http.StatusServiceUnavailable, "config")
			return
		}
		h.journal.SetSecrets(d.ID, snap.Chat.APIKey, snap.Text.APIKey)
		h.log(r, "backend", "config_read", "success", d.ID, "", time.Since(started), "", "")
		h.log(r, "backend", "dialog_created", "success", d.ID, "", time.Since(started), "", "")
		h.ok(w, dialogDTO(d))
	default:
		method(w, http.MethodGet, http.MethodPost)
	}
}
func (h *Handler) dialog(w http.ResponseWriter, r *http.Request, sid, id string) {
	switch r.Method {
	case http.MethodGet:
		d, ok := h.store.Get(sid, id)
		if !ok {
			h.fail(w, 404, "not_found")
			return
		}
		h.ok(w, dialogDTO(d))
	case http.MethodDelete:
		deleted, err := h.store.Delete(sid, id)
		if err != nil {
			h.fail(w, http.StatusServiceUnavailable, "storage")
			return
		}
		if !deleted {
			h.fail(w, 404, "not_found")
			return
		}
		h.log(r, "backend", "llm_cancel", "success", id, "", 0, "cancelled", "")
		h.log(r, "backend", "dialog_deleted", "success", id, "", 0, "", "")
		h.journal.DeleteDialog(id)
		w.WriteHeader(204)
	default:
		method(w, http.MethodGet, http.MethodDelete)
	}
}
func (h *Handler) selectDialog(w http.ResponseWriter, r *http.Request, sid, id string) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	selected, err := h.store.Select(sid, id)
	if err != nil {
		h.fail(w, http.StatusServiceUnavailable, "storage")
		return
	}
	if !selected {
		h.fail(w, 404, "not_found")
		return
	}
	h.log(r, "backend", "dialog_selected", "success", id, "", 0, "", "")
	w.WriteHeader(204)
}

type sendRequest struct {
	ClientMessageID string `json:"client_message_id"`
	Text            string `json:"text"`
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request, sid, id string) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	var payload sendRequest
	if !decode(w, r, &payload) {
		h.log(r, "backend", "message_sent", "failure", id, "", 0, "validation", "")
		h.fail(w, 400, "validation")
		return
	}
	started := time.Now()
	d, err := h.store.Send(r.Context(), sid, id, payload.ClientMessageID, payload.Text)
	if err != nil {
		h.sendError(w, err)
		return
	}
	result, category, _ := attemptOutcome(d)
	h.log(r, "backend", "message_sent", result, id, payload.ClientMessageID, time.Since(started), category, payload.Text)
	h.ok(w, dialogDTO(d))
}
func (h *Handler) retry(w http.ResponseWriter, r *http.Request, sid, id, mid string) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	started := time.Now()
	d, err := h.store.Retry(r.Context(), sid, id, mid)
	if err != nil {
		h.sendError(w, err)
		return
	}
	result, category, _ := attemptOutcome(d)
	h.log(r, "backend", "message_retried", result, id, mid, time.Since(started), category, "")
	h.ok(w, dialogDTO(d))
}
func (h *Handler) sendError(w http.ResponseWriter, err error) {
	text := err.Error()
	switch {
	case errors.Is(err, session.ErrStorage):
		h.fail(w, http.StatusServiceUnavailable, "storage")
	case strings.Contains(text, "не найден"):
		h.fail(w, 404, "not_found")
	case strings.Contains(text, "уже выполняется"):
		h.fail(w, 409, "busy")
	case strings.Contains(text, "сообщение нельзя"):
		h.fail(w, 409, "busy")
	default:
		h.fail(w, 400, "validation")
	}
}

func attemptOutcome(d session.Dialog) (string, string, string) {
	if len(d.Messages) == 0 {
		return "failure", "provider", ""
	}
	last := d.Messages[len(d.Messages)-1]
	if last.Role == "assistant" && last.Status == agent.StatusSuccess {
		return "success", "", last.Text
	}
	if last.Role == "user" && last.Status == agent.StatusError {
		return "failure", last.ErrorCategory, ""
	}
	return "failure", "provider", ""
}

type eventRequest struct {
	Event         string `json:"event"`
	DialogID      string `json:"dialog_id"`
	MessageID     string `json:"message_id"`
	ErrorCategory string `json:"error_category"`
	DurationMS    int64  `json:"duration_ms"`
}

var allowedEvents = map[string]bool{"dialog_created": true, "dialog_selected": true, "dialog_deleted": true, "dialog_id_copied": true, "message_sent": true, "message_retried": true, "message_copied": true, "dialog_validation_failed": true, "message_failed": true, "admin_lookup": true, "admin_refresh": true, "bff_request_completed": true, "bff_request_failed": true}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	if strings.TrimSpace(r.Header.Get("X-Session-ID")) == "" {
		h.fail(w, 400, "validation")
		return
	}
	var p eventRequest
	if !decode(w, r, &p) || !allowedEvents[p.Event] {
		h.fail(w, 400, "validation")
		return
	}
	if p.ErrorCategory != "" && !map[string]bool{"validation": true, "network": true, "timeout": true, "provider": true, "context_limit": true, "invalid_response": true, "cancelled": true, "storage": true}[p.ErrorCategory] {
		h.fail(w, 400, "validation")
		return
	}
	if p.DurationMS < 0 || p.DurationMS > 300000 {
		h.fail(w, http.StatusBadRequest, "validation")
		return
	}
	if p.DialogID != "" {
		if _, ok := h.store.Get(r.Header.Get("X-Session-ID"), p.DialogID); !ok {
			h.fail(w, http.StatusNotFound, "not_found")
			return
		}
	}
	result := "success"
	if strings.HasSuffix(p.Event, "failed") {
		result = "failure"
	}
	h.log(r, "frontend", p.Event, result, p.DialogID, p.MessageID, time.Duration(p.DurationMS)*time.Millisecond, p.ErrorCategory, "")
	w.WriteHeader(204)
}
func (h *Handler) admin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		method(w, http.MethodGet)
		return
	}
	id := r.URL.Query().Get("dialog_id")
	action := r.URL.Query().Get("action")
	if action != "lookup" && action != "refresh" && action != "poll" {
		h.fail(w, 400, "validation")
		return
	}
	found := h.store.Exists(id)
	if action != "poll" {
		if found {
			h.log(r, "backend", "admin_"+action, "success", id, "", 0, "", "")
		} else {
			h.log(r, "backend", "admin_"+action, "success", "", "", 0, "not_found", "")
		}
	}
	logs := h.journal.Logs(id)
	h.ok(w, map[string]any{"found": found, "log_text_payloads": h.journal.LogTextPayloads(), "logs": logs})
}
func (h *Handler) log(r *http.Request, source, event, result, did, mid string, d time.Duration, category, text string) {
	h.logContext(r.Context(), source, event, result, did, mid, d, category, text)
}
func (h *Handler) logContext(ctx context.Context, source, event, result, did, mid string, d time.Duration, category, text string) {
	h.journal.Log(observability.Record{Source: source, Event: event, Result: result, CorrelationID: llm.RequestID(ctx), DialogID: did, MessageID: mid, AttemptID: llm.AttemptID(ctx), DurationMS: d.Milliseconds(), ErrorCategory: category, Text: text}, "")
}
func (h *Handler) ok(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func (h *Handler) fail(w http.ResponseWriter, status int, category string) {
	msg := "Не удалось получить ответ. Повторите отправку."
	if category == "validation" {
		msg = "Некорректный запрос."
	}
	if category == "not_found" {
		msg = "Данные не найдены."
	}
	if category == "busy" {
		msg = "Запрос уже выполняется."
	}
	if category == "config" || category == "storage" {
		msg = "Сервис временно недоступен."
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"category": category, "message": msg}})
}
func method(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	w.WriteHeader(405)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	d := json.NewDecoder(r.Body)
	if d.Decode(v) != nil {
		return false
	}
	return errors.Is(d.Decode(&struct{}{}), io.EOF)
}

type messageDTO struct {
	Usage           *llm.Usage   `json:"usage,omitempty"`
	ID              string       `json:"id"`
	ClientMessageID string       `json:"client_message_id,omitempty"`
	Role            string       `json:"role"`
	Text            string       `json:"text"`
	Status          agent.Status `json:"status"`
	CreatedAt       time.Time    `json:"created_at"`
	ErrorCategory   string       `json:"error_category,omitempty"`
}
type dialogDTOType struct {
	AccountedTokens int64        `json:"accounted_tokens"`
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	TitleStatus     string       `json:"title_status"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	Messages        []messageDTO `json:"messages"`
}

func dialogDTO(d session.Dialog) dialogDTOType {
	out := dialogDTOType{ID: d.ID, Title: d.Title, TitleStatus: d.TitleStatus, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt, AccountedTokens: d.AccountedTokens, Messages: make([]messageDTO, 0, len(d.Messages))}
	for _, m := range d.Messages {
		out.Messages = append(out.Messages, messageDTO{ID: m.ID, ClientMessageID: m.ClientID, Role: m.Role, Text: m.Text, Status: m.Status, CreatedAt: m.CreatedAt, ErrorCategory: m.ErrorCategory, Usage: m.Usage})
	}
	return out
}
func listingDTO(l session.Listing) map[string]any {
	ds := make([]dialogDTOType, 0, len(l.Dialogs))
	for _, d := range l.Dialogs {
		ds = append(ds, dialogDTO(d))
	}
	return map[string]any{"dialogs": ds, "selected_dialog_id": l.SelectedDialogID}
}
