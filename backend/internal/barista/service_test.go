package barista

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/config"
)

func TestServiceRoutesModesToLLM(t *testing.T) {
	tests := []struct {
		name string
		mode Mode
	}{
		{name: "free", mode: ModeFree},
		{name: "controlled", mode: ModeControlled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests++
				var body struct {
					Messages       []struct{ Role, Content string } "json:\"messages\""
					ResponseFormat json.RawMessage                  "json:\"response_format\""
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				switch test.mode {
				case ModeFree:
					if want := []struct{ Role, Content string }{{"system", "free prompt"}, {"user", "кофе"}}; !reflect.DeepEqual(body.Messages, want) {
						t.Errorf("free messages = %#v, want %#v", body.Messages, want)
					}
					if len(body.ResponseFormat) != 0 {
						t.Errorf("free response_format = %s, want absent", body.ResponseFormat)
					}
					writeServiceResponse(t, writer, "free answer")
				case ModeControlled:
					if want := []struct{ Role, Content string }{{"system", "controlled prompt\n{\"type\":\"object\"}"}, {"user", "кофе"}}; !reflect.DeepEqual(body.Messages, want) {
						t.Errorf("controlled messages = %#v, want %#v", body.Messages, want)
					}
					var responseFormat map[string]string
					if err := json.Unmarshal(body.ResponseFormat, &responseFormat); err != nil {
						t.Fatal(err)
					}
					if want := map[string]string{"type": "json_object"}; !reflect.DeepEqual(responseFormat, want) {
						t.Errorf("controlled response_format = %#v, want %#v", responseFormat, want)
					}
					writeServiceResponse(t, writer, "{}")
				}
			}))
			defer server.Close()

			service := NewService(config.Config{
				BaseURL:                server.URL,
				APIKey:                 "test-key",
				Model:                  "test-model",
				RequestTimeout:         time.Second,
				FreeSystemPrompt:       "free prompt",
				ControlledSystemPrompt: "controlled prompt",
				ResponseSchema:         json.RawMessage("{\"type\":\"object\"}"),
			})
			if _, err := service.Chat(context.Background(), test.mode, "кофе"); err != nil {
				t.Fatalf("Chat() error = %v", err)
			}
			if requests != 1 {
				t.Errorf("request count = %d, want 1", requests)
			}
		})
	}
}

func TestServiceRejectsInvalidInput(t *testing.T) {
	service := NewService(config.Config{})
	for _, test := range []struct {
		name   string
		mode   Mode
		prompt string
	}{
		{name: "empty prompt", mode: ModeFree, prompt: " "},
		{name: "unknown mode", mode: "other", prompt: "кофе"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := service.Chat(context.Background(), test.mode, test.prompt); err == nil {
				t.Fatal("Chat() error = nil, want error")
			}
		})
	}
}

func writeServiceResponse(t *testing.T, writer http.ResponseWriter, content string) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	encoded, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(encoded); err != nil {
		t.Error(err)
	}
}
