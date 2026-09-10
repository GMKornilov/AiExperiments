package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
	"aichallenge/week_1/task_1/internal/observability"
	"aichallenge/week_1/task_1/internal/session"
)

func TestHTTPDialogExposesUsageWithoutInternalLedger(t *testing.T) {
	usage := &llm.Usage{PromptTokens: 100, CompletionTokens: 20}
	store := &fakeStore{dialogs: map[string]session.Dialog{"own": {ID: "own", AccountedTokens: 120, Messages: []agent.Message{
		{ID: "m-1", Role: "user", Text: "coffee", Status: agent.StatusSuccess, Attempts: []agent.Attempt{{ID: "m-1-a-1", Usage: usage}}},
		{ID: "m-2", Role: "assistant", Text: "answer", Status: agent.StatusSuccess, Usage: usage},
	}}}}
	h := New(store, nil, observability.NewJournal(false, nil))
	request := httptest.NewRequest(http.MethodGet, "/api/dialogs/own", nil)
	request.Header.Set("X-Session-ID", "s")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("HTTP %d: %s", response.Code, response.Body)
	}
	var got struct {
		AccountedTokens *int64 `json:"accounted_tokens"`
		Messages        []struct {
			Usage *llm.Usage `json:"usage"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AccountedTokens == nil || *got.AccountedTokens != 120 || len(got.Messages) != 2 || got.Messages[1].Usage == nil || *got.Messages[1].Usage != *usage {
		t.Fatalf("missing token DTO fields: %s", response.Body)
	}
	if strings.Contains(response.Body.String(), `"attempts"`) {
		t.Fatal("internal ledger exposed")
	}
	// Empty dialogues must still expose a confirmed zero instead of omitting it.
	raw, err := json.Marshal(dialogDTO(session.Dialog{}))
	if err != nil || !strings.Contains(string(raw), `"accounted_tokens":0`) {
		t.Fatalf("new dialog %s %v", raw, err)
	}
}
