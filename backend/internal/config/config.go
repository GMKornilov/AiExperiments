// Package config loads backend and LLM configuration files.
package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const defaultRequestTimeout = 30 * time.Second

type BackendConfig struct {
	Addr            string `yaml:"addr"`
	LLMConfigPath   string `yaml:"llm_config_path"`
	LogTextPayloads bool   `yaml:"log_text_payloads"`
}

type LLMConfig struct {
	Chat LLMEndpoint `yaml:"chat"`
	Text LLMEndpoint `yaml:"text"`
}
type LLMEndpoint struct {
	BaseURL          string        `yaml:"base_url"`
	APIKey           string        `yaml:"api_key"`
	Model            string        `yaml:"model"`
	RequestTimeout   time.Duration `yaml:"request_timeout"`
	Temperature      float64       `yaml:"temperature"`
	SystemPromptPath string        `yaml:"system_prompt_path"`
	SystemPrompt     string        `yaml:"-"`
}

func LoadBackend(path string) (BackendConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BackendConfig{}, fmt.Errorf("чтение backend config: %w", err)
	}
	var cfg BackendConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return BackendConfig{}, fmt.Errorf("разбор backend config: %w", err)
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
	return cfg, nil
}

// LoadLLM creates a fresh immutable configuration snapshot for a new dialog.
func LoadLLM(path string) (LLMConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return LLMConfig{}, fmt.Errorf("чтение LLM config: %w", err)
	}
	var cfg LLMConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return LLMConfig{}, fmt.Errorf("разбор LLM config: %w", err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return LLMConfig{}, fmt.Errorf("разбор LLM config: %w", err)
	}
	if raw["chat"] == nil || raw["text"] == nil {
		return LLMConfig{}, fmt.Errorf("chat и text обязательны")
	}
	if err := loadEndpoint(&cfg.Chat, filepath.Dir(path), raw["chat"]); err != nil {
		return LLMConfig{}, fmt.Errorf("chat: %w", err)
	}
	if err := loadEndpoint(&cfg.Text, filepath.Dir(path), raw["text"]); err != nil {
		return LLMConfig{}, fmt.Errorf("text: %w", err)
	}
	return cfg, nil
}

func loadEndpoint(cfg *LLMEndpoint, dir string, raw any) error {
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
