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

	"aichallenge/week_1/task_1/internal/adapters/configsnapshot"
	"aichallenge/week_1/task_1/internal/adapters/extractjson"
	"aichallenge/week_1/task_1/internal/adapters/openai"
	"aichallenge/week_1/task_1/internal/adapters/statejson"
	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/application/conversation"
	"aichallenge/week_1/task_1/internal/application/invariant"
	"aichallenge/week_1/task_1/internal/application/state"
	"aichallenge/week_1/task_1/internal/application/taskflow"
	"aichallenge/week_1/task_1/internal/application/workspace"
	"aichallenge/week_1/task_1/internal/config"
	"aichallenge/week_1/task_1/internal/domain/model"
	"aichallenge/week_1/task_1/internal/httpapi"
	"aichallenge/week_1/task_1/internal/llm"
	"aichallenge/week_1/task_1/internal/mcpclient"
	"aichallenge/week_1/task_1/internal/observability"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	logger := slog.Default()
	started := time.Now()
	requestID := llm.RequestID(llm.WithRequestID(context.Background(), ""))
	options, err := parseFlags(os.Args[1:])
	if err != nil {
		logConfig(logger, requestID, "failure", time.Since(started), "validation")
		os.Exit(1)
	}
	if err := config.LoadEnvFiles(options.envFiles); err != nil {
		logConfig(logger, requestID, "failure", time.Since(started), "config")
		os.Exit(1)
	}
	cfg, err := config.LoadBackend(options.configPath)
	if err != nil {
		logConfig(logger, requestID, "failure", time.Since(started), "config")
		os.Exit(1)
	}
	logConfig(logger, requestID, "success", time.Since(started), "")

	snapshot, err := configsnapshot.Loader(cfg.LLMConfigPath)()
	if err != nil {
		logger.Error("barista.server", "error_category", "config", "correlation_id", requestID)
		os.Exit(1)
	}
	if snapshot.InvariantValidation == nil {
		logger.Error("barista.server", "error_category", "config", "correlation_id", requestID)
		os.Exit(1)
	}
	client, err := openai.New(agent.OpenAIProvider{}, snapshot)
	if err != nil {
		logger.Error("barista.server", "error_category", "config", "correlation_id", requestID)
		os.Exit(1)
	}
	disk, initial, err := statejson.Open(cfg.HistoryPath)
	if err != nil {
		logger.Error("barista.server", "error_category", "storage", "correlation_id", requestID)
		os.Exit(1)
	}
	store := state.New(initial, disk)
	id := func() string { return llm.RequestID(llm.WithRequestID(context.Background(), "")) }
	settings := conversation.Settings{Prompt: snapshot.Chat.SystemPrompt, TitlePrompt: snapshot.Text.SystemPrompt, Window: snapshot.ContextWindowMessages}
	invariants := invariant.NewSet(client)
	titles := conversation.NewTitles(store, client, settings.TitlePrompt, time.Now)
	extractor := extractjson.NewExtractor(client, snapshot.Memory.Snapshot.SystemPrompt)
	workspaceService := workspace.New(store, store, id, time.Now)
	conversationService := conversation.New(store, client, extractor, titles, settings, id, time.Now, invariants)
	tasks := taskflow.New(store, client, extractor, extractjson.ProposalDecoder{}, model.TaskRouter{}, titles, settings, id, time.Now, invariants)
	closeStore := func() { store.Close(); titles.Close() }
	defer closeStore()
	journal := observability.NewJournal(cfg.LogTextPayloads, logger)
	mcpToolsClient, mcpClientErr := mcpclient.New(os.Getenv("BREWMARK_MCP_URL"))
	if mcpClientErr != nil {
		mcpToolsClient = nil
	}
	server := &http.Server{Addr: cfg.Addr, Handler: httpapi.NewMemory(httpapi.UseCases{Projects: workspaceService, Chats: workspaceService, Profiles: workspaceService, Memory: workspaceService, Conversations: conversationService, Tasks: tasks, Health: store, MCPTools: mcpToolsClient, Invariants: invariants.Public()}, journal)}
	if err := serve(server, closeStore); err != nil {
		logger.Error("barista.server", "source", "backend", "event", "server", "result", "failure", "correlation_id", requestID, "error_category", "network")
		os.Exit(1)
	}
}

func logConfig(logger *slog.Logger, requestID, result string, duration time.Duration, category string) {
	logger.Info("barista.event", "source", "backend", "event", "config_read", "result", result, "correlation_id", requestID, "duration_ms", duration.Milliseconds(), "error_category", category)
}

type options struct {
	configPath string
	envFiles   []string
}

func parseFlags(args []string) (options, error) {
	flags := flag.NewFlagSet("api-server", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "config.yaml", "путь к backend YAML")
	var envFiles []string
	flags.Func("env-file", "путь к dotenv-файлу; флаг можно указать несколько раз", func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("путь к dotenv-файлу не должен быть пустым")
		}
		envFiles = append(envFiles, value)
		return nil
	})
	if err := flags.Parse(args); err != nil {
		return options{}, fmt.Errorf("разбор аргументов: %w", err)
	}
	if flags.NArg() != 0 || strings.TrimSpace(*path) == "" {
		return options{}, fmt.Errorf("некорректные аргументы")
	}
	return options{configPath: *path, envFiles: envFiles}, nil
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
