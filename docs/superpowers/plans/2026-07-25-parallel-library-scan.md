# Parallel Library Scan Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Parallelize `internal/librarian.Scan`'s per-book work with a configurable concurrency setting, so a cold library scan uses multiple CPU cores instead of processing one book at a time.

**Architecture:** A new `general.scan_concurrency` config field controls a bounded goroutine pool in `librarian.Scan`. A `sync.Mutex` in `internal/metadata/pdf_render.go` makes the single-shared-instance PDFium renderer safe to call concurrently (an `internal/metadata` implementation detail, invisible to callers), and a second mutex in `librarian.Scan` guards the in-memory `librarycache.Cache`, the only other piece of shared mutable state in the scan loop.

**Tech Stack:** Go standard library only (`sync.WaitGroup`, a buffered channel as a semaphore, `sync.Mutex`) -- no new dependency.

## Global Constraints

- `general.scan_concurrency` (yaml key), `0` or unset means "use `runtime.NumCPU()`" -- resolved at the point of use in `librarian.Scan`, mirroring `pdf_cover_page_limit`'s existing "`<= 0` means use the built-in default" convention.
- The PDFium mutex lives in `internal/metadata/pdf_render.go`, not `internal/librarian` -- `metadata.Extract` becomes a documented "safe to call concurrently" function as a package-level guarantee, so `librarian.Scan` never needs to know PDFium exists.
- `librarycache.Cache` access (`Fresh` and `Put`) must be mutex-guarded whenever `librarian.Scan` runs concurrently -- it wraps a plain, non-thread-safe map.
- The parallel `librarian.Scan` must preserve today's behavior exactly: same `paths`-order output, same "one book's extraction failure never drops it or aborts the scan" behavior, same cache contents after a full scan.
- `librarian.Scan`'s public signature does not change.
- No new external dependency.

---

### Task 1: Add the `ScanConcurrency` config field

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `config.General.ScanConcurrency int` (yaml key `scan_concurrency`), consumed by Task 3.

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`, change the `sampleYAML` constant from:

```go
const sampleYAML = `
general:
  working_folder: "/inbox"
  library_folder: "/library"
  log_folder: "/library/.book-organiser-logs"
  filename_format: "{title} ({year}) - {author}"
  pdf_cover_page_limit: 10

heuristics:
```

to:

```go
const sampleYAML = `
general:
  working_folder: "/inbox"
  library_folder: "/library"
  log_folder: "/library/.book-organiser-logs"
  filename_format: "{title} ({year}) - {author}"
  pdf_cover_page_limit: 10
  scan_concurrency: 4

heuristics:
```

And add an assertion to `TestLoad`, right after the existing `PDFCoverPageLimit` assertion:

```go
	if cfg.General.PDFCoverPageLimit != 10 {
		t.Errorf("General.PDFCoverPageLimit = %d, want 10", cfg.General.PDFCoverPageLimit)
	}
	if cfg.General.ScanConcurrency != 4 {
		t.Errorf("General.ScanConcurrency = %d, want 4", cfg.General.ScanConcurrency)
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoad -v`
Expected: FAIL to compile (`cfg.General.ScanConcurrency` doesn't exist yet on the `General` struct).

- [ ] **Step 3: Add the field**

In `internal/config/config.go`, change:

```go
type General struct {
	WorkingFolder     string `yaml:"working_folder"`
	LibraryFolder     string `yaml:"library_folder"`
	LogFolder         string `yaml:"log_folder"`
	FilenameFormat    string `yaml:"filename_format"`
	PDFCoverPageLimit int    `yaml:"pdf_cover_page_limit"`
}
```

to:

```go
type General struct {
	WorkingFolder     string `yaml:"working_folder"`
	LibraryFolder     string `yaml:"library_folder"`
	LogFolder         string `yaml:"log_folder"`
	FilenameFormat    string `yaml:"filename_format"`
	PDFCoverPageLimit int    `yaml:"pdf_cover_page_limit"`
	// ScanConcurrency caps how many books internal/librarian.Scan
	// processes concurrently. 0 (the Go zero value, and what an unset
	// key in the user's config.yaml unmarshals to) means "use
	// runtime.NumCPU()" -- resolved at the point of use in Scan itself,
	// the same "<= 0 means use the built-in default" convention
	// PDFCoverPageLimit's own consumer (walkPDFPageTree) already uses.
	ScanConcurrency int `yaml:"scan_concurrency"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS (all tests, including `TestLoad` and `TestSaveLoadRoundTrip` -- the latter already does a whole-`General`-struct comparison, so it covers the new field automatically without needing its own edit).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "Add general.scan_concurrency config field"
```

---

### Task 2: Serialize PDFium access so metadata.Extract is safe to call concurrently

**Files:**
- Modify: `internal/metadata/pdf_render.go`
- Modify: `internal/metadata/extractor.go`
- Test: `internal/metadata/pdf_render_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `renderPDFPageAsCover` (pre-existing signature, unchanged) is now safe to call from multiple goroutines. New unexported `pdfiumMu sync.Mutex` in `pdf_render.go`, not consumed directly by any other task -- Task 3's `librarian.Scan` calls `metadata.Extract` without ever knowing this mutex exists.

- [ ] **Step 1: Write the failing test**

Add `"sync"`, `"sync/atomic"`, and `"time"` to `internal/metadata/pdf_render_test.go`'s import block, changing:

```go
import (
	"bytes"
	"fmt"
	"testing"
)
```

to:

```go
import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)
```

Add this test, after the file's existing imports/helpers (anywhere at top level is fine; append it at the end of the file):

```go
func TestPDFiumMutex_SerializesConcurrentAccess(t *testing.T) {
	// Directly exercises pdfiumMu itself (same package, so the unexported
	// mutex is reachable) rather than driving real, slow PDFium render
	// calls through renderPDFPageAsCover -- the property being verified
	// is "the mutex actually serializes overlapping callers," which
	// doesn't require a real PDF or a real render to prove.
	const goroutines = 20
	var active int32
	var maxActive int32
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pdfiumMu.Lock()
			n := atomic.AddInt32(&active, 1)
			for {
				m := atomic.LoadInt32(&maxActive)
				if n <= m || atomic.CompareAndSwapInt32(&maxActive, m, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&active, -1)
			pdfiumMu.Unlock()
		}()
	}
	wg.Wait()
	if maxActive != 1 {
		t.Errorf("maxActive = %d, want 1 (pdfiumMu did not serialize concurrent access)", maxActive)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/ -run TestPDFiumMutex_SerializesConcurrentAccess -v`
Expected: FAIL to compile (`pdfiumMu` doesn't exist yet).

- [ ] **Step 3: Add the mutex**

In `internal/metadata/pdf_render.go`, change:

```go
var (
	pdfiumInstance pdfium.Pdfium
	pdfiumInitOnce sync.Once
	pdfiumInitErr  error
)
```

to:

```go
var (
	pdfiumInstance pdfium.Pdfium
	pdfiumInitOnce sync.Once
	pdfiumInitErr  error
	// pdfiumMu serializes every call into the single shared PDFium WASM
	// instance above (webassembly.Init is configured with MaxTotal: 1 --
	// exactly one instance for the process's whole life, reused by every
	// render call). PDFium is not safe for concurrent use from multiple
	// goroutines against the same instance, so renderPDFPageAsCover holds
	// this for its entire body -- callers (metadata.Extract, and
	// therefore internal/librarian.Scan's parallel per-book workers) can
	// call it concurrently without knowing this constraint exists; only
	// the minority of PDFs that actually reach this render path
	// (composite covers, or an image filter this package's other
	// decoders can't handle) ever contend on this lock.
	pdfiumMu sync.Mutex
)
```

Replace `renderPDFPageAsCover`'s doc comment and the start of its body, changing:

```go
// renderPDFPageAsCover renders pageNum (1-based, matching this package's
// pdfPage.number convention) of the PDF in data to a full-page PNG image,
// compositing embedded images with any vector text/graphics drawn on top
// of them -- unlike this package's usual image-XObject extraction, which
// can only ever recover an embedded raster image on its own. ok is false
// (never an error) on any failure -- an unopenable/corrupt PDF, an
// out-of-range page, or a PDFium rendering failure -- matching this
// package's pervasive "one book's failure never fails the whole scan"
// convention.
func renderPDFPageAsCover(data []byte, pageNum int) (imageBytes []byte, contentType string, ok bool) {
	instance, err := getPdfiumInstance()
```

to:

```go
// renderPDFPageAsCover renders pageNum (1-based, matching this package's
// pdfPage.number convention) of the PDF in data to a full-page PNG image,
// compositing embedded images with any vector text/graphics drawn on top
// of them -- unlike this package's usual image-XObject extraction, which
// can only ever recover an embedded raster image on its own. ok is false
// (never an error) on any failure -- an unopenable/corrupt PDF, an
// out-of-range page, or a PDFium rendering failure -- matching this
// package's pervasive "one book's failure never fails the whole scan"
// convention. Safe to call concurrently from multiple goroutines: it
// holds pdfiumMu for its entire body, serializing access to the single
// shared PDFium instance internally, so callers never need their own
// synchronization around it.
func renderPDFPageAsCover(data []byte, pageNum int) (imageBytes []byte, contentType string, ok bool) {
	pdfiumMu.Lock()
	defer pdfiumMu.Unlock()

	instance, err := getPdfiumInstance()
```

In `internal/metadata/extractor.go`, change `Extract`'s doc comment from:

```go
// Extract dispatches to the appropriate format-specific extractor based on
// path's extension, then cleans the Title/Author it returns -- embedded
// metadata not infrequently carries a stray trailing "." or ";" (leftover
// sentence punctuation, or a dangling author-list separator), multiple
// authors are sometimes ";"-separated rather than the app's ","-separated
// convention, and titles sometimes use "_"/"-" as word separators or
// inconsistent casing. hyphenExceptions lists hyphenated words FormatTitle
// should keep hyphenated rather than splitting on "-"
// (cfg.TitleFormatting.HyphenExceptions). It is the only function other
// packages should call for whole-book extraction. ListPDFCoverCandidates
// and ExtractPDFPageCover (pdf_override.go) are the two exceptions: both
// exist specifically for the manual cover-override picker, which needs
// page-level granularity this combined Result can't expose.
func Extract(path string, hyphenExceptions []string, pdfCoverPageLimit int) (Result, error) {
```

to:

```go
// Extract dispatches to the appropriate format-specific extractor based on
// path's extension, then cleans the Title/Author it returns -- embedded
// metadata not infrequently carries a stray trailing "." or ";" (leftover
// sentence punctuation, or a dangling author-list separator), multiple
// authors are sometimes ";"-separated rather than the app's ","-separated
// convention, and titles sometimes use "_"/"-" as word separators or
// inconsistent casing. hyphenExceptions lists hyphenated words FormatTitle
// should keep hyphenated rather than splitting on "-"
// (cfg.TitleFormatting.HyphenExceptions). It is the only function other
// packages should call for whole-book extraction. ListPDFCoverCandidates
// and ExtractPDFPageCover (pdf_override.go) are the two exceptions: both
// exist specifically for the manual cover-override picker, which needs
// page-level granularity this combined Result can't expose. Safe to call
// concurrently for different files: this package's decoders operate only
// on their own local data, and the one piece of process-wide shared state
// (the PDFium renderer, pdf_render.go) serializes itself internally via
// pdfiumMu -- callers never need their own synchronization.
func Extract(path string, hyphenExceptions []string, pdfCoverPageLimit int) (Result, error) {
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the new one and every pre-existing test -- the mutex only adds a lock/unlock around `renderPDFPageAsCover`'s existing body, no behavior change for single-threaded callers).

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_render.go internal/metadata/extractor.go internal/metadata/pdf_render_test.go
git commit -m "Serialize PDFium access so metadata.Extract is safe to call concurrently"
```

---

### Task 3: Parallelize librarian.Scan

**Files:**
- Modify: `internal/librarian/librarian.go`
- Test: `internal/librarian/librarian_test.go`

**Interfaces:**
- Consumes: `cfg.General.ScanConcurrency` (Task 1), `metadata.Extract`'s now-documented concurrency safety (Task 2).
- Produces: `scanOneBook(cfg config.Config, forceRefresh bool, cache *librarycache.Cache, cacheMu *sync.Mutex, path string) (Book, bool)`, a new unexported function extracted from `Scan`'s current loop body -- consumed only by `Scan` in this same task. `Scan`'s own public signature is unchanged.

- [ ] **Step 1: Write the failing tests**

Add `"strconv"` to `internal/librarian/librarian_test.go`'s import block, changing:

```go
import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/librarycache"
	"github.com/FrancisChung/book-organiser/internal/metadata"
)
```

to:

```go
import (
	"archive/zip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/librarycache"
	"github.com/FrancisChung/book-organiser/internal/metadata"
)
```

Add these tests to `internal/librarian/librarian_test.go`, after the existing `TestScan_ForceRefreshBypassesFreshCache` test:

```go
func TestScan_ProcessesAllBooksCorrectlyUnderConcurrency(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	var paths []string
	for i := 0; i < 8; i++ {
		p := writeFixtureFile(t, libDir, filepath.Join("Fiction", "Book"+strconv.Itoa(i)+".epub"))
		paths = append(paths, p)
	}

	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		// Distinct, path-derived output: a concurrency bug that mixed up
		// results between goroutines (e.g. writing to the wrong slice
		// index) would be caught by the per-book assertion below, not
		// silently masked by every book getting an identical value.
		return metadata.Result{Title: "Title-for-" + filepath.Base(path)}, nil
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir, ScanConcurrency: 3}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != len(paths) {
		t.Fatalf("len(books) = %d, want %d", len(books), len(paths))
	}
	for i, b := range books {
		if b.SourcePath != paths[i] {
			t.Errorf("books[%d].SourcePath = %q, want %q (path order not preserved)", i, b.SourcePath, paths[i])
		}
		want := "Title-for-" + filepath.Base(paths[i])
		if b.Title != want {
			t.Errorf("books[%d].Title = %q, want %q (result mismatched to the wrong path -- a concurrency bug)", i, b.Title, want)
		}
	}
}

func TestScan_AllBooksCachedCorrectlyAfterConcurrentScan(t *testing.T) {
	libDir := t.TempDir()
	logDir := t.TempDir()
	var paths []string
	for i := 0; i < 6; i++ {
		p := filepath.Join(libDir, "Fiction", "Book"+strconv.Itoa(i)+".epub")
		writeEpubWithCover(t, p, []byte{0xFF, 0xD8, 0xFF, 0xE0})
		paths = append(paths, p)
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir, ScanConcurrency: 4}}
	if _, err := Scan(cfg, false); err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	reloaded := librarycache.Load(logDir)
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if _, ok := reloaded.Fresh(p, info.ModTime(), info.Size()); !ok {
			t.Errorf("no cache entry found for %s after a concurrent scan (a lost update racing on the cache mutex)", p)
		}
	}
}

func TestScan_ZeroScanConcurrencyStillWorks(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	writeFixtureFile(t, libDir, filepath.Join("Fiction", "Foundation.epub"))

	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		return metadata.Result{Title: "Foundation"}, nil
	}

	// ScanConcurrency left at its zero value (unset) -- matching what an
	// existing config.yaml with no scan_concurrency key unmarshals to --
	// must default to a working concurrency (runtime.NumCPU()), not zero
	// goroutines / a deadlock on the semaphore channel.
	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 1 || books[0].Title != "Foundation" {
		t.Errorf("books = %+v, want one book titled Foundation", books)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/librarian/ -run TestScan_ProcessesAllBooksCorrectlyUnderConcurrency -v`
Expected: FAIL -- either a compile error (`ScanConcurrency` field exists from Task 1, so this should compile) or, more likely, the test passes trivially today since `Scan` is already sequential and correctness-preserving -- **this is expected**: these three tests are regression guards for the *parallel* implementation, not proof of a current bug. Confirm they compile and pass against today's sequential code (establishing a clean baseline) before proceeding to Step 3's refactor.

- [ ] **Step 3: Parallelize the Scan loop**

In `internal/librarian/librarian.go`, add `"runtime"` and `"sync"` to the import block, changing:

```go
import (
	"os"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/heuristics"
	"github.com/FrancisChung/book-organiser/internal/librarycache"
	"github.com/FrancisChung/book-organiser/internal/metadata"
	"github.com/FrancisChung/book-organiser/internal/scanner"
)
```

to:

```go
import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/heuristics"
	"github.com/FrancisChung/book-organiser/internal/librarycache"
	"github.com/FrancisChung/book-organiser/internal/metadata"
	"github.com/FrancisChung/book-organiser/internal/scanner"
)
```

Replace the whole of `Scan`, changing:

```go
func Scan(cfg config.Config, forceRefresh bool) ([]Book, error) {
	paths, err := scanner.Scan(cfg.General.LibraryFolder)
	if err != nil {
		return nil, err
	}

	cache := librarycache.Load(cfg.General.LogFolder)
	seen := make(map[string]bool, len(paths))

	books := make([]Book, 0, len(paths))
	for _, path := range paths {
		seen[path] = true

		rel, err := filepath.Rel(cfg.General.LibraryFolder, path)
		if err != nil {
			continue
		}
		parts := strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/")

		b := Book{
			SourcePath: path,
			Format:     strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		}
		if len(parts) >= 1 && parts[0] != "." {
			b.Category = parts[0]
		}
		if len(parts) >= 2 {
			b.Subcategory = parts[1]
		}

		info, statErr := os.Stat(path)
		if statErr != nil {
			books = append(books, b)
			continue
		}

		if !forceRefresh {
			if entry, ok := cache.Fresh(path, info.ModTime(), info.Size()); ok && entry.CoverVersion == metadata.CoverExtractorVersion && entry.MetadataVersion == metadata.MetadataExtractorVersion {
				b.Title = entry.Title
				b.Author = entry.Author
				b.Year = entry.Year
				b.CoverPath = entry.CoverPath
				b.CoverOverridden = entry.CoverOverridden
				books = append(books, b)
				continue
			}
		}

		if res, err := extractFunc(path, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit); err == nil {
			b.Title = res.Title
			b.Author = res.Author
			b.Year = res.Year

			applyFilenameHeuristicFallback(&b, path, cfg.Heuristics.KnownJunkTags)

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
					coverBytes = nil // already have a stable URL; skip covercache.Force below
				case covercache.OverrideEmbedded:
					if data, ct, ok, pageErr := metadata.ExtractPDFPageCover(path, ov.Page); pageErr == nil && ok {
						coverBytes, coverContentType = data, ct
					} else {
						coverBytes = nil
					}
				}
			}

			if len(coverBytes) > 0 {
				if coverURL, err := covercache.Force(cfg.General.LogFolder, path, coverBytes, coverContentType); err == nil {
					b.CoverPath = coverURL
				}
			}

			cache.Put(path, librarycache.Entry{
				ModTime:         info.ModTime(),
				Size:            info.Size(),
				Title:           b.Title,
				Author:          b.Author,
				Year:            b.Year,
				Category:        b.Category,
				Subcategory:     b.Subcategory,
				CoverPath:       b.CoverPath,
				CoverOverridden: b.CoverOverridden,
				CoverVersion:    metadata.CoverExtractorVersion,
				MetadataVersion: metadata.MetadataExtractorVersion,
			})
		} else {
			// extractFunc failed (e.g. a corrupt file) -- this book is
			// never cached (see this function's doc comment), so it gets
			// no cache-hit benefit from the fallback below, but it should
			// still get a best-effort Title/Author/Year rather than
			// staying blank until the frontend's raw-filename fallback
			// takes over.
			applyFilenameHeuristicFallback(&b, path, cfg.Heuristics.KnownJunkTags)
		}

		books = append(books, b)
	}

	cache.Keep(seen)
	_ = cache.Save(cfg.General.LogFolder) // best-effort: a save failure shouldn't fail this Scan's results

	return books, nil
}
```

to:

```go
func Scan(cfg config.Config, forceRefresh bool) ([]Book, error) {
	paths, err := scanner.Scan(cfg.General.LibraryFolder)
	if err != nil {
		return nil, err
	}

	cache := librarycache.Load(cfg.General.LogFolder)
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		seen[path] = true
	}

	concurrency := cfg.General.ScanConcurrency
	if concurrency <= 0 {
		concurrency = runtime.NumCPU()
	}

	results := make([]Book, len(paths))
	included := make([]bool, len(paths))
	var cacheMu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, path := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i], included[i] = scanOneBook(cfg, forceRefresh, &cache, &cacheMu, path)
		}(i, path)
	}
	wg.Wait()

	books := make([]Book, 0, len(paths))
	for i, b := range results {
		if included[i] {
			books = append(books, b)
		}
	}

	cache.Keep(seen)
	_ = cache.Save(cfg.General.LogFolder) // best-effort: a save failure shouldn't fail this Scan's results

	return books, nil
}

// scanOneBook resolves a single book at path -- its Category/Subcategory
// from its position under cfg.General.LibraryFolder, then either a cache
// hit or a full metadata.Extract-then-override-check -- the single-book
// logic Scan's loop used to run inline, extracted so a bounded pool of
// goroutines can each run one call of this function concurrently. ok is
// false only when path can't be made relative to
// cfg.General.LibraryFolder (mirrors the original loop's "continue"
// entirely skipping that path, rather than adding an empty entry for
// it). cache and cacheMu are shared across every concurrent call: cacheMu
// must be held around every cache.Fresh/cache.Put call (librarycache.Cache
// wraps a plain, non-thread-safe map), but never around the expensive
// metadata.Extract/covercache work in between -- concurrent extraction is
// what actually gets parallelized, not serialized behind the same lock.
func scanOneBook(cfg config.Config, forceRefresh bool, cache *librarycache.Cache, cacheMu *sync.Mutex, path string) (Book, bool) {
	rel, err := filepath.Rel(cfg.General.LibraryFolder, path)
	if err != nil {
		return Book{}, false
	}
	parts := strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/")

	b := Book{
		SourcePath: path,
		Format:     strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
	if len(parts) >= 1 && parts[0] != "." {
		b.Category = parts[0]
	}
	if len(parts) >= 2 {
		b.Subcategory = parts[1]
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		return b, true
	}

	if !forceRefresh {
		cacheMu.Lock()
		entry, ok := cache.Fresh(path, info.ModTime(), info.Size())
		cacheMu.Unlock()
		if ok && entry.CoverVersion == metadata.CoverExtractorVersion && entry.MetadataVersion == metadata.MetadataExtractorVersion {
			b.Title = entry.Title
			b.Author = entry.Author
			b.Year = entry.Year
			b.CoverPath = entry.CoverPath
			b.CoverOverridden = entry.CoverOverridden
			return b, true
		}
	}

	if res, err := extractFunc(path, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit); err == nil {
		b.Title = res.Title
		b.Author = res.Author
		b.Year = res.Year

		applyFilenameHeuristicFallback(&b, path, cfg.Heuristics.KnownJunkTags)

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
				coverBytes = nil // already have a stable URL; skip covercache.Force below
			case covercache.OverrideEmbedded:
				if data, ct, ok, pageErr := metadata.ExtractPDFPageCover(path, ov.Page); pageErr == nil && ok {
					coverBytes, coverContentType = data, ct
				} else {
					coverBytes = nil
				}
			}
		}

		if len(coverBytes) > 0 {
			if coverURL, err := covercache.Force(cfg.General.LogFolder, path, coverBytes, coverContentType); err == nil {
				b.CoverPath = coverURL
			}
		}

		cacheMu.Lock()
		cache.Put(path, librarycache.Entry{
			ModTime:         info.ModTime(),
			Size:            info.Size(),
			Title:           b.Title,
			Author:          b.Author,
			Year:            b.Year,
			Category:        b.Category,
			Subcategory:     b.Subcategory,
			CoverPath:       b.CoverPath,
			CoverOverridden: b.CoverOverridden,
			CoverVersion:    metadata.CoverExtractorVersion,
			MetadataVersion: metadata.MetadataExtractorVersion,
		})
		cacheMu.Unlock()
	} else {
		// extractFunc failed (e.g. a corrupt file) -- this book is never
		// cached (see Scan's doc comment), so it gets no cache-hit
		// benefit from the fallback below, but it should still get a
		// best-effort Title/Author/Year rather than staying blank until
		// the frontend's raw-filename fallback takes over.
		applyFilenameHeuristicFallback(&b, path, cfg.Heuristics.KnownJunkTags)
	}

	return b, true
}
```

Update `Scan`'s own doc comment (directly above `func Scan(...)`) to mention the new concurrency model, changing the final sentence:

```go
// removed. Files no longer present on disk are dropped from the
// saved cache. A file metadata.Extract fails on (e.g. corrupt) still gets
// a Book entry (with an empty CoverPath, and Title/Author/Year filled in
// on a best-effort basis by applyFilenameHeuristicFallback) rather than
// being dropped, so it's still visible on its shelf; such a file is never
// cached, so it's retried on every subsequent Scan until it succeeds or is
// removed.
func Scan(cfg config.Config, forceRefresh bool) ([]Book, error) {
```

to:

```go
// removed. Files no longer present on disk are dropped from the
// saved cache. A file metadata.Extract fails on (e.g. corrupt) still gets
// a Book entry (with an empty CoverPath, and Title/Author/Year filled in
// on a best-effort basis by applyFilenameHeuristicFallback) rather than
// being dropped, so it's still visible on its shelf; such a file is never
// cached, so it's retried on every subsequent Scan until it succeeds or is
// removed.
//
// Every path's single-book work (scanOneBook, below) runs concurrently,
// bounded by cfg.General.ScanConcurrency (0/unset means
// runtime.NumCPU()) via a semaphore channel + WaitGroup -- safe because
// metadata.Extract is documented safe for concurrent use (see its own
// doc comment) and the one other piece of state shared across workers,
// the in-memory cache, is guarded by its own mutex (see scanOneBook).
// The returned slice preserves paths' original order regardless of which
// goroutine finishes first, matching the pre-parallel behavior exactly.
func Scan(cfg config.Config, forceRefresh bool) ([]Book, error) {
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/librarian/... -v`
Expected: PASS (all tests, including the three new ones and every pre-existing `Scan` test).

- [ ] **Step 5: Run the full metadata and librarian test suites under the race detector**

Run: `go test -race ./internal/librarian/... ./internal/metadata/...`
Expected: PASS with no data race reports. This is the single most important verification step in this whole plan -- it exercises every existing test (which already covers many `Scan`/`Extract` code paths) under Go's race detector, catching any unsynchronized access this plan's manual review missed, not just the three concurrency-specific tests added above.

- [ ] **Step 6: Commit**

```bash
git add internal/librarian/librarian.go internal/librarian/librarian_test.go
git commit -m "Parallelize librarian.Scan's per-book work"
```

---

## Manual Verification (after all tasks complete)

1. Run `go build ./... && go vet ./...` at the repo root to confirm the whole module still builds cleanly.
2. In the desktop app (or via a quick throwaway Go program calling `librarian.Scan` directly against the real library), compare wall-clock time for a forced full rescan (`forceRefresh=true`, so every book re-extracts) with `scan_concurrency: 1` in `config.yaml` versus `scan_concurrency` unset (defaults to `runtime.NumCPU()`) -- confirming the parallelization actually reduces scan time on the real library, not just in theory.
3. Confirm the Library view still shows every book with correct Title/Author/Year/cover after a real scan -- no regressions from the refactor.
