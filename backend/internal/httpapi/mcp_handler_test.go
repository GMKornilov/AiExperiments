package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aichallenge/week_1/task_1/internal/mcpclient"
	"aichallenge/week_1/task_1/internal/observability"
)

type fakeMCPTools struct {
	tools []mcpclient.Tool
	err   error
	seen  string
}

func TestMCPCollectorAuthAndStrictSchema(t *testing.T) {
	t.Setenv("MCP_OBSERVABILITY_TOKEN", "test-token")
	journal := observability.NewJournal(false, nil)
	handler := NewMemory(UseCases{}, journal)
	valid := `{"timestamp":"2026-09-22T00:00:00Z","source":"mcp_server","event":"mcp_tools_call","operation":"mcp_tools_call","outcome":"success","correlation_id":"trace-1","duration_ms":1,"tool":"brewmark_list_grinders"}`
	for _, testCase := range []struct {
		name, auth, body string
		status           int
	}{
		{"missing auth", "", valid, http.StatusForbidden},
		{"wrong auth", "Bearer wrong", valid, http.StatusForbidden},
		{"extra field", "Bearer test-token", strings.TrimSuffix(valid, "}") + `,"payload":"secret"}`, http.StatusBadRequest},
		{"invalid source", "Bearer test-token", strings.Replace(valid, "mcp_server", "bad", 1), http.StatusBadRequest},
		{"valid", "Bearer test-token", valid, http.StatusNoContent},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/internal/observability/mcp", strings.NewReader(testCase.body))
			r.Header.Set("Content-Type", "application/json")
			if testCase.auth != "" {
				r.Header.Set("Authorization", testCase.auth)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != testCase.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	if len(journal.MCPLogs()) != 1 {
		t.Fatalf("logs=%+v", journal.MCPLogs())
	}
}

func (f *fakeMCPTools) ListTools(_ context.Context, correlationID string) ([]mcpclient.Tool, error) {
	f.seen = correlationID
	return f.tools, f.err
}

func TestMCPToolsReturnsNormalizedTools(t *testing.T) {
	client := &fakeMCPTools{tools: []mcpclient.Tool{{Name: "brewmark_list_filters", Description: "List BrewMark coffee filters."}}}
	handler := NewMemory(UseCases{MCPTools: client}, observability.NewJournal(false, nil))
	request := httptest.NewRequest(http.MethodPost, "/api/mcp/tools", nil)
	request.Header.Set("X-Request-ID", "0123456789abcdef0123456789abcdef")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Tools []mcpclient.Tool `json:"tools"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Tools) != 1 || payload.Tools[0] != client.tools[0] {
		t.Fatalf("payload=%+v", payload)
	}
	if client.seen != request.Header.Get("X-Request-ID") {
		t.Fatalf("correlation ID=%q", client.seen)
	}
}

func TestMCPToolsMapsSafeErrors(t *testing.T) {
	cases := []struct {
		name   string
		code   mcpclient.ErrorCode
		status int
	}{
		{name: "not configured", code: mcpclient.NotConfigured, status: http.StatusServiceUnavailable},
		{name: "unavailable", code: mcpclient.Unavailable, status: http.StatusServiceUnavailable},
		{name: "timeout", code: mcpclient.Timeout, status: http.StatusGatewayTimeout},
		{name: "protocol", code: mcpclient.ProtocolError, status: http.StatusBadGateway},
		{name: "invalid", code: mcpclient.InvalidResponse, status: http.StatusBadGateway},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			handler := NewMemory(UseCases{MCPTools: &fakeMCPTools{err: &mcpclient.Error{Code: testCase.code}}}, observability.NewJournal(false, nil))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/mcp/tools", nil))
			if response.Code != testCase.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var payload map[string]string
			if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["code"] != string(testCase.code) || payload["message"] == "" || len(payload) != 2 {
				t.Fatalf("payload=%v", payload)
			}
		})
	}
}

func TestMCPToolsRejectsMethodsWithoutCallingMCP(t *testing.T) {
	client := &fakeMCPTools{err: errors.New("must not be called")}
	handler := NewMemory(UseCases{MCPTools: client}, observability.NewJournal(false, nil))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/mcp/tools", nil))
	if response.Code != http.StatusMethodNotAllowed || client.seen != "" {
		t.Fatalf("status=%d seen=%q", response.Code, client.seen)
	}
}
