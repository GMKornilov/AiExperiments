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
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
	"aichallenge/week_1/task_1/internal/memory"
	"aichallenge/week_1/task_1/internal/observability"
)

type memoryProvider struct{ calls int }

func (p *memoryProvider) Complete(_ context.Context, _ agent.Snapshot, _ []llm.Message) (string, error) {
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
	return agent.DialogSnapshot{Chat: agent.Snapshot{BaseURL: "https://x", APIKey: "k", Model: "chat", SystemPrompt: "base", Timeout: time.Second, Temperature: 1}, Text: agent.Snapshot{BaseURL: "https://x", APIKey: "k", Model: "text", SystemPrompt: "text", Timeout: time.Second, Temperature: 1}, Memory: &agent.FactsConfig{Snapshot: agent.Snapshot{BaseURL: "https://x", APIKey: "k", Model: "memory", SystemPrompt: "memory", Timeout: time.Second, Temperature: 0}}, ContextWindowMessages: 3}
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
func TestMemoryHandlerPersistsFailedUserAndRetries(t *testing.T) {
	p := &memoryProvider{}
	s, err := memory.New(p, memorySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	h := NewMemory(s, observability.NewJournal(false, nil))
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
	s, err := memory.New(&memoryProvider{}, memorySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	h := NewMemory(s, observability.NewJournal(false, nil))
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
	s, err := memory.New(&memoryProvider{}, memorySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	h := NewMemory(s, observability.NewJournal(false, nil))
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
	s, err := memory.New(&memoryProvider{}, memorySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	h := NewMemory(s, observability.NewJournal(false, nil))
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
	s, err := memory.New(agent.OpenAIProvider{}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	journal := observability.NewJournal(false, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	h := NewMemory(s, journal)
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
