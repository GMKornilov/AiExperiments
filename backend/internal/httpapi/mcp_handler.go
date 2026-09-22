package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"aichallenge/week_1/task_1/internal/llm"
	"aichallenge/week_1/task_1/internal/mcpclient"
	"aichallenge/week_1/task_1/internal/observability"
)

type MCPTools interface {
	ListTools(context.Context, string) ([]mcpclient.Tool, error)
}

func (h *memoryHandler) mcpObservability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || strings.TrimSpace(os.Getenv("MCP_OBSERVABILITY_TOKEN")) == "" || r.Header.Get("Authorization") != "Bearer "+os.Getenv("MCP_OBSERVABILITY_TOKEN") {
		if r.Method != http.MethodPost {
			method(w, http.MethodPost)
		} else {
			w.WriteHeader(http.StatusForbidden)
		}
		return
	}
	var payload struct {
		Timestamp     time.Time `json:"timestamp"`
		Source        string    `json:"source"`
		Event         string    `json:"event"`
		Operation     string    `json:"operation"`
		Outcome       string    `json:"outcome"`
		CorrelationID string    `json:"correlation_id"`
		DurationMS    int64     `json:"duration_ms"`
		HTTPStatus    int       `json:"http_status,omitempty"`
		ErrorCategory string    `json:"error_category,omitempty"`
		Tool          string    `json:"tool,omitempty"`
	}
	if !decode(w, r, &payload) || !validMCPRecord(observability.Record{Timestamp: payload.Timestamp, Source: payload.Source, Event: payload.Event, Operation: payload.Operation, Outcome: payload.Outcome, CorrelationID: payload.CorrelationID, DurationMS: payload.DurationMS, HTTPStatus: payload.HTTPStatus, ErrorCategory: payload.ErrorCategory, Tool: payload.Tool}) {
		failMemory(w, http.StatusBadRequest, "validation")
		return
	}
	record := observability.Record{Timestamp: payload.Timestamp, Source: payload.Source, Event: payload.Event, Operation: payload.Operation, Outcome: payload.Outcome, Result: payload.Outcome, CorrelationID: payload.CorrelationID, DurationMS: payload.DurationMS, HTTPStatus: payload.HTTPStatus, ErrorCategory: payload.ErrorCategory, Tool: payload.Tool}
	h.journal.Log(record, "")
	w.WriteHeader(http.StatusNoContent)
}

func validMCPRecord(record observability.Record) bool {
	allowed := (record.Source == "frontend_bff" && record.Event == "mcp_tools_request" && record.Operation == "mcp_tools_request") || (record.Source == "mcp_server" && (record.Event == "mcp_http" || record.Event == "mcp_initialize" || record.Event == "mcp_tools_list" || record.Event == "mcp_tools_call") && record.Event == record.Operation) || (record.Source == "brewmark" && record.Event == "brewmark_request" && record.Operation == "brewmark_request")
	if record.Timestamp.IsZero() || !allowed || (record.Outcome != "started" && record.Outcome != "success" && record.Outcome != "failure") || len(record.CorrelationID) > 128 || record.CorrelationID == "" || record.DurationMS < 0 {
		return false
	}
	if record.HTTPStatus != 0 && (record.HTTPStatus < 100 || record.HTTPStatus > 599) {
		return false
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`).MatchString(record.CorrelationID) {
		return false
	}
	if record.ErrorCategory != "" && (record.Outcome != "failure" || !map[string]bool{"MCP_NOT_CONFIGURED": true, "MCP_UNAVAILABLE": true, "MCP_TIMEOUT": true, "MCP_PROTOCOL_ERROR": true, "MCP_INVALID_RESPONSE": true, "UPSTREAM_UNAVAILABLE": true, "UPSTREAM_TIMEOUT": true, "UPSTREAM_RATE_LIMITED": true, "BAD_UPSTREAM_RESPONSE": true, "INVALID_ARGUMENT": true, "network": true}[record.ErrorCategory]) {
		return false
	}
	if record.Tool != "" && (record.Event != "mcp_tools_call" || (record.Tool != "brewmark_list_grinders" && record.Tool != "brewmark_list_brewers" && record.Tool != "brewmark_list_filters" && record.Tool != "brewmark_list_brew_methods")) {
		return false
	}
	return record.HTTPStatus == 0 || record.Event == "mcp_http" || record.Event == "mcp_tools_request" || record.Event == "brewmark_request"
}

var mcpMessages = map[mcpclient.ErrorCode]string{
	mcpclient.NotConfigured:   "MCP endpoint is not configured.",
	mcpclient.Unavailable:     "MCP is temporarily unavailable.",
	mcpclient.Timeout:         "MCP did not respond in time.",
	mcpclient.ProtocolError:   "Unable to connect to MCP.",
	mcpclient.InvalidResponse: "MCP returned an invalid response.",
}

func (h *memoryHandler) mcpTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		method(w, http.MethodPost)
		return
	}
	if r.URL.RawQuery != "" || r.Body == nil {
		failMemory(w, http.StatusBadRequest, "validation")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	if err != nil || len(body) != 0 {
		failMemory(w, http.StatusBadRequest, "validation")
		return
	}
	started := time.Now()
	result, category := "success", ""
	defer func() {
		slog.Info("barista.mcp_tools", "source", "backend", "operation", "mcp_tools_list", "outcome", result, "error_category", category, "correlation_id", llm.RequestID(r.Context()), "duration_ms", time.Since(started).Milliseconds())
		h.journal.Log(observability.Record{Source: "backend", Event: "mcp_tools_list", Operation: "mcp_tools_list", Result: result, Outcome: result, CorrelationID: llm.RequestID(r.Context()), DurationMS: time.Since(started).Milliseconds(), ErrorCategory: category}, "")
	}()
	if h.mcpToolsClient == nil {
		result, category = "failure", string(mcpclient.NotConfigured)
		writeMCPError(w, mcpclient.NotConfigured)
		return
	}
	tools, err := h.mcpToolsClient.ListTools(r.Context(), llm.RequestID(r.Context()))
	if err != nil {
		code := mcpErrorCode(err)
		result, category = "failure", string(code)
		writeMCPError(w, code)
		return
	}
	_ = json.NewEncoder(w).Encode(struct {
		Tools []mcpclient.Tool `json:"tools"`
	}{Tools: tools})
}

func mcpErrorCode(err error) mcpclient.ErrorCode {
	var safeError *mcpclient.Error
	if errors.As(err, &safeError) {
		return safeError.Code
	}
	return mcpclient.ProtocolError
}

func writeMCPError(w http.ResponseWriter, code mcpclient.ErrorCode) {
	status := http.StatusBadGateway
	if code == mcpclient.NotConfigured || code == mcpclient.Unavailable {
		status = http.StatusServiceUnavailable
	}
	if code == mcpclient.Timeout {
		status = http.StatusGatewayTimeout
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code    mcpclient.ErrorCode `json:"code"`
		Message string              `json:"message"`
	}{Code: code, Message: mcpMessages[code]})
}
