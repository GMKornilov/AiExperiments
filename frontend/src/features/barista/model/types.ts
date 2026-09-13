export type MessageRole = "user" | "assistant";
export type MessageStatus = "pending" | "success" | "error";
export type ErrorCategory = "config" | "validation" | "network" | "timeout" | "provider" | "invalid_response" | "not_found" | "busy" | "cancelled" | "storage" | "context_limit";
export type TokenUsage = { prompt_tokens: number; completion_tokens: number };
export type ContextStrategyID = "sliding_window" | "facts" | "branching" | "summary";
export type ContextStrategy = { id: ContextStrategyID; available: boolean; reason?: string };
export type Branch = { id: string; name: string; parent_branch_id?: string; checkpoint_message_id?: string; message_count: number };

export type BaristaMessage = {
  id: string;
  client_message_id?: string;
  role: MessageRole;
  text: string;
  status: MessageStatus;
  created_at: string;
  error_category?: ErrorCategory;
  usage?: TokenUsage;
  localOnly?: boolean;
};

export type TitleStatus = "idle" | "pending" | "success" | "error";
export type CompressionState = { pruned_messages?: number; archived_tokens?: number; available: boolean; enabled: boolean; summary: string; covered_messages: number; summary_tokens: number; summary_usage_missing: boolean; full_estimate: number; sent_estimate: number; last_input_tokens: number | null; context_window_tokens: number };
export type Dialog = { compression?: CompressionState; id: string; title: string; title_status: TitleStatus; created_at: string; updated_at: string; messages: BaristaMessage[]; accounted_tokens: number; context_strategy?: ContextStrategyID; facts?: Record<string, string>; facts_tokens?: number; facts_usage_missing?: boolean; active_branch_id?: string; branches?: Branch[] };
export type DialogList = { dialogs: Dialog[]; selected_dialog_id?: string | null };
export type APIError = { category: ErrorCategory; message: string };
export type LogEvent = "dialog_created" | "dialog_selected" | "dialog_deleted" | "dialog_id_copied" | "message_sent" | "message_retried" | "message_copied" | "dialog_validation_failed" | "message_failed" | "strategy_selected" | "branch_created" | "branch_selected" | "admin_lookup" | "admin_refresh";
export type AdminLog = { call_id?: string; purpose?: "chat" | "title" | "summary" | "facts"; branch_id?: string; payload?: string; http_status?: number; truncated?: boolean; attempt_id?: string; usage?: TokenUsage; timestamp: string; source: "frontend" | "backend"; event: string; result: string; correlation_id: string; dialog_id?: string; message_id?: string; duration_ms?: number; error_category?: string; text?: string };
export type AdminLogsResponse = { found: boolean; log_text_payloads: boolean; logs: AdminLog[] };
