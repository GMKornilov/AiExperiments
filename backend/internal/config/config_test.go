package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLLMValidationAndRelativePrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("system"), 0o600); err != nil {
		t.Fatal(err)
	}
	valid := "base_url: https://example.test/v1\napi_key: secret\nmodel: model\nsystem_prompt_path: prompt.txt\n"
	if err := os.WriteFile(filepath.Join(dir, "llm.yaml"), []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := LoadLLM(filepath.Join(dir, "llm.yaml")); err != nil || cfg.SystemPrompt != "system" {
		t.Fatalf("LoadLLM = %#v, %v", cfg, err)
	}
	for _, body := range []string{"base_url: ftp://bad\napi_key: x\nmodel: x\nsystem_prompt_path: prompt.txt\n", "base_url: https://example.test\napi_key: x\nmodel: x\nsystem_prompt_path: missing.txt\n"} {
		if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadLLM(filepath.Join(dir, "bad.yaml")); err == nil {
			t.Fatal("ожидалась ошибка конфигурации")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("base_url: https://example.test\napi_key: x\nmodel: x\nrequest_timeout: 0s\nsystem_prompt_path: prompt.txt\n"), 0o600); err != nil {
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
