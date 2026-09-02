package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"aichallenge/week_1/task_1/internal/barista"
	"aichallenge/week_1/task_1/internal/config"
)

const configPath = "config.yaml"

type mode = barista.Mode

const (
	modeFree       = barista.ModeFree
	modeControlled = barista.ModeControlled
)

func main() {
	selectedMode, err := parseMode(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
	if err := runWithMode(os.Stdin, os.Stdout, selectedMode); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

// parseMode parses CLI arguments without modifying global flag.CommandLine.
func parseMode(args []string) (mode, error) {
	flags := flag.NewFlagSet("llm-chat", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	value := flags.String("mode", string(modeFree), "режим: free или controlled")
	if err := flags.Parse(args); err != nil {
		return "", fmt.Errorf("разбор аргументов: %w", err)
	}
	if flags.NArg() != 0 {
		return "", fmt.Errorf("неожиданные аргументы: %s", strings.Join(flags.Args(), " "))
	}

	selectedMode := mode(*value)
	switch selectedMode {
	case modeFree, modeControlled:
		return selectedMode, nil
	default:
		return "", fmt.Errorf("недопустимый mode %q: используйте free или controlled", selectedMode)
	}
}

func run(input io.Reader, output io.Writer) error {
	return runWithMode(input, output, modeFree)
}

func runWithMode(input io.Reader, output io.Writer, selectedMode mode) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	service := barista.NewService(cfg)
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

		answer, err := service.Chat(context.Background(), selectedMode, prompt)
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
