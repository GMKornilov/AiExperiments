type TemperatureResponse = { answer: string };
type ErrorResponse = { error: string };

export async function requestTemperature(prompt: string, temperature: number): Promise<TemperatureResponse> {
  const response = await fetch("/api/temperature", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ prompt, temperature }),
    cache: "no-store",
  });
  const payload = (await response.json().catch(() => null)) as TemperatureResponse | ErrorResponse | null;
  if (!response.ok || !payload || "error" in payload || typeof payload.answer !== "string") {
    throw new Error(payload && "error" in payload ? payload.error : "Сервер вернул ответ, который не удалось прочитать.");
  }
  return payload;
}
