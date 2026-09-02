import type { ChatMode, ChatResponse, ErrorResponse } from "../model/types";

export async function requestBarista(
  mode: ChatMode,
  prompt: string,
  signal?: AbortSignal,
): Promise<ChatResponse> {
  const response = await fetch("/api/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ mode, prompt }),
    cache: "no-store",
    signal,
  });

  const payload = (await response.json().catch(() => null)) as
    | ChatResponse
    | ErrorResponse
    | null;

  if (!response.ok || !payload || "error" in payload) {
    throw new Error(
      payload && "error" in payload
        ? payload.error
        : "Сервер вернул ответ, который не удалось прочитать.",
    );
  }

  return payload;
}
