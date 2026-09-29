package statejson

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aichallenge/week_1/task_1/internal/adapters/extractjson"
	"aichallenge/week_1/task_1/internal/application/state"
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

func TestLegacyFeedbackGetsNeutralValidationMarker(t *testing.T) {
	data := `{"version":5,"sessions":{"s":{"global_facts":[],"projects":{"p":{"id":"p","title":"Project","project_facts":[],"chats":{"c":{"id":"c","title":"Coffee","title_status":"success","messages":[],"tasks":{"t":{"id":"t","title":"Task","description":"d","stage":"user_feedback","current_step":"feedback","expected_action":"user: feedback","status":"active","plan":[{"id":"p","title":"done","status":"completed"}],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}}}}}},"profiles":{},"active_profile_id":"barista"}}}`
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	_, restored, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	task := restored.Chat("s", "p", "c").Tasks["t"]
	if task.ValidationResult.Status != model.ValidationNotValidated || !task.ValidationResult.LegacyUnvalidated {
		t.Fatalf("legacy validation=%+v", task.ValidationResult)
	}
}

func TestV8EquipmentMigrationArchivesExactSourceAndWritesV9(t *testing.T) {
	data := `{"version":8,"sessions":{"s":{"global_facts":["grinder"],"projects":{"p":{"id":"p","title":"Project","project_facts":["beans"],"chats":{"c":{"id":"c","title":"Coffee","title_status":"success","messages":[{"id":"m","role":"user","text":"beans","status":"success","created_at":"2026-01-01T00:00:00Z"}],"tasks":{"t":{"id":"t","title":"Task","description":"d","stage":"clarify_input","current_step":"Collect equipment","expected_action":"user: confirm equipment","status":"active","plan":[],"equipment_confirmed":false,"equipment_context":{"intent":"recipe","status":"collecting","items":[]},"validation_result":{"status":"not_validated"},"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}}}}}},"profiles":{},"active_profile_id":"barista"}}}`
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	migrated, err := migrateV8([]byte(data))
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var decoded envelope
	if err := extractjson.Decode(migrated, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	precheck := toState(diskState{Sessions: decoded.Sessions})
	normalizeV8Clarifications(&precheck)
	if err := normalize(&precheck); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := validate(precheck); err != nil {
		t.Fatalf("validate: %v", err)
	}
	_, restored, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	task := restored.Chat("s", "p", "c").Tasks["t"]
	if task.Stage != model.TaskStageClarifyInput || task.CurrentStep != model.ClarificationStep || task.ExpectedAction != model.ClarificationExpectedAction || len(task.Plan) != 0 || task.CurrentPlanItem != "" || restored.Sessions["s"].GlobalFacts[0] != "grinder" || restored.Project("s", "p").Facts[0] != "beans" || restored.Chat("s", "p", "c").Messages[0].Text != "beans" {
		t.Fatalf("state was not preserved: %#v", task)
	}
	entries, err := filepath.Glob(path + ".backup-*")
	if err != nil || len(entries) != 1 {
		t.Fatalf("archives=%v err=%v", entries, err)
	}
	archive, err := os.ReadFile(entries[0])
	if err != nil || string(archive) != data {
		t.Fatalf("archive differs: %v %q", err, archive)
	}
	written, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(written, []byte(`"version": 9`)) || bytes.Contains(written, []byte("equipment_context")) || bytes.Contains(written, []byte("equipment_confirmed")) {
		t.Fatalf("v9 rewrite invalid: %v %s", err, written)
	}
}

func TestV8MigrationRejectsMalformedSourceWithoutWriting(t *testing.T) {
	valid := `{"version":8,"sessions":{"s":{"global_facts":[],"projects":{"p":{"id":"p","title":"P","project_facts":[],"chats":{"c":{"id":"c","title":"C","title_status":"success","messages":[],"tasks":{"t":{"id":"t","title":"T","description":"d","stage":"clarify_input","current_step":"x","expected_action":"user: x","status":"active","plan":[],"equipment_context":{"status":"collecting"},"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}}}}}},"profiles":{},"active_profile_id":"barista"}}}`
	for _, data := range []string{
		strings.Replace(valid, `"equipment_context":{"status":"collecting"}`, `"equipment_context":"wrong"`, 1),
		strings.Replace(valid, `"version":8`, `"version":8,"version":8`, 1),
	} {
		path := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Open(path); !errors.Is(err, state.ErrStorage) {
			t.Fatalf("error=%v", err)
		}
		after, _ := os.ReadFile(path)
		if string(after) != data {
			t.Fatal("malformed v8 was rewritten")
		}
		archives, _ := filepath.Glob(path + ".backup-*")
		if len(archives) != 0 {
			t.Fatal("malformed v8 was archived")
		}
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
