"use client";

import { useState } from "react";
import styles from "./mcp-workspace.module.css";

type Tool = { name: string; description: string };
type State = "idle" | "loading" | "success" | "empty" | "error";

export function MCPWorkspace() {
  const [state, setState] = useState<State>("idle");
  const [tools, setTools] = useState<Tool[]>([]);

  async function connect() {
    setState("loading");
    try {
      const response = await fetch("/api/mcp/tools", { method: "POST" });
      const payload: unknown = await response.json();
      if (!response.ok || !isToolResponse(payload)) throw new Error("MCP request failed");
      setTools(payload.tools);
      setState(payload.tools.length === 0 ? "empty" : "success");
    } catch {
      setTools([]);
      setState("error");
    }
  }

  return (
    <section className={styles.workspace} aria-busy={state === "loading"} aria-labelledby="mcp-heading">
      <p className={styles.eyebrow}>Инструменты оборудования</p>
      <h1 id="mcp-heading" className={styles.title}>BrewMark MCP</h1>
      <p className={styles.intro}>Подключитесь к отдельному MCP-серверу и посмотрите доступные инструменты каталога кофейного оборудования.</p>
      <button className={styles.connectButton} type="button" onClick={connect} disabled={state === "loading"}>
        {state === "loading" ? "Подключаемся…" : "Подключиться и получить tools"}
      </button>

      <div className={styles.result} aria-live="polite">
        {state === "idle" && <p>Подключение ещё не выполнялось.</p>}
        {state === "loading" && <p role="status">Устанавливаем MCP-соединение и получаем список tools…</p>}
        {state === "empty" && <p role="status">MCP-сервер не вернул tools.</p>}
        {state === "error" && <p role="alert">Не удалось подключиться к MCP. Проверьте сервер и повторите попытку.</p>}
        {state === "success" && (
          <>
            <p role="status">Получено tools: {tools.length}.</p>
            <ul className={styles.toolList} aria-label="Доступные MCP tools">
              {tools.map((tool) => (
                <li className={styles.toolCard} key={tool.name}>
                  <code>{tool.name}</code>
                  <p>{tool.description}</p>
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </section>
  );
}

function isToolResponse(value: unknown): value is { tools: Tool[] } {
  return typeof value === "object" && value !== null
    && "tools" in value
    && Array.isArray(value.tools)
    && value.tools.every((tool) => typeof tool === "object" && tool !== null && "name" in tool && typeof tool.name === "string" && "description" in tool && typeof tool.description === "string");
}
