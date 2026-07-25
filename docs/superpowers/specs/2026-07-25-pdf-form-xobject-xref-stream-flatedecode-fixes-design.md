# Design: Form XObject cover recursion, XRef-stream metadata lookup, FlateDecode fallback support

## Background

Investigating why "Programming with Types (2019) - Vlad Riscutia.pdf" showed
no cover and no Title/Author in the Library view (reproduced directly
against the real file, not guessed from source) surfaced three distinct,
confirmed root causes in `internal/metadata`'s hand-rolled PDF scanner. All
three stem from this specific PDF (produced by an iText 7.2.5
prepress/OPI-style workflow) using PDF features the scanner wasn't built to
expect, even though the underlying decoding infrastructure for all three
already exists elsewhere in the package.

## Bug A: cover images nested inside Form XObjects aren't found

**Root cause:** the real cover image (`FlateDecode`/`DeviceCMYK`, 1111x1392,
verified correct by manually decoding it) is not directly in page 1's
`/Resources/XObject` dictionary. Instead, page 1 invokes a Form XObject
(`/Fm1 Do`), and *that* form's own nested `/Resources/XObject` is what
contains the actual image (`/Im1`). `findPDFPageImages`
(`internal/metadata/pdf_images.go`) only inspects a page's own top-level
XObject entries; when an entry's `/Subtype` is `/Form` rather than
`/Image`, it's silently skipped — never recursed into. Because zero images
are ever found on the page, `findPDFCoverPageAware`'s composite-cover
PDFium full-page-render fallback (`internal/metadata/pdf_render.go`, added
in a prior session) never triggers either, since that path requires
`len(images) > 0` before it even runs the composite heuristic. The
whole-file legacy fallback (`findPDFCover`) only recognizes `DCTDecode`
(JPEG) by design, and this image is `FlateDecode`, so it misses it too. Net
result: no cover extracted at all.

Confirmed by direct reproduction that this book's page 1 *is* a genuine
composite cover, not just a nested-image lookup gap: the page's own content
stream draws `/Fm1 Do` (the image) and then separately draws three
vector-text `Tj` operators on top ("MANNING", "Vlad Riscutia", "Examples in
TypeScript" — publisher/author/subtitle). `pageContentSuggestsCompositeCover`
already correctly detects this once the image is found at all, so fixing
the lookup gap is the only change needed here — the composite-render
chain built for the earlier "AI Engineering" bug already does the right
thing once it has an image to key off.

**Fix:** `findPDFPageImages` recurses into Form XObjects (`/Subtype
/Form`) found in a page's `/Resources/XObject`, looking for images in the
form's own nested `/Resources/XObject`, instead of skipping them. Capped
at 4 levels of nesting, with a `visited` set of already-seen object numbers
(mirroring `collectPDFPages`' existing Kids-cycle guard in
`internal/metadata/pdf_pages.go`) to protect against a malformed or cyclic
PDF. `ListPDFCoverCandidates` and `ExtractPDFPageCover`
(`internal/metadata/pdf_override.go`, the manual cover-override picker's
backend) both already call `findPDFPageImages` directly, so this fix
improves them automatically — no separate change needed there.

## Bug B: Title/Author aren't found in PDF 1.5+ cross-reference-stream files

**Root cause:** this file's Info dictionary (object 17191, containing
correct literal `/Title`, `/Author`, `/CreationDate`, etc.) is present and
trivially locatable — but `findInfoDictBody`
(`internal/metadata/pdf.go`) only locates it via a classic `trailer
<<...>>` keyword block, extracting `/Info N M R` from within it. This file
uses a PDF 1.5+ cross-reference **stream** instead (no plain `trailer`
keyword appears anywhere in the file); the trailer-equivalent keys
(`/Root`, `/Info`, `/Size`, etc.) live directly in the XRef stream object's
own dictionary (confirmed: object 18281, `<</Filter/FlateDecode/.../Info
17191 0 R/.../Root 17193 0 R/.../Type/XRef/...>>`). Since
`pdfTrailerRe.FindAllSubmatch` finds zero matches, `findInfoDictBody`
returns `nil, false` immediately, and `extractPDF`'s existing safeguard
(documented in its doc comment) deliberately disables Title/Author
extraction rather than risk a false-positive whole-file scan picking up an
unrelated object's `/Title` (e.g. a bookmark or embedded graphic).
`internal/metadata/pdf_pages.go`'s `walkPDFPageTree` already works around
this same trailer gap for Catalog resolution, by searching for `/Type
/Catalog` directly (`idx.find`) instead of depending on the trailer's
`/Root` reference — Info-dict lookup never got the same treatment.

**Fix:** when the classic-trailer path finds nothing, `findInfoDictBody`
builds a `pdfObjIndex` (`buildPDFObjIndex`) and searches `idx.literal` for
an object whose body matches `/Type\s*/XRef`, taking the *last* such match
in file order (mirroring `findInfoDictBody`'s existing "last trailer wins"
handling for incrementally-updated files, since a later XRef stream is
more authoritative than an earlier one). The same `pdfInfoRefRe` already
used for classic trailers extracts `/Info N M R` from that object's body.
`findInfoDictBody`'s signature stays `(data []byte)` — the `pdfObjIndex` is
only built internally, and only on this fallback path, so the common
case (a classic trailer exists) pays no extra cost.

**Cache invalidation:** Title/Author are cached in
`librarycache.Entry.Title`/`.Author`, and cache freshness is governed only
by the source file's ModTime/Size plus `CoverVersion` — there is no
equivalent version stamp for Title/Author, so this fix alone would never
reach an already-scanned book like this one (its file won't change, so
`cache.Fresh` reports true, and `CoverVersion` matching is silent on
Title/Author correctness). A new `metadata.MetadataExtractorVersion`
const (starting at 1, doc-commented the same way `CoverExtractorVersion`
is: bump whenever a change here could produce different
Title/Author/Year for an already-scanned file) plus a new
`librarycache.Entry.MetadataVersion int` field (JSON tag
`metadataVersion`) mirror the existing `CoverVersion` pattern exactly.
`internal/librarian/librarian.go`'s cache-hit condition
(`librarian.go:117`) becomes:

```go
if entry, ok := cache.Fresh(path, info.ModTime(), info.Size()); ok &&
    entry.CoverVersion == metadata.CoverExtractorVersion &&
    entry.MetadataVersion == metadata.MetadataExtractorVersion {
```

and the `cache.Put(...)` call (`librarian.go:172`) gains
`MetadataVersion: metadata.MetadataExtractorVersion` alongside the
existing `CoverVersion: metadata.CoverExtractorVersion`. This keeps the
two version stamps independent, as intended: a future Title/Author-only
change bumps `MetadataExtractorVersion` without forcing every book's
already-correct cover to be needlessly re-extracted and re-cached, and
vice versa.

## Bug C: the whole-file fallback scanner only recognizes JPEG images

**Root cause:** `findPDFCover` (`internal/metadata/pdf.go`), the
last-resort whole-file byte-order scan used when page-tree resolution
fails entirely, only accepts images whose `/Filter` is `/DCTDecode`
(JPEG) — by original design, per its doc comment. `decodeFlatePDFImage`
(`internal/metadata/pdf_flate.go`) already exists and fully supports
`FlateDecode` images (including `DeviceCMYK`, as proven by this
investigation's own manual decode of this book's cover), but `findPDFCover`
never calls it.

**Fix:** `findPDFCover` gains a `*pdfObjIndex` parameter (its one
production caller, `findPDFCoverPageAware`, already builds one at the top
of the function and can pass it straight through) and, for each candidate
image whose dict doesn't match `/DCTDecode`, attempts
`decodeFlatePDFImage(idx, nil, dict, stream)` — passing `resources=nil`
since this whole-file scan has no page context to draw a Resources scope
from. Colorspaces resolvable without a Resources dict (inline
`/DeviceRGB`/`/DeviceGray`/`/DeviceCMYK`, or an indirect reference)
decode successfully; a named colorspace requiring Resources-dict lookup
(e.g. `/ColorSpace /CS0`) fails gracefully (`ok=false`, image skipped) —
an acceptable, documented limitation of this last-resort path, consistent
with the rest of this package's best-effort philosophy.

## Version bumps

- `metadata.CoverExtractorVersion`: 2 → 3 (Bugs A and C both change what
  cover bytes an already-scanned file can produce).
- `metadata.MetadataExtractorVersion`: new, starts at 1 (Bug B affects
  Title/Author only).

## Testing

Each fix gets unit tests using this package's existing
`buildMinimalValidPDF`-style fixture-construction conventions (real,
byte-offset-computed structures, not hand-waved), extended to also cover:

- **Bug A:** a fixture page whose `/Resources/XObject` contains only a
  `/Subtype /Form` entry, whose own `/Resources/XObject` contains a real
  image — `findPDFPageImages` must find it. A second fixture nests forms
  5 levels deep — the 5th-level image must NOT be found (depth cap
  enforced). A third fixture has two Form XObjects referencing each other
  (a manufactured cycle) — must terminate without hanging or panicking
  (`visited` guard).
- **Bug B:** a fixture PDF with a `/Type /XRef` stream object carrying
  `/Info N M R` and no classic `trailer` keyword anywhere —
  `findInfoDictBody` must locate the Info dict correctly. A second fixture
  has two `/Type /XRef` objects (simulating an incremental update) with
  different `/Info` targets — the *last* one in file order must win.
- **Bug C:** a fixture whole-file (no resolvable page tree) with a single
  `FlateDecode`/`DeviceRGB` image and no `DCTDecode` image anywhere —
  `findPDFCover` must now find and decode it.
- **Cache invalidation:** a `librarian.Scan` test with a cached entry
  matching ModTime/Size and the current `CoverVersion` but
  `MetadataVersion: 0` (simulating a pre-fix entry) must NOT be served
  from cache — extraction must re-run. A companion test confirms a fresh
  `cache.Put` after re-extraction sets `MetadataVersion:
  metadata.MetadataExtractorVersion`.
- **End-to-end regression:** the real file used throughout this
  investigation (`/media/francis/Data1/Books/Library/Technology/Typescript/Programming
  with Types (2019) - Vlad Riscutia.pdf`) is available locally for manual
  verification after implementation, but is not itself committed as a test
  fixture (this package's existing convention is synthetic
  `buildMinimalValidPDF`-style fixtures, not real copyrighted books).

## Non-goals

- No change to `pageContentSuggestsCompositeCover` or the PDFium
  full-page-render path itself — both already behave correctly once Bug
  A's lookup gap is fixed.
- No general-purpose recursive PDF object resolver refactor — each fix is
  a small, targeted addition following this file's existing regex/index
  conventions, not a rewrite.
- Bug C's `resources=nil` limitation (named colorspaces unresolved in the
  whole-file fallback) is accepted as-is, not solved by, e.g., scanning
  every page's Resources to guess a scope.
