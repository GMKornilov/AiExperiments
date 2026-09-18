import "server-only";

const maxBodyBytes = 32 * 1024 * 1024;
// One summary followed by one chat completion (default 30s each).
const timeoutMilliseconds = 65_000;
const bodyTimeoutMilliseconds = 10_000;
const sessionCookie = "barista_session";
const errorCategories = new Set(["config", "validation", "network", "timeout", "provider", "invalid_response", "not_found", "busy", "cancelled", "storage", "context_limit", "memory"]);
const eventNames = new Set(["dialog_created", "dialog_selected", "dialog_deleted", "dialog_id_copied", "message_sent", "message_retried", "message_copied", "dialog_validation_failed", "message_failed", "admin_lookup", "admin_refresh", "strategy_selected", "branch_created", "branch_selected", "project_created", "project_selected", "project_deleted", "chat_created", "chat_selected", "chat_deleted", "memory_cleared", "bff_request_completed", "bff_request_failed"]);
const contextStrategies = new Set(["sliding_window", "facts", "branching", "summary"]);

type JSONRecord = Record<string, unknown>;

function backendURL(path: string): URL {
  const base = process.env.BARISTA_BACKEND_URL ?? "http://127.0.0.1:8080";
  const url = new URL(base.endsWith("/") ? base : `${base}/`);
  if (url.protocol !== "http:" && url.protocol !== "https:") throw new Error("invalid backend URL");
  return new URL(path.replace(/^\//, ""), url);
}

function cookieValue(request: Request): string | undefined {
  const raw = request.headers.get("cookie") ?? "";
  return raw.split(";").map((part) => part.trim()).find((part) => part.startsWith(`${sessionCookie}=`))?.slice(sessionCookie.length + 1);
}

function session(request: Request): { id: string; setCookie?: string } {
  const existing = cookieValue(request);
  if (existing && /^[a-f0-9-]{36}$/i.test(existing)) return { id: existing };
  const secure = new URL(request.url).protocol === "https:" || request.headers.get("x-forwarded-proto") === "https";
  const id = crypto.randomUUID();
  return { id, setCookie: `${sessionCookie}=${id}; Path=/; HttpOnly; SameSite=Lax; Max-Age=2592000${secure ? "; Secure" : ""}` };
}

function response(body: unknown, status: number, requestID: string, setCookie?: string): Response {
  const headers = new Headers({ "Cache-Control": "no-store", "Content-Type": "application/json; charset=utf-8", "X-Request-ID": requestID });
  if (setCookie) headers.set("Set-Cookie", setCookie);
  return Response.json(body, { status, headers });
}

function empty(status: number, requestID: string, setCookie?: string): Response {
  const headers = new Headers({ "Cache-Control": "no-store", "X-Request-ID": requestID });
  if (setCookie) headers.set("Set-Cookie", setCookie);
  return new Response(null, { status, headers });
}

function error(category: string, requestID: string, status = 400, setCookie?: string): Response {
  const safe = errorCategories.has(category) ? category : "provider";
  const messages: Record<string, string> = { validation: "Некорректный запрос.", not_found: "Данные не найдены.", busy: "Запрос уже выполняется.", config: "Сервис временно недоступен.", storage: "Хранилище истории временно недоступно.", context_limit: "Контекст диалога превышает лимит модели. Начните новый диалог." };
  return response({ error: { category: safe, message: messages[safe] ?? "Не удалось получить ответ. Повторите отправку." } }, status, requestID, setCookie);
}

function validationFailure(request: Request, requestID: string, dialogID?: string): Response {
  const browser = session(request);
  console.info(JSON.stringify({ timestamp: new Date().toISOString(), source: "frontend", event: "dialog_validation_failed", result: "failure", correlation_id: requestID, error_category: "validation" }));
  try {
    void Promise.resolve(fetch(backendURL("/api/events"), { method: "POST", cache: "no-store", headers: { "Content-Type": "application/json", "X-Session-ID": browser.id, "X-Request-ID": requestID }, body: JSON.stringify({ event: "dialog_validation_failed", ...(dialogID ? { dialog_id: dialogID } : {}), error_category: "validation" }), signal: AbortSignal.timeout(3_000) })).catch(() => undefined);
  } catch { /* Console record remains available if configuration is invalid. */ }
  return error("validation", requestID, 400, browser.setCookie);
}

function record(browser: { id: string }, requestID: string, event: "bff_request_completed" | "bff_request_failed", durationMS: number, category?: string, dialogID?: string): void {
  console.info(JSON.stringify({ timestamp: new Date().toISOString(), source: "frontend", event, result: event === "bff_request_completed" ? "success" : "failure", correlation_id: requestID, duration_ms: durationMS, ...(dialogID ? { dialog_id: dialogID } : {}), ...(category ? { error_category: category } : {}) }));
  try {
    void Promise.resolve(fetch(backendURL("/api/events"), { method: "POST", cache: "no-store", headers: { "Content-Type": "application/json", "X-Session-ID": browser.id, "X-Request-ID": requestID }, body: JSON.stringify({ event, ...(dialogID ? { dialog_id: dialogID } : {}), ...(category ? { error_category: category } : {}), duration_ms: durationMS }), signal: AbortSignal.timeout(3_000) })).catch(() => undefined);
  } catch { /* Console is the safe fallback when backend configuration is unavailable. */ }
}

async function readJSON(request: Request): Promise<JSONRecord | null> {
  if (!request.headers.get("content-type")?.toLowerCase().startsWith("application/json")) return null;
  const length = Number(request.headers.get("content-length"));
  if (Number.isFinite(length) && length > maxBodyBytes) return null;
  if (!request.body) return null;
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  const read = (async () => {
    for (;;) {
      const next = await reader.read();
      if (next.done) break;
      size += next.value.byteLength;
      if (size > maxBodyBytes) { await reader.cancel(); return null; }
      chunks.push(next.value);
    }
    const bytes = new Uint8Array(size); let offset = 0;
    for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
    return new TextDecoder().decode(bytes);
  })();
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    const raw = await Promise.race([read, new Promise<null>((resolve) => { timer = setTimeout(() => { void reader.cancel(); resolve(null); }, bodyTimeoutMilliseconds); })]);
    if (raw === null) return null;
    const value: unknown = JSON.parse(raw);
    return value && typeof value === "object" && !Array.isArray(value) ? value as JSONRecord : null;
  } catch { return null; } finally { if (timer) clearTimeout(timer); }
}

function only(value: JSONRecord, keys: string[]): boolean { return Object.keys(value).every((key) => keys.includes(key)); }
function text(value: unknown): string | null { return typeof value === "string" && value.trim() ? value : null; }
function string(value: unknown): string | null { return typeof value === "string" && value.trim() ? value : null; }

function projectError(value: unknown): { category: string; message: string } | null {
  if (!value || typeof value !== "object") return null;
  const item = (value as JSONRecord).error;
  if (!item || typeof item !== "object") return null;
  const category = (item as JSONRecord).category;
  const message = (item as JSONRecord).message;
  return typeof category === "string" && typeof message === "string" && errorCategories.has(category) ? { category, message } : null;
}

function tokenCount(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

function projectUsage(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const item = value as JSONRecord;
  if (!only(item, ["prompt_tokens", "completion_tokens"]) || !tokenCount(item.prompt_tokens) || !tokenCount(item.completion_tokens) || !Number.isSafeInteger(item.prompt_tokens + item.completion_tokens)) return null;
  return { prompt_tokens: item.prompt_tokens, completion_tokens: item.completion_tokens };
}

function projectMessage(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object") return null;
  const item = value as JSONRecord;
  if (!only(item, ["id", "client_message_id", "role", "text", "status", "created_at", "error_category", "usage", "attempts"]) || !string(item.id) || (item.role !== "user" && item.role !== "assistant") || typeof item.text !== "string" || !["pending", "success", "error"].includes(item.status as string) || typeof item.created_at !== "string") return null;
  if (item.client_message_id !== undefined && !string(item.client_message_id)) return null;
  if (item.error_category !== undefined && !errorCategories.has(item.error_category as string)) return null;
  // Attempt accounting stays on the backend; only public message data crosses the BFF.
  const projected: JSONRecord = { id: item.id, role: item.role, text: item.text, status: item.status, created_at: item.created_at };
  if (item.client_message_id !== undefined) projected.client_message_id = item.client_message_id;
  if (item.error_category !== undefined) projected.error_category = item.error_category;
  const usage = projectUsage(item.usage);
  if (item.role === "assistant" && usage) projected.usage = usage;
  return projected;
}

function projectFacts(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const facts: JSONRecord = {};
  for (const [key, fact] of Object.entries(value)) {
    if (!/^[a-z][a-z0-9_]*$/.test(key) || typeof fact !== "string" || !fact.trim()) return null;
    facts[key] = fact;
  }
  return facts;
}

function projectBranch(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const branch = value as JSONRecord;
  if (!only(branch, ["id", "name", "parent_branch_id", "checkpoint_message_id", "message_count"]) || !string(branch.id) || !string(branch.name) || !tokenCount(branch.message_count)) return null;
  if (branch.parent_branch_id !== undefined && !string(branch.parent_branch_id)) return null;
  if (branch.checkpoint_message_id !== undefined && !string(branch.checkpoint_message_id)) return null;
  return branch;
}

function projectStrategies(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const item = value as JSONRecord;
  if (!only(item, ["strategies"]) || !Array.isArray(item.strategies)) return null;
  const strategies: JSONRecord[] = [];
  for (const value of item.strategies) {
    if (!value || typeof value !== "object" || Array.isArray(value)) return null;
    const strategy = value as JSONRecord;
    if (!only(strategy, ["id", "available", "reason"]) || typeof strategy.id !== "string" || !contextStrategies.has(strategy.id) || typeof strategy.available !== "boolean" || (strategy.reason !== undefined && !string(strategy.reason))) return null;
    strategies.push(strategy);
  }
  return { strategies };
}

function projectDialog(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object") return null;
  const item = value as JSONRecord;
  if (!only(item, ["id", "title", "title_status", "created_at", "updated_at", "messages", "accounted_tokens", "compression", "context_strategy", "facts", "facts_tokens", "facts_usage_missing", "active_branch_id", "branches"]) || !string(item.id) || typeof item.title !== "string" || !["idle", "pending", "success", "error"].includes(item.title_status as string) || typeof item.created_at !== "string" || typeof item.updated_at !== "string" || !Array.isArray(item.messages) || !tokenCount(item.accounted_tokens)) return null;
  if (item.context_strategy !== undefined && (typeof item.context_strategy !== "string" || !contextStrategies.has(item.context_strategy))) return null;
  if (item.facts !== undefined && !projectFacts(item.facts)) return null;
  if (item.facts_tokens !== undefined && !tokenCount(item.facts_tokens)) return null;
  if (item.facts_usage_missing !== undefined && typeof item.facts_usage_missing !== "boolean") return null;
  if (item.active_branch_id !== undefined && !string(item.active_branch_id)) return null;
  if (item.branches !== undefined && (!Array.isArray(item.branches) || !item.branches.every(projectBranch))) return null;
  if (item.compression !== undefined) {
    const c = item.compression as JSONRecord;
    if (!c || typeof c !== "object" || Array.isArray(c) || !only(c, ["pruned_messages", "archived_tokens", "available", "enabled", "summary", "covered_messages", "summary_tokens", "summary_usage_missing", "full_estimate", "sent_estimate", "last_input_tokens", "context_window_tokens"]) || (c.pruned_messages !== undefined && !tokenCount(c.pruned_messages)) || (c.archived_tokens !== undefined && !tokenCount(c.archived_tokens)) || typeof c.available !== "boolean" || typeof c.enabled !== "boolean" || typeof c.summary !== "string" || typeof c.summary_usage_missing !== "boolean" || ![c.covered_messages, c.summary_tokens, c.full_estimate, c.sent_estimate, c.context_window_tokens].every(tokenCount) || (c.last_input_tokens !== null && !tokenCount(c.last_input_tokens))) return null;
  }
  const messages = item.messages.map(projectMessage);
  return messages.every(Boolean) ? { ...item, messages } : null;
}

function projectSuccess(method: string, path: string, value: unknown): unknown | null {
  if (path === "/api/profiles" && (method === "GET" || method === "POST")) return projectProfiles(value);
  if (/^\/api\/profiles\/[^/]+\/select$/.test(path) && method === "POST") return projectProfiles(value);
  if (path === "/api/projects" && method === "GET") return projectProjectList(value);
  if (path === "/api/projects" && method === "POST") return projectProjectList(value) ?? projectProject(value);
  if (path.startsWith("/api/projects/")) {
    if (path.endsWith("/memory")) return projectMemory(value);
    if (/\/chats\/[^/]+\/tasks\/input$/.test(path) && method === "POST") return projectTaskInput(value);
    if (/\/chats\/[^/]+\/tasks\/[^/]+\/(pause|resume)$/.test(path) && method === "POST") return projectTaskInput(value);
    if (path.includes("/chats/")) return projectChat(value);
    if (path.endsWith("/chats")) return projectChat(value);
    return projectProject(value);
  }
  if (path === "/api/context-strategies" && method === "GET") return projectStrategies(value);
  if (path === "/api/dialogs" && method === "GET") {
    if (!value || typeof value !== "object") return null;
    const item = value as JSONRecord;
    if (!only(item, ["dialogs", "selected_dialog_id"]) || !Array.isArray(item.dialogs) || (item.selected_dialog_id !== "" && typeof item.selected_dialog_id !== "string")) return null;
    const dialogs = item.dialogs.map(projectDialog);
    return dialogs.every(Boolean) ? { dialogs, selected_dialog_id: item.selected_dialog_id } : null;
  }
  if (path === "/api/dialogs" || path.startsWith("/api/dialogs/")) return projectDialog(value);
  // Admin is a diagnostics view. Its backend payload is intentionally opaque to
  // the BFF so newly added journal fields never make an existing chat invisible.
  if (path.startsWith("/api/admin/logs")) return value;
  return value;
}

function projectProfile(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const item = value as JSONRecord;
  if (!only(item, ["id", "name", "style", "constraints", "additional_context", "built_in"]) || !string(item.id) || !string(item.name) || typeof item.style !== "string" || typeof item.constraints !== "string" || typeof item.additional_context !== "string" || typeof item.built_in !== "boolean") return null;
  return item;
}

function projectProfiles(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const item = value as JSONRecord;
  if (!only(item, ["profiles", "active_profile_id"]) || !Array.isArray(item.profiles) || !string(item.active_profile_id)) return null;
  const profiles = item.profiles.map(projectProfile);
  if (!profiles.every((profile): profile is JSONRecord => profile !== null)) return null;
  if (!profiles.some((profile) => profile.id === item.active_profile_id)) return null;
  return { profiles, active_profile_id: item.active_profile_id };
}

function projectMemory(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const item = value as JSONRecord;
  if (!only(item, ["global_facts", "project_facts", "status", "error_category"]) || !Array.isArray(item.global_facts) || !Array.isArray(item.project_facts) || !["idle", "updating", "success", "error"].includes(item.status as string)) return null;
  if (!item.global_facts.every((fact) => typeof fact === "string" && fact.trim()) || !item.project_facts.every((fact) => typeof fact === "string" && fact.trim())) return null;
  if (item.error_category !== undefined && !errorCategories.has(item.error_category as string)) return null;
  return item;
}

function projectTaskPlanItem(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const item = value as JSONRecord;
  if (!only(item, ["id", "title", "status", "stage"]) || !string(item.id) || !string(item.title) || !["pending", "current", "completed"].includes(item.status as string)) return null;
  if (item.stage !== undefined && !["clarify_input", "research_input_data", "execution", "user_feedback"].includes(item.stage as string)) return null;
  return item;
}

function projectTask(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const task = value as JSONRecord;
  if (!only(task, ["id", "title", "description", "stage", "current_step", "expected_action", "status", "plan", "current_plan_item", "created_at", "updated_at"]) || !string(task.id) || !string(task.title) || !string(task.description) || !["clarify_input", "research_input_data", "execution", "user_feedback"].includes(task.stage as string) || !string(task.current_step) || typeof task.expected_action !== "string" || !["active", "paused", "done"].includes(task.status as string) || !Array.isArray(task.plan) || typeof task.created_at !== "string" || typeof task.updated_at !== "string") return null;
  const plan = task.plan.map(projectTaskPlanItem);
  if (!plan.every((item): item is JSONRecord => item !== null) || new Set(plan.map(item => item.id)).size !== plan.length) return null;
  if (task.current_plan_item !== undefined && !string(task.current_plan_item)) return null;
  const current = plan.filter(item => item.status === "current");
  if (plan.length === 0) return task.stage === "clarify_input" && task.current_plan_item === undefined ? task : null;
  if (task.status === "done") return current.length === 0 && task.current_plan_item === undefined && plan.every(item => item.status === "completed") ? task : null;
  // user_feedback can either wait for the user after the confirmed plan, or
  // execute a newly created feedback-revision item before asking again.
  if (task.stage === "user_feedback") {
    if (current.length === 0) return task.current_plan_item === undefined && plan.every(item => item.status === "completed") ? task : null;
    return current.length === 1 && task.current_plan_item === current[0].id ? task : null;
  }
  if (task.stage === "clarify_input" || current.length !== 1 || task.current_plan_item !== current[0].id) return null;
  return task;
}

function projectTaskCandidate(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const candidate = value as JSONRecord;
  if (!only(candidate, ["id", "title", "description"]) || !string(candidate.id) || !string(candidate.title) || !string(candidate.description)) return null;
  return candidate;
}

function projectTaskInput(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const item = value as JSONRecord;
  const chat = projectChat(item.chat);
  if (!only(item, ["chat", "candidates"]) || !chat) return null;
  // Go serializes an absent optional slice as null. It is equivalent to an
  // omitted candidates field; all other invalid shapes remain rejected.
  if (item.candidates === undefined || item.candidates === null) return { chat };
  if (!Array.isArray(item.candidates)) return null;
  const candidates = item.candidates.map(projectTaskCandidate);
  if (!candidates.every((candidate): candidate is JSONRecord => candidate !== null)) return null;
  return { chat, candidates };
}

function projectChat(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const item = value as JSONRecord;
  if (!only(item, ["id", "project_id", "title", "title_status", "created_at", "updated_at", "messages", "memory_status", "memory_error_category", "tasks"]) || !string(item.id) || !string(item.project_id) || typeof item.title !== "string" || !["idle", "pending", "success", "fallback"].includes(item.title_status as string) || typeof item.created_at !== "string" || typeof item.updated_at !== "string" || !Array.isArray(item.messages) || !["idle", "updating", "success", "error"].includes(item.memory_status as string)) return null;
  if (item.memory_error_category !== undefined && !errorCategories.has(item.memory_error_category as string)) return null;
  if (item.tasks !== undefined && (!Array.isArray(item.tasks) || !item.tasks.every(projectTask))) return null;
  const messages = item.messages.map(projectMessage);
  return messages.every(Boolean) ? { ...item, messages } : null;
}

function projectProject(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const item = value as JSONRecord;
  if (!only(item, ["id", "title", "created_at", "updated_at", "chats", "selected_chat_id"]) || !string(item.id) || typeof item.title !== "string" || typeof item.created_at !== "string" || typeof item.updated_at !== "string" || !Array.isArray(item.chats)) return null;
  if (item.selected_chat_id !== undefined && item.selected_chat_id !== null && !string(item.selected_chat_id)) return null;
  const chats = item.chats.map(projectChat);
  return chats.every(Boolean) ? { ...item, chats } : null;
}

function projectProjectList(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const item = value as JSONRecord;
  if (!only(item, ["projects", "selected_project_id", "selected_chat_id"]) || !Array.isArray(item.projects)) return null;
  // Empty IDs encode an empty browser session in the backend list contract.
  if (item.selected_project_id !== undefined && item.selected_project_id !== null && typeof item.selected_project_id !== "string") return null;
  if (item.selected_chat_id !== undefined && item.selected_chat_id !== null && typeof item.selected_chat_id !== "string") return null;
  const projects = item.projects.map(projectProject);
  return projects.every(Boolean) ? { ...item, projects } : null;
}

async function forward(request: Request, method: string, path: string, body?: JSONRecord): Promise<Response> {
  const requestID = crypto.randomUUID();
  const browser = session(request);
  const started = Date.now();
  const observe = path !== "/api/events" && !path.includes("action=poll");
  const dialogID = path.match(/^\/api\/dialogs\/([^/]+)/)?.[1];
  try {
    const upstream = await fetch(backendURL(path), { method, cache: "no-store", headers: { "X-Session-ID": browser.id, "X-Request-ID": requestID, ...(body ? { "Content-Type": "application/json" } : {}) }, ...(body ? { body: JSON.stringify(body) } : {}), signal: AbortSignal.timeout(timeoutMilliseconds) });
    if (upstream.status === 204) { if (observe) record(browser, requestID, "bff_request_completed", Date.now() - started, undefined, dialogID); return empty(204, requestID, browser.setCookie); }
    const raw: unknown = await upstream.json().catch(() => null);
    if (!upstream.ok) {
      const remoteError = projectError(raw);
      const category = remoteError?.category ?? "provider"; if (observe) record(browser, requestID, "bff_request_failed", Date.now() - started, category, dialogID); return error(category, requestID, upstream.status >= 400 && upstream.status < 500 ? upstream.status : 502, browser.setCookie);
    }
    const safe = projectSuccess(method, path, raw);
    if (!safe || typeof safe !== "object") { if (observe) record(browser, requestID, "bff_request_failed", Date.now() - started, "invalid_response", dialogID); return error("invalid_response", requestID, 502, browser.setCookie); }
    if (observe) record(browser, requestID, "bff_request_completed", Date.now() - started, undefined, dialogID);
    return response(safe, upstream.status, requestID, browser.setCookie);
  } catch (caught) {
    const category = caught instanceof DOMException && caught.name === "TimeoutError" ? "timeout" : "network";
    if (observe) record(browser, requestID, "bff_request_failed", Date.now() - started, category, dialogID);
    return error(category, requestID, 502, browser.setCookie);
  }
}

export async function listDialogs(request: Request): Promise<Response> { return forward(request, "GET", "/api/dialogs"); }
export async function listContextStrategies(request: Request): Promise<Response> { return forward(request, "GET", "/api/context-strategies"); }
export async function createDialog(request: Request): Promise<Response> {
 const body = await readJSON(request);
 if (!body || !only(body, ["context_strategy"]) || typeof body.context_strategy !== "string" || !contextStrategies.has(body.context_strategy)) return validationFailure(request, crypto.randomUUID());
 return forward(request, "POST", "/api/dialogs", { context_strategy: body.context_strategy });
}
export async function getDialog(request: Request, id: string): Promise<Response> { return forward(request, "GET", `/api/dialogs/${encodeURIComponent(id)}`); }
export async function deleteDialog(request: Request, id: string): Promise<Response> { return forward(request, "DELETE", `/api/dialogs/${encodeURIComponent(id)}`); }
export async function selectDialog(request: Request, id: string): Promise<Response> { return forward(request, "POST", `/api/dialogs/${encodeURIComponent(id)}/select`); }

export async function sendMessage(request: Request, id: string): Promise<Response> {
  const requestID = crypto.randomUUID();
  const body = await readJSON(request);
  if (!body || !only(body, ["client_message_id", "text"]) || !string(body.client_message_id) || !text(body.text)) return validationFailure(request, requestID, id);
  return forward(request, "POST", `/api/dialogs/${encodeURIComponent(id)}/messages`, { client_message_id: body.client_message_id as string, text: body.text as string });
}

export async function retryMessage(request: Request, id: string, messageID: string): Promise<Response> { return forward(request, "POST", `/api/dialogs/${encodeURIComponent(id)}/messages/${encodeURIComponent(messageID)}/retry`); }

export async function postEvent(request: Request): Promise<Response> {
  const requestID = crypto.randomUUID();
  const body = await readJSON(request);
  if (!body || !only(body, ["event", "dialog_id", "message_id", "project_id", "chat_id", "error_category"]) || !eventNames.has(body.event as string) || (body.dialog_id !== undefined && !string(body.dialog_id)) || (body.message_id !== undefined && !string(body.message_id)) || (body.project_id !== undefined && !string(body.project_id)) || (body.chat_id !== undefined && !string(body.chat_id)) || (body.error_category !== undefined && (!errorCategories.has(body.error_category as string) || body.error_category === "config" || body.error_category === "not_found" || body.error_category === "busy"))) return validationFailure(request, requestID);
  return forward(request, "POST", "/api/events", body);
}

export async function adminLogs(request: Request): Promise<Response> {
  const requestID = crypto.randomUUID();
  const url = new URL(request.url);
  // dialog_id is retained only for compatibility with the existing Admin route.
  const chatID = url.searchParams.get("dialog_id");
  const action = url.searchParams.get("action");
  if (!chatID || !["lookup", "refresh", "poll"].includes(action ?? "")) return validationFailure(request, requestID);
  return forward(request, "GET", `/api/admin/logs?dialog_id=${encodeURIComponent(chatID)}&action=${action}`);
}

export async function setStrategy(request: Request, id: string): Promise<Response> {
 const body = await readJSON(request);
 if (!body || !only(body, ["context_strategy"]) || typeof body.context_strategy !== "string" || !contextStrategies.has(body.context_strategy)) return validationFailure(request, crypto.randomUUID(), id);
 return forward(request, "PATCH", `/api/dialogs/${encodeURIComponent(id)}/strategy`, { context_strategy: body.context_strategy });
}
export async function createBranch(request: Request, id: string): Promise<Response> {
 const body = await readJSON(request);
 if (!body || !only(body, [])) return validationFailure(request, crypto.randomUUID(), id);
 return forward(request, "POST", `/api/dialogs/${encodeURIComponent(id)}/branches`, {});
}
export async function selectBranch(request: Request, id: string, branchID: string): Promise<Response> {
 const body = await readJSON(request);
 if (!body || !only(body, [])) return validationFailure(request, crypto.randomUUID(), id);
 return forward(request, "POST", `/api/dialogs/${encodeURIComponent(id)}/branches/${encodeURIComponent(branchID)}/select`, {});
}

export async function compactDialog(request: Request, id: string): Promise<Response> { return forward(request, "POST", `/api/dialogs/${encodeURIComponent(id)}/compact`); }

export async function listProjects(request: Request): Promise<Response> { return forward(request, "GET", "/api/projects"); }
export async function listProfiles(request: Request): Promise<Response> { return forward(request, "GET", "/api/profiles"); }
export async function createProfile(request: Request): Promise<Response> {
  const body = await readJSON(request);
  const name = body?.name;
  const style = body?.style;
  const constraints = body?.constraints;
  const additionalContext = body?.additional_context;
  if (!body || !only(body, ["name", "style", "constraints", "additional_context"]) || typeof name !== "string" || !name.trim() || [...name.trim()].length > 60 || typeof style !== "string" || !style.trim() || typeof constraints !== "string" || !constraints.trim() || typeof additionalContext !== "string" || !additionalContext.trim()) return validationFailure(request, crypto.randomUUID());
  return forward(request, "POST", "/api/profiles", { name: name.trim(), style, constraints, additional_context: additionalContext });
}
export async function selectProfile(request: Request, profileID: string): Promise<Response> {
  if (!profileID.trim()) return validationFailure(request, crypto.randomUUID());
  return forward(request, "POST", `/api/profiles/${encodeURIComponent(profileID)}/select`);
}
export async function deleteProfile(request: Request, profileID: string): Promise<Response> {
  if (!profileID.trim()) return validationFailure(request, crypto.randomUUID());
  return forward(request, "DELETE", `/api/profiles/${encodeURIComponent(profileID)}`);
}
export async function createProject(request: Request): Promise<Response> {
  const body = await readJSON(request);
  if (!body || !only(body, ["title"]) || (body.title !== undefined && !text(body.title))) return validationFailure(request, crypto.randomUUID());
  return forward(request, "POST", "/api/projects", body);
}
export async function deleteProject(request: Request, projectID: string): Promise<Response> { return forward(request, "DELETE", `/api/projects/${encodeURIComponent(projectID)}`); }
export async function renameProject(request: Request, projectID: string): Promise<Response> {
  const body = await readJSON(request); const title = body?.title;
  if (!body || !only(body, ["title"]) || typeof title !== "string" || !title.trim() || [...title.trim()].length > 100) return validationFailure(request, crypto.randomUUID());
  return forward(request, "PATCH", `/api/projects/${encodeURIComponent(projectID)}`, { title: title.trim() });
}
export async function selectProject(request: Request, projectID: string): Promise<Response> { return forward(request, "POST", `/api/projects/${encodeURIComponent(projectID)}/select`); }
export async function createChat(request: Request, projectID: string): Promise<Response> {
  const body = await readJSON(request);
  if (!body || !only(body, [])) return validationFailure(request, crypto.randomUUID());
  return forward(request, "POST", `/api/projects/${encodeURIComponent(projectID)}/chats`, {});
}
export async function getChat(request: Request, projectID: string, chatID: string): Promise<Response> { return forward(request, "GET", `/api/projects/${encodeURIComponent(projectID)}/chats/${encodeURIComponent(chatID)}`); }
export async function deleteChat(request: Request, projectID: string, chatID: string): Promise<Response> { return forward(request, "DELETE", `/api/projects/${encodeURIComponent(projectID)}/chats/${encodeURIComponent(chatID)}`); }
export async function selectChat(request: Request, projectID: string, chatID: string): Promise<Response> { return forward(request, "POST", `/api/projects/${encodeURIComponent(projectID)}/chats/${encodeURIComponent(chatID)}/select`); }
export async function sendProjectMessage(request: Request, projectID: string, chatID: string): Promise<Response> {
  const requestID = crypto.randomUUID(); const body = await readJSON(request);
  if (!body || !only(body, ["client_message_id", "text"]) || !string(body.client_message_id) || !text(body.text)) return validationFailure(request, requestID, chatID);
  return forward(request, "POST", `/api/projects/${encodeURIComponent(projectID)}/chats/${encodeURIComponent(chatID)}/messages`, { client_message_id: body.client_message_id as string, text: body.text as string });
}
export async function retryProjectMessage(request: Request, projectID: string, chatID: string, messageID: string): Promise<Response> { return forward(request, "POST", `/api/projects/${encodeURIComponent(projectID)}/chats/${encodeURIComponent(chatID)}/messages/${encodeURIComponent(messageID)}/retry`); }
export async function taskInput(request: Request, projectID: string, chatID: string): Promise<Response> {
  const requestID = crypto.randomUUID(); const body = await readJSON(request);
  if (!body || !only(body, ["text", "candidate_task_id"]) || !text(body.text) || (body.candidate_task_id !== undefined && !string(body.candidate_task_id))) return validationFailure(request, requestID, chatID);
  return forward(request, "POST", `/api/projects/${encodeURIComponent(projectID)}/chats/${encodeURIComponent(chatID)}/tasks/input`, body);
}
export async function pauseTask(request: Request, projectID: string, chatID: string, taskID: string): Promise<Response> {
  const body = await readJSON(request);
  if (!body || !only(body, [])) return validationFailure(request, crypto.randomUUID(), chatID);
  return forward(request, "POST", `/api/projects/${encodeURIComponent(projectID)}/chats/${encodeURIComponent(chatID)}/tasks/${encodeURIComponent(taskID)}/pause`, {});
}
export async function resumeTask(request: Request, projectID: string, chatID: string, taskID: string): Promise<Response> {
  const body = await readJSON(request);
  if (!body || !only(body, ["text"]) || !text(body.text)) return validationFailure(request, crypto.randomUUID(), chatID);
  return forward(request, "POST", `/api/projects/${encodeURIComponent(projectID)}/chats/${encodeURIComponent(chatID)}/tasks/${encodeURIComponent(taskID)}/resume`, body);
}
export async function getMemory(request: Request, projectID: string): Promise<Response> { return forward(request, "GET", `/api/projects/${encodeURIComponent(projectID)}/memory`); }
export async function clearMemory(request: Request, projectID: string, layer: "global" | "project"): Promise<Response> { return forward(request, "DELETE", `/api/projects/${encodeURIComponent(projectID)}/memory/${layer}`); }
