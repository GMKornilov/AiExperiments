import type { AdminLog } from "@/features/barista/model/types";
import styles from "@/app/admin/page.module.css";

function renderControlCharacters(json: string): string {
  return json.replace(/\\+(?:n|r|t)/g, (escape) => {
    const slashCount = escape.length - 1;
    if (slashCount % 2 === 0) {
      return escape;
    }

    const character = escape.at(-1);
    const controls: Record<string, string> = { n: "\n", r: "\r", t: "\t" };
    return "\\".repeat(slashCount - 1) + controls[character ?? ""];
  });
}

function isPromptTextField(key: string): boolean {
  return key === "content" || key === "prompt" || key === "text";
}

function decodePromptText(value: unknown, key?: string): unknown {
  if (typeof value === "string") {
    // LLM request bodies occasionally arrive as JSON serialized twice. Decode
    // their visible control sequences only in prompt-bearing fields; other
    // fields (paths, regular expressions, etc.) retain literal backslashes.
    return key && isPromptTextField(key)
      ? value.replace(/\\+(?:n|r|t)/g, (escape) => {
        const control = escape.at(-1);
        return { n: "\n", r: "\r", t: "\t" }[control ?? ""] ?? escape;
      })
      : value;
  }

  if (Array.isArray(value)) {
    return value.map((item) => decodePromptText(item));
  }

  if (value !== null && typeof value === "object") {
    return Object.fromEntries(
      Object.entries(value).map(([entryKey, entryValue]) => [entryKey, decodePromptText(entryValue, entryKey)]),
    );
  }

  return value;
}

export function formatPayload(text: string) {
  try {
    return renderControlCharacters(JSON.stringify(decodePromptText(JSON.parse(text)), null, 2));
  } catch {
    return text;
  }
}

function formatAllFields(log: AdminLog): string {
  const payload = log.payload === undefined ? {} : { payload: (() => {
    try { return JSON.parse(log.payload); } catch { return log.payload; }
  })() };
  return formatPayload(JSON.stringify({ ...log, ...payload }));
}

export function LogCard({ log }: { log: AdminLog }) {
  const labels = { chat: "Ответ агента", title: "Название", summary: "Суммаризация", facts: "Facts", memory: "Память", memory_extractor: "Извлечение памяти" };
  const outcome = log.outcome ?? log.result ?? "success";
  const title = log.event === "llm_request" ? "Запрос → LLM" : log.event === "llm_response" ? "Ответ ← LLM" : log.operation ?? log.event;
  const purpose = log.purpose ? labels[log.purpose as keyof typeof labels] ?? log.purpose : log.event.startsWith("mcp_") || ["frontend_bff", "mcp", "mcp_server", "brewmark"].includes(log.source) ? "MCP → BrewMark" : log.source;
  return <article className={styles.card} data-result={outcome}>
    <header className={styles.cardHeader}>
      <div><span className={styles.kind}>{purpose}</span><h2>{title}</h2></div>
      <span className={styles.badge}>{outcome === "failure" ? "Ошибка" : outcome === "started" ? "Отправлен" : "Успешно"}</span>
    </header>
    <div className={styles.meta}><time dateTime={log.timestamp}>{new Date(log.timestamp).toLocaleString("ru-RU")}</time><span>{log.duration_ms ?? 0} мс</span>{log.http_status !== undefined && <span>HTTP {log.http_status}</span>}{log.error_category && <span>{log.error_category}</span>}</div>
    {log.call_id && <p className={styles.callID}>Вызов: {log.call_id}</p>}
    {(log.event.startsWith("mcp_") || ["frontend_bff", "mcp", "mcp_server", "brewmark"].includes(log.source)) && <p className={styles.callID}>Источник: {log.source} · Корреляция: {log.correlation_id}</p>}
    {log.truncated && <p className={styles.error}>Ответ усечён: превышен предел чтения 1 МиБ.</p>}
    {log.payload !== undefined && <details className={styles.jsonBlock}><summary>{log.event === "llm_request" ? "Тело запроса" : "Тело ответа"}</summary><pre>{formatPayload(log.payload)}</pre></details>}
    {log.text && <details className={styles.jsonBlock}><summary>Текст события</summary><pre>{formatPayload(log.text)}</pre></details>}
    <details className={styles.jsonBlock}><summary>Все поля события · JSON</summary><pre>{formatAllFields(log)}</pre></details>
  </article>;
}
