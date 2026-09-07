package observability

import "testing"

func TestJournalRedactsAndDoesNotResurrectDeletedDialog(t *testing.T) {
	j := NewJournal(true, nil)
	j.SetSecret("dialog", "credential")
	j.Log(Record{DialogID: "dialog", Text: "credential user answer"}, "")
	logs := j.Logs("dialog")
	if len(logs) != 1 || logs[0].Text != "[REDACTED] user answer" {
		t.Fatalf("logs = %#v", logs)
	}
	j.DeleteDialog("dialog")
	j.Log(Record{DialogID: "dialog", Text: "late"}, "")
	if got := j.Logs("dialog"); len(got) != 0 {
		t.Fatalf("late logs = %#v", got)
	}
}

func TestJournalHidesTextWhenDisabled(t *testing.T) {
	j := NewJournal(false, nil)
	j.Log(Record{DialogID: "d", Text: "private"}, "secret")
	if got := j.Logs("d")[0].Text; got != "" {
		t.Fatalf("text=%q", got)
	}
}
