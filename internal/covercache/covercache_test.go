// internal/covercache/covercache_test.go
package covercache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsure_EmptyCoverBytesReturnsEmptyPath(t *testing.T) {
	logFolder := t.TempDir()
	url, err := Ensure(logFolder, "/library/Fiction/book.epub", time.Now(), nil, "image/jpeg")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if url != "" {
		t.Errorf("url = %q, want empty", url)
	}
}

func TestEnsure_WritesCoverAndReturnsURL(t *testing.T) {
	logFolder := t.TempDir()
	data := []byte("fake-jpeg-bytes")

	url, err := Ensure(logFolder, "/library/Fiction/book.epub", time.Now(), data, "image/jpeg")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if filepath.Ext(url) != ".jpg" {
		t.Errorf("url = %q, want a .jpg extension", url)
	}

	cachePath := filepath.Join(Dir(logFolder), filepath.Base(url))
	written, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(written) != string(data) {
		t.Errorf("cached content = %q, want %q", written, data)
	}
}

func TestEnsure_SkipsRewriteWhenCacheIsFresh(t *testing.T) {
	logFolder := t.TempDir()
	sourceModTime := time.Now().Add(-time.Hour)

	if _, err := Ensure(logFolder, "/library/Fiction/book.epub", sourceModTime, []byte("original"), "image/jpeg"); err != nil {
		t.Fatalf("first Ensure returned error: %v", err)
	}
	url, err := Ensure(logFolder, "/library/Fiction/book.epub", sourceModTime, []byte("changed-but-should-be-ignored"), "image/jpeg")
	if err != nil {
		t.Fatalf("second Ensure returned error: %v", err)
	}

	cachePath := filepath.Join(Dir(logFolder), filepath.Base(url))
	written, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(written) != "original" {
		t.Errorf("cached content = %q, want unchanged %q", written, "original")
	}
}

func TestEnsure_RewritesWhenSourceIsNewerThanCache(t *testing.T) {
	logFolder := t.TempDir()

	if _, err := Ensure(logFolder, "/library/Fiction/book.epub", time.Now().Add(-time.Hour), []byte("original"), "image/jpeg"); err != nil {
		t.Fatalf("first Ensure returned error: %v", err)
	}
	url, err := Ensure(logFolder, "/library/Fiction/book.epub", time.Now().Add(time.Hour), []byte("updated"), "image/jpeg")
	if err != nil {
		t.Fatalf("second Ensure returned error: %v", err)
	}

	cachePath := filepath.Join(Dir(logFolder), filepath.Base(url))
	written, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(written) != "updated" {
		t.Errorf("cached content = %q, want %q", written, "updated")
	}
}
