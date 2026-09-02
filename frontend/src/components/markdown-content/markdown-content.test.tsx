import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MarkdownContent } from "./markdown-content";

describe("MarkdownContent", () => {
  it("renders supported Markdown elements", () => {
    render(
      <MarkdownContent>{`## Настройка\n\n**Помол:** мельче\n\n- 18 г кофе\n- 36 г воды\n\n| Время | Температура |\n| --- | --- |\n| 28 с | 93 °C |\n\n\`\`\`text\nЭкстракция\n\`\`\``}</MarkdownContent>,
    );

    expect(screen.getByRole("heading", { name: "Настройка", level: 2 })).toBeVisible();
    expect(screen.getByText("Помол:", { selector: "strong" })).toBeVisible();
    expect(screen.getByRole("list")).toHaveTextContent("18 г кофе");
    expect(screen.getByRole("table")).toHaveTextContent("Температура");
    expect(screen.getByText("Экстракция", { selector: "code" })).toBeVisible();
  });

  it("does not render raw HTML or Markdown images", () => {
    render(
      <MarkdownContent>{`<script>alert("xss")</script>\n\n<img src="https://example.com/raw.png" alt="raw" />\n\n![Markdown image](https://example.com/markdown.png)`}</MarkdownContent>,
    );

    expect(screen.queryByText('alert("xss")')).not.toBeInTheDocument();
    expect(screen.queryByRole("img")).not.toBeInTheDocument();
  });

  it("renders unsafe links as non-clickable text", () => {
    render(<MarkdownContent>{"[Открыть](javascript:alert(1))"}</MarkdownContent>);

    expect(screen.getByText("Открыть")).toBeVisible();
    expect(screen.queryByRole("link", { name: "Открыть" })).not.toBeInTheDocument();
  });
});
