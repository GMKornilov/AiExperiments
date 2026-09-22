// Package mcpserver exposes BrewMark catalogue tools through MCP Streamable HTTP.
package mcpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"aichallenge/week_1/task_1/internal/brewmark"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxRequestBodyBytes = 64 * 1024

type Server struct{ handler http.Handler }
type Config struct {
	AllowedOrigins []string
	Logger         *slog.Logger
}

func New(client *brewmark.Client, cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "brewmark-mcp", Version: "1.0.0"}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{ListChanged: false}},
	})
	register(server, logger, "brewmark_list_brew_methods", "List BrewMark brew methods.", emptySchema(), methodsSchema(), func(ctx context.Context, raw json.RawMessage) (any, error) {
		if _, _, err := filters(raw); err != nil {
			return nil, err
		}
		return client.Methods(ctx)
	})
	register(server, logger, "brewmark_list_brewers", "List BrewMark brewing machines and manual brewers.", brewerInputSchema(), brewersSchema(), func(ctx context.Context, raw json.RawMessage) (any, error) {
		brand, method, err := filters(raw, "brand", "brewMethod")
		if err != nil {
			return nil, err
		}
		return client.Brewers(ctx, brand, method)
	})
	register(server, logger, "brewmark_list_filters", "List BrewMark coffee filters.", emptySchema(), filtersSchema(), func(ctx context.Context, raw json.RawMessage) (any, error) {
		if _, _, err := filters(raw); err != nil {
			return nil, err
		}
		return client.Filters(ctx)
	})
	register(server, logger, "brewmark_list_grinders", "List BrewMark coffee grinders.", grinderInputSchema(), grindersSchema(), func(ctx context.Context, raw json.RawMessage) (any, error) {
		brand, _, err := filters(raw, "brand")
		if err != nil {
			return nil, err
		}
		return client.Grinders(ctx, brand)
	})
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, Logger: logger})
	mux := http.NewServeMux()
	mux.Handle("/mcp", originGuard(cfg.AllowedOrigins, postOnly(limitBody(mcpHandler))))
	mux.Handle("/healthz", getOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })))
	return &Server{handler: observed(mux, logger)}
}
func (s *Server) Handler() http.Handler { return s.handler }

func register(server *mcp.Server, logger *slog.Logger, name, description string, input, output any, operation func(context.Context, json.RawMessage) (any, error)) {
	server.AddTool(&mcp.Tool{Name: name, Description: description, InputSchema: input, OutputSchema: output}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		started := time.Now()
		outcome, category := "success", ""
		defer func() {
			logger.Info("brewmark.mcp", "correlation_id", brewmark.CorrelationID(ctx), "operation", name, "outcome", outcome, "error_category", category, "duration_ms", time.Since(started).Milliseconds())
		}()
		result, err := operation(ctx, request.Params.Arguments)
		if err != nil {
			outcome = "failure"
			if toolErr, ok := err.(*brewmark.ToolError); ok {
				category = string(toolErr.Code)
			} else {
				category = string(brewmark.UpstreamUnavailable)
			}
			return toolFailure(err), nil
		}
		count := countResult(result)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "Found " + itoa(count) + " " + resultName(name)}}, StructuredContent: result}, nil
	})
}
func toolFailure(err error) *mcp.CallToolResult {
	toolErr, ok := err.(*brewmark.ToolError)
	if !ok {
		toolErr = &brewmark.ToolError{Code: brewmark.UpstreamUnavailable, Message: "BrewMark API is temporarily unavailable", Retryable: true}
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: toolErr.Message}}, StructuredContent: toolErr}
}
func filters(raw json.RawMessage, allowed ...string) (string, string, error) {
	var values map[string]json.RawMessage
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if json.Unmarshal(raw, &values) != nil {
		return "", "", invalidArgument()
	}
	if values == nil {
		return "", "", invalidArgument()
	}
	allow := map[string]bool{}
	for _, name := range allowed {
		allow[name] = true
	}
	for name := range values {
		if !allow[name] {
			return "", "", invalidArgument()
		}
	}
	get := func(name string) (string, error) {
		rawValue, ok := values[name]
		if !ok {
			return "", nil
		}
		var value string
		if json.Unmarshal(rawValue, &value) != nil {
			return "", invalidArgument()
		}
		value = strings.TrimSpace(value)
		if value == "" || utf8.RuneCountInString(value) > 100 {
			return "", invalidArgument()
		}
		return value, nil
	}
	brand, err := get("brand")
	if err != nil {
		return "", "", err
	}
	method, err := get("brewMethod")
	if err != nil {
		return "", "", err
	}
	return brand, method, nil
}
func invalidArgument() error {
	return &brewmark.ToolError{Code: brewmark.InvalidArgument, Message: "Invalid tool arguments", Retryable: false}
}
func originGuard(allowed []string, next http.Handler) http.Handler {
	set := map[string]bool{}
	for _, origin := range allowed {
		if origin = strings.TrimSpace(origin); origin != "" {
			set[origin] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && !set[origin] {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func postOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		originalBody := request.Body
		defer originalBody.Close()
		body, err := io.ReadAll(io.LimitReader(originalBody, maxRequestBodyBytes+1))
		if err != nil {
			http.Error(writer, "Bad Request", http.StatusBadRequest)
			return
		}
		if len(body) > maxRequestBodyBytes {
			http.Error(writer, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
			return
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(writer, request)
	})
}
func getOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}
func (writer *statusWriter) Write(data []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return writer.ResponseWriter.Write(data)
}

func observed(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id := requestID(r.Header.Get("X-Request-ID"))
		writer := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(writer, r.WithContext(brewmark.WithCorrelationID(r.Context(), id)))
		if writer.status == 0 {
			writer.status = http.StatusOK
		}
		outcome, category := "success", ""
		if writer.status >= 400 {
			outcome, category = "failure", http.StatusText(writer.status)
		}
		logger.Info("brewmark.mcp", "correlation_id", id, "operation", "http", "outcome", outcome, "error_category", category, "http_status", writer.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

func requestID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 0 && len(value) <= 128 {
		for _, r := range value {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				value = ""
				break
			}
		}
		if value != "" {
			return value
		}
	}
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	return "request-id-unavailable"
}
func countResult(value any) int {
	switch v := value.(type) {
	case brewmark.GrindersResult:
		return v.Count
	case brewmark.BrewersResult:
		return v.Count
	case brewmark.FiltersResult:
		return v.Count
	case brewmark.MethodsResult:
		return v.Count
	}
	return 0
}
func resultName(name string) string {
	switch name {
	case "brewmark_list_grinders":
		return "grinders"
	case "brewmark_list_brewers":
		return "brewers"
	case "brewmark_list_filters":
		return "filters"
	default:
		return "brew methods"
	}
}
func itoa(value int) string { return strconv.Itoa(value) }
