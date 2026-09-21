package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/adapters/extractjson"
	"aichallenge/week_1/task_1/internal/adapters/openai"
	"aichallenge/week_1/task_1/internal/adapters/statejson"
	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/application/conversation"
	"aichallenge/week_1/task_1/internal/application/invariant"
	"aichallenge/week_1/task_1/internal/application/state"
	"aichallenge/week_1/task_1/internal/application/taskflow"
	"aichallenge/week_1/task_1/internal/application/workspace"
	"aichallenge/week_1/task_1/internal/domain/model"
	"aichallenge/week_1/task_1/internal/llm"
	"aichallenge/week_1/task_1/internal/observability"
)

type memoryProvider struct {
	mu    sync.Mutex
	calls int
}

func TestInvariantMetadataAPIIsPublicAndReadOnly(t *testing.T) {
	h := NewMemory(UseCases{Invariants: []invariant.Metadata{{ID: "equipment-availability", Name: "Доступность оборудования", Description: "Описание"}}}, observability.NewJournal(false, nil))
	status, body := callMemory(t, h, http.MethodGet, "/api/invariants", "")
	if status != http.StatusOK {
		t.Fatalf("GET status=%d body=%v", status, body)
	}
	items, ok := body["invariants"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("contract=%v", body)
	}
	item := items[0].(map[string]any)
	if item["id"] != "equipment-availability" || item["name"] != "Доступность оборудования" || item["description"] != "Описание" || len(item) != 3 {
		t.Fatalf("unsafe or incomplete metadata=%v", item)
	}
	status, _ = callMemory(t, h, http.MethodPost, "/api/invariants", "")
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", status)
	}
}

func (p *memoryProvider) Complete(_ context.Context, snap agent.Snapshot, _ []llm.Message) (string, error) {
	if snap.Model == "text" {
		return "Title", nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls == 1 {
		return "", &agent.AttemptError{Category: agent.ErrorTimeout, Err: errors.New("timeout")}
	}
	if p.calls == 2 {
		return "answer", nil
	}
	return `{"global_facts":[],"project_facts":[]}`, nil
}
func memorySnapshot() agent.DialogSnapshot {
	return agent.DialogSnapshot{Chat: agent.Snapshot{BaseURL: "https://x", APIKey: "k", Model: "chat", SystemPrompt: "base", Timeout: time.Second, Temperature: 1}, Text: agent.Snapshot{BaseURL: "https://x", APIKey: "k", Model: "text", SystemPrompt: "text", Timeout: time.Second, Temperature: 1}, Memory: &agent.FactsConfig{Snapshot: agent.Snapshot{BaseURL: "https://x", APIKey: "k", Model: "memory", SystemPrompt: "memory", Timeout: time.Second, Temperature: 0}}, InvariantValidation: &agent.FactsConfig{Snapshot: agent.Snapshot{BaseURL: "https://x", APIKey: "k", Model: "validator", SystemPrompt: "validator", Timeout: time.Second, Temperature: 0}}, ContextWindowMessages: 3}
}
func callMemory(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Session-ID", "s")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestMemoryStatusWriterKeepsExactErrorCategory(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := &memoryStatusWriter{ResponseWriter: recorder}
	failMemory(writer, http.StatusBadGateway, "invalid_response")
	if writer.status != http.StatusBadGateway || writer.errorCategory != "invalid_response" {
		t.Fatalf("status=%d category=%q", writer.status, writer.errorCategory)
	}
}

func TestMemoryHandlerPersistsFailedUserAndRetries(t *testing.T) {
	p := &memoryProvider{}
	s, err := newActiveTestStore(t, p, memorySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	h := NewMemory(UseCases{Projects: s.Service, Chats: s.Service, Profiles: s.Service, Memory: s.Service, Conversations: s.conversation, Tasks: s.tasks, Health: s.state}, observability.NewJournal(false, nil))
	_, listing := callMemory(t, h, http.MethodPost, "/api/projects", `{}`)
	pid := listing["selected_project_id"].(string)
	_, chat := callMemory(t, h, http.MethodPost, "/api/projects/"+pid+"/chats", `{}`)
	cid := chat["id"].(string)
	status, out := callMemory(t, h, http.MethodPost, "/api/projects/"+pid+"/chats/"+cid+"/messages", `{"client_message_id":"one","text":"beans"}`)
	if status != http.StatusGatewayTimeout || out["error"].(map[string]any)["category"] != "timeout" {
		t.Fatalf("status=%d body=%v", status, out)
	}
	stored, ok := s.GetChat("s", pid, cid)
	if !ok || len(stored.Messages) != 1 || stored.Messages[0].Status != "error" {
		t.Fatalf("chat=%+v", stored)
	}
	status, _ = callMemory(t, h, http.MethodPost, "/api/projects/"+pid+"/chats/"+cid+"/messages/"+stored.Messages[0].ID+"/retry", ``)
	if status != http.StatusOK {
		t.Fatalf("retry status=%d", status)
	}
	stored, _ = s.GetChat("s", pid, cid)
	if len(stored.Messages) != 2 || stored.Messages[1].Text != "answer" {
		t.Fatalf("chat=%+v", stored)
	}
}

func TestMemoryHandlerFreshListOmitsEmptySelections(t *testing.T) {
	s, err := newActiveTestStore(t, &memoryProvider{}, memorySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	h := NewMemory(UseCases{Projects: s.Service, Chats: s.Service, Profiles: s.Service, Memory: s.Service, Conversations: s.conversation, Tasks: s.tasks, Health: s.state}, observability.NewJournal(false, nil))
	status, result := callMemory(t, h, http.MethodGet, "/api/projects", "")
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if _, exists := result["selected_project_id"]; exists {
		t.Fatalf("empty project ID must be omitted: %v", result)
	}
	if _, exists := result["selected_chat_id"]; exists {
		t.Fatalf("empty chat ID must be omitted: %v", result)
	}
}

func TestMemoryHandlerRenamesProject(t *testing.T) {
	s, err := newActiveTestStore(t, &memoryProvider{}, memorySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	h := NewMemory(UseCases{Projects: s.Service, Chats: s.Service, Profiles: s.Service, Memory: s.Service, Conversations: s.conversation, Tasks: s.tasks, Health: s.state}, observability.NewJournal(false, nil))
	_, listing := callMemory(t, h, http.MethodPost, "/api/projects", `{}`)
	pid := listing["selected_project_id"].(string)
	status, project := callMemory(t, h, http.MethodPatch, "/api/projects/"+pid, `{"title":"  Эспрессо  "}`)
	if status != http.StatusOK || project["title"] != "Эспрессо" {
		t.Fatalf("status=%d project=%v", status, project)
	}
	status, _ = callMemory(t, h, http.MethodPatch, "/api/projects/"+pid, `{"title":"   "}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d", status)
	}
}

func TestProfileHandlerCRUDValidationAndIsolation(t *testing.T) {
	s, err := newActiveTestStore(t, &memoryProvider{}, memorySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	h := NewMemory(UseCases{Projects: s.Service, Chats: s.Service, Profiles: s.Service, Memory: s.Service, Conversations: s.conversation, Tasks: s.tasks, Health: s.state}, observability.NewJournal(false, nil))
	status, initial := callMemory(t, h, http.MethodGet, "/api/profiles", "")
	if status != http.StatusOK || initial["active_profile_id"] != "barista" || len(initial["profiles"].([]any)) != 2 {
		t.Fatalf("initial status=%d body=%v", status, initial)
	}
	status, unchanged := callMemory(t, h, http.MethodPost, "/api/profiles", `{"name":"custom","style":"","constraints":"x","additional_context":"x"}`)
	if status != http.StatusBadRequest || unchanged["error"].(map[string]any)["category"] != "validation" {
		t.Fatalf("validation status=%d body=%v", status, unchanged)
	}
	status, created := callMemory(t, h, http.MethodPost, "/api/profiles", `{"name":" custom ","style":"short","constraints":"safe","additional_context":"home"}`)
	if status != http.StatusOK || len(created["profiles"].([]any)) != 3 {
		t.Fatalf("created status=%d body=%v", status, created)
	}
	customID := created["active_profile_id"].(string)
	status, duplicate := callMemory(t, h, http.MethodPost, "/api/profiles", `{"name":"CUSTOM","style":"short","constraints":"safe","additional_context":"home"}`)
	if status != http.StatusBadRequest || duplicate["error"].(map[string]any)["category"] != "validation" {
		t.Fatalf("duplicate status=%d body=%v", status, duplicate)
	}
	status, selected := callMemory(t, h, http.MethodPost, "/api/profiles/coffee-equipment/select", "")
	if status != http.StatusOK || selected["active_profile_id"] != "coffee-equipment" {
		t.Fatalf("select status=%d body=%v", status, selected)
	}
	status, _ = callMemory(t, h, http.MethodDelete, "/api/profiles/barista", "")
	if status != http.StatusNotFound {
		t.Fatalf("built-in delete status=%d", status)
	}
	status, _ = callMemory(t, h, http.MethodDelete, "/api/profiles/"+customID, "")
	if status != http.StatusNoContent {
		t.Fatalf("delete status=%d", status)
	}
	otherRequest := httptest.NewRequest(http.MethodGet, "/api/profiles", nil)
	otherRequest.Header.Set("X-Session-ID", "other")
	otherResponse := httptest.NewRecorder()
	h.ServeHTTP(otherResponse, otherRequest)
	var other map[string]any
	if err := json.Unmarshal(otherResponse.Body.Bytes(), &other); err != nil {
		t.Fatal(err)
	}
	if len(other["profiles"].([]any)) != 2 || other["active_profile_id"] != "barista" {
		t.Fatalf("other=%v", other)
	}
}

func TestMemoryAdminFindsChatByIDWithoutSessionAndReturnsJournal(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"answer"}}]}`)
	}))
	defer upstream.Close()
	snapshot := memorySnapshot()
	snapshot.Chat.BaseURL = upstream.URL
	snapshot.Text.BaseURL = upstream.URL
	snapshot.Memory.Snapshot.BaseURL = upstream.URL
	s, err := newActiveTestStore(t, agent.OpenAIProvider{}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	journal := observability.NewJournal(false, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	h := NewMemory(UseCases{Projects: s.Service, Chats: s.Service, Profiles: s.Service, Memory: s.Service, Conversations: s.conversation, Tasks: s.tasks, Health: s.state}, journal)
	_, listing := callMemory(t, h, http.MethodPost, "/api/projects", `{}`)
	pid := listing["selected_project_id"].(string)
	_, chat := callMemory(t, h, http.MethodPost, "/api/projects/"+pid+"/chats", `{}`)
	cid := chat["id"].(string)
	status, _ := callMemory(t, h, http.MethodPost, "/api/projects/"+pid+"/chats/"+cid+"/messages", `{"client_message_id":"one","text":"beans"}`)
	if status != http.StatusOK {
		t.Fatalf("send status=%d", status)
	}

	lookup := httptest.NewRequest(http.MethodGet, "/api/admin/logs?dialog_id="+cid+"&action=lookup", nil)
	lookupResponse := httptest.NewRecorder()
	h.ServeHTTP(lookupResponse, lookup)
	if lookupResponse.Code != http.StatusOK {
		t.Fatalf("lookup status=%d body=%s", lookupResponse.Code, lookupResponse.Body.String())
	}
	var found struct {
		Found bool                   `json:"found"`
		Logs  []observability.Record `json:"logs"`
	}
	if err := json.Unmarshal(lookupResponse.Body.Bytes(), &found); err != nil {
		t.Fatal(err)
	}
	if !found.Found || len(found.Logs) == 0 {
		t.Fatalf("found=%v logs=%+v", found.Found, found.Logs)
	}
	callID := ""
	for _, record := range found.Logs {
		if record.Event == "llm_request" && record.CallID != "" {
			callID = record.CallID
			break
		}
	}
	if callID == "" {
		t.Fatalf("LLM trace with call_id was not recorded: %+v", found.Logs)
	}

	unknown := httptest.NewRequest(http.MethodGet, "/api/admin/logs?dialog_id=missing&action=lookup", nil)
	unknownResponse := httptest.NewRecorder()
	h.ServeHTTP(unknownResponse, unknown)
	var missing struct {
		Found bool `json:"found"`
	}
	if err := json.Unmarshal(unknownResponse.Body.Bytes(), &missing); err != nil {
		t.Fatal(err)
	}
	if unknownResponse.Code != http.StatusOK || missing.Found {
		t.Fatalf("unknown status=%d found=%v", unknownResponse.Code, missing.Found)
	}
}

type activeTestStore struct {
	*workspace.Service
	conversation *conversation.Service
	tasks        *taskflow.Service
	state        *state.Manager
}

func newActiveTestStore(t *testing.T, provider agent.Provider, snap agent.DialogSnapshot) (*activeTestStore, error) {
	t.Helper()
	client, err := openai.New(provider, snap)
	if err != nil {
		return nil, err
	}
	disk, initial, err := statejson.Open("")
	if err != nil {
		return nil, err
	}
	manager := state.New(initial, disk)
	id := func() string { return llm.RequestID(llm.WithRequestID(context.Background(), "")) }
	titles := conversation.NewTitles(manager, client, snap.Text.SystemPrompt, time.Now)
	t.Cleanup(func() { manager.Close(); titles.Close() })
	extractor := extractjson.NewExtractor(client, snap.Memory.Snapshot.SystemPrompt)
	settings := conversation.Settings{Prompt: snap.Chat.SystemPrompt, Window: snap.ContextWindowMessages}
	allow := invariant.Set{}
	return &activeTestStore{Service: workspace.New(manager, manager, id, time.Now), conversation: conversation.New(manager, client, extractor, titles, settings, id, time.Now, allow), tasks: taskflow.New(manager, client, extractor, extractjson.ProposalDecoder{}, model.TaskRouter{}, titles, settings, id, time.Now, allow), state: manager}, nil
}

func TestActiveContractErrorsAreSafe(t *testing.T) {
	s, e := newActiveTestStore(t, &memoryProvider{}, memorySnapshot())
	if e != nil {
		t.Fatal(e)
	}
	h := NewMemory(UseCases{Projects: s.Service, Chats: s.Service, Profiles: s.Service, Memory: s.Service, Conversations: s.conversation, Tasks: s.tasks, Health: s.state}, observability.NewJournal(false, nil))
	for _, tc := range []struct {
		method, path, body string
		status             int
		category           string
	}{{"GET", "/api/projects/missing", "", 404, "not_found"}, {"POST", "/api/projects", "{", 400, "validation"}, {"POST", "/api/projects", `{"secret":"do-not-echo"}`, 400, "validation"}, {"PATCH", "/api/profiles/barista", "{}", 405, ""}, {"DELETE", "/api/profiles/barista", "", 404, "not_found"}, {"GET", "/api/dialogs", "", 404, ""}} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			status, body := callMemory(t, h, tc.method, tc.path, tc.body)
			if status != tc.status {
				t.Fatalf("status %d", status)
			}
			if tc.category != "" {
				out := body["error"].(map[string]any)
				if out["category"] != tc.category {
					t.Fatalf("error %v", out)
				}
			}
		})
	}
}
