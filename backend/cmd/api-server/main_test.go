package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/algorithms"
	"aichallenge/week_1/task_1/internal/config"
)

func TestNewHandlerUsesAlgorithmTimeoutIndependently(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(30 * time.Millisecond)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"answer"}}]}`))
	}))
	defer provider.Close()

	prompts, err := algorithms.NewPrompts(algorithms.PromptSources{
		DirectSystem:               "direct {{.Language}} {{.InterfaceRule}}",
		StepByStepSystem:           "steps {{.Language}}",
		ExpertsSystem:              "experts {{.Language}}",
		MetaPromptGenerationSystem: "generate {{.Language}} {{.InterfaceRule}}",
		MetaSolutionSystem:         "solution {{.Language}}",
		MetaSolutionUser:           "{{.Statement}} {{.GeneratedPrompt}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(config.Config{
		BaseURL:                 provider.URL,
		APIKey:                  "test-key",
		Model:                   "test-model",
		RequestTimeout:          10 * time.Millisecond,
		AlgorithmRequestTimeout: 100 * time.Millisecond,
		AlgorithmPrompts:        prompts,
	})
	request := httptest.NewRequest(http.MethodPost, "/api/algorithms/direct", strings.NewReader(`{"statement":"condition","language":"python"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}
