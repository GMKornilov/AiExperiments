"use client";

import { FormEvent, useEffect, useId, useMemo, useRef, useState } from "react";
import { CollapsibleMessage } from "./collapsible-message";
import { ProfileManager } from "./profile-manager";
import { TaskStatePanel, taskStatusSummary } from "./task-state-panel";
import { BaristaAPIError, baristaClient, userFacingError } from "../lib/chat-client";
import type { BaristaMessage, Chat, Invariant, Memory, ProfileList, Project, ProjectList, Task, TaskCandidate } from "../model/types";
import styles from "./barista-workspace.module.css";

type Confirmation = { kind: "project"; project: Project } | { kind: "chat"; chat: Chat } | { kind: "memory"; layer: "global" | "project" };
type TaskInputAttempt = { text: string; cancellationRequested: boolean; taskID?: string };
const emptyMemory: Memory = { global_facts: [], project_facts: [], status: "idle" };
const messageError = (message: BaristaMessage) => message.status === "error" ? userFacingError(new BaristaAPIError(message.error_category ?? "network")) : null;
const isInvariantValidationError = (cause: unknown) => cause instanceof BaristaAPIError && cause.category === "invariant_validation";

export function BaristaWorkspace() {
  const [data, setData] = useState<ProjectList>({ projects: [] });
  const [selectedProjectID, setSelectedProjectID] = useState<string | null>(null);
  const [selectedChatID, setSelectedChatID] = useState<string | null>(null);
  const [memory, setMemory] = useState<Memory>(emptyMemory);
  const [profiles, setProfiles] = useState<ProfileList | null>(null);
  const [profilesLoading, setProfilesLoading] = useState(false);
  const [profilesError, setProfilesError] = useState<string | null>(null);
  const [profilesOpen, setProfilesOpen] = useState(false);
  const [ready, setReady] = useState(false); const [pending, setPending] = useState(false);
  const [memoryOpen, setMemoryOpen] = useState(false); const [sidebarOpen, setSidebarOpen] = useState(false);
  const [invariantsOpen, setInvariantsOpen] = useState(false);
  const [invariants, setInvariants] = useState<Invariant[]>([]);
  const [invariantsLoading, setInvariantsLoading] = useState(false);
  const [invariantsError, setInvariantsError] = useState<string | null>(null);
  const [tasksOpen, setTasksOpen] = useState(false);
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null);
  const [renameTarget, setRenameTarget] = useState<Project | null>(null); const [renameTitle, setRenameTitle] = useState("");
  const [draft, setDraft] = useState(""); const [error, setError] = useState<string | null>(null);
  const [taskCandidates, setTaskCandidates] = useState<TaskCandidate[]>([]);
  const [candidateInput, setCandidateInput] = useState("");
  const candidateInputID = useRef<string | undefined>(undefined);
  const resumeOperationIDs = useRef(new Map<string, string>());
  const [resumeRetry, setResumeRetry] = useState<Task | null>(null);
  const [pausingTaskID, setPausingTaskID] = useState<string | null>(null);
  const [copyStatus, setCopyStatus] = useState<string | null>(null);
  const taskRevision = useRef(0);
  const taskInputAttempts = useRef(new Map<string, TaskInputAttempt>());
  const activeTaskInputAttemptID = useRef<string | null>(null);
  const taskDetailsID = useId();

  const selectedProject = useMemo(() => data.projects.find((project) => project.id === selectedProjectID) ?? null, [data, selectedProjectID]);
  const selectedChat = useMemo(() => selectedProject?.chats.find((chat) => chat.id === selectedChatID) ?? null, [selectedProject, selectedChatID]);
  const activeTask = useMemo(() => selectedChat?.tasks?.find(task => task.status === "active") ?? null, [selectedChat]);
  const pausedTask = useMemo(() => selectedChat?.tasks?.find(task => task.status === "paused") ?? null, [selectedChat]);
  const replaceChat = (chat: Chat, preservePendingTaskInputs = false) => setData(current => ({ ...current, projects: current.projects.map(project => project.id === chat.project_id ? { ...project, chats: project.chats.map(old => {
    if (old.id !== chat.id || !preservePendingTaskInputs) return old.id === chat.id ? chat : old;
    const pendingInputs = old.messages.filter(message => message.localOnly && taskInputAttempts.current.has(message.id) && !chat.messages.some(saved => saved.client_message_id === message.id));
    return { ...chat, messages: [...chat.messages, ...pendingInputs] };
  }), selected_chat_id: chat.id } : project) }));
  const appendPendingTaskInput = (projectID: string, chatID: string, message: BaristaMessage) => setData(current => ({ ...current, projects: current.projects.map(project => project.id !== projectID ? project : { ...project, chats: project.chats.map(chat => chat.id !== chatID ? chat : { ...chat, messages: [...chat.messages, message] }) }) }));
  const updateLocalTaskInput = (projectID: string, chatID: string, messageID: string, status: BaristaMessage["status"], errorCategory?: BaristaMessage["error_category"]) => setData(current => ({ ...current, projects: current.projects.map(project => project.id !== projectID ? project : { ...project, chats: project.chats.map(chat => chat.id !== chatID ? chat : { ...chat, messages: chat.messages.map(message => message.id !== messageID ? message : { ...message, status, error_category: errorCategory }) }) }) }));
  const cancellationRequested = (messageID: string) => taskInputAttempts.current.get(messageID)?.cancellationRequested === true;
  const clearCancelledTaskInputAttempts = () => {
    for (const [messageID, attempt] of taskInputAttempts.current) {
      if (!attempt.cancellationRequested) continue;
      taskInputAttempts.current.delete(messageID);
      if (activeTaskInputAttemptID.current === messageID) activeTaskInputAttemptID.current = null;
    }
  };

  async function load() {
    const next = await baristaClient.projects(); setData(next);
    const projectID = next.selected_project_id ?? next.projects[0]?.id ?? null;
    setSelectedProjectID(projectID);
    const project = next.projects.find(item => item.id === projectID);
    const chatID = next.selected_chat_id ?? project?.selected_chat_id ?? project?.chats[0]?.id ?? null;
    setSelectedChatID(chatID);
    if (projectID) setMemory(await baristaClient.memory(projectID)); else setMemory(emptyMemory);
  }
  async function loadProfiles() {
    setProfilesLoading(true); setProfilesError(null);
    try { setProfiles(await baristaClient.profiles()); }
    catch (cause) { setProfilesError(userFacingError(cause)); }
    finally { setProfilesLoading(false); }
  }
  async function loadInvariants() {
    setInvariantsLoading(true); setInvariantsError(null);
    try { setInvariants((await baristaClient.invariants()).invariants); }
    catch { setInvariantsError("Не удалось загрузить инварианты."); }
    finally { setInvariantsLoading(false); }
  }
  function toggleInvariants() {
    setInvariantsOpen(open => {
      const next = !open;
      if (next && !invariantsLoading) void loadInvariants();
      return next;
    });
  }
  useEffect(() => {
    // Establish the HttpOnly session cookie before another first-load request
    // can create a competing session and replace ownership in the browser.
    void Promise.resolve().then(load).catch(cause => setError(userFacingError(cause))).then(loadProfiles).finally(() => setReady(true));
  }, []);
  useEffect(() => {
    if (!selectedProjectID || !selectedChatID || selectedChat?.title_status !== "pending") return;
    let cancelled = false; let attempts = 0; let timer: ReturnType<typeof setTimeout> | undefined;
    const poll = async () => {
      try { const fresh = await baristaClient.getChat(selectedProjectID, selectedChatID); if (cancelled) return; replaceChat(fresh, true); if (fresh.title_status === "pending" && ++attempts < 8) timer = setTimeout(() => { void poll(); }, Math.min(500 * (attempts + 1), 2_000)); }
      catch { /* Title generation is optional. */ }
    };
    timer = setTimeout(() => { void poll(); }, 400);
    return () => { cancelled = true; if (timer) clearTimeout(timer); };
  }, [selectedProjectID, selectedChatID, selectedChat?.title_status]);

  useEffect(() => {
    if (!pending || !selectedProjectID || !selectedChatID) return;
    let cancelled = false;
    const timer = setInterval(() => {
      void baristaClient.getChat(selectedProjectID, selectedChatID).then(chat => {
        if (!cancelled) setData(current => ({ ...current, projects: current.projects.map(project => project.id !== chat.project_id ? project : { ...project, chats: project.chats.map(old => old.id !== chat.id ? old : { ...old, title: chat.title, title_status: chat.title_status, tasks: chat.memory_status === "updating" ? chat.tasks : old.tasks }) }) }));
      }).catch(() => { /* The input request owns the error outcome. */ });
    }, 300);
    return () => { cancelled = true; clearInterval(timer); };
  }, [pending, selectedProjectID, selectedChatID]);

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
    const text = draft.trim();
    const messageID = `local-task-input-${crypto.randomUUID()}`;
    const pendingMessage: BaristaMessage = { id: messageID, client_message_id: messageID, role: "user", text, status: "pending", created_at: new Date().toISOString(), localOnly: true };
    setDraft(""); setPending(true); setError(null); setTaskCandidates([]); setCandidateInput(text);
    taskRevision.current += 1;
    const revision = taskRevision.current;
    candidateInputID.current = messageID;
    taskInputAttempts.current.set(messageID, { text, cancellationRequested: false });
    activeTaskInputAttemptID.current = messageID;
    appendPendingTaskInput(selectedProject.id, selectedChat.id, pendingMessage);
    try {
      const result = await baristaClient.taskInput(selectedProject.id, selectedChat.id, text, undefined, messageID);
      // Pause owns the task snapshot. A late response belongs to the cancelled run
      // and must not replace that snapshot.
      if (cancellationRequested(messageID) || revision !== taskRevision.current) return;
      taskInputAttempts.current.delete(messageID);
      if (activeTaskInputAttemptID.current === messageID) activeTaskInputAttemptID.current = null;
      clearCancelledTaskInputAttempts(); replaceChat(result.chat); setTaskCandidates(result.candidates ?? []);
      setMemory(await baristaClient.memory(selectedProject.id));
    }
    catch (cause) {
      // Cancelling an in-flight LLM request is an expected outcome, not a failed
      // user message. Keep its optimistic bubble neutral until Resume takes over.
      if (cancellationRequested(messageID) || revision !== taskRevision.current) return;
      const category = cause instanceof BaristaAPIError ? cause.category : "network";
      updateLocalTaskInput(selectedProject.id, selectedChat.id, messageID, "error", category);
      if (activeTaskInputAttemptID.current === messageID) activeTaskInputAttemptID.current = null;
      try { setMemory(await baristaClient.memory(selectedProject.id)); }
      catch { setMemory(current => ({ ...current, status: "error", error_category: category })); }
      if (!isInvariantValidationError(cause)) setError(userFacingError(cause));
    }
    finally {
      if (cancellationRequested(messageID) || revision !== taskRevision.current) return;
      setPending(false);
    }
  }
  async function selectTaskCandidate(candidate: TaskCandidate) {
    if (!selectedProject || !selectedChat || pending || !candidateInput) return;
    setPending(true); setError(null);
    try { const result = await baristaClient.taskInput(selectedProject.id, selectedChat.id, candidateInput, candidate.id, candidateInputID.current); replaceChat(result.chat); setTaskCandidates(result.candidates ?? []); setCandidateInput(""); setMemory(await baristaClient.memory(selectedProject.id)); }
    catch (cause) { setError(userFacingError(cause)); }
    finally { setPending(false); }
  }
  async function pauseTask(task: Task) {
    if (!selectedProject || !selectedChat || pausingTaskID) return;
    const attemptID = activeTaskInputAttemptID.current;
    const attempt = attemptID ? taskInputAttempts.current.get(attemptID) : undefined;
    // This must happen before the Pause request starts: its input request can
    // reject while Pause is still waiting for the backend.
    if (attempt && (!attempt.taskID || attempt.taskID === task.id)) attempt.cancellationRequested = true;
    taskRevision.current += 1; setPausingTaskID(task.id); setError(null);
    try {
      const result = await baristaClient.pauseTask(selectedProject.id, selectedChat.id, task.id);
      replaceChat(result.chat, true);
      if (attemptID) setData(current => ({ ...current, projects: current.projects.map(project => ({ ...project, chats: project.chats.map(chat => ({ ...chat, messages: chat.messages.map(message => message.id === attemptID ? { ...message, paused: true } : message) })) })) }));
      setTaskCandidates(result.candidates ?? []);
      // The backend accepted cancellation, so the composer must leave its busy
      // state immediately even while the aborted request is still unwinding.
      setPending(false);
    }
    catch (cause) {
      if (attemptID && taskInputAttempts.current.get(attemptID)?.cancellationRequested) {
        taskInputAttempts.current.get(attemptID)!.cancellationRequested = false;
        updateLocalTaskInput(selectedProject.id, selectedChat.id, attemptID, "error", cause instanceof BaristaAPIError ? cause.category : "network");
      }
      setPending(false); setError(userFacingError(cause));
    }
    finally { setPausingTaskID(null); }
  }
  async function resumeTask(task: Task) {
    if (!selectedProject || !selectedChat || pending) return;
    const clientID = resumeOperationIDs.current.get(task.id) ?? crypto.randomUUID();
    resumeOperationIDs.current.set(task.id, clientID);
    taskRevision.current += 1; setPending(true); setError(null); setResumeRetry(null);
    try { const result = await baristaClient.resumeTask(selectedProject.id, selectedChat.id, task.id, "Продолжить сохранённый шаг.", clientID); resumeOperationIDs.current.delete(task.id); clearCancelledTaskInputAttempts(); replaceChat(result.chat); setTaskCandidates(result.candidates ?? []); setMemory(await baristaClient.memory(selectedProject.id)); }
    catch (cause) { setResumeRetry(task); setError(userFacingError(cause)); }
    finally { setPending(false); }
  }
  async function retry(message: BaristaMessage) {
    if (!selectedProject || !selectedChat || pending) return;
    const taskInput = taskInputAttempts.current.get(message.id);
    if (taskInput) {
      setPending(true); setError(null); updateLocalTaskInput(selectedProject.id, selectedChat.id, message.id, "pending");
      try {
        const result = await baristaClient.taskInput(selectedProject.id, selectedChat.id, taskInput.text, taskInput.taskID, message.id);
        taskInputAttempts.current.delete(message.id); replaceChat(result.chat); setTaskCandidates(result.candidates ?? []); setCandidateInput(taskInput.text); setMemory(await baristaClient.memory(selectedProject.id));
      }
      catch (cause) {
        updateLocalTaskInput(selectedProject.id, selectedChat.id, message.id, "error", cause instanceof BaristaAPIError ? cause.category : "network"); if (!isInvariantValidationError(cause)) setError(userFacingError(cause));
      }
      finally { setPending(false); }
      return;
    }
    if (message.localOnly) return;
    setPending(true); setError(null);
    try { const chat = await baristaClient.retry(selectedProject.id, selectedChat.id, message.id); replaceChat(chat); setMemory(await baristaClient.memory(selectedProject.id)); void baristaClient.event("message_retried", { project_id: selectedProject.id, chat_id: selectedChat.id, message_id: message.id }); }
    catch (cause) {
      try { replaceChat(await baristaClient.getChat(selectedProject.id, selectedChat.id)); } catch { /* Retain last safe view offline. */ }
      try { setMemory(await baristaClient.memory(selectedProject.id)); } catch { setMemory(current => ({ ...current, status: "error", error_category: cause instanceof BaristaAPIError ? cause.category : "network" })); }
      setError(userFacingError(cause));
    } finally { setPending(false); }
  }

  async function copyChatID() {
    if (!selectedChat) return;
    try {
      await navigator.clipboard.writeText(selectedChat.id);
      setCopyStatus("ID чата скопирован.");
    } catch {
      setCopyStatus("Не удалось скопировать ID чата.");
    }
  }

  const status = pending || memory.status === "updating" ? "Обновляем память…" : memory.status === "success" ? "Память обновлена" : memory.status === "error" ? "Не удалось обновить память; ответ сохранён." : "Память готова";
  return <div className={styles.shell}>
    <button className={styles.menuButton} type="button" aria-expanded={sidebarOpen} aria-controls="project-sidebar" onClick={() => setSidebarOpen(value => !value)}>Проекты</button>
    <aside id="project-sidebar" className={`${styles.sidebar} ${sidebarOpen ? styles.sidebarOpen : ""}`} aria-label="Проекты и чаты">
      <button className={styles.profilesButton} type="button" disabled={!ready} onClick={() => setProfilesOpen(true)}>Профили</button>
      <button className={styles.newDialog} type="button" disabled={pending || !ready} onClick={() => void createProject()}>+ Новый проект</button>
      <nav className={styles.projectList}>{data.projects.map(project => <section key={project.id} className={styles.projectRow}><div className={styles.dialogMainRow}><button className={project.id === selectedProjectID ? styles.selectedDialog : styles.dialogButton} type="button" onClick={() => void selectProject(project)}>{project.title || "Новый проект"}</button><button className={styles.renameButton} type="button" aria-label={`Переименовать проект ${project.title}`} onClick={() => { setRenameTarget(project); setRenameTitle(project.title); }}>✎</button><button className={styles.deleteButton} type="button" aria-label={`Удалить проект ${project.title}`} onClick={() => setConfirmation({ kind: "project", project })}>×</button></div>{project.id === selectedProjectID && <div className={styles.chatList}>{project.chats.map(chat => <div className={styles.dialogMainRow} key={chat.id}><button className={chat.id === selectedChatID ? styles.selectedDialog : styles.dialogButton} type="button" onClick={() => void selectChat(chat)}>{chat.title || "Новый чат"}</button><button className={styles.deleteButton} type="button" aria-label={`Удалить чат ${chat.title}`} onClick={() => setConfirmation({ kind: "chat", chat })}>×</button></div>)}<button className={styles.addChat} type="button" disabled={pending} onClick={() => void createChat()}>+ Новый чат</button></div>}</section>)}</nav>
    </aside>
    <main className={styles.chat} aria-busy={!ready || pending}>{error && <p className={styles.error} role="alert">{error}</p>}{resumeRetry && <button type="button" disabled={pending} onClick={() => void resumeTask(resumeRetry)}>Повторить продолжение</button>}<p className={styles.srOnly} role="status" aria-live="polite">{status}</p>
      {!ready ? <div className={styles.empty}>Загружаем проекты…</div> : !selectedProject ? <div className={styles.empty}><h2>Создайте проект</h2><p>В проекте можно вести несколько независимых чатов.</p><button type="button" disabled={pending} onClick={() => void createProject()}>Новый проект</button></div> : !selectedChat ? <div className={styles.empty}><h2>{selectedProject.title || "Новый проект"}</h2><p>В этом проекте пока нет чатов.</p><button type="button" disabled={pending} onClick={() => void createChat()}>Создать чат</button></div> : <><header className={styles.chatHeader}><div><h1>{selectedChat.title}</h1><p>{selectedProject.title}</p>{selectedChat.title_status === "pending" && <p className={styles.titlePending} role="status">Обновляем название…</p>}</div><div className={styles.headerActions}><button className={`${styles.memoryButton} ${styles.taskButton}`} type="button" aria-expanded={tasksOpen || taskCandidates.length > 0} aria-controls={taskDetailsID} onClick={() => setTasksOpen(value => !value)}><span>Задачи</span><span className={styles.taskSummary}>{taskStatusSummary(selectedChat.tasks ?? [])}</span><span aria-hidden="true">{tasksOpen || taskCandidates.length > 0 ? "⌃" : "⌄"}</span></button><button className={styles.memoryButton} type="button" aria-expanded={memoryOpen} aria-controls="memory-panel" onClick={() => setMemoryOpen(value => !value)}>Память</button><button className={styles.memoryButton} type="button" aria-expanded={invariantsOpen} aria-controls="invariants-panel" onClick={toggleInvariants}>Инварианты</button></div></header>
      {memoryOpen && <aside id="memory-panel" className={styles.memoryPanel} aria-label="Память"><MemorySection title="Общая память" facts={memory.global_facts} onClear={() => setConfirmation({ kind: "memory", layer: "global" })} disabled={pending}/><MemorySection title="Память проекта" facts={memory.project_facts} onClear={() => setConfirmation({ kind: "memory", layer: "project" })} disabled={pending}/><p role="status">{status}</p></aside>}
      {invariantsOpen && <InvariantPanel invariants={invariants} loading={invariantsLoading} error={invariantsError} onRetry={() => void loadInvariants()} />}
      <TaskStatePanel tasks={selectedChat.tasks ?? []} candidates={taskCandidates} pending={pending} onCandidate={candidate => void selectTaskCandidate(candidate)} detailsID={taskDetailsID} open={tasksOpen} />
      <section className={styles.messages} aria-label="Переписка" aria-live="polite">{selectedChat.messages.length === 0 && <div className={styles.empty}><p>Спросите о зёрнах, помоле или рецепте.</p></div>}{selectedChat.messages.map(message => <article className={`${styles.bubble} ${message.role === "user" ? styles.userBubble : styles.assistantBubble}`} key={message.id}><div className={styles.messageActions}><span>{message.role === "user" ? "Вы" : "Бариста"}</span></div><CollapsibleMessage text={message.text}/>{message.status === "pending" && <p className={styles.status} role="status">{message.paused ? "Задача на паузе" : "Бариста готовит ответ и обновляет память…"}</p>}{message.status === "error" && <div className={styles.failed}><p>{messageError(message)}</p>{message.role === "user" && <button type="button" onClick={() => void retry(message)}>Повторить</button>}</div>}</article>)}</section>
      <form className={styles.composer} onSubmit={(event: FormEvent) => { event.preventDefault(); if (pending && activeTask) void pauseTask(activeTask); else if (!pending && pausedTask && !draft.trim()) void resumeTask(pausedTask); else void submitMessage(); }}><section className={styles.contextStatus} aria-label="Статус диалога"><div className={styles.threadID}><span>ID чата</span><code>ID: {selectedChat.id}</code><button type="button" aria-label="Копировать ID чата" onClick={() => void copyChatID()}>Копировать</button></div>{copyStatus && <p className={styles.copyStatus} role="status" aria-live="polite">{copyStatus}</p>}</section><label htmlFor="barista-message">Ваш вопрос</label><textarea id="barista-message" value={draft} onChange={event => setDraft(event.target.value)} onKeyDown={event => { if (event.nativeEvent.isComposing || event.key !== "Enter" || event.shiftKey) return; event.preventDefault(); if (draft.trim() && !pending) void submitMessage(); }} disabled={pending} rows={3}/><div><span role="status">{status}</span>{pending && activeTask ? <button className={styles.composerStop} type="submit" disabled={pausingTaskID === activeTask.id}>Остановить</button> : !pending && pausedTask && !draft.trim() ? <button type="submit">Продолжить</button> : <button type="submit" disabled={pending || !draft.trim()}>Отправить</button>}</div></form></>}
    </main>
    {confirmation && <div className={styles.dialogOverlay} role="presentation"><section className={styles.confirm} role="dialog" aria-modal="true" aria-labelledby="confirm-title"><h2 id="confirm-title">Подтвердите действие</h2><p>{confirmation.kind === "project" ? "Будут удалены проект, все его чаты и память проекта." : confirmation.kind === "chat" ? "Чат будет удалён. Память проекта останется." : `Будет очищена ${confirmation.layer === "global" ? "общая" : "память проекта"}.`}</p><div><button type="button" onClick={() => setConfirmation(null)}>Отмена</button><button className={styles.danger} type="button" onClick={() => void confirm()} disabled={pending}>{confirmation.kind === "memory" ? "Очистить" : "Удалить"}</button></div></section></div>}
    {renameTarget && <div className={styles.dialogOverlay} role="presentation"><form className={styles.confirm} role="dialog" aria-modal="true" aria-labelledby="rename-title" onSubmit={event => { event.preventDefault(); void renameProject(); }}><h2 id="rename-title">Переименовать проект</h2><label htmlFor="project-title">Название проекта</label><input id="project-title" maxLength={100} value={renameTitle} onChange={event => setRenameTitle(event.target.value)} autoFocus /><div><button type="button" onClick={() => setRenameTarget(null)}>Отмена</button><button type="submit" disabled={pending}>Сохранить</button></div></form></div>}
    {profilesOpen && <ProfileManager initial={profiles} loading={profilesLoading} error={profilesError} onChange={setProfiles} onClose={() => setProfilesOpen(false)} onRetry={() => void loadProfiles()} />}
  </div>;
}

function MemorySection({ title, facts, onClear, disabled }: { title: string; facts: string[]; onClear: () => void; disabled: boolean }) { return <section className={styles.memorySection}><h2>{title}</h2>{facts.length ? <ul>{facts.map((fact, index) => <li key={`${fact}-${index}`}>{fact}</li>)}</ul> : <p>Фактов пока нет.</p>}<button type="button" onClick={onClear} disabled={disabled}>Очистить</button></section>; }

function InvariantPanel({ invariants, loading, error, onRetry }: { invariants: Invariant[]; loading: boolean; error: string | null; onRetry: () => void }) {
  return <aside id="invariants-panel" className={styles.invariantsPanel} aria-label="Инварианты" aria-busy={loading}>
    <h2>Инварианты</h2>
    {loading && <p role="status">Загружаем правила…</p>}
    {error && <div className={styles.panelError} role="alert"><p>{error}</p><button type="button" onClick={onRetry}>Повторить</button></div>}
    {!loading && !error && <ul>{invariants.map(invariant => <li key={invariant.id}><h3>{invariant.name}</h3><p>{invariant.description}</p></li>)}</ul>}
  </aside>;
}
