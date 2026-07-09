package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
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
