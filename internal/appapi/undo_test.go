package appapi

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/BLibOrg/internal/operations"
)

func TestUndoBatch_RestoresFileToOriginalLocation(t *testing.T) {
	working := t.TempDir()
	library := filepath.Join(t.TempDir(), "library")
	logDir := filepath.Join(t.TempDir(), "logs")

	srcPath := filepath.Join(working, "book.epub")
	if err := os.WriteFile(srcPath, []byte("x"), 0644); err != nil {
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
	}
	result, err := app.Apply(books)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if _, err := os.Stat(destPath); err != nil {
		t.Fatalf("expected file at destPath before undo: %v", err)
	}

	if err := app.UndoBatch(result.BatchID); err != nil {
		t.Fatalf("UndoBatch returned error: %v", err)
	}

	if _, err := os.Stat(srcPath); err != nil {
		t.Errorf("expected file restored to %s, stat error: %v", srcPath, err)
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Errorf("expected %s to no longer exist after undo, stat error: %v", destPath, err)
	}
}

func TestUndoBatch_UnknownBatchIDIsNoOp(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"))
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	if err := app.UndoBatch("no-such-batch"); err != nil {
		t.Fatalf("UndoBatch on unknown batch should be a no-op, got error: %v", err)
	}
}

func TestUndoBatch_PropagatesManagerError(t *testing.T) {
	working := t.TempDir()
	library := filepath.Join(t.TempDir(), "library")
	logDir := filepath.Join(t.TempDir(), "logs")

	srcPath := filepath.Join(working, "book.epub")
	if err := os.WriteFile(srcPath, []byte("x"), 0644); err != nil {
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
	}
	result, err := app.Apply(books)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// Recreate a file at the original source path so undo's attempt to move
	// the file back there fails with ErrDestinationExists instead of
	// silently succeeding or being swallowed.
	if err := os.WriteFile(srcPath, []byte("something else now lives here"), 0644); err != nil {
		t.Fatalf("write blocking fixture: %v", err)
	}

	err = app.UndoBatch(result.BatchID)
	if err == nil {
		t.Fatal("expected UndoBatch to return an error when the original path is occupied")
	}
	if !errors.Is(err, operations.ErrDestinationExists) {
		t.Errorf("expected error to wrap ErrDestinationExists, got: %v", err)
	}
}
