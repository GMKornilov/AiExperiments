package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"aichallenge/week_1/task_1/internal/algorithms"
	"aichallenge/week_1/task_1/internal/barista"
	"aichallenge/week_1/task_1/internal/config"
	"aichallenge/week_1/task_1/internal/httpapi"
	"aichallenge/week_1/task_1/internal/llm"
	"aichallenge/week_1/task_1/internal/temperature"
)

func main() {
	configPath, address, err := parseFlags(os.Args[1:])
	if err != nil {
		log.Printf("Ошибка: %v", err)
		os.Exit(1)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Printf("Ошибка: %v", err)
		os.Exit(1)
	}
	server := &http.Server{Addr: address, Handler: newHandler(cfg)}
	if err := serve(server); err != nil {
		log.Printf("Ошибка сервера: %v", err)
		os.Exit(1)
	}
}

func newHandler(cfg config.Config) http.Handler {
	baristaClient := llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.RequestTimeout)
	algorithmsClient := llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.AlgorithmRequestTimeout)
	return httpapi.NewHandlerWithTemperature(
		barista.NewServiceWithClient(cfg, baristaClient),
		algorithms.NewService(algorithmsClient, cfg.Model, cfg.AlgorithmRequestTimeout, cfg.AlgorithmPrompts),
		temperature.NewService(cfg),
	)
}

func parseFlags(args []string) (string, string, error) {
	flags := flag.NewFlagSet("api-server", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "config.yaml", "путь к YAML-конфигурации")
	address := flags.String("addr", ":8080", "адрес прослушивания")
	if err := flags.Parse(args); err != nil {
		return "", "", fmt.Errorf("разбор аргументов: %w", err)
	}
	if flags.NArg() != 0 || strings.TrimSpace(*configPath) == "" || strings.TrimSpace(*address) == "" {
		return "", "", fmt.Errorf("некорректные аргументы")
	}
	return *configPath, *address, nil
}

func serve(server *http.Server) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() { <-signals; _ = server.Shutdown(context.Background()) }()
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
