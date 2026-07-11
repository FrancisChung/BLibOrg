package appapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan_ReturnsBookViewsForScannedFiles(t *testing.T) {
	working := t.TempDir()
	library := filepath.Join(t.TempDir(), "library")
	logDir := filepath.Join(t.TempDir(), "logs")
	if err := os.WriteFile(filepath.Join(working, "Some Book - John Doe (2020).epub"), []byte("not a real zip"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	configPath := writeTestConfig(t, working, library, logDir)

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	views, err := app.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("got %d views, want 1", len(views))
	}
	v := views[0]
	if v.Title.Value != "Some Book" {
		t.Errorf("Title = %q, want %q", v.Title.Value, "Some Book")
	}
	if v.Author.Value != "John Doe" {
		t.Errorf("Author = %q, want %q", v.Author.Value, "John Doe")
	}
	if v.Year.Value != "2020" {
		t.Errorf("Year = %q, want 2020", v.Year.Value)
	}
	if v.DestPath == "" {
		t.Error("DestPath should not be empty")
	}
}

func TestScan_ErrorsWhenWorkingFolderMissing(t *testing.T) {
	configPath := writeTestConfig(t, filepath.Join(t.TempDir(), "does-not-exist"), filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"))
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	if _, err := app.Scan(); err == nil {
		t.Error("expected error when working folder doesn't exist, got nil")
	}
}

func TestScan_EmptyWorkingFolderReturnsEmptySlice(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"))
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	views, err := app.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(views) != 0 {
		t.Errorf("got %d views, want 0", len(views))
	}
}
