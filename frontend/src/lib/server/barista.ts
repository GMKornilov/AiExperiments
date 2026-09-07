import "server-only";

const maxBodyBytes = 64 * 1024;
const maxTextRunes = 4_000;
const timeoutMilliseconds = 35_000;
const bodyTimeoutMilliseconds = 10_000;
const sessionCookie = "barista_session";
const errorCategories = new Set(["config", "validation", "network", "timeout", "provider", "invalid_response", "not_found", "busy"]);
const eventNames = new Set(["dialog_created", "dialog_selected", "dialog_deleted", "message_sent", "message_retried", "message_copied", "dialog_validation_failed", "message_failed", "admin_lookup", "admin_refresh", "bff_request_completed", "bff_request_failed"]);

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
  const messages: Record<string, string> = { validation: "Некорректный запрос.", not_found: "Данные не найдены.", busy: "Запрос уже выполняется.", config: "Сервис временно недоступен." };
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
function text(value: unknown): string | null { return typeof value === "string" && value.trim() && Array.from(value).length <= maxTextRunes ? value : null; }
function string(value: unknown): string | null { return typeof value === "string" && value.trim() ? value : null; }

function projectError(value: unknown): { category: string; message: string } | null {
  if (!value || typeof value !== "object") return null;
  const item = (value as JSONRecord).error;
  if (!item || typeof item !== "object") return null;
  const category = (item as JSONRecord).category;
  const message = (item as JSONRecord).message;
  return typeof category === "string" && typeof message === "string" && errorCategories.has(category) ? { category, message } : null;
}

function projectMessage(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object") return null;
  const item = value as JSONRecord;
  if (!only(item, ["id", "client_message_id", "role", "text", "status", "created_at", "error_category"]) || !string(item.id) || (item.role !== "user" && item.role !== "assistant") || typeof item.text !== "string" || !["pending", "success", "error"].includes(item.status as string) || typeof item.created_at !== "string") return null;
  if (item.client_message_id !== undefined && !string(item.client_message_id)) return null;
  if (item.error_category !== undefined && !errorCategories.has(item.error_category as string)) return null;
  return item;
}

function projectDialog(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object") return null;
  const item = value as JSONRecord;
  if (!only(item, ["id", "title", "created_at", "updated_at", "messages"]) || !string(item.id) || typeof item.title !== "string" || typeof item.created_at !== "string" || typeof item.updated_at !== "string" || !Array.isArray(item.messages)) return null;
  const messages = item.messages.map(projectMessage);
  return messages.every(Boolean) ? { ...item, messages } : null;
}

function projectSuccess(method: string, path: string, value: unknown): unknown | null {
  if (path === "/api/dialogs" && method === "GET") {
    if (!value || typeof value !== "object") return null;
    const item = value as JSONRecord;
    if (!only(item, ["dialogs", "selected_dialog_id"]) || !Array.isArray(item.dialogs) || (item.selected_dialog_id !== "" && typeof item.selected_dialog_id !== "string")) return null;
    const dialogs = item.dialogs.map(projectDialog);
    return dialogs.every(Boolean) ? { dialogs, selected_dialog_id: item.selected_dialog_id } : null;
  }
  if (path === "/api/dialogs" || path.startsWith("/api/dialogs/")) return projectDialog(value);
  if (path.startsWith("/api/admin/logs")) return projectLogs(value);
  return value;
}

function projectLogs(value: unknown): JSONRecord | null {
  if (!value || typeof value !== "object") return null;
  const item = value as JSONRecord;
  if (!only(item, ["found", "log_text_payloads", "logs"]) || typeof item.found !== "boolean" || typeof item.log_text_payloads !== "boolean" || !Array.isArray(item.logs)) return null;
  const logs: JSONRecord[] = [];
  for (const value of item.logs) {
    if (!value || typeof value !== "object") return null;
    const log = value as JSONRecord;
    if (!only(log, ["timestamp", "source", "event", "result", "correlation_id", "dialog_id", "message_id", "duration_ms", "error_category", "text"]) || typeof log.timestamp !== "string" || (log.source !== "frontend" && log.source !== "backend") || !string(log.event) || !string(log.result) || !string(log.correlation_id) || (log.dialog_id !== undefined && !string(log.dialog_id)) || (log.message_id !== undefined && !string(log.message_id)) || (log.duration_ms !== undefined && (typeof log.duration_ms !== "number" || !Number.isFinite(log.duration_ms))) || (log.error_category !== undefined && !errorCategories.has(log.error_category as string) && log.error_category !== "cancelled") || (log.text !== undefined && typeof log.text !== "string")) return null;
    logs.push(log);
  }
  return { found: item.found, log_text_payloads: item.log_text_payloads, logs };
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
export async function createDialog(request: Request): Promise<Response> { return forward(request, "POST", "/api/dialogs"); }
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
  if (!body || !only(body, ["event", "dialog_id", "message_id", "error_category"]) || !eventNames.has(body.event as string) || (body.dialog_id !== undefined && !string(body.dialog_id)) || (body.message_id !== undefined && !string(body.message_id)) || (body.error_category !== undefined && (!errorCategories.has(body.error_category as string) || body.error_category === "config" || body.error_category === "not_found" || body.error_category === "busy"))) return validationFailure(request, requestID);
  return forward(request, "POST", "/api/events", body);
}

export async function adminLogs(request: Request): Promise<Response> {
  const requestID = crypto.randomUUID();
  const url = new URL(request.url);
  const id = url.searchParams.get("dialog_id");
  const action = url.searchParams.get("action");
  if (!id || !["lookup", "refresh", "poll"].includes(action ?? "")) return validationFailure(request, requestID);
  return forward(request, "GET", `/api/admin/logs?dialog_id=${encodeURIComponent(id)}&action=${action}`);
}
