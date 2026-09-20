package configsnapshot

import (
	"aichallenge/week_1/task_1/internal/agent"
	"aichallenge/week_1/task_1/internal/config"
)

func Loader(path string) func() (agent.DialogSnapshot, error) {
	return func() (agent.DialogSnapshot, error) {
		cfg, err := config.LoadLLM(path)
		if err != nil {
			return agent.DialogSnapshot{}, err
		}
		snap := agent.DialogSnapshot{Chat: endpointSnapshot(cfg.Chat), Text: endpointSnapshot(cfg.Text), ContextWindowMessages: cfg.ContextWindowMessages}
		if cfg.Summary != nil {
			snap.Summary = &agent.SummaryConfig{Snapshot: endpointSnapshot(cfg.Summary.Endpoint), KeepLastMessages: cfg.Summary.KeepLastMessages, BatchSize: cfg.Summary.BatchSize}
		}
		if cfg.Facts != nil {
			snap.Facts = &agent.FactsConfig{Snapshot: endpointSnapshot(cfg.Facts.Endpoint)}
		}
		if cfg.Memory != nil {
			snap.Memory = &agent.FactsConfig{Snapshot: endpointSnapshot(cfg.Memory.Endpoint)}
		}
		return snap, nil
	}
}

func endpointSnapshot(cfg config.LLMEndpoint) agent.Snapshot {
	return agent.Snapshot{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model, SystemPrompt: cfg.SystemPrompt, Timeout: cfg.RequestTimeout, Temperature: cfg.Temperature, ContextWindowTokens: cfg.ContextWindowTokens}
}
