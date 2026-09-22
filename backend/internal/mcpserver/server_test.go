package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/brewmark"
)

type observabilityEvent struct {
	Timestamp     time.Time `json:"timestamp"`
	Source        string    `json:"source"`
	Event         string    `json:"event"`
	Operation     string    `json:"operation"`
	Outcome       string    `json:"outcome"`
	CorrelationID string    `json:"correlation_id"`
	DurationMS    int64     `json:"duration_ms"`
	HTTPStatus    int       `json:"http_status"`
	ErrorCategory string    `json:"error_category"`
	Tool          string    `json:"tool"`
}

func TestObservabilityContractEndToEnd(t *testing.T) {
	const (
		collectorToken = "collector-secret"
		brewmarkToken  = "brewmark-api-token"
		filterValue    = "private-filter"
		correlationID  = "mcp-observability-trace-1"
	)

	var collector struct {
		sync.Mutex
		events []observabilityEvent
		raw    [][]byte
	}
	collectorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+collectorToken {
			t.Errorf("collector request: method=%s authorization=%q", r.Method, r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusForbidden)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		var event observabilityEvent
		if err := decoder.Decode(&event); err != nil {
			t.Errorf("strict collector decode: %v; body=%s", err, body)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			t.Errorf("collector body has trailing JSON: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		collector.Lock()
		collector.events = append(collector.events, event)
		collector.raw = append(collector.raw, append([]byte(nil), body...))
		collector.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer collectorServer.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/grinders" || r.URL.Query().Get("brand") != filterValue {
			t.Errorf("unexpected BrewMark request: %s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer "+brewmarkToken {
			t.Errorf("BrewMark authorization=%q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"grinders":[{"id":1,"brand":"Hario","model":"M","minGrindIndex":1,"maxGrindIndex":2,"clicksPerFullRange":3}]}`))
	}))
	defer upstream.Close()

	client, err := brewmark.NewClient(brewmark.Config{
		BaseURL: upstream.URL,
		Token:   brewmarkToken,
		Timeout: time.Second,
		Observer: func(ctx context.Context, outcome, category string, status int, duration time.Duration) {
			payload, marshalErr := json.Marshal(observabilityEvent{
				Timestamp:     time.Now().UTC(),
				Source:        "brewmark",
				Event:         "brewmark_request",
				Operation:     "brewmark_request",
				Outcome:       outcome,
				CorrelationID: brewmark.CorrelationID(ctx),
				DurationMS:    duration.Milliseconds(),
				HTTPStatus:    status,
				ErrorCategory: category,
			})
			if marshalErr != nil {
				t.Errorf("marshal BrewMark event: %v", marshalErr)
				return
			}
			req, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, collectorServer.URL, bytes.NewReader(payload))
			if requestErr != nil {
				t.Errorf("create collector request: %v", requestErr)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+collectorToken)
			response, requestErr := http.DefaultClient.Do(req)
			if requestErr != nil {
				t.Errorf("send BrewMark event: %v", requestErr)
				return
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				t.Errorf("collector status=%d", response.StatusCode)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(client, Config{ObservabilityURL: collectorServer.URL, ObservabilityToken: collectorToken}).Handler())
	defer server.Close()

	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	_ = rpcWithRequestID(t, server.URL, initialize, correlationID)
	_ = rpcWithRequestID(t, server.URL, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, correlationID)
	call := rpcWithRequestID(t, server.URL, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"brewmark_list_grinders","arguments":{"brand":"private-filter"}}}`, correlationID)
	if call["result"].(map[string]any)["isError"] == true {
		t.Fatalf("tool call failed: %#v", call)
	}

	collector.Lock()
	events := append([]observabilityEvent(nil), collector.events...)
	raw := append([][]byte(nil), collector.raw...)
	collector.Unlock()
	for _, wanted := range []struct{ source, event string }{
		{"mcp_server", "mcp_initialize"},
		{"mcp_server", "mcp_tools_list"},
		{"mcp_server", "mcp_tools_call"},
		{"brewmark", "brewmark_request"},
	} {
		found := false
		for _, event := range events {
			if event.Source == wanted.source && event.Event == wanted.event {
				found = true
				if event.CorrelationID != correlationID || event.Operation != wanted.event || event.Outcome != "success" {
					t.Fatalf("event=%+v", event)
				}
			}
		}
		if !found {
			t.Fatalf("missing %s:%s in %+v", wanted.source, wanted.event, events)
		}
	}
	for _, body := range raw {
		if strings.Contains(string(body), filterValue) || strings.Contains(string(body), brewmarkToken) || strings.Contains(string(body), collectorToken) || strings.Contains(string(body), upstream.URL) {
			t.Fatalf("unsafe observability payload: %s", body)
		}
	}
	for index, event := range events {
		if event.Source != "mcp_server" || event.Event != "mcp_initialize" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(raw[index], &payload); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"http_status", "error_category", "tool"} {
			if _, found := payload[field]; found {
				t.Fatalf("initialize payload unexpectedly contains %q: %s", field, raw[index])
			}
		}
	}
}

func TestHTTPContract(t *testing.T) {
	t.Parallel()
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		upstreamCalls++
		switch request.URL.Path {
		case "/api/grinders":
			_, _ = w.Write([]byte(`{"grinders":[{"id":1,"brand":"Hario","model":"M","minGrindIndex":1,"maxGrindIndex":2,"clicksPerFullRange":3}]}`))
		case "/api/machines":
			_, _ = w.Write([]byte(`{"machines":[{"id":1,"brand":"Hario","model":"V60","brewMethod":"V60","defaultWaterTempF":200}]}`))
		case "/api/filters":
			_, _ = w.Write([]byte(`{"filters":[{"id":1,"name":"Paper","type":"paper","description":""}]}`))
		case "/api/brew-methods":
			_, _ = w.Write([]byte(`{"data":[{"name":"V60"}]}`))
		}
	}))
	defer upstream.Close()
	client, err := brewmark.NewClient(brewmark.Config{BaseURL: upstream.URL, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(client, Config{AllowedOrigins: []string{"https://allowed.example"}}).Handler())
	defer server.Close()
	response, err := http.Get(server.URL + "/healthz")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("health: %v %v", response, err)
	}
	response.Body.Close()
	response, err = http.Get(server.URL + "/mcp")
	if err != nil || response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /mcp: %v %v", response, err)
	}
	response.Body.Close()
	_ = rpc(t, server.URL, `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, "")
	initialize := rpc(t, server.URL, `{"jsonrpc":"2.0","id":-1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, "")
	capabilities := initialize["result"].(map[string]any)["capabilities"].(map[string]any)
	toolsCapability := capabilities["tools"].(map[string]any)
	if changed, ok := toolsCapability["listChanged"]; ok && changed != false {
		t.Fatalf("tools.listChanged = %#v", changed)
	}
	result := rpc(t, server.URL, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, "")
	tools := result["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 4 || upstreamCalls != 0 {
		t.Fatalf("tools=%d calls=%d", len(tools), upstreamCalls)
	}
	for _, rawTool := range tools {
		tool := rawTool.(map[string]any)
		schema := tool["outputSchema"].(map[string]any)
		if schema["type"] != "object" || len(schema["oneOf"].([]any)) != 2 {
			t.Fatalf("unexpected output schema: %#v", schema)
		}
	}
	for id, name := range []string{"brewmark_list_grinders", "brewmark_list_brewers", "brewmark_list_filters", "brewmark_list_brew_methods"} {
		callResult := rpc(t, server.URL, `{"jsonrpc":"2.0","id":`+strconv.Itoa(id+10)+`,"method":"tools/call","params":{"name":"`+name+`","arguments":{}}}`, "")
		if callResult["result"].(map[string]any)["isError"] == true {
			t.Fatalf("tool %s failed: %#v", name, callResult)
		}
	}
	if upstreamCalls != 4 {
		t.Fatalf("upstream calls = %d", upstreamCalls)
	}
	denied := rpcStatus(t, server.URL, `{not-json`, "https://denied.example")
	if denied != http.StatusForbidden {
		t.Fatalf("status = %d", denied)
	}
	result = rpc(t, server.URL, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"brewmark_list_grinders","arguments":{"brand":"   "}}}`, "")
	call := result["result"].(map[string]any)
	if call["isError"] != true || upstreamCalls != 4 {
		t.Fatalf("invalid call=%#v calls=%d", call, upstreamCalls)
	}
	for id, name := range []string{"brewmark_list_grinders", "brewmark_list_brewers", "brewmark_list_filters", "brewmark_list_brew_methods"} {
		call = rpc(t, server.URL, `{"jsonrpc":"2.0","id":`+strconv.Itoa(id+30)+`,"method":"tools/call","params":{"name":"`+name+`","arguments":null}}`, "")["result"].(map[string]any)
		if call["isError"] != true || upstreamCalls != 4 {
			t.Fatalf("null arguments tool=%s result=%#v calls=%d", name, call, upstreamCalls)
		}
	}
}

func TestRequestBodyLimit(t *testing.T) {
	t.Parallel()
	client, err := brewmark.NewClient(brewmark.Config{BaseURL: "https://brewmark.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(client, Config{}).Handler())
	defer server.Close()
	response, err := http.Post(server.URL+"/mcp", "application/json", bytes.NewReader(make([]byte, maxRequestBodyBytes+1)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d", response.StatusCode)
	}
}

func TestEmitRetriesOnCollectorFailureAndLogsSafely(t *testing.T) {
	var calls int
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer collector.Close()

	var logs bytes.Buffer
	const token = "collector-secret"
	started := time.Now()
	emit(Config{
		Logger:             slog.New(slog.NewTextHandler(&logs, nil)),
		ObservabilityURL:   collector.URL,
		ObservabilityToken: token,
	}, "trace-1", "success", "", 0, time.Millisecond, "mcp_initialize", "")
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("publisher exceeded deadline: %s", elapsed)
	}
	if calls != 2 {
		t.Fatalf("collector calls = %d, want 2", calls)
	}
	output := logs.String()
	if !strings.Contains(output, "collector_unavailable") {
		t.Fatalf("missing failure log: %s", output)
	}
	if strings.Contains(output, collector.URL) || strings.Contains(output, token) {
		t.Fatalf("failure log contains collector details: %s", output)
	}
}

func rpc(t *testing.T, endpoint, payload, origin string) map[string]any {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint+"/mcp", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	var value map[string]any
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func rpcWithRequestID(t *testing.T, endpoint, payload, requestID string) map[string]any {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint+"/mcp", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("X-Request-ID", requestID)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	var value map[string]any
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func rpcStatus(t *testing.T, endpoint, payload, origin string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint+"/mcp", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", origin)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}
