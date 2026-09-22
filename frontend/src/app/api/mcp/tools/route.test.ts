import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/server/mcp", () => ({
  getMCPURL: vi.fn(),
  listMCPTools: vi.fn(),
  MCPError: class MCPError extends Error {
    constructor(public code: string, message: string) { super(message); }
  },
}));

import { getMCPURL, listMCPTools, MCPError } from "@/lib/server/mcp";
import { POST } from "./route";

afterEach(() => vi.restoreAllMocks());

describe("POST /api/mcp/tools", () => {
  it("отдаёт нормализованный список tools", async () => {
    vi.mocked(getMCPURL).mockReturnValue(new URL("https://mcp.example/mcp"));
    vi.mocked(listMCPTools).mockResolvedValue([{ name: "brewmark_list_filters", description: "List filters." }]);
    const response = await POST(new Request("http://web/api/mcp/tools"));
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ tools: [{ name: "brewmark_list_filters", description: "List filters." }] });
  });

  it("не раскрывает ошибку MCP и возвращает заданный статус", async () => {
    vi.mocked(getMCPURL).mockImplementation(() => { throw new MCPError("MCP_TIMEOUT", "private endpoint and token"); });
    const response = await POST(new Request("http://web/api/mcp/tools"));
    expect(response.status).toBe(504);
    expect(await response.json()).toEqual({ code: "MCP_TIMEOUT", message: "MCP did not respond in time." });
  });

  it.each([
    ["MCP_NOT_CONFIGURED", 503, "MCP endpoint is not configured."],
    ["MCP_UNAVAILABLE", 503, "MCP is temporarily unavailable."],
    ["MCP_PROTOCOL_ERROR", 502, "Unable to connect to MCP."],
    ["MCP_INVALID_RESPONSE", 502, "MCP returned an invalid response."],
  ] as const)("безопасно отображает категорию %s", async (code, status, message) => {
    vi.mocked(getMCPURL).mockReturnValue(new URL("https://mcp.example/mcp"));
    vi.mocked(listMCPTools).mockRejectedValue(new MCPError(code, "https://secret.example/token"));
    const response = await POST(new Request("http://web/api/mcp/tools"));
    expect(response.status).toBe(status);
    expect(await response.json()).toEqual({ code, message });
  });

  it("логирует валидный request ID без endpoint или raw payload", async () => {
    const info = vi.spyOn(console, "info").mockImplementation(() => undefined);
    vi.mocked(getMCPURL).mockReturnValue(new URL("https://mcp.example/mcp"));
    vi.mocked(listMCPTools).mockResolvedValue([]);
    await POST(new Request("http://web/api/mcp/tools", { headers: { "x-request-id": "request-42" } }));
    const record = JSON.parse(String(info.mock.calls[0][0]));
    expect(record).toMatchObject({ correlation_id: "request-42", operation: "mcp_tools_list", outcome: "success" });
    expect(record).toHaveProperty("duration_ms");
    expect(JSON.stringify(record)).not.toContain("mcp.example");
    expect(JSON.stringify(record)).not.toContain("token");
  });

  it("генерирует безопасный correlation ID при невалидном заголовке и логирует error category", async () => {
    const info = vi.spyOn(console, "info").mockImplementation(() => undefined);
    vi.mocked(getMCPURL).mockImplementation(() => { throw new MCPError("MCP_NOT_CONFIGURED", "private"); });
    await POST(new Request("http://web/api/mcp/tools", { headers: { "x-request-id": "bad id with spaces" } }));
    const record = JSON.parse(String(info.mock.calls[0][0]));
    expect(record).toMatchObject({ operation: "mcp_tools_list", outcome: "failure", error_category: "MCP_NOT_CONFIGURED" });
    expect(record.correlation_id).not.toBe("bad id with spaces");
  });
});
