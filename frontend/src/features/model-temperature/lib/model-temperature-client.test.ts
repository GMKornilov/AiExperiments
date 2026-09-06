import { afterEach, describe, expect, it, vi } from "vitest";
import { requestModelTemperature } from "./model-temperature-client";

afterEach(() => vi.unstubAllGlobals());

describe("requestModelTemperature", () => {
  it("sends the selected model and temperature", async () => {
    const response = { answer: "Ответ", metrics: { duration_ms: 1200, input_tokens: 12, output_tokens: 34, cost_usd: 0.0001 } };
    const fetchMock = vi.fn().mockResolvedValue(Response.json(response));
    vi.stubGlobal("fetch", fetchMock);

    await expect(requestModelTemperature("prompt", 1.2, "deepseek", "deepseek-v4-pro")).resolves.toEqual(response);
    expect(fetchMock).toHaveBeenCalledWith("/api/model-temperature", expect.objectContaining({
      body: JSON.stringify({ prompt: "prompt", temperature: 1.2, provider: "deepseek", model: "deepseek-v4-pro" }),
    }));
  });
});
