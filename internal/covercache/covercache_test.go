// internal/covercache/covercache_test.go
package covercache

import (
	"os"
	"path/filepath"
	"strings"
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

func TestForce_AlwaysRewritesRegardlessOfExistingCache(t *testing.T) {
	logFolder := t.TempDir()
	sourceModTime := time.Now().Add(-time.Hour)

	if _, err := Ensure(logFolder, "/library/book.epub", sourceModTime, []byte("original"), "image/jpeg"); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	url, err := Force(logFolder, "/library/book.epub", []byte("forced"), "image/jpeg")
	if err != nil {
		t.Fatalf("Force returned error: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(Dir(logFolder), filepath.Base(url)))
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(written) != "forced" {
		t.Errorf("cached content = %q, want %q (Force must rewrite even though the mtime check would have skipped it)", written, "forced")
	}
}

func TestForce_EmptyBytesReturnsEmptyPath(t *testing.T) {
	url, err := Force(t.TempDir(), "/library/book.epub", nil, "image/jpeg")
	if err != nil {
		t.Fatalf("Force returned error: %v", err)
	}
	if url != "" {
		t.Errorf("url = %q, want empty", url)
	}
}

func TestWriteCustomOverrideImage_WritesUnderCoversDirWithOverridePrefix(t *testing.T) {
	logFolder := t.TempDir()
	url, err := WriteCustomOverrideImage(logFolder, "/library/book.pdf", []byte("custom-bytes"), "image/png")
	if err != nil {
		t.Fatalf("WriteCustomOverrideImage returned error: %v", err)
	}
	if filepath.Ext(url) != ".png" {
		t.Errorf("url = %q, want a .png extension", url)
	}
	name := filepath.Base(url)
	if name[:len("override-")] != "override-" {
		t.Errorf("filename = %q, want an override- prefix", name)
	}
	written, err := os.ReadFile(filepath.Join(Dir(logFolder), name))
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(written) != "custom-bytes" {
		t.Errorf("written = %q, want %q", written, "custom-bytes")
	}
}

func TestWriteCandidateImage_WritesUnderCoversDirWithCandidatePrefixAndPage(t *testing.T) {
	logFolder := t.TempDir()
	url, err := WriteCandidateImage(logFolder, "/library/book.pdf", 3, []byte("candidate-bytes"), "image/jpeg")
	if err != nil {
		t.Fatalf("WriteCandidateImage returned error: %v", err)
	}
	name := filepath.Base(url)
	if name[:len("candidate-")] != "candidate-" {
		t.Errorf("filename = %q, want a candidate- prefix", name)
	}
	if !strings.Contains(name, "-p3") {
		t.Errorf("filename = %q, want it to encode page 3", name)
	}
}

func TestWriteCandidateImage_DifferentPagesGetDifferentFilenames(t *testing.T) {
	logFolder := t.TempDir()
	url1, err := WriteCandidateImage(logFolder, "/library/book.pdf", 1, []byte("page1"), "image/jpeg")
	if err != nil {
		t.Fatalf("WriteCandidateImage page 1: %v", err)
	}
	url2, err := WriteCandidateImage(logFolder, "/library/book.pdf", 2, []byte("page2"), "image/jpeg")
	if err != nil {
		t.Fatalf("WriteCandidateImage page 2: %v", err)
	}
	if url1 == url2 {
		t.Error("url1 == url2, want distinct filenames per page")
	}
}
