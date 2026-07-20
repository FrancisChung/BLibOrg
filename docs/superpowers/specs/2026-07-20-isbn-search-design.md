# ISBN Search (narrow scope) — Design

## Background

`docs/superpowers/specs/2026-07-20-isbn-book-search.md` asked whether the ISBN
search functionality from [ebook-tools](https://github.com/na--/ebook-tools)
(a bash pipeline wrapping calibre, 7z, Tesseract, poppler, etc. to extract
ISBNs and fetch metadata from Goodreads/Amazon/Google Books) was viable to
port into book-organiser.

Conclusion from that investigation: porting the full pipeline is not viable —
book-organiser is a dependency-free, offline, pure-Go extractor
(`internal/metadata`), and ebook-tools is fundamentally a wrapper around five
external binaries plus scraping-based sources. The viable, narrow slice is:

1. A native Go ISBN scanner added to the existing format extractors (no new
   dependencies).
2. An optional, opt-in lookup against the Google Books API (official,
   free, no scraping) as a new metadata source.

Explicitly out of scope: calibre, 7z, Tesseract/OCR, Amazon/Goodreads
scraping, and (per a scoping decision below) PDF body-text extraction.

## Goals

- Detect ISBNs already present in a book's structured metadata or filename,
  with no new runtime dependencies.
- Let the user explicitly trigger an online lookup (Google Books) to fill in
  Title/Author/Year for books that are still `Unresolved`/`Partial` after the
  normal offline scan.
- Keep the feature fully opt-in and clearly decoupled from the existing
  scan pipeline, so it can later be wired to run automatically during scan
  (behind the same config flag) without a redesign.

## Non-goals

- OCR or any scanned-image text extraction.
- PDF body-text extraction (decompressing content streams / parsing text-show
  operators) to find ISBNs on copyright pages. PDFs have no structured ISBN
  metadata field (unlike EPUB/MOBI), and ebook-tools itself only finds PDF
  ISBNs by converting to text via `pdftotext`/calibre and falling back to
  Tesseract OCR — real new capability, not a structured-field read. Adding
  this would also revisit the PDF extractor's existing documented constraint
  of deliberately not being a full PDF parser (see `internal/metadata/pdf.go`
  `extractPDF` doc comment). PDFs are only covered here via filename-regex
  ISBN detection.
- Scraped sources (Amazon, Goodreads) — Google Books has an official free API
  and needs no HTML scraping.
- Automatic/silent lookup during scan. The action is explicit and
  user-triggered for this iteration (see Trigger model below), though the
  design keeps this swappable later.

## Trigger model

Google Books' API has no multi-ISBN batch endpoint — every lookup is one HTTP
call per ISBN regardless of trigger model. The choice is *when* those calls
fire, not how many.

This design uses an **explicit batch action**: after a normal (fully offline,
unchanged) scan, the user can trigger a "Resolve via ISBN" action that fires
one Google Books call for each row still `Unresolved`/`Partial` that has a
detectable ISBN. Scan itself stays exactly as fast and offline as it is
today; the network cost is visible and consciously initiated.

The orchestration logic (`pipeline.ResolveViaISBN`, below) is intentionally
decoupled from this trigger — it takes a `[]*book.Book` and a `config.Config`
and has no knowledge of *why* it was called. If usage after real-world testing
shows the explicit-action model is more friction than it's worth, switching to
"automatic during scan (if enabled)" is a matter of calling the same function
from `pipeline.Run()` behind `cfg.ISBNLookup.Enabled`, not a rework.

## Architecture & components

| File | Change |
|---|---|
| `internal/textutil/isbn.go` (new) | `ExtractISBN(s string) (isbn string, ok bool)` — regex over arbitrary text/filename, ISBN-10 and ISBN-13 check-digit validation, blacklist for obviously-junk numbers (e.g. `0123456789`), normalizes ISBN-10 hits to ISBN-13 for a consistent lookup key. |
| `internal/metadata/epub.go` | `epubPackage.Metadata` gains `Identifier []string` (`xml:"identifier"`, captures every `<dc:identifier>` value in the OPF); `extractEpub` runs each through `textutil.ExtractISBN`, first hit wins. |
| `internal/metadata/mobi.go` | EXTH switch gains `case 104:` (the ISBN EXTH record), run through `textutil.ExtractISBN`. |
| `internal/metadata/result.go` | `Result` gains `ISBN string`. |
| `internal/metadata/pdf.go` | Unchanged — no structured ISBN field exists in PDFs, and body-text scanning is out of scope (see Non-goals). |
| `internal/googlebooks/googlebooks.go` (new package) | `Lookup(isbn string) (Result, error)` where `Result{Title, Author, Year string}`. One HTTP GET to `https://www.googleapis.com/books/v1/volumes?q=isbn:<isbn>`, ~10s timeout, parses `items[0].volumeInfo`. No API key required (public quota); an optional key (for a higher rate limit) is read from config and appended as a query param if set. |
| `internal/pipeline/isbnresolve.go` (new) | `ResolveViaISBN(books []*book.Book, cfg config.Config) ISBNResolveSummary` — batch orchestration, detailed below. |
| `internal/config/config.go` | New `ISBNLookup struct { Enabled bool; APIKey string }` (`yaml:"isbn_lookup"`), gates the whole feature. Defaults to `Enabled: false` (zero value), so existing config files keep working unchanged. |
| `internal/appapi/isbn.go` (new) | `func (a *App) ResolveViaISBN(books []BookView) (ISBNResolveResultView, error)` — Wails-bound entry point, mirrors `Recompute`'s `viewToBook`/`bookToView` round-trip pattern. Returns an error immediately if `cfg.ISBNLookup.Enabled == false` (defense in depth; the frontend is expected to keep the action hidden/disabled in that case, not rely on this error for UX). |

**Why a separate `internal/googlebooks` package rather than folding it into
`internal/metadata`:** every existing extractor in `metadata` is local/offline
and unit-testable against fixture files, no network involved. Keeping the one
networked piece in its own package preserves that property — `metadata` stays
pure/offline, `googlebooks` is the only package that needs an `http.Client`
and mock-server-based tests.

**Why no changes to `Book`/`BookView`:** the alternative (persisting the
detected ISBN on `Book` at scan time) would save a second, cheap
header/OPF-only re-parse of the file at batch-action time, but at the cost of
either a stateful side-map that must be kept in sync with the books slice
across re-scans, or a new field. Given the re-parse is a lightweight
structured-metadata read (not full text or OCR), the simpler, fully decoupled
option — re-derive on demand, touch nothing else — is preferred.

## Data flow: `pipeline.ResolveViaISBN`

```go
type ISBNResolveSummary struct {
    Attempted int // rows with a locally-found ISBN that we queried
    Resolved  int // rows where at least one field got filled
    NotFound  int // ISBN found, but Google Books had no match / lookup failed
    NoISBN    int // no ISBN found locally (structured field or filename) -- skipped
}

func ResolveViaISBN(books []*book.Book, cfg config.Config) ISBNResolveSummary
```

1. **Gate**: if `!cfg.ISBNLookup.Enabled`, return a zero-value
   `ISBNResolveSummary` immediately and make no lookups.
2. For each `b` in `books` where `b.Status()` is `SourceUnresolved` or
   `SourcePartial`:
   - Re-derive ISBN: `metadata.Extract(b.SourcePath, ...)`.ISBN, falling back
     to `textutil.ExtractISBN(filepath.Base(b.SourcePath))` if empty. Neither
     found → `NoISBN++`, continue to the next book (no network call).
   - `Attempted++`. Call `googlebooks.Lookup(isbn)` — one sequential call, not
     concurrent (batch sizes here are personal libraries, tens/low-hundreds of
     unresolved rows at most; sequential keeps the "one call per book" model
     simple). Error, timeout, or zero results → `NotFound++`, continue.
   - Fill only the gaps, the same pattern `pipeline.Run()` already uses for
     embedded metadata:
     ```go
     if b.Title.Value == "" && res.Title != "" {
         b.Title = book.Field{Value: res.Title, Source: book.SourceMetadata}
     }
     // same for Author, Year
     ```
     `SourceMetadata` (not a new Source tier) is used deliberately: a Google
     Books record is real bibliographic data describing the book itself, the
     same category as embedded epub/pdf/mobi metadata — conceptually
     distinct from `SourceHeuristic`, which means "guessed from the
     filename." It also only ever fires as a last resort (rows still
     unresolved after both metadata extraction and heuristics), so precedence
     against `Heuristic` never actually matters in practice. If at least one
     field got filled, `Resolved++`.
3. After the loop, re-run `categorizer.Categorize` and `rename.BuildPath` for
   every touched book (mirrors `appapi.Recompute`), then
   `disambiguateDestPaths(books)` and `duplicates.Detect(books)` over the
   **whole** slice, not just touched rows — newly-filled Title/Author values
   can create fresh `DestPath` collisions or duplicate matches against
   untouched books, the same reasoning `pipeline.Run()` already documents for
   running those two steps last.
4. Return the summary. `books` is mutated in place; `appapi.ResolveViaISBN`
   converts it back to `[]BookView` for the frontend to re-render, alongside
   the summary for a UI toast such as "Resolved 4 of 7 (2 had no ISBN, 1
   not found)."

## Error handling

| Failure | Handling |
|---|---|
| `cfg.ISBNLookup.Enabled == false` | `appapi.ResolveViaISBN` returns an error immediately; frontend keeps the action hidden/disabled, so this is defense in depth, not the primary UX gate. |
| No ISBN locally found | Counted `NoISBN`, row untouched, no network call made. |
| Network error / timeout / non-200 | Counted `NotFound`, row untouched, loop continues — one bad request never aborts the batch. |
| Google Books returns 0 items | Counted `NotFound`, same as above. |
| Google Books returns a match with some empty fields (e.g. no `publishedDate`) | Handled by the existing "only fill if non-empty" per-field guard — no special case needed. |
| Rate limit (HTTP 429) | Treated as any other request error for this iteration — counted `NotFound`, loop continues. No retry/backoff (YAGNI unless real-world usage shows it's needed, given personal-library batch sizes). |

## Testing

- `textutil/isbn_test.go`: table-driven — valid ISBN-10/13 (with/without
  hyphens), invalid check digits rejected, blacklist junk rejected, ISBN-10→13
  normalization, no-match cases.
- `metadata/epub_test.go` / `mobi_test.go`: extend existing fixture-file tests
  with an EPUB `<dc:identifier>` ISBN and a MOBI EXTH 104 case; assert
  `Result.ISBN` populated, and that existing ISBN-less fixtures still pass
  with `Result.ISBN == ""`.
- `googlebooks/googlebooks_test.go`: `httptest.Server` stubbing the Google
  Books JSON response shape — full match, partial `volumeInfo`, zero `items`,
  non-200 status, malformed JSON. No real network calls in tests.
- `pipeline/isbnresolve_test.go`: constructs a mixed-status `[]*book.Book`
  with a fake lookup (function-var seam, same style as `warnings.go`'s
  `nowFunc`) to avoid real HTTP; asserts fill-only-if-empty behavior,
  `ISBNResolveSummary` counts, and that disambiguation/duplicate-detection
  re-run over the full batch.
- `appapi/isbn_test.go`: `Enabled: false` returns an error and makes no
  lookup calls; `Enabled: true` round-trips `BookView` correctly.

No UI/manual browser testing is in scope for this design doc — that happens
during implementation.
