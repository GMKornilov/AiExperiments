import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { BaristaWorkspace } from "./barista-workspace";

const dialog = (messages: unknown[] = [], title_status: "idle" | "pending" | "success" | "error" = "idle") => ({ id: "dialog-123", title: "Новый диалог", title_status, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", messages });
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }));

afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });

describe("BaristaWorkspace", () => {
  it("не повторяет отправку, если сверка уже получила успешный ответ", async () => {
    const failed = { id: "m1", client_message_id: "c1", role: "user", text: "Кофе?", status: "error", created_at: "2026-01-01T00:00:00Z" };
    const answer = { id: "a1", role: "assistant", text: "Ответ", status: "success", created_at: "2026-01-01T00:00:01Z" };
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => json({ dialogs: [dialog([failed])], selected_dialog_id: "dialog-123" }))
      .mockImplementationOnce(() => json(dialog([failed, answer])));
    vi.stubGlobal("fetch", fetchMock);
    render(<BaristaWorkspace />);
    await screen.findByRole("button", { name: "Повторить отправку" });
    fireEvent.click(screen.getByRole("button", { name: "Повторить отправку" }));
    expect(await screen.findByText("Ответ")).toBeVisible();
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes("/retry"))).toBe(false);
  });

  it("оставляет pending без параллельного retry", async () => {
    const pending = { id: "m1", client_message_id: "c1", role: "user", text: "Кофе?", status: "pending", created_at: "2026-01-01T00:00:00Z" };
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => json({ dialogs: [dialog([{ ...pending, status: "error" }])], selected_dialog_id: "dialog-123" }))
      .mockImplementationOnce(() => json(dialog([pending])));
    vi.stubGlobal("fetch", fetchMock);
    render(<BaristaWorkspace />);
    await screen.findByRole("button", { name: "Повторить отправку" });
    fireEvent.click(screen.getByRole("button", { name: "Повторить отправку" }));
    await screen.findByText("Бариста готовит ответ…");
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes("/retry"))).toBe(false);
  });

  it("не отправляет local-only retry, когда обязательная сверка недоступна", async () => {
    const localError = { id: "local-c1", client_message_id: "c1", role: "user", text: "Кофе?", status: "error", created_at: "2026-01-01T00:00:00Z", localOnly: true };
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => json({ dialogs: [dialog([localError])], selected_dialog_id: "dialog-123" }))
      .mockRejectedValueOnce(new Error("offline"));
    vi.stubGlobal("fetch", fetchMock);
    render(<BaristaWorkspace />);
    await screen.findByRole("button", { name: "Повторить отправку" });
    fireEvent.click(screen.getByRole("button", { name: "Повторить отправку" }));
    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Не удалось получить ответ"));
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("сохраняет один persisted user bubble при неудачном retry", async () => {
    const failed = { id: "m1", client_message_id: "c1", role: "user", text: "Кофе?", status: "error", created_at: "2026-01-01T00:00:00Z" };
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => json({ dialogs: [dialog([failed])], selected_dialog_id: "dialog-123" }))
      .mockImplementationOnce(() => json(dialog([failed])))
      .mockRejectedValueOnce(new Error("offline"));
    vi.stubGlobal("fetch", fetchMock);
    render(<BaristaWorkspace />);
    await screen.findByRole("button", { name: "Повторить отправку" });
    fireEvent.click(screen.getByRole("button", { name: "Повторить отправку" }));
    await waitFor(() => expect(screen.getAllByText("Кофе?")).toHaveLength(1));
    expect(screen.getByRole("button", { name: "Повторить отправку" })).toBeVisible();
  });

  it("сохраняет optimistic bubble при polling до принятия сервером", async () => {
    let resolveSend!: (value: Response) => void;
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => json({ dialogs: [dialog()], selected_dialog_id: "dialog-123" }))
      .mockImplementationOnce(() => new Promise<Response>((resolve) => { resolveSend = resolve; }))
      .mockImplementationOnce(() => json({ dialogs: [dialog()], selected_dialog_id: "dialog-123" }));
    vi.stubGlobal("fetch", fetchMock);
    render(<BaristaWorkspace />);
    fireEvent.change(await screen.findByLabelText("Ваш вопрос"), { target: { value: "Не теряй меня" } });
    vi.useFakeTimers();
    fireEvent.click(screen.getByRole("button", { name: "Отправить" }));
    await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
    expect(screen.getByText("Не теряй меня")).toBeVisible();
    resolveSend(new Response(JSON.stringify(dialog()), { headers: { "Content-Type": "application/json" } }));
  });

  it("считает лимит ввода Unicode code points", async () => {
    const request = vi.fn()
      .mockImplementationOnce(() => json({ dialogs: [dialog()], selected_dialog_id: "dialog-123" }))
      .mockImplementationOnce(() => json(dialog()));
    vi.stubGlobal("fetch", request);
    render(<BaristaWorkspace />);
    const value = "😀".repeat(4000);
    fireEvent.change(await screen.findByLabelText("Ваш вопрос"), { target: { value } });
    fireEvent.click(screen.getByRole("button", { name: "Отправить" }));
    await waitFor(() => expect(request).toHaveBeenCalledTimes(2));
    expect(JSON.parse(request.mock.calls[1][1].body).text).toBe(value);
  });

  it("копирует полный ID чата и не блокирует composer при pending названии", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(json({ dialogs: [dialog([], "pending")], selected_dialog_id: "dialog-123" })));
    render(<BaristaWorkspace />);
    await screen.findByLabelText("Ваш вопрос");
    expect(screen.getByLabelText("Ваш вопрос")).not.toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Копировать ID чата" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("dialog-123"));
    expect(screen.getByText("ID чата скопирован")).toBeInTheDocument();
  });

  it("объявляет безопасную ошибку, если ID чата не удалось скопировать", async () => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockRejectedValue(new Error("denied")) } });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(json({ dialogs: [dialog()], selected_dialog_id: "dialog-123" })));
    render(<BaristaWorkspace />);
    await screen.findByRole("button", { name: "Копировать ID чата" });
    fireEvent.click(screen.getByRole("button", { name: "Копировать ID чата" }));
    expect(await screen.findByText("Не удалось скопировать ID чата.")).toBeVisible();
  });

  it("удаление не возвращает поздний ответ", async () => {
    let resolveSend!: (value: Response) => void;
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => json({ dialogs: [dialog()], selected_dialog_id: "dialog-123" }))
      .mockImplementationOnce(() => new Promise<Response>((resolve) => { resolveSend = resolve; }))
      .mockImplementationOnce(() => Promise.resolve(new Response(null, { status: 204 })))
      .mockImplementationOnce(() => json({ dialogs: [], selected_dialog_id: null }));
    vi.stubGlobal("fetch", fetchMock);
    render(<BaristaWorkspace />);
    await screen.findByLabelText("Ваш вопрос");
    fireEvent.change(screen.getByLabelText("Ваш вопрос"), { target: { value: "Кофе?" } });
    fireEvent.click(screen.getByRole("button", { name: "Отправить" }));
    fireEvent.click(screen.getByRole("button", { name: /Удалить диалог/ }));
    const cancel = screen.getByRole("button", { name: "Отмена" });
    expect(cancel).toHaveFocus();
    const confirm = screen.getByRole("button", { name: "Удалить" });
    confirm.focus();
    fireEvent.keyDown(screen.getByRole("alertdialog").parentElement!, { key: "Tab" });
    expect(cancel).toHaveFocus();
    fireEvent.click(screen.getByRole("button", { name: "Удалить" }));
    resolveSend(new Response(JSON.stringify(dialog([{ id: "a1", role: "assistant", text: "Поздно", status: "success", created_at: "2026-01-01T00:00:01Z" }])), { headers: { "Content-Type": "application/json" } }));
    await waitFor(() => expect(screen.queryByText("Поздно")).not.toBeInTheDocument());
  });
});
