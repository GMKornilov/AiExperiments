import { afterEach, describe, expect, it, vi } from "vitest";
import { adminLogs, createDialog, createProfile, deleteProfile, getMemory, listDialogs, listInvariants, listProfiles, listProjects, pauseTask, renameProject, resumeTask, selectProfile, sendMessage, taskInput } from "./barista";

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.unstubAllEnvs(); });

const dialog = { id: "d", title: "Новый диалог", title_status: "idle", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", messages: [], accounted_tokens: 0 };
const createRequest = (init: RequestInit = {}) => new Request("http://web/api/dialogs", { ...init, method: "POST", headers: { "content-type": "application/json", ...(init.headers ?? {}) }, body: JSON.stringify({ context_strategy: "summary" }) });

describe("barista BFF", () => {
  it("строго проецирует три публичных инварианта без служебных полей", async () => {
    const invariants = { invariants: [
      { id: "equipment-availability", name: "Доступность оборудования", description: "Не использовать сломанное оборудование." },
      { id: "beans-availability", name: "Доступность зёрен", description: "Не предлагать закончившиеся зёрна." },
      { id: "inventory-truth", name: "Достоверность инвентаря", description: "Не выдумывать инвентарь." },
    ] };
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ ...invariants, transport: "private" }));
    vi.stubGlobal("fetch", fetchMock);
    const invalid = await listInvariants(new Request("http://web/api/invariants"));
    expect(invalid.status).toBe(502);

    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(invariants)));
    const result = await listInvariants(new Request("http://web/api/invariants"));
    expect(result.status).toBe(200);
    expect(await result.json()).toEqual(invariants);
  });
  it("передаёт произвольные поля журнала Admin без BFF-валидации", async () => {
    const logs = { found: true, log_text_payloads: false, logs: [{ timestamp: "2026-01-01T00:00:00Z", source: "backend", event: "llm_response", result: "success", correlation_id: "request-1", dialog_id: "chat-1", purpose: "future_backend_purpose", provider_trace: { model: "new-model", retry: 2 }, new_flag: true }] };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(logs)));
    const result = await adminLogs(new Request("http://web/api/admin/logs?dialog_id=chat-1&action=lookup"));
    expect(result.status).toBe(200);
    expect(await result.json()).toEqual(logs);
  });

  it("направляет MCP-журнал без dialog ID и action", async () => {
    const logs = { scope: "mcp", retention: "backend_runtime", logs: [{ timestamp: "2026-01-01T00:00:00Z", source: "backend", event: "mcp_tools_list", operation: "mcp_tools_list", result: "success", correlation_id: "request-1", duration_ms: 12 }] };
    const fetchMock = vi.fn().mockResolvedValue(Response.json(logs));
    vi.stubGlobal("fetch", fetchMock);
    const result = await adminLogs(new Request("http://web/api/admin/logs?scope=mcp"));
    expect(result.status).toBe(200);
    expect(await result.json()).toEqual(logs);
    expect(String(fetchMock.mock.calls[0][0])).toContain("/api/admin/logs?scope=mcp");
  });

  it("валидирует и проецирует профили без лишних полей", async () => {
    const profiles = { profiles: [
      { id: "barista", name: "Бариста", style: "Дружелюбно", constraints: "Без выдумок", additional_context: "Рецепты", built_in: true },
      { id: "custom", name: "Дом", style: "Кратко", constraints: "Без молока", additional_context: "V60", built_in: false },
    ], active_profile_id: "custom" };
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(Response.json(profiles))); vi.stubGlobal("fetch", fetchMock);
    expect(await (await listProfiles(new Request("http://web/api/profiles"))).json()).toEqual(profiles);
    const created = await createProfile(new Request("http://web/api/profiles", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ name: "  Дом  ", style: "Кратко", constraints: "Без молока", additional_context: "V60" }) }));
    expect(created.status).toBe(200);
    const createCall = fetchMock.mock.calls.find(([url, init]) => new URL(url).pathname === "/api/profiles" && init.method === "POST");
    expect(createCall?.[1].body).toContain('"name":"Дом"');
    const invalid = await createProfile(new Request("http://web/api/profiles", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ name: "Дом", style: " ", constraints: "Без молока", additional_context: "V60" }) }));
    expect(invalid.status).toBe(400);
  });

  it("выбирает и удаляет профиль по изолированному session ID", async () => {
    const profiles = { profiles: [{ id: "barista", name: "Бариста", style: "Стиль", constraints: "Границы", additional_context: "Контекст", built_in: true }], active_profile_id: "barista" };
    const fetchMock = vi.fn().mockImplementation((url: URL | string) => Promise.resolve(new URL(url).pathname.endsWith("/custom") ? new Response(null, { status: 204 }) : Response.json(profiles))); vi.stubGlobal("fetch", fetchMock);
    const cookie = "barista_session=12345678-1234-1234-1234-123456789abc";
    expect((await selectProfile(new Request("http://web/api/profiles/barista/select", { method: "POST", headers: { cookie } }), "barista")).status).toBe(200);
    expect((await deleteProfile(new Request("http://web/api/profiles/custom", { method: "DELETE", headers: { cookie } }), "custom")).status).toBe(204);
    expect(fetchMock.mock.calls[0][1].headers["X-Session-ID"]).toBe("12345678-1234-1234-1234-123456789abc");
  });

  it("валидирует и передаёт trim-имя проекта при PATCH", async () => {
    const saved = { id: "p1", title: "Дом", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", chats: [] };
    const fetchMock = vi.fn().mockResolvedValue(Response.json(saved)); vi.stubGlobal("fetch", fetchMock);
    const request = new Request("http://web/api/projects/p1", { method: "PATCH", headers: { "content-type": "application/json" }, body: JSON.stringify({ title: "  Дом  " }) });
    expect((await renameProject(request, "p1")).status).toBe(200);
    expect(fetchMock.mock.calls[0][1].body).toContain('"title":"Дом"');
    const invalid = await renameProject(new Request("http://web/api/projects/p1", { method: "PATCH", headers: { "content-type": "application/json" }, body: JSON.stringify({ title: " " }) }), "p1");
    expect(invalid.status).toBe(400);
  });
  it("проецирует проекты и memory facts без лишних backend-полей", async () => {
    const saved = { projects: [{ id: "p1", title: "Кофе", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", chats: [], selected_chat_id: null }], selected_project_id: "p1", selected_chat_id: null };
    const fetchMock = vi.fn().mockResolvedValue(Response.json(saved)); vi.stubGlobal("fetch", fetchMock);
    const result = await listProjects(new Request("http://web/api/projects"));
    expect(result.status).toBe(200); expect(await result.json()).toEqual(saved);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ global_facts: ["V60"], project_facts: ["Эфиопия"], status: "success" })));
    const memory = await getMemory(new Request("http://web/api/projects/p1/memory"), "p1");
    expect(await memory.json()).toEqual({ global_facts: ["V60"], project_facts: ["Эфиопия"], status: "success" });
  });

  it("принимает реальные состояния плана задачи и скрывает лишние поля кандидатов", async () => {
    const task = { id: "t1", title: "Эспрессо", description: "Подобрать рецепт", stage: "execution", current_step: "Проверить помол", expected_action: "user", status: "active", plan: [{ id: "grinder", title: "Проверить помол", status: "current", stage: "execution" }], current_plan_item: "grinder", validation_result: { status: "not_validated" }, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:01:00Z" };
    const saved = { id: "c1", project_id: "p1", title: "Чат", title_status: "success", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", messages: [], memory_status: "success", tasks: [task] };
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ chat: saved, candidates: [{ ...task, private_note: "не выдавать" }] })); vi.stubGlobal("fetch", fetchMock);
    const inputRequest = () => new Request("http://web/api/projects/p1/chats/c1/tasks/input", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ text: "сделай крепче" }) });
    const result = await taskInput(inputRequest(), "p1", "c1");
    expect(result.status).toBe(502);

    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ chat: saved, candidates: [{ id: task.id, title: task.title, description: task.description }] })));
    const safe = await taskInput(inputRequest(), "p1", "c1");
    expect(await safe.json()).toEqual({ chat: saved, candidates: [{ id: "t1", title: "Эспрессо", description: "Подобрать рецепт" }] });

    const initialClarifyTask = {
      id: "t2", title: "Подобрать рецепт", description: "Уточнить параметры для эспрессо",
      stage: "clarify_input", current_step: "Уточнить цель задачи", expected_action: "agent",
      status: "active", plan: [], validation_result: { status: "not_validated" }, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:01:00Z",
    };
    const initialClarifyChat = { ...saved, tasks: [initialClarifyTask] };
    // Actual backend shape: an absent candidate slice is serialized as null.
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ chat: initialClarifyChat, candidates: null })));
    const noCandidates = await taskInput(inputRequest(), "p1", "c1");
    expect(noCandidates.status).toBe(200);
    expect(await noCandidates.json()).toEqual({ chat: initialClarifyChat });

    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ chat: { ...saved, tasks: [{ ...task, current_plan_item: undefined }] } })));
    expect((await taskInput(inputRequest(), "p1", "c1")).status).toBe(502);

    const feedbackTask = {
      ...task,
      stage: "user_feedback",
      validation_result: { status: "passed", summary: "Проверено" },
      current_step: "Учесть обратную связь пользователя",
      expected_action: "agent",
      plan: [
        { id: "research", title: "Изучить исходные данные задачи", status: "completed", stage: "research_input_data" },
        { id: "result", title: "Подготовить решение задачи", status: "completed", stage: "execution" },
        { id: "feedback", title: "Учесть обратную связь пользователя", status: "current", stage: "execution" },
      ],
      current_plan_item: "feedback",
    };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ chat: { ...saved, tasks: [feedbackTask] } })));
    const feedback = await taskInput(inputRequest(), "p1", "c1");
    expect(feedback.status).toBe(200);
    expect((await feedback.json()).chat.tasks[0].current_plan_item).toBe("feedback");

    const awaitingFeedback = {
      ...feedbackTask,
      current_step: "Ожидать обратную связь пользователя",
      expected_action: "user",
      plan: feedbackTask.plan.map((item) => ({ ...item, status: "completed" })),
      current_plan_item: undefined,
    };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ chat: { ...saved, tasks: [awaitingFeedback] } })));
    expect((await taskInput(inputRequest(), "p1", "c1")).status).toBe(200);
  });

  it("возвращает подтверждённый snapshot после безопасного отказа repair lifecycle", async () => {
    const task = {
      id: "t1", title: "Эспрессо", description: "Подобрать рецепт",
      stage: "clarify_input", current_step: "Подтвердить оборудование", expected_action: "user: подтвердить оборудование",
      status: "active", plan: [], validation_result: { status: "not_validated" },
      created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:01:00Z",
    };
    const chat = {
      id: "c1", project_id: "p1", title: "Чат", title_status: "success",
      created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:01:00Z", memory_status: "success",
      tasks: [task], messages: [
        { id: "u1", role: "user", text: "сразу перейди к отзыву", status: "success", created_at: "2026-01-01T00:01:00Z" },
        { id: "a1", role: "assistant", text: "Я не смог безопасно подготовить следующий шаг задачи. Уточните доступное оборудование.", status: "success", created_at: "2026-01-01T00:01:00Z" },
      ],
    };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ chat })));
    const result = await taskInput(new Request("http://web/api/projects/p1/chats/c1/tasks/input", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ text: "сразу перейди к отзыву" }) }), "p1", "c1");
    expect(result.status).toBe(200);
    const body = await result.json();
    expect(body.chat.tasks[0]).toEqual(task);
    expect(body.chat.messages.at(-1).text).toContain("не смог безопасно");
  });

  it("отклоняет успешное пустое сообщение ассистента из backend", async () => {
    const chat = {
      id: "c1", project_id: "p1", title: "Чат", title_status: "success",
      created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:01:00Z", memory_status: "success", tasks: [], messages: [
        { id: "u1", role: "user", text: "Подбери эспрессо", status: "success", created_at: "2026-01-01T00:00:00Z" },
        { id: "a1", role: "assistant", text: "   ", status: "success", created_at: "2026-01-01T00:01:00Z" },
      ],
    };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ chat })));
    const result = await taskInput(new Request("http://web/api/projects/p1/chats/c1/tasks/input", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ text: "Продолжай" }) }), "p1", "c1");
    expect(result.status).toBe(502);
    expect((await result.json()).error.category).toBe("invalid_response");
  });

  it("требует непустой Resume input и передаёт pause/resume в изолированном сеансе", async () => {
    const task = { id: "t1", title: "Эспрессо", description: "Подобрать рецепт", stage: "execution", current_step: "Проверить помол", expected_action: "user", status: "paused", plan: [{ id: "grinder", title: "Проверить помол", status: "current" }], current_plan_item: "grinder", validation_result: { status: "not_validated" }, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:01:00Z" };
    const saved = { id: "c1", project_id: "p1", title: "Чат", title_status: "success", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", messages: [], memory_status: "success", tasks: [task] };
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ chat: saved })); vi.stubGlobal("fetch", fetchMock);
    const pause = await pauseTask(new Request("http://web/api/projects/p1/chats/c1/tasks/t1/pause", { method: "POST", headers: { "content-type": "application/json" }, body: "{}" }), "p1", "c1", "t1");
    expect(pause.status).toBe(200);
    const invalid = await resumeTask(new Request("http://web/api/projects/p1/chats/c1/tasks/t1/resume", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ text: " " }) }), "p1", "c1", "t1");
    expect(invalid.status).toBe(400);
    expect(fetchMock.mock.calls[0][1].body).toBe("{}");
  });

  it("принимает пустые selected IDs для нового browser-сеанса", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ projects: [], selected_project_id: "", selected_chat_id: "" })));
    const result = await listProjects(new Request("http://web/api/projects"));
    expect(result.status).toBe(200);
    expect(await result.json()).toEqual({ projects: [], selected_project_id: "", selected_chat_id: "" });
  });

  it("не пропускает невалидный memory snapshot из backend", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ global_facts: ["V60", ""], project_facts: [], status: "success" })));
    const result = await getMemory(new Request("http://web/api/projects/p1/memory"), "p1");
    expect(result.status).toBe(502); expect((await result.json()).error.category).toBe("invalid_response");
  });

  it("forwards a 14 MB prompt unchanged", async () => {
    const value = "123456 ".repeat(2_000_000);
    const fetchMock = vi.fn().mockResolvedValue(Response.json(dialog));
    vi.stubGlobal("fetch", fetchMock);
    const result = await sendMessage(new Request("http://web/api/dialogs/d/messages", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ client_message_id: "large", text: value }) }), "d");
    expect(result.status).toBe(200);
    const call = fetchMock.mock.calls.find(([url]) => new URL(url).pathname.endsWith("/messages"));
    expect(JSON.parse(call![1].body).text).toBe(value);
  });

  it("projects confirmed usage and hides the backend attempt ledger", async () => {
    const usage = { prompt_tokens: 100, completion_tokens: 20 };
    const saved = { ...dialog, accounted_tokens: 120, messages: [
      { id: "u", role: "user", text: "Кофе?", status: "success", created_at: dialog.created_at, attempts: [{ id: "u-a-1", usage }] },
      { id: "a", role: "assistant", text: "Ответ", status: "success", created_at: dialog.created_at, usage },
    ] };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(saved)));
    const result = await createDialog(createRequest());
    const body = await result.json();
    expect(result.status).toBe(200);
    expect(body.accounted_tokens).toBe(120);
    expect(body.messages[0]).not.toHaveProperty("attempts");
    expect(body.messages[1].usage).toEqual(usage);
  });

  it.each([undefined, null, {}, { prompt_tokens: -1, completion_tokens: 20 }, { prompt_tokens: 1.5, completion_tokens: 20 }, { prompt_tokens: 1, completion_tokens: "20" }, { prompt_tokens: Number.MAX_SAFE_INTEGER, completion_tokens: 1 }])("keeps a valid answer without unconfirmed usage %j", async (usage) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ ...dialog, messages: [{ id: "a", role: "assistant", text: "Ответ", status: "success", created_at: dialog.created_at, usage }] })));
    const result = await createDialog(createRequest());
    expect(result.status).toBe(200);
    const body = await result.json();
    expect(body.messages[0].text).toBe("Ответ");
    expect(body.messages[0]).not.toHaveProperty("usage");
  });

  it.each([-1, 0.5, Number.MAX_SAFE_INTEGER + 1, "120", null])("rejects an invalid accounted total %j", async (accounted_tokens) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ ...dialog, accounted_tokens })));
    const result = await createDialog(createRequest());
    expect(result.status).toBe(502);
    expect((await result.json()).error.category).toBe("invalid_response");
  });

  it("preserves context_limit with safe instructions rather than provider text", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ error: { category: "context_limit", message: "private prompt and credential" } }, { status: 400 })));
    const result = await createDialog(createRequest());
    expect(await result.json()).toEqual({ error: { category: "context_limit", message: "Контекст диалога превышает лимит модели. Начните новый диалог." } });
  });

  it("restores an interrupted message using the existing browser cookie", async () => {
    const saved = { ...dialog, title_status: "error", messages: [{ id: "m-1", client_message_id: "c1", role: "user", text: "Кофе", status: "error", error_category: "cancelled", created_at: dialog.created_at }] };
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ dialogs: [saved], selected_dialog_id: "d" }));
    vi.stubGlobal("fetch", fetchMock);
    const result = await listDialogs(new Request("http://web/api/dialogs", { headers: { cookie: "barista_session=12345678-1234-1234-1234-123456789abc" } }));
    expect(result.status).toBe(200);
    expect(await result.json()).toEqual({ dialogs: [saved], selected_dialog_id: "d" });
    expect(result.headers.get("set-cookie")).toBeNull();
    expect(fetchMock.mock.calls[0][1].headers["X-Session-ID"]).toBe("12345678-1234-1234-1234-123456789abc");
  });

  it("returns a safe storage error without backend details", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ error: { category: "storage", message: "private path and credential" } }, { status: 503 })));
    const result = await listDialogs(new Request("http://web/api/dialogs"));
    expect(result.status).toBe(502);
    expect(await result.json()).toEqual({ error: { category: "storage", message: "Хранилище истории временно недоступно." } });
  });

  it("creates a HttpOnly session cookie and forwards only its ID", async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ dialogs: [], selected_dialog_id: "" }));
    vi.stubGlobal("fetch", fetchMock);
    const first = await listDialogs(new Request("http://web/api/dialogs"));
    const cookie = first.headers.get("set-cookie");
    expect(cookie).toMatch(/HttpOnly; SameSite=Lax/);
    const id = cookie?.match(/barista_session=([^;]+)/)?.[1];
    expect(fetchMock).toHaveBeenCalledWith(expect.any(URL), expect.objectContaining({ headers: expect.objectContaining({ "X-Session-ID": id }) }));
    await createDialog(createRequest({ headers: { cookie: `barista_session=${id}` } }));
    expect(fetchMock.mock.calls[1][1].headers["X-Session-ID"]).toBe(id);
  });

  it("rejects invalid bodies before contacting backend", async () => {
    const fetchMock = vi.fn(); vi.stubGlobal("fetch", fetchMock);
    const result = await sendMessage(new Request("http://web/api/dialogs/d/messages", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ text: "кофе", extra: true }) }), "d");
    expect(result.status).toBe(400); expect(fetchMock).toHaveBeenCalledWith(expect.any(URL), expect.objectContaining({ body: expect.stringContaining("dialog_validation_failed") }));
  });

  it("does not expose a provider response body", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ error: { category: "provider", message: "credential private-answer" } }, { status: 502 })));
    const result = await createDialog(createRequest());
    expect(await result.text()).not.toMatch(/credential|private-answer/);
  });

  it("uses its own deadline rather than the caller signal", async () => {
    const timeout = vi.spyOn(AbortSignal, "timeout");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(dialog)));
    const controller = new AbortController(); controller.abort();
    const result = await createDialog(createRequest({ signal: controller.signal }));
    expect(result.status).toBe(200); expect(timeout).toHaveBeenCalledWith(65_000);
  });
});
