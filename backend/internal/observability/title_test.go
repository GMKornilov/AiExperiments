package observability

import "testing"

func TestJournalRedactsBothSnapshotCredentials(t *testing.T) {
	j := NewJournal(true, nil)
	j.SetSecrets("d", "chat-secret", "text-secret")
	j.Log(Record{DialogID: "d", Text: "chat-secret text-secret answer"}, "")
	if got := j.Logs("d")[0].Text; got != "[REDACTED] [REDACTED] answer" {
		t.Fatalf("%q", got)
	}
}
