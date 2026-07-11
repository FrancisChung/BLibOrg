package appapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
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

func TestDefaultConfigPath_EndsWithExpectedSuffix(t *testing.T) {
	path, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath returned error: %v", err)
	}
	want := filepath.Join("book-organiser", "config.yaml")
	if filepath.Base(filepath.Dir(path)) != "book-organiser" || filepath.Base(path) != "config.yaml" {
		t.Errorf("DefaultConfigPath() = %q, want a path ending in %q", path, want)
	}
}

var _ = os.TempDir // keep os imported for future tests in this file
