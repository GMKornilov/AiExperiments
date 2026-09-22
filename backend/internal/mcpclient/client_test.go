package mcpclient

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"

	"aichallenge/week_1/task_1/internal/brewmark"
	"aichallenge/week_1/task_1/internal/mcpserver"
)

func TestNewRejectsUnsafeEndpoint(t *testing.T) {
	for _, endpoint := range []string{"", "ftp://example.test/mcp", "https://example.test/mcp?token=secret", "https://user:secret@example.test/mcp", "not a url"} {
		client, err := New(endpoint)
		if client != nil {
			t.Fatalf("endpoint %q created a client", endpoint)
		}
		if errorCode(err) != NotConfigured {
			t.Fatalf("endpoint %q error=%v", endpoint, err)
		}
	}
}

func TestNewAcceptsHTTPSEndpoints(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:8080/mcp", "https://mcp.example.test/mcp"} {
		client, err := New(endpoint)
		if err != nil || client == nil {
			t.Fatalf("endpoint %q client=%v error=%v", endpoint, client, err)
		}
	}
}

func TestListToolsInitializesAndNormalizesRegistry(t *testing.T) {
	brewmarkClient, err := brewmark.NewClient(brewmark.Config{BaseURL: "https://brewmark.example.test", Logger: slog.Default()})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(mcpserver.New(brewmarkClient, mcpserver.Config{Logger: slog.Default()}).Handler())
	defer server.Close()
	client, err := New(server.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	tools, err := client.ListTools(context.Background(), "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 4 {
		t.Fatalf("tools=%+v", tools)
	}
	for _, tool := range tools {
		if tool.Name == "" || tool.Description == "" {
			t.Fatalf("invalid tool=%+v", tool)
		}
	}
}

func errorCode(err error) ErrorCode {
	if safeError, ok := err.(*Error); ok {
		return safeError.Code
	}
	return ""
}
