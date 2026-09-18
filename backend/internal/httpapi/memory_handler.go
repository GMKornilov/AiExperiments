package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
	"aichallenge/week_1/task_1/internal/memory"
	"aichallenge/week_1/task_1/internal/observability"
)

// NewMemory exposes the project and three-layer-memory browser contract.
func NewMemory(store *memory.Store, journal *observability.Journal) http.Handler {
	return &memoryHandler{store: store, journal: journal}
}

type memoryHandler struct {
	store   *memory.Store
	journal *observability.Journal
}
type memoryStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *memoryStatusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *memoryStatusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (h *memoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	projectID, chatID := "", ""
	ctx := llm.WithRequestID(r.Context(), r.Header.Get("X-Request-ID"))
	r = r.WithContext(ctx)
	w.Header().Set("X-Request-ID", llm.RequestID(ctx))
	tracked := r.URL.Path != "/healthz"
	writer := &memoryStatusWriter{ResponseWriter: w}
	defer func() {
		if !tracked {
			return
		}
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		result, category := "success", ""
		if status >= 400 {
			result = "failure"
			category = "provider"
			if status == 400 {
				category = "validation"
			}
			if status == 404 {
				category = "not_found"
			}
			if status == 409 {
				category = "busy"
			}
			if status == 503 {
				category = "storage"
			}
		}
		slog.Info("barista.memory_request", "source", "backend", "event", "memory_request", "result", result, "error_category", category, "correlation_id", llm.RequestID(ctx), "project_id", projectID, "chat_id", chatID, "duration_ms", time.Since(started).Milliseconds(), "path", r.URL.Path, "method", r.Method)
		if chatID != "" {
			h.journal.Log(observability.Record{Source: "backend", Event: "http_request", Result: result, CorrelationID: llm.RequestID(ctx), DialogID: chatID, DurationMS: time.Since(started).Milliseconds(), ErrorCategory: category}, "")
		}
	}()
	w = writer
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			method(w, http.MethodGet)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if h.store.StorageError() != nil {
		failMemory(w, http.StatusServiceUnavailable, "storage")
		return
	}
	if r.URL.Path == "/api/admin/logs" {
		if r.URL.Query().Get("action") != "poll" && h.store.HasChat(r.URL.Query().Get("dialog_id")) {
			chatID = r.URL.Query().Get("dialog_id")
		}
		h.admin(w, r)
		return
	}
	sid := strings.TrimSpace(r.Header.Get("X-Session-ID"))
	if sid == "" {
		failMemory(w, 400, "validation")
		return
	}
	if r.URL.Path == "/api/profiles" {
		h.profiles(w, r, sid)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/profiles/") {
		parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/profiles/"), "/"), "/")
		if len(parts) == 2 && parts[1] == "select" {
			h.selectProfile(w, r, sid, parts[0])
			return
		}
		if len(parts) == 1 && parts[0] != "" {
			h.profile(w, r, sid, parts[0])
			return
		}
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/projects"), "/"), "/")
	if !strings.HasPrefix(r.URL.Path, "/api/projects") {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 1 && parts[0] == "" {
		h.projects(w, r, sid)
		return
	}
	pid := parts[0]
	projectID = pid
	if len(parts) == 1 {
		h.project(w, r, sid, pid)
		return
	}
	if len(parts) == 2 && parts[1] == "select" {
		h.selectProject(w, r, sid, pid)
		return
	}
	if len(parts) == 2 && parts[1] == "memory" {
		h.memory(w, r, sid, pid)
		return
	}
	if len(parts) == 3 && parts[1] == "memory" && (parts[2] == "global" || parts[2] == "project") {
		h.clear(w, r, sid, pid, parts[2])
		return
	}
	if parts[1] != "chats" {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 2 {
		h.chats(w, r, sid, pid)
		return
	}
	cid := parts[2]
	chatID = cid
	r = r.WithContext(llm.WithTrace(r.Context(), func(traceCtx context.Context, trace llm.Trace) {
		result := "success"
		if trace.Event == "llm_request" {
			result = "started"
		}
		if trace.ErrorCategory != "" {
			result = "failure"
		}
		h.journal.Log(observability.Record{Source: "backend", Event: trace.Event, Result: result,
			CorrelationID: llm.RequestID(traceCtx), DialogID: cid, CallID: trace.CallID, Purpose: trace.Purpose,
			Payload: trace.Payload, HTTPStatus: trace.HTTPStatus, DurationMS: trace.DurationMS,
			ErrorCategory: trace.ErrorCategory, Truncated: trace.Truncated}, "")
	}))
	if len(parts) == 3 {
		h.chat(w, r, sid, pid, cid)
		return
	}
	if len(parts) == 4 && parts[3] == "select" {
		h.selectChat(w, r, sid, pid, cid)
		return
	}
	if len(parts) == 4 && parts[3] == "messages" {
		h.send(w, r, sid, pid, cid)
		return
	}
	if len(parts) == 5 && parts[3] == "tasks" && parts[4] == "input" {
		h.taskInput(w, r, sid, pid, cid)
		return
	}
	if len(parts) == 6 && parts[3] == "tasks" && parts[5] == "pause" {
		h.pauseTask(w, r, sid, pid, cid, parts[4])
		return
	}
	if len(parts) == 6 && parts[3] == "tasks" && parts[5] == "resume" {
		h.resumeTask(w, r, sid, pid, cid, parts[4])
		return
	}
	if len(parts) == 6 && parts[3] == "messages" && parts[5] == "retry" {
		h.retry(w, r, sid, pid, cid, parts[4])
		return
	}
	http.NotFound(w, r)
}

func (h *memoryHandler) admin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		method(w, http.MethodGet)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("dialog_id"))
	action := r.URL.Query().Get("action")
	if action != "lookup" && action != "refresh" && action != "poll" {
		failMemory(w, http.StatusBadRequest, "validation")
		return
	}
	found := h.store.HasChat(id)
	if action != "poll" && found {
		h.journal.Log(observability.Record{Source: "backend", Event: "admin_" + action, Result: "success", CorrelationID: llm.RequestID(r.Context()), DialogID: id}, "")
	}
	writeMemory(w, map[string]any{"found": found, "log_text_payloads": h.journal.LogTextPayloads(), "logs": h.journal.Logs(id)})
}

func (h *memoryHandler) profiles(w http.ResponseWriter, r *http.Request, sid string) {
	switch r.Method {
	case http.MethodGet:
		writeMemory(w, h.store.ListProfiles(sid))
	case http.MethodPost:
		var payload struct {
			Name              string `json:"name"`
			Style             string `json:"style"`
			Constraints       string `json:"constraints"`
			AdditionalContext string `json:"additional_context"`
		}
		if !decode(w, r, &payload) {
			failMemory(w, http.StatusBadRequest, "validation")
			return
		}
		listing, err := h.store.CreateProfile(sid, payload.Name, payload.Style, payload.Constraints, payload.AdditionalContext)
		if err != nil {
			memoryError(w, err)
			return
		}
		writeMemory(w, listing)
	default:
		method(w, http.MethodGet, http.MethodPost)
	}
}

func (h *memoryHandler) selectProfile(w http.ResponseWriter, r *http.Request, sid, profileID string) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	listing, err := h.store.SelectProfile(sid, profileID)
	if err != nil {
		memoryError(w, err)
		return
	}
	writeMemory(w, listing)
}

func (h *memoryHandler) profile(w http.ResponseWriter, r *http.Request, sid, profileID string) {
	if r.Method != http.MethodDelete {
		method(w, http.MethodDelete)
		return
	}
	_, err := h.store.DeleteProfile(sid, profileID)
	if err != nil {
		memoryError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *memoryHandler) projects(w http.ResponseWriter, r *http.Request, sid string) {
	switch r.Method {
	case http.MethodGet:
		writeMemory(w, h.store.List(sid))
	case http.MethodPost:
		var p struct {
			Title string `json:"title"`
		}
		if !decode(w, r, &p) {
			failMemory(w, 400, "validation")
			return
		}
		if _, e := h.store.CreateProject(sid, p.Title); e != nil {
			memoryError(w, e)
			return
		}
		writeMemory(w, h.store.List(sid))
	default:
		method(w, http.MethodGet, http.MethodPost)
	}
}
func (h *memoryHandler) project(w http.ResponseWriter, r *http.Request, sid, pid string) {
	switch r.Method {
	case http.MethodGet:
		p, ok := h.store.GetProject(sid, pid)
		if !ok {
			failMemory(w, 404, "not_found")
			return
		}
		writeMemory(w, p)
	case http.MethodPatch:
		var payload struct {
			Title string `json:"title"`
		}
		if !decode(w, r, &payload) {
			failMemory(w, http.StatusBadRequest, "validation")
			return
		}
		p, err := h.store.RenameProject(sid, pid, payload.Title)
		if err != nil {
			memoryError(w, err)
			return
		}
		writeMemory(w, p)
	case http.MethodDelete:
		if e := h.store.DeleteProject(sid, pid); e != nil {
			memoryError(w, e)
			return
		}
		w.WriteHeader(204)
	default:
		method(w, http.MethodGet, http.MethodPatch, http.MethodDelete)
	}
}
func (h *memoryHandler) selectProject(w http.ResponseWriter, r *http.Request, sid, pid string) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	if e := h.store.SelectProject(sid, pid); e != nil {
		memoryError(w, e)
		return
	}
	w.WriteHeader(204)
}
func (h *memoryHandler) chats(w http.ResponseWriter, r *http.Request, sid, pid string) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	var p struct {
		Title string `json:"title"`
	}
	if !decode(w, r, &p) {
		failMemory(w, 400, "validation")
		return
	}
	c, e := h.store.CreateChat(sid, pid, p.Title)
	if e != nil {
		memoryError(w, e)
		return
	}
	writeMemory(w, c)
}
func (h *memoryHandler) chat(w http.ResponseWriter, r *http.Request, sid, pid, cid string) {
	switch r.Method {
	case http.MethodGet:
		c, ok := h.store.GetChat(sid, pid, cid)
		if !ok {
			failMemory(w, 404, "not_found")
			return
		}
		writeMemory(w, c)
	case http.MethodDelete:
		if e := h.store.DeleteChat(sid, pid, cid); e != nil {
			memoryError(w, e)
			return
		}
		w.WriteHeader(204)
	default:
		method(w, http.MethodGet, http.MethodDelete)
	}
}
func (h *memoryHandler) selectChat(w http.ResponseWriter, r *http.Request, sid, pid, cid string) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	if e := h.store.SelectChat(sid, pid, cid); e != nil {
		memoryError(w, e)
		return
	}
	w.WriteHeader(204)
}
func (h *memoryHandler) send(w http.ResponseWriter, r *http.Request, sid, pid, cid string) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	var p struct {
		ClientID string `json:"client_message_id"`
		Text     string `json:"text"`
	}
	if !decode(w, r, &p) {
		failMemory(w, 400, "validation")
		return
	}
	c, e := h.store.Send(r.Context(), sid, pid, cid, p.ClientID, p.Text)
	if e != nil {
		memoryError(w, e)
		return
	}
	writeMemory(w, c)
}

func (h *memoryHandler) taskInput(w http.ResponseWriter, r *http.Request, sid, pid, cid string) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	var payload struct {
		Text            string `json:"text"`
		CandidateTaskID string `json:"candidate_task_id"`
	}
	if !decode(w, r, &payload) {
		failMemory(w, http.StatusBadRequest, "validation")
		return
	}
	chat, candidates, err := h.store.TaskInput(r.Context(), sid, pid, cid, payload.Text, payload.CandidateTaskID)
	if err != nil {
		slog.Warn("barista.task", "source", "backend", "event", "task_input", "result", "failure", "error_category", taskErrorCategory(err), "correlation_id", llm.RequestID(r.Context()), "project_id", pid, "chat_id", cid)
		memoryError(w, err)
		return
	}
	event := "task_step"
	if len(candidates) > 1 {
		event = "task_candidates"
	}
	slog.Info("barista.task", "source", "backend", "event", event, "result", "success", "correlation_id", llm.RequestID(r.Context()), "project_id", pid, "chat_id", cid)
	writeMemory(w, map[string]any{"chat": chat, "candidates": candidates})
}

func (h *memoryHandler) pauseTask(w http.ResponseWriter, r *http.Request, sid, pid, cid, taskID string) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	chat, err := h.store.PauseTask(sid, pid, cid, taskID)
	if err != nil {
		memoryError(w, err)
		return
	}
	slog.Info("barista.task", "source", "backend", "event", "task_paused", "result", "success", "correlation_id", llm.RequestID(r.Context()), "project_id", pid, "chat_id", cid, "task_id", taskID)
	writeMemory(w, map[string]any{"chat": chat})
}

func (h *memoryHandler) resumeTask(w http.ResponseWriter, r *http.Request, sid, pid, cid, taskID string) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	var payload struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &payload) {
		failMemory(w, http.StatusBadRequest, "validation")
		return
	}
	if strings.TrimSpace(payload.Text) == "" {
		payload.Text = "Продолжить сохранённый шаг задачи."
	}
	chat, candidates, err := h.store.TaskInput(r.Context(), sid, pid, cid, payload.Text, taskID)
	if err != nil {
		memoryError(w, err)
		return
	}
	slog.Info("barista.task", "source", "backend", "event", "task_resumed", "result", "success", "correlation_id", llm.RequestID(r.Context()), "project_id", pid, "chat_id", cid, "task_id", taskID)
	writeMemory(w, map[string]any{"chat": chat, "candidates": candidates})
}

func taskErrorCategory(err error) string {
	if errors.Is(err, memory.ErrStorage) {
		return "storage"
	}
	if strings.Contains(err.Error(), "validation") {
		return "validation"
	}
	if strings.Contains(err.Error(), "not found") {
		return "not_found"
	}
	return "provider"
}
func (h *memoryHandler) memory(w http.ResponseWriter, r *http.Request, sid, pid string) {
	if r.Method != http.MethodGet {
		method(w, http.MethodGet)
		return
	}
	m, ok := h.store.ReadMemory(sid, pid)
	if !ok {
		failMemory(w, 404, "not_found")
		return
	}
	writeMemory(w, m)
}
func (h *memoryHandler) retry(w http.ResponseWriter, r *http.Request, sid, pid, cid, mid string) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	c, err := h.store.Retry(r.Context(), sid, pid, cid, mid)
	if err != nil {
		memoryError(w, err)
		return
	}
	writeMemory(w, c)
}
func (h *memoryHandler) clear(w http.ResponseWriter, r *http.Request, sid, pid, layer string) {
	if r.Method != http.MethodDelete {
		method(w, http.MethodDelete)
		return
	}
	var e error
	if layer == "global" {
		if _, ok := h.store.GetProject(sid, pid); !ok {
			failMemory(w, http.StatusNotFound, "not_found")
			return
		}
		e = h.store.ClearGlobal(sid)
	} else {
		e = h.store.ClearProject(sid, pid)
	}
	if e != nil {
		memoryError(w, e)
		return
	}
	w.WriteHeader(204)
}
func writeMemory(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
func memoryError(w http.ResponseWriter, e error) {
	if strings.Contains(e.Error(), "validation") {
		failMemory(w, http.StatusBadRequest, "validation")
		return
	}
	if errors.Is(e, memory.ErrStorage) {
		failMemory(w, 503, "storage")
		return
	}
	var attemptErr *agent.AttemptError
	if errors.As(e, &attemptErr) {
		status := http.StatusBadGateway
		if attemptErr.Category == agent.ErrorTimeout {
			status = http.StatusGatewayTimeout
		}
		failMemory(w, status, string(attemptErr.Category))
		return
	}
	if errors.Is(e, context.DeadlineExceeded) {
		failMemory(w, http.StatusGatewayTimeout, "timeout")
		return
	}
	if strings.Contains(e.Error(), "not found") {
		failMemory(w, 404, "not_found")
		return
	}
	if strings.Contains(e.Error(), "busy") {
		failMemory(w, 409, "busy")
		return
	}
	failMemory(w, 502, "provider")
}
func failMemory(w http.ResponseWriter, status int, cat string) {
	msg := "Не удалось выполнить операцию. Повторите попытку."
	if cat == "validation" {
		msg = "Некорректный запрос."
	}
	if cat == "not_found" {
		msg = "Данные не найдены."
	}
	if cat == "busy" {
		msg = "Запрос уже выполняется."
	}
	if cat == "storage" {
		msg = "Сервис временно недоступен."
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"category": cat, "message": msg}})
}
