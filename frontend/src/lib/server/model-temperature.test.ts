import { afterEach, describe, expect, it, vi } from "vitest";
import { proxyModelTemperatureRequest } from "./model-temperature";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

const metrics = { duration_ms: 1200, input_tokens: 12, output_tokens: 34, cost_usd: 0.0001 };

function request(payload: object): Request {
  return new Request("http://frontend/api/model-temperature", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
}

describe("proxyModelTemperatureRequest", () => {
  it("allows backend model requests 180 seconds plus transport margin", async () => {
    const timeout = vi.spyOn(AbortSignal, "timeout");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ answer: "Ответ", metrics })));
    await proxyModelTemperatureRequest(request({ prompt: "тест", temperature: 1, provider: "kimi", model: "kimi-k3" }));
    expect(timeout).toHaveBeenCalledWith(190_000);
  });
  it("correlates backend calls and logs safe error metadata", async () => {
    const logs = vi.spyOn(console, "info").mockImplementation(() => {});
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ error: "private-key private-answer" }, { status: 502 }));
    vi.stubGlobal("fetch", fetchMock);
    const response = await proxyModelTemperatureRequest(request({ prompt: "private-prompt", temperature: 1, provider: "kimi", model: "kimi-k3" }));
    const requestID = response.headers.get("X-Request-ID");
    expect(requestID).toMatch(/^[a-f0-9-]{36}$/);
    expect(fetchMock).toHaveBeenCalledWith(expect.any(URL), expect.objectContaining({
      headers: { "Content-Type": "application/json", "X-Request-ID": requestID },
    }));
    const output = logs.mock.calls.map((call) => String(call[0])).join("\n");
    expect(output).toContain("backend_http_error");
    expect(output).toContain('"backend_status":502');
    expect(output).toContain(requestID!);
    expect(output).not.toMatch(/private-key|private-prompt|private-answer/);
  });
  it.each([
    ["deepseek", "deepseek-v4-flash"],
    ["deepseek", "deepseek-v4-pro"],
    ["deepseek", "deepseek-v4-flash-vision-exp"],
    ["kimi", "kimi-k3"],
    ["kimi", "kimi-k2.7-code"],
    ["kimi", "kimi-k2.6"],
  ])("normalizes and proxies %s model %s", async (provider, model) => {
    vi.stubEnv("BARISTA_BACKEND_URL", "http://barista-api:8080");
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ answer: "  Ответ  ", metrics }));
    vi.stubGlobal("fetch", fetchMock);

    const temperature = provider === "kimi" ? 1 : 0.7;
    const response = await proxyModelTemperatureRequest(request({ prompt: "  кофе  ", temperature, provider, model }));

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ answer: "Ответ", metrics });
    expect(fetchMock).toHaveBeenCalledWith(new URL("http://barista-api:8080/api/model-temperature"), expect.objectContaining({
      body: JSON.stringify({ prompt: "кофе", temperature, provider, model }),
      cache: "no-store",
    }));
  });

  it.each([
    {},
    { prompt: "", temperature: 0.7, provider: "deepseek", model: "deepseek-v4-flash" },
    { prompt: "кофе", temperature: 0.7, provider: "deepseek", model: "deepseek-chat" },
    { prompt: "кофе", temperature: 0.7, provider: "kimi", model: "deepseek-v4-flash" },
    { prompt: "кофе", temperature: 1.2, provider: "kimi", model: "kimi-k3" },
    { prompt: "кофе", temperature: 0.7, provider: "kimi", model: "kimi-k3" },
    { prompt: "кофе", temperature: "0.7", provider: "deepseek", model: "deepseek-v4-flash" },
    { prompt: "кофе", temperature: 0.7, provider: "deepseek", model: "deepseek-v4-flash", extra: true },
  ])("rejects invalid payload %# without contacting backend", async (payload) => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    const response = await proxyModelTemperatureRequest(request(payload));
    expect(response.status).toBe(400);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("hides upstream errors", async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ error: "secret upstream body" }, { status: 502 }));
    vi.stubGlobal("fetch", fetchMock);
    const response = await proxyModelTemperatureRequest(request({ prompt: "кофе", temperature: 0, provider: "deepseek", model: "deepseek-v4-flash" }));
    expect(response.status).toBe(502);
    expect(await response.text()).not.toContain("secret upstream body");
  });
});
