package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const cliBaristaSchema = "{\"type\":\"object\",\"additionalProperties\":false,\"x-max-total-string-words\":60,\"required\":[\"summary\",\"focus_points\",\"recipe\"],\"properties\":{\"summary\":{\"type\":\"string\",\"x-max-words\":15},\"focus_points\":{\"type\":\"array\",\"minItems\":3,\"maxItems\":3,\"items\":{\"type\":\"string\",\"x-max-words\":8}},\"recipe\":{\"type\":\"object\",\"additionalProperties\":false,\"required\":[\"coffee_g\",\"water_g\",\"temperature_c\",\"brew_time_sec\"],\"properties\":{\"coffee_g\":{\"type\":\"number\"},\"water_g\":{\"type\":\"number\"},\"temperature_c\":{\"type\":\"number\"},\"brew_time_sec\":{\"type\":\"integer\"}}}}}"

const cliValidAnswer = "{\"summary\":\"Нужна настройка помола.\",\"focus_points\":[\"Помол мельче.\",\"Воду горячее.\",\"Лейте медленнее.\"],\"recipe\":{\"coffee_g\":18,\"water_g\":36,\"temperature_c\":93,\"brew_time_sec\":28}}"

func TestParseMode(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantMode mode
		wantErr  bool
	}{
		{name: "default", wantMode: modeFree},
		{name: "explicit free", args: []string{"--mode=free"}, wantMode: modeFree},
		{name: "controlled", args: []string{"--mode", "controlled"}, wantMode: modeControlled},
		{name: "invalid value", args: []string{"--mode=invalid"}, wantErr: true},
		{name: "unknown flag", args: []string{"--unknown"}, wantErr: true},
		{name: "positional argument", args: []string{"prompt"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotMode, err := parseMode(test.args)
			if test.wantErr {
				if err == nil {
					t.Fatal("parseMode() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMode() error = %v", err)
			}
			if gotMode != test.wantMode {
				t.Errorf("parseMode() = %q, want %q", gotMode, test.wantMode)
			}
		})
	}
}

func TestRunWithModeFreeSendsOneUnconstrainedRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		body := decodeChatRequest(t, request)
		if want := []chatMessage{{Role: "system", Content: "Свободный режим: дай практичный совет."}, {Role: "user", Content: "кофе"}}; !reflect.DeepEqual(body.Messages, want) {
			t.Errorf("messages = %#v, want %#v", body.Messages, want)
		}
		if len(body.ResponseFormat) != 0 {
			t.Errorf("response_format = %s, want absent", body.ResponseFormat)
		}
		writeChatResponse(t, writer, "free-answer")
	}))
	defer server.Close()

	configureCLITest(t, server.URL)
	var output bytes.Buffer
	if err := runWithMode(bytes.NewBufferString("кофе\n"), &output, modeFree); err != nil {
		t.Fatalf("runWithMode() error = %v", err)
	}
	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}
	if got, want := output.String(), ">>> <<< free-answer\n>>> "; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRunWithModeControlledSendsOneDeepSeekJSONRequest(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		body := decodeChatRequest(t, request)
		if len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1] != (chatMessage{Role: "user", Content: "кофе"}) {
			t.Errorf("messages = %#v, want system then user", body.Messages)
		}
		if want := strings.TrimSpace("Контролируемый режим.") + "\n" + cliBaristaSchema; body.Messages[0].Content != want {
			t.Errorf("system message = %q, want %q", body.Messages[0].Content, want)
		}
		var responseFormat map[string]string
		if err := json.Unmarshal(body.ResponseFormat, &responseFormat); err != nil {
			t.Fatal(err)
		}
		if want := map[string]string{"type": "json_object"}; !reflect.DeepEqual(responseFormat, want) {
			t.Errorf("response_format = %#v, want %#v", responseFormat, want)
		}
		writeChatResponse(t, writer, cliValidAnswer)
	}))
	defer server.Close()

	configureCLITest(t, server.URL)
	var output bytes.Buffer
	if err := runWithMode(bytes.NewBufferString("кофе\n"), &output, modeControlled); err != nil {
		t.Fatalf("runWithMode() error = %v", err)
	}
	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}
	if got, want := output.String(), ">>> <<< "+cliValidAnswer+"\n>>> "; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRunWithModeSendsOneRequestPerInput(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		writeChatResponse(t, writer, "answer")
	}))
	defer server.Close()

	configureCLITest(t, server.URL)
	var output bytes.Buffer
	if err := runWithMode(bytes.NewBufferString("первый\nвторой\n"), &output, modeFree); err != nil {
		t.Fatalf("runWithMode() error = %v", err)
	}
	if requests != 2 {
		t.Errorf("request count = %d, want 2", requests)
	}
}

func TestRunWithModeControlledContinuesAfterInvalidAnswer(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			writeChatResponse(t, writer, "plain text")
			return
		}
		writeChatResponse(t, writer, cliValidAnswer)
	}))
	defer server.Close()

	configureCLITest(t, server.URL)
	var output bytes.Buffer
	if err := runWithMode(bytes.NewBufferString("первый\nвторой\n"), &output, modeControlled); err != nil {
		t.Fatalf("runWithMode() error = %v", err)
	}
	if requests != 2 {
		t.Errorf("request count = %d, want 2", requests)
	}
	got := output.String()
	if !strings.Contains(got, "Ошибка:") || strings.Contains(got, "<<< plain text") {
		t.Errorf("output = %q, want error without invalid answer", got)
	}
	if !strings.Contains(got, "<<< "+cliValidAnswer) {
		t.Errorf("output = %q, want later valid answer", got)
	}
}

type chatRequestBody struct {
	Messages       []chatMessage   "json:\"messages\""
	ResponseFormat json.RawMessage "json:\"response_format\""
}

type chatMessage struct {
	Role    string "json:\"role\""
	Content string "json:\"content\""
}

func decodeChatRequest(t *testing.T, request *http.Request) chatRequestBody {
	t.Helper()
	if request.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", request.Method)
	}
	if request.URL.Path != "/chat/completions" {
		t.Errorf("path = %s, want /chat/completions", request.URL.Path)
	}
	var body chatRequestBody
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func writeChatResponse(t *testing.T, writer http.ResponseWriter, answer string) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	response, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(response); err != nil {
		t.Error(err)
	}
}

func configureCLITest(t *testing.T, baseURL string) {
	t.Helper()
	directory := t.TempDir()
	writeMainTestFile(t, filepath.Join(directory, "free.txt"), "Свободный режим: дай практичный совет.")
	writeMainTestFile(t, filepath.Join(directory, "controlled.txt"), "Контролируемый режим.")
	writeMainTestFile(t, filepath.Join(directory, "schema.json"), cliBaristaSchema)
	writeAlgorithmPromptFixtures(t, directory)
	writeMainTestFile(t, filepath.Join(directory, configPath), "base_url: "+baseURL+"\napi_key: test-key\nmodel: test-model\nfree_system_prompt_path: free.txt\ncontrolled_system_prompt_path: controlled.txt\nresponse_schema_path: schema.json\n")
	t.Chdir(directory)
}

func writeAlgorithmPromptFixtures(t *testing.T, directory string) {
	t.Helper()
	files := map[string]string{
		"algorithm-direct-system.txt":                 "direct {{.Language}} {{.InterfaceRule}}",
		"algorithm-step-by-step-system.txt":           "steps {{.Language}} {{.InterfaceRule}}",
		"algorithm-experts-system.txt":                "experts {{.Language}} {{.InterfaceRule}}",
		"algorithm-meta-prompt-generation-system.txt": "generate {{.Language}} {{.InterfaceRule}}",
		"algorithm-meta-solution-system.txt":          "solution {{.Language}} {{.InterfaceRule}}",
		"algorithm-meta-solution-user.txt":            "{{.Statement}} {{.GeneratedPrompt}}",
	}
	for name, content := range files {
		path := filepath.Join(directory, "prompts", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		writeMainTestFile(t, path, content)
	}
}

func writeMainTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
