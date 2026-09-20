export type MessageRole = "user" | "assistant";
export type MessageStatus = "pending" | "success" | "error";
export type MemoryStatus = "idle" | "updating" | "success" | "error";
export type TitleStatus = "idle" | "pending" | "success" | "fallback";
export type ErrorCategory = "config" | "validation" | "network" | "timeout" | "provider" | "invalid_response" | "not_found" | "busy" | "cancelled" | "storage" | "memory" | "context_limit";

export type BaristaMessage = { id: string; client_message_id?: string; role: MessageRole; text: string; status: MessageStatus; created_at: string; error_category?: ErrorCategory; localOnly?: boolean; paused?: boolean };
export type TaskStage = "clarify_input" | "research_input_data" | "execution" | "user_feedback";
export type TaskStatus = "active" | "paused" | "done";
export type TaskPlanItemStatus = "pending" | "current" | "completed";
export type TaskPlanItem = { id: string; title: string; status: TaskPlanItemStatus; stage?: TaskStage };
export type Task = { id: string; title: string; description: string; stage: TaskStage; current_step: string; expected_action: string; status: TaskStatus; plan: TaskPlanItem[]; current_plan_item?: string; created_at: string; updated_at: string };
export type TaskCandidate = Pick<Task, "id" | "title" | "description">;
export type TaskInputResult = { chat: Chat; candidates?: TaskCandidate[] };
export type Chat = { id: string; project_id: string; title: string; title_status: TitleStatus; created_at: string; updated_at: string; messages: BaristaMessage[]; memory_status: MemoryStatus; memory_error_category?: ErrorCategory; tasks?: Task[] };
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
// The admin journal is an observability surface, not a public domain API. Keep
// its fields open so new backend metadata is visible without a BFF release.
export type AdminLog = { [field: string]: unknown; call_id?: string; timestamp: string; source: "frontend" | "backend"; event: string; result: string; correlation_id: string; dialog_id?: string; message_id?: string; duration_ms?: number; error_category?: string; text?: string; purpose?: string; usage?: TokenUsage; payload?: string; http_status?: number; truncated?: boolean };
export type AdminLogsResponse = { found: boolean; log_text_payloads: boolean; logs: AdminLog[] };
