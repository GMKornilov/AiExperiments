import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { BaristaWorkspace } from "./barista-workspace";

const chat = (id = "c1", messages: unknown[] = [], tasks?: unknown[]) => ({ id, project_id: "p1", title: `Чат ${id}`, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", messages, memory_status: "idle", ...(tasks ? { tasks } : {}) });
const project = (chats = [chat()]) => ({ id: "p1", title: "Мой кофе", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", chats, selected_chat_id: chats[0]?.id ?? null });
const listing = (projects = [project()]) => ({ projects, selected_project_id: projects[0]?.id ?? null, selected_chat_id: projects[0]?.selected_chat_id ?? null });
const memory = { global_facts: ["Оборудование: V60"], project_facts: ["Есть зёрна: Эфиопия"], status: "success" };
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }));

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("BaristaWorkspace", () => {
  it("показывает ID выбранного чата и копирует чистый идентификатор", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    vi.stubGlobal("fetch", vi.fn().mockImplementation((url: string) => url.endsWith("/memory") ? json(memory) : json(listing())));
    render(<BaristaWorkspace />);

    const status = await screen.findByRole("region", { name: "Статус диалога" });
    expect(within(status).getByText("ID: c1", { selector: "code" })).toBeVisible();
    fireEvent.click(within(status).getByRole("button", { name: "Копировать ID чата" }));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith("c1"));
    expect(await within(status).findByRole("status")).toHaveTextContent("ID чата скопирован.");
  });

  it("Enter отправляет, Shift+Enter оставляет перенос, а IME Enter не отправляет", async () => {
    const saved = { ...chat(), title_status: "success", messages: [{ id: "u1", role: "user", text: "кофе", status: "success", created_at: "2026-01-01T00:00:00Z" }, { id: "a1", role: "assistant", text: "ответ", status: "success", created_at: "2026-01-01T00:00:01Z" }] };
    const fetchMock = vi.fn().mockImplementation((url: string) => url.endsWith("/tasks/input") ? json({ chat: saved }) : url.endsWith("/memory") ? json(memory) : json(listing()));
    vi.stubGlobal("fetch", fetchMock); render(<BaristaWorkspace />);
    const input = await screen.findByLabelText("Ваш вопрос");
    expect(fireEvent.keyDown(input, { key: "Enter" })).toBe(false);
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/tasks/input"))).toBe(false);
    fireEvent.change(input, { target: { value: "кофе" } }); fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/tasks/input"))).toBe(true));
    fireEvent.change(input, { target: { value: "один" } }); fireEvent.keyDown(input, { key: "Enter", shiftKey: true }); expect(input).toHaveValue("один");
    fireEvent.keyDown(input, { key: "Enter", isComposing: true }); expect(fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/tasks/input")).length).toBe(1);
  });
  it("создаёт проект через sticky действие", async () => {
    const created = { ...project([]), id: "p2", title: "Новый проект" };
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url === "/api/projects" && init?.method === "POST") return json(listing([project(), created]));
      if (url === "/api/projects/p2/memory") return json({ ...memory, project_facts: [] });
      return json(listing());
    });
    vi.stubGlobal("fetch", fetchMock); render(<BaristaWorkspace />);
    fireEvent.click(await screen.findByRole("button", { name: /Новый проект/ }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([url, init]) => url === "/api/projects" && init?.method === "POST")).toBe(true));
    expect(screen.getByText("В этом проекте пока нет чатов.")).toBeVisible();
  });

  it("не даёт создать чат до завершения создания проекта", async () => {
    let resolve!: (response: Response) => void;
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url === "/api/projects" && init?.method === "POST") return new Promise<Response>(done => { resolve = done; });
      return json({ projects: [], selected_project_id: "", selected_chat_id: "" });
    });
    vi.stubGlobal("fetch", fetchMock); render(<BaristaWorkspace />);
    fireEvent.click(await screen.findByRole("button", { name: "Новый проект" }));
    expect(screen.getByRole("button", { name: "Новый проект" })).toBeDisabled();
    resolve(await json(listing([project([])])));
  });

  it("показывает память read-only и очищает слой только после подтверждения", async () => {
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url === "/api/projects/p1/memory" && init?.method === "DELETE") return new Response(null, { status: 204 });
      if (url === "/api/projects/p1/memory") return json(init?.method === "DELETE" ? memory : memory);
      return json(listing());
    });
    vi.stubGlobal("fetch", fetchMock); render(<BaristaWorkspace />);
    fireEvent.click(await screen.findByRole("button", { name: "Память" }));
    expect(screen.getByText("Оборудование: V60")).toBeVisible();
    expect(screen.getByText("Есть зёрна: Эфиопия")).toBeVisible();
    fireEvent.click(screen.getAllByRole("button", { name: "Очистить" })[0]);
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(fetchMock.mock.calls.some(([url, init]) => url === "/api/projects/p1/memory/global" && init?.method === "DELETE")).toBe(false);
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Очистить" }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([url, init]) => url === "/api/projects/p1/memory/global" && init?.method === "DELETE")).toBe(true));
  });

  it("сразу показывает pending-ввод и не отображает ответ агента до завершения запроса", async () => {
    let resolve!: (response: Response) => void;
    const response = { ...chat("c1", [{ id: "u1", role: "user", text: "Что приготовить?", status: "success", created_at: "2026-01-01T00:00:00Z" }, { id: "a1", role: "assistant", text: "Сделайте V60", status: "success", created_at: "2026-01-01T00:00:01Z" }]), memory_status: "success" };
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url.endsWith("/tasks/input") && init?.method === "POST") return new Promise<Response>(done => { resolve = done; });
      if (url === "/api/projects/p1/memory") return json(memory);
      return json(listing());
    });
    vi.stubGlobal("fetch", fetchMock); render(<BaristaWorkspace />);
    fireEvent.change(await screen.findByLabelText("Ваш вопрос"), { target: { value: "Что приготовить?" } }); fireEvent.click(screen.getByRole("button", { name: "Отправить" }));
    expect(screen.getByText("Что приготовить?")).toBeVisible(); expect(screen.getByText("Бариста готовит ответ и обновляет память…")).toBeVisible(); expect(screen.queryByText("Сделайте V60")).not.toBeInTheDocument(); expect(screen.getByRole("main")).toHaveAttribute("aria-busy", "true");
    resolve(await json({ chat: response }));
    expect(await screen.findByText("Сделайте V60")).toBeVisible(); expect(screen.queryByText("Бариста готовит ответ и обновляет память…")).not.toBeInTheDocument();
  });

  it("повторяет ошибочный task input без второго пользовательского сообщения", async () => {
    const response = { ...chat("c1", [{ id: "u1", role: "user", text: "Что приготовить?", status: "success", created_at: "2026-01-01T00:00:00Z" }]) };
    let calls = 0;
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url.endsWith("/tasks/input") && init?.method === "POST") { calls += 1; return calls === 1 ? Promise.reject(new TypeError("offline")) : json({ chat: response }); }
      if (url === "/api/projects/p1/memory") return json(memory);
      return json(listing());
    });
    vi.stubGlobal("fetch", fetchMock); render(<BaristaWorkspace />);
    fireEvent.change(await screen.findByLabelText("Ваш вопрос"), { target: { value: "Что приготовить?" } }); fireEvent.click(screen.getByRole("button", { name: "Отправить" }));
    const retryButton = await screen.findByRole("button", { name: "Повторить" });
    expect(screen.getAllByText("Что приготовить?")).toHaveLength(1);
    fireEvent.click(retryButton);
    await waitFor(() => expect(calls).toBe(2));
    const inputs = fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/tasks/input"));
    const firstID = JSON.parse(inputs[0][1].body).client_message_id;
    expect(firstID).toBeTruthy();
    expect(JSON.parse(inputs[1][1].body).client_message_id).toBe(firstID);
    expect(screen.getAllByText("Что приготовить?")).toHaveLength(1);
  });

  it("показывает кандидатов задачи и передаёт явный выбор", async () => {
    const candidate = { id: "t1", title: "Рецепт эспрессо", description: "Подобрать рецепт" };
    const saved = { ...chat(), tasks: [{ ...candidate, stage: "execution", current_step: "Проверить помол", expected_action: "Оценить вкус", status: "active", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:01:00Z" }] };
    let calls = 0;
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      if (url.endsWith("/tasks/input")) { calls += 1; return json(calls === 1 ? { chat: chat(), candidates: [candidate] } : { chat: saved }); }
      if (url.endsWith("/memory")) return json(memory);
      return json(listing());
    });
    vi.stubGlobal("fetch", fetchMock); render(<BaristaWorkspace />);
    fireEvent.change(await screen.findByLabelText("Ваш вопрос"), { target: { value: "Сделай крепче" } }); fireEvent.click(screen.getByRole("button", { name: "Отправить" }));
    expect(await screen.findByRole("button", { name: /Рецепт эспрессо/ })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: /Рецепт эспрессо/ }));
    await waitFor(() => expect(calls).toBe(2));
    const inputs = fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/tasks/input"));
    const firstID = JSON.parse(inputs[0][1].body).client_message_id;
    expect(firstID).toBeTruthy();
    expect(JSON.parse(inputs[1][1].body).client_message_id).toBe(firstID);
    const taskCalls = fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/tasks/input"));
    expect(JSON.parse(taskCalls[1][1].body).candidate_task_id).toBe("t1");
  });

  it("сворачивает детали задач и не показывает управление в карточке", async () => {
    const task = { id: "t1", title: "Рецепт", description: "Подобрать рецепт", stage: "execution", current_step: "Проверить помол", expected_action: "Оценить вкус", status: "active", plan: [{ id: "grinder", title: "Узнать информацию о кофемолке", status: "completed" }, { id: "recipe", title: "Подобрать стартовый рецепт", status: "current", stage: "execution" }, { id: "taste", title: "Проверить вкус", status: "pending" }], current_plan_item: "recipe", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:01:00Z" };
    vi.stubGlobal("fetch", vi.fn().mockImplementation((url: string) => url.endsWith("/memory") ? json(memory) : json(listing([project([chat("c1", [], [task])])]))));
    render(<BaristaWorkspace />);
    const toggle = await screen.findByRole("button", { name: /Задачи.*1 в работе/ });
    expect(screen.queryByText("Проверить помол")).not.toBeInTheDocument();
    fireEvent.click(toggle);
    expect(screen.getByText("Проверить помол")).toBeVisible();
    expect(screen.getByRole("list", { name: "План задачи «Рецепт»" })).toBeVisible();
    expect(screen.getByText("Узнать информацию о кофемолке").closest("li")).toHaveTextContent("Готово");
    expect(screen.getByText("Подобрать стартовый рецепт").closest("li")).toHaveTextContent("В работе");
    expect(screen.getByText("Проверить вкус").closest("li")).toHaveTextContent("Предстоит");
    expect(screen.getByText("Этап: Выполняем задачу")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Пауза" })).not.toBeInTheDocument();
  });

  it("показывает paused и done состояния плана без дополнительного управления в карточке", async () => {
    const paused = { id: "t1", title: "Рецепт", description: "Подобрать рецепт", stage: "research_input_data", current_step: "Узнать дату обжарки", expected_action: "Сообщить дату", status: "paused", plan: [{ id: "grinder", title: "Узнать информацию о кофемолке", status: "completed" }, { id: "roast", title: "Уточнить дату обжарки", status: "current", stage: "research_input_data" }], current_plan_item: "roast", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:01:00Z" };
    const done = { ...paused, id: "t2", title: "Готовый рецепт", status: "done", expected_action: "none" };
    vi.stubGlobal("fetch", vi.fn().mockImplementation((url: string) => url.endsWith("/memory") ? json(memory) : json(listing([project([chat("c1", [], [paused, done])])]))));
    render(<BaristaWorkspace />);
    fireEvent.click(await screen.findByRole("button", { name: /Задачи/ }));
    const pausedPlan = screen.getByRole("list", { name: "План задачи «Рецепт»" });
    expect(within(pausedPlan).getByText("Уточнить дату обжарки").closest("li")).toHaveTextContent("На паузе");
    expect(within(pausedPlan).getByText("Продолжите задачу кнопкой в поле ввода.")).toBeVisible();
    const donePlan = screen.getByRole("list", { name: "План задачи «Готовый рецепт»" });
    expect(within(donePlan).getAllByText("Готово")).toHaveLength(2);
    expect(within(donePlan).queryByText("Текущий шаг")).not.toBeInTheDocument();
  });

  it("объясняет отсутствие плана во время доуточнения", async () => {
    const task = { id: "t1", title: "Рецепт", description: "Подобрать рецепт", stage: "clarify_input", current_step: "Подтвердить цель", expected_action: "Подтвердить или уточнить цель", status: "active", plan: [], created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:01:00Z" };
    vi.stubGlobal("fetch", vi.fn().mockImplementation((url: string) => url.endsWith("/memory") ? json(memory) : json(listing([project([chat("c1", [], [task])])]))));
    render(<BaristaWorkspace />);
    fireEvent.click(await screen.findByRole("button", { name: /Задачи/ }));
    expect(screen.getByText("План появится после подтверждения цели.")).toBeVisible();
    expect(screen.getByText("Текущий вопрос")).toBeVisible();
    expect(screen.getByText("Этап: Уточняем запрос")).toBeVisible();
  });

  it("останавливает выполняющуюся задачу из composer и продолжает паузу оттуда же", async () => {
    const task = { id: "t1", title: "Рецепт", description: "Подобрать рецепт", stage: "execution", current_step: "Проверить помол", expected_action: "Оценить вкус", status: "active", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:01:00Z" };
    const paused = { ...chat(), tasks: [{ ...task, status: "paused" }] };
    let rejectInput!: (cause: unknown) => void;
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      if (url.endsWith("/tasks/input")) return new Promise<Response>((_resolve, reject) => { rejectInput = reject; });
      if (url.endsWith("/pause")) return json({ chat: paused });
      if (url.endsWith("/resume")) return json({ chat: { ...chat(), tasks: [task] } });
      if (url.endsWith("/memory")) return json(memory);
      if (url.endsWith("/chats/c1")) return json(paused);
      return json(listing([project([chat("c1", [], [task])])]));
    });
    vi.stubGlobal("fetch", fetchMock); render(<BaristaWorkspace />);
    fireEvent.change(await screen.findByLabelText("Ваш вопрос"), { target: { value: "Сделай крепче" } });
    fireEvent.click(screen.getByRole("button", { name: "Отправить" }));
    fireEvent.click(await screen.findByRole("button", { name: "Остановить" }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/pause"))).toBe(true));
    expect(await screen.findByRole("button", { name: "Продолжить" })).toBeVisible();
    rejectInput(new TypeError("request cancelled"));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Повторить" })).not.toBeInTheDocument();
    expect(screen.getByText("Сделай крепче")).toBeVisible();
    expect(screen.getByText("Задача на паузе")).toBeVisible();
    expect(await screen.findByRole("button", { name: "Продолжить" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Продолжить" }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/resume"))).toBe(true));
  });

  it("не превращает отменённый input в ошибку, если он отклонился раньше ответа Pause", async () => {
    const task = { id: "t1", title: "Рецепт", description: "Подобрать рецепт", stage: "execution", current_step: "Проверить помол", expected_action: "Оценить вкус", status: "active", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:01:00Z" };
    const paused = { ...chat(), tasks: [{ ...task, status: "paused" }] };
    let rejectInput!: (cause: unknown) => void;
    let resolvePause!: (response: Response) => void;
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      if (url.endsWith("/tasks/input")) return new Promise<Response>((_resolve, reject) => { rejectInput = reject; });
      if (url.endsWith("/pause")) return new Promise<Response>(resolve => { resolvePause = resolve; });
      if (url.endsWith("/memory")) return json(memory);
      return json(listing([project([chat("c1", [], [task])])]));
    });
    vi.stubGlobal("fetch", fetchMock); render(<BaristaWorkspace />);
    fireEvent.change(await screen.findByLabelText("Ваш вопрос"), { target: { value: "Сделай крепче" } });
    fireEvent.click(screen.getByRole("button", { name: "Отправить" }));
    fireEvent.click(await screen.findByRole("button", { name: "Остановить" }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/pause"))).toBe(true));

    rejectInput(new TypeError("request cancelled"));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Повторить" })).not.toBeInTheDocument();
    expect(screen.getByText("Бариста готовит ответ и обновляет память…")).toBeVisible();

    resolvePause(await json({ chat: paused }));
    expect(await screen.findByRole("button", { name: "Продолжить" })).toBeVisible();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Повторить" })).not.toBeInTheDocument();
  });
});
