package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryPathResolvesRelativeToBackendConfig(t *testing.T) {
	dir := t.TempDir()
	for _, configured := range []string{"", "saved/dialogs.json", filepath.Join(dir, "absolute.json")} {
		body := "addr: ':8080'\nllm_config_path: llm.yaml\n"
		if configured != "" {
			body += "history_path: '" + configured + "'\n"
		}
		path := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadBackend(path)
		if err != nil {
			t.Fatal(err)
		}
		want := configured
		if want == "" {
			want = "data/history.json"
		}
		if !filepath.IsAbs(want) {
			want = filepath.Join(dir, want)
		}
		if cfg.HistoryPath != want {
			t.Fatalf("history path = %q, want %q", cfg.HistoryPath, want)
		}
	}
}

func TestLoadLLMValidationAndRelativePrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("system"), 0o600); err != nil {
		t.Fatal(err)
	}
	valid := "chat:\n  base_url: https://example.test/v1\n  api_key: secret\n  model: model\n  system_prompt_path: prompt.txt\ntext:\n  base_url: https://example.test/v1\n  api_key: secret\n  model: title\n  system_prompt_path: prompt.txt\n"
	if err := os.WriteFile(filepath.Join(dir, "llm.yaml"), []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := LoadLLM(filepath.Join(dir, "llm.yaml")); err != nil || cfg.Chat.SystemPrompt != "system" || cfg.Text.SystemPrompt != "system" {
		t.Fatalf("LoadLLM = %#v, %v", cfg, err)
	}
	if cfg, err := LoadLLM(filepath.Join(dir, "llm.yaml")); err != nil || cfg.Chat.Temperature != 1 || cfg.Text.Temperature != 1 {
		t.Fatalf("default temperatures %#v %v", cfg, err)
	}
	for _, temperature := range []string{"0", "2", "null", "3", "-1", "NaN", ".inf"} {
		body := "chat:\n  base_url: https://example.test\n  api_key: x\n  model: x\n  temperature: " + temperature + "\n  system_prompt_path: prompt.txt\ntext:\n  base_url: https://example.test\n  api_key: x\n  model: x\n  temperature: 0\n  system_prompt_path: prompt.txt\n"
		if err := os.WriteFile(filepath.Join(dir, "temperature.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadLLM(filepath.Join(dir, "temperature.yaml"))
		valid := temperature == "0" || temperature == "2"
		if valid && (err != nil || cfg.Chat.Temperature != map[string]float64{"0": 0, "2": 2}[temperature]) {
			t.Fatalf("temperature %s: %#v %v", temperature, cfg, err)
		}
		if !valid && err == nil {
			t.Fatalf("temperature %s accepted", temperature)
		}
	}
	for _, body := range []string{"chat:\n  base_url: ftp://bad\n  api_key: x\n  model: x\n  system_prompt_path: prompt.txt\ntext:\n  base_url: https://example.test\n  api_key: x\n  model: x\n  system_prompt_path: prompt.txt\n", "chat:\n  base_url: https://example.test\n  api_key: x\n  model: x\n  system_prompt_path: missing.txt\ntext:\n  base_url: https://example.test\n  api_key: x\n  model: x\n  system_prompt_path: prompt.txt\n"} {
		if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadLLM(filepath.Join(dir, "bad.yaml")); err == nil {
			t.Fatal("ожидалась ошибка конфигурации")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("chat:\n  base_url: https://example.test\n  api_key: x\n  model: x\n  request_timeout: 0s\n  system_prompt_path: prompt.txt\ntext:\n  base_url: https://example.test\n  api_key: x\n  model: x\n  system_prompt_path: prompt.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLLM(filepath.Join(dir, "bad.yaml")); err == nil {
		t.Fatal("явный нулевой timeout принят")
	}
}

func TestLoadBackendRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("addr: ''\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBackend(path); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}
