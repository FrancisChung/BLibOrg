# Library Scan Cache (reconciled with Cover Overrides) + Shelf Scroll Nav Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `librarian.Scan`/`appapi.ListLibrary` skip the expensive `metadata.Extract`/cover-detection work for files that haven't changed since the last scan, while guaranteeing cover-override changes (set/clear) are always reflected correctly with no staleness window — plus land the already-designed shelf-scroll-navigation UI improvement that shipped alongside the original (pre-reconciliation) version of this cache.

**Architecture:** New `internal/librarycache` package persists a path-keyed, ModTime+Size-validated cache of each book's *final, override-resolved* fields (`Title/Author/Year/Category/Subcategory/CoverPath/CoverOverridden`). `librarian.Scan` gains a `forceRefresh bool` param and serves cache hits with zero calls into `metadata`/`covercache`; on a miss it runs today's extract-then-override-check logic unchanged and writes the resolved result to the cache. `appapi.SetCoverOverride`/`SetCoverOverrideCustom`/`ClearCoverOverride` invalidate the affected book's cache entry immediately, which is what keeps the cache honest — a cache hit is never re-validated against `covercache.GetOverride`, so any override change *must* go through invalidation or the next `Scan` would show stale data forever (until that file's mtime/size happens to change). The frontend gets a `forceRefresh` param threaded through `ListLibrary`, a manual "Refresh" button, and a `ShelfRow` component (ported unchanged from the design's reference branch) adding prev/next scroll buttons to each subcategory shelf.

**Tech Stack:** Go 1.x backend (`internal/librarycache`, `internal/librarian`, `internal/appapi`), Svelte + TypeScript frontend (`desktop/frontend/src/lib`), Wails v2 bindings (regenerated via `wails generate module`, never hand-edited).

## Global Constraints

- Design spec: `docs/superpowers/specs/2026-07-24-library-scan-cache-cover-overrides-design.md` — read it in full before starting; every task below implements one section of it.
- This work happens on a **new branch/worktree off current `main`**, not the old `.claude/worktrees/library-scan-cache` worktree (branch `worktree-library-scan-cache`), which predates Plan B/C and is left untouched as reference only. Suggested branch name: `library-scan-cache-v2` (worktree at `.claude/worktrees/library-scan-cache-v2`) — set this up via the `superpowers:using-git-worktrees` skill before Task 1.
- Reference-only, do not copy files directly (the override/cache interaction differs): `.claude/worktrees/library-scan-cache/internal/librarian/librarian.go`, `.claude/worktrees/library-scan-cache/internal/appapi/library.go`.
- Safe to port near-verbatim (confirmed conflict-free, no override interaction): `.claude/worktrees/library-scan-cache/internal/librarycache/librarycache.go` (core Load/Save/Fresh/Put/Keep — this plan adds `CoverOverridden`/`Delete`/`Invalidate` on top), `.claude/worktrees/library-scan-cache/desktop/frontend/src/lib/ShelfRow.svelte` and `ShelfRow.test.ts`.
- `internal/librarian`'s existing override-check block (in the current `main` `Scan` function) must be preserved **exactly** on the cache-miss path — it already has its own passing tests (`TestScan_NoOverrideUsesAutoDetectedCover`, `TestScan_EmbeddedOverridePinsSpecificPage`, `TestScan_CustomOverrideUsesStoredImagePath`, `TestScan_OverrideDoesNotBlankTitleAuthorYear` in `internal/librarian/librarian_test.go`) that must keep passing unmodified except for the `Scan(cfg)` → `Scan(cfg, false)` call-site update.
- Never hand-edit `desktop/frontend/wailsjs/go/main/App.d.ts`, `App.js`, or `desktop/frontend/wailsjs/go/models.ts` — always regenerate via `cd desktop && wails generate module` after a Go-side Wails-bound signature change.
- Run `go build ./...`, `go vet ./...`, and `go test ./...` after every backend task; run `npx vitest run` (from `desktop/frontend`) after every frontend task. All must stay green.

---

### Task 1: `internal/librarycache` package

**Files:**
- Create: `internal/librarycache/librarycache.go`
- Create: `internal/librarycache/librarycache_test.go`

**Interfaces:**
- Produces (used by Task 2 and Task 3):
  - `type Entry struct { ModTime time.Time; Size int64; Title, Author, Year, Category, Subcategory, CoverPath string; CoverOverridden bool }`
  - `type Cache struct { ... }` (zero value valid)
  - `func Load(logFolder string) Cache`
  - `func (c Cache) Fresh(sourcePath string, modTime time.Time, size int64) (Entry, bool)`
  - `func (c *Cache) Put(sourcePath string, entry Entry)`
  - `func (c *Cache) Delete(sourcePath string)`
  - `func (c *Cache) Keep(seen map[string]bool)`
  - `func (c Cache) Dirty() bool`
  - `func (c *Cache) Save(logFolder string) error`
  - `func Invalidate(logFolder, sourcePath string) error`

- [ ] **Step 1: Write `internal/librarycache/librarycache.go`**

```go
// Package librarycache persists internal/librarian.Scan's derived,
// already override-resolved per-book fields
// (Title/Author/Year/Category/Subcategory/CoverPath/CoverOverridden) keyed
// by source path, so a Scan of an unchanged library can skip the expensive
// metadata.Extract/covercache.Ensure/covercache.GetOverride trio entirely
// for files whose ModTime and Size haven't changed since they were last
// cached. Because the stored CoverPath/CoverOverridden are already
// override-resolved (not the "raw" auto-detected result), any cover
// override change (internal/appapi's SetCoverOverride, SetCoverOverrideCustom,
// ClearCoverOverride) MUST call Invalidate for the affected path -- a cache
// hit is never re-checked against the override store, so a missed
// invalidation would silently serve a stale cover indefinitely.
package librarycache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Entry is one cached book's derived fields, valid as long as the source
// file's ModTime and Size match what was recorded when it was cached.
type Entry struct {
	ModTime         time.Time `json:"modTime"`
	Size            int64     `json:"size"`
	Title           string    `json:"title"`
	Author          string    `json:"author"`
	Year            string    `json:"year"`
	Category        string    `json:"category"`
	Subcategory     string    `json:"subcategory"`
	CoverPath       string    `json:"coverPath"`
	CoverOverridden bool      `json:"coverOverridden"`
}

// Cache is an in-memory, path-keyed view of the persisted library scan
// cache. The zero value is a valid empty cache.
type Cache struct {
	entries map[string]Entry
	dirty   bool
}

const cacheFileName = "library-cache.json"

func cachePath(logFolder string) string {
	return filepath.Join(logFolder, cacheFileName)
}

// Load reads the cache file under logFolder. A missing or corrupt file
// returns an empty, valid Cache rather than an error -- the cache is purely
// an optimization; losing it just means the next Scan re-extracts
// everything, the same as having no cache at all.
func Load(logFolder string) Cache {
	data, err := os.ReadFile(cachePath(logFolder))
	if err != nil {
		return Cache{entries: map[string]Entry{}}
	}
	var entries map[string]Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return Cache{entries: map[string]Entry{}}
	}
	if entries == nil {
		entries = map[string]Entry{}
	}
	return Cache{entries: entries}
}

// Fresh returns the cached entry for sourcePath and whether it's still
// valid for a file with the given current modTime and size. A cache miss,
// or a modTime/size mismatch (the file was edited), both report ok=false.
func (c Cache) Fresh(sourcePath string, modTime time.Time, size int64) (Entry, bool) {
	entry, found := c.entries[sourcePath]
	if !found || !entry.ModTime.Equal(modTime) || entry.Size != size {
		return Entry{}, false
	}
	return entry, true
}

// Put records or replaces the cached entry for sourcePath.
func (c *Cache) Put(sourcePath string, entry Entry) {
	if c.entries == nil {
		c.entries = map[string]Entry{}
	}
	c.entries[sourcePath] = entry
	c.dirty = true
}

// Delete removes sourcePath's cached entry, if any, and unconditionally
// marks the cache dirty -- even when sourcePath had no entry to begin
// with. This is deliberate: Delete backs cover-override invalidation
// (Invalidate, below), and the caller needs Save to actually attempt a
// write (and so report any I/O failure) regardless of whether this
// specific path happened to be cached yet.
func (c *Cache) Delete(sourcePath string) {
	delete(c.entries, sourcePath)
	c.dirty = true
}

// Keep drops every cached entry whose path is not in seen, so files
// deleted or moved out of the library folder since the last scan don't
// linger in the saved cache forever.
func (c *Cache) Keep(seen map[string]bool) {
	for path := range c.entries {
		if !seen[path] {
			delete(c.entries, path)
			c.dirty = true
		}
	}
}

// Dirty reports whether the cache has unsaved changes since it was loaded
// (or since the last Save).
func (c Cache) Dirty() bool {
	return c.dirty
}

// Save writes the cache to logFolder if it has unsaved changes; a no-op
// otherwise, so an all-cache-hit Scan doesn't rewrite the file every time.
func (c *Cache) Save(logFolder string) error {
	if !c.dirty {
		return nil
	}
	data, err := json.Marshal(c.entries)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(logFolder, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(cachePath(logFolder), data, 0644); err != nil {
		return err
	}
	c.dirty = false
	return nil
}

// Invalidate is the Load-Delete-Save round trip cover-override changes use
// to drop one book's cached entry outside of a Scan call, forcing the next
// Scan to treat that file as a miss and re-resolve it (which re-checks the
// override store, so it picks up the just-changed override correctly).
func Invalidate(logFolder, sourcePath string) error {
	c := Load(logFolder)
	c.Delete(sourcePath)
	return c.Save(logFolder)
}
```

- [ ] **Step 2: Write `internal/librarycache/librarycache_test.go`**

```go
package librarycache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_MissingFileReturnsEmptyCache(t *testing.T) {
	c := Load(t.TempDir())
	if _, ok := c.Fresh("/some/path.epub", time.Now(), 100); ok {
		t.Error("Fresh() = true for empty cache, want false")
	}
}

func TestLoad_CorruptFileReturnsEmptyCache(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "library-cache.json"), []byte("not json"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	c := Load(dir)
	if _, ok := c.Fresh("/some/path.epub", time.Now(), 100); ok {
		t.Error("Fresh() = true for corrupt cache, want false")
	}
}

func TestPutThenFresh_MatchingModTimeAndSizeIsFresh(t *testing.T) {
	var c Cache
	modTime := time.Now().Truncate(time.Second)
	c.Put("/book.epub", Entry{ModTime: modTime, Size: 100, Title: "Foundation"})

	entry, ok := c.Fresh("/book.epub", modTime, 100)
	if !ok {
		t.Fatal("Fresh() = false, want true")
	}
	if entry.Title != "Foundation" {
		t.Errorf("Title = %q, want Foundation", entry.Title)
	}
}

func TestFresh_DifferentModTimeIsStale(t *testing.T) {
	var c Cache
	modTime := time.Now().Truncate(time.Second)
	c.Put("/book.epub", Entry{ModTime: modTime, Size: 100})

	if _, ok := c.Fresh("/book.epub", modTime.Add(time.Hour), 100); ok {
		t.Error("Fresh() = true for a changed modTime, want false")
	}
}

func TestFresh_DifferentSizeIsStale(t *testing.T) {
	var c Cache
	modTime := time.Now().Truncate(time.Second)
	c.Put("/book.epub", Entry{ModTime: modTime, Size: 100})

	if _, ok := c.Fresh("/book.epub", modTime, 200); ok {
		t.Error("Fresh() = true for a changed size, want false")
	}
}

func TestKeep_DropsEntriesNotInSeen(t *testing.T) {
	var c Cache
	c.Put("/a.epub", Entry{Title: "A"})
	c.Put("/b.epub", Entry{Title: "B"})

	c.Keep(map[string]bool{"/a.epub": true})

	if _, ok := c.Fresh("/a.epub", time.Time{}, 0); !ok {
		t.Error("Fresh(/a.epub) = false after Keep, want true (still present)")
	}
	if _, ok := c.Fresh("/b.epub", time.Time{}, 0); ok {
		t.Error("Fresh(/b.epub) = true after Keep, want false (dropped)")
	}
}

func TestSaveThenLoad_RoundTripsEntriesIncludingCoverOverridden(t *testing.T) {
	dir := t.TempDir()
	modTime := time.Now().Truncate(time.Second)

	var c Cache
	c.Put("/book.epub", Entry{
		ModTime: modTime, Size: 100,
		Title: "Foundation", Author: "Isaac Asimov", Year: "1951",
		Category: "Fiction", Subcategory: "Sci-Fi", CoverPath: "/covers/abc.jpg",
		CoverOverridden: true,
	})
	if err := c.Save(dir); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded := Load(dir)
	entry, ok := loaded.Fresh("/book.epub", modTime, 100)
	if !ok {
		t.Fatal("Fresh() = false after round-trip, want true")
	}
	if entry.Title != "Foundation" || entry.Author != "Isaac Asimov" || entry.Year != "1951" ||
		entry.Category != "Fiction" || entry.Subcategory != "Sci-Fi" || entry.CoverPath != "/covers/abc.jpg" ||
		!entry.CoverOverridden {
		t.Errorf("entry = %+v, want all fields round-tripped incl. CoverOverridden=true", entry)
	}
}

func TestSave_NoOpWhenNotDirty(t *testing.T) {
	dir := t.TempDir()
	c := Load(dir) // empty, not dirty
	if err := c.Save(dir); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "library-cache.json")); !os.IsNotExist(err) {
		t.Error("Save wrote a file for an unchanged empty cache, want no-op")
	}
}

func TestDelete_RemovesEntryAndMarksDirty(t *testing.T) {
	var c Cache
	c.Put("/book.epub", Entry{Title: "Foundation"})
	c.dirty = false // simulate a freshly-Saved, clean state before Delete

	c.Delete("/book.epub")

	if _, ok := c.Fresh("/book.epub", time.Time{}, 0); ok {
		t.Error("Fresh() = true after Delete, want false")
	}
	if !c.Dirty() {
		t.Error("Dirty() = false after Delete, want true")
	}
}

func TestDelete_OfAbsentPathStillMarksDirty(t *testing.T) {
	var c Cache
	c.Delete("/never-cached.epub")
	if !c.Dirty() {
		t.Error("Dirty() = false after Delete of an absent path, want true (Save must still be attempted)")
	}
}

func TestInvalidate_RemovesPersistedEntry(t *testing.T) {
	dir := t.TempDir()
	modTime := time.Now().Truncate(time.Second)

	var c Cache
	c.Put("/book.epub", Entry{ModTime: modTime, Size: 100, Title: "Foundation"})
	if err := c.Save(dir); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	if err := Invalidate(dir, "/book.epub"); err != nil {
		t.Fatalf("Invalidate returned error: %v", err)
	}

	reloaded := Load(dir)
	if _, ok := reloaded.Fresh("/book.epub", modTime, 100); ok {
		t.Error("Fresh() = true after Invalidate, want false (entry was removed and the removal persisted)")
	}
}

func TestInvalidate_PropagatesSaveFailure(t *testing.T) {
	dir := t.TempDir()
	// Make the cache file's own path a directory instead of a writable
	// file, so Cache.Save's os.WriteFile fails with EISDIR -- proving
	// Invalidate surfaces a real I/O failure rather than swallowing it.
	if err := os.MkdirAll(filepath.Join(dir, "library-cache.json"), 0755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	if err := Invalidate(dir, "/book.epub"); err == nil {
		t.Error("Invalidate returned nil error, want an error from the blocked write")
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `go test ./internal/librarycache/... -v`
Expected: all tests PASS (this is a brand new package, nothing to regress).

- [ ] **Step 4: Build and vet**

Run: `go build ./... && go vet ./internal/librarycache/...`
Expected: clean, no output.

- [ ] **Step 5: Commit**

```bash
git add internal/librarycache/librarycache.go internal/librarycache/librarycache_test.go
git commit -m "Add internal/librarycache: persisted, override-aware library scan cache"
```

---

### Task 2: `librarian.Scan` becomes cache-aware

**Files:**
- Modify: `internal/librarian/librarian.go`
- Modify: `internal/librarian/librarian_test.go`

**Interfaces:**
- Consumes: `librarycache.Load`, `Cache.Fresh`, `Cache.Put`, `Cache.Keep`, `Cache.Save`, `librarycache.Entry` (Task 1).
- Produces: `func Scan(cfg config.Config, forceRefresh bool) ([]Book, error)` (breaking signature change consumed by Task 3's `appapi.ListLibrary`). `Book` gains no new fields (`CoverOverridden` already exists on `main`).

- [ ] **Step 1: Replace `internal/librarian/librarian.go` in full**

```go
// Package librarian walks the already-organized library folder
// (cfg.General.LibraryFolder) and reports what's in it, grouped by the
// Category/Subcategory folder structure rename.BuildPath already produces.
// Unlike internal/pipeline, it never computes a destination or moves
// anything -- it only reads back what's already there.
//
// Title/Author/Year/CoverPath/CoverOverridden come from a persisted scan
// cache (internal/librarycache) keyed by each file's ModTime and Size when
// possible; the expensive metadata.Extract/covercache.GetOverride/
// covercache.Ensure trio only runs for a file that's new, edited, or when
// forceRefresh is true. Because the cached fields are the *final,
// override-resolved* result, a cache hit never re-checks the override
// store -- internal/appapi's SetCoverOverride/SetCoverOverrideCustom/
// ClearCoverOverride are responsible for calling librarycache.Invalidate
// so the next Scan treats a changed book as a miss and re-resolves it.
package librarian

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/BLibOrg/internal/config"
	"github.com/FrancisChung/BLibOrg/internal/covercache"
	"github.com/FrancisChung/BLibOrg/internal/librarycache"
	"github.com/FrancisChung/BLibOrg/internal/metadata"
	"github.com/FrancisChung/BLibOrg/internal/scanner"
)

// Book is one already-organized library file, with Category/Subcategory
// read directly from its folder location rather than recomputed.
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

// extractFunc is a seam so tests can verify metadata.Extract is skipped for
// a cache hit; production code always uses metadata.Extract.
var extractFunc = metadata.Extract

// Scan walks cfg.General.LibraryFolder for every supported ebook file,
// deriving each book's Category/Subcategory from its position in the
// <library>/<Category>/<Subcategory>/<file> layout rename.BuildPath
// produces. A file sitting directly in <library>/ (no Category folder) or
// in <library>/<Category>/ with no Subcategory folder gets an empty
// Subcategory (and, for the former, an empty Category too) rather than
// being skipped -- Scan reports what it finds, it doesn't enforce layout.
//
// Per-file Title/Author/Year/CoverPath/CoverOverridden are served from the
// persisted scan cache whenever the file's current ModTime and Size match
// what's cached, skipping metadata.Extract and cover resolution entirely.
// A cache miss (new file, edited file) or forceRefresh=true runs today's
// extract-then-override-check logic and updates the cache with the
// resolved result. Files no longer present on disk are dropped from the
// saved cache. A file metadata.Extract fails on (e.g. corrupt) still gets
// a Book entry with empty Title/Author/Year/CoverPath rather than being
// dropped, so it's still visible on its shelf; such a file is never
// cached, so it's retried on every subsequent Scan until it succeeds or is
// removed.
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
			if entry, ok := cache.Fresh(path, info.ModTime(), info.Size()); ok {
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
				if coverURL, err := covercache.Ensure(cfg.General.LogFolder, path, info.ModTime(), coverBytes, coverContentType); err == nil {
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
			})
		}

		books = append(books, b)
	}

	cache.Keep(seen)
	_ = cache.Save(cfg.General.LogFolder) // best-effort: a save failure shouldn't fail this Scan's results

	return books, nil
}
```

- [ ] **Step 2: Update existing test call sites in `internal/librarian/librarian_test.go`**

Every existing `Scan(cfg)` call becomes `Scan(cfg, false)`. There are 8 call sites in the current file (`TestScan_GroupsByCategoryAndSubcategory`, `TestScan_FileDirectlyInLibraryRootHasNoCategory`, `TestScan_EmptyLibraryReturnsEmptySlice`, `TestScan_PopulatesCoverPathAndMetadataWhenCoverExists`, `TestScan_NoOverrideUsesAutoDetectedCover`, `TestScan_EmbeddedOverridePinsSpecificPage`, `TestScan_CustomOverrideUsesStoredImagePath`, `TestScan_OverrideDoesNotBlankTitleAuthorYear`). Change each `Scan(cfg)` to `Scan(cfg, false)`. Do not change anything else in these tests — they must keep passing as-is otherwise, proving the override-check logic is untouched.

- [ ] **Step 3: Add the import for `librarycache` and new cache-behavior tests**

Add `"github.com/FrancisChung/BLibOrg/internal/librarycache"` and `"time"` to the existing `import` block in `internal/librarian/librarian_test.go`, then append these tests to the end of the file:

```go
func TestScan_UsesCachedFieldsAndSkipsExtractOnCacheHit(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}

	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{
		ModTime: info.ModTime(), Size: info.Size(),
		Title: "Foundation", Author: "Isaac Asimov", Year: "1951",
		Category: "Fiction", Subcategory: "Sci-Fi", CoverPath: "/covers/abc.jpg",
	})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		t.Fatal("extractFunc should not be called for a cache hit")
		return metadata.Result{}, nil
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("len(books) = %d, want 1", len(books))
	}
	if books[0].Title != "Foundation" || books[0].Author != "Isaac Asimov" || books[0].Year != "1951" || books[0].CoverPath != "/covers/abc.jpg" {
		t.Errorf("books[0] = %+v, want cached fields", books[0])
	}
}

func TestScan_CachedCoverOverriddenIsServedWithoutRecheckingOverrideStore(t *testing.T) {
	// Proves the invalidation contract is what keeps overrides correct --
	// not an accidental re-check on every Scan. No entry exists in
	// cover-overrides.json at all; the cached CoverOverridden=true and its
	// CoverPath must still come straight from the cache entry.
	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, "Foundation.epub")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}

	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{
		ModTime: info.ModTime(), Size: info.Size(),
		Title: "Foundation", CoverPath: "/covers/override-xyz.jpg", CoverOverridden: true,
	})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("len(books) = %d, want 1", len(books))
	}
	if !books[0].CoverOverridden {
		t.Error("CoverOverridden = false, want true (from cache, no override store entry exists)")
	}
	if books[0].CoverPath != "/covers/override-xyz.jpg" {
		t.Errorf("CoverPath = %q, want the cached override path", books[0].CoverPath)
	}
}

func TestScan_ExtractsAndCachesANewFile(t *testing.T) {
	libDir := t.TempDir()
	logDir := t.TempDir()
	// Must be a real, extractable epub (not writeFixtureFile's placeholder
	// text): metadata.Extract has to succeed for cache.Put to run and mark
	// the cache dirty, otherwise Save is correctly a no-op per librarycache's
	// documented "only write if there are unsaved changes" contract, and
	// this test's library-cache.json assertion would never observe a write.
	writeEpubWithCover(t, filepath.Join(libDir, "Fiction", "Sci-Fi", "Foundation.epub"), []byte{0xFF, 0xD8, 0xFF, 0xE0})

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	if _, err := Scan(cfg, false); err != nil {
		t.Fatalf("first Scan returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(logDir, "library-cache.json")); err != nil {
		t.Errorf("expected library-cache.json to be written, got: %v", err)
	}
}

func TestScan_ReExtractsAnEditedFile(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"))
	info, _ := os.Stat(path)

	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{ModTime: info.ModTime().Add(-time.Hour), Size: info.Size(), Title: "Stale Title"})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	called := false
	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		called = true
		return metadata.Result{Title: "Fresh Title"}, nil
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !called {
		t.Error("extractFunc was not called for a stale cache entry")
	}
	if len(books) != 1 || books[0].Title != "Fresh Title" {
		t.Errorf("books = %+v, want [{Title: Fresh Title}]", books)
	}
}

func TestScan_DropsRemovedFileFromCache(t *testing.T) {
	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, "Loose.epub")
	info, _ := os.Stat(path)

	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{ModTime: info.ModTime(), Size: info.Size(), Title: "Loose"})
	cache.Put(filepath.Join(libDir, "Gone.epub"), librarycache.Entry{Title: "Gone"})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	if _, err := Scan(cfg, false); err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	reloaded := librarycache.Load(logDir)
	if _, ok := reloaded.Fresh(filepath.Join(libDir, "Gone.epub"), time.Time{}, 0); ok {
		t.Error("removed file's cache entry was not dropped")
	}
}

func TestScan_ForceRefreshBypassesFreshCache(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"))
	info, _ := os.Stat(path)

	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{ModTime: info.ModTime(), Size: info.Size(), Title: "Cached Title"})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	called := false
	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		called = true
		return metadata.Result{Title: "Refreshed Title"}, nil
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, true)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !called {
		t.Error("extractFunc was not called despite forceRefresh=true")
	}
	if len(books) != 1 || books[0].Title != "Refreshed Title" {
		t.Errorf("books = %+v, want [{Title: Refreshed Title}]", books)
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/librarian/... -v`
Expected: all tests PASS, including every pre-existing override test unchanged in behavior.

- [ ] **Step 5: Build and vet**

Run: `go build ./... && go vet ./internal/librarian/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/librarian/librarian.go internal/librarian/librarian_test.go
git commit -m "Make librarian.Scan cache-aware with a forceRefresh escape hatch"
```

---

### Task 3: Cover-override invalidation

**Files:**
- Modify: `internal/appapi/cover_override.go`
- Modify: `internal/appapi/cover_override_test.go`

**Interfaces:**
- Consumes: `librarycache.Invalidate(logFolder, sourcePath string) error` (Task 1).
- Produces: no new exported names; `SetCoverOverride`, `SetCoverOverrideCustom`, `ClearCoverOverride` keep their existing signatures but now also return an error if invalidation fails.

- [ ] **Step 1: Add the invalidation calls to `internal/appapi/cover_override.go`**

Add `"github.com/FrancisChung/BLibOrg/internal/librarycache"` to the import block. Then update the three functions:

```go
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
	if err := librarycache.Invalidate(cfg.General.LogFolder, bookPath); err != nil {
		return "", err
	}
	return covercache.Force(cfg.General.LogFolder, bookPath, data, contentType)
}
```

```go
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
	if err := librarycache.Invalidate(cfg.General.LogFolder, bookPath); err != nil {
		return "", err
	}
	return url, nil
}
```

```go
func (a *App) ClearCoverOverride(bookPath string) (string, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return "", err
	}
	if err := covercache.ClearOverride(cfg.General.LogFolder, bookPath); err != nil {
		return "", err
	}
	if err := librarycache.Invalidate(cfg.General.LogFolder, bookPath); err != nil {
		return "", err
	}
	res, err := metadata.Extract(bookPath, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit)
	if err != nil || len(res.CoverBytes) == 0 {
		return "", err
	}
	return covercache.Force(cfg.General.LogFolder, bookPath, res.CoverBytes, res.CoverContentType)
}
```

`SetCoverOverrideCustomFromFile` needs no change — it already delegates to `SetCoverOverrideCustom`, which now invalidates internally.

- [ ] **Step 2: Write the failing tests first, in `internal/appapi/cover_override_test.go`**

Add `"github.com/FrancisChung/BLibOrg/internal/librarycache"` to the import block, then append:

```go
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
```

- [ ] **Step 3: Run the tests to confirm the new ones pass and nothing regressed**

Run: `go test ./internal/appapi/... -v`
Expected: all tests PASS, including the pre-existing `TestSetCoverOverride_PersistsAndReturnsCoverURL`, `TestSetCoverOverrideCustom_PersistsAndReturnsCoverURL`, `TestClearCoverOverride_RemovesOverrideAndReturnsAutoDetectedURL`, etc.

- [ ] **Step 4: Build and vet**

Run: `go build ./... && go vet ./internal/appapi/...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add internal/appapi/cover_override.go internal/appapi/cover_override_test.go
git commit -m "Invalidate the library scan cache entry on every cover-override change"
```

---

### Task 4: Thread `forceRefresh` through `appapi.ListLibrary` and `desktop/app.go`

**Files:**
- Modify: `internal/appapi/library.go`
- Modify: `internal/appapi/library_test.go`
- Modify: `desktop/app.go`

**Interfaces:**
- Consumes: `librarian.Scan(cfg config.Config, forceRefresh bool) ([]Book, error)` (Task 2).
- Produces: `func (a *App) ListLibrary(forceRefresh bool) (LibraryView, error)` on both `appapi.App` and the Wails-bound `main.App` — consumed by Task 5's frontend changes.

- [ ] **Step 1: Update `internal/appapi/library.go`**

Only `ListLibrary`'s signature and its one call to `librarian.Scan` change; `LibraryBookView`, `LibraryView`, and `libraryBookToView` are untouched:

```go
// ListLibrary walks the configured library folder and returns every
// already-organized book found there, for the frontend's Library/Bookshelf
// view. It never touches the filesystem beyond reading -- no moves, no
// categorization, no destination-path computation (those are Scan/Apply's
// job for the *working* folder; this reads back what's already organized).
// forceRefresh bypasses librarian's scan cache for this call, re-extracting
// every book and repopulating the cache with fresh values -- the frontend's
// manual "Refresh" action.
func (a *App) ListLibrary(forceRefresh bool) (LibraryView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return LibraryView{}, err
	}

	books, err := librarian.Scan(cfg, forceRefresh)
	if err != nil {
		return LibraryView{}, err
	}

	views := make([]LibraryBookView, 0, len(books))
	categorySet := map[string]bool{}
	for _, b := range books {
		views = append(views, libraryBookToView(b))
		if b.Category != "" {
			categorySet[b.Category] = true
		}
	}
	categories := make([]string, 0, len(categorySet))
	for c := range categorySet {
		categories = append(categories, c)
	}
	sort.Strings(categories)

	return LibraryView{Books: views, Categories: categories}, nil
}
```

- [ ] **Step 2: Update call sites in `internal/appapi/library_test.go`**

Both `app.ListLibrary()` calls (`TestListLibrary_ReturnsBooksGroupedByCategory`, `TestListLibrary_EmptyLibraryReturnsEmptyView`) become `app.ListLibrary(false)`.

- [ ] **Step 3: Update `desktop/app.go`**

```go
func (a *App) ListLibrary(forceRefresh bool) (appapi.LibraryView, error) {
	return a.api.ListLibrary(forceRefresh)
}
```

- [ ] **Step 4: Regenerate Wails bindings**

Run: `cd desktop && wails generate module`
Expected: exits cleanly; `desktop/frontend/wailsjs/go/main/App.d.ts` and `App.js` now show `ListLibrary(forceRefresh)` instead of `ListLibrary()`. Do not hand-edit these files or `models.ts` — the command regenerates all three from the current Go source.

- [ ] **Step 5: Run the Go tests and build**

Run: `go build ./... && go vet ./... && go test ./internal/appapi/... ./desktop/... -v`
Expected: all PASS, clean vet.

- [ ] **Step 6: Commit**

```bash
git add internal/appapi/library.go internal/appapi/library_test.go desktop/app.go \
        desktop/frontend/wailsjs/go/main/App.d.ts desktop/frontend/wailsjs/go/main/App.js \
        desktop/frontend/wailsjs/go/models.ts
git commit -m "Thread forceRefresh through appapi.ListLibrary and the Wails binding"
```

---

### Task 5: Frontend `forceRefresh` + Refresh button

**Files:**
- Modify: `desktop/frontend/src/lib/LibraryView.svelte`
- Modify: `desktop/frontend/src/lib/LibraryView.test.ts`

**Interfaces:**
- Consumes: `ListLibrary(forceRefresh: boolean)` from `../../wailsjs/go/main/App` (Task 4).

- [ ] **Step 1: Write the two new failing tests in `LibraryView.test.ts`**

Append to the `describe('LibraryView', ...)` block (existing tests and `makeBook()` in this file already include `coverOverridden: false` — no change needed there):

```js
  it('calls ListLibrary(false) on initial mount', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({ books: [], categories: [] });
    render(LibraryView, { category: '' });
    await screen.findByText('No books found in the library folder yet.');

    expect(ListLibrary).toHaveBeenCalledWith(false);
  });

  it('calls ListLibrary(true) when the Refresh button is clicked', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({ books: [], categories: [] });
    render(LibraryView, { category: '' });
    await screen.findByText('No books found in the library folder yet.');

    await fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));

    expect(ListLibrary).toHaveBeenLastCalledWith(true);
  });
```

- [ ] **Step 2: Run the tests to confirm they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/LibraryView.test.ts`
Expected: FAIL — `ListLibrary` is currently called with no arguments and there is no "Refresh" button yet.

- [ ] **Step 3: Update `LibraryView.svelte`'s script block**

```svelte
  async function load(force: boolean = false) {
    loading = true;
    loadError = '';
    try {
      const view = await ListLibrary(force);
      books = view.books ?? [];
      dispatch('categoriesLoaded', view.categories ?? []);
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
      books = [];
    } finally {
      loading = false;
    }
  }
```

- [ ] **Step 4: Add the Refresh button to the topbar markup**

```svelte
  <div class="topbar">
    <h2>{category ? `Library — ${category}` : 'Library — All categories'}</h2>
    <div class="topbar-controls">
      <button type="button" class="refresh" on:click={() => load(true)} disabled={loading}>Refresh</button>
      <div class="sort-toggle" role="group" aria-label="Sort by">
        <button type="button" class:active={sortMode === 'title'} on:click={() => (sortMode = 'title')}>Title</button>
        <button type="button" class:active={sortMode === 'author'} on:click={() => (sortMode = 'author')}>Author</button>
        <button type="button" class:active={sortMode === 'year'} on:click={() => (sortMode = 'year')}>Year</button>
      </div>
    </div>
  </div>
```

- [ ] **Step 5: Add the new styles**

```css
  .topbar-controls {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .refresh {
    border: 1px solid var(--bf-border);
    background: var(--bf-surface);
    color: var(--bf-text);
    border-radius: 6px;
    padding: 6px 12px;
    font-size: 12.5px;
    font-family: inherit;
    cursor: pointer;
  }
  .refresh:disabled {
    opacity: 0.5;
    cursor: default;
  }
```

- [ ] **Step 6: Run the tests**

Run: `cd desktop/frontend && npx vitest run src/lib/LibraryView.test.ts`
Expected: all PASS.

- [ ] **Step 7: Run the full frontend suite**

Run: `cd desktop/frontend && npx vitest run`
Expected: all files PASS — this file's other pre-existing tests (grouping, filtering, sorting, error banner, empty state) must be unaffected.

- [ ] **Step 8: Commit**

```bash
git add desktop/frontend/src/lib/LibraryView.svelte desktop/frontend/src/lib/LibraryView.test.ts
git commit -m "Add a manual Refresh action to bypass the library scan cache"
```

---

### Task 6: `ShelfRow` scroll navigation

**Files:**
- Create: `desktop/frontend/src/lib/ShelfRow.svelte`
- Create: `desktop/frontend/src/lib/ShelfRow.test.ts`
- Modify: `desktop/frontend/src/lib/LibraryView.svelte`

**Interfaces:**
- Consumes: `LibraryBookCard.svelte` (existing, unchanged — already includes Plan C's hover cover-override button), `LibraryBookView` type (`./types`).
- Produces: `ShelfRow` Svelte component with props `subcategory: string`, `books: LibraryBookView[]`.

- [ ] **Step 1: Write `desktop/frontend/src/lib/ShelfRow.test.ts`**

```ts
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import ShelfRow from './ShelfRow.svelte';
import type { LibraryBookView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  OpenFile: vi.fn(),
}));

function makeBook(overrides: Partial<LibraryBookView> = {}): LibraryBookView {
  return {
    sourcePath: '/library/Fiction/Sci-Fi/book.epub',
    format: 'epub',
    title: 'Foundation',
    author: 'Isaac Asimov',
    year: '1951',
    category: 'Fiction',
    subcategory: 'Sci-Fi',
    coverPath: '',
    coverOverridden: false,
    ...overrides,
  };
}

function setRowMetrics(
  row: HTMLElement,
  { scrollLeft, clientWidth, scrollWidth }: { scrollLeft: number; clientWidth: number; scrollWidth: number },
) {
  Object.defineProperty(row, 'scrollLeft', { value: scrollLeft, configurable: true });
  Object.defineProperty(row, 'clientWidth', { value: clientWidth, configurable: true });
  Object.defineProperty(row, 'scrollWidth', { value: scrollWidth, configurable: true });
}

describe('ShelfRow', () => {
  it('renders the subcategory heading and one card per book', () => {
    render(ShelfRow, {
      subcategory: 'Sci-Fi',
      books: [makeBook({ sourcePath: '/a' }), makeBook({ sourcePath: '/b', title: 'Mistborn' })],
    });
    expect(screen.getByText('Sci-Fi')).toBeInTheDocument();
    expect(screen.getByText('Foundation')).toBeInTheDocument();
    expect(screen.getByText('Mistborn')).toBeInTheDocument();
  });

  it('scrolls the row right when the next button is clicked', async () => {
    render(ShelfRow, { subcategory: 'Sci-Fi', books: [makeBook()] });
    const row = document.querySelector('.shelf-row') as HTMLElement;
    setRowMetrics(row, { scrollLeft: 0, clientWidth: 200, scrollWidth: 800 });
    row.scrollBy = vi.fn();
    await fireEvent.scroll(row);

    await fireEvent.click(screen.getByRole('button', { name: 'Scroll shelf right' }));

    expect(row.scrollBy).toHaveBeenCalledWith({ left: 180, behavior: 'smooth' });
  });

  it('scrolls the row left when the previous button is clicked', async () => {
    render(ShelfRow, { subcategory: 'Sci-Fi', books: [makeBook()] });
    const row = document.querySelector('.shelf-row') as HTMLElement;
    setRowMetrics(row, { scrollLeft: 400, clientWidth: 200, scrollWidth: 800 });
    row.scrollBy = vi.fn();
    await fireEvent.scroll(row);

    await fireEvent.click(screen.getByRole('button', { name: 'Scroll shelf left' }));

    expect(row.scrollBy).toHaveBeenCalledWith({ left: -180, behavior: 'smooth' });
  });

  it('disables the previous button when scrolled to the start', async () => {
    render(ShelfRow, { subcategory: 'Sci-Fi', books: [makeBook()] });
    const row = document.querySelector('.shelf-row') as HTMLElement;
    setRowMetrics(row, { scrollLeft: 0, clientWidth: 200, scrollWidth: 800 });
    await fireEvent.scroll(row);

    expect(screen.getByRole('button', { name: 'Scroll shelf left' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Scroll shelf right' })).not.toBeDisabled();
  });

  it('disables the next button when scrolled to the end', async () => {
    render(ShelfRow, { subcategory: 'Sci-Fi', books: [makeBook()] });
    const row = document.querySelector('.shelf-row') as HTMLElement;
    setRowMetrics(row, { scrollLeft: 600, clientWidth: 200, scrollWidth: 800 });
    await fireEvent.scroll(row);

    expect(screen.getByRole('button', { name: 'Scroll shelf right' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Scroll shelf left' })).not.toBeDisabled();
  });
});
```

- [ ] **Step 2: Run the test to confirm it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/ShelfRow.test.ts`
Expected: FAIL — `ShelfRow.svelte` does not exist yet.

- [ ] **Step 3: Write `desktop/frontend/src/lib/ShelfRow.svelte`**

```svelte
<script lang="ts">
  import { onMount } from 'svelte';
  import LibraryBookCard from './LibraryBookCard.svelte';
  import type { LibraryBookView } from './types';

  export let subcategory: string;
  export let books: LibraryBookView[];

  let rowEl: HTMLDivElement;
  let atStart = true;
  let atEnd = true;

  function updateEdges() {
    if (!rowEl) return;
    atStart = rowEl.scrollLeft <= 0;
    atEnd = rowEl.scrollLeft + rowEl.clientWidth >= rowEl.scrollWidth;
  }

  function scrollByPage(direction: 1 | -1) {
    if (!rowEl) return;
    rowEl.scrollBy({ left: direction * rowEl.clientWidth * 0.9, behavior: 'smooth' });
  }

  onMount(() => {
    updateEdges();
    window.addEventListener('resize', updateEdges);
    return () => window.removeEventListener('resize', updateEdges);
  });
</script>

<div class="shelf-section">
  <div class="shelf-heading">{subcategory}</div>
  <div class="shelf-wrap">
    <button
      type="button"
      class="shelf-nav prev"
      aria-label="Scroll shelf left"
      disabled={atStart}
      on:click={() => scrollByPage(-1)}
    >
      ‹
    </button>
    <div class="shelf-row" bind:this={rowEl} on:scroll={updateEdges}>
      {#each books as book (book.sourcePath)}
        <LibraryBookCard {book} />
      {/each}
    </div>
    <button
      type="button"
      class="shelf-nav next"
      aria-label="Scroll shelf right"
      disabled={atEnd}
      on:click={() => scrollByPage(1)}
    >
      ›
    </button>
  </div>
</div>

<style>
  .shelf-section {
    margin-bottom: 4px;
  }
  .shelf-heading {
    font-size: 13px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--bf-text-muted);
    margin-bottom: 8px;
  }
  .shelf-wrap {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .shelf-row {
    display: flex;
    gap: 12px;
    padding-bottom: 14px;
    border-bottom: 8px solid var(--bf-border);
    overflow-x: auto;
    flex: 1;
    min-width: 0;
  }
  .shelf-nav {
    flex-shrink: 0;
    border: 1px solid var(--bf-border);
    background: var(--bf-surface);
    color: var(--bf-text);
    border-radius: 6px;
    width: 28px;
    height: 28px;
    font-size: 16px;
    line-height: 1;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .shelf-nav:disabled {
    opacity: 0.35;
    cursor: default;
  }
</style>
```

- [ ] **Step 4: Run the test again**

Run: `cd desktop/frontend && npx vitest run src/lib/ShelfRow.test.ts`
Expected: all PASS.

- [ ] **Step 5: Wire `ShelfRow` into `LibraryView.svelte`**

Add the import:

```svelte
  import ShelfRow from './ShelfRow.svelte';
```

Replace the inline shelf markup:

```svelte
    {#each shelves as shelf (shelf.subcategory)}
      <div class="shelf-section">
        <div class="shelf-heading">{shelf.subcategory}</div>
        <div class="shelf-row">
          {#each shelf.books as book (book.sourcePath)}
            <LibraryBookCard {book} />
          {/each}
        </div>
      </div>
    {/each}
```

with:

```svelte
    {#each shelves as shelf (shelf.subcategory)}
      <ShelfRow subcategory={shelf.subcategory} books={shelf.books} />
    {/each}
```

Remove the now-unused `import LibraryBookCard from './LibraryBookCard.svelte';` line and the now-unused `.shelf-heading` / `.shelf-row` rules from `LibraryView.svelte`'s own `<style>` block (they live in `ShelfRow.svelte` now).

- [ ] **Step 6: Run the full frontend suite**

Run: `cd desktop/frontend && npx vitest run`
Expected: all files PASS, including `LibraryView.test.ts`'s pre-existing `'re-sorts shelves when a sort button is clicked'` test, which queries `document.querySelector('.shelf-row')` directly — this must still resolve correctly now that the row lives inside `ShelfRow.svelte`, since Svelte renders it into the same DOM tree `LibraryView` mounts.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/lib/ShelfRow.svelte desktop/frontend/src/lib/ShelfRow.test.ts \
        desktop/frontend/src/lib/LibraryView.svelte
git commit -m "Add ShelfRow: prev/next scroll buttons for bookshelf rows"
```

---

### Task 7: Real-library before/after verification

No display is available in this environment for a full GUI click-through, so — matching the approach Plan B's Task B7 and Plan C's Task C10 used — this substitutes an automated benchmark driving the real backend logic directly against a real copy of the library, comparing a cold scan (empty cache) against a warm scan (fully cached) on the same data, which is the concrete claim this whole plan makes.

**Files:**
- Create (temporary, deleted at the end of this task, not part of the committed test suite): `internal/librarian/manual_verify_benchmark_test.go`

- [ ] **Step 1: Write the benchmark test**

```go
package librarian

import (
	"os"
	"testing"
	"time"

	"github.com/FrancisChung/BLibOrg/internal/config"
)

// Manual verification for the library-scan-cache plan — not part of the
// committed test suite, deleted after use. Points LibraryFolder directly at
// a real, read-only copy of the user's library (Scan never writes to
// LibraryFolder, only reads) and a scratch LogFolder, to give concrete
// before/after numbers for the cache's actual effect at realistic scale.
func TestManualVerify_ColdVsWarmScanOnRealLibrary(t *testing.T) {
	const realLibrary = "/media/francis/Data1/Books/Library/Technology"
	if _, err := os.Stat(realLibrary); err != nil {
		t.Skip("real library not present in this environment")
	}

	logDir := t.TempDir()
	cfg := config.Config{General: config.General{LibraryFolder: realLibrary, LogFolder: logDir, PDFCoverPageLimit: 10}}

	coldStart := time.Now()
	coldBooks, err := Scan(cfg, false)
	coldElapsed := time.Since(coldStart)
	if err != nil {
		t.Fatalf("cold Scan returned error: %v", err)
	}
	t.Logf("cold scan: %d books in %s", len(coldBooks), coldElapsed)

	warmStart := time.Now()
	warmBooks, err := Scan(cfg, false)
	warmElapsed := time.Since(warmStart)
	if err != nil {
		t.Fatalf("warm Scan returned error: %v", err)
	}
	t.Logf("warm scan: %d books in %s", len(warmBooks), warmElapsed)

	if len(warmBooks) != len(coldBooks) {
		t.Errorf("warm scan returned %d books, cold scan returned %d — cache must not change the result set", len(warmBooks), len(coldBooks))
	}
	if warmElapsed*5 > coldElapsed {
		t.Errorf("warm scan (%s) is not at least 5x faster than cold scan (%s) — cache does not appear to be working", warmElapsed, coldElapsed)
	}
}
```

- [ ] **Step 2: Run it and record the numbers**

Run: `go test ./internal/librarian/... -run TestManualVerify_ColdVsWarmScanOnRealLibrary -v`
Expected: PASS, with `t.Logf` output showing the cold-scan and warm-scan durations and book counts. Record both durations — this is the concrete evidence the cache fix actually works, to report back.

- [ ] **Step 3: Delete the benchmark file**

```bash
rm internal/librarian/manual_verify_benchmark_test.go
```

Not committed — it depends on a path (`/media/francis/Data1/Books/Library/Technology`) that only exists on this machine, so it isn't portable test suite material, matching the precedent set by Plan B/C's own manual-verification tests.

- [ ] **Step 4: Full regression pass**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all PASS, clean vet.

Run: `cd desktop/frontend && npx vitest run`
Expected: all PASS.

- [ ] **Step 5: No commit for this task**

The benchmark file was deleted in Step 3, so there is nothing to commit. If `git status` shows anything unexpected at this point, stop and investigate before proceeding — this task should leave the working tree exactly as Task 6 left it.

---

## Final step: whole-branch review and merge

After Task 7, follow this repo's established pattern (see `.superpowers/sdd/progress.md` from Plan B/Plan C for the precedent) for finishing a multi-task SDD branch:

1. Run a final whole-branch code review across the full diff from this branch's merge-base with `main` to `HEAD`.
2. Fix any Important/Critical findings (Minor findings may be accepted as-is with a rationale, matching this repo's established convention).
3. Merge to `main` (fast-forward or `--no-ff`, matching whichever the repo's git history shows was used for the immediately preceding merge).
4. Report back the Task 7 benchmark numbers — that's the direct answer to "why is Library slow" and the proof it's fixed.
