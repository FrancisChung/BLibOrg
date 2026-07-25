# Design: parallel library scan with configurable concurrency

## Background

`internal/librarian.Scan` (the Library view's backend) currently processes
every book sequentially: one `for` loop, one book at a time -- stat, cache
check, `metadata.Extract` (parsing, image decoding, occasionally a PDFium
full-page render), cover write, cache update. For a cold scan (empty or
version-stale cache), this means the whole library's worth of extraction
work runs serially even though each book's extraction is independent of
every other book's.

## Goal

Parallelize the per-book work in `librarian.Scan`, with the concurrency
level controlled by a new config setting, so a cold scan of a large
library completes faster by using multiple CPU cores.

## What's actually shared (and what isn't)

Explored directly before designing, not assumed:

- **`internal/metadata`'s image/text decoding** (regex-based PDF/EPUB
  parsing, zlib/JPEG decoding): no shared mutable package-level state
  found (confirmed via a full grep of package-level `var` declarations)
  beyond what's listed below. Each `metadata.Extract` call reads its own
  file and operates on its own local data -- safe to call concurrently
  for different files today, with one exception:
- **PDFium** (`internal/metadata/pdf_render.go`): a single shared WASM
  instance for the whole process (`webassembly.Init(...MaxTotal: 1)`),
  reused by every `renderPDFPageAsCover` call -- **not** safe for
  concurrent use. Only triggered for a minority of PDFs (composite covers,
  or pages with an image filter like JPXDecode that this package's other
  decoders can't handle).
- **`internal/covercache`**: no shared package-level state. Every write
  goes to a filename derived from the book's own source path, so
  concurrent writes for different books touch different files.
  `GetOverride` re-reads `cover-overrides.json` fresh on every call (no
  in-memory cache to race on).
- **`internal/librarycache.Cache`**: a plain, non-thread-safe struct
  wrapping a `map[string]Entry`. `Fresh` (read) and `Put` (write) both
  need external synchronization if called from multiple goroutines --
  Go's map type is not safe for concurrent access when at least one
  accessor writes, even to different keys.
- **`librarian.Scan`'s own local state**: `seen` (a set of every scanned
  path, used at the end for `cache.Keep`) and `books` (the returned
  slice, in `paths` order).

## Design

### Where the PDFium fix lives

The single-instance constraint is `internal/metadata`'s own problem, not
`internal/librarian`'s -- `librarian.Scan` calls `metadata.Extract` as one
opaque function; it has no way to serialize "just the PDFium part" of a
call it doesn't control the internals of. Fix: add a package-level
`sync.Mutex` in `pdf_render.go`, held for `renderPDFPageAsCover`'s full
body (instance acquisition through document close). This makes
`metadata.Extract` a documented "safe to call concurrently" function as a
package-level guarantee -- callers never need to know PDFium exists.
Only the minority of books that actually trigger a render will ever
contend on this lock.

### Config setting

A new `ScanConcurrency int` field on `config.General` (yaml key
`scan_concurrency`), alongside the existing `PDFCoverPageLimit`. `0` (the
Go zero value, and what an unset key in the user's `config.yaml`
unmarshals to) means "use `runtime.NumCPU()`" -- resolved at the point of
use inside `librarian.Scan`, mirroring `PDFCoverPageLimit`'s existing
"`<= 0` means use the built-in default" convention (`walkPDFPageTree`
already does exactly this). No config-loading-time validation or
patching needed, consistent with how `PDFCoverPageLimit` is handled
today.

### `librarian.Scan`'s restructuring

- `seen` is built directly from `paths` before the loop starts (it was
  always just "every path about to be processed" -- it never needed to
  be built incrementally inside the loop, so this removes a synchronization
  concern rather than needing to add one).
- The current loop body (stat, cache-check, extract, override-check,
  cover-write, cache-put) is extracted into a helper function taking the
  single `path` plus everything else it needs (`cfg`, `forceRefresh`,
  a pointer to the shared `cache` and its guarding mutex), returning one
  `Book`. This is the unit of work each goroutine runs.
- A bounded worker pool -- a buffered channel of size `concurrency` used
  as a semaphore, plus a `sync.WaitGroup` -- runs that helper once per
  path, all from the standard library (no new dependency). The semaphore
  naturally throttles goroutine creation too: the main loop blocks on the
  channel send once `concurrency` workers are already in flight, rather
  than spawning all `len(paths)` goroutines upfront.
- `librarycache.Cache` access (`Fresh` and `Put`) is wrapped in a
  `sync.Mutex`, held only around each individual cache call (not around
  extraction) -- cheap in-memory map operations, so contention should be
  negligible even at high concurrency.
- `books` becomes a pre-sized slice (`make([]Book, len(paths))`) with each
  worker writing to its own index (`books[i]`, matching its path's
  position in `paths`) -- preserves today's deterministic path-order
  output with no synchronization needed for the writes themselves, since
  each goroutine owns a distinct, non-aliased index.
- No error-propagation or cancellation logic is introduced: the existing
  design already tolerates one book's extraction failing without
  affecting any other book or aborting the scan (a failed
  `metadata.Extract` call still produces a `Book` with empty fields
  rather than being dropped) -- that property carries over unchanged, so
  there's no need for `errgroup`-style first-error-wins semantics.
- `Scan`'s public signature (`Scan(cfg config.Config, forceRefresh bool) ([]Book, error)`)
  does not change -- this is purely an internal implementation change plus
  one new config field, so no caller (`internal/appapi`, `desktop/app.go`)
  needs updating.

## Testing

- **PDFium mutex**: a same-package test (`internal/metadata/pdf_render_test.go`,
  which already has access to the unexported `pdfiumMu` mutex) that spawns
  several goroutines each locking `pdfiumMu`, incrementing a shared
  counter, asserting the counter never exceeds 1 while the lock is held,
  then decrementing and unlocking -- proving the mutex itself correctly
  serializes access, without needing slow real PDFium/WASM render calls
  to exercise the property actually being tested (that overlapping
  callers never hold the lock simultaneously).
- **`librarian.Scan` concurrency**: tests using the existing
  `extractFunc` mocking seam, run with multiple books and
  `ScanConcurrency` set to a value `> 1`, confirming: (a) all books still
  get a correct `Book` entry, in the correct `paths` order, (b) the
  cache ends up with a correct entry for every extracted book (no lost
  updates from a race), (c) `ScanConcurrency: 0` (or unset) resolves to
  `runtime.NumCPU()` -- observable via, e.g., asserting scan behavior is
  unaffected by leaving the field unset (falls back to today's behavior,
  just parallel now) rather than asserting the literal number to keep
  the test unaffected by which machine it runs on.
- **Race detector**: `go test -race ./internal/librarian/... ./internal/metadata/...` as part of this plan's own verification, given this is inherently a concurrency change -- the existing test suite already exercises `librarian.Scan` and `metadata.Extract` extensively, so running it under `-race` is a strong, cheap check beyond hand-written concurrency-specific tests.
- **End-to-end**: manual verification against the real library (hundreds
  of books) isn't practical to commit as an automated test, but is worth
  a manual timing comparison (`ScanConcurrency: 1` vs. unset) after
  implementation, to confirm the parallelization actually helps in
  practice and not just in theory.

## Non-goals

- No change to `internal/pipeline.Run` (the separate Scan & Review
  workflow) -- this plan is scoped to `internal/librarian.Scan` (the
  Library view) only, per the original request.
- No attempt to parallelize `scanner.Scan` (the directory walk that
  produces `paths`) -- that's a fast filesystem walk, not the bottleneck
  this design targets.
- No new external dependency (e.g. `golang.org/x/sync/errgroup`) -- the
  standard library's `sync.WaitGroup` plus a buffered channel as a
  semaphore is sufficient for this workload's scale (a personal book
  library, not a distributed system).
- No attempt to tune or benchmark the `runtime.NumCPU()` default against
  real hardware/disk characteristics as part of this plan -- it's a
  reasoned starting point, explicitly configurable so it can be adjusted
  later without a code change.
