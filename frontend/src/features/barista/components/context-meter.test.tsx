import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, it, expect } from "vitest";
import { ContextMeter } from "./context-meter";
import type { CompressionState } from "../model/types";

const state: CompressionState = { available: true, enabled: true, summary: "", covered_messages: 10, summary_tokens: 200, summary_usage_missing: false, full_estimate: 1000, sent_estimate: 300, last_input_tokens: 250, context_window_tokens: 1000 };
afterEach(cleanup);
describe("context window", () => {
  it("uses last provider input instead of cumulative usage", () => {
    render(<ContextMeter state={state} />);
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "250");
    expect(screen.getByText(/25.0%/)).toBeInTheDocument();
    expect(screen.getByText(/Оценка входа/)).toBeInTheDocument();
  });
  it("does not turn unknown usage into zero", () => {
    render(<ContextMeter state={{ ...state, last_input_tokens: null }} />);
    expect(screen.getByRole("progressbar")).not.toHaveAttribute("aria-valuenow");
    expect(screen.getByText(/Нет данных о входе/)).toBeInTheDocument();
  });
  it("clamps overflowing visuals while retaining actual input", () => {
    render(<ContextMeter state={{ ...state, last_input_tokens: 1200 }} />);
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "1000");
    expect(screen.getByText(/120.0%/)).toBeInTheDocument();
  });
});
