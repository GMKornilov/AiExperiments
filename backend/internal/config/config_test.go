package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadUsesDefaultsAndLoadsTwoPrompts(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
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

func TestLoadUsesExplicitPromptPaths(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
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

func TestConfigValidateRejectsInvalidArguments(t *testing.T) {
	t.Parallel()
	valid := Config{
		BaseURL:                    "https://example.com/v1",
		APIKey:                     "test-key",
		Model:                      "test-model",
		RequestTimeout:             time.Second,
		FreeSystemPromptPath:       "free.txt",
		ControlledSystemPromptPath: "controlled.txt",
		ResponseSchemaPath:         "schema.json",
	}
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{name: "empty base URL", edit: func(cfg *Config) { cfg.BaseURL = " " }},
		{name: "empty API key", edit: func(cfg *Config) { cfg.APIKey = " " }},
		{name: "empty model", edit: func(cfg *Config) { cfg.Model = " " }},
		{name: "non-positive timeout", edit: func(cfg *Config) { cfg.RequestTimeout = 0 }},
		{name: "empty free prompt path", edit: func(cfg *Config) { cfg.FreeSystemPromptPath = " " }},
		{name: "empty controlled prompt path", edit: func(cfg *Config) { cfg.ControlledSystemPromptPath = " " }},
		{name: "empty schema path", edit: func(cfg *Config) { cfg.ResponseSchemaPath = " " }},
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

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
