package appapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/BLibOrg/internal/config"
)

func writeTestConfig(t *testing.T, working, library, logDir string) string {
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
		Categories: map[string]config.Category{"Uncategorized": {}},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	return path
}

func TestConfigStatus_ValidConfig(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"))

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	status := app.ConfigStatus()
	if status.Path != configPath {
		t.Errorf("Path = %q, want %q", status.Path, configPath)
	}
	if status.Error != "" {
		t.Errorf("Error = %q, want empty", status.Error)
	}
}

func TestConfigStatus_MissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	app := NewApp()
	app.configPath = func() (string, error) { return missing, nil }

	status := app.ConfigStatus()
	if status.Path != missing {
		t.Errorf("Path = %q, want %q", status.Path, missing)
	}
	if status.Error == "" {
		t.Error("Error should be non-empty when the config file doesn't exist")
	}
}

func TestConfigStatus_InvalidRuleRegexSurfacedAsWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Config{
		General: config.General{
			WorkingFolder:  dir,
			LibraryFolder:  filepath.Join(dir, "library"),
			LogFolder:      filepath.Join(dir, "logs"),
			FilenameFormat: "{title} ({year}) - {author}",
		},
		Categories: map[string]config.Category{"Uncategorized": {}},
		Rules: []config.Rule{
			{MatchField: "filename", MatchValue: `(?i)\bc++\b`, Category: "Technology"},
		},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}

	app := NewApp()
	app.configPath = func() (string, error) { return path, nil }

	status := app.ConfigStatus()
	if status.Error != "" {
		t.Errorf("Error = %q, want empty (an invalid rule regex is a warning, not a load failure)", status.Error)
	}
	if len(status.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want exactly 1", status.Warnings)
	}
}

func TestDefaultConfigPath_EndsWithExpectedSuffix(t *testing.T) {
	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath returned error: %v", err)
	}
	want := filepath.Join("BLibOrg", "config.yaml")
	if filepath.Base(filepath.Dir(path)) != "BLibOrg" || filepath.Base(path) != "config.yaml" {
		t.Errorf("DefaultConfigPath() = %q, want a path ending in %q", path, want)
	}
}

func TestCategories_ReturnsSortedDestinationsExcludingUncategorized(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfigWithRules(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"),
		nil,
		map[string]config.Category{
			"Technology":    {Subcategories: []string{"Python", "Java"}},
			"Food":          {Subcategories: []string{"Mexican"}},
			"Fiction":       {},
			"Uncategorized": {},
		},
	)

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	got, err := app.Categories()
	if err != nil {
		t.Fatalf("Categories returned error: %v", err)
	}
	want := []DestinationView{
		{Category: "Fiction", Subcategory: ""},
		{Category: "Food", Subcategory: "Mexican"},
		{Category: "Technology", Subcategory: "Java"},
		{Category: "Technology", Subcategory: "Python"},
	}
	if len(got) != len(want) {
		t.Fatalf("Categories() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Categories()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

var _ = os.TempDir // keep os imported for future tests in this file
