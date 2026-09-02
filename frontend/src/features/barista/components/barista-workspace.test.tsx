import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { BaristaWorkspace } from "./barista-workspace";

const controlledAnswer = {
  summary: "**Помол** слишком крупный.",
  focus_points: ["Сделайте `помол` мельче.", "Продлите экстракцию.", "Используйте свежую воду."],
  recipe: {
    coffee_g: 18,
    water_g: 36,
    temperature_c: 93,
    brew_time_sec: 28,
  },
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("BaristaWorkspace", () => {
  it("switches response mode from the keyboard", () => {
    render(<BaristaWorkspace />);
    const freeMode = screen.getByLabelText("Свободный");
    const controlledMode = screen.getByLabelText("Структурный");

    freeMode.focus();
    fireEvent.keyDown(freeMode, { key: "ArrowRight" });

    expect(controlledMode).toBeChecked();
    expect(controlledMode).toHaveFocus();
  });

  it("renders controlled summary and focus points as Markdown, with recipe and raw response", async () => {
    const raw = JSON.stringify(controlledAnswer);
    const fetchMock = vi.fn().mockResolvedValue(
      Response.json({ mode: "controlled", raw, data: controlledAnswer }),
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<BaristaWorkspace />);
    fireEvent.click(screen.getByLabelText("Структурный"));
    fireEvent.change(screen.getByLabelText("Ваш вопрос"), {
      target: { value: "Почему эспрессо кислый?" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Спросить бариста/ }));

    expect(await screen.findByText("Помол", { selector: "strong" })).toBeVisible();
    expect(screen.getByText("помол", { selector: "code" })).toBeVisible();
    expect(screen.getByText("Кратко")).toBeVisible();
    expect(screen.getByText("На что обратить внимание")).toBeVisible();
    expect(screen.getByText("18 г")).toBeVisible();
    expect(screen.getByText("93 °C")).toBeVisible();
    expect(screen.getAllByRole("listitem")).toHaveLength(3);

    const rawDetails = screen.getByText(/Сырой ответ/).closest("details");
    expect(rawDetails).not.toHaveAttribute("open");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/chat",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          mode: "controlled",
          prompt: "Почему эспрессо кислый?",
        }),
      }),
    );
  });

  it("renders a free-form answer as Markdown and preserves raw source in a closed spoiler", async () => {
    const raw = "## Настройка\n\n**Сделайте помол мельче.**";
    const fetchMock = vi.fn().mockResolvedValue(
      Response.json({ mode: "free", raw, data: null }),
    );
    vi.stubGlobal("fetch", fetchMock);

    render(<BaristaWorkspace />);
    fireEvent.change(screen.getByLabelText("Ваш вопрос"), {
      target: { value: "Как улучшить чашку?" },
    });
    fireEvent.submit(screen.getByRole("button", { name: /Спросить бариста/ }).closest("form")!);

    expect(await screen.findByRole("heading", { name: "Настройка", level: 2 })).toBeVisible();
    expect(screen.getByText("Сделайте помол мельче.", { selector: "strong" })).toBeVisible();
    expect(screen.getByText("Свободный режим")).toBeVisible();

    const rawDetails = screen.getByText(/Сырой ответ/).closest("details");
    expect(rawDetails).not.toHaveAttribute("open");
    expect(rawDetails?.querySelector("pre")?.textContent).toBe(raw);
  });

  it("shows a safe error state", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("Backend недоступен")));

    render(<BaristaWorkspace />);
    fireEvent.change(screen.getByLabelText("Ваш вопрос"), {
      target: { value: "Нужен рецепт" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Спросить бариста/ }));

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Backend недоступен"));
  });
});
