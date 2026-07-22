# PDF page-aware cover extraction + manual override

## Problem

`internal/metadata/pdf.go`'s current `findPDFCover` scans the raw PDF byte
stream for the *first* image XObject stream matching `/Subtype /Image` +
`/Filter /DCTDecode` (JPEG), in whatever order those objects happen to
appear in the file. This has three real gaps, confirmed against the
192-PDF library at `/media/francis/Data1/Books/Library`:

1. **No page awareness.** "First in file byte order" is not the same as
   "first page of the document" — object order in a PDF file has no
   required relationship to page order. Most real covers are on page 1
   (occasionally within the first ~10 pages), but the current scan has no
   way to prefer that.
2. **JPEG-only.** 30% of library PDFs (57/192) have their first image as
   `FlateDecode` (raw raster, not a pre-encoded JPEG), which the current
   code skips entirely.
3. **Nested-dictionary blind spot.** The scanning regex `<<([^>]*?)>>` is
   not nesting-aware, so any image dict containing a nested dict (e.g.
   `/DecodeParms<<...>>`, common on `FlateDecode` images with a
   `Predictor`) breaks the match. This alone accounted for all 16 PDFs
   (8%) where the current scanner finds *no* image at all, even though
   the images are present.

Combined, roughly a third to more of the library either gets no cover or
a byte-order-lucky (not page-order-correct) one today.

Separately: even a perfect heuristic will sometimes pick the wrong image
(e.g. a logo or diagram instead of the real cover), so a manual
override/undo mechanism is needed.

## Non-goals

- **JPXDecode (JPEG2000) support.** Only 4.7% of the library (9/192)
  contains a JPX image anywhere. Extracting the raw codestream is trivial
  (same as JPEG — JPX streams are already self-contained), but *displaying*
  it requires a JPEG2000 decoder, which no target webview (WebKitGTK,
  WebView2, WKWebView) supports natively, and Go's stdlib has none either.
  Supporting it would require a cgo binding to a native JPEG2000 library,
  breaking this codebase's dependency-free convention. Left out for now;
  may revisit.
- Any change to the *staging* (pre-organize) cover flow — this design is
  scoped to the already-organized Library view and its re-derive-on-scan
  covers.

## Design

### 1. PDF parsing engine (`internal/metadata/pdf.go`)

Extends the existing regex-based, dependency-free scanner with a real
(still text/stdlib-only) object model:

- **Object index**: map `objNum → byte offset`, built from literal
  `N 0 obj` markers.
- **ObjStm-aware lookup**: when an object isn't in the literal index,
  locate `/Type /ObjStm` objects, zlib-inflate them (`compress/zlib`,
  stdlib — no new dependency), and parse their `(objNum, offset)` header
  table to resolve objects compressed inside. Confirmed necessary: 30% of
  the library (58/192) has its page tree hidden inside a compressed
  ObjStm, which a plain-text scan cannot see at all.
- **Nesting-aware dict parser**: replaces `<<([^>]*?)>>` with a
  bracket-depth counter so dicts containing nested dicts (e.g.
  `/DecodeParms<<...>>`) parse correctly.
- **Page tree walker**: resolves `Catalog → Pages → Kids` recursively
  (via the object index) into an ordered page list, capped at
  `general.pdf_cover_page_limit` (new config key, default `10`).
- **Per-page image lookup**: for each page in order, resolves
  `Resources → XObject`, filters for `/Subtype /Image`, and handles:
  - `DCTDecode` → return raw stream bytes as-is (unchanged from today;
    already a complete JPEG).
  - `FlateDecode` → inflate, undo the predictor if present, map samples
    through colorspace, re-encode as PNG (`image/png`, stdlib). Scope,
    based on sampling the first FlateDecode image per file across the
    library:
    - Predictor: none (76% of samples) → direct byte mapping; TIFF-style
      `Predictor 2` (11%) → per-component horizontal diff; PNG-style
      `Predictor` ≥10 (14%) → per-row filter byte (Sub/Up/Average/Paeth).
      All three supported.
    - Colorspace: `DeviceRGB` (44%) and `DeviceGray` (26%) direct;
      `DeviceCMYK` (3%) via standard CMYK→RGB formula; a named/indirect
      colorspace resource (28%, resolved through `Resources/ColorSpace`)
      — `Indexed` handled via palette + base colorspace lookup,
      `ICCBased` approximated by its component count (1/3/4 →
      Gray/RGB/CMYK). This is a pragmatic approximation, not full ICC
      profile support.
  - `JPXDecode` → skipped (see Non-goals).
- **Fallback safety net**: if the page-tree walk fails entirely
  (malformed PDF, unresolvable refs) or finds nothing within the page
  cap, fall back to today's whole-file byte-order scan, then to
  no-cover. Never worse than current behavior.

### 2. Cover selection & caching flow

- `extractPDF` calls the new page-tree walker instead of `findPDFCover`,
  returning the first qualifying image in page order within the first
  `pdf_cover_page_limit` pages. `Result.CoverBytes`/`CoverContentType`
  contract is unchanged, so nothing downstream needs to change shape.
- `librarian.Scan` (librarian.go:73-78) checks the override index
  (Section 3) for a book's source path *before* calling
  `metadata.Extract`. If an override exists, it's used directly —
  extraction is skipped entirely for overridden books.
- `covercache.Ensure` is unchanged: still hashes by source path + mtime,
  caches under `log_folder/covers/`. Only *what* bytes it's asked to
  cache changes (from override or from the new extractor). Cache
  invalidation stays mtime-based regardless of override state — the
  smarter extractor doesn't change what triggers a re-cache.
- New config: `general.pdf_cover_page_limit` (default `10`), threaded
  from `internal/config` into the `librarian.Scan` call site.

### 3. Override / undo persistence

- New `log_folder/cover-overrides.json`: a flat JSON map keyed by source
  book path.
  ```json
  {
    "<absolute book path>": {
      "type": "embedded",
      "page": 3
    },
    "<absolute book path 2>": {
      "type": "custom",
      "imagePath": "log_folder/covers/overrides/<hash>.jpg"
    }
  }
  ```
  - `type: "embedded"` — re-extract that specific page's image at scan
    time (same per-page image lookup as Section 1, pointed at a fixed
    page instead of walking pages 1-N for the first hit).
  - `type: "custom"` — user-uploaded bytes, stored once under
    `log_folder/covers/overrides/`, referenced by path.
- New package code (`internal/covercache` or a sibling file) exposing
  `GetOverride(path) (Override, bool)`, `SetOverride(path, Override)
  error`, `ClearOverride(path) error` (the "undo").
- `librarian.Scan` calls `GetOverride` per book before extraction.
- Concurrency: whole-file read-modify-write on `Set`/`Clear`, matching
  this codebase's existing scale (no concurrent multi-process access to
  the Library today).
- Chosen over a sidecar file next to each book in `library_folder`
  because it's consistent with the existing convention that
  `log_folder` is where all derived/cache state lives, and it doesn't
  require `librarian.Scan` or the rename/move pipeline to learn about a
  new file type living inside the organized library. Trade-off: an
  override won't travel if `library_folder` is copied to another
  machine without also copying `log_folder`.

### 4. Frontend UI

**New `appapi` methods (Wails-bound):**
- `ListPDFCoverCandidates(bookPath string) ([]CoverCandidate, error)` —
  runs the page 1-N image walk in "collect all" mode (not stop-at-first)
  and returns `{page int, thumbnailURL string}` per candidate, writing
  thumbnails into `log_folder/covers/candidates/`.
- `SetCoverOverride(bookPath string, page int) (coverURL string, error)`
  — writes an `"embedded"` override, immediately re-extracts/re-caches
  (bypassing the normal mtime check since the source file hasn't
  changed but the result must update now), returns the new
  `/covers/...` URL.
- `SetCoverOverrideCustom(bookPath string, imageBytes []byte,
  contentType string) (coverURL string, error)` — same, for an uploaded
  image.
- `ClearCoverOverride(bookPath string) (coverURL string, error)` — the
  undo: removes the override entry, re-runs normal auto-detection,
  returns the resulting URL (which may be empty if extraction genuinely
  finds nothing).

**`internal/librarian.Book`** gains one new field, `CoverOverridden
bool`, so the UI knows whether to offer "Choose cover..." vs. "Change
cover... / Reset to auto-detected".

**Frontend** (`desktop/frontend/src`): `LibraryBookCard.svelte` gets a
hover-revealed action button (matching the card's existing click-to-open
discoverability) opening a new `CoverPickerModal.svelte`:
- Calls `ListPDFCoverCandidates` on open; renders a thumbnail grid
  (page number as caption) plus an "Upload custom image…" tile.
- Selecting a thumbnail calls `SetCoverOverride`; the upload tile opens
  a native file picker (Wails runtime dialog) and calls
  `SetCoverOverrideCustom`.
- On success, the modal closes and the book card's `<img>` src updates
  to the returned URL immediately — no re-scan needed to see the change.
- If `CoverOverridden` is true, the modal also offers "Reset to
  auto-detected", calling `ClearCoverOverride`.

## Testing

- Unit tests for the new parser pieces against synthetic PDF fixtures:
  object index resolution, ObjStm decompression + lookup, nested-dict
  parsing, page-tree walk (including a Kids-array-of-Kids case), and
  each predictor/colorspace combination for `FlateDecode` reconstruction.
- Regression fixtures built from the specific library files that
  surfaced each gap during design (nested-`DecodeParms` file, an
  ObjStm-only-page-tree file, a `Predictor 2` file, a `Predictor 15`
  file, an `Indexed`-colorspace file) so real-world cases stay covered.
- Override persistence: set/get/clear round-trip, and a re-scan
  confirming an override suppresses extraction entirely.
- Manual UI verification: open the picker on a book with multiple
  page-1-10 images, confirm thumbnail grid, select one, confirm the
  card updates immediately; upload a custom image; reset to
  auto-detected; confirm all three update the visible cover without
  requiring an app restart or full rescan.
