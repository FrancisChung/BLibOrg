package pipeline

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/BLibOrg/internal/book"
	"github.com/FrancisChung/BLibOrg/internal/config"
)

func warningsTestConfig(logFolder string) config.Config {
	return config.Config{General: config.General{LogFolder: logFolder}}
}

func TestLogCategoryWarnings_WritesEntryForBooksWithWarning(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	books := []*book.Book{
		{SourcePath: "/inbox/a.epub", Category: "Fiction", Subcategory: "SpaceOpera", CategoryWarning: `rule matched undeclared subcategory "SpaceOpera" under category "Fiction"`},
	}

	if err := LogCategoryWarnings(books, warningsTestConfig(logDir)); err != nil {
		t.Fatalf("LogCategoryWarnings error: %v", err)
	}

	logPath := filepath.Join(logDir, "category-warnings.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected log file to exist: %v", err)
	}

	var entry CategoryWarningEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("unmarshal entry: %v (data: %s)", err, data)
	}
	if entry.SourcePath != "/inbox/a.epub" {
		t.Errorf("SourcePath = %q, want /inbox/a.epub", entry.SourcePath)
	}
	if entry.Category != "Fiction" || entry.Subcategory != "SpaceOpera" {
		t.Errorf("Category/Subcategory = %s/%s, want Fiction/SpaceOpera", entry.Category, entry.Subcategory)
	}
	if entry.Warning != books[0].CategoryWarning {
		t.Errorf("Warning = %q, want %q", entry.Warning, books[0].CategoryWarning)
	}
	if entry.Timestamp.IsZero() {
		t.Error("expected a non-zero Timestamp")
	}
}

func TestLogCategoryWarnings_SkipsBooksWithoutWarning(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	books := []*book.Book{
		{SourcePath: "/inbox/a.epub", Category: "Fiction", Subcategory: "Sci-Fi"},
		{SourcePath: "/inbox/b.epub", Category: "Fiction", Subcategory: "SpaceOpera", CategoryWarning: "undeclared"},
	}

	if err := LogCategoryWarnings(books, warningsTestConfig(logDir)); err != nil {
		t.Fatalf("LogCategoryWarnings error: %v", err)
	}

	logPath := filepath.Join(logDir, "category-warnings.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected log file to exist: %v", err)
	}
	if got := countLines(data); got != 1 {
		t.Errorf("got %d log lines, want 1 (only the book with a warning)", got)
	}
}

func TestLogCategoryWarnings_ErrorsClearlyWhenLogFolderUnset(t *testing.T) {
	books := []*book.Book{
		{SourcePath: "/inbox/a.epub", CategoryWarning: "undeclared"},
	}

	err := LogCategoryWarnings(books, warningsTestConfig(""))
	if err == nil {
		t.Fatal("expected an error when LogFolder is unset and there are warnings to log")
	}
	if !errors.Is(err, ErrLogFolderNotConfigured) {
		t.Errorf("error = %v, want it to wrap/match ErrLogFolderNotConfigured", err)
	}
}

func TestLogCategoryWarnings_NoErrorWhenLogFolderUnsetAndNoWarnings(t *testing.T) {
	books := []*book.Book{
		{SourcePath: "/inbox/a.epub"},
	}

	if err := LogCategoryWarnings(books, warningsTestConfig("")); err != nil {
		t.Errorf("expected no error when there's nothing to log, even with LogFolder unset, got: %v", err)
	}
}

func TestLogCategoryWarnings_NoOpWhenNoWarnings(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	books := []*book.Book{
		{SourcePath: "/inbox/a.epub", Category: "Fiction", Subcategory: "Sci-Fi"},
	}

	if err := LogCategoryWarnings(books, warningsTestConfig(logDir)); err != nil {
		t.Fatalf("LogCategoryWarnings error: %v", err)
	}

	logPath := filepath.Join(logDir, "category-warnings.jsonl")
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("expected no log file to be created when no books have a warning, stat err = %v", err)
	}
}

func TestLogCategoryWarnings_AppendsAcrossMultipleCalls(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	cfg := warningsTestConfig(logDir)

	first := []*book.Book{{SourcePath: "/inbox/a.epub", CategoryWarning: "first"}}
	second := []*book.Book{{SourcePath: "/inbox/b.epub", CategoryWarning: "second"}}

	if err := LogCategoryWarnings(first, cfg); err != nil {
		t.Fatalf("first LogCategoryWarnings error: %v", err)
	}
	if err := LogCategoryWarnings(second, cfg); err != nil {
		t.Fatalf("second LogCategoryWarnings error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(logDir, "category-warnings.jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if got := countLines(data); got != 2 {
		t.Errorf("got %d log lines across two calls, want 2 (append, not overwrite)", got)
	}
}

func TestReadCategoryWarnings_MissingFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.jsonl")
	entries, err := ReadCategoryWarnings(path)
	if err != nil {
		t.Fatalf("ReadCategoryWarnings error: %v", err)
	}
	if entries != nil {
		t.Errorf("entries = %v, want nil for a missing file", entries)
	}
}

func TestReadCategoryWarnings_ReadsBackWhatWasWritten(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	books := []*book.Book{
		{SourcePath: "/inbox/a.epub", Category: "Fiction", Subcategory: "SpaceOpera", CategoryWarning: `rule matched undeclared subcategory "SpaceOpera" under category "Fiction"`},
		{SourcePath: "/inbox/b.epub", Category: "NonFiction", Subcategory: "History2", CategoryWarning: `rule matched undeclared subcategory "History2" under category "NonFiction"`},
	}
	if err := LogCategoryWarnings(books, warningsTestConfig(logDir)); err != nil {
		t.Fatalf("LogCategoryWarnings error: %v", err)
	}

	entries, err := ReadCategoryWarnings(filepath.Join(logDir, "category-warnings.jsonl"))
	if err != nil {
		t.Fatalf("ReadCategoryWarnings error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].SourcePath != "/inbox/a.epub" || entries[1].SourcePath != "/inbox/b.epub" {
		t.Errorf("entries in unexpected order/content: %+v", entries)
	}
	if entries[0].Warning != books[0].CategoryWarning {
		t.Errorf("Warning = %q, want %q", entries[0].Warning, books[0].CategoryWarning)
	}
	if entries[0].Timestamp.IsZero() {
		t.Error("expected a non-zero Timestamp")
	}
}

func countLines(data []byte) int {
	n := 0
	for _, line := range splitLines(data) {
		if len(line) > 0 {
			n++
		}
	}
	return n
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
