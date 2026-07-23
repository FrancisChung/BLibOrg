# PDF Cover Override Persistence + Picker UI (Plan C) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user override a book's auto-detected cover (pick a different page from the PDF, or upload a custom image) and undo back to auto-detection, persisted across scans, with a frontend picker modal on the Library view's book cards.

**Architecture:** A new `internal/covercache/override.go` persists overrides as a flat JSON map (`log_folder/cover-overrides.json`) keyed by source path. `internal/metadata` gains two narrowly-scoped exported functions (`ListPDFCoverCandidates`, `ExtractPDFPageCover`) alongside its existing sole entry point `Extract`, for the picker's page-level granularity. `librarian.Scan` checks for an override before falling through to normal extraction. `internal/appapi` exposes four new Wails-bound methods; `desktop/app.go` adds a native-file-picker passthrough. The frontend adds `CoverPickerModal.svelte`, opened from a new hover button on `LibraryBookCard.svelte`.

**Tech Stack:** Go stdlib (`encoding/json`, `os`) for persistence; existing Wails `runtime.OpenFileDialog` for the native upload picker; Svelte, matching the rest of `desktop/frontend/src`. No new dependencies.

## Global Constraints

- Reuses the existing `/covers/<name>` HTTP route (`desktop/covers.go`) for override and candidate-thumbnail images too, rather than adding new subdirectories/routes: `coverHandler` currently rejects any name containing `/` or `\` (single path segment only), so both new image kinds are written flat into the existing `covercache.Dir(logFolder)` directory with a distinguishing filename prefix (`override-...`, `candidate-...`) instead of the design doc's literal `covers/overrides/` and `covers/candidates/` subdirectories -- this is a deliberate simplification that avoids touching the path-traversal-guarded handler, and is otherwise behaviorally identical.
- `librarian.Scan` still calls `metadata.Extract` unconditionally for every book, even when an override exists, so `Title`/`Author`/`Year` are never lost for an overridden book -- only the *cover* portion of `Extract`'s result is replaced/bypassed when an override is present. (The design doc says extraction is "skipped entirely" for overridden books; taken literally, that would silently blank Title/Author/Year for those books, which contradicts this whole feature's own "never regress" goal, so this plan deliberately narrows the skip to cover selection only.)
- The manual page-override picker (`ListPDFCoverCandidates`/`SetCoverOverride`/`ExtractPDFPageCover`) is PDF-specific, matching the design doc's Section 4 scope; the custom-image-upload override (`SetCoverOverrideCustom`) works for any format.
- `metadata.Extract` remains "the only function other packages should call" for *whole-book* extraction; `ListPDFCoverCandidates` and `ExtractPDFPageCover` are documented exceptions that exist specifically for the override picker's page-level granularity.
- Override persistence is whole-file read-modify-write (no concurrent multi-process access to the Library today, matching the design doc's stated scope).
- No image resizing/thumbnailing library is added: candidate "thumbnails" are the full extracted image bytes, sized down via CSS in the picker grid (matching `LibraryBookCard.svelte`'s existing pattern of a full image in a small fixed-size tile).

---

## Task 1: Override persistence (`internal/covercache/override.go`)

**Files:**
- Create: `internal/covercache/override.go`
- Test: `internal/covercache/override_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `type OverrideType string` with consts `OverrideEmbedded`, `OverrideCustom`; `type Override struct { Type OverrideType; Page int; ImagePath string }`; `func GetOverride(logFolder, sourcePath string) (Override, bool, error)`, `func SetOverride(logFolder, sourcePath string, ov Override) error`, `func ClearOverride(logFolder, sourcePath string) error`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/covercache/override_test.go
package covercache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetOverride_NoFileYetReturnsNotFound(t *testing.T) {
	logFolder := t.TempDir()
	_, found, err := GetOverride(logFolder, "/library/Fiction/book.pdf")
	if err != nil {
		t.Fatalf("GetOverride returned error: %v", err)
	}
	if found {
		t.Error("found = true, want false (no overrides file exists yet)")
	}
}

func TestSetOverride_ThenGetOverrideRoundTrips(t *testing.T) {
	logFolder := t.TempDir()
	sourcePath := "/library/Fiction/book.pdf"
	want := Override{Type: OverrideEmbedded, Page: 3}

	if err := SetOverride(logFolder, sourcePath, want); err != nil {
		t.Fatalf("SetOverride returned error: %v", err)
	}

	got, found, err := GetOverride(logFolder, sourcePath)
	if err != nil {
		t.Fatalf("GetOverride returned error: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if got != want {
		t.Errorf("got = %+v, want %+v", got, want)
	}
}

func TestSetOverride_PersistsAcrossSeparateCalls(t *testing.T) {
	logFolder := t.TempDir()
	if err := SetOverride(logFolder, "/library/a.pdf", Override{Type: OverrideEmbedded, Page: 1}); err != nil {
		t.Fatalf("SetOverride a: %v", err)
	}
	if err := SetOverride(logFolder, "/library/b.pdf", Override{Type: OverrideCustom, ImagePath: "/covers/override-xyz.jpg"}); err != nil {
		t.Fatalf("SetOverride b: %v", err)
	}

	a, found, err := GetOverride(logFolder, "/library/a.pdf")
	if err != nil || !found || a.Page != 1 {
		t.Errorf("GetOverride a = %+v, %v, %v, want Page=1, true, nil", a, found, err)
	}
	b, found, err := GetOverride(logFolder, "/library/b.pdf")
	if err != nil || !found || b.ImagePath != "/covers/override-xyz.jpg" {
		t.Errorf("GetOverride b = %+v, %v, %v", b, found, err)
	}
}

func TestClearOverride_RemovesEntry(t *testing.T) {
	logFolder := t.TempDir()
	sourcePath := "/library/Fiction/book.pdf"
	if err := SetOverride(logFolder, sourcePath, Override{Type: OverrideEmbedded, Page: 2}); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	if err := ClearOverride(logFolder, sourcePath); err != nil {
		t.Fatalf("ClearOverride returned error: %v", err)
	}

	_, found, err := GetOverride(logFolder, sourcePath)
	if err != nil {
		t.Fatalf("GetOverride returned error: %v", err)
	}
	if found {
		t.Error("found = true, want false (override was cleared)")
	}
}

func TestClearOverride_NoFileYetIsANoOp(t *testing.T) {
	logFolder := t.TempDir()
	if err := ClearOverride(logFolder, "/library/never-set.pdf"); err != nil {
		t.Fatalf("ClearOverride returned error: %v, want nil (nothing to clear)", err)
	}
}

func TestSetOverride_WritesReadableJSONFile(t *testing.T) {
	logFolder := t.TempDir()
	if err := SetOverride(logFolder, "/library/book.pdf", Override{Type: OverrideEmbedded, Page: 1}); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	path := filepath.Join(logFolder, "cover-overrides.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/covercache/... -run 'TestGetOverride|TestSetOverride|TestClearOverride' -v`
Expected: FAIL with `undefined: GetOverride` (compile error).

- [ ] **Step 3: Implement `internal/covercache/override.go`**

```go
// This file persists manual cover overrides -- a user's choice to pin a
// book's cover to a specific PDF page or an uploaded image, overriding
// the auto-detected one -- as a flat JSON map keyed by source book path,
// under the same log_folder covercache.go already uses for cached cover
// images. Chosen over a sidecar file next to each book because it's
// consistent with the existing convention that log_folder is where all
// derived/cache state lives, and it doesn't require librarian.Scan or the
// rename/move pipeline to learn about a new file type living inside the
// organized library. Trade-off: an override won't travel if
// library_folder is copied to another machine without also copying
// log_folder.
package covercache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// OverrideType distinguishes a page pinned from the book's own PDF pages
// ("embedded") from a user-uploaded replacement image ("custom").
type OverrideType string

const (
	OverrideEmbedded OverrideType = "embedded"
	OverrideCustom   OverrideType = "custom"
)

// Override is one book's manual cover choice. Page is meaningful only for
// OverrideEmbedded (1-based page number within the source PDF).
// ImagePath is meaningful only for OverrideCustom, and holds the already-
// served "/covers/..." URL of the uploaded image (see
// WriteCustomOverrideImage), not a filesystem path -- so callers can use
// it directly as CoverPath with no further resolution.
type Override struct {
	Type      OverrideType `json:"type"`
	Page      int          `json:"page,omitempty"`
	ImagePath string       `json:"imagePath,omitempty"`
}

func overridesPath(logFolder string) string {
	return filepath.Join(logFolder, "cover-overrides.json")
}

// loadOverrides reads the whole override map, treating a missing file as
// an empty map (no overrides set yet) rather than an error.
func loadOverrides(logFolder string) (map[string]Override, error) {
	data, err := os.ReadFile(overridesPath(logFolder))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Override{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]Override{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func saveOverrides(logFolder string, m map[string]Override) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(logFolder, 0755); err != nil {
		return err
	}
	return os.WriteFile(overridesPath(logFolder), data, 0644)
}

// GetOverride returns sourcePath's override, if one has been set.
func GetOverride(logFolder, sourcePath string) (Override, bool, error) {
	m, err := loadOverrides(logFolder)
	if err != nil {
		return Override{}, false, err
	}
	ov, ok := m[sourcePath]
	return ov, ok, nil
}

// SetOverride persists ov as sourcePath's override, replacing any
// existing one, via a whole-file read-modify-write.
func SetOverride(logFolder, sourcePath string, ov Override) error {
	m, err := loadOverrides(logFolder)
	if err != nil {
		return err
	}
	m[sourcePath] = ov
	return saveOverrides(logFolder, m)
}

// ClearOverride removes sourcePath's override (the "undo"). A no-op (not
// an error) if no override file exists yet, or none was set for
// sourcePath.
func ClearOverride(logFolder, sourcePath string) error {
	m, err := loadOverrides(logFolder)
	if err != nil {
		return err
	}
	delete(m, sourcePath)
	return saveOverrides(logFolder, m)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/covercache/... -v`
Expected: PASS (all tests, including the existing `covercache_test.go` ones)

- [ ] **Step 5: Commit**

```bash
git add internal/covercache/override.go internal/covercache/override_test.go
git commit -m "Add cover-override persistence (log_folder/cover-overrides.json)"
```

---

## Task 2: Override/candidate image writers (`covercache.Force`, `WriteCustomOverrideImage`, `WriteCandidateImage`)

**Files:**
- Modify: `internal/covercache/covercache.go`
- Test: `internal/covercache/covercache_test.go`

**Interfaces:**
- Consumes: `Dir`, `fileName`, `extByContentType` (existing, `covercache.go`).
- Produces: `func Force(logFolder, sourcePath string, coverBytes []byte, contentType string) (string, error)`, `func WriteCustomOverrideImage(logFolder, sourcePath string, imageBytes []byte, contentType string) (string, error)`, `func WriteCandidateImage(logFolder, sourcePath string, page int, imageBytes []byte, contentType string) (string, error)`.

- [ ] **Step 1: Write the failing tests**

```go
// Append to internal/covercache/covercache_test.go

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
```

Add `"strings"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/covercache/... -run 'TestForce|TestWriteCustomOverrideImage|TestWriteCandidateImage' -v`
Expected: FAIL with `undefined: Force` (compile error).

- [ ] **Step 3: Implement**

Append to `internal/covercache/covercache.go`:

```go
// Force writes coverBytes to the cache unconditionally, unlike Ensure
// (which reuses an existing cache file at least as new as
// sourceModTime). Used by manual cover-override set/clear: the source
// file's own mtime hasn't changed, but the served URL must reflect the
// new choice immediately rather than waiting for the next time the
// source file itself changes.
func Force(logFolder, sourcePath string, coverBytes []byte, contentType string) (string, error) {
	if len(coverBytes) == 0 {
		return "", nil
	}
	dir := Dir(logFolder)
	name := fileName(sourcePath, contentType)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, name), coverBytes, 0644); err != nil {
		return "", err
	}
	return "/covers/" + name, nil
}

// WriteCustomOverrideImage stores a user-uploaded cover image under the
// same covercache.Dir as auto-detected covers (rather than the design
// doc's separate covers/overrides/ subdirectory -- see this plan's Global
// Constraints for why), with an "override-" filename prefix so it can't
// collide with sourcePath's own auto-cache entry. Always (re)writes:
// there's no "is this already cached" question for a fresh upload.
func WriteCustomOverrideImage(logFolder, sourcePath string, imageBytes []byte, contentType string) (string, error) {
	dir := Dir(logFolder)
	name := "override-" + fileName(sourcePath, contentType)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, name), imageBytes, 0644); err != nil {
		return "", err
	}
	return "/covers/" + name, nil
}

// candidateFileName mirrors fileName's "hash the source path, not the
// bytes" approach, plus the page number, so the cover-picker's thumbnail
// grid gets one stable, distinct URL per (book, page) pair across calls.
func candidateFileName(sourcePath string, page int, contentType string) string {
	sum := sha256.Sum256([]byte(sourcePath))
	ext := extByContentType[contentType]
	if ext == "" {
		ext = ".img"
	}
	return "candidate-" + hex.EncodeToString(sum[:]) + "-p" + strconv.Itoa(page) + ext
}

// WriteCandidateImage stores one page's candidate cover image (from
// metadata.ListPDFCoverCandidates) for the cover-picker's thumbnail grid,
// under covercache.Dir with a "candidate-" prefix (see
// WriteCustomOverrideImage's doc comment for why this dir, not a
// candidates/ subdirectory).
func WriteCandidateImage(logFolder, sourcePath string, page int, imageBytes []byte, contentType string) (string, error) {
	dir := Dir(logFolder)
	name := candidateFileName(sourcePath, page, contentType)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, name), imageBytes, 0644); err != nil {
		return "", err
	}
	return "/covers/" + name, nil
}
```

Add `"strconv"` to `covercache.go`'s imports (`crypto/sha256`, `encoding/hex`, `os`, `path/filepath`, `time` are already imported).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/covercache/... -v`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add internal/covercache/covercache.go internal/covercache/covercache_test.go
git commit -m "Add Force write and override/candidate image writers to covercache"
```

---

## Task 3: `metadata.ListPDFCoverCandidates` + `metadata.ExtractPDFPageCover`

**Files:**
- Modify: `internal/metadata/extractor.go` (doc comment only)
- Create: `internal/metadata/pdf_override.go`
- Test: `internal/metadata/pdf_override_test.go`

**Interfaces:**
- Consumes: `buildPDFObjIndex`, `walkPDFPageTree`, `findPDFPageImages`, `pdfPage` (Plan A, all in package `metadata`).
- Produces: `type PDFCoverCandidate struct { Page int; Bytes []byte; ContentType string }`, `func ListPDFCoverCandidates(path string, pageLimit int) ([]PDFCoverCandidate, error)`, `func ExtractPDFPageCover(path string, page int) (data []byte, contentType string, ok bool, err error)`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/metadata/pdf_override_test.go
package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

func writePDFFixture(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.pdf")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func twoPageTwoImageFixture() []byte {
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

func TestListPDFCoverCandidates_ReturnsOneCandidatePerPage(t *testing.T) {
	path := writePDFFixture(t, twoPageTwoImageFixture())

	candidates, err := ListPDFCoverCandidates(path, 10)
	if err != nil {
		t.Fatalf("ListPDFCoverCandidates returned error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("len(candidates) = %d, want 2", len(candidates))
	}
	if candidates[0].Page != 1 || candidates[1].Page != 2 {
		t.Errorf("pages = %d, %d, want 1, 2", candidates[0].Page, candidates[1].Page)
	}
	if candidates[0].ContentType != "image/jpeg" {
		t.Errorf("ContentType = %q, want image/jpeg", candidates[0].ContentType)
	}
}

func TestListPDFCoverCandidates_UnresolvablePageTreeReturnsEmptyNotError(t *testing.T) {
	path := writePDFFixture(t, []byte("not a real pdf"))
	candidates, err := ListPDFCoverCandidates(path, 10)
	if err != nil {
		t.Fatalf("ListPDFCoverCandidates returned error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("len(candidates) = %d, want 0", len(candidates))
	}
}

func TestListPDFCoverCandidates_NonExistentFileReturnsError(t *testing.T) {
	if _, err := ListPDFCoverCandidates(filepath.Join(t.TempDir(), "missing.pdf"), 10); err == nil {
		t.Error("expected an error for a nonexistent file, got nil")
	}
}

func TestExtractPDFPageCover_ReturnsExactPageImage(t *testing.T) {
	path := writePDFFixture(t, twoPageTwoImageFixture())

	data, contentType, ok, err := ExtractPDFPageCover(path, 2)
	if err != nil {
		t.Fatalf("ExtractPDFPageCover returned error: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if string(data) != "\xFF\xD8\xFFpage2jpeg" {
		t.Errorf("data = %q, want page 2's image", data)
	}
	if contentType != "image/jpeg" {
		t.Errorf("contentType = %q, want image/jpeg", contentType)
	}
}

func TestExtractPDFPageCover_OutOfRangePageNotOK(t *testing.T) {
	path := writePDFFixture(t, twoPageTwoImageFixture())
	_, _, ok, err := ExtractPDFPageCover(path, 99)
	if err != nil {
		t.Fatalf("ExtractPDFPageCover returned error: %v", err)
	}
	if ok {
		t.Error("ok = true, want false (page 99 doesn't exist)")
	}
}

func TestExtractPDFPageCover_PageWithNoImageNotOK(t *testing.T) {
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /Font << /F1 9 0 R >> >> >>\nendobj\n")
	path := writePDFFixture(t, data)

	_, _, ok, err := ExtractPDFPageCover(path, 1)
	if err != nil {
		t.Fatalf("ExtractPDFPageCover returned error: %v", err)
	}
	if ok {
		t.Error("ok = true, want false (page 1 has no image)")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run 'TestListPDFCoverCandidates|TestExtractPDFPageCover' -v`
Expected: FAIL with `undefined: ListPDFCoverCandidates` (compile error).

- [ ] **Step 3: Implement `internal/metadata/pdf_override.go`**

```go
// This file backs the manual cover-override picker with page-level
// granularity Extract's single combined Result can't expose: listing
// every candidate image across a PDF's first pageLimit pages (for the
// picker's thumbnail grid), and re-extracting one specific page's image
// (once the user has chosen it, and on every later scan while that
// override is in effect).
package metadata

import "os"

// PDFCoverCandidate is one image found on a specific page during
// ListPDFCoverCandidates' collect-all walk.
type PDFCoverCandidate struct {
	Page        int
	Bytes       []byte
	ContentType string
}

// ListPDFCoverCandidates walks up to pageLimit pages of the PDF at path
// and returns every qualifying image found (not just the first, unlike
// Extract's auto-detect path), for the cover-override picker's thumbnail
// grid. Returns an empty (not nil-error) slice if the page tree can't be
// resolved at all -- matching this package's convention of degrading
// gracefully for atypical PDFs rather than erroring.
func ListPDFCoverCandidates(path string, pageLimit int) ([]PDFCoverCandidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, pageLimit)
	if !ok {
		return nil, nil
	}
	images := findPDFPageImages(idx, pages, false)
	candidates := make([]PDFCoverCandidate, len(images))
	for i, img := range images {
		candidates[i] = PDFCoverCandidate{Page: img.page, Bytes: img.bytes, ContentType: img.contentType}
	}
	return candidates, nil
}

// ExtractPDFPageCover re-extracts the qualifying image found on exactly
// page (1-based), for a manual override that pins a specific page rather
// than auto-detecting the first qualifying one. ok is false (not an
// error) if the page tree can't be resolved, page is out of range, or no
// qualifying image is found on that exact page.
func ExtractPDFPageCover(path string, page int) (data []byte, contentType string, ok bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", false, err
	}
	idx := buildPDFObjIndex(raw)
	pages, treeOK := walkPDFPageTree(idx, page)
	if !treeOK || page < 1 || page > len(pages) {
		return nil, "", false, nil
	}
	images := findPDFPageImages(idx, []pdfPage{pages[page-1]}, true)
	if len(images) == 0 {
		return nil, "", false, nil
	}
	return images[0].bytes, images[0].contentType, true, nil
}
```

Update `Extract`'s doc comment in `internal/metadata/extractor.go` (currently ends "It is the only function other packages should call."):

```go
// ... (unchanged preceding lines) ...
// hyphenExceptions lists hyphenated words FormatTitle should keep
// hyphenated rather than splitting on "-" (cfg.TitleFormatting.HyphenExceptions).
// It is the only function other packages should call for whole-book
// extraction. ListPDFCoverCandidates and ExtractPDFPageCover
// (pdf_override.go) are the two exceptions: both exist specifically for
// the manual cover-override picker, which needs page-level granularity
// this combined Result can't expose.
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_override.go internal/metadata/pdf_override_test.go internal/metadata/extractor.go
git commit -m "Add ListPDFCoverCandidates and ExtractPDFPageCover for the override picker"
```

---

## Task 4: Wire overrides into `librarian.Scan`

**Files:**
- Modify: `internal/librarian/librarian.go`
- Test: `internal/librarian/librarian_test.go`

**Interfaces:**
- Consumes: `covercache.GetOverride`, `covercache.Force`, `covercache.OverrideEmbedded`, `covercache.OverrideCustom` (Tasks 1-2); `metadata.ExtractPDFPageCover` (Task 3).
- Produces: `librarian.Book` gains `CoverOverridden bool`.

- [ ] **Step 1: Write the failing tests**

```go
// Append to internal/librarian/librarian_test.go

func writeRealPDFFixture(t *testing.T, path string) {
	t.Helper()
	jpeg := []byte("\xFF\xD8\xFFrealjpeg")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /Font << /F1 9 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 5 0 R >> >> >>\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpeg) + "\nendstream\nendobj\n")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func TestScan_NoOverrideUsesAutoDetectedCover(t *testing.T) {
	libDir := t.TempDir()
	path := filepath.Join(libDir, "book.pdf")
	writeRealPDFFixture(t, path)

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: t.TempDir()}}
	books, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("len(books) = %d, want 1", len(books))
	}
	if books[0].CoverOverridden {
		t.Error("CoverOverridden = true, want false (no override set)")
	}
	if books[0].CoverPath == "" {
		t.Error("CoverPath is empty, want the auto-detected page-2 cover")
	}
}

func TestScan_EmbeddedOverridePinsSpecificPage(t *testing.T) {
	libDir := t.TempDir()
	path := filepath.Join(libDir, "book.pdf")
	writeRealPDFFixture(t, path)
	logFolder := t.TempDir()

	// The fixture's only image is on page 2; pin page 1 (which has none) to
	// prove the override is actually driving cover selection, not being
	// ignored in favor of auto-detection.
	if err := covercache.SetOverride(logFolder, path, covercache.Override{Type: covercache.OverrideEmbedded, Page: 1}); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logFolder}}
	books, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !books[0].CoverOverridden {
		t.Error("CoverOverridden = false, want true")
	}
	if books[0].CoverPath != "" {
		t.Error("CoverPath is non-empty, want empty (page 1 has no image, and the override should not fall back to auto-detection)")
	}
}

func TestScan_CustomOverrideUsesStoredImagePath(t *testing.T) {
	libDir := t.TempDir()
	path := filepath.Join(libDir, "book.pdf")
	writeRealPDFFixture(t, path)
	logFolder := t.TempDir()

	if err := covercache.SetOverride(logFolder, path, covercache.Override{
		Type:      covercache.OverrideCustom,
		ImagePath: "/covers/override-abc123.jpg",
	}); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logFolder}}
	books, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !books[0].CoverOverridden {
		t.Error("CoverOverridden = false, want true")
	}
	if books[0].CoverPath != "/covers/override-abc123.jpg" {
		t.Errorf("CoverPath = %q, want the stored override URL", books[0].CoverPath)
	}
}

func TestScan_OverrideDoesNotBlankTitleAuthorYear(t *testing.T) {
	// Confirms this plan's deliberate deviation from the design doc's
	// literal "extraction is skipped entirely" wording: Title/Author/Year
	// must still come from metadata.Extract even for an overridden book.
	libDir := t.TempDir()
	path := filepath.Join(libDir, "Foundation - Isaac Asimov.pdf")
	writeRealPDFFixture(t, path)
	logFolder := t.TempDir()
	if err := covercache.SetOverride(logFolder, path, covercache.Override{Type: covercache.OverrideEmbedded, Page: 2}); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logFolder}}
	books, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	// The fixture PDF has no /Title, so this only proves Extract still ran
	// (Format/SourcePath were always set regardless) -- combined with the
	// two tests above, which prove CoverPath genuinely reflects the
	// override rather than a blanked/skipped result.
	if books[0].Format != "pdf" {
		t.Errorf("Format = %q, want pdf (metadata.Extract path still ran)", books[0].Format)
	}
}
```

Add `"github.com/FrancisChung/book-organiser/internal/covercache"` to `librarian_test.go`'s imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/librarian/... -run 'TestScan_NoOverride|TestScan_EmbeddedOverride|TestScan_CustomOverride|TestScan_OverrideDoesNotBlank' -v`
Expected: FAIL with `books[0].CoverOverridden undefined` (compile error).

- [ ] **Step 3: Implement**

Add `CoverOverridden bool` to the `Book` struct in `internal/librarian/librarian.go`:

```go
type Book struct {
	SourcePath      string
	Format          string
	Title           string
	Author          string
	Year            string
	Category        string
	Subcategory     string
	CoverPath       string // "" if no cover was found; otherwise a /covers/... URL path
	CoverOverridden bool   // true if a manual cover override (see internal/covercache) is in effect
}
```

Replace the cover-handling block inside `Scan`'s loop:

```go
		if res, err := metadata.Extract(path, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit); err == nil {
			b.Title = res.Title
			b.Author = res.Author
			b.Year = res.Year

			coverBytes, coverContentType := res.CoverBytes, res.CoverContentType

			// A manual override, if one is set for this book, replaces the
			// cover portion of Extract's result -- but Extract itself still
			// ran above, so Title/Author/Year are never lost for an
			// overridden book (see this plan's Global Constraints for why
			// this deliberately narrows the design doc's "extraction is
			// skipped entirely" wording to cover selection only).
			if ov, found, ovErr := covercache.GetOverride(cfg.General.LogFolder, path); ovErr == nil && found {
				b.CoverOverridden = true
				switch ov.Type {
				case covercache.OverrideCustom:
					b.CoverPath = ov.ImagePath
					coverBytes = nil // already have a stable URL; skip covercache.Ensure below
				case covercache.OverrideEmbedded:
					if data, ct, ok, pageErr := metadata.ExtractPDFPageCover(path, ov.Page); pageErr == nil && ok {
						coverBytes, coverContentType = data, ct
					} else {
						coverBytes = nil
					}
				}
			}

			if len(coverBytes) > 0 {
				if coverURL, err := covercache.Ensure(cfg.General.LogFolder, path, statModTime(path), coverBytes, coverContentType); err == nil {
					b.CoverPath = coverURL
				}
			}
		}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/librarian/... -v`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add internal/librarian/librarian.go internal/librarian/librarian_test.go
git commit -m "Check cover overrides before auto-detecting a book's cover in Scan"
```

---

## Task 5: `appapi` cover-override methods

**Files:**
- Create: `internal/appapi/cover_override.go`
- Test: `internal/appapi/cover_override_test.go`
- Modify: `internal/appapi/library.go` (add `CoverOverridden` to `LibraryBookView`)
- Modify: `internal/appapi/library_test.go`

**Interfaces:**
- Consumes: `metadata.ListPDFCoverCandidates`, `metadata.ExtractPDFPageCover`, `metadata.Extract` (Task 3); `covercache.WriteCandidateImage`, `covercache.SetOverride`, `covercache.ClearOverride`, `covercache.Force`, `covercache.Override*` (Tasks 1-2); `a.loadConfig` (existing, `app.go`).
- Produces: `type CoverCandidateView struct { Page int; ThumbnailURL string }`; `func (a *App) ListPDFCoverCandidates(bookPath string) ([]CoverCandidateView, error)`, `func (a *App) SetCoverOverride(bookPath string, page int) (string, error)`, `func (a *App) SetCoverOverrideCustom(bookPath string, imageBytes []byte, contentType string) (string, error)`, `func (a *App) SetCoverOverrideCustomFromFile(bookPath, imagePath string) (string, error)`, `func (a *App) ClearCoverOverride(bookPath string) (string, error)`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/appapi/cover_override_test.go
package appapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
)

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
	if filepath.Ext(url) != ".png" {
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
	if filepath.Ext(url) != ".png" {
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
```

Update `internal/appapi/library.go`: add `CoverOverridden bool \`json:"coverOverridden"\`` to `LibraryBookView`, and set it in `libraryBookToView`:

```go
type LibraryBookView struct {
	SourcePath      string `json:"sourcePath"`
	Format          string `json:"format"`
	Title           string `json:"title"`
	Author          string `json:"author"`
	Year            string `json:"year"`
	Category        string `json:"category"`
	Subcategory     string `json:"subcategory"`
	CoverPath       string `json:"coverPath"`
	CoverOverridden bool   `json:"coverOverridden"`
}
```

```go
func libraryBookToView(b librarian.Book) LibraryBookView {
	return LibraryBookView{
		SourcePath:      b.SourcePath,
		Format:          b.Format,
		Title:           b.Title,
		Author:          b.Author,
		Year:            b.Year,
		Category:        b.Category,
		Subcategory:     b.Subcategory,
		CoverPath:       b.CoverPath,
		CoverOverridden: b.CoverOverridden,
	}
}
```

Add to `internal/appapi/library_test.go`'s `TestListLibrary_ReturnsBooksGroupedByCategory`, after the existing `Category`/`Subcategory` assertion:

```go
	if view.Books[0].CoverOverridden {
		t.Error("CoverOverridden = true, want false (no override set for this fixture)")
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/appapi/... -run 'TestListPDFCoverCandidates|TestSetCoverOverride|TestClearCoverOverride|TestListLibrary' -v`
Expected: FAIL with `undefined: app.ListPDFCoverCandidates` (compile error).

- [ ] **Step 3: Implement `internal/appapi/cover_override.go`**

```go
// This file exposes the manual cover-override picker (design doc Section
// 4) to the Wails-bound frontend: listing every candidate image on a
// PDF's first N pages, pinning one, uploading a custom replacement, and
// undoing back to auto-detection.
package appapi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/metadata"
)

// CoverCandidateView is one selectable page/image for the cover-picker's
// thumbnail grid.
type CoverCandidateView struct {
	Page         int    `json:"page"`
	ThumbnailURL string `json:"thumbnailUrl"`
}

// ListPDFCoverCandidates returns every qualifying cover image found
// within the configured page limit of the PDF at bookPath, for the
// cover-override picker's thumbnail grid. A candidate this package fails
// to cache (covercache.WriteCandidateImage error) is silently omitted
// rather than failing the whole list -- matching this app's existing
// per-book best-effort convention (see librarian.Scan).
func (a *App) ListPDFCoverCandidates(bookPath string) ([]CoverCandidateView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	candidates, err := metadata.ListPDFCoverCandidates(bookPath, cfg.General.PDFCoverPageLimit)
	if err != nil {
		return nil, err
	}
	views := make([]CoverCandidateView, 0, len(candidates))
	for _, c := range candidates {
		url, err := covercache.WriteCandidateImage(cfg.General.LogFolder, bookPath, c.Page, c.Bytes, c.ContentType)
		if err != nil {
			continue
		}
		views = append(views, CoverCandidateView{Page: c.Page, ThumbnailURL: url})
	}
	return views, nil
}

// SetCoverOverride pins bookPath's cover to the image found on page,
// persists that choice (so future scans reuse it without re-prompting),
// and returns the resulting /covers/... URL immediately -- covercache.Force
// bypasses the mtime check Ensure would otherwise apply, since the source
// file's own mtime hasn't changed but the displayed cover must update
// right away.
func (a *App) SetCoverOverride(bookPath string, page int) (string, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return "", err
	}
	data, contentType, ok, err := metadata.ExtractPDFPageCover(bookPath, page)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("no qualifying image found on page %d", page)
	}
	if err := covercache.SetOverride(cfg.General.LogFolder, bookPath, covercache.Override{
		Type: covercache.OverrideEmbedded,
		Page: page,
	}); err != nil {
		return "", err
	}
	return covercache.Force(cfg.General.LogFolder, bookPath, data, contentType)
}

// SetCoverOverrideCustom pins bookPath's cover to an uploaded image,
// persisting it under log_folder/covers/ (covercache.WriteCustomOverrideImage)
// and recording the override so future scans reuse it without
// re-uploading.
func (a *App) SetCoverOverrideCustom(bookPath string, imageBytes []byte, contentType string) (string, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return "", err
	}
	url, err := covercache.WriteCustomOverrideImage(cfg.General.LogFolder, bookPath, imageBytes, contentType)
	if err != nil {
		return "", err
	}
	if err := covercache.SetOverride(cfg.General.LogFolder, bookPath, covercache.Override{
		Type:      covercache.OverrideCustom,
		ImagePath: url,
	}); err != nil {
		return "", err
	}
	return url, nil
}

// SetCoverOverrideCustomFromFile reads imagePath (from the frontend's
// native file-picker flow, see desktop/app.go's PickCoverImageFile) and
// delegates to SetCoverOverrideCustom -- kept separate so the byte-slice
// core stays directly unit-testable without a real file on disk.
func (a *App) SetCoverOverrideCustomFromFile(bookPath, imagePath string) (string, error) {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", err
	}
	return a.SetCoverOverrideCustom(bookPath, data, contentTypeFromExt(imagePath))
}

func contentTypeFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	default:
		return "image/jpeg"
	}
}

// ClearCoverOverride removes bookPath's override (the "undo") and re-runs
// normal auto-detection, returning the resulting URL -- which may be ""
// if extraction genuinely finds no cover.
func (a *App) ClearCoverOverride(bookPath string) (string, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return "", err
	}
	if err := covercache.ClearOverride(cfg.General.LogFolder, bookPath); err != nil {
		return "", err
	}
	res, err := metadata.Extract(bookPath, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit)
	if err != nil || len(res.CoverBytes) == 0 {
		return "", err
	}
	return covercache.Force(cfg.General.LogFolder, bookPath, res.CoverBytes, res.CoverContentType)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/appapi/... -v`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add internal/appapi/cover_override.go internal/appapi/cover_override_test.go internal/appapi/library.go internal/appapi/library_test.go
git commit -m "Expose cover-override picker methods via appapi"
```

---

## Task 6: Wails passthroughs + native file picker + regenerate bindings

**Files:**
- Modify: `desktop/app.go`
- Modify (regenerated): `desktop/frontend/wailsjs/go/main/App.d.ts`, `desktop/frontend/wailsjs/go/main/App.js`, `desktop/frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: `appapi.CoverCandidateView`, `a.api.ListPDFCoverCandidates`, `a.api.SetCoverOverride`, `a.api.SetCoverOverrideCustomFromFile`, `a.api.ClearCoverOverride` (Task 5); `runtime.OpenFileDialog` (Wails runtime, already imported in `app.go` as `github.com/wailsapp/wails/v2/pkg/runtime`).
- Produces: `func (a *App) ListPDFCoverCandidates(bookPath string) ([]appapi.CoverCandidateView, error)`, `func (a *App) SetCoverOverride(bookPath string, page int) (string, error)`, `func (a *App) SetCoverOverrideCustomFromFile(bookPath, imagePath string) (string, error)`, `func (a *App) ClearCoverOverride(bookPath string) (string, error)`, `func (a *App) PickCoverImageFile() (string, error)`.

This task has no new unit test: `PickCoverImageFile` needs a real Wails runtime context to show a native dialog, which isn't available under `go test` -- matching this codebase's existing convention of leaving `ConfirmApply`/`ConfirmUndo` (the other two native-dialog methods in `desktop/app.go`) without direct unit test coverage. It's verified manually in this plan's Task 10. The four passthrough methods are simple one-line delegations (already covered indirectly by Task 5's `appapi` tests on the methods they call), so they don't need their own tests either -- matching the existing passthroughs in `desktop/app.go` (e.g. `ListLibrary`, `Categories`), none of which have a `desktop`-package-level test of their own.

- [ ] **Step 1: Implement**

Add to `desktop/app.go` (alongside the other `a.api.X()` passthroughs):

```go
func (a *App) ListPDFCoverCandidates(bookPath string) ([]appapi.CoverCandidateView, error) {
	return a.api.ListPDFCoverCandidates(bookPath)
}

func (a *App) SetCoverOverride(bookPath string, page int) (string, error) {
	return a.api.SetCoverOverride(bookPath, page)
}

func (a *App) SetCoverOverrideCustomFromFile(bookPath, imagePath string) (string, error) {
	return a.api.SetCoverOverrideCustomFromFile(bookPath, imagePath)
}

func (a *App) ClearCoverOverride(bookPath string) (string, error) {
	return a.api.ClearCoverOverride(bookPath)
}

// PickCoverImageFile shows a native "choose an image" file dialog and
// returns the chosen path, or "" (not an error) if the user cancels --
// matching runtime.OpenFileDialog's own cancel behavior. The frontend
// passes the returned path straight to SetCoverOverrideCustomFromFile
// rather than reading the file itself, so image bytes never cross the
// Wails JS<->Go bridge as a base64 blob.
func (a *App) PickCoverImageFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose cover image",
		Filters: []runtime.FileFilter{
			{DisplayName: "Images (*.jpg, *.jpeg, *.png)", Pattern: "*.jpg;*.jpeg;*.png"},
		},
	})
}
```

Add `"github.com/FrancisChung/book-organiser/internal/appapi"` to `desktop/app.go`'s imports if not already present (it already is, for `appapi.BookView` etc.).

- [ ] **Step 2: Run the existing desktop package tests to confirm no regressions**

Run: `go test ./desktop/... -v`
Expected: PASS (all existing tests; no new ones were added in this task, see the note above)

- [ ] **Step 3: Regenerate Wails bindings**

Run: `cd desktop && wails generate module`

This regenerates `desktop/frontend/wailsjs/go/main/App.d.ts`, `App.js`, and `desktop/frontend/wailsjs/go/models.ts` to include the five new methods and the `CoverCandidateView` / updated `LibraryBookView` (with `coverOverridden`) types.

If `wails generate module` isn't available in this environment, hand-edit the three generated files to match the existing entries for e.g. `ListLibrary`/`LibraryBookView`, substituting the five new method signatures and the `CoverCandidateView` type and `LibraryBookView.coverOverridden` field -- but prefer the generator; hand-editing generated files risks drifting from what a real `wails build` would produce.

- [ ] **Step 4: Commit**

```bash
git add desktop/app.go desktop/frontend/wailsjs
git commit -m "Bind cover-override methods to the frontend and add a native file picker"
```

---

## Task 7: Frontend types

**Files:**
- Modify: `desktop/frontend/src/lib/types.ts`

**Interfaces:**
- Consumes: nothing new.
- Produces: `interface CoverCandidateView { page: number; thumbnailUrl: string }`; `LibraryBookView` gains `coverOverridden: boolean`.

- [ ] **Step 1: Write the failing test**

```ts
// Append to desktop/frontend/src/lib/LibraryBookCard.test.ts (or, if that
// file doesn't yet import LibraryBookView in a way that would catch a
// missing field, add this standalone check instead)
import { describe, it, expect } from 'vitest';
import type { LibraryBookView, CoverCandidateView } from './types';

describe('CoverCandidateView / LibraryBookView.coverOverridden', () => {
  it('LibraryBookView accepts coverOverridden', () => {
    const book: LibraryBookView = {
      sourcePath: '/library/book.pdf',
      format: 'pdf',
      title: 'Title',
      author: 'Author',
      year: '2020',
      category: 'Fiction',
      subcategory: '',
      coverPath: '/covers/abc.jpg',
      coverOverridden: true,
    };
    expect(book.coverOverridden).toBe(true);
  });

  it('CoverCandidateView shape', () => {
    const candidate: CoverCandidateView = { page: 1, thumbnailUrl: '/covers/candidate-abc-p1.jpg' };
    expect(candidate.page).toBe(1);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/LibraryBookCard.test.ts`
Expected: FAIL with a TypeScript error (`coverOverridden` does not exist on type `LibraryBookView`, `CoverCandidateView` not exported).

- [ ] **Step 3: Implement**

In `desktop/frontend/src/lib/types.ts`, update `LibraryBookView` and add `CoverCandidateView`:

```ts
export interface LibraryBookView {
  sourcePath: string;
  format: string;
  title: string;
  author: string;
  year: string;
  category: string;
  subcategory: string;
  coverPath: string;
  coverOverridden: boolean;
}

export interface CoverCandidateView {
  page: number;
  thumbnailUrl: string;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/lib/LibraryBookCard.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/types.ts desktop/frontend/src/lib/LibraryBookCard.test.ts
git commit -m "Add CoverCandidateView and LibraryBookView.coverOverridden frontend types"
```

---

## Task 8: `CoverPickerModal.svelte`

**Files:**
- Create: `desktop/frontend/src/lib/CoverPickerModal.svelte`
- Create: `desktop/frontend/src/lib/CoverPickerModal.test.ts`

**Interfaces:**
- Consumes: `ListPDFCoverCandidates`, `SetCoverOverride`, `SetCoverOverrideCustomFromFile`, `ClearCoverOverride`, `PickCoverImageFile` (Wails bindings, `../../wailsjs/go/main/App`); `CoverCandidateView` (Task 7, `./types`).
- Produces: a `CoverPickerModal` Svelte component with props `sourcePath: string`, `coverOverridden: boolean`, and dispatched events `close` (no payload) and `updated` (`{ coverPath: string; coverOverridden: boolean }`).

- [ ] **Step 1: Write the failing test**

```ts
// desktop/frontend/src/lib/CoverPickerModal.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import CoverPickerModal from './CoverPickerModal.svelte';

vi.mock('../../wailsjs/go/main/App', () => ({
  ListPDFCoverCandidates: vi.fn(),
  SetCoverOverride: vi.fn(),
  SetCoverOverrideCustomFromFile: vi.fn(),
  ClearCoverOverride: vi.fn(),
  PickCoverImageFile: vi.fn(),
}));

import {
  ListPDFCoverCandidates,
  SetCoverOverride,
  SetCoverOverrideCustomFromFile,
  ClearCoverOverride,
  PickCoverImageFile,
} from '../../wailsjs/go/main/App';

// vitest.config.ts sets neither restoreMocks nor clearMocks, so call
// history (not just return values) would otherwise leak between `it`
// blocks in this file -- e.g. the "cancelling" test's
// `not.toHaveBeenCalled()` on SetCoverOverrideCustomFromFile would
// falsely fail after the earlier "uploading" test already called it.
beforeEach(() => {
  vi.mocked(ListPDFCoverCandidates).mockReset();
  vi.mocked(SetCoverOverride).mockReset();
  vi.mocked(SetCoverOverrideCustomFromFile).mockReset();
  vi.mocked(ClearCoverOverride).mockReset();
  vi.mocked(PickCoverImageFile).mockReset();
});

describe('CoverPickerModal', () => {
  it('renders a thumbnail per candidate returned by ListPDFCoverCandidates', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([
      { page: 1, thumbnailUrl: '/covers/candidate-a-p1.jpg' },
      { page: 3, thumbnailUrl: '/covers/candidate-a-p3.jpg' },
    ]);

    render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: false });

    await waitFor(() => {
      expect(screen.getByAltText('Page 1')).toBeTruthy();
      expect(screen.getByAltText('Page 3')).toBeTruthy();
    });
  });

  it('choosing a thumbnail calls SetCoverOverride and dispatches updated + close', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([{ page: 2, thumbnailUrl: '/covers/candidate-a-p2.jpg' }]);
    vi.mocked(SetCoverOverride).mockResolvedValue('/covers/abc.jpg');

    const { component } = render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: false });
    const updated = vi.fn();
    const closed = vi.fn();
    component.$on('updated', updated);
    component.$on('close', closed);

    await waitFor(() => screen.getByAltText('Page 2'));
    await fireEvent.click(screen.getByAltText('Page 2'));

    await waitFor(() => {
      expect(SetCoverOverride).toHaveBeenCalledWith('/library/book.pdf', 2);
      expect(updated).toHaveBeenCalledTimes(1);
      expect(closed).toHaveBeenCalledTimes(1);
    });
    expect(updated.mock.calls[0][0].detail).toEqual({ coverPath: '/covers/abc.jpg', coverOverridden: true });
  });

  it('uploading a custom image calls PickCoverImageFile then SetCoverOverrideCustomFromFile', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([]);
    vi.mocked(PickCoverImageFile).mockResolvedValue('/tmp/chosen.png');
    vi.mocked(SetCoverOverrideCustomFromFile).mockResolvedValue('/covers/override-xyz.png');

    const { component } = render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: false });
    const updated = vi.fn();
    component.$on('updated', updated);

    await waitFor(() => screen.getByText('Upload custom image…'));
    await fireEvent.click(screen.getByText('Upload custom image…'));

    await waitFor(() => {
      expect(PickCoverImageFile).toHaveBeenCalled();
      expect(SetCoverOverrideCustomFromFile).toHaveBeenCalledWith('/library/book.pdf', '/tmp/chosen.png');
      expect(updated).toHaveBeenCalledTimes(1);
    });
  });

  it('cancelling the native file dialog does not call SetCoverOverrideCustomFromFile', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([]);
    vi.mocked(PickCoverImageFile).mockResolvedValue('');

    render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: false });

    await waitFor(() => screen.getByText('Upload custom image…'));
    await fireEvent.click(screen.getByText('Upload custom image…'));

    expect(SetCoverOverrideCustomFromFile).not.toHaveBeenCalled();
  });

  it('shows "Reset to auto-detected" only when coverOverridden is true', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([]);
    render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: true });
    await waitFor(() => expect(screen.getByText('Reset to auto-detected')).toBeTruthy());
  });

  it('clicking "Reset to auto-detected" calls ClearCoverOverride', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([]);
    vi.mocked(ClearCoverOverride).mockResolvedValue('/covers/auto.jpg');

    const { component } = render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: true });
    const updated = vi.fn();
    component.$on('updated', updated);

    await waitFor(() => screen.getByText('Reset to auto-detected'));
    await fireEvent.click(screen.getByText('Reset to auto-detected'));

    await waitFor(() => {
      expect(ClearCoverOverride).toHaveBeenCalledWith('/library/book.pdf');
      expect(updated).toHaveBeenCalledTimes(1);
    });
    expect(updated.mock.calls[0][0].detail).toEqual({ coverPath: '/covers/auto.jpg', coverOverridden: false });
  });

  it('shows the error message if ListPDFCoverCandidates rejects', async () => {
    vi.mocked(ListPDFCoverCandidates).mockRejectedValue(new Error('boom'));
    render(CoverPickerModal, { sourcePath: '/library/book.pdf', coverOverridden: false });
    await waitFor(() => expect(screen.getByText(/boom/)).toBeTruthy());
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/CoverPickerModal.test.ts`
Expected: FAIL (module `./CoverPickerModal.svelte` doesn't exist)

- [ ] **Step 3: Implement `CoverPickerModal.svelte`**

```svelte
<script lang="ts">
  import { createEventDispatcher, onMount } from 'svelte';
  import type { CoverCandidateView } from './types';
  import {
    ListPDFCoverCandidates,
    SetCoverOverride,
    SetCoverOverrideCustomFromFile,
    ClearCoverOverride,
    PickCoverImageFile,
  } from '../../wailsjs/go/main/App';

  export let sourcePath: string;
  export let coverOverridden: boolean;

  const dispatch = createEventDispatcher<{
    close: void;
    updated: { coverPath: string; coverOverridden: boolean };
  }>();

  let candidates: CoverCandidateView[] = [];
  let loading = true;
  let busy = false;
  let error = '';

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      candidates = await ListPDFCoverCandidates(sourcePath);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function choosePage(page: number) {
    if (busy) return;
    busy = true;
    error = '';
    try {
      const coverPath = await SetCoverOverride(sourcePath, page);
      dispatch('updated', { coverPath, coverOverridden: true });
      dispatch('close');
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function uploadCustom() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      const picked = await PickCoverImageFile();
      if (!picked) return;
      const coverPath = await SetCoverOverrideCustomFromFile(sourcePath, picked);
      dispatch('updated', { coverPath, coverOverridden: true });
      dispatch('close');
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function resetToAuto() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      const coverPath = await ClearCoverOverride(sourcePath);
      dispatch('updated', { coverPath, coverOverridden: false });
      dispatch('close');
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="backdrop" on:click={() => dispatch('close')}>
  <div class="modal" on:click|stopPropagation>
    <div class="header">
      <h3>Choose cover</h3>
      <button type="button" class="close" on:click={() => dispatch('close')} aria-label="Close">×</button>
    </div>

    {#if error}
      <div class="banner error">{error}</div>
    {/if}

    {#if loading}
      <p>Loading pages…</p>
    {:else}
      <div class="grid">
        {#each candidates as candidate (candidate.page)}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="tile" on:click={() => choosePage(candidate.page)}>
            <img src={candidate.thumbnailUrl} alt={`Page ${candidate.page}`} />
            <span class="page-label">Page {candidate.page}</span>
          </div>
        {/each}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="tile upload" on:click={uploadCustom}>
          <span>Upload custom image…</span>
        </div>
      </div>
    {/if}

    {#if coverOverridden}
      <button type="button" class="reset" on:click={resetToAuto} disabled={busy}>Reset to auto-detected</button>
    {/if}
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }
  .modal {
    background: var(--bf-surface);
    border-radius: 8px;
    padding: 16px;
    width: min(560px, 90vw);
    max-height: 80vh;
    overflow-y: auto;
  }
  .header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
  }
  .close {
    background: none;
    border: none;
    font-size: 20px;
    cursor: pointer;
    color: var(--bf-text);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(90px, 1fr));
    gap: 10px;
  }
  .tile {
    cursor: pointer;
    border: 1px solid var(--bf-border);
    border-radius: 4px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    align-items: center;
  }
  .tile img {
    width: 100%;
    height: 110px;
    object-fit: cover;
    display: block;
  }
  .page-label {
    font-size: 11px;
    padding: 2px;
    color: var(--bf-text-muted);
  }
  .tile.upload {
    min-height: 110px;
    justify-content: center;
    text-align: center;
    font-size: 12px;
    padding: 8px;
    color: var(--bf-text-muted);
  }
  .reset {
    margin-top: 12px;
    width: 100%;
    padding: 8px;
    border-radius: 6px;
    border: 1px solid var(--bf-border);
    background: none;
    color: var(--bf-text);
    cursor: pointer;
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 8px 10px;
    border-radius: 6px;
    font-size: 12px;
    margin-bottom: 10px;
  }
</style>
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/CoverPickerModal.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/CoverPickerModal.svelte desktop/frontend/src/lib/CoverPickerModal.test.ts
git commit -m "Add CoverPickerModal for choosing/uploading/resetting a book's cover"
```

---

## Task 9: Wire the picker into `LibraryBookCard.svelte`

**Files:**
- Modify: `desktop/frontend/src/lib/LibraryBookCard.svelte`
- Modify: `desktop/frontend/src/lib/LibraryBookCard.test.ts`

**Interfaces:**
- Consumes: `CoverPickerModal` (Task 8).
- Produces: a hover-revealed "Choose cover…" / "Change cover…" button on the card that opens the modal; the card's own `book.coverPath`/`book.coverOverridden` update locally (no re-fetch) when the modal dispatches `updated`.

- [ ] **Step 1: Write the failing tests**

The existing `LibraryBookCard.test.ts` mocks `../../wailsjs/go/main/App` with only `{ OpenFile: vi.fn() }`. Since `LibraryBookCard.svelte` now imports `CoverPickerModal`, which itself imports `ListPDFCoverCandidates` etc. from that same module, that mock must be widened first -- otherwise `CoverPickerModal`'s `onMount` call to `ListPDFCoverCandidates` would hit `undefined` and throw. Replace the file's existing `vi.mock`/import block:

```ts
// Replace the existing vi.mock('../../wailsjs/go/main/App', ...) block and
// its "import { OpenFile } ..." line at the top of
// desktop/frontend/src/lib/LibraryBookCard.test.ts with:

vi.mock('../../wailsjs/go/main/App', () => ({
  OpenFile: vi.fn(),
  ListPDFCoverCandidates: vi.fn(),
  SetCoverOverride: vi.fn(),
  SetCoverOverrideCustomFromFile: vi.fn(),
  ClearCoverOverride: vi.fn(),
  PickCoverImageFile: vi.fn(),
}));

import { OpenFile, ListPDFCoverCandidates } from '../../wailsjs/go/main/App';
```

Then append, also widening `makeBook`'s return type to include `coverOverridden` (already added to `LibraryBookView` in Task 7) with a default of `false`:

```ts
// Append to desktop/frontend/src/lib/LibraryBookCard.test.ts

describe('cover override button', () => {
  it('shows "Choose cover…" for a book with no override', () => {
    render(LibraryBookCard, { book: makeBook({ coverOverridden: false }) });
    expect(screen.getByText('Choose cover…')).toBeInTheDocument();
  });

  it('shows "Change cover…" for an already-overridden book', () => {
    render(LibraryBookCard, { book: makeBook({ coverOverridden: true }) });
    expect(screen.getByText('Change cover…')).toBeInTheDocument();
  });

  it('clicking the button opens CoverPickerModal', async () => {
    vi.mocked(ListPDFCoverCandidates).mockResolvedValue([]);
    render(LibraryBookCard, { book: makeBook({ coverOverridden: false }) });

    await fireEvent.click(screen.getByText('Choose cover…'));

    // CoverPickerModal renders its own "Loading pages…" state initially.
    await screen.findByText('Loading pages…');
  });
});
```

`makeBook`'s existing default object literal needs `coverOverridden: false` added alongside its other defaults (`coverPath: ''`, etc.) so tests that don't pass it explicitly still satisfy `LibraryBookView`'s type.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/LibraryBookCard.test.ts`
Expected: FAIL (`Choose cover…` not found -- button doesn't exist yet)

- [ ] **Step 3: Implement**

Replace `desktop/frontend/src/lib/LibraryBookCard.svelte` in full:

```svelte
<script lang="ts">
  import type { LibraryBookView } from './types';
  import { OpenFile } from '../../wailsjs/go/main/App';
  import CoverPickerModal from './CoverPickerModal.svelte';

  export let book: LibraryBookView;

  let openError = '';
  let pickerOpen = false;

  function filenameNoExt(sourcePath: string): string {
    const base = sourcePath.split(/[\\/]+/).pop() ?? '';
    const dot = base.lastIndexOf('.');
    return dot > 0 ? base.slice(0, dot) : base;
  }

  async function open() {
    openError = '';
    try {
      await OpenFile(book.sourcePath);
    } catch (e) {
      openError = String(e);
    }
  }

  function onCoverUpdated(e: CustomEvent<{ coverPath: string; coverOverridden: boolean }>) {
    book = { ...book, coverPath: e.detail.coverPath, coverOverridden: e.detail.coverOverridden };
  }
</script>

<div class="tile">
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <!-- svelte-ignore a11y_click_events_have_key_events -- click-to-open is a
       supplementary affordance (like a file manager icon), matching
       BookCard.svelte's openOriginal pattern -->
  <div class="cover" on:click={open} title={filenameNoExt(book.sourcePath)}>
    {#if book.coverPath}
      <img src={book.coverPath} alt={book.title || filenameNoExt(book.sourcePath)} />
    {:else}
      <div class="placeholder">{book.title || filenameNoExt(book.sourcePath)}</div>
    {/if}
    <button
      type="button"
      class="cover-action"
      on:click|stopPropagation={() => (pickerOpen = true)}
    >
      {book.coverOverridden ? 'Change cover…' : 'Choose cover…'}
    </button>
  </div>
  {#if openError}
    <div class="banner error">{openError}</div>
  {/if}
</div>

{#if pickerOpen}
  <CoverPickerModal
    sourcePath={book.sourcePath}
    coverOverridden={book.coverOverridden}
    on:close={() => (pickerOpen = false)}
    on:updated={onCoverUpdated}
  />
{/if}

<style>
  .tile {
    width: 90px;
    flex-shrink: 0;
  }
  .cover {
    position: relative;
    width: 90px;
    height: 130px;
    border-radius: 4px;
    overflow: hidden;
    cursor: pointer;
    box-shadow: 0 2px 6px rgba(0, 0, 0, 0.3);
    background: var(--bf-surface);
    border: 1px solid var(--bf-border);
  }
  .cover img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .placeholder {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: flex-end;
    padding: 6px;
    font-size: 11px;
    line-height: 1.2;
    color: var(--bf-text-muted);
    background: repeating-linear-gradient(
      45deg,
      var(--bf-surface),
      var(--bf-surface) 8px,
      var(--bf-border) 8px,
      var(--bf-border) 16px
    );
  }
  .cover-action {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    padding: 4px 2px;
    font-size: 9.5px;
    line-height: 1.2;
    text-align: center;
    background: rgba(0, 0, 0, 0.65);
    color: white;
    border: none;
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.15s;
  }
  .cover:hover .cover-action {
    opacity: 1;
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 4px 6px;
    border-radius: 6px;
    font-size: 10px;
    margin-top: 4px;
  }
</style>
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/LibraryBookCard.test.ts`
Expected: PASS

Then run the full frontend suite: `cd desktop/frontend && npx vitest run`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/LibraryBookCard.svelte desktop/frontend/src/lib/LibraryBookCard.test.ts
git commit -m "Add a hover cover-override button to LibraryBookCard"
```

---

## Task 10: Manual verification

- [ ] **Step 1: Build and run the desktop app**

Run: `cd desktop && wails build` (or `wails dev`), launch it against a real library.

- [ ] **Step 2: Open the picker on a multi-image PDF**

Hover a PDF book card in the Library view, click "Choose cover…", confirm the thumbnail grid shows one tile per page-1-N image (matching the configured `pdf_cover_page_limit`) plus an "Upload custom image…" tile.

- [ ] **Step 3: Select a thumbnail**

Click a thumbnail other than the currently-auto-detected one. Confirm the modal closes and the book card's cover image updates immediately (no page reload or re-scan needed).

- [ ] **Step 4: Upload a custom image**

Reopen the picker (button should now read "Change cover…"), click "Upload custom image…", pick a local image file via the native dialog. Confirm the card updates to show the uploaded image.

- [ ] **Step 5: Reset to auto-detected**

Reopen the picker, click "Reset to auto-detected". Confirm the card reverts to the originally auto-detected cover (or the placeholder tile, if the book genuinely has none).

- [ ] **Step 6: Confirm persistence across a re-scan**

Navigate away from the Library view and back (or restart the app), confirming the override chosen in Step 3/4 (before the Step 5 reset) survives a fresh `ListLibrary` call -- i.e. it's read from `cover-overrides.json`, not just held in frontend state.
