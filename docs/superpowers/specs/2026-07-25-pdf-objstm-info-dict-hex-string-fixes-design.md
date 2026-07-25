# Design: ObjStm-aware Info dict lookup + hex-string field support

## Background

Investigating why "Domain-Driven Design in PHP (2022)" shows no Title/Author
in the Library view (reproduced directly against the real file) surfaced two
distinct, confirmed, compounding bugs in `internal/metadata/pdf.go`'s
`findInfoDictBody` and the field-extraction logic it feeds
(`extractPDF`). The book's cover extracts correctly already (a separate,
previously-fixed issue) — this investigation is scoped to Title/Author only.

This PDF is produced by XeTeX. Its Info dictionary (object 2) is compressed
inside a `/Type /ObjStm` object rather than present as a standalone literal
`2 0 obj ... endobj` block, and its `/Title`/`/Author` values are written as
PDF hex strings (`<feff0044...>`, UTF-16BE with a BOM) rather than literal
parenthesized strings (`(...)`). Both are spec-legal, common PDF-producer
choices this package's scanner doesn't yet handle.

## Bug 1: `findInfoDictBody`'s object lookup doesn't resolve ObjStm-compressed objects

**Root cause:** after locating the `/Info N 0 R` reference (via either a
classic trailer or, since a prior fix, an XRef-stream trailer),
`findInfoDictBody`'s final step does its own raw regex scan of the whole
file (`regexp.MustCompile(`(?s)\bN\s+\d+\s+obj(.*?)endobj`)`) to find that
object's body. This can never find an object compressed inside an ObjStm,
since such an object never appears as literal `N M obj...endobj` text
anywhere in the file — it only exists as bytes inside another object's
compressed stream. Confirmed directly: `idx.literal[2]` (the plain literal
index) doesn't have this book's Info object, but `idx.lookup(2)` (the
ObjStm-aware lookup already used everywhere else in this package — page-tree
walking, image finding) resolves it correctly.

**Fix:** build a `pdfObjIndex` once at the top of `findInfoDictBody` via the
existing `buildPDFObjIndex`, and use `idx.lookup(objNum)` for the final
object-body lookup instead of a fresh regex scan. `findXRefStreamTrailerDict`
(added in the prior fix) currently builds its own second, redundant index
internally — its signature changes from `(data []byte)` to
`(idx *pdfObjIndex)` so `findInfoDictBody` passes the same index through,
avoiding a second full-file re-scan per call. No other caller of either
function exists, so this is an internal-only signature change.

`idx.literal`'s existing behavior (built by `buildPDFObjIndex`, iterating
matches in file order and overwriting on each occurrence of the same object
number) already stores the *last* literal occurrence of a given object
number — exactly the semantics `findInfoDictBody`'s current
`objMatches[len(objMatches)-1]` provides for "last incremental update wins."
Switching to `idx.lookup()` is therefore a strict capability superset: it
preserves that existing behavior for literal objects and additionally
resolves ObjStm-compressed ones. (A pre-existing, already-accepted edge case
elsewhere in this package — `idx.lookup()` checks the literal index before
ever falling back to ObjStm, so an object that was literal in an early
incremental-update revision but got recompressed into an ObjStm in a later
revision would resolve to the stale literal version — is inherited here
too, unchanged from how the rest of the package already behaves. Not
addressed by this fix; out of scope.)

## Bug 2: hex-string Title/Author values aren't recognized

**Root cause:** PDF text fields can be written as literal strings
(`(...)`, with backslash-escapes) or hex strings (`<...>`, pairs of hex
digits). `extractPDF`'s field-scraping regex, `pdfLiteralStringRe`, only
matches the literal-string syntax. This book's actual Info dict body (once
Bug 1's fix locates it) is
`<</Creator(LaTeX with hyperref package)/Title<feff0044006f...>/Author<feff0043006100...>/Producer(XeTeX 0.99999)/CreationDate(D:20220523070329-00'00')>>`
— `/Title` and `/Author` are hex strings; `/Creator`, `/Producer`,
`/CreationDate` happen to be literal strings in this file (which is why
`CreationDate`/Year already extracted correctly even before this fix, and
only Title/Author were missing).

**Fix:** add a second regex, `pdfHexStringRe`, matching the same four keys
(`Title|Author|Subject|CreationDate`) in hex-string syntax:
`` /(Title|Author|Subject|CreationDate)\s*<([0-9A-Fa-f\s]*)>` ``. Add a new
`decodePDFHexBytes(h []byte) []byte` helper — sibling to the existing
`unescapePDFBytes` (which turns literal-string source syntax into raw
bytes) — that strips whitespace/non-hex characters, pads an odd trailing
digit with an implicit `0` (per spec), and hex-decodes via
`encoding/hex.DecodeString`. The resulting raw bytes feed into the
*existing* `decodePDFString`, unchanged — it already correctly detects a
leading `0xFE 0xFF` BOM and decodes UTF-16BE, exactly what this book's
hex-encoded Title/Author need; no new encoding-handling logic required.

In `extractPDF`'s field-population loop, a second pass over
`pdfHexStringRe.FindAllSubmatch(scope, -1)` runs after the existing
literal-string pass, applying the identical guards already in place (the
`!foundInfo` safeguard skipping Title/Author when the real Info dict
couldn't be confirmed; "keep first match, skip if already set" dedup) — so
a hex-string match only fills in a key the literal-string pass didn't
already set. This handles the (vanishingly rare in practice) theoretical
case of a dict mixing both syntaxes, with literal-string syntax taking
precedence, without needing a single combined regex.

## Testing

- **Bug 1:** a synthetic fixture (reusing the existing
  `TestPDFObjIndex_ResolvesObjectInsideObjStm`-style pattern: real,
  byte-offset-computed ObjStm header + zlib-compressed content, not
  hand-waved) with an Info dictionary compressed inside an ObjStm and no
  literal occurrence anywhere in the file, referenced via a classic
  trailer's `/Info N 0 R`. `extractPDF` must locate and extract its
  Title/Author correctly. A second fixture repeats this via an XRef-stream
  trailer instead of a classic one (exercising `findXRefStreamTrailerDict`'s
  updated signature end-to-end).
- **Bug 2:** a fixture with `/Title`/`/Author` written as hex strings
  (UTF-16BE with a BOM, matching this book's real encoding) inside an
  otherwise-normal literal Info dict (classic trailer, literal object) —
  isolates Bug 2 from Bug 1. `extractPDF` must decode both correctly. A
  second, smaller test exercises `decodePDFHexBytes` directly: odd-length
  hex input gets the implicit trailing-zero padding: PDF spec example is
  `<901FA3>` (even, 3 bytes) vs `<901FA>` (odd, pads to `<901FA0>`, 3
  bytes) — confirms the padding behavior in isolation, not just through
  the full `extractPDF` path.
- **End-to-end regression:** the real file used throughout this
  investigation is available locally for manual verification after
  implementation (not committed as a test fixture, matching this
  package's existing synthetic-fixture convention).
- **Version bump:** this only affects Title/Author, not cover bytes, so
  `metadata.MetadataExtractorVersion` bumps from 1 to 2 (no change to
  `CoverExtractorVersion`) — mirroring the version-split convention
  established in the prior fix, so already-cached books with empty
  Title/Author self-heal on their next scan without a manual cache
  reset. A `librarian_test.go` test confirms a stale `MetadataVersion`
  entry (matching ModTime/Size/CoverVersion, but `MetadataVersion: 1`)
  forces re-extraction under the new `MetadataExtractorVersion = 2`.

## Non-goals

- No general hex-string support beyond the four already-scraped keys
  (Title/Author/Subject/CreationDate) — this package deliberately doesn't
  parse arbitrary PDF dictionary values.
- No fix for the pre-existing literal-vs-ObjStm revision-precedence edge
  case noted under Bug 1 — inherited as-is from `idx.lookup()`'s existing,
  already-accepted behavior.
- No consolidation of the separate `pdfObjIndex` builds already happening
  across `findInfoDictBody` and `findPDFCoverPageAware` within a single
  `extractPDF` call (each serves a different, independent extraction
  concern) — out of scope for this fix, which only touches
  `findInfoDictBody`'s own internal duplication.
