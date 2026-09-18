import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { formatPayload, LogCard } from "./log-card";
afterEach(cleanup);
describe("LLM log cards", () => {
  it("formats nested JSON and preserves plain text responses", () => {
    expect(formatPayload('{"messages":[{"role":"user","content":"test"}]}')).toContain('\n  "messages": [\n    {');
    expect(formatPayload("<html>failure</html>")).toBe("<html>failure</html>");
  });
  it("renders semantic JSON control characters and preserves literal escapes in ordinary fields", () => {
    expect(formatPayload('{"prompt":"first\\nsecond\\r\\nthird\\tfourth"}')).toContain('first\nsecond\r\nthird\tfourth');
    expect(formatPayload('{"path":"literal \\\\n"}')).toContain('literal \\\\n');
  });
  it("renders double-escaped line breaks in LLM message content", () => {
    const payload = JSON.stringify({
      model: "deepseek-v4-flash",
      messages: [{ role: "system", content: "Первая строка\\nВторая строка\\n\\nТретья строка" }],
    });

    const rendered = formatPayload(payload);

    expect(rendered).toContain("Первая строка\nВторая строка\n\nТретья строка");
    expect(rendered).not.toContain("Первая строка\\nВторая строка");
  });
  it("shows purpose, HTTP status and the raw provider response safely", () => {
    render(<LogCard log={{ source:"backend", event:"llm_response", result:"failure", timestamp:"2026-09-12T00:00:00Z", correlation_id:"request", call_id:"call", purpose:"summary", http_status:502, truncated:true, payload:'{"error":{"message":"<script>alert(1)</script>"}}' }} />);
    expect(screen.getByText("Суммаризация")).toBeInTheDocument();
    expect(screen.getByText("HTTP 502")).toBeInTheDocument();
    expect(screen.getByText(/Ответ усечён/)).toBeInTheDocument();
    expect(document.querySelector("script")).toBeNull();
    expect(screen.getByText("Тело ответа")).toBeInTheDocument();
  });
  it("shows an unknown backend purpose without an allowlist", () => {
    render(<LogCard log={{ source:"backend", event:"llm_response", result:"success", timestamp:"2026-09-12T00:00:00Z", correlation_id:"request", purpose:"future_backend_purpose", provider_trace:{ retry: 2 } }} />);
    expect(screen.getByText("future_backend_purpose")).toBeInTheDocument();
    expect(screen.getByText("Все поля события · JSON").parentElement?.querySelector("pre")?.textContent).toContain('"provider_trace"');
  });
  it("uses the same formatting for the complete event payload", () => {
    render(<LogCard log={{ source:"backend", event:"llm_request", result:"success", timestamp:"2026-09-12T00:00:00Z", correlation_id:"request", payload:'{"prompt":"first\\nsecond"}' }} />);
    const allFields = screen.getByText("Все поля события · JSON").parentElement;
    expect(allFields?.querySelector("pre")?.textContent).toContain("first\nsecond");
  });
});
