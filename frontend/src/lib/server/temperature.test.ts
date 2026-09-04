import { afterEach, describe, expect, it, vi } from "vitest";
import { proxyTemperatureRequest } from "./temperature";

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("proxyTemperatureRequest", () => {
  it("normalizes and proxies one valid temperature request", async () => {
    vi.stubEnv("BARISTA_BACKEND_URL", "http://barista-api:8080");
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ answer: "  Ответ  " }));
    vi.stubGlobal("fetch", fetchMock);

    const response = await proxyTemperatureRequest(new Request("http://frontend/api/temperature", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ prompt: "  кофе  ", temperature: 0.7 }),
    }));

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ answer: "Ответ" });
    expect(response.headers.get("cache-control")).toBe("no-store");
    expect(fetchMock).toHaveBeenCalledWith(new URL("http://barista-api:8080/api/temperature"), expect.objectContaining({
      method: "POST",
      body: JSON.stringify({ prompt: "кофе", temperature: 0.7 }),
      cache: "no-store",
    }));
  });

  it.each([
    {},
    { prompt: "", temperature: 0.7 },
    { prompt: "кофе", temperature: "0.7" },
    { prompt: "кофе", temperature: -0.1 },
    { prompt: "кофе", temperature: 2.1 },
    { prompt: "кофе", temperature: 0.7, mode: "free" },
  ])("rejects invalid payload %# without contacting backend", async (payload) => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const response = await proxyTemperatureRequest(new Request("http://frontend/api/temperature", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload),
    }));
    expect(response.status).toBe(400);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects non-JSON and hides bad upstream responses", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const nonJSON = await proxyTemperatureRequest(new Request("http://frontend/api/temperature", { method: "POST", body: "text" }));
    expect(nonJSON.status).toBe(415);
    expect(fetchMock).not.toHaveBeenCalled();

    fetchMock.mockResolvedValue(Response.json({ error: "secret upstream body" }, { status: 502 }));
    const failed = await proxyTemperatureRequest(new Request("http://frontend/api/temperature", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ prompt: "кофе", temperature: 0 }),
    }));
    expect(failed.status).toBe(502);
    expect(await failed.text()).not.toContain("secret upstream body");
  });

  it("rejects JSONP both from the caller and from upstream", async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const input = await proxyTemperatureRequest(new Request("http://frontend/api/temperature", {
      method: "POST", headers: { "Content-Type": "application/jsonp" }, body: JSON.stringify({ prompt: "кофе", temperature: 0 }),
    }));
    expect(input.status).toBe(415);
    expect(fetchMock).not.toHaveBeenCalled();

    fetchMock.mockResolvedValue(new Response('{"answer":"Ответ"}', { headers: { "Content-Type": "application/jsonp" } }));
    const upstream = await proxyTemperatureRequest(new Request("http://frontend/api/temperature", {
      method: "POST", headers: { "Content-Type": "application/json; charset=utf-8" }, body: JSON.stringify({ prompt: "кофе", temperature: 0 }),
    }));
    expect(upstream.status).toBe(502);
  });
});
