package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"aichallenge/week_1/task_1/internal/algorithms"
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

type fakeAlgorithmService struct {
	calls   int
	method  algorithms.Method
	request algorithms.Request
	result  algorithms.Result
	err     error
}

func (s *fakeAlgorithmService) Solve(_ context.Context, method algorithms.Method, request algorithms.Request) (algorithms.Result, error) {
	s.calls++
	s.method = method
	s.request = request
	return s.result, s.err
}

func TestAlgorithmHandlerRoutesRawSnapshotAndSuccessfulEnvelope(t *testing.T) {
	statement := "  условие\\nОграничение: n ≤ 10\\nПример: 1 -> 2  "
	tests := []struct {
		path   string
		method algorithms.Method
	}{
		{"/api/algorithms/direct", algorithms.MethodDirect},
		{"/api/algorithms/step-by-step", algorithms.MethodStepByStep},
		{"/api/algorithms/generated-prompt", algorithms.MethodGeneratedPrompt},
		{"/api/algorithms/experts", algorithms.MethodExperts},
	}
	for _, test := range tests {
		t.Run(string(test.method), func(t *testing.T) {
			service := &fakeAlgorithmService{result: algorithms.Result{
				Method: test.method, Answer: "неполный Markdown без кода", Trace: []algorithms.TraceStep{{
					Step: "solution", Messages: []algorithms.Message{{Role: "system", Content: "exact prompt"}},
				}},
			}}
			response := postAlgorithm(NewHandler(&fakeChatService{}, service), test.path, `{"statement":`+strconvQuote(statement)+`,"language":"cpp"}`, "application/json")
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
			}
			if service.calls != 1 || service.method != test.method || service.request != (algorithms.Request{Statement: statement, Language: algorithms.LanguageCPP}) {
				t.Errorf("service call = %#v, method=%q, request=%#v", service.calls, service.method, service.request)
			}
			var body algorithmResponse
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Status != "success" || body.Answer != "неполный Markdown без кода" || body.Error != nil || !reflect.DeepEqual(body.Trace, service.result.Trace) {
				t.Errorf("response body = %#v", body)
			}
		})
	}
}

func TestAlgorithmHandlerRejectsInvalidRequestWithoutServiceCall(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		contentType string
		wantStatus  int
	}{
		{"non JSON", "text", "text/plain", http.StatusUnsupportedMediaType},
		{"empty", `{"statement":" ","language":"python"}`, "application/json", http.StatusBadRequest},
		{"unknown language", `{"statement":"condition","language":"go"}`, "application/json", http.StatusBadRequest},
		{"unknown field", `{"statement":"condition","language":"python","extra":true}`, "application/json", http.StatusBadRequest},
		{"too many code points", `{"statement":"` + strings.Repeat("я", maxStatementRunes+1) + `","language":"python"}`, "application/json", http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeAlgorithmService{}
			response := postAlgorithm(NewHandler(&fakeChatService{}, service), "/api/algorithms/direct", test.payload, test.contentType)
			if response.Code != test.wantStatus || service.calls != 0 {
				t.Errorf("status/calls = %d/%d, want %d/0", response.Code, service.calls, test.wantStatus)
			}
			var body algorithmResponse
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Status != "error" || body.Error == nil || body.Error.Code != algorithms.ErrorInvalidRequest || body.Answer != "" || len(body.Trace) != 0 {
				t.Errorf("error envelope = %#v", body)
			}
		})
	}
}

func TestAlgorithmHandlerKeepsPartialTraceAndHidesProviderDetails(t *testing.T) {
	secret := "Bearer private-test-secret https://llm.private.example"
	trace := []algorithms.TraceStep{{Step: "generate-prompt", Messages: []algorithms.Message{{Role: "system", Content: "first"}}, Response: stringPointer("raw generated prompt")}, {Step: "solution", Messages: []algorithms.Message{{Role: "user", Content: "second"}}}}
	service := &fakeAlgorithmService{result: algorithms.Result{Method: algorithms.MethodGeneratedPrompt, Trace: trace}, err: algorithms.NewError(algorithms.ErrorTimeout, errors.New(secret))}
	response := postAlgorithm(NewHandler(&fakeChatService{}, service), "/api/algorithms/generated-prompt", `{"statement":"condition","language":"java"}`, "application/json")
	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), secret) {
		t.Errorf("response leaks provider detail: %s", response.Body.String())
	}
	var body algorithmResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error == nil || body.Error.Code != algorithms.ErrorTimeout || !reflect.DeepEqual(body.Trace, trace) {
		t.Errorf("partial failure envelope = %#v", body)
	}
}

func postAlgorithm(handler http.Handler, path, payload, contentType string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func strconvQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func stringPointer(value string) *string { return &value }
