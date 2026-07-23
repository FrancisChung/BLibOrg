# Design: Library scan cache (reconciled with cover overrides) + shelf scroll navigation

## Background

`internal/librarian.Scan` re-parses every single file in `library_folder` on
every call: full file reads for MOBI/PDF, zip decompression plus cover-image
bytes for EPUB, binary PDB-record scanning for MOBI covers, and PDF page-tree
walking plus (since Plan B) FlateDecode/colorspace image reconstruction for
PDF covers. `LibraryView.svelte` calls `ListLibrary()` fresh on every mount,
so every visit to the Library view redoes all of this for every book,
regardless of whether anything changed. This is the direct cause of slow
Library loads, and scales linearly with library size.

A prior session (`.claude/worktrees/library-scan-cache`, branch
`worktree-library-scan-cache`) designed and fully implemented a fix for
this — a persisted, path-keyed scan cache (`internal/librarycache`) plus a
`forceRefresh` escape hatch and a shelf-scroll-navigation UI improvement — but
it was built *before* Plan C (the manual cover-override picker, merged to
`main` as of this doc) existed. Its cache entry has no concept of a cover
override, so serving `Title/Author/Year/CoverPath` from a stale-but-present
cache entry would silently ignore an override the user set later, until that
book's file `mtime`/size happened to change or the user clicked Refresh — a
correctness regression. That branch is not reused directly (see
"Landing approach" below); this doc reconciles its design with the
now-merged override feature.

## Goal

Cache scan results so an unchanged library returns near-instantly, while
still: (a) detecting and re-extracting books that were added or edited, (b)
always reflecting the current cover-override state exactly, with no
window where a set/cleared override shows stale — without requiring a
manual refresh for correctness (only for picking up edits to a book file
itself).

## Architecture

### `internal/librarycache` (new package, sibling to `internal/covercache`)

Persists a JSON file at `LogFolder/library-cache.json`, a map keyed by
absolute `SourcePath`. Each `Entry` holds the **final, already
override-resolved** state — the same shape `librarian.Book` exposes today:

```go
type Entry struct {
    ModTime         time.Time
    Size            int64
    Title           string
    Author          string
    Year            string
    Category        string
    Subcategory     string
    CoverPath       string
    CoverOverridden bool
}
```

Storing the resolved state (not the pre-override "raw" extraction) means a
cache hit needs zero calls into `covercache` or `metadata` — it's a pure
map lookup.

API (mirrors `covercache`'s existing Load/Save style):

- `Load(logFolder string) Cache` — reads the file; missing/corrupt returns
  an empty `Cache`, not an error (fail-open, same convention as
  `covercache` and the original scan-cache design).
- `Cache.Fresh(sourcePath string, modTime time.Time, size int64) (Entry, bool)`
- `Cache.Put(sourcePath string, entry Entry)`
- `Cache.Delete(sourcePath string)` — **new**: removes one entry, marks the
  cache dirty. Used for override invalidation (below).
- `Cache.Keep(seen map[string]bool)` — drops entries for files no longer
  present in this scan (deletions/moves).
- `Cache.Save(logFolder string) error` — no-op if nothing changed.
- `Invalidate(logFolder, sourcePath string) error` — **new** package-level
  convenience: `Load` → `Delete` → `Save` in one call, for use outside
  `Scan` itself.

Staleness check stays ModTime + file size (cheap, consistent with
`covercache`'s own mtime-based freshness check) — not a content hash.

### `internal/librarian.Scan` becomes cache-aware

`Scan(cfg config.Config, forceRefresh bool) ([]Book, error)`:

1. Load the cache (fail-open).
2. Walk the library folder as today (`scanner.Scan`).
3. Per file, **if `!forceRefresh` and `Cache.Fresh` reports a hit**: copy
   `Title/Author/Year/Category/Subcategory/CoverPath/CoverOverridden`
   straight from the `Entry` onto the `Book`. Nothing else runs for this
   file — no `metadata.Extract`, no `covercache.GetOverride`, no
   `covercache.Ensure`.
4. **On a cache miss** (new/edited file, or `forceRefresh`): run exactly
   today's `main` logic, unchanged —
   `metadata.Extract` → check `covercache.GetOverride` → for an embedded
   override, `metadata.ExtractPDFPageCover`; for a custom override, use its
   stored `ImagePath` directly; otherwise `covercache.Ensure` the
   auto-detected cover — producing the same final `b.CoverPath` /
   `b.CoverOverridden` `Scan` computes on `main` today. Then `Cache.Put` an
   `Entry` built from those final, resolved fields.
5. After the walk, `Cache.Keep(seen)` drops entries for files no longer
   present, then `Cache.Save` (no-op if nothing changed).

Note one micro-optimization carried over from the old design as a
non-goal-scoped nice-to-have, *not* required for correctness: today's
`covercache.GetOverride` reloads and re-parses the entire
`cover-overrides.json` file on every call (once per cache-miss book, not
once per `Scan`). Left as-is here — it's already only paid on a cache
*miss*, which is now the rare path, so it stops being the dominant cost
this design is targeting. Not worth the added API surface unless it shows
up as a real cost later.

### Override invalidation (the correctness-critical piece)

`appapi.SetCoverOverride`, `appapi.SetCoverOverrideCustom`, and
`appapi.ClearCoverOverride` (`internal/appapi/cover_override.go`) each call
`librarycache.Invalidate(cfg.General.LogFolder, bookPath)` immediately
after their existing `covercache.SetOverride`/`ClearOverride` call
succeeds, and **return the error** if invalidation fails — unlike `Scan`'s
own best-effort `Cache.Save`, a silently-swallowed invalidation failure
here would mean the next `Scan` serves a stale cached cover indefinitely
(until that book's file `mtime`/size happens to change), which is exactly
the correctness bug this whole design exists to prevent. Surfacing it
through the picker's existing error banner (`CoverPickerModal.svelte`
already has one) is strictly better than a silent, hard-to-diagnose
staleness bug.

This makes the cache's correctness invariant explicit: **a
`librarycache.Entry`'s `CoverOverridden`/`CoverPath` are only ever trusted
between the moment they're written and the next successful call to
`SetCoverOverride`/`SetCoverOverrideCustom`/`ClearCoverOverride` for that
same path** — any of those three calls unconditionally invalidates the
entry, forcing the next `Scan` to treat that one file as a cache miss and
resolve it fresh (which itself re-checks `covercache.GetOverride`, so it
picks up the just-changed override correctly).

### `appapi.ListLibrary` signature change

`func (a *App) ListLibrary(forceRefresh bool) (LibraryView, error)` —
passes `forceRefresh` straight through to `librarian.Scan`. Breaking
signature change to an existing method; acceptable, no external consumer
beyond this app's own frontend. `desktop/app.go`'s wrapper and the three
`wailsjs` binding files (`App.d.ts`, `App.js`, `models.ts`) need
regenerating/updating to match.

### Frontend

- `LibraryView.svelte`'s `load()` takes a `force: boolean = false` param,
  passed to `ListLibrary(force)`. A "Refresh" button sits in the topbar
  next to the existing Title/Author/Year sort toggle; clicking it calls
  `load(true)`.
- **`ShelfRow.svelte`** (new component, ported from the old branch
  unchanged — confirmed conflict-free against Plan C's changes since the
  old branch never touched `LibraryBookCard.svelte` itself): extracted from
  `LibraryView.svelte`'s inline shelf markup. Takes `subcategory: string`
  and `books: LibraryBookView[]`, renders the heading + horizontally
  scrollable row of `<LibraryBookCard>` tiles (picking up Plan C's
  hover cover-override button on each card automatically, since
  `LibraryBookCard.svelte` itself is untouched by this change), plus ‹/›
  buttons that call `scrollBy({left, behavior: 'smooth'})` on the row
  (~90% of `clientWidth` per click). Each button disables itself when the
  row is already scrolled fully to that edge (tracked via a `scroll`
  listener). `LibraryView.svelte` becomes
  `{#each shelves as shelf (shelf.subcategory)}<ShelfRow subcategory={shelf.subcategory} books={shelf.books} />{/each}`.
  This supersedes the stale "no ‹/› pagination in v1" line in
  `2026-07-22-Bookshelf-design.md`'s Non-goals section.

## Testing

Key cases, `internal/librarycache`:
- Fresh entry (ModTime+Size match) → returned as-is.
- Missing/corrupt cache file → empty cache, no error.
- `Delete`/`Invalidate` remove an entry and persist that removal.

Key cases, `internal/librarian` (needs a spy seam on extraction — a
package-level `var extractFunc = metadata.Extract`, same pattern the old
branch used and the ISBN-search plan's `lookupFunc` established):
- Fresh cache entry, no override → `metadata.Extract` not called, cached
  fields (including `CoverOverridden=false`) returned as-is.
- Fresh cache entry, `CoverOverridden=true` in the cached entry → still not
  re-checked against `covercache.GetOverride`; served straight from cache
  (this is the case that would have been a bug in the old branch's design
  without invalidation — the test should assert the override state came
  from the cache, not from a fresh lookup, to prove the invalidation
  contract is what's doing the work, not an accidental re-check).
- New/edited file → extracted, override-checked, and cached with the
  resolved fields (same as today's `main` behavior, just now also cached).
- Removed file → dropped from the saved cache.
- `forceRefresh=true` → every file re-extracted regardless of freshness.

Key cases, `internal/appapi/cover_override_test.go`:
- `SetCoverOverride` on a book with an existing fresh `librarycache` entry
  → entry is gone afterward (or: the next `Scan`-equivalent call treats it
  as a miss). Same for `SetCoverOverrideCustom` and `ClearCoverOverride`.
- Invalidation failure (simulate via an unwritable `LogFolder`) surfaces as
  a returned error from `SetCoverOverride`/`ClearCoverOverride`, not
  swallowed.

`ShelfRow.svelte` tests ported unchanged from the old branch: click ›
calls `scrollBy` with positive `left`; click ‹ negative; › disabled when
`scrollLeft + clientWidth >= scrollWidth`; ‹ disabled when `scrollLeft <= 0`
(stubbed directly on the rendered element, jsdom does no real layout).

## Landing approach

The old branch/worktree (`worktree-library-scan-cache`) predates Plan B and
Plan C; a literal `git merge`/rebase would require hand-resolving conflicts
in `librarian.go`/`appapi/library.go`/generated bindings that overlap
exactly with this reconciliation anyway. Rather than fight that history:

- A **new branch off current `main`** is created for this work (fresh
  worktree, not reusing the old one) — the old worktree/branch is left
  untouched as reference, nothing discarded.
- Conflict-free pieces are ported verbatim: `internal/librarycache`'s core
  Load/Save/Fresh/Keep machinery, `ShelfRow.svelte`/`ShelfRow.test.ts`.
- Everything touching the override/cache interaction (`librarian.Scan`,
  `cover_override.go`'s invalidation calls, the new `Entry.CoverOverridden`
  field, `ListLibrary`'s `forceRefresh` threading end-to-end) is
  implemented fresh against current `main`, per this design — not
  copy-pasted from the old branch's version, which didn't have to handle
  overrides at all.

## Non-goals

- No cache eviction/size limits beyond dropping entries for files no
  longer present.
- No content-hash-based staleness check — ModTime+Size only, consistent
  with `covercache`'s own convention.
- No change to the "never persist Title/Author/Year for the *working
  folder* scan" convention (`pipeline.Run`) — this cache is scoped
  entirely to the already-organized library side (`librarian.Scan`).
- No batching/optimization of `covercache.GetOverride`'s per-call file
  reload — deferred as noted above; the cache-miss path (the only path
  that still calls it) is no longer the common case.
