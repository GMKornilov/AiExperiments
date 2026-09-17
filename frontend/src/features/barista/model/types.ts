export type MessageRole = "user" | "assistant";
export type MessageStatus = "pending" | "success" | "error";
export type MemoryStatus = "idle" | "updating" | "success" | "error";
export type TitleStatus = "idle" | "pending" | "success" | "fallback";
export type ErrorCategory = "config" | "validation" | "network" | "timeout" | "provider" | "invalid_response" | "not_found" | "busy" | "cancelled" | "storage" | "memory";

export type BaristaMessage = { id: string; client_message_id?: string; role: MessageRole; text: string; status: MessageStatus; created_at: string; error_category?: ErrorCategory; localOnly?: boolean };
export type Chat = { id: string; project_id: string; title: string; title_status: TitleStatus; created_at: string; updated_at: string; messages: BaristaMessage[]; memory_status: MemoryStatus; memory_error_category?: ErrorCategory };
export type Project = { id: string; title: string; created_at: string; updated_at: string; chats: Chat[]; selected_chat_id?: string | null };
export type ProjectList = { projects: Project[]; selected_project_id?: string | null; selected_chat_id?: string | null };
export type Memory = { global_facts: string[]; project_facts: string[]; status: MemoryStatus; error_category?: ErrorCategory };
export type APIError = { category: ErrorCategory; message: string };
export type Profile = { id: string; name: string; style: string; constraints: string; additional_context: string; built_in: boolean };
export type ProfileList = { profiles: Profile[]; active_profile_id: string };
export type LogEvent = "project_created" | "project_selected" | "project_deleted" | "chat_created" | "chat_selected" | "chat_deleted" | "message_sent" | "message_retried" | "message_failed" | "memory_cleared" | "bff_request_completed" | "bff_request_failed";

// Deprecated diagnostics remain available to the admin screen while its API is migrated.
export type CompressionState = { available: boolean; enabled: boolean; summary: string; covered_messages: number; summary_tokens: number; summary_usage_missing: boolean; full_estimate: number; sent_estimate: number; last_input_tokens: number | null; context_window_tokens: number; pruned_messages?: number; archived_tokens?: number };
export type TokenUsage = { prompt_tokens: number; completion_tokens: number };
export type AdminLog = { call_id?: string; timestamp: string; source: "frontend" | "backend"; event: string; result: string; correlation_id: string; dialog_id?: string; message_id?: string; duration_ms?: number; error_category?: string; text?: string; purpose?: "chat" | "title" | "summary" | "facts"; usage?: TokenUsage; payload?: string; http_status?: number; truncated?: boolean };
export type AdminLogsResponse = { found: boolean; log_text_payloads: boolean; logs: AdminLog[] };
