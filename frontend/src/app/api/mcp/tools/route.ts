import "server-only";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const timeoutMilliseconds = 5_000;
const maxResponseBytes = 64 * 1024;

const statuses = {
  MCP_NOT_CONFIGURED: 503,
  MCP_UNAVAILABLE: 503,
  MCP_TIMEOUT: 504,
  MCP_PROTOCOL_ERROR: 502,
  MCP_INVALID_RESPONSE: 502,
} as const;

type ErrorCode = keyof typeof statuses;
type Tool = { name: string; description: string };
type ErrorResponse = { code: ErrorCode; message: string };
type Outcome = "success" | "failure";

class BFFError extends Error {
  constructor(public readonly code: ErrorCode) {
    super(messages[code]);
  }
}

const messages = {
  MCP_NOT_CONFIGURED: "MCP endpoint is not configured.",
  MCP_UNAVAILABLE: "MCP is temporarily unavailable.",
  MCP_TIMEOUT: "MCP did not respond in time.",
  MCP_PROTOCOL_ERROR: "Unable to connect to MCP.",
  MCP_INVALID_RESPONSE: "MCP returned an invalid response.",
} as const;

export async function POST(request: Request): Promise<Response> {
  const startedAt = Date.now();
  const correlationID = getCorrelationID(request);
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMilliseconds);
  let backendStatus: number | undefined;
  try {
    const response = await fetchBackend(correlationID, controller.signal);
    backendStatus = response.status;
    const body = await readJSON(response, controller.signal);
    if (response.ok) {
      const tools = projectTools(body);
      if (!tools) throw new BFFError("MCP_INVALID_RESPONSE");
      await recordAttempt(correlationID, "success", undefined, startedAt, response.status);
      return Response.json({ tools });
    }

    const error = projectError(response.status, body);
    if (!error) throw new BFFError("MCP_INVALID_RESPONSE");
    await recordAttempt(correlationID, "failure", error.code, startedAt, response.status);
    return Response.json(error, { status: response.status });
  } catch (error) {
    const safeError = controller.signal.aborted
      ? new BFFError("MCP_TIMEOUT")
      : error instanceof BFFError ? error : mapFetchError(error);
    await recordAttempt(correlationID, "failure", safeError.code, startedAt, backendStatus);
    return Response.json(
      { code: safeError.code, message: messages[safeError.code] },
      { status: statuses[safeError.code] },
    );
  } finally {
    clearTimeout(timer);
  }
}

async function fetchBackend(correlationID: string, signal: AbortSignal): Promise<Response> {
  try {
    return await fetch(backendURL(), {
      method: "POST",
      headers: { "X-Request-ID": correlationID },
      cache: "no-store",
      signal,
    });
  } catch (error) {
    if (signal.aborted || isAbortError(error)) throw new BFFError("MCP_TIMEOUT");
    throw new BFFError("MCP_UNAVAILABLE");
  }
}

function backendURL(): URL {
  const raw = process.env.BARISTA_BACKEND_URL ?? "http://127.0.0.1:8080";
  try {
    const base = new URL(raw);
    if ((base.protocol !== "http:" && base.protocol !== "https:") || base.username || base.password || base.search || base.hash) throw new Error("invalid backend URL");
    return new URL("/api/mcp/tools", base);
  } catch {
    throw new BFFError("MCP_UNAVAILABLE");
  }
}

async function readJSON(response: Response, signal: AbortSignal): Promise<unknown> {
  if (!response.headers.get("content-type")?.toLowerCase().startsWith("application/json")) throw new BFFError("MCP_INVALID_RESPONSE");
  const length = Number(response.headers.get("content-length"));
  if (Number.isFinite(length) && length > maxResponseBytes) throw new BFFError("MCP_INVALID_RESPONSE");
  if (!response.body) throw new BFFError("MCP_INVALID_RESPONSE");

  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const next = await readChunk(reader, signal);
      if (next.done) break;
      size += next.value.byteLength;
      if (size > maxResponseBytes) {
        await reader.cancel();
        throw new BFFError("MCP_INVALID_RESPONSE");
      }
      chunks.push(next.value);
    }
    const bytes = new Uint8Array(size);
    let offset = 0;
    for (const chunk of chunks) {
      bytes.set(chunk, offset);
      offset += chunk.byteLength;
    }
    return JSON.parse(new TextDecoder().decode(bytes)) as unknown;
  } catch (error) {
    await reader.cancel().catch(() => undefined);
    if (error instanceof BFFError) throw error;
    if (signal.aborted || isAbortError(error)) throw new BFFError("MCP_TIMEOUT");
    throw new BFFError("MCP_INVALID_RESPONSE");
  }
}

function readChunk(reader: ReadableStreamDefaultReader<Uint8Array>, signal: AbortSignal): Promise<ReadableStreamReadResult<Uint8Array>> {
  if (signal.aborted) return Promise.reject(new DOMException("Backend request timed out", "AbortError"));
  return new Promise((resolve, reject) => {
    const onAbort = () => reject(new DOMException("Backend request timed out", "AbortError"));
    signal.addEventListener("abort", onAbort, { once: true });
    reader.read().then(resolve, reject).finally(() => signal.removeEventListener("abort", onAbort));
  });
}

function projectTools(value: unknown): Tool[] | null {
  if (!isRecord(value) || !hasOnlyKeys(value, ["tools"]) || !Array.isArray(value.tools)) return null;
  const tools: Tool[] = [];
  for (const item of value.tools) {
    if (!isRecord(item) || !hasOnlyKeys(item, ["name", "description"]) || !isNonEmptyString(item.name) || !isNonEmptyString(item.description)) return null;
    tools.push({ name: item.name, description: item.description });
  }
  return tools;
}

function projectError(status: number, value: unknown): ErrorResponse | null {
  if (!isRecord(value) || !hasOnlyKeys(value, ["code", "message"]) || !isErrorCode(value.code) || typeof value.message !== "string" || status !== statuses[value.code]) return null;
  return { code: value.code, message: messages[value.code] };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function hasOnlyKeys(value: Record<string, unknown>, keys: string[]): boolean {
  return Object.keys(value).every((key) => keys.includes(key));
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

function isErrorCode(value: unknown): value is ErrorCode {
  return typeof value === "string" && Object.hasOwn(statuses, value);
}

function mapFetchError(error: unknown): BFFError {
  return isAbortError(error) ? new BFFError("MCP_TIMEOUT") : new BFFError("MCP_UNAVAILABLE");
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}

function getCorrelationID(request: Request): string {
  const candidate = request.headers.get("x-request-id");
  if (candidate && /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(candidate)) return candidate;
  return crypto.randomUUID();
}

async function recordAttempt(
  correlationID: string,
  outcome: Outcome,
  errorCategory: ErrorCode | undefined,
  startedAt: number,
  httpStatus?: number,
): Promise<void> {
  const record = {
    timestamp: new Date().toISOString(),
    source: "frontend_bff",
    event: "mcp_tools_request",
    operation: "mcp_tools_request",
    outcome,
    correlation_id: correlationID,
    duration_ms: Date.now() - startedAt,
    ...(httpStatus === undefined ? {} : { http_status: httpStatus }),
    ...(errorCategory ? { error_category: errorCategory } : {}),
  };
  console.info(JSON.stringify(record));
  await publishMCPRecord(record);
}

async function publishMCPRecord(record: Record<string, unknown>): Promise<void> {
  const token = process.env.MCP_OBSERVABILITY_TOKEN;
  if (!token?.trim()) {
    logCollectorFailure(record.correlation_id);
    return;
  }

  let endpoint: URL;
  try {
    endpoint = new URL("/api/internal/observability/mcp", backendURL());
  } catch {
    logCollectorFailure(record.correlation_id);
    return;
  }
  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      const response = await fetch(endpoint, {
        method: "POST",
        headers: { "Authorization": `Bearer ${token}`, "Content-Type": "application/json" },
        body: JSON.stringify(record),
        cache: "no-store",
        signal: AbortSignal.timeout(400),
      });
      if (response.status === 204) return;
    } catch {
      // Collector failure is intentionally isolated from the product response.
    }
  }
  logCollectorFailure(record.correlation_id);
}

function logCollectorFailure(correlationID: unknown): void {
  console.info(JSON.stringify({
    source: "frontend_bff",
    operation: "mcp_observability_publish",
    outcome: "failure",
    correlation_id: correlationID,
    error_category: "collector_unavailable",
  }));
}
