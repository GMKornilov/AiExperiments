package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientChat(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want %s", request.Method, http.MethodPost)
		}
		if request.URL.Path != "/chat/completions" {
			t.Errorf("path = %s, want /chat/completions", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}

		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"model":"test-model"`) {
			t.Errorf("request body = %s", body)
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":" Привет! "}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", time.Second)
	answer, err := client.Chat(context.Background(), "test-model", "Привет")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if answer != "Привет!" {
		t.Errorf("Chat() = %q, want %q", answer, "Привет!")
	}
}

func TestClientChatRejectsEmptyAnswer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"  "}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", time.Second)
	_, err := client.Chat(context.Background(), "test-model", "Привет")
	if err == nil || !strings.Contains(err.Error(), "пустой ответ") {
		t.Fatalf("Chat() error = %v, want empty-answer error", err)
	}
}
