# Design: linearized-PDF trailer selection and JPXDecode cover fallback

## Background

Investigating why "Build Your API with Spring (2022)" showed no cover
(reproduced directly against the real file) surfaced two independent,
confirmed bugs in `internal/metadata`'s PDF scanner. Both were verified
directly before this design: Bug A by byte-offset inspection of the raw
file, Bug B by actually calling the existing PDFium renderer against the
real page and visually confirming the correct cover comes out.

## Bug A: linearized-PDF trailer selection picks the wrong trailer

**Root cause:** `findInfoDictBody` (`internal/metadata/pdf.go`) takes the
byte-order-*last* classic `trailer <<...>>` block as authoritative — a
deliberate, already-tested heuristic for genuinely incrementally-updated
files, where the last trailer in file order really is the newest
revision. This book is a **linearized** PDF (`/Linearized 1` in its
header, matching the ISO 32000 Annex F structure), where that assumption
is backwards: the *first* trailer in byte order (prepended near the start
of the file, for fast web view) is the complete, authoritative one — it
has `/Info` and a `/Prev` pointing to the base/tail cross-reference
table — while the *last* trailer in byte order (associated with that
base/tail table, at the very end of the file) has been stripped of
`/Root` and `/Info` entirely by the linearization tool. Confirmed
directly: trailer[0] (byte offset 571) has `/Info 1025 0 R`; trailer[1]
(byte offset 1884813, near the true end of the file) has only
`/Size 1026/ID[...]`, no `/Info` at all. Blindly trusting "last in file
order" therefore picks the trailer with no `/Info`, even though one
exists earlier in the file.

(For this specific book, object 1025 — the real Info dictionary — turns
out to have no `/Title`/`/Author` fields set at all, only
`/CreationDate`/`/Creator`/`/Producer`, so this particular fix doesn't
change this book's own Title/Author output. It's a real, generalizable
correctness bug for other linearized PDFs whose Info dict does have
Title/Author, which is why it's worth fixing regardless.)

**Fix:** instead of unconditionally taking the byte-order-last trailer,
scan trailers in reverse file order and use the first one encountered
(i.e., the last-in-file-order one) that actually has a resolvable
`/Info` key. This preserves the existing "last wins" semantics for the
already-tested genuine-incremental-update case (where every trailer has
`/Info`, and the true last one should win — the reverse scan finds it
immediately, on its first iteration), while correctly skipping over a
linearized PDF's tail trailer that lacks `/Info`. Falls through to the
existing `findXRefStreamTrailerDict` fallback only if *no* classic
trailer has `/Info` at all — identical to today's "no trailer found"
behavior.

## Bug B: JPXDecode (JPEG 2000) images are silently unsupported

**Root cause:** page 1's cover image (object 1046) has
`/Filter/JPXDecode`. Neither `decodePDFImageStream`'s DCTDecode
passthrough nor its FlateDecode reconstruction path recognizes this
filter, so it returns `ok=false` and the image is silently skipped
entirely. `findPDFPageImages` therefore returns zero images for this
page — and since `findPDFCoverPageAware`'s composite-cover PDFium-render
fallback is gated behind "at least one image was *decoded* first," it
never even triggers. The whole-file legacy fallback (`findPDFCover`) also
only recognizes DCTDecode/FlateDecode. Net result: no cover at all, even
though the image is directly referenced on the page (not nested in a Form
XObject — a different root cause from the earlier "Programming with
Types" bug this session). Unlike DCTDecode, there's no simple raw
passthrough option for JPXDecode either: JPEG 2000 isn't natively
renderable in the app's webview (Chromium/WebKitGTK don't support it in
`<img>` tags), so even extracting the raw bytes wouldn't display.
Confirmed directly: calling the existing `renderPDFPageAsCover` (PDFium,
added in a prior session for composite covers) against this exact page
succeeds and produces the correct, visually-verified cover.

**Fix:** a new function,
`findFirstPageWithUndecodableImage(idx *pdfObjIndex, pages []pdfPage) (pageNumber int, ok bool)`,
in `internal/metadata/pdf_images.go`. It walks each page's own
(non-nested-Form) XObject entries looking for a `/Subtype /Image` entry
that `decodePDFImageStream` couldn't decode, returning the first such
page. This is a standalone function — a deliberate, small amount of
duplicated traversal logic versus reusing `findImagesInXObjects`, chosen
specifically to avoid touching that already-shipped, already-tested
function's signature, since `ListPDFCoverCandidates`/`ExtractPDFPageCover`
(the manual cover-picker's backend) also depend on it and should stay
unaffected — this fix is scoped to `findPDFCoverPageAware`'s auto-detect
path only, matching how the existing composite-cover PDFium fallback is
also auto-detect-only.

In `findPDFCoverPageAware` (`pdf.go`), once the primary
`findPDFPageImages` search comes up empty (`len(images) == 0`), it calls
`findFirstPageWithUndecodableImage` on the same already-walked pages; if
it finds a page, `renderPDFPageAsCoverFunc` is tried on it (same seam and
pattern as the existing composite-cover fallback). Only if that also
fails, or no such page exists, does it fall through to the final
whole-file `findPDFCover` scan — preserving the "never worse than before"
guarantee already established for the composite-cover fallback.

## Version bumps

- `metadata.CoverExtractorVersion`: 4 → 5 (Bug B changes cover bytes for
  already-cached books with an undecodable-filter cover image).
- `metadata.MetadataExtractorVersion`: 3 → 4 (Bug A changes which Info
  dict object is used for Title/Author/Subject on a linearized PDF whose
  tail trailer lacks `/Info`).

## Testing

- **Bug A:** a synthetic fixture with two classic trailers — the first
  (byte-order) with a resolvable `/Info`, the second (byte-order-last)
  without one — confirms `extractPDF` uses the `/Info`-bearing trailer
  rather than the literal last one. The existing
  `TestExtractPDF_UsesLatestIncrementalUpdateOfInfoObject` (both trailers
  have `/Info`) and `TestExtractPDF_TitleAuthorEmptyWhenInfoReferenceMissing`/
  `TestExtractPDF_NoTrailerLeavesTitleAuthorEmptyButStillFindsYear` (no
  trailer has `/Info`, or no trailer at all) already lock in that the fix
  doesn't change either of those cases — re-run as regression checks, not
  modified.
- **Bug B:** a direct unit test for `findFirstPageWithUndecodableImage`
  (finds the right page among several; returns `ok=false` when no page has
  any image XObject at all) plus two `findPDFCoverPageAware`-level tests
  using the existing `renderPDFPageAsCoverFunc` seam — one proving the
  PDFium fallback is invoked (and with the correct page number) when the
  only image has an unsupported filter, one proving the existing
  whole-file DCTDecode fallback still runs if PDFium rendering *also*
  fails, so this case is never worse than before.
- **End-to-end regression:** the real file used throughout this
  investigation is available locally for manual verification after
  implementation, not committed as a test fixture (matching this
  package's existing synthetic-fixture convention).

## Non-goals

- No general JPEG 2000 decoder — PDFium already handles this via full-page
  rendering; no new dependency or codec work needed.
- No change to `ListPDFCoverCandidates`/`ExtractPDFPageCover` (the manual
  cover-picker) — both fixes are scoped to `findPDFCoverPageAware`'s
  auto-detect path only.
- No attempt to generalize Bug A's fix into a full linearized-PDF-aware
  parser — this is a targeted trailer-selection correction, not a
  rewrite of this package's deliberately best-effort approach.
