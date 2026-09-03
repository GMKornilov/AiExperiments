import "server-only";
import { Agent } from "undici";

const defaultBackendURL = "http://127.0.0.1:8080";
const maxRequestBytes = 64 * 1024;
const maxResponseBytes = 8 * 1024 * 1024;
const maxStatementCodePoints = 10_000;
const maxLLMTextBytes = 1024 * 1024;
const maxComposedMessageBytes = maxLLMTextBytes + maxRequestBytes;
const inputDeadlineMilliseconds = 10_000;
const algorithmRequestDeadlineMilliseconds = 185_000;
const generatedPromptRequestDeadlineMilliseconds = 365_000;
export const algorithmTransportTimeoutMilliseconds = 370_000;

export function createAlgorithmsDispatcher(AgentConstructor: typeof Agent = Agent): Agent {
  return new AgentConstructor({
    headersTimeout: algorithmTransportTimeoutMilliseconds,
    bodyTimeout: algorithmTransportTimeoutMilliseconds,
  });
}

const algorithmsDispatcher = createAlgorithmsDispatcher();

export const algorithmMethods = [
  "direct",
  "step-by-step",
  "generated-prompt",
  "experts",
] as const;

export type AlgorithmMethod = (typeof algorithmMethods)[number];
type AlgorithmLanguage = "python" | "java" | "cpp";
type AlgorithmErrorCode = "invalid_request" | "unavailable" | "timeout" | "invalid_response";

type AlgorithmPayload = { statement: string; language: AlgorithmLanguage };
type TraceMessage = { role: "system" | "user" | "assistant"; content: string };
type TraceStep = { step: "generate-prompt" | "solution"; messages: TraceMessage[]; response?: string };
type AlgorithmEnvelope = {
  method: AlgorithmMethod;
  status: "success" | "error";
  answer: string;
  trace: TraceStep[];
  error?: { code: AlgorithmErrorCode; message: string };
};

const errorMessages: Record<AlgorithmErrorCode, string> = {
  invalid_request: "Проверьте заполнение формы и попробуйте снова.",
  unavailable: "Сервис временно недоступен. Повторите запуск позже.",
  timeout: "Время ожидания истекло. Повторите запуск.",
  invalid_response: "Ответ сервиса нельзя прочитать. Повторите запуск.",
};

function errorEnvelope(method: AlgorithmMethod, code: AlgorithmErrorCode, trace: TraceStep[] = []): AlgorithmEnvelope {
  return { method, status: "error", answer: "", trace, error: { code, message: errorMessages[code] } };
}

function backendURL(pathname: string): URL {
  const configuredURL = process.env.ALGORITHMS_BACKEND_URL ?? process.env.BARISTA_BACKEND_URL ?? defaultBackendURL;
  const baseURL = new URL(configuredURL.endsWith("/") ? configuredURL : `${configuredURL}/`);
  if (baseURL.protocol !== "http:" && baseURL.protocol !== "https:") throw new Error("Unsupported backend protocol");
  return new URL(pathname.replace(/^\//, ""), baseURL);
}

async function readLimitedBody(
  stream: ReadableStream<Uint8Array> | null,
  maximum: number,
  deadline: number,
  signal?: AbortSignal,
): Promise<Uint8Array | null> {
  if (!stream) return new Uint8Array();
  const reader = stream.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  let finished = false;
  let timedOut = false;
  const expire = () => {
    timedOut = true;
    void reader.cancel().catch(() => undefined);
  };
  const timer = deadline > 0 ? setTimeout(expire, deadline) : undefined;
  if (signal?.aborted) expire();
  else if (signal) signal.addEventListener("abort", expire, { once: true });

  try {
    for (;;) {
      const chunk = await reader.read();
      if (chunk.done) {
        finished = true;
        break;
      }
      size += chunk.value.byteLength;
      if (size > maximum) {
        return null;
      }
      chunks.push(chunk.value);
    }
    if (timedOut) return null;
    const body = new Uint8Array(size);
    let offset = 0;
    for (const chunk of chunks) {
      body.set(chunk, offset);
      offset += chunk.byteLength;
    }
    return body;
  } catch {
    return null;
  } finally {
    if (timer !== undefined) clearTimeout(timer);
    if (signal) signal.removeEventListener("abort", expire);
    if (!finished) await reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}

function parsePayload(body: Uint8Array): AlgorithmPayload | null {
  try {
    const value: unknown = JSON.parse(new TextDecoder().decode(body));
    if (!value || typeof value !== "object" || Array.isArray(value)) return null;
    const candidate = value as Record<string, unknown>;
    const keys = Object.keys(candidate);
    if (keys.length !== 2 || keys.some((key) => key !== "statement" && key !== "language")) return null;
    if (typeof candidate.statement !== "string" || typeof candidate.language !== "string" || !["python", "java", "cpp"].includes(candidate.language)) return null;
    if (!candidate.statement.trim() || Array.from(candidate.statement).length > maxStatementCodePoints) return null;
    return { statement: candidate.statement, language: candidate.language as AlgorithmLanguage };
  } catch {
    return null;
  }
}

function isTrace(value: unknown): value is TraceStep[] {
  if (!Array.isArray(value)) return false;
  return value.every((step) => {
    if (!step || typeof step !== "object" || Array.isArray(step)) return false;
    const candidate = step as Record<string, unknown>;
    if ((candidate.step !== "generate-prompt" && candidate.step !== "solution") || !Array.isArray(candidate.messages)) return false;
    if (candidate.response !== undefined && (typeof candidate.response !== "string" || new TextEncoder().encode(candidate.response).byteLength > maxLLMTextBytes)) return false;
    return candidate.messages.every((message) => message && typeof message === "object" && !Array.isArray(message)
      && ["system", "user", "assistant"].includes(String((message as Record<string, unknown>).role))
      && typeof (message as Record<string, unknown>).content === "string"
      && new TextEncoder().encode((message as Record<string, unknown>).content as string).byteLength <= maxComposedMessageBytes);
  });
}

function parseEnvelope(value: unknown, method: AlgorithmMethod): AlgorithmEnvelope | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const candidate = value as Record<string, unknown>;
  const allowed = new Set(["method", "status", "answer", "trace", "error"]);
  if (Object.keys(candidate).some((key) => !allowed.has(key)) || candidate.method !== method || !isTrace(candidate.trace) || typeof candidate.answer !== "string" || new TextEncoder().encode(candidate.answer).byteLength > maxLLMTextBytes) return null;
  if (candidate.status === "success" && candidate.answer.length > 0 && candidate.error === undefined) {
    return { method, status: "success", answer: candidate.answer, trace: candidate.trace };
  }
  if (candidate.status !== "error" || candidate.answer !== "" || !candidate.error || typeof candidate.error !== "object") return null;
  const code = (candidate.error as Record<string, unknown>).code;
  if (code !== "invalid_request" && code !== "unavailable" && code !== "timeout" && code !== "invalid_response") return null;
  return errorEnvelope(method, code, candidate.trace);
}

function responseStatus(code: AlgorithmErrorCode): number {
  if (code === "invalid_request") return 400;
  if (code === "timeout") return 504;
  return 502;
}

function jsonResponse(envelope: AlgorithmEnvelope, status = envelope.status === "success" ? 200 : responseStatus(envelope.error!.code)): Response {
  return Response.json(envelope, { status, headers: { "Cache-Control": "no-store" } });
}

export async function proxyAlgorithmRequest(request: Request, method: AlgorithmMethod): Promise<Response> {
  const contentType = request.headers.get("content-type") ?? "";
  if (!contentType.toLowerCase().startsWith("application/json")) return jsonResponse(errorEnvelope(method, "invalid_request"), 415);
  const declaredLength = Number(request.headers.get("content-length"));
  if (Number.isFinite(declaredLength) && declaredLength > maxRequestBytes) return jsonResponse(errorEnvelope(method, "invalid_request"), 413);

  const rawBody = await readLimitedBody(request.body, maxRequestBytes, inputDeadlineMilliseconds);
  const payload = rawBody ? parsePayload(rawBody) : null;
  if (!payload) return jsonResponse(errorEnvelope(method, "invalid_request"));

  try {
    const timeout = method === "generated-prompt" ? generatedPromptRequestDeadlineMilliseconds : algorithmRequestDeadlineMilliseconds;
    const signal = AbortSignal.timeout(timeout);
    const response = await fetch(backendURL(`/api/algorithms/${method}`), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
      cache: "no-store",
      signal,
      dispatcher: algorithmsDispatcher,
    } as RequestInit & { dispatcher: Agent });
    const contentTypeFromBackend = response.headers.get("content-type") ?? "";
    const rawResponse = await readLimitedBody(response.body, maxResponseBytes, 0, signal);
    if (signal.aborted) return jsonResponse(errorEnvelope(method, "timeout"));
    if (!contentTypeFromBackend.toLowerCase().includes("application/json") || !rawResponse) return jsonResponse(errorEnvelope(method, "invalid_response"));
    let responseValue: unknown;
    try {
      responseValue = JSON.parse(new TextDecoder().decode(rawResponse));
    } catch {
      return jsonResponse(errorEnvelope(method, "invalid_response"));
    }
    const parsed = parseEnvelope(responseValue, method);
    if (!parsed) return jsonResponse(errorEnvelope(method, "invalid_response"));
    return jsonResponse(parsed, response.ok ? undefined : responseStatus(parsed.error?.code ?? "invalid_response"));
  } catch (error) {
    const code = error instanceof DOMException && error.name === "TimeoutError" ? "timeout" : "unavailable";
    return jsonResponse(errorEnvelope(method, code));
  }
}
