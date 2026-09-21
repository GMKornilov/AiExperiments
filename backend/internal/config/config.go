// Package config loads backend and LLM configuration files.
package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

const defaultRequestTimeout = 30 * time.Second

var environmentPlaceholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

type BackendConfig struct {
	Addr            string `yaml:"addr"`
	LLMConfigPath   string `yaml:"llm_config_path"`
	LogTextPayloads bool   `yaml:"log_text_payloads"`
	HistoryPath     string `yaml:"history_path"`
}

type LLMConfig struct {
	Chat                  LLMEndpoint    `yaml:"chat"`
	Text                  LLMEndpoint    `yaml:"text"`
	Summary               *SummaryConfig `yaml:"summary"`
	Facts                 *FactsConfig   `yaml:"facts"`
	Memory                *FactsConfig   `yaml:"memory"`
	InvariantValidation   *FactsConfig   `yaml:"invariant_validation"`
	ContextWindowMessages int            `yaml:"context_window_messages"`
}
type SummaryConfig struct {
	Endpoint         LLMEndpoint `yaml:",inline"`
	KeepLastMessages int         `yaml:"keep_last_messages"`
	BatchSize        int         `yaml:"batch_size"`
}
type FactsConfig struct {
	Endpoint LLMEndpoint `yaml:",inline"`
}
type LLMEndpoint struct {
	ContextWindowTokens int64         `yaml:"context_window_tokens"`
	BaseURL             string        `yaml:"base_url"`
	APIKey              string        `yaml:"api_key"`
	Model               string        `yaml:"model"`
	RequestTimeout      time.Duration `yaml:"request_timeout"`
	Temperature         float64       `yaml:"temperature"`
	SystemPromptPath    string        `yaml:"system_prompt_path"`
	SystemPrompt        string        `yaml:"-"`
}

func LoadBackend(path string) (BackendConfig, error) {
	var cfg BackendConfig
	if err := loadYAML(path, &cfg); err != nil {
		return BackendConfig{}, fmt.Errorf("backend config: %w", err)
	}
	if strings.TrimSpace(cfg.Addr) == "" {
		return BackendConfig{}, fmt.Errorf("addr не должен быть пустым")
	}
	if strings.TrimSpace(cfg.LLMConfigPath) == "" {
		return BackendConfig{}, fmt.Errorf("llm_config_path не должен быть пустым")
	}
	if !filepath.IsAbs(cfg.LLMConfigPath) {
		cfg.LLMConfigPath = filepath.Join(filepath.Dir(path), cfg.LLMConfigPath)
	}
	if strings.TrimSpace(cfg.HistoryPath) == "" {
		cfg.HistoryPath = "data/history.json"
	}
	if !filepath.IsAbs(cfg.HistoryPath) {
		cfg.HistoryPath = filepath.Join(filepath.Dir(path), cfg.HistoryPath)
	}
	return cfg, nil
}

// LoadLLM creates a fresh immutable configuration snapshot for a new dialog.
func LoadLLM(path string) (LLMConfig, error) {
	var cfg LLMConfig
	var raw map[string]any
	if err := loadYAML(path, &cfg); err != nil {
		return LLMConfig{}, fmt.Errorf("LLM config: %w", err)
	}
	if err := loadYAML(path, &raw); err != nil {
		return LLMConfig{}, fmt.Errorf("LLM config: %w", err)
	}
	if raw["chat"] == nil || raw["text"] == nil {
		return LLMConfig{}, fmt.Errorf("chat и text обязательны")
	}
	if raw["invariant_validation"] == nil {
		return LLMConfig{}, fmt.Errorf("invariant_validation обязательна")
	}
	if raw["context_window_messages"] == nil {
		cfg.ContextWindowMessages = 10
	}
	if cfg.ContextWindowMessages < 1 {
		return LLMConfig{}, fmt.Errorf("context_window_messages должен быть положительным")
	}
	if err := loadEndpoint(&cfg.Chat, filepath.Dir(path), raw["chat"]); err != nil {
		return LLMConfig{}, fmt.Errorf("chat: %w", err)
	}
	if err := loadEndpoint(&cfg.Text, filepath.Dir(path), raw["text"]); err != nil {
		return LLMConfig{}, fmt.Errorf("text: %w", err)
	}
	if cfg.Summary != nil {
		if err := loadEndpoint(&cfg.Summary.Endpoint, filepath.Dir(path), raw["summary"]); err != nil {
			return LLMConfig{}, fmt.Errorf("summary: %w", err)
		}
		m, _ := raw["summary"].(map[string]any)
		if m["keep_last_messages"] == nil {
			cfg.Summary.KeepLastMessages = 10
		}
		if m["batch_size"] == nil {
			cfg.Summary.BatchSize = 10
		}
		if cfg.Summary.KeepLastMessages < 1 || cfg.Summary.BatchSize < 1 {
			return LLMConfig{}, fmt.Errorf("summary: keep_last_messages и batch_size должны быть положительными")
		}
	}
	if cfg.Facts != nil {
		if err := loadEndpoint(&cfg.Facts.Endpoint, filepath.Dir(path), raw["facts"]); err != nil {
			return LLMConfig{}, fmt.Errorf("facts: %w", err)
		}
	}
	if cfg.Memory != nil {
		if err := loadEndpoint(&cfg.Memory.Endpoint, filepath.Dir(path), raw["memory"]); err != nil {
			return LLMConfig{}, fmt.Errorf("memory: %w", err)
		}
	}
	if cfg.InvariantValidation == nil {
		return LLMConfig{}, fmt.Errorf("invariant_validation обязательна")
	}
	if err := loadEndpoint(&cfg.InvariantValidation.Endpoint, filepath.Dir(path), raw["invariant_validation"]); err != nil {
		return LLMConfig{}, fmt.Errorf("invariant_validation: %w", err)
	}
	return cfg, nil
}

// LoadEnvFiles loads dotenv files without overwriting values already set in the
// process environment. Earlier files take precedence over later files.
func LoadEnvFiles(paths []string) error {
	for _, path := range paths {
		values, err := godotenv.Read(path)
		if err != nil {
			return fmt.Errorf("чтение dotenv-файла %q: %w", path, err)
		}
		for name, value := range values {
			if _, exists := os.LookupEnv(name); exists {
				continue
			}
			if err := os.Setenv(name, value); err != nil {
				return fmt.Errorf("установка переменной окружения %q: %w", name, err)
			}
		}
	}
	return nil
}

func loadYAML(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("чтение YAML: %w", err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("разбор YAML: %w", err)
	}
	if err := expandNodeEnvironment(&document); err != nil {
		return err
	}
	if err := document.Decode(target); err != nil {
		return fmt.Errorf("разбор YAML: %w", err)
	}
	return nil
}

func expandNodeEnvironment(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
		value, err := expandEnvironment(node.Value)
		if err != nil {
			return err
		}
		node.Value = value
	}
	for _, child := range node.Content {
		if err := expandNodeEnvironment(child); err != nil {
			return err
		}
	}
	return nil
}

func expandEnvironment(value string) (string, error) {
	var missing string
	expanded := environmentPlaceholder.ReplaceAllStringFunc(value, func(match string) string {
		name := environmentPlaceholder.FindStringSubmatch(match)[1]
		environmentValue, exists := os.LookupEnv(name)
		if !exists || strings.TrimSpace(environmentValue) == "" {
			missing = name
			return match
		}
		return environmentValue
	})
	if missing != "" {
		return "", fmt.Errorf("переменная окружения %q для YAML-конфигурации не задана или пуста", missing)
	}
	return expanded, nil
}

func loadEndpoint(cfg *LLMEndpoint, dir string, raw any) error {
	if cfg.ContextWindowTokens < 0 || cfg.ContextWindowTokens > 9007199254740991 {
		return fmt.Errorf("некорректный context_window_tokens")
	}
	m, _ := raw.(map[string]any)
	if cfg.RequestTimeout == 0 && m["request_timeout"] == nil {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	value, configured := m["temperature"]
	if !configured {
		cfg.Temperature = 1
	}
	if configured && value == nil {
		return fmt.Errorf("temperature не должна быть null")
	}
	if math.IsNaN(cfg.Temperature) || math.IsInf(cfg.Temperature, 0) || cfg.Temperature < 0 || cfg.Temperature > 2 {
		return fmt.Errorf("temperature должна быть конечным числом от 0 до 2")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("base_url, api_key и model не должны быть пустыми")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("base_url должен быть HTTP URL")
	}
	if cfg.RequestTimeout <= 0 {
		return fmt.Errorf("request_timeout должен быть больше нуля")
	}
	if strings.TrimSpace(cfg.SystemPromptPath) == "" {
		return fmt.Errorf("system_prompt_path не должен быть пустым")
	}
	p := cfg.SystemPromptPath
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return fmt.Errorf("чтение system prompt: %w", err)
	}
	if !utf8.Valid(data) || strings.TrimSpace(string(data)) == "" {
		return fmt.Errorf("system prompt должен быть непустым UTF-8 текстом")
	}
	cfg.SystemPrompt = string(data)
	return nil
}
