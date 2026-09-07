import type { AdminLogsResponse, APIError, Dialog, DialogList, ErrorCategory, LogEvent } from "../model/types";

const genericError = "Не удалось получить ответ. Повторите отправку.";

export class BaristaAPIError extends Error {
  constructor(readonly category: ErrorCategory, message = genericError) {
    super(message);
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try { response = await fetch(path, { cache: "no-store", ...init }); } catch { throw new BaristaAPIError("network"); }
  if (response.status === 204) return undefined as T;
  const payload = await response.json().catch(() => null) as T | { error?: APIError } | null;
  if (!response.ok) {
    const error = payload && typeof payload === "object" && "error" in payload ? payload.error : undefined;
    throw new BaristaAPIError(error?.category ?? "network", error?.message || genericError);
  }
  if (!payload) throw new BaristaAPIError("invalid_response");
  return payload as T;
}

const json = (body: unknown): RequestInit => ({ method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });

export const baristaClient = {
  list: () => request<DialogList>("/api/dialogs"),
  get: (id: string) => request<Dialog>(`/api/dialogs/${encodeURIComponent(id)}`),
  create: () => request<Dialog>("/api/dialogs", { method: "POST" }),
  select: async (id: string) => { await request<unknown>(`/api/dialogs/${encodeURIComponent(id)}/select`, { method: "POST" }); },
  remove: async (id: string) => {
    const response = await fetch(`/api/dialogs/${encodeURIComponent(id)}`, { method: "DELETE", cache: "no-store" });
    if (!response.ok && response.status !== 204) {
      const payload = await response.json().catch(() => null) as { error?: APIError } | null;
      throw new BaristaAPIError(payload?.error?.category ?? "network", payload?.error?.message || genericError);
    }
  },
  send: (dialogID: string, clientMessageID: string, text: string) => request<Dialog>(`/api/dialogs/${encodeURIComponent(dialogID)}/messages`, json({ client_message_id: clientMessageID, text })),
  retry: (dialogID: string, messageID: string) => request<Dialog>(`/api/dialogs/${encodeURIComponent(dialogID)}/messages/${encodeURIComponent(messageID)}/retry`, { method: "POST" }),
  event: async (event: LogEvent, fields: { dialog_id?: string; message_id?: string; error_category?: ErrorCategory } = {}) => {
    try { await request<unknown>("/api/events", json({ event, ...fields })); } catch { /* telemetry must not break chat */ }
  },
  logs: (dialogID: string, action: "lookup" | "refresh" | "poll") => request<AdminLogsResponse>(`/api/admin/logs?dialog_id=${encodeURIComponent(dialogID)}&action=${action}`),
};

export function userFacingError(error: unknown) {
  return error instanceof BaristaAPIError && error.category === "validation" ? error.message : genericError;
}
