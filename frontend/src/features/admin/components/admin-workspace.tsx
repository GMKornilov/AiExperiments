"use client";

import Link from "next/link";
import { LogCard } from "./log-card";
import { FormEvent, useEffect, useRef, useState } from "react";
import { baristaClient } from "@/features/barista/lib/chat-client";
import type { AdminLogsResponse, MCPAdminLogsResponse } from "@/features/barista/model/types";
import styles from "@/app/admin/page.module.css";

function isMCPLogsResponse(value: AdminLogsResponse | MCPAdminLogsResponse | null): value is MCPAdminLogsResponse {
  return value !== null && "scope" in value && value.scope === "mcp";
}

export function AdminWorkspace() {
  const [scope, setScope] = useState<"chat" | "mcp">("chat");
  const [onlyLLM, setOnlyLLM] = useState(true);
  const [chatID, setChatID] = useState("");
  const [activeChatID, setActiveChatID] = useState("");
  const [data, setData] = useState<AdminLogsResponse | MCPAdminLogsResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const revision = useRef(0);

  async function loadChat(id: string, action: "lookup" | "refresh" | "poll") {
    const sequence = ++revision.current;
    try {
      const result = await baristaClient.logs(id, action);
      if (sequence !== revision.current) return;
      setData(result); setError(null);
    } catch {
      if (sequence === revision.current) setError("Не удалось загрузить журнал.");
    }
  }

  async function loadMCP() {
    const sequence = ++revision.current;
    try {
      const result = await baristaClient.mcpLogs();
      if (sequence !== revision.current) return;
      setData(result); setError(null);
    } catch {
      if (sequence === revision.current) setError("Не удалось загрузить MCP-журнал.");
    }
  }

  useEffect(() => {
    if (scope === "mcp") {
      const initial = window.setTimeout(() => { void loadMCP(); }, 0);
      const timer = window.setInterval(() => { void loadMCP(); }, 5000);
      return () => { window.clearTimeout(initial); window.clearInterval(timer); };
    }
    if (!activeChatID) return;
    const timer = window.setInterval(() => { void loadChat(activeChatID, "poll"); }, 5000);
    return () => window.clearInterval(timer);
  }, [activeChatID, scope]);

  const mcpData = isMCPLogsResponse(data) ? data : null;
  const chatData = data && !isMCPLogsResponse(data) ? data : null;
  const isMCP = Boolean(mcpData);
  const logs = [...(data?.logs ?? [])].filter(log => isMCP || !onlyLLM || log.call_id).sort((left, right) => left.timestamp.localeCompare(right.timestamp));
  return <main className={styles.page}>
    <Link href="/" className={styles.back}>← К чату</Link>
    <h1>{scope === "mcp" ? "Журнал MCP" : "Журнал чата"}</h1>
    <fieldset className={styles.controls}><legend>Источник журнала</legend><label><input type="radio" name="log-scope" checked={scope === "chat"} onChange={() => { setScope("chat"); setData(null); setError(null); }} /> Журнал чата</label><label><input type="radio" name="log-scope" checked={scope === "mcp"} onChange={() => { setScope("mcp"); setData(null); setError(null); }} /> Системный MCP</label></fieldset>
    {scope === "chat" && <><p>Введите точный ID чата. Список доступных ID не показывается.</p>
      <form onSubmit={(event: FormEvent) => { event.preventDefault(); const id = chatID.trim(); if (!id) return; setActiveChatID(id); void loadChat(id, "lookup"); }}>
        <label htmlFor="chat-id">ID чата</label><div className={styles.controls}><input id="chat-id" value={chatID} onChange={(event) => setChatID(event.target.value)} required /><button type="submit">Найти</button><button type="button" onClick={() => activeChatID && void loadChat(activeChatID, "refresh")} disabled={!activeChatID}>Обновить</button></div>
      </form>
    </>}
    {scope === "mcp" && <p>События запроса tools и цепочки MCP → BrewMark. ID чата не требуется.</p>}
    {scope === "mcp" && !data && !error && <p role="status">Загрузка MCP-журнала…</p>}
    {error && <p role="alert" className={styles.error}>{error}</p>}
    {chatData && !chatData.found && <p className={styles.notFound}>Данные не найдены.</p>}
    {data && (mcpData || chatData?.found) && <>
      <div className={styles.toolbar}>{mcpData ? <span className={styles.indicator}>Хранение: {mcpData.retention === "backend_runtime" ? "до перезапуска backend" : mcpData.retention}</span> : <><span className={styles.indicator}>Тела запросов и ответов: {chatData?.log_text_payloads ? "включены" : "выключены"}</span><label><input type="checkbox" checked={onlyLLM} onChange={event => setOnlyLLM(event.target.checked)} /> Только LLM</label></>}<span role="status">{logs.length} событий · обновление каждые 5 с</span>{mcpData && <button type="button" onClick={() => void loadMCP()}>Обновить</button>}</div>
      <p className={styles.notice}>{isMCP ? "Один correlation ID связывает BFF, backend и MCP-взаимодействие." : "Журнал хранится до перезапуска backend. Один ID вызова связывает запрос с ответом."}</p>
      <ol className={styles.timeline}>{logs.map((log, index) => <li key={`${log.timestamp}-${log.call_id ?? log.correlation_id}-${log.event}-${index}`}><LogCard log={log} /></li>)}</ol>
      {!logs.length && <p role="status" className={styles.notFound}>{isMCP ? "MCP-событий пока нет. Выполните подключение на вкладке MCP." : "Записей пока нет. Отправьте сообщение в диалоге или отключите фильтр LLM."}</p>}
    </>}
  </main>;
}
