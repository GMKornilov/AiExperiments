package acceptance_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/adapters/extractjson"
	"aichallenge/week_1/task_1/internal/adapters/statejson"
	"aichallenge/week_1/task_1/internal/application/completion"
	"aichallenge/week_1/task_1/internal/application/conversation"
	"aichallenge/week_1/task_1/internal/application/invariant"
	"aichallenge/week_1/task_1/internal/application/state"
	"aichallenge/week_1/task_1/internal/application/taskflow"
	"aichallenge/week_1/task_1/internal/application/workspace"
	"aichallenge/week_1/task_1/internal/domain/model"
)

type memoryDisk struct{ value model.State }

func (d *memoryDisk) Save(v model.State) error { d.value = v.Clone(); return nil }

type faultDisk struct {
	base state.Committer
	fail atomic.Bool
}

func (d *faultDisk) Save(v model.State) error {
	if d.fail.Load() {
		return errors.New("disk unavailable")
	}
	return d.base.Save(v)
}

type provider struct {
	titleRaw     *string
	mu           sync.Mutex
	calls        []string
	messages     map[string][]completion.Message
	raw          string
	raws         []string
	fail         string
	block        string
	entered      chan struct{}
	release      chan struct{}
	ignoreCancel bool
}

func (p *provider) Complete(ctx context.Context, purpose string, m []completion.Message) (string, error) {
	p.mu.Lock()
	p.calls = append(p.calls, purpose)
	if p.messages == nil {
		p.messages = map[string][]completion.Message{}
	}
	p.messages[purpose] = append([]completion.Message{}, m...)
	titleRaw := p.titleRaw
	raw, fail, block, entered, release, ignore := p.raw, p.fail, p.block, p.entered, p.release, p.ignoreCancel
	if purpose == "task_step" && len(p.raws) > 0 {
		raw = p.raws[0]
		p.raws = p.raws[1:]
	}
	p.mu.Unlock()
	if purpose == block {
		select {
		case entered <- struct{}{}:
		default:
		}
		if ignore {
			<-release
		} else {
			select {
			case <-release:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}
	if purpose == fail {
		return "", &completion.Error{Category: "network"}
	}
	switch purpose {
	case "title":
		if titleRaw != nil {
			return *titleRaw, nil
		}
		return "Title", nil
	case "memory_extractor":
		return `{"global_facts":["grinder"],"project_facts":["beans"]}`, nil
	case "task_step":
		return raw, nil
	default:
		return "answer", nil
	}
}
func (p *provider) set(raw, fail, block string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.raw = raw
	p.raws = nil
	p.fail = fail
	p.block = block
	p.entered = make(chan struct{}, 4)
	p.release = make(chan struct{})
}
func (p *provider) setSequence(raws ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.raws = append([]string{}, raws...)
	p.fail = ""
	p.block = ""
	p.entered = make(chan struct{}, 4)
	p.release = make(chan struct{})
}
func (p *provider) count(purpose string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, v := range p.calls {
		if v == purpose {
			n++
		}
	}
	return n
}
func proposal(stage string) string {
	plan := []map[string]string{}
	current := ""
	expected := "user: Какова модель кофемолки?"
	questions := []string{"Какова модель кофемолки?"}
	if stage != "clarify_input" {
		plan = []map[string]string{{"id": "grinder", "title": "Узнать информацию о кофемолке", "status": "current"}, {"id": "recipe", "title": "Подобрать рецепт", "status": "pending"}}
		current = "grinder"
		expected = "user: подтвердить следующий шаг"
		questions = []string{}
	}
	if stage == "execution" {
		plan[0]["status"] = "completed"
		plan[1]["status"] = "current"
		current = "recipe"
	}
	if stage == "user_feedback" {
		for _, p := range plan {
			p["status"] = "completed"
		}
		current = ""
		expected = "user: Оцените рецепт"
	}
	body := map[string]any{"output": "Ответ шага", "understanding": "Подобрать эспрессо", "questions": questions, "stage": stage, "current_step": "Узнать информацию о кофемолке", "expected_action": expected, "status": "active", "plan": plan, "current_plan_item": current, "goal_confirmed": stage != "clarify_input", "positive_feedback": false}
	data, _ := json.Marshal(body)
	return string(data)
}
func proposalValues(stage, output, expected string) string {
	var value map[string]any
	_ = json.Unmarshal([]byte(proposal(stage)), &value)
	value["output"] = output
	value["expected_action"] = expected
	data, _ := json.Marshal(value)
	return string(data)
}

type fixture struct {
	ws             *workspace.Service
	chat           *conversation.Service
	tasks          *taskflow.Service
	state          *state.Manager
	disk           *faultDisk
	provider       *provider
	titles         *conversation.Titles
	pid, cid, path string
	sequence       atomic.Int64
}

func setup(t *testing.T, kind string) *fixture {
	t.Helper()
	f := &fixture{provider: &provider{}}
	f.provider.set(proposal("clarify_input"), "", "")
	var disk state.Committer
	initial := model.State{Sessions: map[string]*model.Browser{}}
	if kind == "json" {
		f.path = filepath.Join(t.TempDir(), "state.json")
		var err error
		disk, initial, err = statejson.Open(f.path)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		disk = &memoryDisk{value: initial}
	}
	f.disk = &faultDisk{base: disk}
	f.state = state.New(initial, f.disk)
	id := func() string { return fmt.Sprintf("id-%d", f.sequence.Add(1)) }
	f.titles = conversation.NewTitles(f.state, f.provider, "TITLE", time.Now)
	extractor := extractjson.NewExtractor(f.provider, "MEMORY")
	settings := conversation.Settings{Prompt: "BASE", Window: 3}
	f.ws = workspace.New(f.state, f.state, id, time.Now)
	allow := invariant.Set{}
	f.chat = conversation.New(f.state, f.provider, extractor, f.titles, settings, id, time.Now, allow)
	f.tasks = taskflow.New(f.state, f.provider, extractor, extractjson.ProposalDecoder{}, model.TaskRouter{}, f.titles, settings, id, time.Now, allow)
	p, err := f.ws.CreateProject("s", "Project")
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.ws.CreateChat("s", p.ID, "Chat")
	if err != nil {
		t.Fatal(err)
	}
	f.pid = p.ID
	f.cid = c.ID
	t.Cleanup(func() { f.state.Close(); f.titles.Close() })
	return f
}
func (f *fixture) input(text, id, tid string) (model.Chat, error) {
	c, _, e := f.tasks.TaskInput(context.Background(), "s", f.pid, f.cid, text, tid, id)
	return c, e
}
func (f *fixture) stored() model.Chat { c, _ := f.ws.GetChat("s", f.pid, f.cid); return c }
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("barrier not reached")
	}
}
func settleTitles(t *testing.T, f *fixture) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f.stored().TitleStatus != "pending" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("title did not finish")
}

func TestRepositoryContracts(t *testing.T) {
	for _, kind := range []string{"fake", "json"} {
		t.Run(kind, func(t *testing.T) {
			t.Run("ownership-and-copy", func(t *testing.T) {
				f := setup(t, kind)
				if _, ok := f.ws.GetChat("other", f.pid, f.cid); ok {
					t.Fatal("owner leak")
				}
				snap := f.state.Snapshot()
				snap.Sessions["s"].Projects[f.pid].Title = "mutated"
				if f.ws.List("s").Projects[0].Title != "Project" {
					t.Fatal("aliased read")
				}
			})
			t.Run("all-workspace-failures-rollback", func(t *testing.T) {
				f := setup(t, kind)
				_, err := f.chat.Send(context.Background(), "s", f.pid, f.cid, "normal", "beans")
				if err != nil {
					t.Fatal(err)
				}
				settleTitles(t, f)
				before := f.state.Snapshot()
				f.disk.fail.Store(true)
				ops := []func() error{func() error { _, e := f.ws.CreateProject("s", "bad"); return e }, func() error { _, e := f.ws.RenameProject("s", f.pid, "bad"); return e }, func() error { return f.ws.ClearGlobal("s") }, func() error { return f.ws.ClearProject("s", f.pid) }, func() error { return f.ws.DeleteChat("s", f.pid, f.cid) }, func() error { return f.ws.DeleteProject("s", f.pid) }, func() error { _, e := f.ws.CreateProfile("s", "custom", "a", "b", "c"); return e }, func() error { _, e := f.ws.SelectProfile("s", "coffee-equipment"); return e }}
				for i, op := range ops {
					if !errors.Is(op(), state.ErrStorage) {
						t.Fatalf("operation %d did not fail", i)
					}
					if !reflect.DeepEqual(before, f.state.Snapshot()) {
						t.Fatalf("operation %d published failed state", i)
					}
				}
			})
			t.Run("normal-vs-task-extractor-failure", func(t *testing.T) {
				f := setup(t, kind)
				f.provider.set(proposal("clarify_input"), "memory_extractor", "")
				c, e := f.chat.Send(context.Background(), "s", f.pid, f.cid, "normal", "beans")
				if e != nil || len(c.Messages) != 2 || c.MemoryStatus != "error" {
					t.Fatalf("normal %+v %v", c, e)
				}
				_, e = f.input("эспрессо", "task-1", "")
				if e == nil || len(f.stored().Messages) != 2 || f.stored().Tasks[0].Stage != model.TaskStageClarifyInput {
					t.Fatalf("partial task %+v %v", f.stored(), e)
				}
			})
			t.Run("task-proposal-and-idempotency", func(t *testing.T) {
				f := setup(t, kind)
				c, e := f.input("подобрать эспрессо", "one", "")
				if e != nil {
					t.Fatal(e)
				}
				tid := c.Tasks[0].ID
				if c.Tasks[0].Stage != model.TaskStageClarifyInput || len(c.Tasks[0].Plan) != 0 {
					t.Fatal("first step skipped clarify")
				}
				f.provider.set(proposal("research_input_data"), "", "")
				c, e = f.input("Кофемолка Niche Zero, зерно обжарено вчера", "two", tid)
				if e != nil {
					t.Fatal(e)
				}
				if c.Tasks[0].Plan[0].Title != "Узнать информацию о кофемолке" {
					t.Fatal("template plan")
				}
				count := f.provider.count("task_step")
				c, e = f.input("Кофемолка Niche Zero, зерно обжарено вчера", "two", tid)
				if e != nil || len(c.Messages) != 4 || f.provider.count("task_step") != count {
					t.Fatal("lost-response replay duplicated step")
				}
				_, e = f.input("different", "two", tid)
				if !errors.Is(e, model.ErrValidation) {
					t.Fatal("id collision accepted")
				}
				for i, stage := range []string{"execution", "user_feedback"} {
					f.provider.set(proposal(stage), "", "")
					c, e = f.input("дальше", fmt.Sprintf("next-%d", i), tid)
					if e != nil {
						t.Fatal(e)
					}
				}
				done := strings.ReplaceAll(proposal("user_feedback"), `"status":"active"`, `"status":"done"`)
				done = strings.ReplaceAll(done, `"positive_feedback":false`, `"positive_feedback":true`)
				done = strings.ReplaceAll(done, `"expected_action":"user: Оцените рецепт"`, `"expected_action":"none"`)
				f.provider.set(done, "", "")
				c, e = f.input("да, подходит", "done", tid)
				if e != nil || c.Tasks[0].Status != model.TaskStatusDone {
					t.Fatalf("done %v %+v", e, c)
				}
				if _, e = f.tasks.PauseTask("s", f.pid, f.cid, tid); !errors.Is(e, model.ErrValidation) {
					t.Fatal("paused done task")
				}
			})
			for _, block := range []string{"task_step", "memory_extractor"} {
				t.Run("pause-before-commit-"+block, func(t *testing.T) {
					f := setup(t, kind)
					c, e := f.input("эспрессо", "one", "")
					if e != nil {
						t.Fatal(e)
					}
					settleTitles(t, f)
					tid := c.Tasks[0].ID
					before := f.state.Snapshot()
					f.provider.set(proposal("research_input_data"), "", block)
					f.provider.ignoreCancel = true
					result := make(chan error, 1)
					go func() { _, e := f.input("кофемолка Niche", "two", tid); result <- e }()
					await(t, f.provider.entered)
					if len(f.stored().Messages) != 2 {
						t.Fatal("early output")
					}
					paused, e := f.tasks.PauseTask("s", f.pid, f.cid, tid)
					if e != nil || paused.Tasks[0].Status != model.TaskStatusPaused {
						t.Fatalf("pause %v", e)
					}
					close(f.provider.release)
					if e = <-result; e != nil {
						t.Fatal(e)
					}
					after := f.state.Snapshot()
					if len(f.stored().Messages) != 2 || f.stored().Tasks[0].Status != model.TaskStatusPaused || !reflect.DeepEqual(before.Sessions["s"].GlobalFacts, after.Sessions["s"].GlobalFacts) {
						t.Fatal("late result won pause")
					}
					f.provider.set(proposal("research_input_data"), "", "")
					c, _, e = f.tasks.Resume(context.Background(), "s", f.pid, f.cid, tid, "", "")
					if e != nil || len(c.Messages) != 4 || c.Tasks[0].Stage != model.TaskStageResearchInputData {
						t.Fatalf("resume %v %+v", e, c)
					}
				})
			}
			t.Run("commit-before-pause", func(t *testing.T) {
				f := setup(t, kind)
				c, e := f.input("эспрессо", "one", "")
				if e != nil {
					t.Fatal(e)
				}
				tid := c.Tasks[0].ID
				f.provider.set(proposal("research_input_data"), "", "")
				_, e = f.input("данные", "two", tid)
				if e != nil {
					t.Fatal(e)
				}
				c, e = f.tasks.PauseTask("s", f.pid, f.cid, tid)
				if e != nil || len(c.Messages) != 4 || c.Tasks[0].Stage != model.TaskStageResearchInputData {
					t.Fatal("confirmed output lost")
				}
			})
			t.Run("task-storage-failure-no-output", func(t *testing.T) {
				f := setup(t, kind)
				c, e := f.input("эспрессо", "one", "")
				if e != nil {
					t.Fatal(e)
				}
				settleTitles(t, f)
				tid := c.Tasks[0].ID
				f.provider.set(proposal("research_input_data"), "", "memory_extractor")
				result := make(chan error, 1)
				go func() { _, e := f.input("данные", "two", tid); result <- e }()
				await(t, f.provider.entered)
				f.disk.fail.Store(true)
				close(f.provider.release)
				if !errors.Is(<-result, state.ErrStorage) {
					t.Fatal("expected storage error")
				}
				c = f.stored()
				if len(c.Messages) != 2 || c.Tasks[0].Stage != model.TaskStageClarifyInput {
					t.Fatal("failed save published output")
				}
			})
			t.Run("delete-inflight", func(t *testing.T) {
				f := setup(t, kind)
				f.provider.set("", "", "chat")
				result := make(chan error, 1)
				go func() { _, e := f.chat.Send(context.Background(), "s", f.pid, f.cid, "normal", "beans"); result <- e }()
				await(t, f.provider.entered)
				if e := f.ws.DeleteChat("s", f.pid, f.cid); e != nil {
					t.Fatal(e)
				}
				if !errors.Is(<-result, model.ErrNotFound) {
					t.Fatal("deleted completion accepted")
				}
				if _, ok := f.ws.GetChat("s", f.pid, f.cid); ok {
					t.Fatal("resurrection")
				}
			})
			t.Run("title-after-accepted-commit", func(t *testing.T) {
				f := setup(t, kind)
				f.provider.set("", "chat", "chat")
				result := make(chan error, 1)
				go func() { _, e := f.chat.Send(context.Background(), "s", f.pid, f.cid, "normal", "beans"); result <- e }()
				await(t, f.provider.entered)
				if f.provider.count("title") != 0 || f.stored().TitleStatus != "idle" {
					t.Fatal("title started before accepted commit")
				}
				close(f.provider.release)
				if <-result == nil {
					t.Fatal("expected main failure")
				}
				f.provider.set("", "", "")
				c := f.stored()
				if _, e := f.chat.Retry(context.Background(), "s", f.pid, f.cid, c.Messages[0].ID); e != nil {
					t.Fatal(e)
				}
				settleTitles(t, f)
				if f.provider.count("title") != 1 {
					t.Fatal("title was not generated after accepted retry")
				}
			})
		})
	}
}

func TestRestartPausesOnlyUnconfirmedTask(t *testing.T) {
	f := setup(t, "json")
	c, e := f.input("эспрессо", "one", "")
	if e != nil {
		t.Fatal(e)
	}
	settleTitles(t, f)
	tid := c.Tasks[0].ID
	f.provider.set(proposal("research_input_data"), "", "memory_extractor")
	result := make(chan error, 1)
	go func() { _, e := f.input("данные", "two", tid); result <- e }()
	await(t, f.provider.entered)
	// Open another repository from the durable in-flight snapshot before any
	// orderly shutdown cleanup: this is the exact state surviving a killed process.
	_, restored, e := statejson.Open(f.path)
	if e != nil {
		t.Fatal(e)
	}
	saved := restored.Chat("s", f.pid, f.cid)
	if restored.Sessions["s"].Pending != nil || saved.Tasks[tid].Status != model.TaskStatusPaused || len(saved.Messages) != 2 || saved.Operations["two"].Status != "paused" {
		t.Fatalf("bad recovery %+v", saved)
	}
	f.state.Close()
	if <-result == nil {
		t.Fatal("closed process accepted step")
	}
}

func TestRetryAfterFinalStorageFailure(t *testing.T) {
	f := setup(t, "json")
	c, e := f.input("эспрессо", "first", "")
	if e != nil {
		t.Fatal(e)
	}
	settleTitles(t, f)
	tid := c.Tasks[0].ID
	f.provider.set(proposal("research_input_data"), "", "memory_extractor")
	result := make(chan error, 1)
	go func() { _, e := f.input("данные", "same", tid); result <- e }()
	await(t, f.provider.entered)
	f.disk.fail.Store(true)
	close(f.provider.release)
	if !errors.Is(<-result, state.ErrStorage) {
		t.Fatal("expected failure")
	}
	f.disk.fail.Store(false)
	f.provider.set(proposal("research_input_data"), "", "")
	c, e = f.input("данные", "same", tid)
	if e != nil || len(c.Messages) != 4 {
		t.Fatalf("manual retry failed %v %+v", e, c)
	}
}
func TestPromptWindowProfileAndExtractorPrivacy(t *testing.T) {
	f := setup(t, "fake")
	_, e := f.ws.CreateProfile("s", "Custom", "STYLE-SECRET", "CONSTRAINT-SECRET", "CONTEXT-SECRET")
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		_, e = f.chat.Send(context.Background(), "s", f.pid, f.cid, fmt.Sprint(i), fmt.Sprint("input-", i))
		if e != nil {
			t.Fatal(e)
		}
	}
	f.provider.mu.Lock()
	defer f.provider.mu.Unlock()
	m := f.provider.messages["chat"]
	if len(m) != 4 || m[1].Content != "input-1" || m[3].Content != "input-2" {
		t.Fatalf("N=3 window: %+v", m)
	}
	for _, text := range []string{"STYLE-SECRET", "CONSTRAINT-SECRET", "CONTEXT-SECRET", "active profile > current chat > project memory > global memory", "grinder", "beans"} {
		if !strings.Contains(m[0].Content, text) {
			t.Fatalf("missing prompt %s", text)
		}
	}
	for _, m := range f.provider.messages["memory_extractor"] {
		if strings.Contains(m.Content, "SECRET") {
			t.Fatal("profile leaked to extractor")
		}
	}
}
func TestProfileValidationAndDeleteRollback(t *testing.T) {
	f := setup(t, "json")
	for _, v := range [][4]string{{"", "s", "c", "a"}, {strings.Repeat("я", 61), "s", "c", "a"}, {"name", " ", "c", "a"}, {"name", "s", " ", "a"}, {"name", "s", "c", " "}} {
		if _, e := f.ws.CreateProfile("s", v[0], v[1], v[2], v[3]); !errors.Is(e, model.ErrValidation) {
			t.Fatal("invalid profile accepted")
		}
	}
	p, e := f.ws.CreateProfile("s", " Custom ", "s", "c", "a")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.ws.CreateProfile("s", "CUSTOM", "s", "c", "a"); !errors.Is(e, model.ErrValidation) {
		t.Fatal("duplicate profile")
	}
	f.disk.fail.Store(true)
	if _, e = f.ws.DeleteProfile("s", p.ActiveProfileID); !errors.Is(e, state.ErrStorage) {
		t.Fatal("delete failure")
	}
	if f.ws.ListProfiles("s").ActiveProfileID != p.ActiveProfileID {
		t.Fatal("failed deletion changed active")
	}
	f.disk.fail.Store(false)
	if _, e = f.ws.DeleteProfile("s", p.ActiveProfileID); e != nil {
		t.Fatal(e)
	}
	if f.ws.ListProfiles("s").ActiveProfileID != model.BaristaProfileID {
		t.Fatal("default not restored")
	}
	if len(f.ws.ListProfiles("other").Profiles) != 2 {
		t.Fatal("profile leak")
	}
}

func TestOrdinaryResponseWaitsForMemoryAndTitleDoesNotBlock(t *testing.T) {
	f := setup(t, "fake")
	f.provider.set("", "", "memory_extractor")
	result := make(chan error, 1)
	go func() { _, e := f.chat.Send(context.Background(), "s", f.pid, f.cid, "one", "beans"); result <- e }()
	await(t, f.provider.entered)
	if len(f.stored().Messages) != 0 || f.stored().MemoryStatus != "updating" {
		t.Fatal("main result published before extraction")
	}
	select {
	case <-result:
		t.Fatal("returned before memory")
	default:
	}
	close(f.provider.release)
	if e := <-result; e != nil {
		t.Fatal(e)
	}
	second, e := f.ws.CreateChat("s", f.pid, "Second")
	if e != nil {
		t.Fatal(e)
	}
	f.provider.set("", "", "title")
	_, e = f.chat.Send(context.Background(), "s", f.pid, second.ID, "two", "beans")
	if e != nil {
		t.Fatal(e)
	}
	await(t, f.provider.entered)
	close(f.provider.release)
}
func TestRoutingCandidatesDoNotCallProvider(t *testing.T) {
	f := setup(t, "fake")
	a, e := f.input("эспрессо помол", "one", "")
	if e != nil {
		t.Fatal(e)
	}
	firstID := a.Tasks[0].ID
	// Inject two established tasks to exercise an ambiguous policy result without
	// relying on model text or introducing a second classifier completion.
	err := f.state.Update(func(root *model.State) error {
		c := root.Chat("s", f.pid, f.cid)
		c.Tasks["second"] = &model.Task{ID: "second", Title: "эспрессо зерно", Description: "эспрессо", Stage: model.TaskStageClarifyInput, Status: model.TaskStatusActive, Plan: []model.TaskPlanItem{}, CurrentStep: model.ClarificationStep, ExpectedAction: model.ClarificationExpectedAction}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before := f.provider.count("task_step")
	_, candidates, e := f.tasks.TaskInput(context.Background(), "s", f.pid, f.cid, "эспрессо", "", "ambiguous")
	if e != nil || len(candidates) != 2 || f.provider.count("task_step") != before {
		t.Fatalf("candidates %v %+v", e, candidates)
	}
	f.provider.set(proposal("research_input_data"), "", "")
	_, _, e = f.tasks.TaskInput(context.Background(), "s", f.pid, f.cid, "эспрессо", firstID, "ambiguous")
	if e != nil {
		t.Fatal(e)
	}
}
func TestResumeLostResponseReplaysSameOperation(t *testing.T) {
	f := setup(t, "json")
	c, e := f.input("эспрессо", "one", "")
	if e != nil {
		t.Fatal(e)
	}
	tid := c.Tasks[0].ID
	_, e = f.tasks.PauseTask("s", f.pid, f.cid, tid)
	if e != nil {
		t.Fatal(e)
	}
	f.provider.set(proposal("research_input_data"), "", "")
	c, _, e = f.tasks.Resume(context.Background(), "s", f.pid, f.cid, tid, "", "resume-id")
	if e != nil {
		t.Fatal(e)
	}
	calls := f.provider.count("task_step")
	again, _, e := f.tasks.Resume(context.Background(), "s", f.pid, f.cid, tid, "", "resume-id")
	if e != nil || len(again.Messages) != len(c.Messages) || f.provider.count("task_step") != calls {
		t.Fatalf("resume duplicated %v", e)
	}
}

func TestPauseResumeAcceptsAutonomousForwardStage(t *testing.T) {
	f := setup(t, "json")
	chat, err := f.input("подобрать эспрессо", "clarify", "")
	if err != nil {
		t.Fatal(err)
	}
	settleTitles(t, f)
	tid := chat.Tasks[0].ID
	f.provider.set(proposal("research_input_data"), "", "task_step")
	result := make(chan error, 1)
	go func() {
		_, runErr := f.input("Niche Zero, цель подтверждаю", "paused-input", tid)
		result <- runErr
	}()
	await(t, f.provider.entered)
	paused, err := f.tasks.PauseTask("s", f.pid, f.cid, tid)
	if err != nil || paused.Tasks[0].Status != model.TaskStatusPaused {
		t.Fatalf("pause failed: %v %+v", err, paused)
	}
	if err = <-result; err != nil {
		t.Fatal(err)
	}
	f.provider.setSequence(`{}`)
	_, _, err = f.tasks.Resume(context.Background(), "s", f.pid, f.cid, tid, "", "")
	if completion.Category(err) != "invalid_response" || f.stored().Tasks[0].Status != model.TaskStatusPaused {
		t.Fatalf("failed resume was not kept paused: %v %+v", err, f.stored().Tasks[0])
	}
	beforeTaskCalls := f.provider.count("task_step")
	beforeMemoryCalls := f.provider.count("memory_extractor")
	f.provider.setSequence(
		proposalValues("research_input_data", "Данные собраны", "agent: подготовить результат"),
		proposalValues("user_feedback", "Результат подготовлен", "user: оценить результат"),
	)
	chat, _, err = f.tasks.Resume(context.Background(), "s", f.pid, f.cid, tid, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if f.provider.count("task_step")-beforeTaskCalls != 2 || f.provider.count("memory_extractor")-beforeMemoryCalls != 1 {
		t.Fatalf("unexpected resume calls: task=%d memory=%d", f.provider.count("task_step")-beforeTaskCalls, f.provider.count("memory_extractor")-beforeMemoryCalls)
	}
	if chat.Tasks[0].Stage != model.TaskStageUserFeedback || chat.Tasks[0].Status != model.TaskStatusActive || len(chat.Messages) != 4 || chat.Messages[3].Text != "Данные собраны\n\nРезультат подготовлен" {
		t.Fatalf("unexpected resume result: %+v", chat)
	}
}

func TestAutonomousAgentStepsCommitOnce(t *testing.T) {
	for _, kind := range []string{"fake", "json"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t, kind)
			chat, err := f.input("подобрать эспрессо", "clarify", "")
			if err != nil {
				t.Fatal(err)
			}
			tid := chat.Tasks[0].ID
			beforeTaskCalls := f.provider.count("task_step")
			beforeMemoryCalls := f.provider.count("memory_extractor")
			f.provider.setSequence(
				proposalValues("research_input_data", "Исследование", "agent: выполнить план"),
				proposalValues("execution", "Выполнение", "agent: подготовить итог"),
				proposalValues("user_feedback", "Итог", "user: оценить результат"),
			)
			chat, err = f.input("Niche Zero, цель подтверждаю", "autonomous", tid)
			if err != nil {
				t.Fatal(err)
			}
			if f.provider.count("task_step")-beforeTaskCalls != 3 || f.provider.count("memory_extractor")-beforeMemoryCalls != 1 {
				t.Fatalf("unexpected calls: task=%d memory=%d", f.provider.count("task_step")-beforeTaskCalls, f.provider.count("memory_extractor")-beforeMemoryCalls)
			}
			if chat.Tasks[0].Stage != model.TaskStageUserFeedback || len(chat.Messages) != 4 || chat.Messages[3].Text != "Исследование\n\nВыполнение\n\nИтог" {
				t.Fatalf("unexpected autonomous result: %+v", chat)
			}
			f.provider.mu.Lock()
			lastPrompt := f.provider.messages["task_step"][0].Content
			f.provider.mu.Unlock()
			if !strings.Contains(lastPrompt, `"prior_outputs":["Исследование","Выполнение"]`) {
				t.Fatal("prior autonomous outputs were not passed to the next call")
			}
		})
	}
}

func TestAutonomousAgentStepLimitRollsBack(t *testing.T) {
	f := setup(t, "json")
	chat, err := f.input("подобрать эспрессо", "clarify", "")
	if err != nil {
		t.Fatal(err)
	}
	tid := chat.Tasks[0].ID
	before := f.stored()
	beforeTaskCalls := f.provider.count("task_step")
	beforeMemoryCalls := f.provider.count("memory_extractor")
	f.provider.set(proposalValues("research_input_data", "Бесконечный шаг", "agent: продолжать"), "", "")
	limited, err := f.input("цель подтверждаю", "endless", tid)
	if err != nil {
		t.Fatalf("limit refusal failed: %v", err)
	}
	if f.provider.count("task_step")-beforeTaskCalls != maxAutonomousTaskCallsForTest || f.provider.count("memory_extractor") != beforeMemoryCalls+1 {
		t.Fatalf("limit did not stop calls: task=%d memory=%d", f.provider.count("task_step")-beforeTaskCalls, f.provider.count("memory_extractor")-beforeMemoryCalls)
	}
	after := f.stored()
	if len(after.Messages) != len(before.Messages)+2 || !strings.Contains(limited.Messages[len(limited.Messages)-1].Text, "не смог безопасно") || after.Tasks[0].Stage != before.Tasks[0].Stage || !reflect.DeepEqual(after.Tasks[0].Plan, before.Tasks[0].Plan) {
		t.Fatal("autonomous limit published partial state")
	}
}

const maxAutonomousTaskCallsForTest = 8

func TestResumeLostFinalResponseReplaysCompletedTask(t *testing.T) {
	for _, kind := range []string{"fake", "json"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t, kind)
			var chat model.Chat
			var err error
			for index, stage := range []string{"clarify_input", "research_input_data", "execution", "user_feedback"} {
				f.provider.set(proposal(stage), "", "")
				tid := ""
				if len(chat.Tasks) > 0 {
					tid = chat.Tasks[0].ID
				}
				chat, err = f.input("эспрессо", fmt.Sprintf("input-%d", index), tid)
				if err != nil {
					t.Fatal(err)
				}
			}
			tid := chat.Tasks[0].ID
			if _, err = f.tasks.PauseTask("s", f.pid, f.cid, tid); err != nil {
				t.Fatal(err)
			}
			raw := strings.ReplaceAll(proposal("user_feedback"), `"status":"active"`, `"status":"done"`)
			raw = strings.ReplaceAll(raw, `"positive_feedback":false`, `"positive_feedback":true`)
			raw = strings.ReplaceAll(raw, `"expected_action":"user: Оцените рецепт"`, `"expected_action":"none"`)
			f.provider.set(raw, "", "")
			chat, _, err = f.tasks.Resume(context.Background(), "s", f.pid, f.cid, tid, "спасибо", "final-resume")
			if err != nil || chat.Tasks[0].Status != model.TaskStatusDone {
				t.Fatalf("completion: %v", err)
			}
			calls := f.provider.count("task_step")
			again, _, err := f.tasks.Resume(context.Background(), "s", f.pid, f.cid, tid, "", "final-resume")
			if err != nil || !reflect.DeepEqual(again, chat) || f.provider.count("task_step") != calls {
				t.Fatalf("lost final response was not replayed: %v", err)
			}
			if _, _, err = f.tasks.Resume(context.Background(), "s", f.pid, f.cid, tid, "", "new-resume"); !errors.Is(err, model.ErrValidation) {
				t.Fatalf("new operation accepted for done task: %v", err)
			}
		})
	}
}

func TestRestartInFlightAtEveryStage(t *testing.T) {
	for _, stage := range []string{"clarify_input", "research_input_data", "execution", "user_feedback"} {
		for _, block := range []string{"task_step", "memory_extractor"} {
			t.Run(stage+"/"+block, func(t *testing.T) {
				f := setup(t, "json")
				c, e := f.input("эспрессо", "initial", "")
				if e != nil {
					t.Fatal(e)
				}
				tid := c.Tasks[0].ID
				if stage != "clarify_input" {
					for _, next := range []string{"research_input_data", "execution", "user_feedback"} {
						f.provider.set(proposal(next), "", "")
						c, e = f.input("содержательные данные", next, tid)
						if e != nil {
							t.Fatal(e)
						}
						if next == stage {
							break
						}
					}
				}
				settleTitles(t, f)
				before := f.stored()
				next := stage
				// Correcting user feedback must return to execution while retaining all
				// completed items, so this controlled step uses a new subject item.
				raw := proposal(next)
				if stage == "user_feedback" {
					var p map[string]any
					json.Unmarshal([]byte(proposal("user_feedback")), &p)
					p["stage"] = "execution"
					p["plan"] = append(p["plan"].([]any), map[string]string{"id": "correction", "title": "Уточнить рецепт по отзыву", "status": "current"})
					p["current_plan_item"] = "correction"
					p["expected_action"] = "user: проверить исправление"
					data, _ := json.Marshal(p)
					raw = string(data)
				}
				f.provider.set(raw, "", block)
				result := make(chan error, 1)
				go func() {
					_, e := f.input("данные для текущего шага", "interrupted", tid)
					result <- e
				}()
				await(t, f.provider.entered)
				_, restored, e := statejson.Open(f.path)
				if e != nil {
					t.Fatal(e)
				}
				saved := restored.Chat("s", f.pid, f.cid)
				task := saved.Tasks[tid]
				if task.Status != model.TaskStatusPaused || task.Stage != before.Tasks[0].Stage || task.CurrentStep != before.Tasks[0].CurrentStep || !reflect.DeepEqual(task.Plan, before.Tasks[0].Plan) || len(saved.Messages) != len(before.Messages) {
					t.Fatal("restart published partial step")
				}
				f.state.Close()
				if <-result == nil {
					t.Fatal("closed worker accepted output")
				}
			})
		}
	}
}

func TestTitleFallbackIsDurableAndNotRepeated(t *testing.T) {
	for _, value := range []string{"", "line\nbreak", strings.Repeat("я", 61), "**markup**", "network"} {
		t.Run(value, func(t *testing.T) {
			f := setup(t, "json")
			f.provider.titleRaw = &value
			if value == "network" {
				f.provider.fail = "title"
			}
			input := strings.Repeat("кофе ", 20)
			_, e := f.chat.Send(context.Background(), "s", f.pid, f.cid, "first", input)
			if e != nil {
				t.Fatal(e)
			}
			settleTitles(t, f)
			c := f.stored()
			if c.TitleStatus != "fallback" || c.Title != model.TitleFallback(input) {
				t.Fatalf("title %+v", c)
			}
			_, restored, e := statejson.Open(f.path)
			if e != nil {
				t.Fatal(e)
			}
			if restored.Chat("s", f.pid, f.cid).Title != c.Title {
				t.Fatal("fallback lost on restart")
			}
			_, e = f.chat.Send(context.Background(), "s", f.pid, f.cid, "second", "more")
			if e != nil {
				t.Fatal(e)
			}
			if f.provider.count("title") != 1 {
				t.Fatal("second title call")
			}
		})
	}
}
