import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SiteNavigation } from "./site-navigation";

describe("SiteNavigation", () => {
  it("renders both labelled destinations and marks the current page", () => {
    render(<SiteNavigation active="algorithms" />);

    const barista = screen.getByRole("link", { name: "Бариста, Неделя 1, Задание 2" });
    const algorithms = screen.getByRole("link", { name: "Алгоритмы, Неделя 1, Задание 3" });

    expect(barista).toHaveAttribute("href", "/");
    expect(barista).not.toHaveAttribute("aria-current");
    expect(algorithms).toHaveAttribute("href", "/algorithms");
    expect(algorithms).toHaveAttribute("aria-current", "page");
  });
});
