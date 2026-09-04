import "server-only";

const defaultBackendURL = "http://127.0.0.1:8080";
const maxRequestBytes = 64 * 1024;
const maxPromptCharacters = 4_000;
const requestTimeoutMilliseconds = 35_000;

type TemperaturePayload = {
  prompt: string;
  temperature: number;
};

function noStoreJSON(body: object, status: number) {
  return Response.json(body, { status, headers: { "Cache-Control": "no-store" } });
}

function isJSONContentType(contentType: string): boolean {
  return contentType.split(";", 1)[0].trim().toLowerCase() === "application/json";
}

function parseTemperaturePayload(body: string): TemperaturePayload | null {
  try {
    const value: unknown = JSON.parse(body);
    if (!value || typeof value !== "object" || Array.isArray(value)) return null;

    const candidate = value as Record<string, unknown>;
    const keys = Object.keys(candidate);
    if (keys.length !== 2 || keys.some((key) => key !== "prompt" && key !== "temperature")) return null;
    if (typeof candidate.prompt !== "string" || typeof candidate.temperature !== "number") return null;

    const prompt = candidate.prompt.trim();
    const { temperature } = candidate;
    if (!prompt || Array.from(prompt).length > maxPromptCharacters || !Number.isFinite(temperature) || temperature < 0 || temperature > 2) return null;
    return { prompt, temperature };
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

export async function proxyTemperatureRequest(request: Request): Promise<Response> {
  const contentType = request.headers.get("content-type") ?? "";
  if (!isJSONContentType(contentType)) return noStoreJSON({ error: "Ожидается JSON-запрос." }, 415);

  const declaredLength = Number(request.headers.get("content-length"));
  if (Number.isFinite(declaredLength) && declaredLength > maxRequestBytes) return noStoreJSON({ error: "Запрос слишком большой." }, 413);

  const rawBody = await request.text();
  if (new TextEncoder().encode(rawBody).byteLength > maxRequestBytes) return noStoreJSON({ error: "Запрос слишком большой." }, 413);

  const payload = parseTemperaturePayload(rawBody);
  if (!payload) return noStoreJSON({ error: "Укажите prompt до 4000 символов и температуру от 0 до 2." }, 400);

  try {
    const response = await fetch(backendURL("/api/temperature"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
      cache: "no-store",
      signal: AbortSignal.timeout(requestTimeoutMilliseconds),
    });
    const responseType = response.headers.get("content-type") ?? "";
    if (!response.ok || !isJSONContentType(responseType)) return noStoreJSON({ error: "Не удалось получить ответ." }, 502);

    const result: unknown = await response.json().catch(() => null);
    if (!result || typeof result !== "object" || Array.isArray(result) || Object.keys(result).length !== 1 || typeof (result as { answer?: unknown }).answer !== "string") {
      return noStoreJSON({ error: "Сервис вернул некорректный ответ." }, 502);
    }
    const answer = (result as { answer: string }).answer.trim();
    if (!answer) return noStoreJSON({ error: "Сервис вернул пустой ответ." }, 502);
    return noStoreJSON({ answer }, 200);
  } catch {
    return noStoreJSON({ error: "Сервис временно недоступен." }, 502);
  }
}
