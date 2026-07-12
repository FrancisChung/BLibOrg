package operations

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLog_ListBatches_GroupsEntriesByBatchID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.jsonl")
	log := NewLog(path)

	if err := log.Append([]LogEntry{
		{BatchID: "b1", Timestamp: time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC), OldPath: "/a", NewPath: "/b"},
		{BatchID: "b1", Timestamp: time.Date(2026, 7, 10, 9, 0, 1, 0, time.UTC), OldPath: "/c", NewPath: "/d"},
		{BatchID: "b2", Timestamp: time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC), OldPath: "/e", NewPath: "/f"},
	}); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	summaries, err := log.ListBatches()
	if err != nil {
		t.Fatalf("ListBatches error: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("got %d batch summaries, want 2", len(summaries))
	}
	if summaries[0].BatchID != "b1" || summaries[0].EntryCount != 2 {
		t.Errorf("summaries[0] = %+v, want BatchID=b1 EntryCount=2", summaries[0])
	}
	if !summaries[0].Timestamp.Equal(time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("summaries[0].Timestamp = %v, want the first entry's timestamp", summaries[0].Timestamp)
	}
	if summaries[1].BatchID != "b2" || summaries[1].EntryCount != 1 {
		t.Errorf("summaries[1] = %+v, want BatchID=b2 EntryCount=1", summaries[1])
	}
}

func TestLog_ListBatches_TracksUndoneCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.jsonl")
	log := NewLog(path)

	if err := log.Append([]LogEntry{
		{BatchID: "b1", OldPath: "/a", NewPath: "/b"},
		{BatchID: "b1", OldPath: "/c", NewPath: "/d"},
	}); err != nil {
		t.Fatalf("Append error: %v", err)
	}
	if err := log.SetEntryUndone("b1", "/a", "/b", true); err != nil {
		t.Fatalf("SetEntryUndone error: %v", err)
	}

	summaries, err := log.ListBatches()
	if err != nil {
		t.Fatalf("ListBatches error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("got %d summaries, want 1", len(summaries))
	}
	if summaries[0].EntryCount != 2 || summaries[0].UndoneCount != 1 {
		t.Errorf("summaries[0] = %+v, want EntryCount=2 UndoneCount=1", summaries[0])
	}
}

func TestLog_ListBatches_EmptyLogReturnsNoSummaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.jsonl")
	log := NewLog(path)

	summaries, err := log.ListBatches()
	if err != nil {
		t.Fatalf("ListBatches error: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("got %d summaries for an empty log, want 0", len(summaries))
	}
}

func withFixedNow(t *testing.T, fixed time.Time) {
	t.Helper()
	original := nowFunc
	nowFunc = func() time.Time { return fixed }
	t.Cleanup(func() { nowFunc = original })
}

func TestNextBatchID_FirstOfDay(t *testing.T) {
	withFixedNow(t, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	path := filepath.Join(t.TempDir(), "ops.jsonl")
	log := NewLog(path)

	id, err := NextBatchID(log)
	if err != nil {
		t.Fatalf("NextBatchID error: %v", err)
	}
	if id != "20260710-1" {
		t.Errorf("id = %q, want %q", id, "20260710-1")
	}
}

func TestNextBatchID_IncrementsFromExistingBatchesToday(t *testing.T) {
	withFixedNow(t, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	path := filepath.Join(t.TempDir(), "ops.jsonl")
	log := NewLog(path)
	if err := log.Append([]LogEntry{
		{BatchID: "20260710-1", OldPath: "/a", NewPath: "/b"},
		{BatchID: "20260710-2", OldPath: "/c", NewPath: "/d"},
	}); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	id, err := NextBatchID(log)
	if err != nil {
		t.Fatalf("NextBatchID error: %v", err)
	}
	if id != "20260710-3" {
		t.Errorf("id = %q, want %q", id, "20260710-3")
	}
}

func TestNextBatchID_IgnoresBatchesFromOtherDatesOrFormats(t *testing.T) {
	withFixedNow(t, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	path := filepath.Join(t.TempDir(), "ops.jsonl")
	log := NewLog(path)
	if err := log.Append([]LogEntry{
		{BatchID: "20260709-5", OldPath: "/a", NewPath: "/b"},  // yesterday
		{BatchID: "manual-test", OldPath: "/c", NewPath: "/d"}, // non-date format
	}); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	id, err := NextBatchID(log)
	if err != nil {
		t.Fatalf("NextBatchID error: %v", err)
	}
	if id != "20260710-1" {
		t.Errorf("id = %q, want %q (should ignore yesterday's and non-date-format batches)", id, "20260710-1")
	}
}
