export const providerModels = {
  deepseek: [
    { id: "deepseek-v4-flash", label: "DeepSeek V4 Flash" },
    { id: "deepseek-v4-pro", label: "DeepSeek V4 Pro" },
    { id: "deepseek-v4-flash-vision-exp", label: "DeepSeek V4 Flash Vision (experimental)" },
  ],
  kimi: [
    { id: "kimi-k3", label: "Kimi K3" },
    { id: "kimi-k2.7-code", label: "Kimi K2.7 Code" },
    { id: "kimi-k2.6", label: "Kimi K2.6" },
  ],
} as const;

export type Provider = keyof typeof providerModels;
export type Model = (typeof providerModels)[Provider][number]["id"];

export type RequestMetrics = {
  duration_ms: number;
  input_tokens: number;
  output_tokens: number;
  cost_usd: number;
};

export type ModelTemperatureResponse = {
  answer: string;
  metrics: RequestMetrics;
};

type ErrorResponse = { error: string };

export async function requestModelTemperature(
  prompt: string,
  temperature: number,
  provider: Provider,
  model: Model,
): Promise<ModelTemperatureResponse> {
  const response = await fetch("/api/model-temperature", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ prompt, temperature, provider, model }),
    cache: "no-store",
  });
  const payload = (await response.json().catch(() => null)) as ModelTemperatureResponse | ErrorResponse | null;
  if (!response.ok || !payload || "error" in payload || typeof payload.answer !== "string" || !validMetrics(payload.metrics)) {
    throw new Error(payload && "error" in payload ? payload.error : "Сервер вернул ответ, который не удалось прочитать.");
  }
  return payload;
}

function validMetrics(metrics: unknown): metrics is RequestMetrics {
  if (!metrics || typeof metrics !== "object" || Array.isArray(metrics)) return false;
  const value = metrics as Record<string, unknown>;
  return Number.isInteger(value.duration_ms) && Number(value.duration_ms) >= 0
    && Number.isInteger(value.input_tokens) && Number(value.input_tokens) >= 0
    && Number.isInteger(value.output_tokens) && Number(value.output_tokens) >= 0
    && typeof value.cost_usd === "number" && Number.isFinite(value.cost_usd) && value.cost_usd >= 0;
}
