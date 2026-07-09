package operations

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLog_AppendAndReadAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.jsonl")
	log := NewLog(path)

	entries := []LogEntry{
		{BatchID: "b1", Timestamp: time.Now(), OpType: OpMove, OldPath: "/a", NewPath: "/b", Undone: false},
		{BatchID: "b1", Timestamp: time.Now(), OpType: OpMove, OldPath: "/c", NewPath: "/d", Undone: false},
	}
	if err := log.Append(entries); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	got, err := log.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ReadAll returned %d entries, want 2", len(got))
	}
	if got[0].OldPath != "/a" || got[1].OldPath != "/c" {
		t.Errorf("unexpected entries: %+v", got)
	}
}

func TestLog_ReadAllOnMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.jsonl")
	log := NewLog(path)
	got, err := log.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll error on missing file: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no entries, got %d", len(got))
	}
}

func TestLog_SetBatchUndone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.jsonl")
	log := NewLog(path)
	if err := log.Append([]LogEntry{
		{BatchID: "b1", OldPath: "/a", NewPath: "/b"},
		{BatchID: "b2", OldPath: "/e", NewPath: "/f"},
	}); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	if err := log.SetBatchUndone("b1", true); err != nil {
		t.Fatalf("SetBatchUndone error: %v", err)
	}

	got, err := log.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	for _, e := range got {
		if e.BatchID == "b1" && !e.Undone {
			t.Errorf("expected b1 entries to be marked Undone")
		}
		if e.BatchID == "b2" && e.Undone {
			t.Errorf("expected b2 entries to remain not-Undone")
		}
	}
}
