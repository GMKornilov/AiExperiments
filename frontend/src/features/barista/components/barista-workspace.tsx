"use client";

import { FormEvent, KeyboardEvent, useEffect, useMemo, useRef, useState } from "react";
import { MarkdownContent } from "@/components/markdown-content/markdown-content";
import { BaristaAPIError, baristaClient, userFacingError } from "../lib/chat-client";
import type { BaristaMessage, Dialog } from "../model/types";
import styles from "./barista-workspace.module.css";

const newID = () => globalThis.crypto?.randomUUID?.() ?? `local-${Date.now()}-${Math.random()}`;
const hasPending = (dialog: Dialog) => dialog.messages.some((message) => message.status === "pending");
const hasError = (dialog: Dialog) => dialog.messages.some((message) => message.role === "user" && message.status === "error");
const sortDialogs = (dialogs: Dialog[]) => [...dialogs].sort((left, right) => right.updated_at.localeCompare(left.updated_at));

function mergeServerDialog(server: Dialog, local?: Dialog) {
  const localOnly = local?.messages.filter((message) => message.localOnly && !server.messages.some((saved) => saved.client_message_id === message.client_message_id)) ?? [];
  return localOnly.length ? { ...server, messages: [...server.messages, ...localOnly] } : server;
}

export function BaristaWorkspace() {
  const [dialogs, setDialogs] = useState<Dialog[]>([]);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [text, setText] = useState("");
  const [ready, setReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Dialog | null>(null);
  const [idCopyStatus, setIDCopyStatus] = useState("");
  const removed = useRef(new Set<string>());
  const retrying = useRef(new Set<string>());

  const selected = dialogs.find((dialog) => dialog.id === selectedID) ?? null;
  const messagePending = dialogs.some(hasPending);
  const sessionPending = dialogs.some((dialog) => hasPending(dialog) || dialog.title_status === "pending");

  const reconcile = async () => {
    const list = await baristaClient.list();
    setDialogs((current) => sortDialogs(list.dialogs.map((dialog) => mergeServerDialog(dialog, current.find((item) => item.id === dialog.id)))));
    setSelectedID((current) => list.selected_dialog_id || (current && list.dialogs.some((dialog) => dialog.id === current) ? current : list.dialogs[0]?.id ?? null));
    return list;
  };

  useEffect(() => {
    const timer = window.setTimeout(() => { void reconcile().catch(() => setError("Не удалось загрузить диалоги.")).finally(() => setReady(true)); }, 0);
    return () => window.clearTimeout(timer);
  }, []);
  useEffect(() => {
    if (!sessionPending) return;
    const timer = window.setInterval(() => { void reconcile().catch(() => undefined); }, 2000);
    return () => window.clearInterval(timer);
  }, [sessionPending]);

  const replaceDialog = (next: Dialog) => {
    if (removed.current.has(next.id)) return;
    setDialogs((current) => sortDialogs(current.map((dialog) => dialog.id === next.id ? next : dialog)));
  };

  async function createDialog() {
    setError(null);
    setIDCopyStatus("");
    try {
      const dialog = await baristaClient.create();
      setDialogs((current) => sortDialogs([dialog, ...current]));
      setSelectedID(dialog.id);
      setSidebarOpen(false);
      void baristaClient.event("dialog_created", { dialog_id: dialog.id });
    } catch (cause) { setError(userFacingError(cause)); }
  }

  async function selectDialog(id: string) {
    setIDCopyStatus("");
    setSelectedID(id);
    setSidebarOpen(false);
    try {
      await baristaClient.select(id);
      const dialog = await baristaClient.get(id);
      replaceDialog(dialog);
      void baristaClient.event("dialog_selected", { dialog_id: id });
    } catch { void reconcile().catch(() => undefined); }
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    const id = deleteTarget.id;
    removed.current.add(id);
    setDialogs((current) => current.filter((dialog) => dialog.id !== id));
    setSelectedID((current) => current === id ? null : current);
    setDeleteTarget(null);
    try {
      await baristaClient.remove(id);
      void baristaClient.event("dialog_deleted", { dialog_id: id });
      await reconcile();
    } catch (cause) {
      removed.current.delete(id);
      setError(userFacingError(cause));
      void reconcile().catch(() => undefined);
    }
  }

  async function send(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    if (!selected || messagePending || hasError(selected)) return;
    const normalized = text.trim();
    if (!normalized || Array.from(normalized).length > 4000) {
      setError("Введите вопрос длиной до 4 000 символов.");
      void baristaClient.event("dialog_validation_failed", { dialog_id: selected.id, error_category: "validation" });
      return;
    }
    const clientID = newID();
    const optimistic: BaristaMessage = { id: `local-${clientID}`, client_message_id: clientID, role: "user", text: normalized, status: "pending", created_at: new Date().toISOString(), localOnly: true };
    setText("");
    replaceDialog({ ...selected, messages: [...selected.messages, optimistic], updated_at: optimistic.created_at });
    try {
      const dialog = await baristaClient.send(selected.id, clientID, normalized);
      replaceDialog(dialog);
      void baristaClient.event("message_sent", { dialog_id: selected.id, message_id: clientID });
    } catch (cause) {
      const category = cause instanceof BaristaAPIError ? cause.category : "network";
      replaceDialog({ ...selected, messages: [...selected.messages, { ...optimistic, status: "error", error_category: category }], updated_at: optimistic.created_at });
      void baristaClient.event("message_failed", { dialog_id: selected.id, message_id: clientID, error_category: category });
      setError(userFacingError(cause));
    }
  }

  async function retry(message: BaristaMessage) {
    if (!selected || messagePending || retrying.current.has(message.id)) return;
    retrying.current.add(message.id);
    setError(null);
    let current: Dialog | undefined;
    let canonical = message;
    let persisted = false;
    try {
      current = await baristaClient.get(selected.id);
      const serverMessage = current.messages.find((item) => item.id === message.id || item.client_message_id === message.client_message_id);
      canonical = serverMessage ?? message;
      persisted = Boolean(serverMessage);
      if (serverMessage?.status === "success" || (serverMessage && current.messages.some((item) => item.role === "assistant" && item.created_at >= serverMessage.created_at))) { replaceDialog(current); return; }
      if (serverMessage?.status === "pending") { replaceDialog(current); return; }
      const pending = { ...canonical, status: "pending" as const, error_category: undefined, localOnly: !serverMessage || message.localOnly };
      replaceDialog({ ...current, messages: serverMessage ? current.messages.map((item) => item.id === serverMessage.id ? pending : item) : [...current.messages, pending] });
      const retryID = serverMessage?.id;
      if (!retryID) {
        const dialog = await baristaClient.send(selected.id, message.client_message_id ?? newID(), message.text);
        replaceDialog(dialog);
      } else {
        const dialog = await baristaClient.retry(selected.id, retryID);
        replaceDialog(dialog);
      }
      void baristaClient.event("message_retried", { dialog_id: selected.id, message_id: message.id });
    } catch (cause) {
      const category = cause instanceof BaristaAPIError ? cause.category : "network";
      if (current) replaceDialog({ ...current, messages: [...current.messages.filter((item) => item.id !== canonical.id && item.client_message_id !== canonical.client_message_id), { ...canonical, status: "error", error_category: category, localOnly: persisted ? undefined : true }] });
      setError(userFacingError(cause));
    } finally {
      retrying.current.delete(message.id);
    }
  }

  async function copy(message: BaristaMessage) {
    try { await navigator.clipboard.writeText(message.text); } catch { setError("Не удалось скопировать сообщение."); return; }
    if (selected) void baristaClient.event("message_copied", { dialog_id: selected.id, message_id: message.id });
  }

  async function copyDialogID() {
    if (!selected) return;
    try { await navigator.clipboard.writeText(selected.id); setIDCopyStatus("ID чата скопирован"); void baristaClient.event("dialog_id_copied", { dialog_id: selected.id }); }
    catch { setIDCopyStatus("Не удалось скопировать ID чата."); }
  }

  const composerDisabled = !selected || messagePending || (selected && hasError(selected));
  const dialogsLabel = useMemo(() => `${dialogs.length} диалогов`, [dialogs.length]);

  return <div className={styles.shell}>
    <button className={styles.menuButton} type="button" aria-expanded={sidebarOpen} aria-controls="dialogs-sidebar" onClick={() => setSidebarOpen((open) => !open)}>Диалоги</button>
    <aside id="dialogs-sidebar" className={`${styles.sidebar} ${sidebarOpen ? styles.sidebarOpen : ""}`} aria-label="Диалоги">
      <button className={styles.newDialog} type="button" disabled={!ready} onClick={createDialog}>+ Новый диалог</button>
      <p className={styles.counter}>{dialogsLabel}</p>
      <nav>{dialogs.map((dialog) => <div className={styles.dialogRow} key={dialog.id}>
        <button type="button" className={dialog.id === selectedID ? styles.selectedDialog : styles.dialogButton} onClick={() => void selectDialog(dialog.id)}>
          <span>{dialog.title}</span><small>ID: {dialog.id}</small>
        </button>
        <button type="button" className={styles.deleteButton} aria-label={`Удалить диалог ${dialog.title}`} onClick={() => setDeleteTarget(dialog)}>×</button>
      </div>)}</nav>
    </aside>
    <main className={styles.chat} aria-busy={!ready}>
      {error && <p className={styles.error} role="alert">{error}</p>}
      {!ready ? <div className={styles.empty}>Загружаем диалоги…</div> : !selected ? <div className={styles.empty}><h2>Чашка ждёт вопроса</h2><p>Создайте диалог, чтобы поговорить с бариста.</p><button type="button" onClick={createDialog}>Новый диалог</button></div> : <>
        <header className={styles.chatHeader}><div><h1>{selected.title}</h1><p>ID: {selected.id} <button type="button" className={styles.iconButton} aria-label="Копировать ID чата" onClick={() => void copyDialogID()}><CopyIcon /></button></p>{selected.title_status === "pending" && <p className={styles.titlePending} role="status">Обновляем название…</p>}<span className={styles.srOnly} aria-live="polite">{idCopyStatus}</span></div></header>
        <section className={styles.messages} aria-label="Переписка" aria-live="polite">
          {selected.messages.length === 0 && <div className={styles.empty}><p>Задайте вопрос о зёрнах, помоле или рецепте.</p></div>}
          {selected.messages.map((message) => <article className={`${styles.bubble} ${message.role === "user" ? styles.userBubble : styles.assistantBubble}`} key={message.id}>
            <div className={styles.messageActions}><span>{message.role === "user" ? "Вы" : "Бариста"}</span><button type="button" className={styles.iconButton} aria-label="Копировать сообщение" onClick={() => void copy(message)}><CopyIcon /></button></div>
            <MarkdownContent>{message.text}</MarkdownContent>
            {message.status === "pending" && <p className={styles.status} role="status">Бариста готовит ответ…</p>}
            {message.status === "error" && <div className={styles.failed}><p>Не удалось получить ответ. Повторите отправку.</p>{message.role === "user" && <button type="button" className={styles.iconButton} aria-label="Повторить отправку" onClick={() => void retry(message)}><RetryIcon /></button>}</div>}
          </article>)}
        </section>
        <form className={styles.composer} onSubmit={send}>
          <label htmlFor="barista-message">Ваш вопрос</label>
          <textarea id="barista-message" value={text} rows={3} disabled={composerDisabled} onChange={(event) => setText(event.target.value)} onKeyDown={(event: KeyboardEvent<HTMLTextAreaElement>) => { if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }} placeholder="Например: эспрессо горчит — что изменить?" />
          <div><span>{Array.from(text).length}/4000</span><button type="submit" disabled={composerDisabled || !text.trim()}>Отправить</button></div>
          {selected && hasError(selected) && <p className={styles.blocked}>Повторите ошибочную отправку, чтобы продолжить диалог.</p>}
        </form>
      </>}
    </main>
    {deleteTarget && <div className={styles.dialogOverlay} role="presentation" onKeyDown={(event) => {
      if (event.key === "Escape") { setDeleteTarget(null); return; }
      if (event.key !== "Tab") return;
      const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>("button:not([disabled])"));
      if (!controls.length) return;
      const first = controls[0]; const last = controls.at(-1);
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
      if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    }}><section className={styles.confirm} role="alertdialog" aria-modal="true" aria-labelledby="delete-title"><h2 id="delete-title">Удалить диалог?</h2><p>Переписка и незавершённый ответ будут удалены без возможности восстановления.</p><div><button type="button" autoFocus onClick={() => setDeleteTarget(null)}>Отмена</button><button type="button" className={styles.danger} onClick={() => void confirmDelete()}>Удалить</button></div></section></div>}
  </div>;
}

function CopyIcon() { return <svg aria-hidden="true" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2"><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M15 9V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h3" /></svg>; }
function RetryIcon() { return <svg aria-hidden="true" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2"><path d="M20 11a8 8 0 1 0 2 5.5" /><path d="M20 4v7h-7" /></svg>; }
