import "server-only";
import { randomUUID } from "node:crypto";

const defaultBackendURL = "http://127.0.0.1:8080";
const maxRequestBytes = 64 * 1024;
const maxPromptCharacters = 4_000;
const requestTimeoutMilliseconds = 190_000;
const deepSeekModels = new Set([
  "deepseek-v4-flash",
  "deepseek-v4-pro",
  "deepseek-v4-flash-vision-exp",
]);
const kimiModels = new Set(["kimi-k3", "kimi-k2.7-code", "kimi-k2.6"]);

type ModelTemperaturePayload = {
  prompt: string;
  temperature: number;
  provider: "deepseek" | "kimi";
  model: string;
};

function noStoreJSON(body: object, status: number) {
  return Response.json(body, { status, headers: { "Cache-Control": "no-store" } });
}

function isJSONContentType(contentType: string): boolean {
  return contentType.split(";", 1)[0].trim().toLowerCase() === "application/json";
}

function parseModelTemperaturePayload(body: string): ModelTemperaturePayload | null {
  try {
    const value: unknown = JSON.parse(body);
    if (!value || typeof value !== "object" || Array.isArray(value)) return null;

    const candidate = value as Record<string, unknown>;
    const keys = Object.keys(candidate);
    if (keys.length !== 4 || keys.some((key) => key !== "prompt" && key !== "temperature" && key !== "provider" && key !== "model")) return null;
    if (typeof candidate.prompt !== "string" || typeof candidate.temperature !== "number" || typeof candidate.provider !== "string" || typeof candidate.model !== "string") return null;

    const prompt = candidate.prompt.trim();
    const { temperature, provider, model } = candidate;
    if (provider !== "deepseek" && provider !== "kimi") return null;
    const maximumTemperature = provider === "kimi" ? 1 : 2;
    if (provider === "kimi" && temperature !== 1) return null;
    const supportedModel = provider === "kimi" ? kimiModels.has(model) : deepSeekModels.has(model);
    if (!prompt || Array.from(prompt).length > maxPromptCharacters || !Number.isFinite(temperature) || temperature < 0 || temperature > maximumTemperature || !supportedModel) return null;
    return { prompt, temperature, provider, model };
  } catch {
    return null;
  }
}

function backendURL(pathname: string): URL {
  const configuredURL = process.env.BARISTA_BACKEND_URL ?? defaultBackendURL;
  const baseURL = new URL(configuredURL.endsWith("/") ? configuredURL : `${configuredURL}/`);
  if (baseURL.protocol !== "http:" && baseURL.protocol !== "https:") throw new Error("Unsupported backend protocol");
  return new URL(pathname.replace(/^\//, ""), baseURL);
}

export async function proxyModelTemperatureRequest(request: Request): Promise<Response> {
  const requestID = randomUUID();
  const startedAt = Date.now();
  const target: { model?: string; provider?: string } = {};
  const respond = (body: object, status: number, reason = "invalid_request", backendStatus?: number) => {
    console.info(JSON.stringify({ event: "bff.model.finish", request_id: requestID, ...target,
      duration_ms: Date.now() - startedAt, http_status: status, backend_status: backendStatus, reason }));
    const response = noStoreJSON(body, status);
    response.headers.set("X-Request-ID", requestID);
    return response;
  };
  const contentType = request.headers.get("content-type") ?? "";
  if (!isJSONContentType(contentType)) return respond({ error: "Ожидается JSON-запрос." }, 415);

  const declaredLength = Number(request.headers.get("content-length"));
  if (Number.isFinite(declaredLength) && declaredLength > maxRequestBytes) return respond({ error: "Запрос слишком большой." }, 413);

  const rawBody = await request.text();
  if (new TextEncoder().encode(rawBody).byteLength > maxRequestBytes) return respond({ error: "Запрос слишком большой." }, 413);

  const payload = parseModelTemperaturePayload(rawBody);
  if (!payload) return respond({ error: "Укажите prompt до 4000 символов, доступные провайдер и модель, а также допустимую температуру." }, 400);

  target.model = payload.model;
  target.provider = payload.provider;
  console.info(JSON.stringify({ event: "bff.model.start", request_id: requestID, ...target }));
  try {
    const response = await fetch(backendURL("/api/model-temperature"), {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-Request-ID": requestID },
      body: JSON.stringify(payload),
      cache: "no-store",
      signal: AbortSignal.timeout(requestTimeoutMilliseconds),
    });
    const responseType = response.headers.get("content-type") ?? "";
    if (!response.ok || !isJSONContentType(responseType)) return respond({ error: "Не удалось получить ответ." }, 502, response.ok ? "backend_content_type" : "backend_http_error", response.status);

    const result: unknown = await response.json().catch(() => null);
    if (!validUpstreamResult(result)) {
      return respond({ error: "Сервис вернул некорректный ответ." }, 502, "backend_invalid_response", response.status);
    }
    const answer = result.answer.trim();
    if (!answer) return respond({ error: "Сервис вернул пустой ответ." }, 502, "backend_empty_answer", response.status);
    return respond({ answer, metrics: result.metrics }, 200, "success", response.status);
  } catch (error) {
    const timeout = error instanceof Error && (error.name === "TimeoutError" || error.name === "AbortError");
    return respond({ error: "Сервис временно недоступен." }, 502, timeout ? "backend_timeout" : "backend_network_or_config");
  }
}

type UpstreamResult = {
  answer: string;
  metrics: { duration_ms: number; input_tokens: number; output_tokens: number; cost_usd: number };
};

function validUpstreamResult(result: unknown): result is UpstreamResult {
  if (!result || typeof result !== "object" || Array.isArray(result)) return false;
  const value = result as Record<string, unknown>;
  if (Object.keys(value).length !== 2 || typeof value.answer !== "string" || !value.metrics || typeof value.metrics !== "object" || Array.isArray(value.metrics)) return false;
  const metrics = value.metrics as Record<string, unknown>;
  return Object.keys(metrics).length === 4
    && Number.isInteger(metrics.duration_ms) && Number(metrics.duration_ms) >= 0
    && Number.isInteger(metrics.input_tokens) && Number(metrics.input_tokens) >= 0
    && Number.isInteger(metrics.output_tokens) && Number(metrics.output_tokens) >= 0
    && typeof metrics.cost_usd === "number" && Number.isFinite(metrics.cost_usd) && metrics.cost_usd >= 0;
}
