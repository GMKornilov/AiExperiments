// Package observability stores safe product audit records in memory.
package observability

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"aichallenge/week_1/task_1/internal/llm"
)

type Record struct {
	CallID        string     `json:"call_id,omitempty"`
	Purpose       string     `json:"purpose,omitempty"`
	Payload       string     `json:"payload,omitempty"`
	HTTPStatus    int        `json:"http_status,omitempty"`
	Truncated     bool       `json:"truncated,omitempty"`
	AttemptID     string     `json:"attempt_id,omitempty"`
	Usage         *llm.Usage `json:"usage,omitempty"`
	Timestamp     time.Time  `json:"timestamp"`
	Source        string     `json:"source"`
	Event         string     `json:"event"`
	Operation     string     `json:"operation,omitempty"`
	Tool          string     `json:"tool,omitempty"`
	Result        string     `json:"result"`
	Outcome       string     `json:"outcome,omitempty"`
	CorrelationID string     `json:"correlation_id"`
	DialogID      string     `json:"dialog_id,omitempty"`
	BranchID      string     `json:"branch_id,omitempty"`
	MessageID     string     `json:"message_id,omitempty"`
	DurationMS    int64      `json:"duration_ms"`
	ErrorCategory string     `json:"error_category,omitempty"`
	Text          string     `json:"text,omitempty"`
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
		record.Payload = ""
	} else {
		for _, secret := range j.secrets[record.DialogID] {
			record.Text = strings.ReplaceAll(record.Text, secret, "[REDACTED]")
			record.Payload = redactPayload(record.Payload, secret)
		}
		if credential != "" {
			record.Text = strings.ReplaceAll(record.Text, credential, "[REDACTED]")
			record.Payload = redactPayload(record.Payload, credential)
		}
	}
	deleted := record.DialogID != "" && j.deleted[record.DialogID]
	if deleted {
		record.Text = ""
		record.Payload = ""
	}
	if !deleted {
		j.records = append(j.records, record)
	}
	j.mu.Unlock()
	attrs := []any{"source", record.Source, "event", record.Event, "result", record.Result, "correlation_id", record.CorrelationID, "dialog_id", record.DialogID, "branch_id", record.BranchID, "message_id", record.MessageID, "duration_ms", record.DurationMS, "error_category", record.ErrorCategory}
	if record.AttemptID != "" {
		attrs = append(attrs, "attempt_id", record.AttemptID)
	}
	if record.CallID != "" {
		attrs = append(attrs, "call_id", record.CallID, "purpose", record.Purpose, "http_status", record.HTTPStatus, "truncated", record.Truncated)
	}
	if record.Payload != "" {
		var value any
		decoder := json.NewDecoder(strings.NewReader(record.Payload))
		decoder.UseNumber()
		if json.Valid([]byte(record.Payload)) && decoder.Decode(&value) == nil {
			attrs = append(attrs, "payload", value)
		} else {
			attrs = append(attrs, "payload", record.Payload)
		}
	}
	if record.Usage != nil {
		attrs = append(attrs, "usage", record.Usage)
	}
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

func (j *Journal) MCPLogs() []Record {
	j.mu.Lock()
	defer j.mu.Unlock()
	logs := make([]Record, 0)
	for _, record := range j.records {
		backend := record.Source == "backend" && record.Event == "mcp_tools_list" && record.Operation == "mcp_tools_list"
		remote := (record.Source == "frontend_bff" && record.Event == "mcp_tools_request" && record.Operation == record.Event) || (record.Source == "mcp_server" && record.Event == record.Operation) || (record.Source == "brewmark" && record.Event == "brewmark_request" && record.Operation == record.Event)
		if record.DialogID == "" && (backend || remote) {
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

func redactPayload(payload, secret string) string {
	var value any
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.UseNumber()
	if !json.Valid([]byte(payload)) || decoder.Decode(&value) != nil {
		return strings.ReplaceAll(payload, secret, "[REDACTED]")
	}
	var clean func(any) any
	clean = func(value any) any {
		switch v := value.(type) {
		case string:
			return strings.ReplaceAll(v, secret, "[REDACTED]")
		case []any:
			for i := range v {
				v[i] = clean(v[i])
			}
			return v
		case map[string]any:
			result := make(map[string]any, len(v))
			for k, item := range v {
				result[strings.ReplaceAll(k, secret, "[REDACTED]")] = clean(item)
			}
			return result
		default:
			return value
		}
	}
	data, err := json.Marshal(clean(value))
	if err != nil {
		return "[unavailable]"
	}
	return string(data)
}
