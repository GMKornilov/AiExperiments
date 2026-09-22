package main

import (
	"context"
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
	client, err := brewmark.NewClient(brewmark.Config{BaseURL: os.Getenv("BREWMARK_BASE_URL"), Token: os.Getenv("BREWMARK_API_TOKEN"), Timeout: timeout, Logger: logger})
	if err != nil {
		logger.Error("brewmark.mcp", "operation", "config", "outcome", "failure", "error_category", "config")
		os.Exit(1)
	}
	server := &http.Server{
		Addr:              value("MCP_ADDR", "127.0.0.1:8080"),
		Handler:           mcpserver.New(client, mcpserver.Config{AllowedOrigins: strings.Split(strings.TrimSpace(os.Getenv("MCP_ALLOWED_ORIGINS")), ","), Logger: logger}).Handler(),
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
