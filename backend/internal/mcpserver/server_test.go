package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/brewmark"
)

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
