import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ModelTemperatureWorkspace } from "./model-temperature-workspace";

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("ModelTemperatureWorkspace", () => {
  it("retries different models independently while preventing duplicate and full runs", async () => {
    const pending: Array<{ resolve: (response: Response) => void; reject: (error: Error) => void }> = [];
    const fetchMock = vi.fn().mockImplementation(() => new Promise<Response>((resolve, reject) => pending.push({ resolve, reject })));
    vi.stubGlobal("fetch", fetchMock);
    render(<ModelTemperatureWorkspace />);
    fireEvent.change(screen.getByLabelText("Промпт"), { target: { value: "исходный промпт" } });
    const submit = screen.getByRole("button", { name: "Сравнить 3 модели" });
    fireEvent.click(submit);
    await act(async () => {
      pending[0].reject(new Error("Flash failed"));
      pending[1].reject(new Error("Pro failed"));
    });
    fireEvent.change(screen.getByLabelText("Промпт"), { target: { value: "новый промпт" } });
    const flash = within(screen.getByRole("region", { name: "DeepSeek V4 Flash" }));
    const pro = within(screen.getByRole("region", { name: "DeepSeek V4 Pro" }));
    const kimi = within(screen.getByRole("region", { name: "Kimi K3" }));
    const flashRetry = flash.getByRole("button", { name: "Повторить" });
    const proRetry = pro.getByRole("button", { name: "Повторить" });
    act(() => {
      fireEvent.click(flashRetry);
      fireEvent.click(flashRetry);
      fireEvent.click(proRetry);
      fireEvent.click(proRetry);
    });
    expect(fetchMock).toHaveBeenCalledTimes(5);
    expect(fetchMock.mock.calls.slice(3).map((call) => JSON.parse(call[1].body))).toEqual([
      { prompt: "исходный промпт", temperature: 1, provider: "deepseek", model: "deepseek-v4-flash" },
      { prompt: "исходный промпт", temperature: 1, provider: "deepseek", model: "deepseek-v4-pro" },
    ]);
    expect(flash.getByRole("status")).toBeVisible();
    expect(pro.getByRole("status")).toBeVisible();
    expect(kimi.getByRole("status")).toBeVisible();
    expect(submit).toBeDisabled();
    fireEvent.submit(submit.closest("form")!);
    expect(fetchMock).toHaveBeenCalledTimes(5);
    const metrics = { duration_ms: 1000, input_tokens: 10, output_tokens: 20, cost_usd: 0.01 };
    await act(async () => pending[2].resolve(Response.json({ answer: "Kimi готов", metrics })));
    expect(submit).toBeDisabled();
    await act(async () => pending[4].resolve(Response.json({ answer: "Pro готов", metrics })));
    expect(pro.getByText("Pro готов")).toBeVisible();
    expect(submit).toBeDisabled();
    await act(async () => pending[3].resolve(Response.json({ answer: "Flash готов", metrics })));
    expect(flash.getByText("Flash готов")).toBeVisible();
    expect(kimi.getByText("Kimi готов")).toBeVisible();
    expect(submit).toBeEnabled();
  });

  it("runs three requests concurrently, shows partial results and retries only the failed snapshot", async () => {
    const pending: Array<{ resolve: (response: Response) => void; reject: (error: Error) => void }> = [];
    const fetchMock = vi.fn().mockImplementation(() => new Promise<Response>((resolve, reject) => pending.push({ resolve, reject })));
    vi.stubGlobal("fetch", fetchMock);
    render(<ModelTemperatureWorkspace />);
    fireEvent.change(screen.getByLabelText("Промпт"), { target: { value: "  общий промпт  " } });
    const compareButton = screen.getByRole("button", { name: "Сравнить 3 модели" });
    fireEvent.click(compareButton);
    fireEvent.click(compareButton);
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(fetchMock.mock.calls.map((call) => JSON.parse(call[1].body))).toEqual([
      { prompt: "общий промпт", temperature: 1, provider: "deepseek", model: "deepseek-v4-flash" },
      { prompt: "общий промпт", temperature: 1, provider: "deepseek", model: "deepseek-v4-pro" },
      { prompt: "общий промпт", temperature: 1, provider: "kimi", model: "kimi-k3" },
    ]);
    expect(compareButton).toBeDisabled();
    expect(screen.getByRole("button", { name: "Сравниваем…" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Промпт"), { target: { value: "изменённый промпт" } });
    const metrics = { duration_ms: 1250, input_tokens: 12, output_tokens: 34, cost_usd: 0.000123 };
    await act(async () => {
      pending[0].resolve(Response.json({ answer: "Ответ Flash\n\n<img src=x>", metrics }));
      pending[1].reject(new Error("Ошибка Pro"));
    });
    const flash = within(screen.getByRole("region", { name: "DeepSeek V4 Flash" }));
    const pro = within(screen.getByRole("region", { name: "DeepSeek V4 Pro" }));
    const kimi = within(screen.getByRole("region", { name: "Kimi K3" }));
    expect(flash.getByText("Ответ Flash")).toBeVisible();
    expect(document.querySelector("img")).toBeNull();
    expect(flash.getByText("1.25 с")).toBeVisible();
    expect(flash.getByText("12")).toBeVisible();
    expect(flash.getByText("34")).toBeVisible();
    expect(flash.getByText("$0.000123")).toBeVisible();
    expect(pro.getByRole("alert")).toBeVisible();
    expect(pro.getByRole("button", { name: "Повторить" })).toBeEnabled();
    expect(kimi.getByRole("status")).toBeVisible();
    await act(async () => pending[2].resolve(Response.json({ answer: "Ответ Kimi", metrics: { ...metrics, cost_usd: 0.01 } })));
    expect(kimi.getByText("Ответ Kimi")).toBeVisible();
    expect(kimi.getByText("$0.010000")).toBeVisible();
    await waitFor(() => expect(compareButton).toBeEnabled());
    fireEvent.click(pro.getByRole("button", { name: "Повторить" }));
    expect(fetchMock).toHaveBeenCalledTimes(4);
    expect(JSON.parse(fetchMock.mock.calls[3][1].body)).toEqual({
      prompt: "общий промпт", temperature: 1, provider: "deepseek", model: "deepseek-v4-pro",
    });
    expect(flash.getByText("Ответ Flash")).toBeVisible();
    expect(kimi.getByText("Ответ Kimi")).toBeVisible();
    await act(async () => pending[3].resolve(Response.json({ answer: "Ответ Pro", metrics })));
    expect(pro.getByText("Ответ Pro")).toBeVisible();
    fireEvent.click(compareButton);
    expect(fetchMock).toHaveBeenCalledTimes(7);
    expect(JSON.parse(fetchMock.mock.calls[4][1].body).prompt).toBe("изменённый промпт");
    expect(screen.queryByText("Ответ Flash")).not.toBeInTheDocument();
  });

  it("does not start a comparison with an empty or oversized prompt", () => {
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    render(<ModelTemperatureWorkspace />);
    fireEvent.click(screen.getByRole("button", { name: "Сравнить 3 модели" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Введите промпт");
    fireEvent.change(screen.getByLabelText("Промпт"), { target: { value: "x".repeat(4001) } });
    fireEvent.click(screen.getByRole("button", { name: "Сравнить 3 модели" }));
    expect(screen.getByRole("alert")).toHaveTextContent("4 000");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("shows only a prompt and comparison submit, without single-model controls", () => {
    render(<ModelTemperatureWorkspace />);
    expect(screen.getByLabelText("Промпт")).toBeVisible();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(screen.queryByRole("spinbutton")).not.toBeInTheDocument();
    expect(screen.queryByText("Провайдер")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Отправить" })).not.toBeInTheDocument();
    const button = screen.getByRole("button", { name: "Сравнить 3 модели" });
    expect(button).toHaveAttribute("type", "submit");
    expect(screen.getAllByRole("button")).toHaveLength(1);
  });
});
