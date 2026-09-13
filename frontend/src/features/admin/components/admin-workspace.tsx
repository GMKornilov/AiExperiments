"use client";

import Link from "next/link";
import { LogCard } from "./log-card";
import { FormEvent, useEffect, useRef, useState } from "react";
import { baristaClient } from "@/features/barista/lib/chat-client";
import type { AdminLogsResponse } from "@/features/barista/model/types";
import styles from "@/app/admin/page.module.css";

export function AdminWorkspace() {
  const [onlyLLM, setOnlyLLM] = useState(true);
  const [dialogID, setDialogID] = useState("");
  const [activeID, setActiveID] = useState("");
  const [data, setData] = useState<AdminLogsResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const revision = useRef(0);

  async function load(id: string, action: "lookup" | "refresh" | "poll") {
    const sequence = ++revision.current;
    try {
      const result = await baristaClient.logs(id, action);
      if (sequence !== revision.current) return;
      setData(result); setError(null);
    } catch {
      if (sequence === revision.current) setError("Не удалось загрузить журнал.");
    }
  }

  useEffect(() => {
    if (!activeID) return;
    const timer = window.setInterval(() => { void load(activeID, "poll"); }, 5000);
    return () => window.clearInterval(timer);
  }, [activeID]);

  const logs = [...(data?.logs ?? [])].filter(log => !onlyLLM || log.call_id).sort((left, right) => left.timestamp.localeCompare(right.timestamp));
  return <main className={styles.page}>
    <Link href="/" className={styles.back}>← К чату</Link>
    <h1>Журнал диалога</h1><p>Введите точный ID диалога. Список доступных ID не показывается.</p>
    <form onSubmit={(event: FormEvent) => { event.preventDefault(); const id = dialogID.trim(); if (!id) return; setActiveID(id); void load(id, "lookup"); }}>
      <label htmlFor="dialog-id">ID диалога</label><div className={styles.controls}><input id="dialog-id" value={dialogID} onChange={(event) => setDialogID(event.target.value)} required /><button type="submit">Найти</button><button type="button" onClick={() => activeID && void load(activeID, "refresh")} disabled={!activeID}>Обновить</button></div>
    </form>
    {error && <p role="alert" className={styles.error}>{error}</p>}
    {data && !data.found && <p className={styles.notFound}>Данные не найдены.</p>}
    {data?.found && <>
      <div className={styles.toolbar}><span className={styles.indicator}>Тела запросов и ответов: {data.log_text_payloads ? "включены" : "выключены"}</span><label><input type="checkbox" checked={onlyLLM} onChange={event => setOnlyLLM(event.target.checked)} /> Только LLM</label><span>{logs.length} событий · обновление каждые 5 с</span></div>
      <p className={styles.notice}>Журнал хранится до перезапуска backend. Один ID вызова связывает запрос с ответом.</p>
      <ol className={styles.timeline}>{logs.map((log, index) => <li key={`${log.timestamp}-${log.call_id ?? log.correlation_id}-${log.event}-${index}`}><LogCard log={log} /></li>)}</ol>
      {!logs.length && <p className={styles.notFound}>Записей пока нет. Отправьте сообщение в диалоге или отключите фильтр LLM.</p>}
    </>}
  </main>;
}
