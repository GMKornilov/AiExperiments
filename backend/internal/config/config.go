// Package config loads application configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultRequestTimeout = 30 * time.Second

const (
	defaultFreeSystemPromptPath       = "prompts/barista-free-system.txt"
	defaultControlledSystemPromptPath = "prompts/barista-controlled-system.txt"
	defaultResponseSchemaPath         = "schemas/barista-response.schema.json"
)

// Config contains settings for an OpenAI-compatible API.
type Config struct {
	BaseURL                    string          `yaml:"base_url"`
	APIKey                     string          `yaml:"api_key"`
	Model                      string          `yaml:"model"`
	RequestTimeout             time.Duration   `yaml:"request_timeout"`
	FreeSystemPromptPath       string          `yaml:"free_system_prompt_path"`
	ControlledSystemPromptPath string          `yaml:"controlled_system_prompt_path"`
	ResponseSchemaPath         string          `yaml:"response_schema_path"`
	FreeSystemPrompt           string          `yaml:"-"`
	ControlledSystemPrompt     string          `yaml:"-"`
	ResponseSchema             json.RawMessage `yaml:"-"`
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
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = defaultRequestTimeout
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
	if strings.TrimSpace(c.FreeSystemPromptPath) == "" {
		return fmt.Errorf("free_system_prompt_path не должен быть пустым")
	}
	if strings.TrimSpace(c.ControlledSystemPromptPath) == "" {
		return fmt.Errorf("controlled_system_prompt_path не должен быть пустым")
	}
	if strings.TrimSpace(c.ResponseSchemaPath) == "" {
		return fmt.Errorf("response_schema_path не должен быть пустым")
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
	return nil
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
