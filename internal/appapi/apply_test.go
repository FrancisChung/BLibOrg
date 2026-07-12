package appapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApply_MovesResolvedFilesAndSkipsUnresolved(t *testing.T) {
	working := t.TempDir()
	library := filepath.Join(t.TempDir(), "library")
	logDir := filepath.Join(t.TempDir(), "logs")

	srcPath := filepath.Join(working, "book.epub")
	if err := os.WriteFile(srcPath, []byte("x"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	unresolvedSrc := filepath.Join(working, "mystery.epub")
	if err := os.WriteFile(unresolvedSrc, []byte("x"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	destPath := filepath.Join(library, "Uncategorized", "Foundation (1951) - Isaac Asimov.epub")

	configPath := writeTestConfig(t, working, library, logDir)
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	books := []BookView{
		{
			SourcePath: srcPath,
			Title:      Field{Value: "Foundation", Source: "Edited"},
			Author:     Field{Value: "Isaac Asimov", Source: "Edited"},
			Year:       Field{Value: "1951", Source: "Edited"},
			DestPath:   destPath,
		},
		{
			SourcePath: unresolvedSrc,
			Title:      Field{Value: "", Source: "Unresolved"},
			DestPath:   "",
		},
	}

	result, err := app.Apply(books)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.BatchID == "" {
		t.Error("expected a non-empty BatchID")
	}
	if len(result.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(result.Results))
	}

	byPath := map[string]ApplyResultEntry{}
	for _, r := range result.Results {
		byPath[r.SourcePath] = r
	}
	if got := byPath[srcPath]; !got.OK || got.Skipped {
		t.Errorf("result for %s = %+v, want OK=true Skipped=false", srcPath, got)
	}
	if got := byPath[unresolvedSrc]; !got.Skipped {
		t.Errorf("result for %s = %+v, want Skipped=true", unresolvedSrc, got)
	}

	if _, err := os.Stat(destPath); err != nil {
		t.Errorf("expected file at %s, stat error: %v", destPath, err)
	}
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Errorf("expected %s to no longer exist, stat error: %v", srcPath, err)
	}
	if _, err := os.Stat(unresolvedSrc); err != nil {
		t.Errorf("unresolved file should not have been moved: %v", err)
	}
}

func TestApply_NoResolvedRowsReturnsAllSkippedNoError(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"))
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	books := []BookView{
		{SourcePath: filepath.Join(working, "a.epub"), Title: Field{Source: "Unresolved"}},
	}

	result, err := app.Apply(books)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(result.Results) != 1 || !result.Results[0].Skipped {
		t.Errorf("Results = %+v, want a single Skipped entry", result.Results)
	}
}
