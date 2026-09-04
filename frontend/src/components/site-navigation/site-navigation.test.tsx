import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SiteNavigation } from "./site-navigation";

describe("SiteNavigation", () => {
  it("renders all labelled destinations and marks the current page", () => {
    render(<SiteNavigation active="temperature" />);

    const barista = screen.getByRole("link", { name: "Бариста, Неделя 1, Задание 2" });
    const algorithms = screen.getByRole("link", { name: "Алгоритмы, Неделя 1, Задание 3" });
    const temperature = screen.getByRole("link", { name: "Температура, Неделя 1, задание 4" });

    expect(barista).toHaveAttribute("href", "/");
    expect(barista).not.toHaveAttribute("aria-current");
    expect(algorithms).toHaveAttribute("href", "/algorithms");
    expect(algorithms).not.toHaveAttribute("aria-current");
    expect(temperature).toHaveAttribute("href", "/temperature");
    expect(temperature).toHaveAttribute("aria-current", "page");
  });
});
