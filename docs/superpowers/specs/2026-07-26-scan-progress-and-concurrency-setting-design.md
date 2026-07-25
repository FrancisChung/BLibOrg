# Design: library-scan progress reporting and a Settings concurrency control

## Background

Two gaps left over from the parallel-scan work
([2026-07-25-parallel-library-scan-design.md](2026-07-25-parallel-library-scan-design.md)):

1. `LibraryView.svelte`'s loading state is a static `<p>Loading library…</p>`.
   `ListLibrary` is a single blocking Wails call that returns nothing until
   the whole scan finishes, so the frontend has no way to show elapsed time
   or how many books are done.
2. `config.General.ScanConcurrency` can only be set by hand-editing
   `config.yaml`. `SettingsView.svelte` has no config-editing UI at all
   today (its only control is "Reset cover cache").

This design covers both, since the second reuses the same "loading a
number into a Settings input" pattern regardless of the first, and neither
touches the other's code.

## Goal

- Show elapsed time and a "`<done>` / `<total>` books" counter in the
  Library view's loading banner during a refresh.
- Let the user view and set the scan concurrency from Settings, pre-filled
  with the currently-effective value (configured override, or the
  detected core count if unset).

## Part 1: scan progress

### Data flow

`librarian.Scan` gains a new parameter:

```go
func Scan(cfg config.Config, forceRefresh bool, onProgress func(done, total int)) ([]Book, error)
```

`onProgress` is nil-safe (existing/test callers pass `nil`) and is called
once per book as workers finish -- cache hit or fresh extraction both
count, per the earlier decision that the counter should reflect every
book, not just cache misses. `total` is `len(paths)`, known before the
worker pool starts. The increment-and-call is done under a single mutex
(reusing the existing `cacheMu` is tempting but wrong -- cache access and
progress reporting are unrelated critical sections; this gets its own
`progressMu`) so that, even though extraction itself stays fully
concurrent, `done` values are strictly increasing in the order
`onProgress` is invoked. Without this, two workers could race between
"increment" and "invoke callback," letting the frontend observe `done`
values arrive out of order -- e.g. 5 then 4 -- which is harmless (the
counter self-corrects on the next event) but needlessly confusing to see.

`appapi.App.ListLibrary` gains the same parameter and threads it straight
through to `librarian.Scan` -- no logic of its own, matching the package's
existing "pure Go, no Wails import" boundary.

`desktop/app.go`'s Wails-bound `ListLibrary` (the only place in the
codebase that holds the Wails `ctx`) is the only place that knows an event
exists. It builds a closure:

```go
func(done, total int) {
    runtime.EventsEmit(a.ctx, "library:scan-progress", ProgressPayload{Done: done, Total: total})
}
```

and passes it to `a.api.ListLibrary(forceRefresh, onProgress)`. This is
the app's first use of Wails' event-emission mechanism; nothing else in
the codebase currently emits or listens for a Wails event.

### Frontend

`LibraryView.svelte`'s `load()`:

- On entry: reset `elapsedSeconds = 0` and `progress = null`; start a
  `setInterval` ticking `elapsedSeconds` once per second; subscribe with
  `EventsOn('library:scan-progress', (p) => { progress = p })`.
- In `finally` (covers both success and error paths): clear the interval
  and call `EventsOff('library:scan-progress')`.
- Banner text: while `progress` is still `null` (no event has landed yet),
  show elapsed time alone; once the first event arrives, show
  `"Loading library… <done> / <total> books · <elapsed>"`. Elapsed
  renders as `Ns` under a minute, `Mm Ss` at or above 60s.

No special-casing for a fast, all-cache-hit scan: the banner will just
flash briefly through a few progress updates, which is fine.

## Part 2: Settings concurrency control

### Backend

Two new `appapi.App` methods:

```go
func (a *App) GetScanConcurrency() (ScanConcurrencyView, error)
func (a *App) SetScanConcurrency(n int) error
```

```go
type ScanConcurrencyView struct {
    Configured int `json:"configured"` // raw cfg.General.ScanConcurrency; 0 means unset
    Detected   int `json:"detected"`   // runtime.NumCPU()
}
```

`SetScanConcurrency` rejects `n < 0` with a plain validation error, then
does a `config.Load` → mutate `General.ScanConcurrency` → `config.Save`
round trip -- the full-rewrite approach, per your call: simpler than a
targeted text edit, at the accepted cost that `config.Save`'s
`yaml.Marshal` round trip will strip comments and may reorder map keys
(`Categories`) the next time any setting is saved. This is a pre-existing
property of `config.Save` (already used by nothing today, since this is
the first Settings control to write config back), not something new this
design introduces.

### Frontend

`SettingsView.svelte` gets a new section, styled like the existing
cover-cache block:

- On mount, calls `GetScanConcurrency` and pre-fills a number input with
  `configured` if `> 0`, else `detected` (so the field always shows a
  concrete number, never a blank/zero that reads as "unset").
- A "Save" button calls `SetScanConcurrency(value)`. `0` (or clearing the
  field back to blank, which submits as `0`) explicitly means "auto" --
  there's no separate reset control; blanking the field is the reset.
- Success/error banners match the cover-cache section's existing pattern.

No confirmation dialog (unlike cover-cache reset) -- this is a
non-destructive, instantly-reversible numeric setting.

## Error handling

- `SetScanConcurrency` validation and `config.Load`/`Save` failures
  surface through `SettingsView`'s existing error-banner pattern.
- Nothing on the progress path is fatal: a missed event just leaves the
  counter stale for a moment until the next one arrives; elapsed time
  keeps ticking regardless of whether any progress event ever lands.

## Testing

- **`librarian.Scan`**: a progress-callback test asserting `onProgress` is
  called exactly `len(paths)` times, with strictly increasing `done`
  (1..N) and constant `total`, run with `ScanConcurrency > 1` under
  `-race` -- this is the concurrency-sensitive piece, same rigor as the
  parallel-scan work itself.
- **`appapi`**: round-trip tests for `GetScanConcurrency` (configured
  value vs. falling back to `detected` when unset) and
  `SetScanConcurrency` (persists, rejects negative, propagates a
  Load/Save failure).
- **Frontend**: `LibraryView.test.ts` -- elapsed timer ticks and is
  cleaned up when loading finishes (both success and error paths), a
  `library:scan-progress` event updates the displayed counter, banner
  text matches the two formats (elapsed-only vs. elapsed+counter).
  `SettingsView.test.ts` -- field pre-fill from both branches of
  `GetScanConcurrency`, save call, success/error banner.

## Non-goals

- No change to `internal/pipeline.Run` (Scan & Review, the working-folder
  flow) -- both parts of this design are scoped to the Library view and
  its Settings, matching how the parallel-scan work itself was scoped.
- No targeted/comment-preserving config file edit -- explicitly decided
  against in favor of reusing `config.Save` as-is.
- No cap on the concurrency value beyond `>= 0` -- a user who types 200 on
  an 8-core machine gets 200 goroutines contending for 8 cores, which is
  wasteful but not unsafe (the existing semaphore-bounded worker pool
  handles any positive value correctly); not worth a hardcoded ceiling.
- No progress reporting for the working-folder Scan & Review flow --
  scoped to the Library view's refresh only, per the original request.
