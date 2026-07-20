package appapi

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
)

func writeTestConfigWithRules(t *testing.T, working, library, logDir string, rules []config.Rule, categories map[string]config.Category) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Config{
		General: config.General{
			WorkingFolder:  working,
			LibraryFolder:  library,
			LogFolder:      logDir,
			FilenameFormat: "{title} ({year}) - {author}",
		},
		Categories: categories,
		Rules:      rules,
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	return path
}

func TestRecompute_AppliesCategorizationAndDestPath(t *testing.T) {
	working := t.TempDir()
	library := filepath.Join(t.TempDir(), "library")
	logDir := filepath.Join(t.TempDir(), "logs")
	configPath := writeTestConfigWithRules(t, working, library, logDir,
		[]config.Rule{{MatchField: "author", MatchValue: "Isaac Asimov", Category: "Fiction", Subcategory: "Sci-Fi"}},
		map[string]config.Category{"Fiction": {Subcategories: []string{"Sci-Fi"}}},
	)

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	edited := BookView{
		SourcePath: filepath.Join(working, "some.epub"),
		Title:      Field{Value: "Foundation", Source: "Edited"},
		Author:     Field{Value: "Isaac Asimov", Source: "Edited"},
		Year:       Field{Value: "1951", Source: "Edited"},
	}

	got, err := app.Recompute(edited)
	if err != nil {
		t.Fatalf("Recompute returned error: %v", err)
	}
	if got.Category != "Fiction" || got.Subcategory != "Sci-Fi" {
		t.Errorf("Category/Subcategory = %q/%q, want Fiction/Sci-Fi", got.Category, got.Subcategory)
	}
	if got.Status != "Edited" {
		t.Errorf("Status = %q, want Edited", got.Status)
	}
	wantDestFragment := filepath.Join("Fiction", "Sci-Fi", "Foundation (1951) - Isaac Asimov.epub")
	if !strings.HasSuffix(got.DestPath, wantDestFragment) {
		t.Errorf("DestPath = %q, want suffix %q", got.DestPath, wantDestFragment)
	}
}

func TestRecompute_PartialStatusWhenAuthorMissing(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"))

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	edited := BookView{
		SourcePath: filepath.Join(working, "some.epub"),
		Title:      Field{Value: "Foundation", Source: "Edited"},
		Author:     Field{Value: "", Source: "Unresolved"},
		Year:       Field{Value: "1951", Source: "Edited"},
	}

	got, err := app.Recompute(edited)
	if err != nil {
		t.Fatalf("Recompute returned error: %v", err)
	}
	if got.Status != "Partial" {
		t.Errorf("Status = %q, want Partial", got.Status)
	}
}

func TestRecompute_ManualCategoryPickSurvivesRecompute(t *testing.T) {
	working := t.TempDir()
	library := filepath.Join(t.TempDir(), "library")
	logDir := filepath.Join(t.TempDir(), "logs")
	configPath := writeTestConfigWithRules(t, working, library, logDir,
		nil, // no rules that would match -- would fall back to Uncategorized without the manual override
		map[string]config.Category{
			"Fiction":       {Subcategories: []string{"Sci-Fi"}},
			"Uncategorized": {},
		},
	)

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	edited := BookView{
		SourcePath:     filepath.Join(working, "some.epub"),
		Title:          Field{Value: "Foundation", Source: "Metadata"},
		Author:         Field{Value: "Someone Unmatched", Source: "Metadata"},
		Year:           Field{Value: "1951", Source: "Metadata"},
		Category:       "Fiction",
		Subcategory:    "Sci-Fi",
		CategoryManual: true,
	}

	got, err := app.Recompute(edited)
	if err != nil {
		t.Fatalf("Recompute returned error: %v", err)
	}
	if got.Category != "Fiction" || got.Subcategory != "Sci-Fi" {
		t.Errorf("Category/Subcategory = %q/%q, want Fiction/Sci-Fi (manual pick preserved)", got.Category, got.Subcategory)
	}
	if got.Status != "Edited" {
		t.Errorf("Status = %q, want Edited", got.Status)
	}
	wantDestFragment := filepath.Join("Fiction", "Sci-Fi", "Foundation (1951) - Someone Unmatched.epub")
	if !strings.HasSuffix(got.DestPath, wantDestFragment) {
		t.Errorf("DestPath = %q, want suffix %q", got.DestPath, wantDestFragment)
	}
}
