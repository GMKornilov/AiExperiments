import { afterEach, describe, expect, it, vi } from "vitest";
import { requestTemperature } from "./temperature-client";

afterEach(() => vi.unstubAllGlobals());

describe("requestTemperature", () => {
  it("sends the same-origin temperature request", async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ answer: "Ответ" }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(requestTemperature("Промпт", 1.2)).resolves.toEqual({ answer: "Ответ" });
    expect(fetchMock).toHaveBeenCalledWith("/api/temperature", expect.objectContaining({ method: "POST", body: JSON.stringify({ prompt: "Промпт", temperature: 1.2 }) }));
  });
});
