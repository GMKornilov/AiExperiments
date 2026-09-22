package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"aichallenge/week_1/task_1/internal/brewmark"
	"aichallenge/week_1/task_1/internal/mcpserver"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	timeout, err := requestTimeout(os.Getenv("BREWMARK_REQUEST_TIMEOUT"))
	if err != nil {
		logger.Error("brewmark.mcp", "operation", "config", "outcome", "failure", "error_category", "config")
		os.Exit(1)
	}
	client, err := brewmark.NewClient(brewmark.Config{BaseURL: os.Getenv("BREWMARK_BASE_URL"), Token: os.Getenv("BREWMARK_API_TOKEN"), Timeout: timeout, Logger: logger, Observer: func(ctx context.Context, outcome, category string, status int, duration time.Duration) {
		emitEvent(logger, os.Getenv("MCP_OBSERVABILITY_URL"), os.Getenv("MCP_OBSERVABILITY_TOKEN"), "brewmark", "brewmark_request", "brewmark_request", outcome, category, status, brewmark.CorrelationID(ctx), duration, "")
	}})
	if err != nil {
		logger.Error("brewmark.mcp", "operation", "config", "outcome", "failure", "error_category", "config")
		os.Exit(1)
	}
	server := &http.Server{
		Addr:              value("MCP_ADDR", "127.0.0.1:8080"),
		Handler:           mcpserver.New(client, mcpserver.Config{AllowedOrigins: strings.Split(strings.TrimSpace(os.Getenv("MCP_ALLOWED_ORIGINS")), ","), Logger: logger, ObservabilityURL: os.Getenv("MCP_OBSERVABILITY_URL"), ObservabilityToken: os.Getenv("MCP_OBSERVABILITY_TOKEN")}).Handler(),
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
		<-signals
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		logger.Error("brewmark.mcp", "operation", "serve", "outcome", "failure", "error_category", "network")
		os.Exit(1)
	}
}
func emitEvent(logger *slog.Logger, endpoint, token, source, event, operation, outcome, category string, status int, id string, duration time.Duration, tool string) {
	if endpoint == "" || token == "" {
		return
	}
	type eventRecord struct {
		Timestamp     string `json:"timestamp"`
		Source        string `json:"source"`
		Event         string `json:"event"`
		Operation     string `json:"operation"`
		Outcome       string `json:"outcome"`
		CorrelationID string `json:"correlation_id"`
		DurationMS    int64  `json:"duration_ms"`
		HTTPStatus    *int   `json:"http_status,omitempty"`
		ErrorCategory string `json:"error_category,omitempty"`
		Tool          string `json:"tool,omitempty"`
	}
	record := eventRecord{
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		Source:        source,
		Event:         event,
		Operation:     operation,
		Outcome:       outcome,
		CorrelationID: id,
		DurationMS:    duration.Milliseconds(),
		ErrorCategory: category,
		Tool:          tool,
	}
	if status != 0 {
		record.HTTPStatus = &status
	}
	body, err := json.Marshal(record)
	if err != nil {
		return
	}

	deadline := time.Now().Add(time.Second)
	client := &http.Client{}
	for attempt := 0; attempt < 2 && time.Now().Before(deadline); attempt++ {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		req, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if requestErr == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			response, requestErr := client.Do(req)
			if response != nil {
				_ = response.Body.Close()
			}
			if requestErr == nil && response.StatusCode == http.StatusNoContent {
				cancel()
				return
			}
		}
		cancel()
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("brewmark.mcp.observability", "operation", "collector_publish", "outcome", "failure", "error_category", "collector_unavailable")
}
func value(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
func requestTimeout(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 10 * time.Second, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, errors.New("BREWMARK_REQUEST_TIMEOUT must be a positive duration")
	}
	return parsed, nil
}
