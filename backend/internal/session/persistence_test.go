package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/llm"
)

func openTestStore(t *testing.T, path string, p agent.Provider) *Store {
	t.Helper()
	s, err := OpenStore(p, path, func() (agent.DialogSnapshot, error) { return testSnapshot(), nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestPersistenceRestartRestoresHistoryContextAndSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "history.json")
	p := &fakeProvider{ignoreTitle: true, complete: func(context.Context) (string, error) { return "answer", nil }}
	s := openTestStore(t, path, p)
	d, err := s.Create("browser", testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), "browser", d.ID, "first", "кофе ☕"); err != nil {
		t.Fatal(err)
	}
	other, err := s.Create("browser", testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Select("browser", d.ID); !ok || err != nil {
		t.Fatal("select", err)
	}
	s.Close()
	before := s.List("browser")
	data, err := os.ReadFile(filepath.Join(dialogDirectory(path), d.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "api_key") {
		t.Fatal("credential in JSON")
	}
	restored := openTestStore(t, path, p)
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	afterJSON, err := json.Marshal(restored.List("browser"))
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeJSON) != string(afterJSON) {
		t.Fatalf("history, title or selection changed: before=%s after=%s", beforeJSON, afterJSON)
	}
	if _, found := restored.Get("stranger", d.ID); found || len(restored.List("stranger").Dialogs) != 0 {
		t.Fatal("session leak")
	}
	if _, err := restored.Send(context.Background(), "browser", d.ID, "first", "duplicate"); err != nil {
		t.Fatal(err)
	}
	answer, err := restored.Send(context.Background(), "browser", d.ID, "second", "продолжим")
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Messages) != 4 || answer.Messages[3].ID != "m-4" {
		t.Fatalf("messages: %#v", answer.Messages)
	}
	p.mu.Lock()
	requests := append([][]llm.Message(nil), p.requests...)
	p.mu.Unlock()
	want := []llm.Message{{Role: "system", Content: testSnapshot().Chat.SystemPrompt}, {Role: "user", Content: "кофе ☕"}, {Role: "assistant", Content: "answer"}, {Role: "user", Content: "продолжим"}}
	if len(requests) != 2 || !reflect.DeepEqual(requests[1], want) {
		t.Fatalf("context: %#v", requests)
	}
	if ok, err := restored.Delete("browser", d.ID); !ok || err != nil {
		t.Fatal("delete", err)
	}
	restored.Close()
	last := openTestStore(t, path, p)
	if last.Exists(d.ID) || !last.Exists(other.ID) || last.List("browser").SelectedDialogID != "" {
		t.Fatal("delete not durable")
	}
}

func TestPersistenceRecoversPendingAndFailedMessages(t *testing.T) {
	for _, status := range []agent.Status{agent.StatusPending, agent.StatusError} {
		t.Run(string(status), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history.json")
			p := &fakeProvider{ignoreTitle: true, complete: func(context.Context) (string, error) { return "answer", nil }}
			s := openTestStore(t, path, p)
			d, err := s.Create("browser", testSnapshot())
			if err != nil {
				t.Fatal(err)
			}
			s.mu.Lock()
			stored := s.dialogLocked("browser", d.ID)
			m, err := stored.agent.Begin("client", "question")
			if err == nil && status == agent.StatusError {
				err = stored.agent.Finish(m.ID, "", errors.New("offline"))
			}
			stored.dialog.TitleStatus = "pending"
			stored.dialog.Title = "question"
			if err == nil {
				err = s.saveLocked(context.Background())
			}
			s.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			s.Close()
			restored := openTestStore(t, path, p)
			got, _ := restored.Get("browser", d.ID)
			if got.Messages[0].Status != agent.StatusError || got.TitleStatus != "error" {
				t.Fatalf("recovery: %#v", got)
			}
			if len(p.requests) != 0 {
				t.Fatal("automatic retry")
			}
			if _, err := restored.Send(context.Background(), "browser", d.ID, "new", "blocked"); err == nil {
				t.Fatal("unresolved error accepted")
			}
			answer, err := restored.Retry(context.Background(), "browser", d.ID, m.ID)
			if err != nil || len(answer.Messages) != 2 || answer.Messages[0].ClientID != "client" {
				t.Fatalf("retry: %#v, %v", answer, err)
			}
		})
	}
}

func TestPersistenceRejectsCorruptionWithoutOverwrite(t *testing.T) {
	for _, body := range []string{"{", "null", `{}`, `{"version":3,"sessions":{}}`, `{"version":1,"sessions":{"s":{"selected_dialog_id":"missing","dialogs":[]}}}`} {
		path := filepath.Join(t.TempDir(), "history.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenStore(nil, path, nil); !errors.Is(err, ErrStorage) {
			t.Fatalf("corrupt file accepted: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != body {
			t.Fatal("corrupt file overwritten")
		}
	}
}

func TestPersistenceWriteFailureBlocksProviderAndFurtherMutations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	p := &fakeProvider{ignoreTitle: true, complete: func(context.Context) (string, error) { return "answer", nil }}
	s := openTestStore(t, path, p)
	d, err := s.Create("browser", testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	// A directory at the target path makes the atomic replacement fail on all platforms.
	chatPath := filepath.Join(dialogDirectory(path), d.ID+".json")
	if err := os.Remove(chatPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(chatPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), "browser", d.ID, "client", "question"); !errors.Is(err, ErrStorage) {
		t.Fatal("write failure hidden", err)
	}
	if len(p.requests) != 0 {
		t.Fatal("provider called before durable acceptance")
	}
	if _, err := s.Create("browser", testSnapshot()); !errors.Is(err, ErrStorage) {
		t.Fatal("failed store accepted create")
	}
	if _, err := s.Delete("browser", d.ID); !errors.Is(err, ErrStorage) {
		t.Fatal("failed store accepted delete")
	}
	if s.StorageError() == nil {
		t.Fatal("failed store healthy")
	}
}

func TestEachChatHasSeparateFileAndOnlyChangedFilesAreWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	p := &fakeProvider{ignoreTitle: true, complete: func(context.Context) (string, error) { return "answer", nil }}
	s := openTestStore(t, path, p)
	first, err := s.Create("browser", testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Create("browser", testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(dialogDirectory(path), first.ID+".json")
	secondPath := filepath.Join(dialogDirectory(path), second.ID+".json")
	stamp := time.Unix(1000000000, 0)
	mark := func(path string) {
		t.Helper()
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	unchanged := func(path string) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().Equal(stamp) {
			t.Fatalf("unexpected rewrite: %s (%v)", path, err)
		}
	}
	mark(secondPath)
	mark(path)
	if _, err := s.Send(context.Background(), "browser", first.ID, "one", "only first chat"); err != nil {
		t.Fatal(err)
	}
	s.titleWG.Wait()
	unchanged(secondPath)
	unchanged(path)
	for _, name := range []string{path, secondPath} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "only first chat") || strings.Contains(string(data), "system_prompt") && name == path {
			t.Fatal("history leaked into index or other chat")
		}
	}
	mark(firstPath)
	if ok, err := s.Select("browser", first.ID); !ok || err != nil {
		t.Fatal(err)
	}
	unchanged(firstPath)
	unchanged(secondPath)
	if ok, err := s.Delete("browser", first.ID); !ok || err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(firstPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted chat file remains", err)
	}
	unchanged(secondPath)
}

func TestLegacyHistoryMigratesWithoutLosingContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	p := &fakeProvider{ignoreTitle: true, complete: func(context.Context) (string, error) { return "answer", nil }}
	s := NewStore(p)
	first, err := s.Create("browser", testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), "browser", first.ID, "one", "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("other-browser", testSnapshot()); err != nil {
		t.Fatal(err)
	}
	s.Close()
	legacy := diskState{Version: 1, Sessions: make(map[string]diskSession)}
	for sid, browser := range s.sessions {
		saved := diskSession{Selected: browser.selected, Dialogs: []diskDialog{}}
		for _, stored := range browser.dialogs {
			saved.Dialogs = append(saved.Dialogs, diskDialog{Dialog: s.dialogCopyLocked(stored), Snapshot: testSnapshot()})
		}
		legacy.Sessions[sid] = saved
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Simulate an interrupted previous migration: the old index still wins.
	if err := replaceFile(filepath.Join(dialogDirectory(path), first.ID+".json"), []byte("partial")); err != nil {
		t.Fatal(err)
	}
	migrated := openTestStore(t, path, p)
	for _, sid := range []string{"browser", "other-browser"} {
		before, _ := json.Marshal(s.List(sid))
		after, _ := json.Marshal(migrated.List(sid))
		if string(before) != string(after) {
			t.Fatal("migration lost history or selection")
		}
	}
	indexBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var index diskIndex
	if json.Unmarshal(indexBytes, &index) != nil || index.Version != 2 || strings.Contains(string(indexBytes), "messages") {
		t.Fatal("legacy index not replaced")
	}
	files, err := os.ReadDir(dialogDirectory(path))
	if err != nil || len(files) != 2 {
		t.Fatal("expected two chat files", err)
	}
	migrated.Close()
	restored := openTestStore(t, path, p)
	if _, err := restored.Send(context.Background(), "browser", first.ID, "two", "second"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.requests) != 2 || len(p.requests[1]) != 4 || p.requests[1][1].Content != "first" || p.requests[1][2].Content != "answer" {
		t.Fatalf("lost context: %#v", p.requests)
	}
}

func TestMissingOrCorruptChatBlocksStartupWithoutOverwritingIndex(t *testing.T) {
	for _, kind := range []string{"missing", "corrupt", "wrong-id", "traversal", "missing-index"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history.json")
			s := openTestStore(t, path, nil)
			d, err := s.Create("browser", testSnapshot())
			if err != nil {
				t.Fatal(err)
			}
			s.Close()
			chatPath := filepath.Join(dialogDirectory(path), d.ID+".json")
			switch kind {
			case "missing":
				err = os.Remove(chatPath)
			case "corrupt":
				err = os.WriteFile(chatPath, []byte("{"), 0o600)
			case "wrong-id":
				body, readErr := os.ReadFile(chatPath)
				if readErr != nil {
					t.Fatal(readErr)
				}
				err = os.WriteFile(chatPath, []byte(strings.ReplaceAll(string(body), d.ID, strings.Repeat("f", 32))), 0o600)
			case "traversal":
				body, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				err = os.WriteFile(path, []byte(strings.ReplaceAll(string(body), d.ID, "../outside")), 0o600)
			case "missing-index":
				err = os.Remove(path)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			if _, err := OpenStore(nil, path, func() (agent.DialogSnapshot, error) { return testSnapshot(), nil }); !errors.Is(err, ErrStorage) {
				t.Fatal("bad store accepted", err)
			}
			after, _ := os.ReadFile(path)
			if string(before) != string(after) {
				t.Fatal("index overwritten")
			}
		})
	}
}

func TestOrphanChatIsRemovedWithoutResurrectingDeletedDialog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	s := openTestStore(t, path, nil)
	d, err := s.Create("browser", testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	chatPath := filepath.Join(dialogDirectory(path), d.ID+".json")
	data, err := os.ReadFile(chatPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Delete("browser", d.ID); err != nil {
		t.Fatal(err)
	}
	s.Close()
	// Crash after index commit but before physical deletion leaves this orphan.
	if err := os.WriteFile(chatPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	restored := openTestStore(t, path, nil)
	if restored.Exists(d.ID) {
		t.Fatal("deleted chat resurrected")
	}
	if _, err := os.Stat(chatPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("orphan remains", err)
	}
}
