// Package config loads application configuration.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultRequestTimeout = 30 * time.Second

// Config contains settings for an OpenAI-compatible API.
type Config struct {
	BaseURL        string        `yaml:"base_url"`
	APIKey         string        `yaml:"api_key"`
	Model          string        `yaml:"model"`
	RequestTimeout time.Duration `yaml:"request_timeout"`
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
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("проверка %s: %w", path, err)
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

	return nil
}
