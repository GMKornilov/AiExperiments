package llm

import "encoding/json"

// MaxSafeTokens is the largest integer represented exactly by browser JSON numbers.
const MaxSafeTokens int64 = 1<<53 - 1

// Usage contains only the provider's confirmed input and output counts.
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
}

func (u Usage) Valid() bool {
	return u.PromptTokens >= 0 && u.CompletionTokens >= 0 && u.PromptTokens <= MaxSafeTokens && u.CompletionTokens <= MaxSafeTokens-u.PromptTokens
}

// Completion can retain confirmed usage even when the answer cannot be used.
type Completion struct {
	Text  string
	Usage *Usage
}

func parseUsage(raw json.RawMessage) *Usage {
	var fields struct {
		Prompt     *int64 `json:"prompt_tokens"`
		Completion *int64 `json:"completion_tokens"`
	}
	if json.Unmarshal(raw, &fields) != nil || fields.Prompt == nil || fields.Completion == nil {
		return nil
	}
	u := Usage{PromptTokens: *fields.Prompt, CompletionTokens: *fields.Completion}
	if !u.Valid() {
		return nil
	}
	return &u
}
