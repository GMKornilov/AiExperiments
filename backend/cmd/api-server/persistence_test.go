package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/llm"
	"aichallenge/week_1/task_1/internal/session"
)

func TestPersistenceServerProcess(t *testing.T) {
	path := os.Getenv("BARISTA_TEST_CONFIG")
	if path == "" {
		return
	}
	os.Args = []string{"api-server", "--config=" + path}
	main()
}

// This test kills the actual server process, starts a new one, and captures the
// next upstream HTTP request. No in-memory store can survive this boundary.
func TestServerProcessRestartRetainsAgentContext(t *testing.T) {
	var mu sync.Mutex
	var requests [][]llm.Message
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string        `json:"model"`
			Messages []llm.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if body.Model == "chat" {
			mu.Lock()
			requests = append(requests, body.Messages)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"answer"}}]}`)
	}))
	defer upstream.Close()
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	write("config.yaml", "addr: '"+addr+"'\nllm_config_path: llm.yaml\nhistory_path: history.json\n")
	write("prompt.txt", "ORIGINAL SYSTEM")
	write("llm.yaml", "chat:\n  base_url: "+upstream.URL+"\n  api_key: TEST-CREDENTIAL\n  model: chat\n  system_prompt_path: prompt.txt\ntext:\n  base_url: "+upstream.URL+"\n  api_key: TEST-TITLE-CREDENTIAL\n  model: title\n  system_prompt_path: prompt.txt\n")
	client := &http.Client{Timeout: 5 * time.Second}
	start := func() func() {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestPersistenceServerProcess$")
		cmd.Env = append(os.Environ(), "BARISTA_TEST_CONFIG="+filepath.Join(dir, "config.yaml"))
		var logs bytes.Buffer
		cmd.Stdout = &logs
		cmd.Stderr = &logs
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		var once sync.Once
		stop := func() { once.Do(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }) }
		t.Cleanup(stop)
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := client.Get("http://" + addr + "/healthz")
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == 204 {
					return stop
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		stop()
		t.Fatalf("server not ready: %s", logs.String())
		return stop
	}
	call := func(method, path, body string) []byte {
		t.Helper()
		req, err := http.NewRequest(method, "http://"+addr+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Session-ID", "browser")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: status=%d body=%s err=%v", path, resp.StatusCode, data, err)
		}
		return data
	}
	stop := start()
	var dialog session.Dialog
	if err := json.Unmarshal(call("POST", "/api/dialogs", ""), &dialog); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/dialogs/"+dialog.ID+"/messages", `{"client_message_id":"one","text":"first"}`)
	stop()
	write("prompt.txt", "CHANGED SYSTEM")
	stop = start()
	defer stop()
	var listing session.Listing
	if err := json.Unmarshal(call("GET", "/api/dialogs", ""), &listing); err != nil {
		t.Fatal(err)
	}
	if listing.SelectedDialogID != dialog.ID || len(listing.Dialogs) != 1 || len(listing.Dialogs[0].Messages) != 2 {
		t.Fatalf("history lost: %#v", listing)
	}
	call("POST", "/api/dialogs/"+dialog.ID+"/messages", `{"client_message_id":"two","text":"second"}`)
	mu.Lock()
	defer mu.Unlock()
	want := []llm.Message{{Role: "system", Content: "ORIGINAL SYSTEM"}, {Role: "user", Content: "first"}, {Role: "assistant", Content: "answer"}, {Role: "user", Content: "second"}}
	if len(requests) != 2 || !reflect.DeepEqual(requests[1], want) {
		t.Fatalf("context lost: %#v", requests)
	}
}
