// Package observability stores safe product audit records in memory.
package observability

import (
	"log/slog"
	"strings"
	"sync"
	"time"
)

type Record struct {
	Timestamp     time.Time `json:"timestamp"`
	Source        string    `json:"source"`
	Event         string    `json:"event"`
	Result        string    `json:"result"`
	CorrelationID string    `json:"correlation_id"`
	DialogID      string    `json:"dialog_id,omitempty"`
	MessageID     string    `json:"message_id,omitempty"`
	DurationMS    int64     `json:"duration_ms"`
	ErrorCategory string    `json:"error_category,omitempty"`
	Text          string    `json:"text,omitempty"`
}

type Journal struct {
	mu      sync.Mutex
	records []Record
	logText bool
	logger  *slog.Logger
	deleted map[string]bool
	secrets map[string][]string
}

func NewJournal(logText bool, logger *slog.Logger) *Journal {
	if logger == nil {
		logger = slog.Default()
	}
	return &Journal{logText: logText, logger: logger, deleted: make(map[string]bool), secrets: make(map[string][]string)}
}

func (j *Journal) Log(record Record, credential string) {
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}
	j.mu.Lock()
	if !j.logText {
		record.Text = ""
	} else {
		for _, secret := range append(j.secrets[record.DialogID], credential) {
			if secret != "" {
				record.Text = strings.ReplaceAll(record.Text, secret, "[REDACTED]")
			}
		}
	}
	deleted := record.DialogID != "" && j.deleted[record.DialogID]
	if deleted {
		record.Text = ""
	}
	if !deleted {
		j.records = append(j.records, record)
	}
	j.mu.Unlock()
	attrs := []any{"source", record.Source, "event", record.Event, "result", record.Result, "correlation_id", record.CorrelationID, "dialog_id", record.DialogID, "message_id", record.MessageID, "duration_ms", record.DurationMS, "error_category", record.ErrorCategory}
	if j.logText && record.Text != "" {
		attrs = append(attrs, "text", record.Text)
	}
	j.logger.Info("barista.event", attrs...)
}

func (j *Journal) SetSecret(dialogID, secret string) {
	j.SetSecrets(dialogID, secret)
}

// SetSecrets registers every credential captured by one dialog snapshot.
func (j *Journal) SetSecrets(dialogID string, secrets ...string) {
	if dialogID == "" {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, secret := range secrets {
		if secret != "" {
			j.secrets[dialogID] = append(j.secrets[dialogID], secret)
		}
	}
}

func (j *Journal) Logs(dialogID string) []Record {
	j.mu.Lock()
	defer j.mu.Unlock()
	logs := make([]Record, 0)
	for _, record := range j.records {
		if record.DialogID == dialogID {
			logs = append(logs, record)
		}
	}
	return logs
}

func (j *Journal) DeleteDialog(dialogID string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.deleted[dialogID] = true
	delete(j.secrets, dialogID)
	kept := j.records[:0]
	for _, record := range j.records {
		if record.DialogID != dialogID {
			kept = append(kept, record)
		}
	}
	for index := len(kept); index < len(j.records); index++ {
		j.records[index] = Record{}
	}
	j.records = kept
}

func (j *Journal) LogTextPayloads() bool { return j.logText }
