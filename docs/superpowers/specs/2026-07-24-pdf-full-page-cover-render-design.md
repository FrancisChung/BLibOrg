# Design: Full-page PDF rendering fallback for composite covers

## Background

`internal/metadata`'s PDF cover extraction is a "dependency-free, best-effort
textual scanner, not a real PDF parser": it locates a page's embedded raster
image XObject (the `/ImN Do` operator in a page's content stream) and returns
its raw bytes. This is exactly right for the common case where a book's cover
*is* a single flattened raster image filling the page.

It structurally cannot handle a **composite cover**: a page where the
illustration is one embedded image, but the title, author name, and/or
publisher logo are drawn *separately* as vector text (`BT`/`Tj`/`TJ`
operators) and vector paths (`m`/`l`/`c`/`re`/fill operators) directly in the
page's content stream, layered on top of the image. "AI Engineering" by Chip
Huyen (O'Reilly) is a confirmed real example — page 1's decompressed content
stream contains `/Im0 Do` (the owl illustration) *and* a separate `BT ...
[(C)(h)(i)(p)( H)(u)(ye)(n)]TJ ... ET` block (the author's name) *and* vector
path-fill operators in O'Reilly's brand red (the logo mark). No amount of
cache-correctness work (this session's two prior fixes: version-stamping and
cache-busting) can fix this — the extractor was never returning the wrong
bytes for the image it found; it was returning the *only* bytes it's capable
of finding, which is just the illustration.

## Goal

When the current extractor's result looks like it's missing composited
text/graphics, fall back to rendering that page in full (fonts, vector paths,
compositing, at the page's actual dimensions) as a raster image, so the
extracted cover matches what a human sees when they open the PDF.

## Constraints

- Desktop app: must not require the end user to separately install any
  external tool or system library (rules out shelling out to `pdftoppm`/
  `mutool`, and rules out CGo bindings that need PDFium/MuPDF present on the
  host).
- No AGPL/GPL dependency (rules out MuPDF-based options like `go-fitz`).
- Must not become a per-book cost on every scan — this session's caching
  work (`internal/librarycache`, `metadata.CoverExtractorVersion`) already
  makes re-extraction a one-time cost per book; this feature must fit that
  same model, not bypass it.

## Chosen approach: `github.com/klippa-app/go-pdfium`, WASM/wazero backend

- Wraps Google's PDFium (the engine Chrome uses for PDF viewing) — BSD
  licensed. The Go wrapper itself is MIT licensed.
- Its WebAssembly implementation (via the pure-Go `wazero` runtime) needs no
  CGo and no external binary; the compiled PDFium-as-WASM blob is
  `go:embed`ded directly into the app binary — fully offline, single
  cross-platform binary, consistent with how this app already embeds its
  frontend (`desktop/main.go`'s `all:frontend/dist`).
- Trade-off, accepted: this is the project's first real external dependency
  (everything built so far has been deliberately dependency-free), and it
  will meaningfully increase the compiled binary's size (a rough estimate
  based on comparable WASM-embedded PDFium builds is single-digit-to-low-
  double-digit megabytes; the implementation plan's first task should
  measure this concretely in this repo before the rest of the work
  proceeds, as a go/no-go checkpoint).

## Architecture

### New file: `internal/metadata/pdf_render.go`

- A lazily-initialized, package-level PDFium WASM instance (`sync.Once`-
  guarded) — WASM runtime startup has real cost, so it's created once and
  reused across every render call in the process's lifetime, not per book.
- `renderPDFPageAsCover(data []byte, pageNum int) (imageBytes []byte,
  contentType string, ok bool)`: opens the document, renders `pageNum`
  (1-based, matching this package's existing `pdfPage.number` convention)
  at a fixed DPI (150 — chosen to produce a reasonably sized cover image
  without being excessive; for this session's confirmed example, a
  504×661.5pt page renders to roughly 1050×1378px at 150 DPI), encodes the
  result as PNG, and returns it. `ok=false` (never a hard error) on any
  failure — corrupt PDF, unsupported PDFium feature, WASM panic recovered
  — matching this package's pervasive "a single book's extraction failure
  never fails the whole scan" convention.

### New heuristic, same file: `pageContentSuggestsCompositeCover`

- Given the already-decompressed content stream bytes for the page a
  candidate image was found on (reusing the decompression path
  `decodeFlatePDFImage`/`pdf_objects.go` already has for compressed
  streams), returns `true` if the stream contains a text-show operator —
  `Tj` or `TJ`, each preceded by whitespace so a false match inside some
  unrelated token isn't picked up — the exact signal already used by hand
  to diagnose this bug (page 1's stream has a `BT ... TJ ... ET` block
  showing "Chip Huyen" as vector text, separate from the image). Vector
  path/logo detection is deliberately left out of v1 — text is the
  strongest, simplest, already-proven-sufficient signal, and path-fill
  operators are common enough in decorative borders/backgrounds that
  including them risks false-positiving on plain single-image covers that
  merely have a colored border. A regex/textual check, consistent with the
  rest of this package's approach — not a full content-stream operator
  parser.

### Wiring into `findPDFCoverPageAware` (`pdf.go`)

Today: walks pages in order, returns the first qualifying image found
(`stopAtFirst=true`). New behavior: when that first qualifying image is
found on page N, before accepting it, run the heuristic against page N's
content stream. If it suggests a composite cover, call
`renderPDFPageAsCover(data, N)` and use *that* result instead (falling back
to the plain image bytes if rendering itself fails) — the page stays fixed
at N (the page order convention: whichever page the walk already decided
is "the cover page"), only the extraction method for that one page changes.

### `metadata.CoverExtractorVersion` bump

This changes what bytes a composite-cover book's next scan produces, so the
existing version-stamped cache (`internal/librarycache.Entry.CoverVersion`)
must treat every already-cached book as needing re-resolution once —
exactly the self-healing mechanism this session's earlier fix built, now
exercised for real by this change. Bump the constant; no other change
needed there.

## Testing

- `renderPDFPageAsCover` fixture tests: a small synthetic PDF (following
  this package's existing test-fixture convention — see
  `writeRealPDFFixture` in `internal/librarian/librarian_test.go` and
  similar helpers already in `internal/metadata`'s own test files) with a
  known page size renders to non-empty PNG bytes of a plausible size; a
  corrupt/malformed PDF returns `ok=false`, not an error.
- `pageContentSuggestsCompositeCover` unit tests: a content stream with
  only an image `Do` operator (no text) returns `false`; one with a
  trailing `BT...Tj...ET` block returns `true`.
- `findPDFCoverPageAware` integration test: a fixture PDF whose page 1 has
  both an image and a separate text block ends up calling the render path
  (verifiable via the same function-variable-seam pattern
  `internal/librarian`'s `extractFunc` already established, applied here to
  spy on whether `renderPDFPageAsCover` was invoked) and returns
  PNG-content-typed bytes rather than the raw JPEG.
- Manual verification (matching this session's established pattern for PDF
  work): drive `metadata.Extract` directly against the real "AI Engineering"
  file and confirm the returned `CoverContentType` is `image/png` (rendered)
  rather than `image/jpeg` (raw extracted), and that the resulting image
  visibly contains the title/author text this whole feature exists to
  recover.

## Non-goals

- Not replacing the fast, dependency-free image extraction as the default
  path — it stays the first thing tried for every book, per the "fallback
  only" scope decision.
- No new UI — this is a scan-time backend change; the existing "Choose
  cover" picker and its per-page candidate thumbnails are unaffected (they
  already call `metadata.ListPDFCoverCandidates`, a separate function from
  `findPDFCoverPageAware`, which this design does not modify — this could
  be revisited later, but is out of scope now, per the "fallback only"
  decision explicitly not extending to the picker).
- No configurable DPI or render-quality setting — 150 DPI is a fixed
  starting point; revisit only if real-world results show it's wrong.
