package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"aichallenge/week_1/task_1/internal/config"
	"aichallenge/week_1/task_1/internal/llm"
)

const configPath = "config.yaml"

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	client := llm.NewClient(cfg.BaseURL, cfg.APIKey, cfg.RequestTimeout)
	scanner := bufio.NewScanner(input)
	for {
		if _, err := fmt.Fprint(output, ">>> "); err != nil {
			return fmt.Errorf("вывод приглашения: %w", err)
		}
		if !scanner.Scan() {
			break
		}

		prompt := strings.TrimSpace(scanner.Text())
		if prompt == "" {
			continue
		}

		answer, err := client.Chat(context.Background(), cfg.Model, prompt)
		if err != nil {
			if _, writeErr := fmt.Fprintln(output, "Ошибка:", err); writeErr != nil {
				return fmt.Errorf("вывод ошибки: %w", writeErr)
			}
			continue
		}

		if _, err := fmt.Fprintln(output, "<<< "+answer); err != nil {
			return fmt.Errorf("вывод ответа: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("чтение ввода: %w", err)
	}

	return nil
}
