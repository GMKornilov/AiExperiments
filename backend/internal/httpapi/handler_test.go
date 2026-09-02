package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aichallenge/week_1/task_1/internal/barista"
)

type fakeChatService struct {
	calls  int
	answer string
	err    error
	mode   barista.Mode
	prompt string
}

func (s *fakeChatService) Chat(_ context.Context, mode barista.Mode, prompt string) (string, error) {
	s.calls++
	s.mode = mode
	s.prompt = prompt
	return s.answer, s.err
}

func TestHandlerServesHealthAndMethodRoutes(t *testing.T) {
	handler := NewHandler(&fakeChatService{})
	tests := []struct {
		name        string
		method      string
		target      string
		wantStatus  int
		contentType string
	}{
		{name: "health", method: http.MethodGet, target: "/healthz", wantStatus: http.StatusOK, contentType: "application/json"},
		{name: "unknown path", method: http.MethodGet, target: "/unknown", wantStatus: http.StatusNotFound},
		{name: "health wrong method", method: http.MethodPost, target: "/healthz", wantStatus: http.StatusMethodNotAllowed},
		{name: "API wrong method", method: http.MethodGet, target: "/api/chat", wantStatus: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(test.method, test.target, nil))
			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if test.contentType != "" && !strings.Contains(response.Header().Get("Content-Type"), test.contentType) {
				t.Errorf("Content-Type = %q, want %q", response.Header().Get("Content-Type"), test.contentType)
			}
		})
	}
}

func TestChatHandlerSuccessResponses(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		answer      string
		wantMode    barista.Mode
		wantDataNil bool
	}{
		{name: "free", payload: "{\"mode\":\"free\",\"prompt\":\"кофе\"}", answer: "Свободный ответ", wantMode: barista.ModeFree, wantDataNil: true},
		{name: "controlled", payload: "{\"mode\":\"controlled\",\"prompt\":\"кофе\"}", answer: "{\"summary\":\"ok\"}", wantMode: barista.ModeControlled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeChatService{answer: test.answer}
			response := postChat(NewHandler(service), test.payload)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
				t.Errorf("Content-Type = %q, want JSON", response.Header().Get("Content-Type"))
			}
			var body struct {
				Mode barista.Mode    "json:\"mode\""
				Raw  string          "json:\"raw\""
				Data json.RawMessage "json:\"data\""
			}
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if service.calls != 1 || service.mode != test.wantMode || service.prompt != "кофе" {
				t.Errorf("service calls/mode/prompt = %d/%q/%q", service.calls, service.mode, service.prompt)
			}
			if body.Raw != test.answer {
				t.Errorf("raw = %q, want %q", body.Raw, test.answer)
			}
			if test.wantDataNil {
				if string(body.Data) != "null" {
					t.Errorf("data = %s, want null", body.Data)
				}
			} else if string(body.Data) != "{\"summary\":\"ok\"}" {
				t.Errorf("data = %s, want parsed controlled JSON", body.Data)
			}
		})
	}
}

func TestChatHandlerRejectsInvalidRequestWithoutUpstreamCall(t *testing.T) {
	tooLong := strings.Repeat("я", maxPromptRunes+1)
	tooLarge := strings.Repeat("x", maxRequestBody+1)
	tests := []struct {
		name    string
		payload string
	}{
		{name: "invalid mode", payload: "{\"mode\":\"other\",\"prompt\":\"кофе\"}"},
		{name: "empty prompt", payload: "{\"mode\":\"free\",\"prompt\":\"\"}"},
		{name: "whitespace prompt", payload: "{\"mode\":\"free\",\"prompt\":\" \\t \"}"},
		{name: "prompt too long", payload: "{\"mode\":\"free\",\"prompt\":\"" + tooLong + "\"}"},
		{name: "unknown field", payload: "{\"mode\":\"free\",\"prompt\":\"кофе\",\"other\":true}"},
		{name: "trailing JSON", payload: "{\"mode\":\"free\",\"prompt\":\"кофе\"} {}"},
		{name: "malformed JSON", payload: "{\"mode\":\"free\",\"prompt\":"},
		{name: "body too large", payload: "{\"mode\":\"free\",\"prompt\":\"" + tooLarge + "\"}"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeChatService{answer: "unexpected"}
			response := postChat(NewHandler(service), test.payload)
			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body = %s", response.Code, response.Body.String())
			}
			if service.calls != 0 {
				t.Errorf("service calls = %d, want 0", service.calls)
			}
		})
	}
}

func TestChatHandlerReturnsBadGatewayForServiceFailures(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		err    error
	}{
		{name: "upstream error", err: errors.New("upstream unavailable")},
		{name: "invalid controlled output", answer: "not JSON"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeChatService{answer: test.answer, err: test.err}
			response := postChat(NewHandler(service), "{\"mode\":\"controlled\",\"prompt\":\"кофе\"}")
			if response.Code != http.StatusBadGateway {
				t.Errorf("status = %d, want 502", response.Code)
			}
			if !strings.Contains(response.Header().Get("Content-Type"), "application/json") {
				t.Errorf("Content-Type = %q, want JSON", response.Header().Get("Content-Type"))
			}
			if strings.Contains(response.Body.String(), "test-key") {
				t.Errorf("error response leaks API key: %s", response.Body.String())
			}
		})
	}
}

func postChat(handler http.Handler, payload string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
