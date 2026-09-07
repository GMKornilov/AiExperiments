"use client";

import Link from "next/link";
import { FormEvent, useEffect, useRef, useState } from "react";
import { baristaClient } from "@/features/barista/lib/chat-client";
import type { AdminLogsResponse } from "@/features/barista/model/types";
import styles from "@/app/admin/page.module.css";

export function AdminWorkspace() {
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

  const groups = [...(data?.logs ?? [])].sort((left, right) => left.timestamp.localeCompare(right.timestamp)).reduce<Record<string, AdminLogsResponse["logs"]>>((all, log) => { (all[log.source] ??= []).push(log); return all; }, {});
  return <main className={styles.page}>
    <Link href="/" className={styles.back}>← К чату</Link>
    <h1>Журнал диалога</h1><p>Введите точный ID диалога. Список доступных ID не показывается.</p>
    <form onSubmit={(event: FormEvent) => { event.preventDefault(); const id = dialogID.trim(); if (!id) return; setActiveID(id); void load(id, "lookup"); }}>
      <label htmlFor="dialog-id">ID диалога</label><div className={styles.controls}><input id="dialog-id" value={dialogID} onChange={(event) => setDialogID(event.target.value)} required /><button type="submit">Найти</button><button type="button" onClick={() => activeID && void load(activeID, "refresh")} disabled={!activeID}>Обновить</button></div>
    </form>
    {error && <p role="alert" className={styles.error}>{error}</p>}
    {data && !data.found && <p className={styles.notFound}>Данные не найдены.</p>}
    {data?.found && <><p className={styles.indicator}>Текстовые payloads: {data.log_text_payloads ? "включены" : "выключены"}</p>{(["frontend", "backend"] as const).map((source) => <section className={styles.group} key={source}><h2>{source === "frontend" ? "Frontend / BFF" : "Backend"}</h2>{groups[source]?.length ? <ol>{groups[source].map((log, index) => <li key={`${log.correlation_id}-${index}`}><time>{new Date(log.timestamp).toLocaleString("ru-RU")}</time><span>{log.event}</span><span>{log.result}</span><span>request: {log.correlation_id}</span>{log.error_category && <span>{log.error_category}</span>}{log.duration_ms !== undefined && <span>{log.duration_ms} мс</span>}{log.text && <span>{log.text}</span>}</li>)}</ol> : <p>Записей нет.</p>}</section>)}</>}
  </main>;
}
