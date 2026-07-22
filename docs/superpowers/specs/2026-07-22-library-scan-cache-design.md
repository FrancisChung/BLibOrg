# Design: Library scan cache + shelf scroll navigation

## Background

The Library/Bookshelf feature (`internal/librarian.Scan`) re-parses every single file in `library_folder` on every call: full file reads for MOBI/PDF, zip decompression plus cover-image bytes for EPUB, binary PDB-record scanning for MOBI covers, and regex matching over full PDF byte content for PDF covers. `LibraryView.svelte` calls `ListLibrary()` fresh on every mount, so every visit to the Library view re-does all of this for every book, regardless of whether anything changed. This scales linearly with library size and file sizes and is the direct cause of slow Library loads.

## Goal

Cache scan results so an unchanged library returns near-instantly, while still detecting and re-extracting books that were added, edited, or removed — without requiring a manual step from the user in the common case.

## Architecture

**New package `internal/librarycache`** (sibling to the existing `internal/covercache`, same architectural role — a small, focused persistence helper with no business logic of its own):

- Persists a JSON file at `LogFolder/library-cache.json`: a map keyed by absolute `SourcePath`, each entry holding `ModTime`, `Size`, `Title`, `Author`, `Year`, `Category`, `Subcategory`, `CoverPath`.
- `Load(logFolder string) (Cache, error)` — reads the file; a missing or corrupt file returns an empty `Cache`, not an error (fail-open, matching this codebase's best-effort convention elsewhere).
- `Cache.Fresh(sourcePath string, modTime time.Time, size int64) (Entry, bool)` — returns the cached entry and whether it's still valid for the given file's current ModTime+Size.
- `Cache.Put(sourcePath string, entry Entry)` — records/updates an entry.
- `Cache.Save(logFolder string) error` — writes the file back.

Staleness check is ModTime + file size, not a content hash — cheap, and consistent with the mtime-based freshness check `covercache` already uses for cover images.

**`internal/librarian.Scan` becomes cache-aware:**

1. Load the cache (fail-open on missing/corrupt).
2. Walk the library folder as today (`scanner.Scan`).
3. Per file: if the cache has a fresh entry (`Cache.Fresh` returns `true`), reuse its Title/Author/Year/Category/Subcategory/CoverPath directly — **`metadata.Extract` and `covercache.Ensure` are not called at all** for that file. This is the actual expensive work being skipped.
4. On a cache miss, staleness, or `forceRefresh`, extract as today (`metadata.Extract` + `covercache.Ensure`) and `Cache.Put` the fresh entry.
5. After the walk, any cache entries whose path wasn't seen in this scan are dropped before saving — this naturally handles deletions and moves (a moved file is "old path gone, new path appears as a miss").
6. Save the cache if anything changed (new/edited/removed entries); skip the write if the scan was a pure cache-hit pass with no changes.

`Scan` gains a `forceRefresh bool` parameter: when true, every file is treated as a miss regardless of the cache, and the cache is fully repopulated with fresh values by the end of the call — this is the manual refresh path, not a cache-bypass-forever mode.

**`appapi.ListLibrary` signature change:** `func (a *App) ListLibrary(forceRefresh bool) (LibraryView, error)` — passes `forceRefresh` straight through to `librarian.Scan`. This is a breaking signature change to an existing method; acceptable since there's no external consumer beyond this app's own frontend (no back-compat concern for a pre-1.0 desktop app). The Wails-bound `desktop/app.go` wrapper and the three hand-maintained `wailsjs` binding files (`App.d.ts`, `App.js`, `models.ts`) need updating to match.

**Frontend:** `LibraryView.svelte`'s existing `load()` function takes a `force` parameter (default `false`) and passes it to `ListLibrary(force)`. A new "Refresh" button sits next to the existing Title/Author/Year sort toggle in the topbar; clicking it calls `load(true)`.

## Testing

`internal/librarian`'s tests need a way to assert `metadata.Extract` was or wasn't actually called for a given file — a package-level function-variable seam (`var extractFunc = metadata.Extract`), the same pattern the ISBN-search plan used for `lookupFunc`, so a cache-hit test can spy on it rather than only checking the returned values look plausible.

Key cases across `internal/librarycache` and `internal/librarian`:
- Fresh cache entry (matching ModTime+Size) → `metadata.Extract` not called, cached fields returned as-is.
- New file (no cache entry) → extracted and cached.
- Edited file (ModTime or Size changed) → re-extracted and cache entry updated.
- Removed file (in cache, absent from this scan) → dropped from the saved cache, absent from results.
- `forceRefresh=true` → every file re-extracted regardless of cache freshness, cache fully repopulated.
- Missing/corrupt cache file → treated as empty, no error, first scan behaves as a cold scan (today's behavior).

## Part 2: Shelf scroll navigation

### Background

The original design deferred shelf-overflow controls to native `overflow-x: auto` scrolling, with an explicit note to revisit "after some UX testing." That testing surfaced the problem immediately: native scroll needs a trackpad swipe or shift+scrollwheel, neither of which is discoverable with a plain mouse — there's no visible affordance that a shelf even has more books off-screen.

### Design

**New component `ShelfRow.svelte`** (extracted from `LibraryView.svelte`'s current inline shelf-section markup, keeping `LibraryView.svelte` thin): takes `subcategory: string` and `books: LibraryBookView[]` as props, renders the existing heading + horizontally-scrollable row of `<LibraryBookCard>` tiles, and adds a ‹ button before the row and a › button after it.

- Clicking › calls `scrollBy({left: +amount, behavior: 'smooth'})` on the row element (bound via `bind:this`); ‹ scrolls the same amount negative. `amount` is ~90% of the row's current `clientWidth`, so one click advances roughly one screen's worth of tiles with a little overlap for context.
- Each button is disabled (dimmed, non-interactive) when the row is already scrolled fully to that edge — tracked via a `scroll` event listener on the row updating `atStart`/`atEnd` booleans, checked on mount and on every scroll/resize.
- `LibraryView.svelte` changes to `{#each shelves as shelf (shelf.subcategory)}<ShelfRow subcategory={shelf.subcategory} books={shelf.books} />{/each}` — no other changes to its own logic (grouping/sort/filter stay exactly as they are).

This supersedes the original plan's "no `<`/`>` pagination controls in v1" line in the design spec's Non-goals section (`2026-07-22-Bookshelf-design.md`) — that line is now stale and should be removed/updated once this ships.

### Testing

`ShelfRow.svelte` is tested in isolation (a fixed set of books, no `ListLibrary` mock needed): clicking › calls `scrollBy` with a positive left value on the row element; clicking ‹ calls it with a negative value; the › button is disabled when the row's simulated `scrollLeft + clientWidth >= scrollWidth`; the ‹ button is disabled when `scrollLeft <= 0`. `HTMLElement.prototype.scrollBy` and the `scrollLeft`/`clientWidth`/`scrollWidth` properties are stubbed/set directly on the rendered element in tests, since jsdom does no real layout.

## Non-goals

- No cache eviction/size limits beyond dropping entries for files no longer present — bounded naturally by the number of live files.
- No partial/incremental cache-file writes — the whole map is read/written as one JSON file, consistent with this app's existing small-scale JSON/YAML file conventions (no database).
- No change to the "never persist Title/Author/Year for the *working folder* scan" convention (`pipeline.Run`) — this cache is scoped entirely to the already-organized library side (`librarian.Scan`).
