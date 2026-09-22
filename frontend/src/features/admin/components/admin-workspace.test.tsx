import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AdminWorkspace } from "./admin-workspace";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

describe("AdminWorkspace", () => {
  it("загружает MCP журнал без chat ID и показывает MCP/BrewMark событие", async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({
      scope: "mcp", retention: "backend_runtime", logs: [{ timestamp: "2026-09-22T12:00:00Z", source: "backend", event: "mcp_tools_list", operation: "mcp_tools_list", result: "success", correlation_id: "request-42", duration_ms: 20 }],
    }));
    vi.stubGlobal("fetch", fetchMock);
    render(<AdminWorkspace />);

    fireEvent.click(screen.getByRole("radio", { name: "Системный MCP" }));

    await screen.findByText("MCP → BrewMark");
    expect(screen.getByText("mcp_tools_list")).toBeInTheDocument();
    expect(screen.getByText("Источник: backend · Корреляция: request-42")).toBeInTheDocument();
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith("/api/admin/logs?scope=mcp", expect.anything()));
    expect(screen.queryByLabelText("ID чата")).toBeNull();
  });
});
