package configsnapshot

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoaderKeepsInvariantValidationTimeoutIndependent(t *testing.T) {
	dir := t.TempDir()
	prompt := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(prompt, []byte("system"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "llm.yaml")
	body := "chat:\n  base_url: https://example.test\n  api_key: key\n  model: chat\n  request_timeout: 30s\n  system_prompt_path: prompt.txt\ntext:\n  base_url: https://example.test\n  api_key: key\n  model: text\n  request_timeout: 30s\n  system_prompt_path: prompt.txt\ninvariant_validation:\n  base_url: https://example.test\n  api_key: key\n  model: validator\n  request_timeout: 60s\n  system_prompt_path: prompt.txt\n"
	if err := os.WriteFile(config, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	snapshot, err := Loader(config)()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.InvariantValidation == nil {
		t.Fatal("invariant validation snapshot is missing")
	}
	if got, want := snapshot.Chat.Timeout, 30*time.Second; got != want {
		t.Fatalf("chat timeout=%s, want %s", got, want)
	}
	if got, want := snapshot.InvariantValidation.Snapshot.Timeout, 60*time.Second; got != want {
		t.Fatalf("invariant timeout=%s, want %s", got, want)
	}
}
