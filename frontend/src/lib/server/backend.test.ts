import { afterEach, describe, expect, it, vi } from "vitest";
import { checkBackendHealth, proxyChatRequest } from "./backend";

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("proxyChatRequest", () => {
  it("proxies JSON to the private backend without caching", async () => {
    vi.stubEnv("BARISTA_BACKEND_URL", "http://barista-api:8080");
    const fetchMock = vi.fn().mockResolvedValue(
      Response.json({ mode: "free", raw: "Ответ", data: null }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const body = JSON.stringify({ mode: "free", prompt: "кофе" });

    const response = await proxyChatRequest(
      new Request("http://frontend/api/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body,
      }),
    );

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ mode: "free", raw: "Ответ", data: null });
    expect(fetchMock).toHaveBeenCalledWith(
      new URL("http://barista-api:8080/api/chat"),
      expect.objectContaining({ method: "POST", body, cache: "no-store" }),
    );
  });

  it("rejects non-JSON without contacting the backend", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const response = await proxyChatRequest(
      new Request("http://frontend/api/chat", { method: "POST", body: "text" }),
    );

    expect(response.status).toBe(415);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects an invalid chat payload without contacting the backend", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);

    const response = await proxyChatRequest(
      new Request("http://frontend/api/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mode: "unsafe", prompt: "кофе" }),
      }),
    );

    expect(response.status).toBe(400);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("hides upstream errors", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        Response.json({ error: "private upstream details" }, { status: 502 }),
      ),
    );

    const response = await proxyChatRequest(
      new Request("http://frontend/api/chat", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ mode: "free", prompt: "кофе" }),
      }),
    );

    expect(response.status).toBe(502);
    expect(await response.text()).not.toContain("private upstream details");
  });
});

describe("checkBackendHealth", () => {
  it("reports an available backend", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ status: "ok" })));

    const response = await checkBackendHealth();

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ status: "ok" });
  });
});
