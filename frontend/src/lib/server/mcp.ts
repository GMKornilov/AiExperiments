import "server-only";

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport, StreamableHTTPError } from "@modelcontextprotocol/sdk/client/streamableHttp.js";

const REQUEST_TIMEOUT_MS = 5_000;
const MAX_RESPONSE_BYTES = 64 * 1024;

export type MCPTool = { name: string; description: string };

export type MCPErrorCode =
  | "MCP_NOT_CONFIGURED"
  | "MCP_UNAVAILABLE"
  | "MCP_TIMEOUT"
  | "MCP_PROTOCOL_ERROR"
  | "MCP_INVALID_RESPONSE";

export class MCPError extends Error {
  constructor(
    public readonly code: MCPErrorCode,
    message: string,
  ) {
    super(message);
  }
}

type MCPClient = {
  connect: (transport: StreamableHTTPClientTransport) => Promise<void>;
  listTools: () => Promise<{ tools: unknown[] }>;
  close: () => Promise<void>;
};

type Dependencies = {
  createClient?: () => MCPClient;
  createTransport?: (url: URL, options: ConstructorParameters<typeof StreamableHTTPClientTransport>[1]) => StreamableHTTPClientTransport;
};

export function getMCPURL(value = process.env.BREWMARK_MCP_URL): URL {
  if (!value?.trim()) {
    throw new MCPError("MCP_NOT_CONFIGURED", "MCP endpoint is not configured.");
  }

  try {
    const url = new URL(value);
    if (!["http:", "https:"].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
      throw new Error("invalid URL");
    }
    return url;
  } catch {
    throw new MCPError("MCP_NOT_CONFIGURED", "MCP endpoint is not configured.");
  }
}

export async function listMCPTools(url: URL, correlationID: string, dependencies: Dependencies = {}): Promise<MCPTool[]> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
  const fetchWithLimits: typeof fetch = async (input, init) => {
    const response = await fetch(input, { ...init, signal: controller.signal });
    const declaredSize = Number(response.headers.get("content-length"));
    if (Number.isFinite(declaredSize) && declaredSize > MAX_RESPONSE_BYTES) {
      throw new MCPError("MCP_INVALID_RESPONSE", "MCP returned an invalid response.");
    }
    return readMCPResponseWithinLimit(response);
  };
  const createClient = dependencies.createClient ?? (() => new Client({ name: "ai-barista-frontend", version: "0.1.0" }));
  const createTransport = dependencies.createTransport ?? ((endpoint, options) => new StreamableHTTPClientTransport(endpoint, options));
  const client = createClient();

  try {
    const transport = createTransport(url, {
      fetch: fetchWithLimits,
      requestInit: { headers: { "X-Request-ID": correlationID } },
    });
    await withinTimeout(client.connect(transport), controller);
    const result = await withinTimeout(client.listTools(), controller);
    return normalizeTools(result.tools);
  } catch (error) {
    if (error instanceof MCPError) throw error;
    if (controller.signal.aborted || isAbortError(error)) {
      throw new MCPError("MCP_TIMEOUT", "MCP did not respond in time.");
    }
    if (error instanceof SyntaxError || isInvalidResponseError(error)) {
      throw new MCPError("MCP_INVALID_RESPONSE", "MCP returned an invalid response.");
    }
    if (isProtocolError(error)) {
      throw new MCPError("MCP_PROTOCOL_ERROR", "Unable to connect to MCP.");
    }
    // StreamableHTTPError denotes a transport-level HTTP failure; never expose its status or text.
    if (error instanceof StreamableHTTPError) {
      throw new MCPError("MCP_UNAVAILABLE", "MCP is temporarily unavailable.");
    }
    throw new MCPError("MCP_UNAVAILABLE", "MCP is temporarily unavailable.");
  } finally {
    clearTimeout(timer);
    await client.close().catch(() => undefined);
  }
}

export function normalizeTools(value: unknown[]): MCPTool[] {
  if (!Array.isArray(value)) {
    throw new MCPError("MCP_INVALID_RESPONSE", "MCP returned an invalid response.");
  }
  return value.map((tool) => {
    if (!isRecord(tool) || !isNonEmptyString(tool.name) || !isNonEmptyString(tool.description)) {
      throw new MCPError("MCP_INVALID_RESPONSE", "MCP returned an invalid response.");
    }
    return { name: tool.name, description: tool.description };
  });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

export async function readMCPResponseWithinLimit(response: Response): Promise<Response> {
  if (!response.body) return response;

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let byteLength = 0;
  try {
    for (;;) {
      const next = await reader.read();
      if (next.done) break;
      byteLength += next.value.byteLength;
      if (byteLength > MAX_RESPONSE_BYTES) {
        await reader.cancel();
        throw new MCPError("MCP_INVALID_RESPONSE", "MCP returned an invalid response.");
      }
      chunks.push(next.value);
    }
  } catch (error) {
    await reader.cancel().catch(() => undefined);
    throw error;
  }

  const body = new Uint8Array(byteLength);
  let offset = 0;
  for (const chunk of chunks) {
    body.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new Response(body, { headers: response.headers, status: response.status, statusText: response.statusText });
}

function isInvalidResponseError(error: unknown): boolean {
  return error instanceof Error && (error.name === "ZodError" || error.name === "ParseError");
}

function isProtocolError(error: unknown): boolean {
  return error instanceof Error && (error.name === "McpError" || error.name === "ProtocolError");
}

async function withinTimeout<T>(operation: Promise<T>, controller: AbortController): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const onAbort = () => reject(new DOMException("MCP request timed out", "AbortError"));
    controller.signal.addEventListener("abort", onAbort, { once: true });
    operation.then(resolve, reject).finally(() => controller.signal.removeEventListener("abort", onAbort));
  });
}
