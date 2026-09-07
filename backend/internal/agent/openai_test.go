package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/llm"
)

func TestOpenAIProviderMapsInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{"))
	}))
	defer server.Close()
	_, err := (OpenAIProvider{}).Complete(context.Background(), Snapshot{BaseURL: server.URL, APIKey: "key", Model: "test", SystemPrompt: "system", Timeout: time.Second}, []llm.Message{{Role: "user", Content: "coffee"}})
	var typed *AttemptError
	if !errors.As(err, &typed) || typed.Category != ErrorInvalidResponse {
		t.Fatalf("error = %#v", err)
	}
}
