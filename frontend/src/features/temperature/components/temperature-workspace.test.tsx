import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TemperatureWorkspace } from "./temperature-workspace";

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("TemperatureWorkspace", () => {
  it("starts at 0.7 and supports quick and custom temperatures", () => {
    render(<TemperatureWorkspace />);
    const input = screen.getByLabelText("Своё значение");
    expect(input).toHaveValue(0.7);
    expect(screen.getByRole("button", { name: "0.7" })).toHaveAttribute("aria-pressed", "true");

    fireEvent.click(screen.getByRole("button", { name: "1.2" }));
    expect(input).toHaveValue(1.2);
    expect(screen.getByRole("button", { name: "1.2" })).toHaveAttribute("aria-pressed", "true");

    fireEvent.change(input, { target: { value: "0.3" } });
    expect(input).toHaveValue(0.3);
    for (const value of ["0", "0.7", "1.2"]) expect(screen.getByRole("button", { name: value })).toHaveAttribute("aria-pressed", "false");
  });

  it("submits one immutable snapshot and renders Markdown safely", async () => {
    let resolveFetch: ((response: Response) => void) | undefined;
    const fetchMock = vi.fn().mockImplementation(() => new Promise<Response>((resolve) => { resolveFetch = resolve; }));
    vi.stubGlobal("fetch", fetchMock);
    render(<TemperatureWorkspace />);
    const prompt = screen.getByLabelText("Промпт");
    fireEvent.change(prompt, { target: { value: "  первый prompt  " } });
    fireEvent.submit(screen.getByRole("button", { name: "Отправить" }).closest("form")!);
    fireEvent.change(prompt, { target: { value: "другой prompt" } });
    expect(screen.getByRole("button", { name: /Генерируем/ })).toBeDisabled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith("/api/temperature", expect.objectContaining({ body: JSON.stringify({ prompt: "первый prompt", temperature: 0.7 }) }));

    resolveFetch?.(Response.json({ answer: "## Готово\n\n<img src=x>" }));
    expect(await screen.findByRole("heading", { name: "Готово", level: 2 })).toBeVisible();
    expect(document.querySelector("img")).toBeNull();
  });

  it("shows validation and retryable errors", async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ error: "Сервис недоступен" }, { status: 502 }));
    vi.stubGlobal("fetch", fetchMock);
    render(<TemperatureWorkspace />);
    fireEvent.submit(screen.getByRole("button", { name: "Отправить" }).closest("form")!);
    expect(screen.getByRole("alert")).toHaveTextContent("Введите промпт");
    fireEvent.change(screen.getByLabelText("Промпт"), { target: { value: "Нужен ответ" } });
    fireEvent.click(screen.getByRole("button", { name: "Отправить" }));
    expect(await screen.findByRole("button", { name: "Повторить" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Повторить" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });
});
