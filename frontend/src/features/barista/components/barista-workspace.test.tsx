import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { BaristaWorkspace } from "./barista-workspace";

const chat = (id = "c1", messages: unknown[] = []) => ({ id, project_id: "p1", title: `Чат ${id}`, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", messages, memory_status: "idle" });
const project = (chats = [chat()]) => ({ id: "p1", title: "Мой кофе", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", chats, selected_chat_id: chats[0]?.id ?? null });
const listing = (projects = [project()]) => ({ projects, selected_project_id: projects[0]?.id ?? null, selected_chat_id: projects[0]?.selected_chat_id ?? null });
const memory = { global_facts: ["Оборудование: V60"], project_facts: ["Есть зёрна: Эфиопия"], status: "success" };
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }));

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("BaristaWorkspace", () => {
  it("Enter отправляет, Shift+Enter оставляет перенос, а IME Enter не отправляет", async () => {
    const saved = { ...chat(), title_status: "success", messages: [{ id: "u1", role: "user", text: "кофе", status: "success", created_at: "2026-01-01T00:00:00Z" }, { id: "a1", role: "assistant", text: "ответ", status: "success", created_at: "2026-01-01T00:00:01Z" }] };
    const fetchMock = vi.fn().mockImplementation((url: string) => url.endsWith("/messages") ? json(saved) : url.endsWith("/memory") ? json(memory) : json(listing()));
    vi.stubGlobal("fetch", fetchMock); render(<BaristaWorkspace />);
    const input = await screen.findByLabelText("Ваш вопрос");
    expect(fireEvent.keyDown(input, { key: "Enter" })).toBe(false);
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/messages"))).toBe(false);
    fireEvent.change(input, { target: { value: "кофе" } }); fireEvent.keyDown(input, { key: "Enter" });
    await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/messages"))).toBe(true));
    fireEvent.change(input, { target: { value: "один" } }); fireEvent.keyDown(input, { key: "Enter", shiftKey: true }); expect(input).toHaveValue("один");
    fireEvent.keyDown(input, { key: "Enter", isComposing: true }); expect(fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/messages")).length).toBe(1);
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

  it("не отображает assistant response до завершения запроса", async () => {
    let resolve!: (response: Response) => void;
    const response = { ...chat("c1", [{ id: "u1", role: "user", text: "Что приготовить?", status: "success", created_at: "2026-01-01T00:00:00Z" }, { id: "a1", role: "assistant", text: "Сделайте V60", status: "success", created_at: "2026-01-01T00:00:01Z" }]), memory_status: "success" };
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (url.endsWith("/messages") && init?.method === "POST") return new Promise<Response>(done => { resolve = done; });
      if (url === "/api/projects/p1/memory") return json(memory);
      return json(listing());
    });
    vi.stubGlobal("fetch", fetchMock); render(<BaristaWorkspace />);
    fireEvent.change(await screen.findByLabelText("Ваш вопрос"), { target: { value: "Что приготовить?" } }); fireEvent.click(screen.getByRole("button", { name: "Отправить" }));
    expect(screen.queryByText("Сделайте V60")).not.toBeInTheDocument(); expect(screen.getByText(/обновляет память/i)).toBeVisible();
    resolve(await json(response));
    expect(await screen.findByText("Сделайте V60")).toBeVisible();
  });
});
