package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/observability"
	"aichallenge/week_1/task_1/internal/session"
)

type fakeStore struct{ dialogs map[string]session.Dialog }

func (s *fakeStore) Create(_ string, _ agent.Snapshot) (session.Dialog, error) {
	return session.Dialog{}, nil
}
func (s *fakeStore) List(_ string) session.Listing {
	return session.Listing{Dialogs: []session.Dialog{}}
}
func (s *fakeStore) Get(_ string, id string) (session.Dialog, bool) {
	d, ok := s.dialogs[id]
	return d, ok
}
func (s *fakeStore) Exists(id string) bool      { _, ok := s.dialogs[id]; return ok }
func (s *fakeStore) Select(string, string) bool { return false }
func (s *fakeStore) Delete(string, string) bool { return false }
func (s *fakeStore) Send(context.Context, string, string, string, string) (session.Dialog, error) {
	return session.Dialog{}, nil
}
func (s *fakeStore) Retry(context.Context, string, string, string) (session.Dialog, error) {
	return session.Dialog{}, nil
}

func TestEventsRejectForeignDialog(t *testing.T) {
	store := &fakeStore{dialogs: map[string]session.Dialog{"own": {ID: "own"}}}
	h := New(store, func() (agent.Snapshot, error) { return agent.Snapshot{}, nil }, observability.NewJournal(false, nil))
	r := httptest.NewRequest(http.MethodPost, "/api/events", strings.NewReader(`{"event":"message_copied","dialog_id":"other"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Session-ID", "s")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUnknownStaysUnknown(t *testing.T) {
	store := &fakeStore{dialogs: map[string]session.Dialog{}}
	h := New(store, func() (agent.Snapshot, error) { return agent.Snapshot{}, nil }, observability.NewJournal(false, nil))
	for range 2 {
		r := httptest.NewRequest(http.MethodGet, "/api/admin/logs?dialog_id=missing&action=lookup", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if strings.Contains(w.Body.String(), `"found":true`) {
			t.Fatal(w.Body.String())
		}
	}
}

func TestInvalidOwnedMessageIsLoggedForDialog(t *testing.T) {
	store := &fakeStore{dialogs: map[string]session.Dialog{"owned": {ID: "owned"}}}
	journal := observability.NewJournal(false, nil)
	h := New(store, func() (agent.Snapshot, error) { return agent.Snapshot{}, nil }, journal)
	r := httptest.NewRequest(http.MethodPost, "/api/dialogs/owned/messages", strings.NewReader(`{"client_message_id":"id","text":" "}`))
	r.Header.Set("X-Session-ID", "s")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", w.Code)
	}
	var found bool
	for _, record := range journal.Logs("owned") {
		if record.Event == "http_request" && record.Result == "failure" && record.ErrorCategory == "validation" {
			found = true
		}
	}
	if !found {
		t.Fatalf("logs=%#v", journal.Logs("owned"))
	}
}
