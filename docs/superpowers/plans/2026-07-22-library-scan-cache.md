# Library Scan Cache + Shelf Scroll Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Library view load near-instantly on an unchanged library by caching scan results (invalidated per-file by ModTime+Size), with a manual Refresh escape hatch; and add prev/next scroll buttons to each bookshelf row since native horizontal scroll isn't discoverable with a plain mouse.

**Architecture:** A new `internal/librarycache` package persists a path-keyed JSON cache under `LogFolder/library-cache.json`. `internal/librarian.Scan` gains a `forceRefresh bool` parameter and consults the cache per file before calling the expensive `metadata.Extract`/`covercache.Ensure` pair, skipping both entirely on a fresh cache hit. This threads through `appapi.ListLibrary` and the Wails-bound `desktop/app.go` wrapper as a new required boolean parameter. On the frontend, `LibraryView.svelte` gets a Refresh button, and a new `ShelfRow.svelte` component (extracted from `LibraryView.svelte`'s inline shelf markup) adds ‹/› scroll buttons per shelf.

**Tech Stack:** Go 1.25, standard library only (`encoding/json`, `os`, `path/filepath`, `time` — no new Go dependencies). Svelte + TypeScript + Vitest, no new frontend dependencies.

## Global Constraints

- No new third-party Go or npm dependencies.
- Cache staleness check is file ModTime + Size (not a content hash) — cheap, matches `covercache`'s existing mtime-based freshness check.
- A missing or corrupt cache file is treated as an empty cache, never an error — the cache is purely an optimization; losing it just means the next `Scan` re-extracts everything, matching today's (pre-cache) behavior.
- Cache persistence failures (`Save` errors) must not fail `Scan` or drop results — matches this codebase's existing best-effort convention for `covercache.Ensure` errors.
- Files removed from the library folder since the last scan must be dropped from the saved cache (not linger forever).
- `forceRefresh=true` bypasses the cache for every file in that call and repopulates the cache with fresh values — it is a one-shot bypass, not a mode that disables caching going forward.
- `internal/librarian.Scan`, `appapi.ListLibrary`, and the Wails-bound `desktop/app.go` `ListLibrary` all change signature to take the new boolean parameter — this is a breaking change to existing methods, acceptable since there is no external consumer beyond this app's own frontend.
- Shelf scroll buttons use native `scrollBy` on the existing `overflow-x: auto` row — no new scroll-position state library, no virtualization, no page-number-based pagination (that was explicitly rejected in favor of arrow buttons during design).

---

## Task 1: `internal/librarycache` package

**Files:**
- Create: `internal/librarycache/librarycache.go`
- Test: `internal/librarycache/librarycache_test.go`

**Interfaces:**
- Produces: `librarycache.Entry{ModTime time.Time; Size int64; Title, Author, Year, Category, Subcategory, CoverPath string}`, `librarycache.Cache` (zero value valid), `func Load(logFolder string) Cache`, `(c Cache) Fresh(sourcePath string, modTime time.Time, size int64) (Entry, bool)`, `(c *Cache) Put(sourcePath string, entry Entry)`, `(c *Cache) Keep(seen map[string]bool)`, `(c Cache) Dirty() bool`, `(c *Cache) Save(logFolder string) error`. Consumed by Task 2.

- [ ] **Step 1: Write the failing tests**

```go
// internal/librarycache/librarycache_test.go
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

func TestSaveThenLoad_RoundTripsEntries(t *testing.T) {
	dir := t.TempDir()
	modTime := time.Now().Truncate(time.Second)

	var c Cache
	c.Put("/book.epub", Entry{
		ModTime: modTime, Size: 100,
		Title: "Foundation", Author: "Isaac Asimov", Year: "1951",
		Category: "Fiction", Subcategory: "Sci-Fi", CoverPath: "/covers/abc.jpg",
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
		entry.Category != "Fiction" || entry.Subcategory != "Sci-Fi" || entry.CoverPath != "/covers/abc.jpg" {
		t.Errorf("entry = %+v, want all fields round-tripped", entry)
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/librarycache/... -v`
Expected: FAIL to compile — package `librarycache` doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```go
// internal/librarycache/librarycache.go

// Package librarycache persists internal/librarian.Scan's derived
// per-book fields (Title/Author/Year/Category/Subcategory/CoverPath) keyed
// by source path, so a Scan of an unchanged library can skip the expensive
// metadata.Extract/covercache.Ensure pair entirely for files whose ModTime
// and Size haven't changed since they were last cached.
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
	ModTime     time.Time `json:"modTime"`
	Size        int64     `json:"size"`
	Title       string    `json:"title"`
	Author      string    `json:"author"`
	Year        string    `json:"year"`
	Category    string    `json:"category"`
	Subcategory string    `json:"subcategory"`
	CoverPath   string    `json:"coverPath"`
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
// everything, the same as today's behavior with no cache at all.
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/librarycache/... -v`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add internal/librarycache/librarycache.go internal/librarycache/librarycache_test.go
git commit -m "Add internal/librarycache: path-keyed persisted library scan cache"
```

---

## Task 2: `librarian.Scan` becomes cache-aware

**Files:**
- Modify: `internal/librarian/librarian.go`
- Modify: `internal/librarian/librarian_test.go`

**Interfaces:**
- Consumes: `librarycache.Load`, `(Cache).Fresh`, `(Cache).Put`, `(Cache).Keep`, `(Cache).Save`, `librarycache.Entry` (Task 1).
- Produces: `func Scan(cfg config.Config, forceRefresh bool) ([]Book, error)` (signature change — was `Scan(cfg config.Config)`), `var extractFunc = metadata.Extract` (test seam). Consumed by Task 3.

- [ ] **Step 1: Write the failing tests**

Update the four existing test functions in `internal/librarian/librarian_test.go` to call `Scan(cfg, false)` instead of `Scan(cfg)` (the call sites are in `TestScan_GroupsByCategoryAndSubcategory`, `TestScan_FileDirectlyInLibraryRootHasNoCategory`, `TestScan_EmptyLibraryReturnsEmptySlice`, `TestScan_PopulatesCoverPathAndMetadataWhenCoverExists` — four `Scan(cfg)` calls, each becomes `Scan(cfg, false)`). Everything else in those four tests (fixtures, assertions) stays unchanged.

Add `"time"` to the import block, then add these five new tests:

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

	extractFunc = func(path string, hyphenExceptions []string) (metadata.Result, error) {
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

func TestScan_ExtractsAndCachesANewFile(t *testing.T) {
	libDir := t.TempDir()
	logDir := t.TempDir()
	writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"))

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
	extractFunc = func(path string, hyphenExceptions []string) (metadata.Result, error) {
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
	extractFunc = func(path string, hyphenExceptions []string) (metadata.Result, error) {
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

Add `"github.com/FrancisChung/BLibOrg/internal/librarycache"` and `"github.com/FrancisChung/BLibOrg/internal/metadata"` (if not already imported by name in the test file — it isn't yet) to `librarian_test.go`'s import block.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/librarian/... -v`
Expected: FAIL to compile — `Scan` still takes one argument, `extractFunc` and `librarycache` don't exist in this package yet.

- [ ] **Step 3: Write the implementation**

Replace `internal/librarian/librarian.go` in full:

```go
// Package librarian walks the already-organized library folder
// (cfg.General.LibraryFolder) and reports what's in it, grouped by the
// Category/Subcategory folder structure rename.BuildPath already produces.
// Unlike internal/pipeline, it never computes a destination or moves
// anything -- it only reads back what's already there. Title/Author/Year
// and cover art come from a persisted scan cache (internal/librarycache)
// keyed by each file's ModTime and Size when possible; the expensive
// metadata.Extract/covercache.Ensure pair only runs for a file that's new,
// edited, or when forceRefresh is true.
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
	SourcePath  string
	Format      string
	Title       string
	Author      string
	Year        string
	Category    string
	Subcategory string
	CoverPath   string // "" if no cover was found; otherwise a /covers/... URL path
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
// Title/Author/Year/CoverPath are served from the persisted scan cache
// (internal/librarycache) whenever a file's current ModTime and Size match
// what's cached, skipping metadata.Extract and covercache.Ensure entirely
// for that file. A cache miss (new file, edited file) or forceRefresh=true
// runs extraction as before and updates the cache. Files no longer present
// on disk are dropped from the saved cache. A file metadata.Extract fails
// on (e.g. corrupt) still gets a Book entry with empty
// Title/Author/Year/CoverPath rather than being dropped, so it's still
// visible on its shelf; such a file is never cached as fresh, so it's
// retried on every subsequent Scan until it succeeds or is removed.
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
				books = append(books, b)
				continue
			}
		}

		if res, err := extractFunc(path, cfg.TitleFormatting.HyphenExceptions); err == nil {
			b.Title = res.Title
			b.Author = res.Author
			b.Year = res.Year

			if len(res.CoverBytes) > 0 {
				if coverURL, err := covercache.Ensure(cfg.General.LogFolder, path, info.ModTime(), res.CoverBytes, res.CoverContentType); err == nil {
					b.CoverPath = coverURL
				}
			}

			cache.Put(path, librarycache.Entry{
				ModTime:     info.ModTime(),
				Size:        info.Size(),
				Title:       b.Title,
				Author:      b.Author,
				Year:        b.Year,
				Category:    b.Category,
				Subcategory: b.Subcategory,
				CoverPath:   b.CoverPath,
			})
		}

		books = append(books, b)
	}

	cache.Keep(seen)
	_ = cache.Save(cfg.General.LogFolder) // best-effort: a save failure shouldn't fail this Scan's results

	return books, nil
}
```

(This removes the old `statModTime` helper, which was a second, redundant `os.Stat` call after `metadata.Extract` succeeded -- the single `os.Stat` at the top of the loop, needed for the cache freshness check, now also supplies `covercache.Ensure`'s `sourceModTime` argument.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/librarian/... -v`
Expected: PASS (all tests, including the four updated pre-existing ones)

- [ ] **Step 5: Commit**

```bash
git add internal/librarian/librarian.go internal/librarian/librarian_test.go
git commit -m "Make librarian.Scan cache-aware with a forceRefresh escape hatch"
```

---

## Task 3: `appapi.ListLibrary(forceRefresh)`

**Files:**
- Modify: `internal/appapi/library.go`
- Modify: `internal/appapi/library_test.go`

**Interfaces:**
- Consumes: `librarian.Scan(cfg config.Config, forceRefresh bool) ([]librarian.Book, error)` (Task 2).
- Produces: `func (a *App) ListLibrary(forceRefresh bool) (LibraryView, error)` (signature change — was `ListLibrary()`). Consumed by Task 4.

- [ ] **Step 1: Write the failing tests**

In `internal/appapi/library_test.go`, change both existing `app.ListLibrary()` call sites (in `TestListLibrary_ReturnsBooksGroupedByCategory` and `TestListLibrary_EmptyLibraryReturnsEmptyView`) to `app.ListLibrary(false)`. Then add:

```go
func TestListLibrary_ForceRefreshTrueStillReturnsResults(t *testing.T) {
	libDir := t.TempDir()
	fictionSciFi := filepath.Join(libDir, "Fiction", "Sci-Fi", "Foundation.epub")
	if err := os.MkdirAll(filepath.Dir(fictionSciFi), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fictionSciFi, []byte("not a real epub"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	configPath := writeTestConfigForLibrary(t, libDir, t.TempDir())
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	view, err := app.ListLibrary(true)
	if err != nil {
		t.Fatalf("ListLibrary(true) returned error: %v", err)
	}
	if len(view.Books) != 1 {
		t.Fatalf("len(Books) = %d, want 1", len(view.Books))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/appapi/... -run TestListLibrary -v`
Expected: FAIL to compile — `ListLibrary` still takes zero arguments.

- [ ] **Step 3: Write the implementation**

In `internal/appapi/library.go`, change the `ListLibrary` method signature and its call into `librarian.Scan`:

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

(Only the function signature and the `librarian.Scan(cfg, forceRefresh)` call change — everything else in `library.go` is unchanged.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/appapi/... -v`
Expected: PASS (all tests, including pre-existing `scan_test.go`/`apply_test.go`/etc.)

- [ ] **Step 5: Commit**

```bash
git add internal/appapi/library.go internal/appapi/library_test.go
git commit -m "Thread forceRefresh through appapi.ListLibrary"
```

---

## Task 4: Desktop wiring — `desktop/app.go` + Wails bindings

**Files:**
- Modify: `desktop/app.go`
- Modify: `desktop/frontend/wailsjs/go/main/App.d.ts`
- Modify: `desktop/frontend/wailsjs/go/main/App.js`

**Interfaces:**
- Consumes: `appapi.App.ListLibrary(forceRefresh bool) (appapi.LibraryView, error)` (Task 3).
- Produces: `(a *App) ListLibrary(forceRefresh bool) (appapi.LibraryView, error)` (Wails-bound, signature change), the updated frontend TypeScript binding `ListLibrary(arg1: boolean)`. Consumed by Task 5.

- [ ] **Step 1: Update the wrapper method**

In `desktop/app.go`, change:

```go
func (a *App) ListLibrary() (appapi.LibraryView, error) {
	return a.api.ListLibrary()
}
```

to:

```go
func (a *App) ListLibrary(forceRefresh bool) (appapi.LibraryView, error) {
	return a.api.ListLibrary(forceRefresh)
}
```

- [ ] **Step 2: Update the generated TypeScript bindings**

In `desktop/frontend/wailsjs/go/main/App.d.ts`, change:

```typescript
export function ListLibrary():Promise<appapi.LibraryView>;
```

to:

```typescript
export function ListLibrary(arg1:boolean):Promise<appapi.LibraryView>;
```

In `desktop/frontend/wailsjs/go/main/App.js`, change:

```js
export function ListLibrary() {
  return window['go']['main']['App']['ListLibrary']();
}
```

to:

```js
export function ListLibrary(arg1) {
  return window['go']['main']['App']['ListLibrary'](arg1);
}
```

(`desktop/frontend/wailsjs/go/models.ts` needs no change — `LibraryView`/`LibraryBookView`'s shape is unchanged, only the function's parameter list changed.)

- [ ] **Step 3: Verify the whole module builds**

Run: `go build ./...`
Expected: build succeeds (this task has no new Go tests of its own — `desktop/app.go`'s one-line wrappers aren't unit-tested individually, consistent with `Categories`/`Scan`/etc. — but a broken signature here would fail the build, which is the real check).

Run: `go test ./desktop/... ./internal/... -v`
Expected: PASS (confirms Task 2/3's changes plus this wiring compile and pass together)

- [ ] **Step 4: Commit**

```bash
git add desktop/app.go desktop/frontend/wailsjs/go/main/App.d.ts desktop/frontend/wailsjs/go/main/App.js
git commit -m "Wire forceRefresh through to the Wails-bound ListLibrary binding"
```

---

## Task 5: Frontend — Refresh button in `LibraryView.svelte`

**Files:**
- Modify: `desktop/frontend/src/lib/LibraryView.svelte`
- Modify: `desktop/frontend/src/lib/LibraryView.test.ts`

**Interfaces:**
- Consumes: `ListLibrary(forceRefresh: boolean): Promise<appapi.LibraryView>` (Task 4, `../../wailsjs/go/main/App`).
- Produces: a "Refresh" button in the Library view's topbar that forces a full rescan.

- [ ] **Step 1: Write the failing tests**

Add these two tests to `desktop/frontend/src/lib/LibraryView.test.ts` (leave the existing tests untouched):

```typescript
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

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/LibraryView.test.ts`
Expected: FAIL — `ListLibrary` is currently called with no arguments, and there's no button named "Refresh" yet.

- [ ] **Step 3: Write the implementation**

In `desktop/frontend/src/lib/LibraryView.svelte`, change the script block's `load` function and the topbar markup/styles. Replace the `<script>` block's `load` function:

```typescript
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

(`onMount(load)` stays exactly as it is — calling `load` with no arguments uses the new default `force = false`.)

Replace the `.topbar` markup:

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

Add these two rules to the `<style>` block (right after `.topbar`'s existing rule):

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

Everything else in `LibraryView.svelte` (the shelf-rendering block, `groupIntoShelves` usage, error/empty states) stays exactly as it is.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/LibraryView.test.ts`
Expected: PASS (all tests, including the pre-existing ones — none of their assertions depend on `ListLibrary`'s call arguments)

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/LibraryView.svelte desktop/frontend/src/lib/LibraryView.test.ts
git commit -m "Add a manual Refresh action to bypass the library scan cache"
```

---

## Task 6: Frontend — `ShelfRow.svelte` scroll navigation

**Files:**
- Create: `desktop/frontend/src/lib/ShelfRow.svelte`
- Test: `desktop/frontend/src/lib/ShelfRow.test.ts`
- Modify: `desktop/frontend/src/lib/LibraryView.svelte`
- Modify: `docs/superpowers/specs/2026-07-22-Bookshelf-design.md`

**Interfaces:**
- Consumes: `LibraryBookCard` (existing, `./LibraryBookCard.svelte`), `LibraryBookView` (existing, `./types`).
- Produces: `<ShelfRow subcategory={string} books={LibraryBookView[]} />`. Consumed by `LibraryView.svelte`.

- [ ] **Step 1: Write the failing tests**

```typescript
// desktop/frontend/src/lib/ShelfRow.test.ts
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

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/ShelfRow.test.ts`
Expected: FAIL — `./ShelfRow.svelte` doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```svelte
<!-- desktop/frontend/src/lib/ShelfRow.svelte -->
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

In `desktop/frontend/src/lib/LibraryView.svelte`:

1. Replace the import `import LibraryBookCard from './LibraryBookCard.svelte';` with `import ShelfRow from './ShelfRow.svelte';`.
2. Replace the shelf-rendering block:

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

3. Remove the now-unused `.shelf-heading` and `.shelf-row` rules from `LibraryView.svelte`'s `<style>` block (they now live in `ShelfRow.svelte`) — leave `.library`, `.topbar`, `.topbar-controls`, `.refresh`, `.sort-toggle`/`.sort-toggle button`, `.banner.error`, `.empty` in place.

**Note:** `LibraryView.test.ts`'s existing "re-sorts shelves" test does `document.querySelector('.shelf-row')` — this still finds the row correctly after this change, since Svelte renders child components directly into the DOM tree (no shadow DOM), so `ShelfRow`'s internal `.shelf-row` element is just as reachable from the top-level render as it was when it was inline in `LibraryView.svelte`. No test changes are needed in `LibraryView.test.ts` for this task.

Finally, update `docs/superpowers/specs/2026-07-22-Bookshelf-design.md`: in its Non-goals section, remove the line `- Prev/Next arrow buttons or pagination for shelf overflow — start with native scroll, revisit based on real usage.` (it's now implemented, so it no longer belongs in Non-goals).

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run`
Expected: PASS (the entire frontend suite, including `ShelfRow.test.ts`, `LibraryView.test.ts`, and every other pre-existing test file)

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/ShelfRow.svelte desktop/frontend/src/lib/ShelfRow.test.ts desktop/frontend/src/lib/LibraryView.svelte docs/superpowers/specs/2026-07-22-Bookshelf-design.md
git commit -m "Add ShelfRow: prev/next scroll buttons for bookshelf rows"
```

---

## Final verification

- [ ] **Run the full backend and frontend test suites**

Run: `go build ./... && go test ./... && (cd desktop/frontend && npx vitest run)`
Expected: Go build succeeds, all Go packages PASS, all Vitest files PASS.

- [ ] **Manual smoke test**

Run the app (`cd desktop && wails dev`), point `library_folder` at a folder with enough books that a shelf overflows its visible width, open Library, and confirm: first load populates the cache (`<log_folder>/library-cache.json` appears), a second visit to Library (navigate away and back) is visibly faster and doesn't re-decode covers, editing or adding a book's file causes it to show updated info on the next visit, clicking Refresh forces a full rescan, and the ‹/› buttons on an overflowing shelf scroll it and disable themselves at each end.

This plan covers both parts of the design spec (`2026-07-22-library-scan-cache-design.md`) end-to-end — nothing deferred beyond what that spec's own Non-goals section already excludes (no cache eviction/size limits beyond dropping stale entries, no incremental cache-file writes, no change to the working-folder scan's never-persist convention).
