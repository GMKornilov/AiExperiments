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
)

func TestPersistenceServerProcess(t *testing.T) {
	path := os.Getenv("BARISTA_TEST_CONFIG")
	if path == "" {
		return
	}
	os.Args = []string{"api-server", "--config=" + path}
	main()
}

// TestServerProcessRestartRetainsMemory exercises the active API over a real
// process boundary: projects, chat history and both durable memory layers must
// be recovered by the next server process for the same browser session.
func TestServerProcessRestartRetainsMemory(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if body.Model == "memory" {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{\"global_facts\":[\"global grinder\"],\"project_facts\":[\"project beans\"]}"}}]}`)
			return
		}
		if body.Model == "validator" {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"{\"status\":\"allow\"}"}}]}`)
			return
		}
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
	_ = listener.Close()
	write("config.yaml", "addr: '"+addr+"'\nllm_config_path: llm.yaml\nhistory_path: history.json\n")
	write("prompt.txt", "BASE")
	write("memory.txt", "Return strict JSON")
	write("llm.yaml", "chat:\n  base_url: "+upstream.URL+"\n  api_key: TEST\n  model: chat\n  system_prompt_path: prompt.txt\ntext:\n  base_url: "+upstream.URL+"\n  api_key: TEST\n  model: text\n  system_prompt_path: prompt.txt\nmemory:\n  base_url: "+upstream.URL+"\n  api_key: TEST\n  model: memory\n  system_prompt_path: memory.txt\ninvariant_validation:\n  base_url: "+upstream.URL+"\n  api_key: TEST\n  model: validator\n  system_prompt_path: prompt.txt\n")
	client := &http.Client{Timeout: 5 * time.Second}
	start := func() func() {
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
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			resp, e := client.Get("http://" + addr + "/healthz")
			if e == nil {
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
	call := func(method, path, body string, out any) {
		t.Helper()
		req, e := http.NewRequest(method, "http://"+addr+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Session-ID", "browser")
		resp, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		data, e := io.ReadAll(resp.Body)
		if e != nil || resp.StatusCode != 200 {
			t.Fatalf("%s status=%d body=%s err=%v", path, resp.StatusCode, data, e)
		}
		if e = json.Unmarshal(data, out); e != nil {
			t.Fatal(e)
		}
	}
	stop := start()
	var listing restartListing
	call(http.MethodPost, "/api/projects", `{}`, &listing)
	if len(listing.Projects) != 1 {
		t.Fatalf("projects=%+v", listing)
	}
	pid := listing.SelectedProjectID
	var chat restartChat
	call(http.MethodPost, "/api/projects/"+pid+"/chats", `{}`, &chat)
	call(http.MethodPost, "/api/projects/"+pid+"/chats/"+chat.ID+"/messages", `{"client_message_id":"one","text":"beans"}`, &chat)
	stop()
	stop = start()
	defer stop()
	call(http.MethodGet, "/api/projects", ``, &listing)
	if len(listing.Projects) != 1 || listing.SelectedProjectID != pid {
		t.Fatalf("listing=%+v", listing)
	}
	var facts restartMemory
	call(http.MethodGet, "/api/projects/"+pid+"/memory", ``, &facts)
	if !reflect.DeepEqual(facts.GlobalFacts, []string{"global grinder"}) || !reflect.DeepEqual(facts.ProjectFacts, []string{"project beans"}) {
		t.Fatalf("facts=%+v", facts)
	}
	var restored restartChat
	call(http.MethodGet, "/api/projects/"+pid+"/chats/"+chat.ID, ``, &restored)
	if len(restored.Messages) != 2 {
		t.Fatalf("history=%+v", restored.Messages)
	}
}

type restartListing struct {
	Projects          []json.RawMessage `json:"projects"`
	SelectedProjectID string            `json:"selected_project_id"`
}
type restartChat struct {
	ID       string            `json:"id"`
	Messages []json.RawMessage `json:"messages"`
}
type restartMemory struct {
	GlobalFacts  []string `json:"global_facts"`
	ProjectFacts []string `json:"project_facts"`
}
