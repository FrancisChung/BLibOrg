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

func TestLog_AppendCreatesMissingParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "does-not-exist-yet", "ops.jsonl")
	log := NewLog(path)

	if err := log.Append([]LogEntry{{BatchID: "b1", OldPath: "/a", NewPath: "/b"}}); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	got, err := log.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ReadAll returned %d entries, want 1", len(got))
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

func TestLog_SetEntryUndone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.jsonl")
	log := NewLog(path)
	if err := log.Append([]LogEntry{
		{BatchID: "b1", OldPath: "/a", NewPath: "/b"},
		{BatchID: "b1", OldPath: "/c", NewPath: "/d"},
	}); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	// Flip only the second entry, identified by its (OldPath, NewPath)
	// pair, and confirm the first entry -- which shares BatchID but not
	// OldPath/NewPath -- is left untouched.
	if err := log.SetEntryUndone("b1", "/c", "/d", true); err != nil {
		t.Fatalf("SetEntryUndone error: %v", err)
	}

	got, err := log.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	for _, e := range got {
		if e.OldPath == "/a" && e.Undone {
			t.Errorf("expected entry /a->/b to remain not-Undone, got Undone=true")
		}
		if e.OldPath == "/c" && !e.Undone {
			t.Errorf("expected entry /c->/d to be marked Undone")
		}
	}
}

func TestLog_SetEntryUndoneUnknownEntryIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.jsonl")
	log := NewLog(path)
	if err := log.Append([]LogEntry{
		{BatchID: "b1", OldPath: "/a", NewPath: "/b"},
	}); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	if err := log.SetEntryUndone("b1", "/no-such-old", "/no-such-new", true); err != nil {
		t.Fatalf("SetEntryUndone on unknown entry should be a no-op, got error: %v", err)
	}

	got, err := log.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(got) != 1 || got[0].Undone {
		t.Errorf("expected the unrelated existing entry to remain unmodified, got: %+v", got)
	}
}
