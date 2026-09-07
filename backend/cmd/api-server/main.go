package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/config"
	"aichallenge/week_1/task_1/internal/httpapi"
	"aichallenge/week_1/task_1/internal/llm"
	"aichallenge/week_1/task_1/internal/observability"
	"aichallenge/week_1/task_1/internal/session"
)

func main() {
	logger := slog.Default()
	started := time.Now()
	requestID := llm.RequestID(llm.WithRequestID(context.Background(), ""))
	configPath, err := parseFlags(os.Args[1:])
	if err != nil {
		logConfig(logger, requestID, "failure", time.Since(started), "validation")
		os.Exit(1)
	}
	cfg, err := config.LoadBackend(configPath)
	if err != nil {
		logConfig(logger, requestID, "failure", time.Since(started), "config")
		os.Exit(1)
	}
	logConfig(logger, requestID, "success", time.Since(started), "")
	journal := observability.NewJournal(cfg.LogTextPayloads, nil)
	store := session.NewStore(agent.OpenAIProvider{})
	defer store.Close()
	server := &http.Server{Addr: cfg.Addr, Handler: httpapi.New(store, httpapi.SnapshotLoader(cfg.LLMConfigPath), journal)}
	if err := serve(server, store.Close); err != nil {
		logger.Error("barista.server", "source", "backend", "event", "server", "result", "failure", "correlation_id", requestID, "error_category", "network")
		os.Exit(1)
	}
}

func logConfig(logger *slog.Logger, requestID, result string, duration time.Duration, category string) {
	logger.Info("barista.event", "source", "backend", "event", "config_read", "result", result, "correlation_id", requestID, "duration_ms", duration.Milliseconds(), "error_category", category)
}

func parseFlags(args []string) (string, error) {
	flags := flag.NewFlagSet("api-server", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "config.yaml", "путь к backend YAML")
	if err := flags.Parse(args); err != nil {
		return "", fmt.Errorf("разбор аргументов: %w", err)
	}
	if flags.NArg() != 0 || strings.TrimSpace(*path) == "" {
		return "", fmt.Errorf("некорректные аргументы")
	}
	return *path, nil
}
func serve(server *http.Server, closeStore func()) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		<-signals
		closeStore()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
