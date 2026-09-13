"use client";

import { FormEvent, KeyboardEvent, useEffect, useMemo, useRef, useState } from "react";
import { ContextMeter } from "./context-meter";
import { CollapsibleMessage } from "./collapsible-message";
import { BaristaAPIError, baristaClient, userFacingError } from "../lib/chat-client";
import type { BaristaMessage, ContextStrategy, ContextStrategyID, Dialog } from "../model/types";
import styles from "./barista-workspace.module.css";

const newID = () => globalThis.crypto?.randomUUID?.() ?? `local-${Date.now()}-${Math.random()}`;
const hasPending = (dialog: Dialog) => dialog.messages.some((message) => message.status === "pending");
const hasError = (dialog: Dialog) => dialog.messages.some((message) => message.role === "user" && message.status === "error");
const sortDialogs = (dialogs: Dialog[]) => [...dialogs].sort((left, right) => right.updated_at.localeCompare(left.updated_at));
const strategyLabels: Record<ContextStrategyID, string> = { sliding_window: "Sliding Window", facts: "Facts", branching: "Branching", summary: "Summary" };
const defaultStrategies: ContextStrategy[] = (Object.keys(strategyLabels) as ContextStrategyID[]).map((id) => ({ id, available: true }));

function mergeServerDialog(server: Dialog, local?: Dialog) {
  if (local?.active_branch_id !== server.active_branch_id) return server;
  const localOnly = local?.messages.filter((message) => message.localOnly && !server.messages.some((saved) => saved.client_message_id === message.client_message_id)) ?? [];
  return localOnly.length ? { ...server, messages: [...server.messages, ...localOnly] } : server;
}

export function BaristaWorkspace() {
  const [dialogs, setDialogs] = useState<Dialog[]>([]);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [text, setText] = useState("");
  const [commandsDismissed, setCommandsDismissed] = useState(false);
  const [strategies, setStrategies] = useState<ContextStrategy[]>(defaultStrategies);
  const [draftStrategy, setDraftStrategy] = useState<ContextStrategyID | null>(null);
  const [choosingStrategy, setChoosingStrategy] = useState(false);
  const [branchBusy, setBranchBusy] = useState(false);
  const branchRevision = useRef(0);
  const [compacting, setCompacting] = useState(false);
  const compactLock = useRef(false);
  const [compactStatus, setCompactStatus] = useState<{ id: string; text: string } | null>(null);
  const commandButton = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const messagesRef = useRef<HTMLElement>(null);
  const compressionRevision = useRef(0);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Dialog | null>(null);
  const [idCopyStatus, setIDCopyStatus] = useState("");
  const removed = useRef(new Set<string>());
  const retrying = useRef(new Set<string>());

  const selected = dialogs.find((dialog) => dialog.id === selectedID) ?? null;
  const showCommands = selected?.context_strategy === "summary" && !commandsDismissed && /^\/[a-z]*$/i.test(text) && "/compact".startsWith(text.toLowerCase());
  const messageCount = `${selected?.active_branch_id ?? "main"}:${selected?.messages.length ?? 0}`;
  useEffect(() => {
    const list = messagesRef.current;
    if (list) list.scrollTop = list.scrollHeight;
  }, [selectedID, messageCount]);
  const messagePending = compacting || dialogs.some(hasPending);
  const sessionPending = dialogs.some((dialog) => hasPending(dialog) || dialog.title_status === "pending");

  const reconcile = async () => {
    const revision = compressionRevision.current;
    const list = await baristaClient.list();
    if (revision !== compressionRevision.current) return list;
    setDialogs((current) => sortDialogs(list.dialogs.map((dialog) => mergeServerDialog(dialog, current.find((item) => item.id === dialog.id)))));
    setSelectedID((current) => list.selected_dialog_id || (current && list.dialogs.some((dialog) => dialog.id === current) ? current : list.dialogs[0]?.id ?? null));
    return list;
  };
  const loadStrategies = async () => {
    const result = await baristaClient.strategies();
    if (Array.isArray(result.strategies)) setStrategies(result.strategies);
  };

  useEffect(() => {
    const timer = window.setTimeout(() => { void reconcile().catch(() => setError("Не удалось загрузить диалоги.")).finally(() => setReady(true)); }, 0);
    return () => window.clearTimeout(timer);
  }, []);
  useEffect(() => {
    if (!ready || selected?.context_strategy) return;
    const timer = window.setTimeout(() => { void loadStrategies().catch(() => setError("Не удалось загрузить список стратегий.")); }, 0);
    return () => window.clearTimeout(timer);
  }, [ready, selected?.context_strategy]);
  useEffect(() => {
    if (!sessionPending) return;
    const timer = window.setInterval(() => { void reconcile().catch(() => undefined); }, 2000);
    return () => window.clearInterval(timer);
  }, [sessionPending]);

  const replaceDialog = (next: Dialog) => {
    if (removed.current.has(next.id)) return;
    setDialogs((current) => sortDialogs(current.map((dialog) => dialog.id === next.id ? next : dialog)));
  };

  async function createDialog(strategy = draftStrategy) {
    if (!strategy) { setDraftStrategy(null); setSelectedID(null); void loadStrategies().catch(() => setError("Не удалось загрузить список стратегий.")); return; }
    setError(null);
    setIDCopyStatus("");
    try {
      const dialog = await baristaClient.create(strategy);
      setDialogs((current) => sortDialogs([dialog, ...current]));
      setSelectedID(dialog.id);
      setDraftStrategy(null);
      setSidebarOpen(false);
      void baristaClient.event("dialog_created", { dialog_id: dialog.id });
    } catch (cause) { setError(userFacingError(cause)); }
  }

  async function chooseStrategy(strategy: ContextStrategyID) {
    if (choosingStrategy || messagePending) return;
    const option = strategies.find((item) => item.id === strategy);
    if (!option?.available) return;
    setChoosingStrategy(true); setError(null);
    try {
      if (!selected) { setDraftStrategy(strategy); await createDialog(strategy); return; }
      const saved = await baristaClient.strategy(selected.id, strategy);
      replaceDialog(saved);
      void baristaClient.event("strategy_selected", { dialog_id: selected.id });
    } catch (cause) { setError(userFacingError(cause)); }
    finally { setChoosingStrategy(false); }
  }

  async function addBranch() {
    if (!selected || branchBusy || messagePending || selected.context_strategy !== "branching") return;
    const revision = ++branchRevision.current;
    setBranchBusy(true); setError(null);
    try { const saved = await baristaClient.createBranch(selected.id); if (revision === branchRevision.current) replaceDialog(saved); void baristaClient.event("branch_created", { dialog_id: selected.id }); }
    catch (cause) { setError(userFacingError(cause)); }
    finally { setBranchBusy(false); }
  }

  async function switchBranch(branchID: string) {
    if (!selected || branchBusy || branchID === selected.active_branch_id) return;
    ++branchRevision.current;
    setBranchBusy(true); setError(null);
    try { replaceDialog(await baristaClient.selectBranch(selected.id, branchID)); void baristaClient.event("branch_selected", { dialog_id: selected.id }); }
    catch (cause) { setError(userFacingError(cause)); }
    finally { setBranchBusy(false); }
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

  async function compact() {
    if (!selected) return;
    if (selected.context_strategy !== "summary") {
      setError("Команда /compact доступна только в режиме Summary.");
      return;
    }
    if (compactLock.current || messagePending || hasError(selected)) return;
    if (!selected.compression?.available) { setError("Суммаризация не настроена для этого диалога."); return; }
    const id = selected.id;
    compactLock.current = true;
    setCompacting(true);
    setCommandsDismissed(true);
    setError(null);
    setCompactStatus({ id, text: "Сжимаем контекст…" });
    compressionRevision.current++;
    try {
      const saved = await baristaClient.compact(id);
      replaceDialog(saved);
      setText(current => /^\/[a-z]*$/i.test(current) ? "" : current);
      const count = (saved.compression?.covered_messages ?? 0) - (selected.compression?.covered_messages ?? 0);
      setCompactStatus({ id, text: count > 0 ? `Контекст сжат: ${count} сообщений заменено на summary. История чата сохранена.` : "Summary обновлено. В памяти агента осталось только summary." });
    } catch {
      setCompactStatus({ id, text: "Не удалось сжать контекст. Повторите /compact." });
    } finally {
      compactLock.current = false;
      setCompacting(false);
      compressionRevision.current++;
      void reconcile().catch(() => undefined);
    }
  }

  async function send(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    if (!selected || !selected.context_strategy || messagePending || hasError(selected)) return;
    const normalized = text.trim();
    if (!normalized) {
      setError("Введите вопрос.");
      void baristaClient.event("dialog_validation_failed", { dialog_id: selected.id, error_category: "validation" });
      return;
    }
    if (normalized === "/compact") { await compact(); return; }
    const clientID = newID();
    const branchAtSend = selected.active_branch_id;
    const branchRevisionAtSend = branchRevision.current;
    const optimistic: BaristaMessage = { id: `local-${clientID}`, client_message_id: clientID, role: "user", text: normalized, status: "pending", created_at: new Date().toISOString(), localOnly: true };
    setText("");
    replaceDialog({ ...selected, messages: [...selected.messages, optimistic], updated_at: optimistic.created_at });
    try {
      const dialog = await baristaClient.send(selected.id, clientID, normalized);
      if (branchRevisionAtSend === branchRevision.current && branchAtSend === dialog.active_branch_id) replaceDialog(dialog);
      void baristaClient.event("message_sent", { dialog_id: selected.id, message_id: clientID });
    } catch (cause) {
      const category = cause instanceof BaristaAPIError ? cause.category : "network";
      if (branchRevisionAtSend === branchRevision.current) replaceDialog({ ...selected, messages: [...selected.messages, { ...optimistic, status: "error", error_category: category }], updated_at: optimistic.created_at });
      void baristaClient.event("message_failed", { dialog_id: selected.id, message_id: clientID, error_category: category });
      setError(userFacingError(cause));
      // A failed attempt may still have confirmed usage. Read the persisted result.
      try { const saved = await baristaClient.get(selected.id); if (branchRevisionAtSend === branchRevision.current) replaceDialog(mergeServerDialog(saved, { ...selected, messages: [...selected.messages, { ...optimistic, status: "error", error_category: category }] })); } catch { /* Keep the local error when the backend is unavailable. */ }
    }
  }

  async function retry(message: BaristaMessage) {
    if (!selected || !selected.context_strategy || messagePending || retrying.current.has(message.id)) return;
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
      if (current) {
        try { replaceDialog(mergeServerDialog(await baristaClient.get(selected.id), { ...current, messages: persisted ? current.messages : [...current.messages, { ...canonical, status: "error", error_category: category, localOnly: true }] })); } catch { /* Keep the last known result. */ }
      }
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

  const composerDisabled = !selected || !selected.context_strategy || choosingStrategy || messagePending || (selected && hasError(selected));
  const dialogsLabel = useMemo(() => `${dialogs.length} диалогов`, [dialogs.length]);
  const extraTokens = !selected ? null : selected.context_strategy === "facts" ? (selected.facts_tokens === undefined || selected.facts_usage_missing ? null : selected.facts_tokens) : selected.context_strategy === "summary" ? (selected.compression?.summary_tokens === undefined || selected.compression.summary_usage_missing ? null : selected.compression.summary_tokens) : 0;
  const totalTokens = selected && extraTokens !== null ? selected.accounted_tokens + extraTokens : null;

  return <div className={styles.shell}>
    <button className={styles.menuButton} type="button" aria-expanded={sidebarOpen} aria-controls="dialogs-sidebar" onClick={() => setSidebarOpen((open) => !open)}>Диалоги</button>
    <aside id="dialogs-sidebar" className={`${styles.sidebar} ${sidebarOpen ? styles.sidebarOpen : ""}`} aria-label="Диалоги">
      <button className={styles.newDialog} type="button" disabled={!ready} onClick={() => void createDialog()}>+ Новый диалог</button>
      <p className={styles.counter}>{dialogsLabel}</p>
      <nav>{dialogs.map((dialog) => <div className={styles.dialogRow} key={dialog.id}>
        <div className={styles.dialogMainRow}>
          <button type="button" className={dialog.id === selectedID ? styles.selectedDialog : styles.dialogButton} onClick={() => void selectDialog(dialog.id)}>
            <span>{dialog.title}</span><small>ID: {dialog.id}</small>
          </button>
          <button type="button" className={styles.deleteButton} aria-label={`Удалить диалог ${dialog.title}`} onClick={() => setDeleteTarget(dialog)}>×</button>
        </div>
        {dialog.id === selectedID && dialog.context_strategy === "branching" && <div className={styles.branchList} aria-label="Ветки диалога">
          {(dialog.branches ?? []).map((branch) => <button key={branch.id} type="button" data-testid={`branch-${branch.id}`} className={branch.id === dialog.active_branch_id ? styles.selectedBranch : styles.branchButton} aria-current={branch.id === dialog.active_branch_id ? "page" : undefined} disabled={branchBusy || messagePending} onClick={() => void switchBranch(branch.id)}>{branch.parent_branch_id ? "↳ " : ""}{branch.name} <small>{branch.message_count}</small></button>)}
          <button type="button" data-testid="add-branch" className={styles.addBranch} disabled={branchBusy || messagePending || !canAddBranch(dialog)} title={branchReason(dialog)} onClick={() => void addBranch()}>+ Добавить ветку</button>
        </div>}
      </div>)}</nav>
    </aside>
    <main className={styles.chat} aria-busy={!ready}>
      {error && <p className={styles.error} role="alert">{error}</p>}
      {!ready ? <div className={styles.empty}>Загружаем диалоги…</div> : !selected ? <div className={styles.empty}><h2>Чашка ждёт вопроса</h2><p>Выберите стратегию контекста, чтобы начать диалог.</p><StrategyPicker strategies={strategies} selected={draftStrategy} disabled={choosingStrategy} onChoose={chooseStrategy} /></div> : <>
        <header className={styles.chatHeader}><div><h1>{selected.title}</h1>{selected.title_status === "pending" && <p className={styles.titlePending} role="status">Обновляем название…</p>}<span className={styles.srOnly} aria-live="polite">{idCopyStatus}</span></div></header>
        {(!selected.context_strategy || selected.messages.length === 0) && <StrategyPicker strategies={strategies} selected={selected.context_strategy} disabled={choosingStrategy || selected.messages.length > 0} onChoose={chooseStrategy} />}
        <section ref={messagesRef} className={styles.messages} aria-label="Переписка" aria-live="polite">
          {selected.messages.length === 0 && <div className={styles.empty}><p>Задайте вопрос о зёрнах, помоле или рецепте.</p></div>}
          {selected.messages.map((message) => <article className={`${styles.bubble} ${message.role === "user" ? styles.userBubble : styles.assistantBubble}`} key={message.id}>
            <div className={styles.messageActions}><span>{message.role === "user" ? "Вы" : "Бариста"}</span><button type="button" className={styles.iconButton} aria-label="Копировать сообщение" onClick={() => void copy(message)}><CopyIcon /></button></div>
            <CollapsibleMessage key={`${selected.id}-${message.id}`} text={message.text} />
            {message.role === "assistant" && <p className={styles.tokenUsage}>{message.usage ? `Вход: ${message.usage.prompt_tokens} токенов · Выход: ${message.usage.completion_tokens} токенов` : "Токены: нет данных"}</p>}
            {message.status === "pending" && <p className={styles.status} role="status">Бариста готовит ответ…</p>}
            {message.status === "error" && <div className={styles.failed}><p>{userFacingError(new BaristaAPIError(message.error_category ?? "network"))}</p>{message.role === "user" && <button type="button" className={styles.iconButton} aria-label="Повторить отправку" onClick={() => void retry(message)}><RetryIcon /></button>}</div>}
          </article>)}
        </section>
        <form className={styles.composer} onSubmit={send}>
          <section className={styles.contextStatus} aria-label="Статус диалога">
            <div className={styles.threadID}><span>Диалог</span><code>{selected.id}</code><button type="button" className={styles.iconButton} aria-label="Копировать ID чата" onClick={() => void copyDialogID()}><CopyIcon /></button></div>
            <p data-testid="context-strategy-status" className={styles.status}>Стратегия: {selected.context_strategy ? strategyLabels[selected.context_strategy] : "не выбрана"}</p>
            <ContextMeter state={selected.compression} />
          </section>
          <p className={styles.accountedTokens} role="status">Chat: {selected.accounted_tokens.toLocaleString("ru-RU")} токенов{selected.context_strategy === "facts" && <> · Facts: {selected.facts_tokens === undefined ? "нет данных" : `${selected.facts_tokens.toLocaleString("ru-RU")} токенов`}{selected.facts_usage_missing && " (часть расхода неизвестна)"}</>}{selected.context_strategy === "summary" && <> · Summary: {selected.compression?.summary_tokens === undefined ? "нет данных" : `${selected.compression.summary_tokens.toLocaleString("ru-RU")} токенов`}{selected.compression?.summary_usage_missing && " (часть расхода неизвестна)"}</>} · Всего: {totalTokens === null ? "нет данных" : `${totalTokens.toLocaleString("ru-RU")} токенов`}</p>
          {selected.context_strategy === "facts" && <details className={styles.factsPanel} data-testid="facts-panel"><summary>Факты, используемые для контекста</summary>{selected.facts && Object.keys(selected.facts).length > 0 ? <dl>{Object.entries(selected.facts).map(([key, value]) => <div key={key}><dt>{key}</dt><dd>{value}</dd></div>)}</dl> : <p>Подтверждённых фактов пока нет.</p>}</details>}
          {compactStatus?.id === selected.id && <p className={styles.status} role="status">{compactStatus.text}</p>}
          <label className={styles.srOnly} htmlFor="barista-message">Ваш вопрос</label>
          <section className={styles.inputAnchor}>
            {showCommands && selected.context_strategy === "summary" && !compacting && <div className={styles.commandPopover}>
              <p id="commands-heading">Команды</p>
              <ul id="slash-commands" aria-labelledby="commands-heading">
                <li><button ref={commandButton} type="button" disabled={composerDisabled || !selected.compression?.available} onClick={() => void compact()} onKeyDown={event => { if (event.key === "Escape") { setCommandsDismissed(true); inputRef.current?.focus(); } if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); inputRef.current?.focus(); } }}>
                  <span className={styles.commandIcon} aria-hidden="true">↗↙</span>
                  <span><strong>/compact</strong><small>{selected.compression?.available ? "Сжать весь контекст · оставить только summary" : "Суммаризация не настроена"}</small></span>
                  <kbd>↵</kbd>
                </button></li>
              </ul>
            </div>}

          <textarea ref={inputRef} aria-controls={showCommands ? "slash-commands" : undefined} id="barista-message" value={text.length > 20_000 ? text.slice(0, 1000) : text} readOnly={text.length > 20_000} onPaste={(event) => { const pasted = event.clipboardData.getData("text"); if (pasted.length > 20_000) { event.preventDefault(); const element = event.currentTarget; setText(text.slice(0, element.selectionStart) + pasted + text.slice(element.selectionEnd)); } }} rows={3} disabled={composerDisabled} onChange={(event) => { setText(event.target.value); setCommandsDismissed(false); }} onKeyDown={(event: KeyboardEvent<HTMLTextAreaElement>) => { if (event.nativeEvent.isComposing) return; if (showCommands) {
            if (event.key === "Escape") { event.preventDefault(); setCommandsDismissed(true); return; }
            if (event.key === "Tab") { if (!event.shiftKey) { event.preventDefault(); setText("/compact"); } return; }
            if (event.key === "ArrowUp" || event.key === "ArrowDown") { event.preventDefault(); commandButton.current?.focus(); return; }
            if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); void compact(); return; }
          } if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }} placeholder={selected.context_strategy === "summary" ? "Вопрос о кофе или / для команд" : "Вопрос о кофе"} />
          </section>
          <div>{text.length > 20_000 && <p>Большой промпт: {text.length.toLocaleString("ru-RU")} символов. Показано превью; будет отправлен полный текст. <button type="button" onClick={() => setText("")}>Очистить промпт</button></p>}<button type="submit" disabled={composerDisabled || !text.trim()}>Отправить</button></div>
          {selected && hasError(selected) && <p className={styles.blocked}>{selected.messages.some((message) => message.status === "error" && message.error_category === "context_limit") ? "Начните новый диалог: повторная отправка сохранит тот же контекст." : "Повторите ошибочную отправку, чтобы продолжить диалог."}</p>}
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

function canAddBranch(dialog: Dialog) {
  const last = dialog.messages.at(-1);
  return last?.role === "assistant" && last.status === "success";
}

function branchReason(dialog: Dialog) {
  if (canAddBranch(dialog)) return "Создать ветку от последней успешной реплики.";
  if (dialog.messages.some((message) => message.status === "pending")) return "Дождитесь завершения ответа.";
  if (dialog.messages.some((message) => message.status === "error")) return "Сначала повторите ошибочную отправку.";
  return "Ветка появится после успешного ответа ассистента.";
}

function StrategyPicker({ strategies, selected, disabled, onChoose }: { strategies: ContextStrategy[]; selected: ContextStrategyID | null | undefined; disabled: boolean; onChoose: (id: ContextStrategyID) => void }) {
  return <fieldset className={styles.strategyPicker} aria-label="Стратегия контекста" data-testid="strategy-picker" disabled={disabled}>
    <legend>Стратегия контекста</legend>
    <p>Выберите способ передачи памяти в модель. После первого сообщения выбор фиксируется.</p>
    <div role="radiogroup" aria-label="Стратегия контекста">
      {strategies.map((strategy) => <button key={strategy.id} type="button" role="radio" data-testid={`strategy-card-${strategy.id}`} aria-checked={strategy.id === selected} disabled={disabled || !strategy.available} title={strategy.available ? strategyLabels[strategy.id] : strategy.reason ?? "Недоступно"} onClick={() => onChoose(strategy.id)}><strong>{strategyLabels[strategy.id]}</strong><small>{strategy.available ? strategyDescription(strategy.id) : strategy.reason ?? "Недоступно"}</small></button>)}
    </div>
  </fieldset>;
}

function strategyDescription(id: ContextStrategyID) {
  return ({ sliding_window: "Последние сообщения линии", facts: "Факты и последние сообщения", branching: "Полная активная ветка", summary: "Summary и несжатый хвост" })[id];
}
