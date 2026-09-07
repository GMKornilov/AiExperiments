export type MessageRole = "user" | "assistant";
export type MessageStatus = "pending" | "success" | "error";
export type ErrorCategory = "config" | "validation" | "network" | "timeout" | "provider" | "invalid_response" | "not_found" | "busy";

export type BaristaMessage = {
  id: string;
  client_message_id?: string;
  role: MessageRole;
  text: string;
  status: MessageStatus;
  created_at: string;
  error_category?: ErrorCategory;
  localOnly?: boolean;
};

export type TitleStatus = "idle" | "pending" | "success" | "error";
export type Dialog = { id: string; title: string; title_status: TitleStatus; created_at: string; updated_at: string; messages: BaristaMessage[] };
export type DialogList = { dialogs: Dialog[]; selected_dialog_id?: string | null };
export type APIError = { category: ErrorCategory; message: string };
export type LogEvent = "dialog_created" | "dialog_selected" | "dialog_deleted" | "dialog_id_copied" | "message_sent" | "message_retried" | "message_copied" | "dialog_validation_failed" | "message_failed" | "admin_lookup" | "admin_refresh";
export type AdminLog = { timestamp: string; source: "frontend" | "backend"; event: string; result: string; correlation_id: string; dialog_id?: string; message_id?: string; duration_ms?: number; error_category?: string; text?: string };
export type AdminLogsResponse = { found: boolean; log_text_payloads: boolean; logs: AdminLog[] };
