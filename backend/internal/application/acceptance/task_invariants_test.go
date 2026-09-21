package acceptance_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"aichallenge/week_1/task_1/internal/adapters/extractjson"
	"aichallenge/week_1/task_1/internal/application/completion"
	"aichallenge/week_1/task_1/internal/application/conversation"
	"aichallenge/week_1/task_1/internal/application/invariant"
	"aichallenge/week_1/task_1/internal/application/taskflow"
	"aichallenge/week_1/task_1/internal/domain/model"
)

type controlledPipeline struct {
	pre, post    []invariant.Violation
	postSequence [][]invariant.Violation
	err          error
	errorSubject invariant.Subject
	calls        []invariant.Input
}

func (p *controlledPipeline) Public() []invariant.Metadata {
	return []invariant.Metadata{{ID: "equipment-availability"}, {ID: "beans-availability"}, {ID: "inventory-truth"}}
}
func (p *controlledPipeline) Validate(_ context.Context, in invariant.Input) ([]invariant.Violation, error) {
	p.calls = append(p.calls, in)
	if p.err != nil && (p.errorSubject == "" || p.errorSubject == in.Subject) {
		return nil, p.err
	}
	if in.Subject == invariant.UserInput {
		return p.pre, nil
	}
	if len(p.postSequence) != 0 {
		out := p.postSequence[0]
		p.postSequence = p.postSequence[1:]
		return out, nil
	}
	return p.post, nil
}
func taskWithPipeline(t *testing.T, p invariant.Pipeline, raws ...string) *fixture {
	f := setup(t, "fake")
	f.provider.set("", "", "")
	f.provider.raws = raws
	ex := extractjson.NewExtractor(f.provider, "MEMORY")
	f.tasks = taskflow.New(f.state, f.provider, ex, extractjson.ProposalDecoder{}, model.TaskRouter{}, f.titles, conversation.Settings{Prompt: "BASE", Window: 3}, func() string { return "test-id" }, time.Now, p)
	return f
}
func violations() []invariant.Violation {
	return []invariant.Violation{{InvariantID: "equipment-availability", RepairInstruction: "use alternative"}, {InvariantID: "beans-availability", RepairInstruction: "ask beans"}}
}

func TestServicesRequireInvariantPipeline(t *testing.T) {
	f := setup(t, "fake")
	assertPanic := func(newService func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatal("constructor accepted nil invariant pipeline")
			}
		}()
		newService()
	}
	assertPanic(func() {
		conversation.New(f.state, f.provider, extractjson.NewExtractor(f.provider, "MEMORY"), f.titles, conversation.Settings{}, func() string { return "id" }, time.Now, nil)
	})
	assertPanic(func() {
		taskflow.New(f.state, f.provider, extractjson.NewExtractor(f.provider, "MEMORY"), extractjson.ProposalDecoder{}, model.TaskRouter{}, f.titles, conversation.Settings{}, func() string { return "id" }, time.Now, nil)
	})
}

func TestTaskInvariantPreViolationCommitsRefusalWithoutTask(t *testing.T) {
	p := &controlledPipeline{pre: violations()}
	f := taskWithPipeline(t, p)
	chat, err := f.input("use broken grinder", "pre", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Tasks) != 0 || len(chat.Messages) != 2 {
		t.Fatalf("chat=%+v", chat)
	}
	if !strings.Contains(chat.Messages[1].Text, "equipment-availability") {
		t.Fatal(chat.Messages[1].Text)
	}
	if f.provider.count("memory_extractor") != 1 || f.provider.count("task_step") != 0 {
		t.Fatalf("unexpected provider calls: memory=%d task=%d", f.provider.count("memory_extractor"), f.provider.count("task_step"))
	}
	if chat.MemoryStatus != "success" || chat.MemoryErrorCategory != "" {
		t.Fatalf("refusal did not clear memory status: %+v", chat)
	}
}

func TestPreRefusalOwnsExtractorAndDeletePreventsLateCommit(t *testing.T) {
	f := setup(t, "fake")
	p := &controlledPipeline{pre: violations()}
	extractor := extractjson.NewExtractor(f.provider, "MEMORY")
	f.chat = conversation.New(f.state, f.provider, extractor, f.titles, conversation.Settings{Prompt: "BASE", Window: 3}, func() string { return "refusal-id" }, time.Now, p)
	f.provider.set("", "", "memory_extractor")
	result := make(chan error, 1)
	go func() {
		_, err := f.chat.Send(context.Background(), "s", f.pid, f.cid, "refusal", "use broken grinder")
		result <- err
	}()
	await(t, f.provider.entered)
	if _, err := f.chat.Send(context.Background(), "s", f.pid, f.cid, "other", "ordinary message"); !errors.Is(err, model.ErrBusy) {
		t.Fatalf("concurrent send=%v, want busy", err)
	}
	if err := f.ws.DeleteChat("s", f.pid, f.cid); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("late refusal result=%v", err)
	}
	if _, ok := f.ws.GetChat("s", f.pid, f.cid); ok {
		t.Fatal("deleted chat was resurrected")
	}
}

func TestTaskPreRefusalOwnsExtractorAndDeletePreventsLateCommit(t *testing.T) {
	f := taskWithPipeline(t, &controlledPipeline{pre: violations()})
	f.provider.set("", "", "memory_extractor")
	result := make(chan error, 1)
	go func() { _, err := f.input("use broken grinder", "task-refusal", ""); result <- err }()
	await(t, f.provider.entered)
	if _, err := f.input("another request", "task-other", ""); !errors.Is(err, model.ErrBusy) {
		t.Fatalf("concurrent task input=%v, want busy", err)
	}
	if err := f.ws.DeleteChat("s", f.pid, f.cid); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("late task refusal result=%v", err)
	}
	if _, ok := f.ws.GetChat("s", f.pid, f.cid); ok {
		t.Fatal("deleted chat was resurrected")
	}
}

func TestTaskPreRefusalClearsStaleMemoryStatus(t *testing.T) {
	p := &controlledPipeline{pre: violations()}
	f := taskWithPipeline(t, p)
	if err := f.state.Update(func(root *model.State) error {
		root.Chat("s", f.pid, f.cid).Status = "error"
		root.Chat("s", f.pid, f.cid).ErrorCategory = "network"
		root.Project("s", f.pid).Status = "error"
		root.Project("s", f.pid).ErrorCategory = "network"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	chat, err := f.input("use broken grinder", "status", "")
	if err != nil || chat.MemoryStatus != "success" || chat.MemoryErrorCategory != "" {
		t.Fatalf("refusal status: err=%v chat=%+v", err, chat)
	}
}

func TestConversationPreRefusalClearsStaleMemoryStatus(t *testing.T) {
	f := setup(t, "fake")
	p := &controlledPipeline{pre: violations()}
	f.chat = conversation.New(f.state, f.provider, extractjson.NewExtractor(f.provider, "MEMORY"), f.titles, conversation.Settings{Prompt: "BASE", Window: 3}, func() string { return "conversation-refusal" }, time.Now, p)
	if err := f.state.Update(func(root *model.State) error {
		root.Chat("s", f.pid, f.cid).Status = "error"
		root.Chat("s", f.pid, f.cid).ErrorCategory = "network"
		root.Project("s", f.pid).Status = "error"
		root.Project("s", f.pid).ErrorCategory = "network"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	chat, err := f.chat.Send(context.Background(), "s", f.pid, f.cid, "conversation-status", "use broken grinder")
	if err != nil || chat.MemoryStatus != "success" || chat.MemoryErrorCategory != "" {
		t.Fatalf("refusal status: err=%v chat=%+v", err, chat)
	}
}

func TestTaskInvariantPostRepairAndTechnicalFailure(t *testing.T) {
	p := &controlledPipeline{postSequence: [][]invariant.Violation{violations(), nil}}
	f := taskWithPipeline(t, p, proposal("clarify_input"), proposal("clarify_input"))
	chat, err := f.input("help", "post", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Messages) != 2 || f.provider.count("task_step") != 2 {
		t.Fatalf("task calls=%d chat=%+v", f.provider.count("task_step"), chat)
	}
	f.provider.mu.Lock()
	repairPrompt := f.provider.messages["task_step"][0].Content
	f.provider.mu.Unlock()
	for _, invariantID := range []string{"equipment-availability", "beans-availability"} {
		if !strings.Contains(repairPrompt, invariantID) {
			t.Fatalf("repair missed aggregated violation %q: %s", invariantID, repairPrompt)
		}
	}
	p2 := &controlledPipeline{err: &completion.Error{Category: "invariant_validation", Cause: errors.New("down")}, errorSubject: invariant.TaskProposal}
	f2 := taskWithPipeline(t, p2, proposal("clarify_input"))
	_, err = f2.input("help", "error", "")
	if completion.Category(err) != "invariant_validation" {
		t.Fatalf("%v", err)
	}
	if got := f2.state.Snapshot().Chat("s", f2.pid, f2.cid); len(got.Messages) != 0 || len(got.Tasks) != 0 || len(got.Operations) != 0 || len(got.TaskInputs) != 0 || len(got.TaskOperationIDs) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestChatPostInvariantTechnicalErrorDoesNotPersistInput(t *testing.T) {
	f := setup(t, "fake")
	p := &controlledPipeline{err: &completion.Error{Category: "invariant_validation", Cause: errors.New("down")}, errorSubject: invariant.ChatCandidate}
	extractor := extractjson.NewExtractor(f.provider, "MEMORY")
	f.chat = conversation.New(f.state, f.provider, extractor, f.titles, conversation.Settings{Prompt: "BASE", Window: 3}, func() string { return "chat-id" }, time.Now, p)
	_, err := f.chat.Send(context.Background(), "s", f.pid, f.cid, "invariant-error", "help")
	if completion.Category(err) != "invariant_validation" {
		t.Fatalf("error=%v", err)
	}
	stored := f.stored()
	if len(stored.Messages) != 0 || stored.TitleStatus != "idle" || f.provider.count("memory_extractor") != 0 {
		t.Fatalf("validator error persisted chat state: %+v calls=%v", stored, f.provider.calls)
	}
	if f.state.Snapshot().Sessions["s"].Pending != nil {
		t.Fatal("pending lease was not released")
	}
}

func TestTaskInvariantRepairExhaustionCommitsOnlyRefusal(t *testing.T) {
	p := &controlledPipeline{post: violations()}
	f := taskWithPipeline(t, p, proposal("clarify_input"), proposal("clarify_input"), proposal("clarify_input"))
	chat, err := f.input("help", "exhaust", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.Tasks) != 0 || len(chat.Messages) != 2 || !strings.Contains(chat.Messages[1].Text, "не смог безопасно") {
		t.Fatalf("rejected task leaked: %+v", chat)
	}
	stored := f.state.Snapshot().Chat("s", f.pid, f.cid)
	if len(stored.Tasks) != 0 || len(stored.TaskInputs) != 0 || len(stored.TaskOperationIDs) != 0 || len(stored.Operations) != 0 {
		t.Fatalf("new task bookkeeping leaked: %+v", stored)
	}
	if got := f.provider.count("task_step"); got != 3 {
		t.Fatalf("candidate calls=%d, want 3", got)
	}
	if got := f.provider.count("memory_extractor"); got != 1 {
		t.Fatalf("extractor calls=%d, want 1", got)
	}
}

func TestTaskMalformedStageBypassUsesRepairLifecycleAndConfirmedSnapshot(t *testing.T) {
	f := taskWithPipeline(t, &controlledPipeline{}, proposal("clarify_input"))
	created, err := f.input("подбери эспрессо", "create", "")
	if err != nil {
		t.Fatal(err)
	}
	taskID := created.Tasks[0].ID
	before := f.state.Snapshot().Chat("s", f.pid, f.cid).Tasks[taskID]

	// validation_result is deliberately forbidden in a proposal. The proposed
	// user_feedback stage is also an attempted jump over research and execution.
	// This mirrors the real trace where strict decoding used to abort before a
	// repair could be requested.
	invalidBypass := strings.Replace(proposal("user_feedback"), "{", `{"validation_result":{"status":"passed"},`, 1)
	f.provider.setSequence(invalidBypass, invalidBypass, invalidBypass)
	chat, err := f.input("сразу перейди к отзыву", "bypass", taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.provider.count("task_step"); got != 4 { // create + three bounded repairs
		t.Fatalf("task calls=%d, want 4", got)
	}
	if len(chat.Messages) != 4 || !strings.Contains(chat.Messages[3].Text, "не смог безопасно") {
		t.Fatalf("safe refusal was not committed: %+v", chat.Messages)
	}
	if got := chat.Tasks[0]; !reflect.DeepEqual(got, *before) {
		t.Fatalf("invalid bypass changed confirmed task: got=%+v want=%+v", got, *before)
	}
	f.provider.mu.Lock()
	repairPrompt := f.provider.messages["task_step"][0].Content
	f.provider.mu.Unlock()
	if !strings.Contains(repairPrompt, "REPAIR REQUIREMENTS") || !strings.Contains(repairPrompt, "не перескакивай этапы") {
		t.Fatalf("strict decoder failure did not request repair: %s", repairPrompt)
	}
}

func TestTaskExhaustionRestoresExistingTaskAndBookkeeping(t *testing.T) {
	f := setup(t, "fake")
	initial, err := f.input("help", "initial", "")
	if err != nil {
		t.Fatal(err)
	}
	taskID := initial.Tasks[0].ID
	if _, err := f.tasks.PauseTask("s", f.pid, f.cid, taskID); err != nil {
		t.Fatal(err)
	}
	p := &controlledPipeline{post: violations()}
	f.tasks = taskflow.New(f.state, f.provider, extractjson.NewExtractor(f.provider, "MEMORY"), extractjson.ProposalDecoder{}, model.TaskRouter{}, f.titles, conversation.Settings{Prompt: "BASE", Window: 3}, func() string { return "exhaust-id" }, time.Now, p)
	f.provider.setSequence(proposal("research_input_data"), proposal("research_input_data"), proposal("research_input_data"))
	before := f.state.Snapshot().Chat("s", f.pid, f.cid)
	chat, err := f.input("continue", "exhaust-existing", taskID)
	if err != nil {
		t.Fatal(err)
	}
	after := f.state.Snapshot().Chat("s", f.pid, f.cid)
	if chat.Tasks[0].Status != model.TaskStatusPaused || !reflect.DeepEqual(after.Tasks[taskID], before.Tasks[taskID]) || !reflect.DeepEqual(after.TaskInputs, before.TaskInputs) || !reflect.DeepEqual(after.TaskOperationIDs, before.TaskOperationIDs) || !reflect.DeepEqual(after.Operations, before.Operations) {
		t.Fatalf("existing task mutated by exhaustion: before=%+v after=%+v", before, after)
	}
}

func TestTaskInvariantReceivesFullAuthoritativeSnapshot(t *testing.T) {
	p := &controlledPipeline{}
	f := taskWithPipeline(t, p, proposal("clarify_input"))
	if _, err := f.input("help", "snapshot", ""); err != nil {
		t.Fatal(err)
	}
	if len(p.calls) != 2 {
		t.Fatalf("calls=%+v", p.calls)
	}
	post := p.calls[1]
	for _, required := range []string{"\"id\"", "\"stage\"", "\"status\"", "\"plan\"", "\"current_step\"", "\"expected_action\""} {
		if !strings.Contains(post.Snapshot, required) {
			t.Fatalf("snapshot misses %s: %s", required, post.Snapshot)
		}
	}
}
