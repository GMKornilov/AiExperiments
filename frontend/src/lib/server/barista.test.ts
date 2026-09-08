import { afterEach, describe, expect, it, vi } from "vitest";
import { createDialog, listDialogs, sendMessage } from "./barista";

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.unstubAllEnvs(); });

const dialog = { id: "d", title: "Новый диалог", title_status: "idle", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", messages: [] };

describe("barista BFF", () => {
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
    await createDialog(new Request("http://web/api/dialogs", { method: "POST", headers: { cookie: `barista_session=${id}` } }));
    expect(fetchMock.mock.calls[1][1].headers["X-Session-ID"]).toBe(id);
  });

  it("rejects invalid bodies before contacting backend", async () => {
    const fetchMock = vi.fn(); vi.stubGlobal("fetch", fetchMock);
    const result = await sendMessage(new Request("http://web/api/dialogs/d/messages", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ text: "кофе", extra: true }) }), "d");
    expect(result.status).toBe(400); expect(fetchMock).toHaveBeenCalledWith(expect.any(URL), expect.objectContaining({ body: expect.stringContaining("dialog_validation_failed") }));
  });

  it("does not expose a provider response body", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ error: { category: "provider", message: "credential private-answer" } }, { status: 502 })));
    const result = await createDialog(new Request("http://web/api/dialogs", { method: "POST" }));
    expect(await result.text()).not.toMatch(/credential|private-answer/);
  });

  it("uses its own deadline rather than the caller signal", async () => {
    const timeout = vi.spyOn(AbortSignal, "timeout");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(dialog)));
    const controller = new AbortController(); controller.abort();
    const result = await createDialog(new Request("http://web/api/dialogs", { method: "POST", signal: controller.signal }));
    expect(result.status).toBe(200); expect(timeout).toHaveBeenCalledWith(35_000);
  });
});
