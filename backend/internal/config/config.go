// Package config loads backend and LLM configuration files.
package config

import (
	"fmt"
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
	BaseURL          string        `yaml:"base_url"`
	APIKey           string        `yaml:"api_key"`
	Model            string        `yaml:"model"`
	RequestTimeout   time.Duration `yaml:"request_timeout"`
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
	if cfg.RequestTimeout == 0 && raw["request_timeout"] == nil {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return LLMConfig{}, fmt.Errorf("base_url, api_key и model не должны быть пустыми")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return LLMConfig{}, fmt.Errorf("base_url должен быть HTTP URL")
	}
	if cfg.RequestTimeout <= 0 {
		return LLMConfig{}, fmt.Errorf("request_timeout должен быть больше нуля")
	}
	if strings.TrimSpace(cfg.SystemPromptPath) == "" {
		return LLMConfig{}, fmt.Errorf("system_prompt_path не должен быть пустым")
	}
	promptPath := cfg.SystemPromptPath
	if !filepath.IsAbs(promptPath) {
		promptPath = filepath.Join(filepath.Dir(path), promptPath)
	}
	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		return LLMConfig{}, fmt.Errorf("чтение system prompt: %w", err)
	}
	if !utf8.Valid(prompt) || strings.TrimSpace(string(prompt)) == "" {
		return LLMConfig{}, fmt.Errorf("system prompt должен быть непустым UTF-8 текстом")
	}
	cfg.SystemPrompt = string(prompt)
	return cfg, nil
}
