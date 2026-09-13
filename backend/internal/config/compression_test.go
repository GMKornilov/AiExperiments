package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSummaryConfigurationDefaultsAndValidation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("summary"), 0600); err != nil {
		t.Fatal(err)
	}
	endpoint := "  base_url: https://example.com\n  api_key: secret\n  model: test\n  system_prompt_path: prompt.txt\n"
	base := "chat:\n" + endpoint + "text:\n" + endpoint
	for _, tc := range []struct {
		name, extra string
		valid       bool
	}{
		{"legacy", "", true},
		{"defaults", "summary:\n" + endpoint, true},
		{"custom", "summary:\n" + endpoint + "  keep_last_messages: 3\n  batch_size: 4\n", true},
		{"zero", "summary:\n" + endpoint + "  keep_last_messages: 0\n", false},
		{"negative", "summary:\n" + endpoint + "  batch_size: -2\n", false},
		{"invalid endpoint", "summary:\n  model: alone\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, "llm.yaml")
			if err := os.WriteFile(path, []byte(base+tc.extra), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadLLM(path)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if tc.name == "defaults" && (cfg.Summary.KeepLastMessages != 10 || cfg.Summary.BatchSize != 10) {
				t.Fatal("wrong defaults")
			}
			if tc.name == "custom" && (cfg.Summary.KeepLastMessages != 3 || cfg.Summary.BatchSize != 4) {
				t.Fatal("wrong custom settings")
			}
		})
	}
}
