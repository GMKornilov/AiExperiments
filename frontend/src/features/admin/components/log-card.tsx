import type { AdminLog } from "@/features/barista/model/types";
import styles from "@/app/admin/page.module.css";

export function formatPayload(text: string) {
  try { return JSON.stringify(JSON.parse(text), null, 2); } catch { return text; }
}

export function LogCard({ log }: { log: AdminLog }) {
  const labels = { chat: "Ответ агента", title: "Название", summary: "Суммаризация" };
  const title = log.event === "llm_request" ? "Запрос → LLM" : log.event === "llm_response" ? "Ответ ← LLM" : log.event;
  return <article className={styles.card} data-result={log.result}>
    <header className={styles.cardHeader}>
      <div><span className={styles.kind}>{log.purpose ? labels[log.purpose] : log.source}</span><h2>{title}</h2></div>
      <span className={styles.badge}>{log.result === "failure" ? "Ошибка" : log.result === "started" ? "Отправлен" : "Успешно"}</span>
    </header>
    <div className={styles.meta}><time dateTime={log.timestamp}>{new Date(log.timestamp).toLocaleString("ru-RU")}</time><span>{log.duration_ms ?? 0} мс</span>{log.http_status !== undefined && <span>HTTP {log.http_status}</span>}{log.error_category && <span>{log.error_category}</span>}</div>
    {log.call_id && <p className={styles.callID}>Вызов: {log.call_id}</p>}
    {log.truncated && <p className={styles.error}>Ответ усечён: превышен предел чтения 1 МиБ.</p>}
    {log.payload !== undefined && <details className={styles.jsonBlock}><summary>{log.event === "llm_request" ? "Тело запроса" : "Тело ответа"}</summary><pre>{formatPayload(log.payload)}</pre></details>}
    {log.text && <details className={styles.jsonBlock}><summary>Текст события</summary><pre>{formatPayload(log.text)}</pre></details>}
    <details className={styles.jsonBlock}><summary>Все поля события · JSON</summary><pre>{JSON.stringify({ ...log, ...(log.payload !== undefined ? { payload: (() => { try { return JSON.parse(log.payload); } catch { return log.payload; } })() } : {}) }, null, 2)}</pre></details>
  </article>;
}
