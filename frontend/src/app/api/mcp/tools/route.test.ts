import { afterEach, describe, expect, it, vi } from "vitest";
import { POST } from "./route";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});

describe("POST /api/mcp/tools", () => {
  it("передаёт bodyless POST в backend и возвращает нормализованный список tools", async () => {
    vi.stubEnv("BARISTA_BACKEND_URL", "https://backend.example/private");
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ tools: [{ name: "brewmark_list_filters", description: "List filters." }] }));
    vi.stubGlobal("fetch", fetchMock);
    const response = await POST(new Request("http://web/api/mcp/tools", { method: "POST", body: "ignored", headers: { "x-request-id": "request-42" } }));
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ tools: [{ name: "brewmark_list_filters", description: "List filters." }] });
    expect(String(fetchMock.mock.calls[0][0])).toBe("https://backend.example/api/mcp/tools");
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: "POST", headers: { "X-Request-ID": "request-42" } });
    expect(fetchMock.mock.calls[0][1]).not.toHaveProperty("body");
  });

  it.each([
    ["MCP_NOT_CONFIGURED", 503, "MCP endpoint is not configured."],
    ["MCP_UNAVAILABLE", 503, "MCP is temporarily unavailable."],
    ["MCP_PROTOCOL_ERROR", 502, "Unable to connect to MCP."],
    ["MCP_INVALID_RESPONSE", 502, "MCP returned an invalid response."],
  ] as const)("безопасно отображает валидную backend-категорию %s", async (code, status, message) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ code, message: "private backend error" }, { status })));
    const response = await POST(new Request("http://web/api/mcp/tools", { method: "POST" }));
    expect(response.status).toBe(status);
    expect(await response.json()).toEqual({ code, message });
  });

  it("отображает сетевую ошибку backend безопасной категорией", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("http://private.example/token")));
    const response = await POST(new Request("http://web/api/mcp/tools", { method: "POST" }));
    expect(response.status).toBe(503);
    expect(await response.json()).toEqual({ code: "MCP_UNAVAILABLE", message: "MCP is temporarily unavailable." });
  });

  it("отклоняет невалидный backend response и не раскрывает payload", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ tools: [{ name: "tool", description: "ok", secret: "token" }] })));
    const response = await POST(new Request("http://web/api/mcp/tools", { method: "POST" }));
    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({ code: "MCP_INVALID_RESPONSE", message: "MCP returned an invalid response." });
  });

  it("отклоняет backend response больше 64 KiB", async () => {
    const stream = new ReadableStream<Uint8Array>({ start(controller) { controller.enqueue(new Uint8Array(64 * 1024 + 1)); controller.close(); } });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(stream, { headers: { "Content-Type": "application/json" } })));
    const response = await POST(new Request("http://web/api/mcp/tools", { method: "POST" }));
    expect(response.status).toBe(502);
    expect(await response.json()).toEqual({ code: "MCP_INVALID_RESPONSE", message: "MCP returned an invalid response." });
  });

  it("ограничивает 5 s полным чтением backend response", async () => {
    vi.useFakeTimers();
    const cancel = vi.fn();
    const stream = new ReadableStream<Uint8Array>({ cancel });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(stream, { headers: { "Content-Type": "application/json" } })));
    const request = POST(new Request("http://web/api/mcp/tools", { method: "POST" }));
    const assertion = expect(request).resolves.toMatchObject({ status: 504 });
    await vi.advanceTimersByTimeAsync(5_000);
    await assertion;
    expect(cancel).toHaveBeenCalledOnce();
  });

  it("логирует валидный request ID без endpoint или raw payload", async () => {
    const info = vi.spyOn(console, "info").mockImplementation(() => undefined);
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ tools: [] })));
    await POST(new Request("http://web/api/mcp/tools", { method: "POST", headers: { "x-request-id": "request-42" } }));
    const record = JSON.parse(String(info.mock.calls[0][0]));
    expect(record).toMatchObject({ correlation_id: "request-42", source: "frontend_bff", event: "mcp_tools_request", operation: "mcp_tools_request", outcome: "success" });
    expect(record).toHaveProperty("duration_ms");
    expect(JSON.stringify(record)).not.toContain("mcp.example");
    expect(JSON.stringify(record)).not.toContain("token");
  });

  it("генерирует безопасный correlation ID при невалидном заголовке и логирует error category", async () => {
    const info = vi.spyOn(console, "info").mockImplementation(() => undefined);
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("private")));
    await POST(new Request("http://web/api/mcp/tools", { method: "POST", headers: { "x-request-id": "bad id with spaces" } }));
    const record = JSON.parse(String(info.mock.calls[0][0]));
    expect(record).toMatchObject({ operation: "mcp_tools_request", outcome: "failure", error_category: "MCP_UNAVAILABLE" });
    expect(record.correlation_id).not.toBe("bad id with spaces");
  });

  it("публикует exact safe BFF record в private collector с server-only token", async () => {
    vi.stubEnv("MCP_OBSERVABILITY_TOKEN", "collector-secret");
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(Response.json({ tools: [] }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetchMock);
    await POST(new Request("http://web/api/mcp/tools", { method: "POST", headers: { "x-request-id": "request-42" } }));
    const [endpoint, init] = fetchMock.mock.calls[1];
    expect(String(endpoint)).toContain("/api/internal/observability/mcp");
    expect(init).toMatchObject({ method: "POST", headers: { "Authorization": "Bearer collector-secret", "Content-Type": "application/json" } });
    expect(JSON.parse(init.body)).toMatchObject({ source: "frontend_bff", event: "mcp_tools_request", operation: "mcp_tools_request", outcome: "success", correlation_id: "request-42", http_status: 200 });
    expect(Object.keys(JSON.parse(init.body)).sort()).toEqual(["correlation_id", "duration_ms", "event", "http_status", "operation", "outcome", "source", "timestamp"]);
  });
});
