import type { APIError, AdminLogsResponse, Chat, ErrorCategory, InvariantList, LogEvent, MCPAdminLogsResponse, Memory, ProfileList, Project, ProjectList, TaskInputResult } from "../model/types";

const genericError = "Не удалось получить ответ. Повторите отправку.";
export class BaristaAPIError extends Error { constructor(readonly category: ErrorCategory, message = genericError) { super(message); } }

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try { response = await fetch(path, { cache: "no-store", ...init }); } catch { throw new BaristaAPIError("network"); }
  if (response.status === 204) return undefined as T;
  const payload = await response.json().catch(() => null) as T | { error?: APIError } | null;
  if (!response.ok) { const error = payload && typeof payload === "object" && "error" in payload ? payload.error : undefined; throw new BaristaAPIError(error?.category ?? "network", error?.message || genericError); }
  if (!payload) throw new BaristaAPIError("invalid_response");
  return payload as T;
}
const json = (body: unknown): RequestInit => ({ method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
const projectPath = (projectID: string) => `/api/projects/${encodeURIComponent(projectID)}`;
const chatPath = (projectID: string, chatID: string) => `${projectPath(projectID)}/chats/${encodeURIComponent(chatID)}`;

export const baristaClient = {
  projects: () => request<ProjectList>("/api/projects"),
  invariants: () => request<InvariantList>("/api/invariants"),
  profiles: () => request<ProfileList>("/api/profiles"),
  createProfile: (profile: { name: string; style: string; constraints: string; additional_context: string }) => request<ProfileList>("/api/profiles", json(profile)),
  selectProfile: (id: string) => request<ProfileList>(`/api/profiles/${encodeURIComponent(id)}/select`, json({})),
  removeProfile: (id: string) => request<void>(`/api/profiles/${encodeURIComponent(id)}`, { method: "DELETE" }),
  createProject: (title?: string) => request<ProjectList | Project>("/api/projects", json(title ? { title } : {})),
  selectProject: (id: string) => request<unknown>(`${projectPath(id)}/select`, { method: "POST" }),
  renameProject: (id: string, title: string) => request<Project>(projectPath(id), { method: "PATCH", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ title }) }),
  removeProject: (id: string) => request<unknown>(projectPath(id), { method: "DELETE" }),
  createChat: (projectID: string) => request<Chat>(`${projectPath(projectID)}/chats`, json({})),
  getChat: (projectID: string, chatID: string) => request<Chat>(chatPath(projectID, chatID)),
  selectChat: (projectID: string, chatID: string) => request<unknown>(`${chatPath(projectID, chatID)}/select`, { method: "POST" }),
  removeChat: (projectID: string, chatID: string) => request<unknown>(chatPath(projectID, chatID), { method: "DELETE" }),
  send: (projectID: string, chatID: string, clientMessageID: string, text: string) => request<Chat>(`${chatPath(projectID, chatID)}/messages`, json({ client_message_id: clientMessageID, text })),
  taskInput: (projectID: string, chatID: string, text: string, candidateTaskID?: string, clientMessageID?: string) => request<TaskInputResult>(`${chatPath(projectID, chatID)}/tasks/input`, json({ text, ...(clientMessageID ? { client_message_id: clientMessageID } : {}), ...(candidateTaskID ? { candidate_task_id: candidateTaskID } : {}) })),
  pauseTask: (projectID: string, chatID: string, taskID: string) => request<TaskInputResult>(`${chatPath(projectID, chatID)}/tasks/${encodeURIComponent(taskID)}/pause`, json({})),
  resumeTask: (projectID: string, chatID: string, taskID: string, text: string, clientMessageID?: string) => request<TaskInputResult>(`${chatPath(projectID, chatID)}/tasks/${encodeURIComponent(taskID)}/resume`, json({ text: text.trim(), ...(clientMessageID ? { client_message_id: clientMessageID } : {}) })),
  retry: (projectID: string, chatID: string, messageID: string) => request<Chat>(`${chatPath(projectID, chatID)}/messages/${encodeURIComponent(messageID)}/retry`, { method: "POST" }),
  memory: (projectID: string) => request<Memory>(`${projectPath(projectID)}/memory`),
  clearGlobalMemory: (projectID: string) => request<void>(`${projectPath(projectID)}/memory/global`, { method: "DELETE" }),
  clearProjectMemory: (projectID: string) => request<void>(`${projectPath(projectID)}/memory/project`, { method: "DELETE" }),
  // dialog_id remains the compatibility wire parameter; callers deal in chat IDs.
  logs: (chatID: string, action: "lookup" | "refresh" | "poll") => request<AdminLogsResponse>(`/api/admin/logs?dialog_id=${encodeURIComponent(chatID)}&action=${action}`),
  mcpLogs: () => request<MCPAdminLogsResponse>("/api/admin/logs?scope=mcp"),
  event: async (event: LogEvent, fields: { project_id?: string; chat_id?: string; message_id?: string; error_category?: ErrorCategory } = {}) => { try { await request<unknown>("/api/events", json({ event, ...fields })); } catch { /* Telemetry never blocks chat. */ } },
};

export function userFacingError(error: unknown) {
  if (error instanceof BaristaAPIError && error.category === "validation") return error.message;
  if (error instanceof BaristaAPIError && error.category === "storage") return "Хранилище временно недоступно. Повторите действие.";
  if (error instanceof BaristaAPIError && error.category === "invariant_validation") return "Не удалось проверить инварианты. Повторите тот же запрос.";
  return genericError;
}
