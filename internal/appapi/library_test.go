package appapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
)

func writeTestConfigForLibrary(t *testing.T, libraryFolder, logFolder string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Config{
		General:    config.General{LibraryFolder: libraryFolder, LogFolder: logFolder},
		Categories: map[string]config.Category{"Uncategorized": {}},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return path
}

func TestListLibrary_ReturnsBooksGroupedByCategory(t *testing.T) {
	libDir := t.TempDir()
	fictionSciFi := filepath.Join(libDir, "Fiction", "Sci-Fi", "Foundation.epub")
	if err := os.MkdirAll(filepath.Dir(fictionSciFi), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fictionSciFi, []byte("not a real epub"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	configPath := writeTestConfigForLibrary(t, libDir, t.TempDir())
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	view, err := app.ListLibrary(false, nil)
	if err != nil {
		t.Fatalf("ListLibrary returned error: %v", err)
	}
	if len(view.Books) != 1 {
		t.Fatalf("len(Books) = %d, want 1", len(view.Books))
	}
	if view.Books[0].Category != "Fiction" || view.Books[0].Subcategory != "Sci-Fi" {
		t.Errorf("Category/Subcategory = %q/%q, want Fiction/Sci-Fi", view.Books[0].Category, view.Books[0].Subcategory)
	}
	if view.Books[0].CoverOverridden {
		t.Error("CoverOverridden = true, want false (no override set for this fixture)")
	}
	if len(view.Categories) != 1 || view.Categories[0] != "Fiction" {
		t.Errorf("Categories = %v, want [Fiction]", view.Categories)
	}
}

func TestListLibrary_EmptyLibraryReturnsEmptyView(t *testing.T) {
	configPath := writeTestConfigForLibrary(t, t.TempDir(), t.TempDir())
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	view, err := app.ListLibrary(false, nil)
	if err != nil {
		t.Fatalf("ListLibrary returned error: %v", err)
	}
	if len(view.Books) != 0 {
		t.Errorf("len(Books) = %d, want 0", len(view.Books))
	}
	if len(view.Categories) != 0 {
		t.Errorf("len(Categories) = %d, want 0", len(view.Categories))
	}
}

func TestListLibrary_ForwardsProgressToCallback(t *testing.T) {
	libDir := t.TempDir()
	for _, rel := range []string{
		filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"),
		filepath.Join("Fiction", "Fantasy", "Mistborn.epub"),
	} {
		path := filepath.Join(libDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("not a real epub"), 0644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	configPath := writeTestConfigForLibrary(t, libDir, t.TempDir())
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	var calls int
	var lastTotal int
	_, err := app.ListLibrary(false, func(done, total int) {
		calls++
		lastTotal = total
	})
	if err != nil {
		t.Fatalf("ListLibrary returned error: %v", err)
	}

	if calls != 2 {
		t.Errorf("onProgress called %d times, want 2", calls)
	}
	if lastTotal != 2 {
		t.Errorf("last total = %d, want 2", lastTotal)
	}
}
