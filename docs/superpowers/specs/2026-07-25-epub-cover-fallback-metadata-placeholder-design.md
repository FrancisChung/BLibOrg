# Design: EPUB cover fallback, placeholder Title/Author, and Library-view heuristic fallback

## Background

Investigating why "Cloud Native Microservices Cookbook (2024) - Varun
Yadav.epub" showed no cover in the Library view (reproduced directly
against the real file) surfaced two independent, confirmed bugs, plus a
broader architectural gap the second bug's fix depends on. The file is a
calibre-converted EPUB (filenames like `728310488_split_000.html` and
`image-tybbjpje.jpg` are calibre's auto-generated pattern, likely from a
Google Play Books export) with no real cover, title, or author metadata.

## Bug A: no cover -- no OPF cover convention present

**Root cause:** `findEpubCoverItem`
(`internal/metadata/epub.go`) only recognizes the EPUB3 convention (a
manifest `<item>` whose `properties` includes `cover-image`) and the
EPUB2 convention (`<meta name="cover" content="ITEM_ID"/>`). This file's
`content.opf` contains zero occurrences of the word "cover" anywhere --
confirmed by direct inspection. Its actual cover is embedded the
old-fashioned way: the first spine document
(`728310488_split_000.html`) is a near-empty page whose entire body is
exactly `<img src="image-tybbjpje.jpg"/>`.

**Fix:** a new fallback, `findEpubFirstSpineImage`, used only when
`findEpubCoverItem` returns `ok=false`. It resolves the first
`<itemref idref="...">` in the OPF's `<spine>` to its manifest `href`,
opens that spine document, and regex-matches its first `<img src="...">`
tag (`(?i)<img[^>]*\ssrc=["']([^"']*)["']`) -- consistent with this
package's existing best-effort, non-full-parser philosophy (matching how
the PDF scanner already handles analogous whole-file/page fallbacks). The
resolved image path is looked up against the manifest for its declared
`media-type`; if the image isn't itself declared in the manifest (a more
malformed file than this one), the fix falls back to guessing a
media-type from the file extension (`.jpg`/`.jpeg` -> `image/jpeg`,
`.png` -> `image/png`, `.gif` -> `image/gif`, `.svg` -> `image/svg+xml`),
and gives up cleanly (`ok=false`, no cover set) if even that fails --
this fallback never sets `CoverContentType` to an unknown/empty value.

The existing "open the zip entry, read its bytes, set
`Result.CoverBytes`/`CoverContentType`" block in `extractEpub` (currently
inlined once for `findEpubCoverItem`'s result) is extracted into a small
shared helper, `readEpubCoverBytes`, so both cover-finding paths reuse it
rather than duplicating it a second time.

## Bug B: garbage placeholder Title/Author, and a Library-view gap

**Root cause (part 1):** this file's OPF metadata literally contains
`<dc:title>728310488</dc:title>` (the source catalog's internal numeric
ID, left behind by the conversion) and `<dc:creator>Unknown</dc:creator>`
(a literal placeholder). `extractEpub` currently returns these as-is.
Both are non-empty, so nothing downstream ever treats them as "not
found."

**Root cause (part 2, broader):** even once a field is blank, the
filename-heuristic fallback (`internal/heuristics.Parse`, which correctly
derives `Title="Cloud Native Microservices Cookbook"`,
`Author="Varun Yadav"`, `Year="2024"` from this file's actual filename --
verified directly) is only wired into `internal/pipeline.Run` (the
separate Scan & Review / "organize new books" workflow). `internal/
librarian.Scan` (the Library view's own backend, which reads back
already-organized books) never calls it at all -- an empty Title there
just stays empty, falling through only to the frontend's own raw-filename
display fallback (`book.title || filenameNoExt(sourcePath)` in
`LibraryBookCard.svelte`), not the cleaner, properly-parsed heuristic
result.

**Fix:**
1. In `extractEpub`, blank an all-numeric `Title` (regex `^[0-9]+$`,
   trimmed) and an `Author` matching `"unknown"` (case-insensitive,
   trimmed) back to `""` -- mirroring the existing
   `internal/metadata/pdf.go` `placeholderTitles` pattern (`{"untitled":
   true}`), extended with an EPUB-specific analog for both fields.
2. In `internal/librarian.Scan`, after extraction (`b.Title = res.Title`,
   etc.), add the same "if Title/Author/Year is empty, try
   `heuristics.Parse` on the filename stem, keep the metadata-sourced
   value if non-empty" fallback that `internal/pipeline.Run` already
   has, reusing `cfg.Heuristics.KnownJunkTags` (already in scope in
   `Scan`). No `book.Field{Source: ...}` wrapping is needed here --
   unlike `internal/pipeline`'s `book.Book`, `librarian.Book`'s
   Title/Author/Year are plain strings with no provenance tracking, so
   the fallback is a direct string assignment. This applies to every
   book in the library with an empty field after extraction (any format,
   not just this EPUB), not only this one file.

## Version bumps

- `metadata.CoverExtractorVersion`: bumps (Bug A changes what cover
  bytes an already-cached EPUB can produce).
- `metadata.MetadataExtractorVersion`: bumps (both Bug B's
  placeholder-blanking and the new `librarian.Scan` heuristic-fallback
  wiring change Title/Author/Year for already-cached books).

## Testing

- **Bug A:** a synthetic EPUB fixture (real zip, real OPF with a
  `<spine>` and a manifest declaring both the first spine HTML document
  and its embedded image, no cover-image property/meta anywhere) where
  `extractEpub` must locate and return the correct cover bytes/content-type
  via the new fallback. A second fixture confirms the existing
  `TestExtractEpub_NoCoverLeavesFieldEmpty` test (no spine at all) still
  passes unchanged -- the fallback requires a spine to exist, so it
  correctly stays inert for that fixture. A third fixture covers the
  extension-guessing branch: an `<img>` referencing a file not declared
  in the manifest at all, confirming the media-type is still correctly
  inferred from its extension.
- **Bug B part 1:** two new `extractEpub`-level tests -- an all-numeric
  `<dc:title>` resolves to an empty `Title`; `<dc:creator>Unknown</dc:creator>`
  (and a mixed-case variant, e.g. `"UNKNOWN"`) resolves to an empty
  `Author`.
- **Bug B part 2:** a new `librarian.Scan` test with a mocked
  `extractFunc` returning empty Title/Author/Year, confirming the
  returned `Book`'s fields are populated from `heuristics.Parse` against
  the file's basename instead of staying empty -- mirroring this
  package's existing `extractFunc`-mocking test conventions.
- **End-to-end regression:** the real file used throughout this
  investigation is available locally for manual verification after
  implementation, not committed as a test fixture (matching this
  package's existing synthetic-fixture convention).

## Non-goals

- No change to `internal/pipeline.Run`'s own existing heuristic-fallback
  logic -- it already does the right thing; only `internal/librarian.Scan`
  gains the equivalent.
- No attempt to give `librarian.Book`'s Title/Author/Year the same
  `Source` provenance tracking `pipeline`'s `book.Field` has -- out of
  scope, the Library view has never needed to distinguish "from metadata"
  vs. "from filename heuristics" for display purposes.
- No general HTML/XHTML parser for spine documents -- the `<img>` scan is
  a single best-effort regex, matching this package's established
  philosophy elsewhere (PDF content-stream scanning, EPUB's own existing
  container/OPF handling already tolerates malformed input by degrading
  rather than erroring).
