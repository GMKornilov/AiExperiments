package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/algorithms"
	"aichallenge/week_1/task_1/internal/llm"
)

type algorithmPromptClient struct{ messages [][]llm.Message }

func (c *algorithmPromptClient) ChatMessages(_ context.Context, _ string, messages []llm.Message) (string, error) {
	c.messages = append(c.messages, append([]llm.Message(nil), messages...))
	return "answer", nil
}

func TestLoadUsesDefaultsAndLoadsTwoPrompts(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	writeAlgorithmPromptFixtures(t, directory)
	writeTestFile(t, filepath.Join(directory, defaultFreeSystemPromptPath), "Свободный prompt.")
	writeTestFile(t, filepath.Join(directory, defaultControlledSystemPromptPath), "Контролируемый prompt.")
	writeTestFile(t, filepath.Join(directory, defaultResponseSchemaPath), "{\"type\":\"object\"}")
	configPath := filepath.Join(directory, "config.yaml")
	writeTestFile(t, configPath, "base_url: https://example.com/v1\napi_key: test-key\nmodel: test-model\n")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Errorf("RequestTimeout = %v, want 30s", cfg.RequestTimeout)
	}
	if cfg.AlgorithmRequestTimeout != 180*time.Second {
		t.Errorf("AlgorithmRequestTimeout = %v, want 180s", cfg.AlgorithmRequestTimeout)
	}
	if cfg.FreeSystemPromptPath != defaultFreeSystemPromptPath {
		t.Errorf("FreeSystemPromptPath = %q, want %q", cfg.FreeSystemPromptPath, defaultFreeSystemPromptPath)
	}
	if cfg.ControlledSystemPromptPath != defaultControlledSystemPromptPath {
		t.Errorf("ControlledSystemPromptPath = %q, want %q", cfg.ControlledSystemPromptPath, defaultControlledSystemPromptPath)
	}
	if cfg.FreeSystemPrompt != "Свободный prompt." || cfg.ControlledSystemPrompt != "Контролируемый prompt." {
		t.Errorf("loaded prompts = %q, %q", cfg.FreeSystemPrompt, cfg.ControlledSystemPrompt)
	}
}

func TestDefaultCommonAlgorithmPromptPublishesOnlySolutionParts(t *testing.T) {
	promptPath := filepath.Join("..", "..", "prompts", "algorithm-direct-system.txt")
	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(prompt)
	for _, required := range []string{
		"сразу дай решение задачи",
		"без предварительного плана",
		"Если интерфейс нельзя обосновать",
		"краткое объяснение выбранного алгоритма",
		"готовый вызываемый код",
		"примером вызова и ожидаемым результатом",
	} {
		if !strings.Contains(content, required) {
			t.Errorf("default common prompt does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"пошаг", "Теоретик", "Практик", "Скептик", "generated prompt", "meta"} {
		if strings.Contains(content, forbidden) {
			t.Errorf("default common prompt retains %q", forbidden)
		}
	}
}

func TestDefaultAlgorithmSystemPromptsDoNotCrossMethodContracts(t *testing.T) {
	tests := map[string][]string{
		"algorithm-direct-system.txt":                 {"пошаг", "Теоретик", "Практик", "Скептик", "generated prompt", "meta"},
		"algorithm-step-by-step-system.txt":           {"Теоретик", "Практик", "Скептик", "generated prompt", "meta"},
		"algorithm-experts-system.txt":                {"пошаг", "generated prompt", "meta", "direct"},
		"algorithm-meta-prompt-generation-system.txt": {"пошаг", "Теоретик", "Практик", "Скептик", "direct", "режим"},
		"algorithm-meta-solution-system.txt":          {"пошаг", "Теоретик", "Практик", "Скептик", "direct", "режим"},
	}
	for name, forbidden := range tests {
		content, err := os.ReadFile(filepath.Join("..", "..", "prompts", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range forbidden {
			if strings.Contains(strings.ToLower(string(content)), strings.ToLower(marker)) {
				t.Errorf("%s contains other-method marker %q", name, marker)
			}
		}
	}
}

func TestDefaultStandalonePromptsPutFallbackBeforeMethodOutput(t *testing.T) {
	tests := map[string]string{
		"algorithm-direct-system.txt":       "сразу дай решение задачи",
		"algorithm-step-by-step-system.txt": "решай пошагово",
		"algorithm-experts-system.txt":      "дай три явно подписанных",
	}
	for name, marker := range tests {
		content, err := os.ReadFile(filepath.Join("..", "..", "prompts", name))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(content))
		fallback := strings.Index(text, "если интерфейс нельзя обосновать")
		output := strings.Index(text, marker)
		if fallback < 0 || output < 0 || fallback > output {
			t.Errorf("%s must put fallback before its method output", name)
		}
		if !strings.Contains(text, "только одно краткое уточнение") {
			t.Errorf("%s lacks single fallback clarification", name)
		}
	}
}

func TestLoadUsesExplicitPromptPaths(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	writeAlgorithmPromptFixtures(t, directory)
	writeTestFile(t, filepath.Join(directory, "custom", "free.txt"), "Я свободный.")
	writeTestFile(t, filepath.Join(directory, "custom", "controlled.txt"), "Я контролируемый.")
	writeTestFile(t, filepath.Join(directory, "custom", "schema.json"), "{\"type\":\"object\"}")
	configPath := filepath.Join(directory, "config.yaml")
	writeTestFile(t, configPath, "base_url: https://example.com/v1\napi_key: test-key\nmodel: test-model\nfree_system_prompt_path: custom/free.txt\ncontrolled_system_prompt_path: custom/controlled.txt\nresponse_schema_path: custom/schema.json\n")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.FreeSystemPrompt != "Я свободный." || cfg.ControlledSystemPrompt != "Я контролируемый." {
		t.Errorf("loaded prompts = %q, %q", cfg.FreeSystemPrompt, cfg.ControlledSystemPrompt)
	}
}

func TestLoadRejectsInvalidResources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		free       string
		controlled string
		schema     string
		want       string
	}{
		{name: "empty free prompt", free: " \n\t", controlled: "controlled", schema: "{}", want: "free.txt не должен быть пустым"},
		{name: "empty controlled prompt", free: "free", controlled: " \n\t", schema: "{}", want: "controlled.txt не должен быть пустым"},
		{name: "missing free prompt", free: "", controlled: "controlled", schema: "{}", want: "загрузка free system prompt"},
		{name: "missing controlled prompt", free: "free", controlled: "", schema: "{}", want: "загрузка controlled system prompt"},
		{name: "invalid schema", free: "free", controlled: "controlled", schema: "{", want: "разбор JSON Schema"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			writeAlgorithmPromptFixtures(t, directory)
			if test.free != "" {
				writeTestFile(t, filepath.Join(directory, "free.txt"), test.free)
			}
			if test.controlled != "" {
				writeTestFile(t, filepath.Join(directory, "controlled.txt"), test.controlled)
			}
			writeTestFile(t, filepath.Join(directory, "schema.json"), test.schema)
			configPath := filepath.Join(directory, "config.yaml")
			writeTestFile(t, configPath, "base_url: https://example.com/v1\napi_key: test-key\nmodel: test-model\nfree_system_prompt_path: free.txt\ncontrolled_system_prompt_path: controlled.txt\nresponse_schema_path: schema.json\n")

			_, err := Load(configPath)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load() error = %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestLoadRejectsMissingOrInvalidAlgorithmPrompt(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		edit func(t *testing.T, directory string)
	}{
		{name: "missing", edit: func(t *testing.T, directory string) {
			t.Helper()
			_ = os.Remove(filepath.Join(directory, defaultAlgorithmPromptsDir, "algorithm-experts-system.txt"))
		}},
		{name: "invalid placeholder", edit: func(t *testing.T, directory string) {
			writeTestFile(t, filepath.Join(directory, defaultAlgorithmPromptsDir, "algorithm-experts-system.txt"), "{{.Unknown}}")
		}},
		{name: "invalid UTF-8", edit: func(t *testing.T, directory string) {
			writeTestFile(t, filepath.Join(directory, defaultAlgorithmPromptsDir, "algorithm-experts-system.txt"), string([]byte{0xff}))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			writeAlgorithmPromptFixtures(t, directory)
			test.edit(t, directory)
			writeTestFile(t, filepath.Join(directory, defaultFreeSystemPromptPath), "free")
			writeTestFile(t, filepath.Join(directory, defaultControlledSystemPromptPath), "controlled")
			writeTestFile(t, filepath.Join(directory, defaultResponseSchemaPath), "{}")
			configPath := filepath.Join(directory, "config.yaml")
			writeTestFile(t, configPath, "base_url: https://example.com/v1\napi_key: test-key\nmodel: test-model\n")
			if _, err := Load(configPath); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestLoadAlgorithmPromptFixtureChangesRenderedMessages(t *testing.T) {
	directory := t.TempDir()
	writeAlgorithmPromptFixtures(t, directory)
	writeTestFile(t, filepath.Join(directory, defaultAlgorithmPromptsDir, "algorithm-direct-system.txt"), "fixture-v1 {{.Language}} {{.InterfaceRule}}")
	writeTestFile(t, filepath.Join(directory, defaultFreeSystemPromptPath), "free")
	writeTestFile(t, filepath.Join(directory, defaultControlledSystemPromptPath), "controlled")
	writeTestFile(t, filepath.Join(directory, defaultResponseSchemaPath), "{}")
	configPath := filepath.Join(directory, "config.yaml")
	writeTestFile(t, configPath, "base_url: https://example.com/v1\napi_key: test-key\nmodel: test-model\n")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(directory, defaultAlgorithmPromptsDir)); err != nil {
		t.Fatal(err)
	}
	client := &algorithmPromptClient{}
	_, err = algorithms.NewService(client, cfg.Model, time.Second, cfg.AlgorithmPrompts).Solve(context.Background(), algorithms.MethodDirect, algorithms.Request{Statement: "statement", Language: algorithms.LanguagePython})
	if err != nil {
		t.Fatal(err)
	}
	if got := client.messages[0][0].Content; !strings.HasPrefix(got, "fixture-v1 python") {
		t.Errorf("rendered system prompt = %q, want fixture content", got)
	}
}

func TestLoadUsesCustomAlgorithmPromptsDirectoryRelativeToConfig(t *testing.T) {
	directory := t.TempDir()
	customDir := filepath.Join(directory, "fixtures", "algorithms")
	writeAlgorithmPromptFixtures(t, customDir)
	if err := os.Rename(filepath.Join(customDir, defaultAlgorithmPromptsDir), customDir+"-moved"); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(directory, defaultFreeSystemPromptPath), "free")
	writeTestFile(t, filepath.Join(directory, defaultControlledSystemPromptPath), "controlled")
	writeTestFile(t, filepath.Join(directory, defaultResponseSchemaPath), "{}")
	configPath := filepath.Join(directory, "config.yaml")
	writeTestFile(t, configPath, "base_url: https://example.com/v1\napi_key: test-key\nmodel: test-model\nalgorithms_prompts_dir: fixtures/algorithms-moved\n")
	if _, err := Load(configPath); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestConfigValidateRejectsInvalidArguments(t *testing.T) {
	t.Parallel()
	valid := Config{
		BaseURL:                    "https://example.com/v1",
		APIKey:                     "test-key",
		Model:                      "test-model",
		RequestTimeout:             time.Second,
		AlgorithmRequestTimeout:    time.Second,
		FreeSystemPromptPath:       "free.txt",
		ControlledSystemPromptPath: "controlled.txt",
		ResponseSchemaPath:         "schema.json",
		AlgorithmPromptsDir:        "prompts",
	}
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{name: "empty base URL", edit: func(cfg *Config) { cfg.BaseURL = " " }},
		{name: "empty API key", edit: func(cfg *Config) { cfg.APIKey = " " }},
		{name: "empty model", edit: func(cfg *Config) { cfg.Model = " " }},
		{name: "non-positive timeout", edit: func(cfg *Config) { cfg.RequestTimeout = 0 }},
		{name: "non-positive algorithm timeout", edit: func(cfg *Config) { cfg.AlgorithmRequestTimeout = 0 }},
		{name: "algorithm timeout over maximum", edit: func(cfg *Config) { cfg.AlgorithmRequestTimeout = maxAlgorithmRequestTimeout + time.Nanosecond }},
		{name: "empty free prompt path", edit: func(cfg *Config) { cfg.FreeSystemPromptPath = " " }},
		{name: "empty controlled prompt path", edit: func(cfg *Config) { cfg.ControlledSystemPromptPath = " " }},
		{name: "empty schema path", edit: func(cfg *Config) { cfg.ResponseSchemaPath = " " }},
		{name: "empty algorithm prompts directory", edit: func(cfg *Config) { cfg.AlgorithmPromptsDir = " " }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			test.edit(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
		})
	}
}

func TestLoadUsesExplicitAlgorithmTimeoutWithinBounds(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	writeAlgorithmPromptFixtures(t, directory)
	writeTestFile(t, filepath.Join(directory, defaultFreeSystemPromptPath), "Свободный prompt.")
	writeTestFile(t, filepath.Join(directory, defaultControlledSystemPromptPath), "Контролируемый prompt.")
	writeTestFile(t, filepath.Join(directory, defaultResponseSchemaPath), "{\"type\":\"object\"}")
	configPath := filepath.Join(directory, "config.yaml")
	writeTestFile(t, configPath, "base_url: https://example.com/v1\napi_key: test-key\nmodel: test-model\nrequest_timeout: 1s\nalgorithm_request_timeout: 180s\n")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RequestTimeout != time.Second || cfg.AlgorithmRequestTimeout != 180*time.Second {
		t.Errorf("timeouts = %v/%v, want 1s/180s", cfg.RequestTimeout, cfg.AlgorithmRequestTimeout)
	}
}

func TestLoadRejectsExplicitInvalidAlgorithmTimeout(t *testing.T) {
	t.Parallel()
	for _, timeout := range []string{"0s", "-1s", "181s"} {
		t.Run(timeout, func(t *testing.T) {
			directory := t.TempDir()
			writeAlgorithmPromptFixtures(t, directory)
			writeTestFile(t, filepath.Join(directory, defaultFreeSystemPromptPath), "Свободный prompt.")
			writeTestFile(t, filepath.Join(directory, defaultControlledSystemPromptPath), "Контролируемый prompt.")
			writeTestFile(t, filepath.Join(directory, defaultResponseSchemaPath), "{\"type\":\"object\"}")
			configPath := filepath.Join(directory, "config.yaml")
			writeTestFile(t, configPath, "base_url: https://example.com/v1\napi_key: test-key\nmodel: test-model\nalgorithm_request_timeout: "+timeout+"\n")

			_, err := Load(configPath)
			if err == nil || !strings.Contains(err.Error(), "algorithm_request_timeout") {
				t.Fatalf("Load() error = %v, want algorithm timeout validation error", err)
			}
		})
	}
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
		writeTestFile(t, filepath.Join(directory, defaultAlgorithmPromptsDir, name), content)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
