import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AlgorithmsProvider } from "../algorithms-store";
import { AlgorithmsWorkspace } from "./algorithms-workspace";

type Deferred = { resolve: (response: Response) => void; reject: (reason?: unknown) => void };

function deferredFetch() {
  const waiting = new Map<string, Deferred>();
  const fetchMock = vi.fn((url: string, init?: RequestInit) => {
    void init;
    return new Promise<Response>((resolve, reject) => {
      waiting.set(url.split("/").at(-1)!, { resolve, reject });
    });
  });
  return { fetchMock, waiting };
}

function envelope(method: string, answer: string, trace: unknown[] = []) {
  return { method, status: "success", answer, trace };
}

function renderWorkspace() {
  return render(<AlgorithmsProvider><AlgorithmsWorkspace /></AlgorithmsProvider>);
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("AlgorithmsWorkspace", () => {
  it("keeps every tab connected to the active panel while switching with keyboard", () => {
    renderWorkspace();

    const assertTabPanelRelationships = () => {
      const tabs = screen.getAllByRole("tab");
      const panel = screen.getByRole("tabpanel");
      expect(tabs).toHaveLength(4);
      for (const tab of tabs) {
        const controlledID = tab.getAttribute("aria-controls");
        expect(controlledID).toBeTruthy();
        expect(document.getElementById(controlledID!)).toBe(panel);
      }
      const selectedTab = tabs.find((tab) => tab.getAttribute("aria-selected") === "true");
      expect(selectedTab).toBeTruthy();
      expect(panel.getAttribute("aria-labelledby")).toBe(selectedTab!.id);
    };

    assertTabPanelRelationships();
    const direct = screen.getByRole("tab", { name: /Прямой ответ/ });
    direct.focus();
    fireEvent.keyDown(direct, { key: "ArrowRight" });
    assertTabPanelRelationships();
    fireEvent.keyDown(screen.getByRole("tab", { name: /Решай пошагово/ }), { key: "End" });
    assertTabPanelRelationships();
    fireEvent.keyDown(screen.getByRole("tab", { name: /Группа экспертов/ }), { key: "Home" });
    assertTabPanelRelationships();
  });

  it("starts four independent requests from one immutable raw snapshot and does not refetch on tab changes", async () => {
    const { fetchMock, waiting } = deferredFetch();
    vi.stubGlobal("fetch", fetchMock);
    renderWorkspace();
    const statement = "  Условие\nПример: 1 -> 2  ";
    fireEvent.change(screen.getByLabelText("Условие задачи"), { target: { value: statement } });
    fireEvent.change(screen.getByLabelText("Язык"), { target: { value: "java" } });
    fireEvent.click(screen.getByRole("button", { name: /Получить решения/ }));

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4));
    expect(screen.getByRole("button", { name: /Получаем решения/ })).toBeDisabled();
    for (const method of ["direct", "step-by-step", "generated-prompt", "experts"]) {
      expect(fetchMock).toHaveBeenCalledWith(`/api/algorithms/${method}`, expect.objectContaining({
        body: JSON.stringify({ statement, language: "java" }),
      }));
    }
    fireEvent.click(screen.getByRole("tab", { name: /Группа экспертов/ }));
    expect(fetchMock).toHaveBeenCalledTimes(4);

    fireEvent.change(screen.getByLabelText("Условие задачи"), { target: { value: "edited after run" } });
    waiting.get("experts")!.resolve(Response.json(envelope("experts", "Ответ экспертов")));
    await screen.findByText("Ответ экспертов");
    expect(fetchMock.mock.calls.every((call) => call[1]?.body === JSON.stringify({ statement, language: "java" }))).toBe(true);
  });

  it("shows independent completion and error states, including partial generated-prompt trace", async () => {
    const { fetchMock, waiting } = deferredFetch();
    vi.stubGlobal("fetch", fetchMock);
    renderWorkspace();
    fireEvent.change(screen.getByLabelText("Условие задачи"), { target: { value: "condition" } });
    fireEvent.click(screen.getByRole("button", { name: /Получить решения/ }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4));

    waiting.get("direct")!.resolve(Response.json(envelope("direct", "# Готовый ответ")));
    waiting.get("generated-prompt")!.resolve(Response.json({
      method: "generated-prompt", status: "error", answer: "", error: { message: "Время ожидания истекло." },
      trace: [{ step: "generate-prompt", messages: [{ role: "system", content: "first  \n prompt" }], response: "## Raw prompt" }, { step: "solution", messages: [{ role: "user", content: "second" }] }],
    }, { status: 504 }));
    waiting.get("step-by-step")!.resolve(Response.json(envelope("step-by-step", "steps")));
    waiting.get("experts")!.resolve(Response.json(envelope("experts", "experts")));

    await screen.findByRole("tab", { name: /Прямой ответ.*Готово/ });
    fireEvent.click(screen.getByRole("tab", { name: /Сначала промпт, затем решение.*Ошибка/ }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Время ожидания истекло.");
    expect(screen.getByText("Сгенерированный prompt")).toBeVisible();
    expect(screen.getByRole("heading", { name: "Raw prompt", level: 2 })).toBeVisible();
    fireEvent.click(screen.getByText(/Сырые промпты \(2\)/));
    expect(Array.from(document.querySelectorAll("pre")).some((element) => element.textContent === "first  \n prompt")).toBe(true);
  });

  it("blocks invalid input locally and starts a new generation only after the prior four settle", async () => {
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      void init;
      return Response.json(envelope(url.split("/").at(-1)!, `answer ${url}`));
    });
    vi.stubGlobal("fetch", fetchMock);
    renderWorkspace();
    fireEvent.click(screen.getByRole("button", { name: /Получить решения/ }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Введите условие задачи.");
    expect(fetchMock).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("Условие задачи"), { target: { value: "first" } });
    fireEvent.click(screen.getByRole("button", { name: /Получить решения/ }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4));
    await waitFor(() => expect(screen.getByRole("button", { name: /Получить решения/ })).toBeEnabled());
    fireEvent.change(screen.getByLabelText("Условие задачи"), { target: { value: "second" } });
    fireEvent.click(screen.getByRole("button", { name: /Получить решения/ }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(8));
    expect(fetchMock.mock.calls.slice(4).every((call) => call[1]?.body === JSON.stringify({ statement: "second", language: "python" }))).toBe(true);
  });
});
