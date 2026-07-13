package appapi

import (
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/pipeline"
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
