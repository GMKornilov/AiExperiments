package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadLLMExpandsEnvironmentPlaceholder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("system"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_TEST_API_KEY", "from-environment")
	path := filepath.Join(dir, "llm.yaml")
	body := "chat:\n  base_url: https://example.test\n  api_key: ${CONFIG_TEST_API_KEY}\n  model: chat\n  system_prompt_path: prompt.txt\ntext:\n  base_url: https://example.test\n  api_key: ${CONFIG_TEST_API_KEY}\n  model: text\n  system_prompt_path: prompt.txt\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadLLM(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Chat.APIKey != "from-environment" || cfg.Text.APIKey != "from-environment" {
		t.Fatalf("ключи не подставлены: chat=%q text=%q", cfg.Chat.APIKey, cfg.Text.APIKey)
	}
}

func TestLoadLLMRejectsMissingOrEmptyEnvironmentPlaceholderWithoutSecret(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("system"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_TEST_EMPTY_API_KEY", "")
	if err := os.Unsetenv("CONFIG_TEST_MISSING_API_KEY"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("CONFIG_TEST_MISSING_API_KEY") })
	t.Setenv("CONFIG_TEST_UNRELATED_SECRET", "must-not-appear")
	for _, name := range []string{"CONFIG_TEST_EMPTY_API_KEY", "CONFIG_TEST_MISSING_API_KEY"} {
		path := filepath.Join(dir, name+".yaml")
		body := "chat:\n  base_url: https://example.test\n  api_key: ${" + name + "}\n  model: chat\n  system_prompt_path: prompt.txt\ntext:\n  base_url: https://example.test\n  api_key: ${CONFIG_TEST_UNRELATED_SECRET}\n  model: text\n  system_prompt_path: prompt.txt\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		_, err := LoadLLM(path)
		if err == nil {
			t.Fatalf("ожидалась ошибка переменной %q", name)
		}
		message := err.Error()
		if !strings.Contains(message, name) {
			t.Fatalf("в ошибке нет имени переменной: %q", message)
		}
		if strings.Contains(message, "must-not-appear") {
			t.Fatalf("ошибка раскрыла значение секрета: %q", message)
		}
	}
}

func TestLoadEnvFilesPreservesProcessEnvironmentAndFileOrder(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.env")
	second := filepath.Join(dir, "second.env")
	if err := os.WriteFile(first, []byte("CONFIG_TEST_PROCESS=from-first\nCONFIG_TEST_FILE_ORDER=first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("CONFIG_TEST_PROCESS=from-second\nCONFIG_TEST_FILE_ORDER=second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_TEST_PROCESS", "from-process")
	if err := os.Unsetenv("CONFIG_TEST_FILE_ORDER"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("CONFIG_TEST_FILE_ORDER") })

	if err := LoadEnvFiles([]string{first, second}); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("CONFIG_TEST_PROCESS"); got != "from-process" {
		t.Fatalf("значение окружения = %q, want from-process", got)
	}
	if got := os.Getenv("CONFIG_TEST_FILE_ORDER"); got != "first" {
		t.Fatalf("приоритет dotenv-файлов = %q, want first", got)
	}
}
