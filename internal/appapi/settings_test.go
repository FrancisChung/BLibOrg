package appapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
)

func writeTestConfigForSettings(t *testing.T, logFolder string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(configPath, config.Config{General: config.General{LogFolder: logFolder}}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return configPath
}

func TestResetCoverCache_RemovesCoversDirAndLibraryCache(t *testing.T) {
	logFolder := t.TempDir()
	coversDir := covercache.Dir(logFolder)
	if err := os.MkdirAll(coversDir, 0755); err != nil {
		t.Fatalf("mkdir covers: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coversDir, "abc123.jpg"), []byte("cached-cover"), 0644); err != nil {
		t.Fatalf("write fixture cover: %v", err)
	}
	libraryCachePath := filepath.Join(logFolder, "library-cache.json")
	if err := os.WriteFile(libraryCachePath, []byte(`{"/book.epub":{}}`), 0644); err != nil {
		t.Fatalf("write fixture library cache: %v", err)
	}

	app := NewApp()
	app.configPath = func() (string, error) { return writeTestConfigForSettings(t, logFolder), nil }

	if err := app.ResetCoverCache(); err != nil {
		t.Fatalf("ResetCoverCache returned error: %v", err)
	}

	if _, err := os.Stat(coversDir); !os.IsNotExist(err) {
		t.Errorf("covers dir still exists after reset (stat err = %v), want removed", err)
	}
	if _, err := os.Stat(libraryCachePath); !os.IsNotExist(err) {
		t.Errorf("library-cache.json still exists after reset (stat err = %v), want removed", err)
	}
}

func TestResetCoverCache_LeavesOverridesFileUntouched(t *testing.T) {
	logFolder := t.TempDir()
	overridesPath := filepath.Join(logFolder, "cover-overrides.json")
	overridesContent := []byte(`{"/book.pdf":{"type":"embedded","page":3}}`)
	if err := os.WriteFile(overridesPath, overridesContent, 0644); err != nil {
		t.Fatalf("write fixture overrides: %v", err)
	}

	app := NewApp()
	app.configPath = func() (string, error) { return writeTestConfigForSettings(t, logFolder), nil }

	if err := app.ResetCoverCache(); err != nil {
		t.Fatalf("ResetCoverCache returned error: %v", err)
	}

	got, err := os.ReadFile(overridesPath)
	if err != nil {
		t.Fatalf("read overrides file after reset: %v", err)
	}
	if string(got) != string(overridesContent) {
		t.Errorf("overrides file content = %q, want untouched %q", got, overridesContent)
	}
}

func TestResetCoverCache_MissingFilesIsNotAnError(t *testing.T) {
	logFolder := t.TempDir()
	app := NewApp()
	app.configPath = func() (string, error) { return writeTestConfigForSettings(t, logFolder), nil }

	if err := app.ResetCoverCache(); err != nil {
		t.Errorf("ResetCoverCache on an already-empty log folder returned error: %v, want nil", err)
	}
}

func TestResetCoverCache_PropagatesConfigLoadError(t *testing.T) {
	app := NewApp()
	app.configPath = func() (string, error) { return "", os.ErrNotExist }

	if err := app.ResetCoverCache(); err == nil {
		t.Error("ResetCoverCache returned nil error, want the config-load failure to propagate")
	}
}
