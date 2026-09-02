import "server-only";

const defaultBackendURL = "http://127.0.0.1:8080";
const maxRequestBytes = 64 * 1024;
const maxPromptCharacters = 4_000;
const requestTimeoutMilliseconds = 35_000;

type ChatPayload = {
  mode: "free" | "controlled";
  prompt: string;
};

function parseChatPayload(body: string): ChatPayload | null {
  try {
    const value: unknown = JSON.parse(body);
    if (!value || typeof value !== "object" || Array.isArray(value)) return null;

    const candidate = value as Record<string, unknown>;
    const keys = Object.keys(candidate);
    if (keys.some((key) => key !== "mode" && key !== "prompt")) return null;
    if (candidate.mode !== "free" && candidate.mode !== "controlled") return null;
    if (typeof candidate.prompt !== "string") return null;

    const prompt = candidate.prompt.trim();
    if (!prompt || Array.from(prompt).length > maxPromptCharacters) return null;
    return { mode: candidate.mode, prompt };
  } catch {
    return null;
  }
}

function backendURL(pathname: string): URL {
  const configuredURL = process.env.BARISTA_BACKEND_URL ?? defaultBackendURL;
  const baseURL = new URL(configuredURL.endsWith("/") ? configuredURL : `${configuredURL}/`);
  if (baseURL.protocol !== "http:" && baseURL.protocol !== "https:") {
    throw new Error("Unsupported backend protocol");
  }
  return new URL(pathname.replace(/^\//, ""), baseURL);
}

export async function proxyChatRequest(request: Request): Promise<Response> {
  const contentType = request.headers.get("content-type") ?? "";
  if (!contentType.toLowerCase().startsWith("application/json")) {
    return Response.json({ error: "Ожидается JSON-запрос." }, { status: 415 });
  }

  const declaredLength = Number(request.headers.get("content-length"));
  if (Number.isFinite(declaredLength) && declaredLength > maxRequestBytes) {
    return Response.json({ error: "Запрос слишком большой." }, { status: 413 });
  }

  const rawBody = await request.text();
  if (new TextEncoder().encode(rawBody).byteLength > maxRequestBytes) {
    return Response.json({ error: "Запрос слишком большой." }, { status: 413 });
  }

  const payload = parseChatPayload(rawBody);
  if (!payload) {
    return Response.json(
      { error: "Укажите mode (free или controlled) и prompt до 4000 символов." },
      { status: 400 },
    );
  }

  try {
    const response = await fetch(backendURL("/api/chat"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
      cache: "no-store",
      signal: AbortSignal.timeout(requestTimeoutMilliseconds),
    });

    const responseType = response.headers.get("content-type") ?? "";
    if (!responseType.includes("application/json")) {
      return Response.json({ error: "Backend вернул некорректный ответ." }, { status: 502 });
    }

    if (response.status >= 500) {
      return Response.json({ error: "Не удалось получить ответ бариста." }, { status: 502 });
    }

    return new Response(await response.text(), {
      status: response.status,
      headers: {
        "Content-Type": "application/json; charset=utf-8",
        "Cache-Control": "no-store",
      },
    });
  } catch {
    return Response.json({ error: "Backend временно недоступен." }, { status: 502 });
  }
}

export async function checkBackendHealth(): Promise<Response> {
  try {
    const response = await fetch(backendURL("/healthz"), {
      cache: "no-store",
      signal: AbortSignal.timeout(3_000),
    });
    if (!response.ok) throw new Error("Backend is unhealthy");
    return Response.json({ status: "ok" });
  } catch {
    return Response.json({ status: "unavailable" }, { status: 503 });
  }
}
