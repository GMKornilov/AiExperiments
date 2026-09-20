package statejson

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aichallenge/week_1/task_1/internal/domain/model"
)

func TestRestoreRejectsWithoutWriting(t *testing.T) {
	for _, data := range []string{`{`, `null`, `{"version":6,"sessions":{}}`, `{"version":4,"sessions":{},"future":1}`, `{"version":4,"sessions":{"s":null}}`, `{"version":4,"sessions":{"s":{"projects":{},"future":true}}}`, `{"version":4,"sessions":{},"version":5}`, `{"version":4,"sessions":{}} null`, `{"version":1,"sessions":{"s":{"projects":{}}}}`} {
		t.Run(data, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if e := os.WriteFile(path, []byte(data), 0600); e != nil {
				t.Fatal(e)
			}
			if _, _, e := Open(path); e == nil {
				t.Fatal("accepted unsafe state")
			}
			after, _ := os.ReadFile(path)
			if string(after) != data {
				t.Fatal("source overwritten")
			}
		})
	}
}
func TestCurrentVersionsRoundTrip(t *testing.T) {
	for _, version := range []string{"3", "4", "5"} {
		t.Run(version, func(t *testing.T) {
			data := `{"version":` + version + `,"sessions":{"s":{"global_facts":["grinder"],"projects":{"p":{"id":"p","title":"Project","project_facts":["beans"],"chats":{"c":{"id":"c","title":"Coffee","title_status":"success","messages":[{"id":"m","role":"user","text":"beans","status":"success","created_at":"2026-01-01T00:00:00Z"}],"tasks":{}}}}},"profiles":{"custom":{"id":"custom","name":"Name","style":"Style","constraints":"Constraints","additional_context":"Context"}},"active_profile_id":"custom","selected_project_id":"p","selected_chat_id":"c"}}}`
			path := filepath.Join(t.TempDir(), "state.json")
			os.WriteFile(path, []byte(data), 0600)
			r, value, e := Open(path)
			if e != nil {
				t.Fatal(e)
			}
			b := value.Sessions["s"]
			if b.ActiveProfileID != "custom" || b.GlobalFacts[0] != "grinder" || value.Chat("s", "p", "c").Messages[0].Text != "beans" {
				t.Fatal("data lost")
			}
			if e = r.Save(value); e != nil {
				t.Fatal(e)
			}
			_, again, e := Open(path)
			if e != nil {
				t.Fatal(e)
			}
			if again.Sessions["s"].Profiles["custom"].AdditionalContext != "Context" {
				t.Fatal("profile lost")
			}
			persisted, _ := os.ReadFile(path)
			if bytes.Contains(persisted, []byte(`"api_key"`)) {
				t.Fatal("credentials persisted")
			}
		})
	}
}
func TestHistoricalResetArchivesIndexAndDialogs(t *testing.T) {
	for _, version := range []string{"1", "2"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			key := "dialogs"
			if version == "2" {
				key = "dialog_ids"
			}
			data := `{"version":` + version + `,"sessions":{"s":{"selected_dialog_id":"","` + key + `":[]}}}`
			os.WriteFile(path, []byte(data), 0600)
			os.Mkdir(filepath.Join(dir, "state"), 0700)
			os.WriteFile(filepath.Join(dir, "state", "old.json"), []byte("historical"), 0600)
			_, v, e := Open(path)
			if e != nil || len(v.Sessions) != 0 {
				t.Fatalf("reset %v", e)
			}
			backup, _ := os.ReadFile(path + ".legacy-backup")
			if string(backup) != data {
				t.Fatal("backup missing")
			}
			old, _ := os.ReadFile(filepath.Join(dir, "state.legacy-backup", "old.json"))
			if string(old) != "historical" {
				t.Fatal("dialog backup missing")
			}
			if _, e = os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(e) {
				t.Fatal("legacy directory still live")
			}
		})
	}
}
func TestPendingTitleNormalizesWithoutSecondCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	r, v, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	b := v.Browser("s")
	b.Projects["p"] = &model.ProjectState{ID: "p", Title: "Project", Facts: []string{}, Chats: map[string]*model.ChatState{"c": {ID: "c", Title: "первый ввод", TitleStatus: "pending", Messages: []model.Message{}, Tasks: map[string]*model.Task{}}}}
	if e = r.Save(v); e != nil {
		t.Fatal(e)
	}
	_, v, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if c := v.Chat("s", "p", "c"); c.TitleStatus != "fallback" || c.Title != "первый ввод" {
		t.Fatal("claim was lost")
	}
}
func TestAtomicWriteFailurePreservesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	r, v, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(path)
	r.path = filepath.Join(path, "invalid")
	v.Browser("new")
	if e = r.Save(v); e == nil {
		t.Fatal("expected storage failure")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("source changed")
	}
	var data map[string]any
	if json.Unmarshal(after, &data) != nil || strings.Contains(string(after), "new") {
		t.Fatal("partial file")
	}
}
