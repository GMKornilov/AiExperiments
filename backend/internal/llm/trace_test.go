package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTransportTraceIncludesActualBodiesAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"success", "application/json", `{"choices":[{"message":{"content":"answer","reasoning_content":"reasoning"}}],"usage":{"prompt_tokens":12,"completion_tokens":4}}`, 200},
		{"provider error", "application/json", `{"error":{"message":"failure","code":"upstream"}}`, 503},
		{"non json", "text/html", "<html>unavailable</html>", 502},
		{"invalid json", "application/json", "{invalid", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var received string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer private-key" {
					t.Error("missing credential")
				}
				data, _ := io.ReadAll(r.Body)
				received = string(data)
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			var traces []Trace
			ctx := WithTrace(WithPurpose(context.Background(), "summary"), func(_ context.Context, trace Trace) { traces = append(traces, trace) })
			_, _ = NewClient(server.URL, "private-key", time.Second).ChatCompletion(ctx, "model", []Message{{Role: "system", Content: "system"}, {Role: "user", Content: "question"}}, 0.2)
			if len(traces) > 0 && traces[0].Payload != received {
				t.Fatal("request log differs from sent body")
			}
			if len(traces) != 2 || traces[0].Event != "llm_request" || traces[1].Event != "llm_response" || traces[0].CallID != traces[1].CallID || traces[0].Purpose != "summary" {
				t.Fatalf("bad trace: %+v", traces)
			}
			if !strings.Contains(traces[0].Payload, "question") || traces[1].Payload != tc.body || traces[1].HTTPStatus != tc.status || strings.Contains(traces[0].Payload, "private-key") {
				t.Fatal("incomplete body or leaked authorization")
			}
			if (traces[1].ErrorCategory == "") != (tc.name == "success") {
				t.Fatal("incorrect outcome")
			}
		})
	}
}

func TestTraceMarksTruncationAndCompletesCancelledCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, strings.Repeat("x", maxResponseBytes+10))
	}))
	defer server.Close()
	var traces []Trace
	ctx := WithTrace(context.Background(), func(_ context.Context, trace Trace) { traces = append(traces, trace) })
	client := NewClient(server.URL, "private-key", time.Second)
	_, _ = client.ChatCompletion(ctx, "model", []Message{{Role: "user", Content: "test"}}, 1)
	if len(traces) != 2 || !traces[1].Truncated || len(traces[1].Payload) != maxResponseBytes {
		t.Fatal("missing truncation marker")
	}
	traces = nil
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, _ = client.ChatCompletion(cancelled, "model", []Message{{Role: "user", Content: "test"}}, 1)
	if len(traces) != 2 || traces[1].ErrorCategory == "" || traces[1].HTTPStatus != 0 || traces[1].Payload != "" {
		t.Fatal("cancelled call has no terminal trace")
	}
}
