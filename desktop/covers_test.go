package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
)

func writeTestConfigForCovers(t *testing.T, logFolder string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Config{General: config.General{LogFolder: logFolder}}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return path
}

func TestCoverHandler_ServesExistingCoverFile(t *testing.T) {
	logFolder := t.TempDir()
	coversDir := filepath.Join(logFolder, "covers")
	if err := os.MkdirAll(coversDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coversDir, "abc123.jpg"), []byte("fake-jpeg"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	configPath := writeTestConfigForCovers(t, logFolder)

	handler := coverHandler(func() (string, error) { return configPath, nil })
	req := httptest.NewRequest(http.MethodGet, "/covers/abc123.jpg", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "fake-jpeg" {
		t.Errorf("body = %q, want fake-jpeg", rec.Body.String())
	}
}

func TestCoverHandler_ServesFileWhenRequestHasCacheBustingQuery(t *testing.T) {
	// covercache appends a "?v=<hash>" cache-busting query to every URL it
	// returns (see internal/covercache.writeCoverFile) so the frontend's
	// <img src> actually changes when a cover is rewritten. This proves
	// the handler still resolves the file correctly -- Go's net/http
	// already separates r.URL.Path from the query, so this should need no
	// code change here, but it's the one behavior the whole fix depends on.
	logFolder := t.TempDir()
	coversDir := filepath.Join(logFolder, "covers")
	if err := os.MkdirAll(coversDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coversDir, "abc123.jpg"), []byte("fake-jpeg"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	configPath := writeTestConfigForCovers(t, logFolder)

	handler := coverHandler(func() (string, error) { return configPath, nil })
	req := httptest.NewRequest(http.MethodGet, "/covers/abc123.jpg?v=deadbeef", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "fake-jpeg" {
		t.Errorf("body = %q, want fake-jpeg", rec.Body.String())
	}
}

func TestCoverHandler_MissingFileReturns404(t *testing.T) {
	configPath := writeTestConfigForCovers(t, t.TempDir())

	handler := coverHandler(func() (string, error) { return configPath, nil })
	req := httptest.NewRequest(http.MethodGet, "/covers/does-not-exist.jpg", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestCoverHandler_RejectsPathTraversal(t *testing.T) {
	configPath := writeTestConfigForCovers(t, t.TempDir())

	handler := coverHandler(func() (string, error) { return configPath, nil })
	req := httptest.NewRequest(http.MethodGet, "/covers/..%2F..%2Fetc%2Fpasswd", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
