package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompletionUsageIndependentOfAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		valid       bool
	}{
		{"missing", `null`, false}, {"zero", `{"prompt_tokens":0,"completion_tokens":0}`, true},
		{"normal", `{"prompt_tokens":100,"completion_tokens":20,"total_tokens":999,"prompt_cache_hit_tokens":80}`, true},
		{"partial", `{"prompt_tokens":100}`, false}, {"negative", `{"prompt_tokens":-1,"completion_tokens":20}`, false},
		{"fraction", `{"prompt_tokens":1.5,"completion_tokens":20}`, false}, {"string", `{"prompt_tokens":"1","completion_tokens":20}`, false},
		{"unsafe", `{"prompt_tokens":9007199254740992,"completion_tokens":0}`, false},
		{"unsafe sum", `{"prompt_tokens":9007199254740991,"completion_tokens":1}`, false},
	} {
		for _, validAnswer := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/answer=%v", tc.name, validAnswer), func(t *testing.T) {
				choices := `[]`
				if validAnswer {
					choices = `[{"message":{"content":"coffee"}}]`
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `{"choices":%s,"usage":%s}`, choices, tc.usage)
				}))
				defer server.Close()
				result, err := NewClient(server.URL, "key", time.Second).ChatCompletion(context.Background(), "model", []Message{{Role: "user", Content: "hi"}}, 0)
				if (err == nil) != validAnswer || (result.Usage != nil) != tc.valid {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			})
		}
	}
}

func TestContextLimitRequiresExplicitProviderSignal(t *testing.T) {
	for _, tc := range []struct {
		body string
		want ErrorKind
	}{
		{`{"error":{"code":"context_length_exceeded"}}`, ErrorContextLimit},
		{`{"error":{"message":"This model's maximum context length is 1048576 tokens. However, you requested 6000348 tokens (6000348 in the messages, 0 in the completion). Please reduce the length of the messages or completion.","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`, ErrorContextLimit},
		{`{"error":{"message":"maximum context length is unavailable"}}`, ErrorProvider},
		{`{"error":{"message":"request body too large"}}`, ErrorProvider},
		{`{"error":{"code":"invalid_request_error","message":"bad request"}}`, ErrorProvider},
		{`{"error":{"message":"context"}}`, ErrorProvider},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			fmt.Fprint(w, tc.body)
		}))
		_, err := NewClient(server.URL, "key", time.Second).ChatCompletion(context.Background(), "model", []Message{{Role: "user", Content: "hi"}}, 0)
		server.Close()
		var typed *Error
		if !errors.As(err, &typed) || typed.Kind != tc.want {
			t.Fatalf("%s: %v", tc.body, err)
		}
	}
}
