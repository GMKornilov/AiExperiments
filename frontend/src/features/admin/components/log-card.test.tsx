import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { formatPayload, LogCard } from "./log-card";
afterEach(cleanup);
describe("LLM log cards", () => {
  it("formats nested JSON and preserves plain text responses", () => {
    expect(formatPayload('{"messages":[{"role":"user","content":"test"}]}')).toContain('\n  "messages": [\n    {');
    expect(formatPayload("<html>failure</html>")).toBe("<html>failure</html>");
  });
  it("shows purpose, HTTP status and the raw provider response safely", () => {
    render(<LogCard log={{ source:"backend", event:"llm_response", result:"failure", timestamp:"2026-09-12T00:00:00Z", correlation_id:"request", call_id:"call", purpose:"summary", http_status:502, truncated:true, payload:'{"error":{"message":"<script>alert(1)</script>"}}' }} />);
    expect(screen.getByText("Суммаризация")).toBeInTheDocument();
    expect(screen.getByText("HTTP 502")).toBeInTheDocument();
    expect(screen.getByText(/Ответ усечён/)).toBeInTheDocument();
    expect(document.querySelector("script")).toBeNull();
    expect(screen.getByText("Тело ответа")).toBeInTheDocument();
  });
});
