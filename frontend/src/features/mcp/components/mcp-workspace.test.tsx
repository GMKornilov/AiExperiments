import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MCPWorkspace } from "./mcp-workspace";

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("MCPWorkspace", () => {
  it("показывает idle-state и доступную кнопку", () => {
    render(<MCPWorkspace />);
    expect(screen.getByText("Подключение ещё не выполнялось.")).toBeVisible();
    expect(screen.getByRole("button", { name: "Подключиться и получить tools" })).toBeEnabled();
  });

  it("объявляет loading и блокирует кнопку", async () => {
    let resolve!: (response: Response) => void;
    vi.stubGlobal("fetch", vi.fn().mockReturnValue(new Promise<Response>((done) => { resolve = done; })));
    render(<MCPWorkspace />);
    fireEvent.click(screen.getByRole("button", { name: "Подключиться и получить tools" }));
    expect(screen.getByRole("status")).toHaveTextContent("Устанавливаем MCP-соединение");
    expect(screen.getByRole("button", { name: "Подключаемся…" })).toBeDisabled();
    resolve(Response.json({ tools: [] }));
  });

  it("показывает name и description ответа без локальной подмены", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ tools: [{ name: "brewmark_list_grinders", description: "Exactly from MCP." }] })));
    render(<MCPWorkspace />);
    fireEvent.click(screen.getByRole("button", { name: "Подключиться и получить tools" }));
    expect(await screen.findByText("brewmark_list_grinders")).toBeVisible();
    expect(screen.getByText("Exactly from MCP.")).toBeVisible();
    expect(screen.getByRole("list", { name: "Доступные MCP tools" })).toBeVisible();
  });

  it("показывает штатное пустое состояние и разрешает повтор", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ tools: [] })));
    render(<MCPWorkspace />);
    fireEvent.click(screen.getByRole("button", { name: "Подключиться и получить tools" }));
    expect(await screen.findByText("MCP-сервер не вернул tools.")).toBeVisible();
    expect(screen.getByRole("button", { name: "Подключиться и получить tools" })).toBeEnabled();
  });

  it("показывает безопасную ошибку и повторяет один same-origin запрос", async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ code: "MCP_UNAVAILABLE", message: "private host" }, { status: 503 }));
    vi.stubGlobal("fetch", fetchMock);
    render(<MCPWorkspace />);
    fireEvent.click(screen.getByRole("button", { name: "Подключиться и получить tools" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Не удалось подключиться к MCP");
    fireEvent.click(screen.getByRole("button", { name: "Подключиться и получить tools" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(fetchMock).toHaveBeenCalledWith("/api/mcp/tools", { method: "POST" });
  });
});
