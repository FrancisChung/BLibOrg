package appapi

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/FrancisChung/BLibOrg/internal/book"
	"github.com/FrancisChung/BLibOrg/internal/operations"
	"github.com/FrancisChung/BLibOrg/internal/pipeline"
)

func TestListCategoryWarnings_EmptyFileReturnsEmptySlice(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"))
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	warnings, err := app.ListCategoryWarnings()
	if err != nil {
		t.Fatalf("ListCategoryWarnings error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("got %d warnings, want 0 for a log that's never been written", len(warnings))
	}
}

func TestListCategoryWarnings_ReturnsWrittenEntries(t *testing.T) {
	working := t.TempDir()
	logDir := filepath.Join(t.TempDir(), "logs")
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), logDir)
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	cfg, err := app.loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	books := []*book.Book{
		{SourcePath: "/inbox/a.epub", Category: "Fiction", Subcategory: "SpaceOpera", CategoryWarning: `rule matched undeclared subcategory "SpaceOpera" under category "Fiction"`},
	}
	if err := pipeline.LogCategoryWarnings(books, cfg); err != nil {
		t.Fatalf("LogCategoryWarnings: %v", err)
	}

	warnings, err := app.ListCategoryWarnings()
	if err != nil {
		t.Fatalf("ListCategoryWarnings error: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1", len(warnings))
	}
	if warnings[0].SourcePath != "/inbox/a.epub" {
		t.Errorf("SourcePath = %q, want /inbox/a.epub", warnings[0].SourcePath)
	}
	if warnings[0].Category != "Fiction" || warnings[0].Subcategory != "SpaceOpera" {
		t.Errorf("Category/Subcategory = %s/%s, want Fiction/SpaceOpera", warnings[0].Category, warnings[0].Subcategory)
	}
	if warnings[0].Warning != books[0].CategoryWarning {
		t.Errorf("Warning = %q, want %q", warnings[0].Warning, books[0].CategoryWarning)
	}
	if warnings[0].Timestamp == "" {
		t.Error("expected a non-empty Timestamp")
	}
}

func TestListOperationBatches_EmptyLogReturnsEmptySlice(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"))
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	batches, err := app.ListOperationBatches()
	if err != nil {
		t.Fatalf("ListOperationBatches error: %v", err)
	}
	if len(batches) != 0 {
		t.Errorf("got %d batches, want 0 for a log that's never been written", len(batches))
	}
}

func TestListOperationBatches_GroupsEntriesByBatchNewestFirst(t *testing.T) {
	working := t.TempDir()
	logDir := filepath.Join(t.TempDir(), "logs")
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), logDir)
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	opsLog := operations.NewLog(filepath.Join(logDir, "ops.jsonl"))
	older := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	if err := opsLog.Append([]operations.LogEntry{
		{BatchID: "20260701-1", Timestamp: older, OpType: operations.OpMove, OldPath: "/inbox/a.epub", NewPath: "/library/a.epub"},
		{BatchID: "20260701-1", Timestamp: older, OpType: operations.OpMove, OldPath: "/inbox/b.epub", NewPath: "/library/b.epub", Undone: true},
	}); err != nil {
		t.Fatalf("seed batch 1: %v", err)
	}
	if err := opsLog.Append([]operations.LogEntry{
		{BatchID: "20260713-1", Timestamp: newer, OpType: operations.OpMove, OldPath: "/inbox/c.epub", NewPath: "/library/c.epub"},
	}); err != nil {
		t.Fatalf("seed batch 2: %v", err)
	}

	batches, err := app.ListOperationBatches()
	if err != nil {
		t.Fatalf("ListOperationBatches error: %v", err)
	}
	if len(batches) != 2 {
		t.Fatalf("got %d batches, want 2", len(batches))
	}
	if batches[0].BatchID != "20260713-1" {
		t.Errorf("batches[0].BatchID = %q, want 20260713-1 (newest first)", batches[0].BatchID)
	}
	if batches[1].BatchID != "20260701-1" {
		t.Errorf("batches[1].BatchID = %q, want 20260701-1", batches[1].BatchID)
	}

	olderBatch := batches[1]
	if olderBatch.EntryCount != 2 {
		t.Errorf("EntryCount = %d, want 2", olderBatch.EntryCount)
	}
	if olderBatch.UndoneCount != 1 {
		t.Errorf("UndoneCount = %d, want 1", olderBatch.UndoneCount)
	}
	if len(olderBatch.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(olderBatch.Entries))
	}
	if olderBatch.Entries[0].OldPath != "/inbox/a.epub" || olderBatch.Entries[0].NewPath != "/library/a.epub" {
		t.Errorf("Entries[0] = %+v, want a.epub move", olderBatch.Entries[0])
	}
	if !olderBatch.Entries[1].Undone {
		t.Errorf("Entries[1].Undone = %v, want true", olderBatch.Entries[1].Undone)
	}
}
