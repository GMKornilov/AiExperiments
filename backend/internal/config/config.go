// Package config loads application configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"aichallenge/week_1/task_1/internal/algorithms"

	"gopkg.in/yaml.v3"
)

const (
	defaultRequestTimeout          = 30 * time.Second
	defaultAlgorithmRequestTimeout = 180 * time.Second
	maxAlgorithmRequestTimeout     = 180 * time.Second
)

const (
	defaultFreeSystemPromptPath       = "prompts/barista-free-system.txt"
	defaultControlledSystemPromptPath = "prompts/barista-controlled-system.txt"
	defaultResponseSchemaPath         = "schemas/barista-response.schema.json"
	defaultAlgorithmPromptsDir        = "prompts"
)

// Config contains settings for an OpenAI-compatible API.
type Config struct {
	BaseURL                    string             `yaml:"base_url"`
	APIKey                     string             `yaml:"api_key"`
	Model                      string             `yaml:"model"`
	RequestTimeout             time.Duration      `yaml:"request_timeout"`
	AlgorithmRequestTimeout    time.Duration      `yaml:"algorithm_request_timeout"`
	FreeSystemPromptPath       string             `yaml:"free_system_prompt_path"`
	ControlledSystemPromptPath string             `yaml:"controlled_system_prompt_path"`
	ResponseSchemaPath         string             `yaml:"response_schema_path"`
	AlgorithmPromptsDir        string             `yaml:"algorithms_prompts_dir"`
	FreeSystemPrompt           string             `yaml:"-"`
	ControlledSystemPrompt     string             `yaml:"-"`
	ResponseSchema             json.RawMessage    `yaml:"-"`
	AlgorithmPrompts           algorithms.Prompts `yaml:"-"`
}

// Load reads and validates a YAML configuration file.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("чтение %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("разбор %s: %w", path, err)
	}
	var configuredFields map[string]any
	if err := yaml.Unmarshal(data, &configuredFields); err != nil {
		return Config{}, fmt.Errorf("разбор %s: %w", path, err)
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if _, configured := configuredFields["algorithm_request_timeout"]; !configured {
		cfg.AlgorithmRequestTimeout = defaultAlgorithmRequestTimeout
	}
	if strings.TrimSpace(cfg.FreeSystemPromptPath) == "" {
		cfg.FreeSystemPromptPath = defaultFreeSystemPromptPath
	}
	if strings.TrimSpace(cfg.ControlledSystemPromptPath) == "" {
		cfg.ControlledSystemPromptPath = defaultControlledSystemPromptPath
	}
	if strings.TrimSpace(cfg.ResponseSchemaPath) == "" {
		cfg.ResponseSchemaPath = defaultResponseSchemaPath
	}
	if strings.TrimSpace(cfg.AlgorithmPromptsDir) == "" {
		cfg.AlgorithmPromptsDir = defaultAlgorithmPromptsDir
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("проверка %s: %w", path, err)
	}
	if err := cfg.loadResources(filepath.Dir(path)); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Validate verifies that all required settings have useful values.
func (c Config) Validate() error {
	if strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("base_url не должен быть пустым")
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("api_key не должен быть пустым")
	}
	if strings.TrimSpace(c.Model) == "" {
		return fmt.Errorf("model не должен быть пустым")
	}
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("request_timeout должен быть больше нуля")
	}
	if c.AlgorithmRequestTimeout <= 0 || c.AlgorithmRequestTimeout > maxAlgorithmRequestTimeout {
		return fmt.Errorf("algorithm_request_timeout должен быть больше нуля и не превышать %s", maxAlgorithmRequestTimeout)
	}
	if strings.TrimSpace(c.FreeSystemPromptPath) == "" {
		return fmt.Errorf("free_system_prompt_path не должен быть пустым")
	}
	if strings.TrimSpace(c.ControlledSystemPromptPath) == "" {
		return fmt.Errorf("controlled_system_prompt_path не должен быть пустым")
	}
	if strings.TrimSpace(c.ResponseSchemaPath) == "" {
		return fmt.Errorf("response_schema_path не должен быть пустым")
	}
	if strings.TrimSpace(c.AlgorithmPromptsDir) == "" {
		return fmt.Errorf("algorithms_prompts_dir не должен быть пустым")
	}
	return nil
}

func (c *Config) loadResources(configDir string) error {
	freeSystemPrompt, err := loadPrompt(configDir, c.FreeSystemPromptPath)
	if err != nil {
		return fmt.Errorf("загрузка free system prompt: %w", err)
	}
	controlledSystemPrompt, err := loadPrompt(configDir, c.ControlledSystemPromptPath)
	if err != nil {
		return fmt.Errorf("загрузка controlled system prompt: %w", err)
	}
	algorithmPrompts, err := loadAlgorithmPrompts(configDir, c.AlgorithmPromptsDir)
	if err != nil {
		return err
	}

	responseSchemaPath := resourcePath(configDir, c.ResponseSchemaPath)
	responseSchema, err := os.ReadFile(responseSchemaPath)
	if err != nil {
		return fmt.Errorf("чтение JSON Schema %s: %w", c.ResponseSchemaPath, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(responseSchema, &schema); err != nil {
		return fmt.Errorf("разбор JSON Schema %s: %w", c.ResponseSchemaPath, err)
	}
	if schema == nil {
		return fmt.Errorf("JSON Schema %s должна быть JSON-объектом", c.ResponseSchemaPath)
	}

	c.FreeSystemPrompt = freeSystemPrompt
	c.ControlledSystemPrompt = controlledSystemPrompt
	c.ResponseSchema = responseSchema
	c.AlgorithmPrompts = algorithmPrompts
	return nil
}

func loadAlgorithmPrompts(configDir, directory string) (algorithms.Prompts, error) {
	base := resourcePath(configDir, directory)
	sources := algorithms.PromptSources{}
	paths := []struct {
		name   string
		target *string
	}{
		{"algorithm-direct-system.txt", &sources.DirectSystem},
		{"algorithm-step-by-step-system.txt", &sources.StepByStepSystem},
		{"algorithm-experts-system.txt", &sources.ExpertsSystem},
		{"algorithm-meta-prompt-generation-system.txt", &sources.MetaPromptGenerationSystem},
		{"algorithm-meta-solution-system.txt", &sources.MetaSolutionSystem},
		{"algorithm-meta-solution-user.txt", &sources.MetaSolutionUser},
	}
	for _, prompt := range paths {
		content, err := os.ReadFile(filepath.Join(base, prompt.name))
		if err != nil {
			return algorithms.Prompts{}, fmt.Errorf("чтение algorithm prompt %s: %w", filepath.Join(directory, prompt.name), err)
		}
		if !utf8.Valid(content) {
			return algorithms.Prompts{}, fmt.Errorf("algorithm prompt %s должен быть валидным UTF-8", filepath.Join(directory, prompt.name))
		}
		*prompt.target = string(content)
	}
	prompts, err := algorithms.NewPrompts(sources)
	if err != nil {
		return algorithms.Prompts{}, fmt.Errorf("загрузка algorithm prompts: %w", err)
	}
	return prompts, nil
}

func loadPrompt(configDir, path string) (string, error) {
	prompt, err := os.ReadFile(resourcePath(configDir, path))
	if err != nil {
		return "", fmt.Errorf("чтение %s: %w", path, err)
	}
	if strings.TrimSpace(string(prompt)) == "" {
		return "", fmt.Errorf("%s не должен быть пустым", path)
	}
	return string(prompt), nil
}

func resourcePath(configDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(configDir, path)
}
