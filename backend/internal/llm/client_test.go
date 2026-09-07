package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChatMessagesSerializesOrderedContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Fatal("credential was not sent")
		}
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "test" || len(request.Messages) != 3 || request.Messages[0].Role != "system" || request.Messages[2].Content != "next" {
			t.Fatalf("request = %#v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"answer"}}]}`))
	}))
	defer server.Close()
	answer, err := NewClient(server.URL, "key", time.Second).ChatMessages(context.Background(), "test", []Message{{Role: "system", Content: "system"}, {Role: "user", Content: "first"}, {Role: "user", Content: "next"}})
	if err != nil || answer != "answer" {
		t.Fatalf("ChatMessages() = %q, %v", answer, err)
	}
}

func TestChatMessagesClassifiesInvalidAndProviderResponses(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		contentType string
		body        string
		want        ErrorKind
	}{
		{"invalid JSON", 200, "application/json", "{", ErrorInvalidResponse},
		{"invalid type", 200, "text/plain", `{"choices":[]}`, ErrorInvalidResponse},
		{"provider error", 503, "application/json", `{"error":"private-secret"}`, ErrorProvider},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			_, err := NewClient(server.URL, "key", time.Second).ChatMessages(context.Background(), "test", []Message{{Role: "user", Content: "coffee"}})
			var got *Error
			if !errors.As(err, &got) || got.Kind != test.want {
				t.Fatalf("error = %#v", err)
			}
			if strings.Contains(err.Error(), "private-secret") {
				t.Fatal("provider body leaked")
			}
		})
	}
}

func TestChatMessagesTimeoutCallsProviderOnce(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer server.Close()
	_, err := NewClient(server.URL, "key", 20*time.Millisecond).ChatMessages(context.Background(), "test", []Message{{Role: "user", Content: "coffee"}})
	var got *Error
	if !errors.As(err, &got) || got.Kind != ErrorTimeout || calls.Load() != 1 {
		t.Fatalf("error=%v calls=%d", err, calls.Load())
	}
}
