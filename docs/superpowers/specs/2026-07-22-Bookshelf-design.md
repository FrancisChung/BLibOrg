# Design: Bookshelf functionality

## Background

We have built an Organiser that has rearranged our Books Library into neatly arranged and renamed, worthy of a proper library.
Why not show case this newly organised Library like a real Library Bookshelf.

## Inspiration

UX/UI inspiration: https://www.bookfusion.com/, and https://www.linkedin.com/pulse/i-migrated-all-my-ebooks-bookfusion-heres-why-katie-paxton-fear-wh0ne/

## Goal

Let the user browse the already-organized library (`library_folder` in config.yaml) the way they'd browse a physical library: one bookshelf per subcategory, book covers on display, click to open.

## Architecture

New backend package `internal/librarian`, mirroring `internal/pipeline`'s role but for the *organized* side of the library rather than the incoming working folder:

- `Scan(cfg config.Config) ([]LibraryBook, error)` — walks `library_folder/<Category>/<Subcategory>/*` two levels deep (exactly the layout `rename.BuildPath` already produces). For each file, calls the existing `metadata.Extract` for Title/Author/Year, and reads Category/Subcategory directly from the folder path rather than re-categorizing. Nothing is persisted — this re-derives on every call, the same convention established for ISBN lookup (see `2026-07-20-isbn-search-design.md`).
- `LibraryBook{SourcePath, Format, Title, Author, Year, Category, Subcategory, CoverPath string}`.

**Cover extraction** — new capability added to each format extractor (does not exist anywhere today):
- EPUB: resolve the manifest's `cover-image` property / `<meta name="cover">` item, decode the referenced image from the zip.
- MOBI/AZW3: read the EXTH cover-image-index record, decode the embedded image resource.
- PDF: best-effort extraction of the first embedded XObject image on page 1. PDFs have no standard cover convention, so this is heuristic, not guaranteed, unlike EPUB/MOBI.
- A book with no extractable cover gets an empty `CoverPath`; the frontend shows a placeholder tile.

**Cover caching:** extracted covers are written once to `cfg.General.LogFolder/covers/<hash of SourcePath>.<ext>` (reusing the existing app-data-folder convention already established by `ops.jsonl` etc.). A re-scan skips re-extraction when the cached file exists and is newer than the source file's mtime.

**Serving covers to the frontend:** Wails 2.13 blocks `file://` URLs in the webview (already noted in the existing `desktop/app.go` `OpenFile` comment), so covers aren't served as raw file paths. Instead, `desktop/main.go`'s `assetserver.Options` gets a custom `Handler` serving `/covers/<hash>.<ext>` from the cache folder. `LibraryBook.CoverPath` becomes a same-origin relative URL like `/covers/ab12cd.jpg` that an `<img src>` loads directly — no new IPC round-trip needed.

**appapi surface:** `appapi.App.ListLibrary() (LibraryView, error)` → `LibraryView{Categories []string, Books []LibraryBookView}`, following the existing `Categories()`/`Scan()` naming pattern, thinly wrapped in `desktop/app.go` like the other bound methods.

## Frontend

**Navigation:**
- `Sidebar.svelte`'s array-driven `topLevelItems` gains a `{view: 'library', label: 'Library'}` entry, prepended above Scan & Review (the sidebar already anticipated a future top-level item like this — a one-line addition).
- `SidebarView` type gains `'library'`.
- A submenu (same visual pattern as the existing log items) lists "All" + each major category from config. Selecting a category **filters** the Library view to that category's subcategory shelves only; "All" shows every shelf across every category.

**Main view — new `LibraryView.svelte`:**
- On mount and on category-filter change, calls `ListLibrary()`, groups the returned books by `Subcategory` into one bookshelf section per group (filtered to the active category, or all).
- One **global** sort control — Title / Author / Year — at the top of the view, applied client-side across every shelf at once; re-sorting is a re-render, not a re-fetch.
- Each bookshelf renders as a horizontal row of book tiles, ordered left-to-right per the active sort. Overflow within a shelf is native horizontal scroll (`overflow-x: auto` — trackpad/shift-scroll/scrollbar), matching the mockup. No `<`/`>` pagination controls in v1; revisit after the shelf gets real usage if native scroll proves hard to discover.

**Book tile — new `LibraryBookCard.svelte`** (a new component, not a reuse of the existing `BookCard.svelte`, which is built around the Scan & Review row/status-pill model — this one is cover-forward):
- Shows the cover image (`<img src={book.coverPath}>`), falling back to a placeholder graphic when `coverPath` is empty.
- Hovering reveals the filename minus its extension.
- Clicking calls the existing `OpenFile(book.sourcePath)` — same pattern and error-banner handling as `BookCard.svelte`'s `openOriginal()`.

**Data flow:** `ListLibrary()` (one backend call) → group by subcategory client-side → sort client-side → render shelves. No pagination/virtualization in v1 (mirrors `Scan()`, which already returns its whole batch at once).

## Testing

**Backend (Go, colocated `_test.go`):**
- `internal/librarian`: fixture-based walk of a fake `library_folder/Category/Subcategory/` tree — correct grouping and metadata extraction.
- Cover extraction: one test per format (EPUB fixture with a manifest cover-image, MOBI fixture with an EXTH cover record, PDF fixture with an embedded XObject image), plus a "no cover found" case per format.
- Cover cache: second extraction of an unchanged file is a cache hit (no re-decode); a modified source file invalidates the cache.
- `appapi.ListLibrary`: table-style test, same shape as the existing `scan_test.go`.

**Frontend (Vitest, colocated `.test.ts`, Wails bindings mocked — same convention as `BookCard.test.ts`):**
- `LibraryView.svelte`: shelf grouping, client-side sort re-ordering, category filter narrowing.
- `LibraryBookCard.svelte`: cover vs. placeholder rendering, hover-reveals-filename, click calls `OpenFile` with `sourcePath`.
- `Sidebar.svelte`: new Library item + submenu wiring.

## Non-goals (out of scope for this pass)

- Prev/Next arrow buttons or pagination for shelf overflow — start with native scroll, revisit based on real usage.
- Pagination/virtualization for very large libraries.
- Persisting sort/filter choice across app restarts.
- Any cover art beyond best-effort EPUB/MOBI/PDF extraction — no manual cover upload, no fetching cover art from an external API.
