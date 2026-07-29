package appapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FrancisChung/BLibOrg/internal/config"
	"github.com/FrancisChung/BLibOrg/internal/covercache"
	"github.com/FrancisChung/BLibOrg/internal/librarycache"
)

// urlPath strips covercache's cache-busting "?v=" query, if present, so
// tests can assert on the underlying file path/extension.
func urlPath(url string) string {
	if i := strings.IndexByte(url, '?'); i >= 0 {
		return url[:i]
	}
	return url
}

func twoPagePDFFixture() []byte {
	jpeg1 := []byte("\xFF\xD8\xFFpage1jpeg")
	jpeg2 := []byte("\xFF\xD8\xFFpage2jpeg")
	return []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 5 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 6 0 R >> >> >>\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpeg1) + "\nendstream\nendobj\n" +
			"6 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpeg2) + "\nendstream\nendobj\n")
}

func newTestAppWithConfig(t *testing.T) (*App, config.Config, string) {
	t.Helper()
	logFolder := t.TempDir()
	cfg := config.Config{General: config.General{LogFolder: logFolder}}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }
	return app, cfg, logFolder
}

func TestListPDFCoverCandidates_ReturnsThumbnailPerPage(t *testing.T) {
	app, _, _ := newTestAppWithConfig(t)
	bookPath := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(bookPath, twoPagePDFFixture(), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	candidates, err := app.ListPDFCoverCandidates(bookPath)
	if err != nil {
		t.Fatalf("ListPDFCoverCandidates returned error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2", len(candidates))
	}
	if candidates[0].Page != 1 || candidates[0].ThumbnailURL == "" {
		t.Errorf("candidates[0] = %+v, want Page=1 and a non-empty ThumbnailURL", candidates[0])
	}
}

func TestSetCoverOverride_ToADifferentPageReturnsADifferentURL(t *testing.T) {
	// Regression test for the "Choose cover" picker appearing to do
	// nothing: covercache's filename depends only on sourcePath+
	// contentType, never on which page was chosen or its bytes, so
	// picking a different page must still surface as a different URL
	// (via a cache-busting query) or the frontend's <img src> binding
	// would never notice the change.
	app, _, _ := newTestAppWithConfig(t)
	bookPath := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(bookPath, twoPagePDFFixture(), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	url1, err := app.SetCoverOverride(bookPath, 1)
	if err != nil {
		t.Fatalf("SetCoverOverride(page=1): %v", err)
	}
	url2, err := app.SetCoverOverride(bookPath, 2)
	if err != nil {
		t.Fatalf("SetCoverOverride(page=2): %v", err)
	}
	if url1 == url2 {
		t.Errorf("url1 == url2 (%q), want a different URL for a different page's image", url1)
	}
}

func TestSetCoverOverride_PersistsAndReturnsCoverURL(t *testing.T) {
	app, _, logFolder := newTestAppWithConfig(t)
	bookPath := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(bookPath, twoPagePDFFixture(), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	url, err := app.SetCoverOverride(bookPath, 2)
	if err != nil {
		t.Fatalf("SetCoverOverride returned error: %v", err)
	}
	if url == "" {
		t.Error("url is empty, want a /covers/... URL")
	}

	ov, found, err := covercache.GetOverride(logFolder, bookPath)
	if err != nil || !found {
		t.Fatalf("GetOverride = %+v, %v, %v, want found=true", ov, found, err)
	}
	if ov.Type != covercache.OverrideEmbedded || ov.Page != 2 {
		t.Errorf("override = %+v, want Type=embedded, Page=2", ov)
	}
}

func TestSetCoverOverride_NoImageOnPageReturnsError(t *testing.T) {
	app, _, _ := newTestAppWithConfig(t)
	bookPath := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(bookPath, twoPagePDFFixture(), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := app.SetCoverOverride(bookPath, 99); err == nil {
		t.Error("expected an error for a page with no image, got nil")
	}
}

func TestSetCoverOverrideCustom_PersistsAndReturnsCoverURL(t *testing.T) {
	app, _, logFolder := newTestAppWithConfig(t)
	bookPath := filepath.Join(t.TempDir(), "book.epub")

	url, err := app.SetCoverOverrideCustom(bookPath, []byte("uploaded-bytes"), "image/png")
	if err != nil {
		t.Fatalf("SetCoverOverrideCustom returned error: %v", err)
	}
	if filepath.Ext(urlPath(url)) != ".png" {
		t.Errorf("url = %q, want a .png extension", url)
	}

	ov, found, err := covercache.GetOverride(logFolder, bookPath)
	if err != nil || !found {
		t.Fatalf("GetOverride = %+v, %v, %v, want found=true", ov, found, err)
	}
	if ov.Type != covercache.OverrideCustom || ov.ImagePath != url {
		t.Errorf("override = %+v, want Type=custom, ImagePath=%q", ov, url)
	}
}

func TestSetCoverOverrideCustomFromFile_ReadsFileAndInfersContentType(t *testing.T) {
	app, _, _ := newTestAppWithConfig(t)
	bookPath := filepath.Join(t.TempDir(), "book.epub")
	imagePath := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(imagePath, []byte("png-bytes"), 0644); err != nil {
		t.Fatalf("write image fixture: %v", err)
	}

	url, err := app.SetCoverOverrideCustomFromFile(bookPath, imagePath)
	if err != nil {
		t.Fatalf("SetCoverOverrideCustomFromFile returned error: %v", err)
	}
	if filepath.Ext(urlPath(url)) != ".png" {
		t.Errorf("url = %q, want a .png extension", url)
	}
}

func TestClearCoverOverride_RemovesOverrideAndReturnsAutoDetectedURL(t *testing.T) {
	app, _, logFolder := newTestAppWithConfig(t)
	bookPath := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(bookPath, twoPagePDFFixture(), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := app.SetCoverOverride(bookPath, 2); err != nil {
		t.Fatalf("SetCoverOverride: %v", err)
	}

	url, err := app.ClearCoverOverride(bookPath)
	if err != nil {
		t.Fatalf("ClearCoverOverride returned error: %v", err)
	}
	if url == "" {
		t.Error("url is empty, want the auto-detected cover's URL")
	}

	_, found, err := covercache.GetOverride(logFolder, bookPath)
	if err != nil {
		t.Fatalf("GetOverride returned error: %v", err)
	}
	if found {
		t.Error("found = true, want false (override was cleared)")
	}
}

func TestSetCoverOverride_InvalidatesExistingLibraryCacheEntry(t *testing.T) {
	app, _, logFolder := newTestAppWithConfig(t)
	bookPath := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(bookPath, twoPagePDFFixture(), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	info, err := os.Stat(bookPath)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}

	cache := librarycache.Load(logFolder)
	cache.Put(bookPath, librarycache.Entry{ModTime: info.ModTime(), Size: info.Size(), Title: "Stale Cached Title"})
	if err := cache.Save(logFolder); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	if _, err := app.SetCoverOverride(bookPath, 2); err != nil {
		t.Fatalf("SetCoverOverride returned error: %v", err)
	}

	reloaded := librarycache.Load(logFolder)
	if _, ok := reloaded.Fresh(bookPath, info.ModTime(), info.Size()); ok {
		t.Error("library cache entry still fresh after SetCoverOverride, want it invalidated")
	}
}

func TestClearCoverOverride_InvalidatesExistingLibraryCacheEntry(t *testing.T) {
	app, _, logFolder := newTestAppWithConfig(t)
	bookPath := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(bookPath, twoPagePDFFixture(), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := app.SetCoverOverride(bookPath, 2); err != nil {
		t.Fatalf("SetCoverOverride: %v", err)
	}

	info, err := os.Stat(bookPath)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}
	cache := librarycache.Load(logFolder)
	cache.Put(bookPath, librarycache.Entry{ModTime: info.ModTime(), Size: info.Size(), Title: "Cached With Override", CoverOverridden: true})
	if err := cache.Save(logFolder); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	if _, err := app.ClearCoverOverride(bookPath); err != nil {
		t.Fatalf("ClearCoverOverride returned error: %v", err)
	}

	reloaded := librarycache.Load(logFolder)
	if _, ok := reloaded.Fresh(bookPath, info.ModTime(), info.Size()); ok {
		t.Error("library cache entry still fresh after ClearCoverOverride, want it invalidated")
	}
}

func TestSetCoverOverride_PropagatesInvalidationFailure(t *testing.T) {
	logFolder := t.TempDir()
	// Block only the library cache's own file path with a directory, so
	// covercache.SetOverride (a different file, cover-overrides.json)
	// still succeeds and this test isolates the invalidation failure.
	if err := os.MkdirAll(filepath.Join(logFolder, "library-cache.json"), 0755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	cfg := config.Config{General: config.General{LogFolder: logFolder}}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	bookPath := filepath.Join(t.TempDir(), "book.pdf")
	if err := os.WriteFile(bookPath, twoPagePDFFixture(), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	if _, err := app.SetCoverOverride(bookPath, 2); err == nil {
		t.Error("SetCoverOverride returned nil error, want the blocked invalidation write to surface")
	}
}
