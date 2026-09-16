"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import { CollapsibleMessage } from "./collapsible-message";
import { BaristaAPIError, baristaClient, userFacingError } from "../lib/chat-client";
import type { BaristaMessage, Chat, Memory, Project, ProjectList } from "../model/types";
import styles from "./barista-workspace.module.css";

type Confirmation = { kind: "project"; project: Project } | { kind: "chat"; chat: Chat } | { kind: "memory"; layer: "global" | "project" };
const newID = () => crypto.randomUUID();
const emptyMemory: Memory = { global_facts: [], project_facts: [], status: "idle" };
const messageError = (message: BaristaMessage) => message.status === "error" ? userFacingError(new BaristaAPIError(message.error_category ?? "network")) : null;

export function BaristaWorkspace() {
  const [data, setData] = useState<ProjectList>({ projects: [] });
  const [selectedProjectID, setSelectedProjectID] = useState<string | null>(null);
  const [selectedChatID, setSelectedChatID] = useState<string | null>(null);
  const [memory, setMemory] = useState<Memory>(emptyMemory);
  const [ready, setReady] = useState(false); const [pending, setPending] = useState(false);
  const [memoryOpen, setMemoryOpen] = useState(false); const [sidebarOpen, setSidebarOpen] = useState(false);
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null);
  const [renameTarget, setRenameTarget] = useState<Project | null>(null); const [renameTitle, setRenameTitle] = useState("");
  const [draft, setDraft] = useState(""); const [error, setError] = useState<string | null>(null);

  const selectedProject = useMemo(() => data.projects.find((project) => project.id === selectedProjectID) ?? null, [data, selectedProjectID]);
  const selectedChat = useMemo(() => selectedProject?.chats.find((chat) => chat.id === selectedChatID) ?? null, [selectedProject, selectedChatID]);
  const replaceChat = (chat: Chat) => setData(current => ({ ...current, projects: current.projects.map(project => project.id === chat.project_id ? { ...project, chats: project.chats.map(old => old.id === chat.id ? chat : old), selected_chat_id: chat.id } : project) }));

  async function load() {
    const next = await baristaClient.projects(); setData(next);
    const projectID = next.selected_project_id ?? next.projects[0]?.id ?? null;
    setSelectedProjectID(projectID);
    const project = next.projects.find(item => item.id === projectID);
    const chatID = next.selected_chat_id ?? project?.selected_chat_id ?? project?.chats[0]?.id ?? null;
    setSelectedChatID(chatID);
    if (projectID) setMemory(await baristaClient.memory(projectID)); else setMemory(emptyMemory);
  }
  useEffect(() => { void Promise.resolve().then(load).catch(cause => setError(userFacingError(cause))).finally(() => setReady(true)); }, []);
  useEffect(() => {
    if (!selectedProjectID || !selectedChatID || selectedChat?.title_status !== "pending") return;
    let cancelled = false; let attempts = 0; let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      try { const fresh = await baristaClient.getChat(selectedProjectID, selectedChatID); if (cancelled) return; replaceChat(fresh); if (fresh.title_status === "pending" && ++attempts < 8) timer = setTimeout(() => { void poll(); }, Math.min(500 * (attempts + 1), 2_000)); }
      catch { /* Title generation is optional. */ }
    };
    timer = setTimeout(() => { void poll(); }, 400);
    return () => { cancelled = true; if (timer) clearTimeout(timer); };
  }, [selectedProjectID, selectedChatID, selectedChat?.title_status]);

  async function selectProject(project: Project) {
    if (pending) return; setSelectedProjectID(project.id); setSelectedChatID(project.selected_chat_id ?? project.chats[0]?.id ?? null); setSidebarOpen(false); setError(null);
    try { await baristaClient.selectProject(project.id); setMemory(await baristaClient.memory(project.id)); void baristaClient.event("project_selected", { project_id: project.id }); } catch (cause) { setError(userFacingError(cause)); }
  }
  async function selectChat(chat: Chat) {
    if (!selectedProject || pending) return; setSelectedChatID(chat.id); setError(null);
    try { await baristaClient.selectChat(selectedProject.id, chat.id); replaceChat(await baristaClient.getChat(selectedProject.id, chat.id)); void baristaClient.event("chat_selected", { project_id: selectedProject.id, chat_id: chat.id }); } catch (cause) { setError(userFacingError(cause)); }
  }
  async function createProject() {
    if (pending) return; setPending(true); setError(null);
    try { const result = await baristaClient.createProject(); if ("projects" in result) { setData(result); const project = result.projects.find(item => item.id === result.selected_project_id) ?? result.projects.at(-1) ?? null; setSelectedProjectID(project?.id ?? null); setSelectedChatID(null); setMemory(project ? await baristaClient.memory(project.id) : emptyMemory); } else { setData(current => ({ ...current, projects: [...current.projects, result], selected_project_id: result.id })); setSelectedProjectID(result.id); setSelectedChatID(null); setMemory(emptyMemory); } void baristaClient.event("project_created"); } catch (cause) { setError(userFacingError(cause)); } finally { setPending(false); }
  }
  async function createChat() {
    if (!selectedProject || pending) return; setPending(true); setError(null);
    try { const chat = await baristaClient.createChat(selectedProject.id); setData(current => ({ ...current, projects: current.projects.map(project => project.id === selectedProject.id ? { ...project, chats: [...project.chats, chat], selected_chat_id: chat.id } : project) })); setSelectedChatID(chat.id); void baristaClient.event("chat_created", { project_id: selectedProject.id, chat_id: chat.id }); } catch (cause) { setError(userFacingError(cause)); } finally { setPending(false); }
  }
  async function renameProject() {
    if (!renameTarget || pending) return; const title = renameTitle.trim();
    if (!title || [...title].length > 100) { setError("Название проекта должно содержать от 1 до 100 символов."); return; }
    setPending(true); setError(null);
    try { const saved = await baristaClient.renameProject(renameTarget.id, title); setData(current => ({ ...current, projects: current.projects.map(project => project.id === saved.id ? saved : project) })); setRenameTarget(null); }
    catch (cause) { setError(userFacingError(cause)); } finally { setPending(false); }
  }
  async function confirm() {
    if (!confirmation || pending) return; setPending(true); setError(null);
    try {
      if (confirmation.kind === "project") { await baristaClient.removeProject(confirmation.project.id); setData(current => ({ ...current, projects: current.projects.filter(project => project.id !== confirmation.project.id) })); await load(); void baristaClient.event("project_deleted", { project_id: confirmation.project.id }); }
      if (confirmation.kind === "chat" && selectedProject) { await baristaClient.removeChat(selectedProject.id, confirmation.chat.id); await load(); void baristaClient.event("chat_deleted", { project_id: selectedProject.id, chat_id: confirmation.chat.id }); }
      if (confirmation.kind === "memory" && selectedProject) { if (confirmation.layer === "global") await baristaClient.clearGlobalMemory(selectedProject.id); else await baristaClient.clearProjectMemory(selectedProject.id); setMemory(await baristaClient.memory(selectedProject.id)); void baristaClient.event("memory_cleared", { project_id: selectedProject.id }); }
      setConfirmation(null);
    } catch (cause) { setError(userFacingError(cause)); } finally { setPending(false); }
  }
  async function submitMessage() {
    if (!selectedProject || !selectedChat || pending || !draft.trim()) return;
    const text = draft.trim(); const clientID = newID(); const optimistic: BaristaMessage = { id: `local-${clientID}`, client_message_id: clientID, role: "user", text, status: "pending", created_at: new Date().toISOString(), localOnly: true };
    setDraft(""); setPending(true); setError(null); replaceChat({ ...selectedChat, memory_status: "updating", messages: [...selectedChat.messages, optimistic] }); setMemory(current => ({ ...current, status: "updating", error_category: undefined }));
    try { const chat = await baristaClient.send(selectedProject.id, selectedChat.id, clientID, text); replaceChat(chat); setMemory(await baristaClient.memory(selectedProject.id)); void baristaClient.event("message_sent", { project_id: selectedProject.id, chat_id: selectedChat.id, message_id: clientID }); }
    catch (cause) {
      const category = cause instanceof BaristaAPIError ? cause.category : "network";
      // The backend may have persisted a failed user message with its canonical ID.
      // Prefer that state, so a later retry always targets the server-side message.
      try { replaceChat(await baristaClient.getChat(selectedProject.id, selectedChat.id)); }
      catch { replaceChat({ ...selectedChat, memory_status: "error", memory_error_category: category, messages: [...selectedChat.messages, { ...optimistic, status: "error", error_category: category }] }); }
      try { setMemory(await baristaClient.memory(selectedProject.id)); }
      catch { setMemory(current => ({ ...current, status: "error", error_category: category })); }
      setError(userFacingError(cause)); void baristaClient.event("message_failed", { project_id: selectedProject.id, chat_id: selectedChat.id, message_id: clientID, error_category: category });
    }
    finally { setPending(false); }
  }
  async function retry(message: BaristaMessage) {
    if (!selectedProject || !selectedChat || pending || message.localOnly) return;
    setPending(true); setError(null);
    try { const chat = await baristaClient.retry(selectedProject.id, selectedChat.id, message.id); replaceChat(chat); setMemory(await baristaClient.memory(selectedProject.id)); void baristaClient.event("message_retried", { project_id: selectedProject.id, chat_id: selectedChat.id, message_id: message.id }); }
    catch (cause) {
      try { replaceChat(await baristaClient.getChat(selectedProject.id, selectedChat.id)); } catch { /* Retain last safe view offline. */ }
      try { setMemory(await baristaClient.memory(selectedProject.id)); } catch { setMemory(current => ({ ...current, status: "error", error_category: cause instanceof BaristaAPIError ? cause.category : "network" })); }
      setError(userFacingError(cause));
    } finally { setPending(false); }
  }

  const status = pending || memory.status === "updating" ? "Обновляем память…" : memory.status === "success" ? "Память обновлена" : memory.status === "error" ? "Не удалось обновить память; ответ сохранён." : "Память готова";
  return <div className={styles.shell}>
    <button className={styles.menuButton} type="button" aria-expanded={sidebarOpen} aria-controls="project-sidebar" onClick={() => setSidebarOpen(value => !value)}>Проекты</button>
    <aside id="project-sidebar" className={`${styles.sidebar} ${sidebarOpen ? styles.sidebarOpen : ""}`} aria-label="Проекты и чаты">
      <button className={styles.newDialog} type="button" disabled={pending || !ready} onClick={() => void createProject()}>+ Новый проект</button>
      <nav className={styles.projectList}>{data.projects.map(project => <section key={project.id} className={styles.projectRow}><div className={styles.dialogMainRow}><button className={project.id === selectedProjectID ? styles.selectedDialog : styles.dialogButton} type="button" onClick={() => void selectProject(project)}>{project.title || "Новый проект"}</button><button className={styles.renameButton} type="button" aria-label={`Переименовать проект ${project.title}`} onClick={() => { setRenameTarget(project); setRenameTitle(project.title); }}>✎</button><button className={styles.deleteButton} type="button" aria-label={`Удалить проект ${project.title}`} onClick={() => setConfirmation({ kind: "project", project })}>×</button></div>{project.id === selectedProjectID && <div className={styles.chatList}>{project.chats.map(chat => <div className={styles.dialogMainRow} key={chat.id}><button className={chat.id === selectedChatID ? styles.selectedDialog : styles.dialogButton} type="button" onClick={() => void selectChat(chat)}>{chat.title || "Новый чат"}</button><button className={styles.deleteButton} type="button" aria-label={`Удалить чат ${chat.title}`} onClick={() => setConfirmation({ kind: "chat", chat })}>×</button></div>)}<button className={styles.addChat} type="button" disabled={pending} onClick={() => void createChat()}>+ Новый чат</button></div>}</section>)}</nav>
    </aside>
    <main className={styles.chat} aria-busy={!ready || pending}>{error && <p className={styles.error} role="alert">{error}</p>}<p className={styles.srOnly} role="status" aria-live="polite">{status}</p>
      {!ready ? <div className={styles.empty}>Загружаем проекты…</div> : !selectedProject ? <div className={styles.empty}><h2>Создайте проект</h2><p>В проекте можно вести несколько независимых чатов.</p><button type="button" disabled={pending} onClick={() => void createProject()}>Новый проект</button></div> : !selectedChat ? <div className={styles.empty}><h2>{selectedProject.title || "Новый проект"}</h2><p>В этом проекте пока нет чатов.</p><button type="button" disabled={pending} onClick={() => void createChat()}>Создать чат</button></div> : <><header className={styles.chatHeader}><div><h1>{selectedChat.title}</h1><p>{selectedProject.title}</p>{selectedChat.title_status === "pending" && <p className={styles.titlePending} role="status">Обновляем название…</p>}</div><button className={styles.memoryButton} type="button" aria-expanded={memoryOpen} onClick={() => setMemoryOpen(value => !value)}>Память</button></header>
      {memoryOpen && <aside className={styles.memoryPanel} aria-label="Память"><MemorySection title="Общая память" facts={memory.global_facts} onClear={() => setConfirmation({ kind: "memory", layer: "global" })} disabled={pending}/><MemorySection title="Память проекта" facts={memory.project_facts} onClear={() => setConfirmation({ kind: "memory", layer: "project" })} disabled={pending}/><p role="status">{status}</p></aside>}
      <section className={styles.messages} aria-label="Переписка" aria-live="polite">{selectedChat.messages.length === 0 && <div className={styles.empty}><p>Спросите о зёрнах, помоле или рецепте.</p></div>}{selectedChat.messages.map(message => <article className={`${styles.bubble} ${message.role === "user" ? styles.userBubble : styles.assistantBubble}`} key={message.id}><div className={styles.messageActions}><span>{message.role === "user" ? "Вы" : "Бариста"}</span></div><CollapsibleMessage text={message.text}/>{message.status === "pending" && <p className={styles.status} role="status">Бариста готовит ответ и обновляет память…</p>}{message.status === "error" && <div className={styles.failed}><p>{messageError(message)}</p>{message.role === "user" && <button type="button" onClick={() => void retry(message)}>Повторить</button>}</div>}</article>)}</section>
      <form className={styles.composer} onSubmit={(event: FormEvent) => { event.preventDefault(); void submitMessage(); }}><label htmlFor="barista-message">Ваш вопрос</label><textarea id="barista-message" value={draft} onChange={event => setDraft(event.target.value)} onKeyDown={event => { if (event.nativeEvent.isComposing || event.key !== "Enter" || event.shiftKey) return; event.preventDefault(); if (draft.trim() && !pending) void submitMessage(); }} disabled={pending} rows={3}/><div><span role="status">{status}</span><button type="submit" disabled={pending || !draft.trim()}>Отправить</button></div></form></>}
    </main>
    {confirmation && <div className={styles.dialogOverlay} role="presentation"><section className={styles.confirm} role="dialog" aria-modal="true" aria-labelledby="confirm-title"><h2 id="confirm-title">Подтвердите действие</h2><p>{confirmation.kind === "project" ? "Будут удалены проект, все его чаты и память проекта." : confirmation.kind === "chat" ? "Чат будет удалён. Память проекта останется." : `Будет очищена ${confirmation.layer === "global" ? "общая" : "память проекта"}.`}</p><div><button type="button" onClick={() => setConfirmation(null)}>Отмена</button><button className={styles.danger} type="button" onClick={() => void confirm()} disabled={pending}>{confirmation.kind === "memory" ? "Очистить" : "Удалить"}</button></div></section></div>}
    {renameTarget && <div className={styles.dialogOverlay} role="presentation"><form className={styles.confirm} role="dialog" aria-modal="true" aria-labelledby="rename-title" onSubmit={event => { event.preventDefault(); void renameProject(); }}><h2 id="rename-title">Переименовать проект</h2><label htmlFor="project-title">Название проекта</label><input id="project-title" maxLength={100} value={renameTitle} onChange={event => setRenameTitle(event.target.value)} autoFocus /><div><button type="button" onClick={() => setRenameTarget(null)}>Отмена</button><button type="submit" disabled={pending}>Сохранить</button></div></form></div>}
  </div>;
}

function MemorySection({ title, facts, onClear, disabled }: { title: string; facts: string[]; onClear: () => void; disabled: boolean }) { return <section className={styles.memorySection}><h2>{title}</h2>{facts.length ? <ul>{facts.map((fact, index) => <li key={`${fact}-${index}`}>{fact}</li>)}</ul> : <p>Фактов пока нет.</p>}<button type="button" onClick={onClear} disabled={disabled}>Очистить</button></section>; }
