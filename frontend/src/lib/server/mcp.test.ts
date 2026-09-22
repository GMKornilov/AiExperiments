import { afterEach, describe, expect, it, vi } from "vitest";
import { getMCPURL, listMCPTools, MCPError, normalizeTools, readMCPResponseWithinLimit } from "./mcp";

afterEach(() => vi.useRealTimers());

describe("MCP adapter", () => {
  it("принимает только безопасный URL endpoint без query и credentials", () => {
    expect(getMCPURL("https://mcp.example/mcp").toString()).toBe("https://mcp.example/mcp");
    expect(() => getMCPURL(undefined)).toThrow(MCPError);
    expect(() => getMCPURL("https://user:secret@mcp.example/mcp")).toThrow(MCPError);
    expect(() => getMCPURL("https://mcp.example/mcp?token=secret")).toThrow(MCPError);
  });

  it("последовательно подключается, читает tools/list и возвращает только name с description", async () => {
    const connect = vi.fn().mockResolvedValue(undefined);
    const listTools = vi.fn().mockResolvedValue({ tools: [{ name: "brewmark_list_grinders", description: "List grinders.", inputSchema: { type: "object" } }] });
    const close = vi.fn().mockResolvedValue(undefined);
    const createTransport = vi.fn().mockReturnValue({});

    await expect(listMCPTools(new URL("https://mcp.example/mcp"), "request-123", {
      createClient: () => ({ connect, listTools, close }),
      createTransport: createTransport as never,
    })).resolves.toEqual([{ name: "brewmark_list_grinders", description: "List grinders." }]);
    expect(connect).toHaveBeenCalledBefore(listTools);
    expect(createTransport).toHaveBeenCalledWith(expect.any(URL), expect.objectContaining({
      requestInit: { headers: { "X-Request-ID": "request-123" } },
    }));
    expect(close).toHaveBeenCalledOnce();
  });

  it("отвергает tool без непустого name или description", () => {
    expect(() => normalizeTools([{ name: "grinders", description: "" }])).toThrow("MCP returned an invalid response.");
    expect(() => normalizeTools([{ name: "", description: "List grinders." }])).toThrow("MCP returned an invalid response.");
  });

  it("превращает недоступность MCP в безопасную категорию и закрывает клиента", async () => {
    const close = vi.fn().mockResolvedValue(undefined);
    await expect(listMCPTools(new URL("https://mcp.example/mcp"), "request-123", {
      createClient: () => ({ connect: vi.fn().mockRejectedValue(new Error("network details")), listTools: vi.fn(), close }),
      createTransport: (() => ({})) as never,
    })).rejects.toMatchObject({ code: "MCP_UNAVAILABLE" });
    expect(close).toHaveBeenCalledOnce();
  });

  it("отделяет protocol и invalid-response ошибки SDK от недоступности", async () => {
    const protocol = new Error("lifecycle"); protocol.name = "ProtocolError";
    await expect(listMCPTools(new URL("https://mcp.example/mcp"), "request-123", {
      createClient: () => ({ connect: vi.fn().mockRejectedValue(protocol), listTools: vi.fn(), close: vi.fn().mockResolvedValue(undefined) }),
      createTransport: (() => ({})) as never,
    })).rejects.toMatchObject({ code: "MCP_PROTOCOL_ERROR" });
    await expect(listMCPTools(new URL("https://mcp.example/mcp"), "request-123", {
      createClient: () => ({ connect: vi.fn().mockRejectedValue(new SyntaxError("invalid JSON")), listTools: vi.fn(), close: vi.fn().mockResolvedValue(undefined) }),
      createTransport: (() => ({})) as never,
    })).rejects.toMatchObject({ code: "MCP_INVALID_RESPONSE" });
  });

  it("прерывает полную MCP-операцию по таймауту", async () => {
    vi.useFakeTimers();
    const request = listMCPTools(new URL("https://mcp.example/mcp"), "request-123", {
      createClient: () => ({ connect: vi.fn().mockImplementation(() => new Promise<void>(() => undefined)), listTools: vi.fn(), close: vi.fn().mockResolvedValue(undefined) }),
      createTransport: (() => ({})) as never,
    });
    const assertion = expect(request).rejects.toMatchObject({ code: "MCP_TIMEOUT" });
    await vi.advanceTimersByTimeAsync(5_000);
    await assertion;
  });

  it("не читает поток MCP больше 64 KiB и отменяет его", async () => {
    const cancel = vi.fn();
    const stream = new ReadableStream<Uint8Array>({
      start(controller) { controller.enqueue(new Uint8Array(64 * 1024 + 1)); },
      cancel,
    });
    await expect(readMCPResponseWithinLimit(new Response(stream))).rejects.toMatchObject({ code: "MCP_INVALID_RESPONSE" });
    expect(cancel).toHaveBeenCalledOnce();
  });

  it("сохраняет пустой валидный tools/list", () => {
    expect(normalizeTools([])).toEqual([]);
  });
});
