# Design: Sidebar shell + Operations/Warnings log views

## Problem

The app currently has one screen (Scan & Review) and no navigation chrome at all
— `App.svelte` renders the scan list directly. Two backend logs already exist and
are being written on every run (`internal/operations.Log` → `log_folder/ops.jsonl`,
and `internal/pipeline.LogCategoryWarnings` → `log_folder/category-warnings.jsonl`)
but neither is exposed anywhere in the UI — the only way to see them is to open the
JSONL files by hand. This became a real pain point during manual testing/debugging
this session, with no way to see what a scan or apply actually did from inside the
app.

## Goal

A persistent left sidebar (visually modeled on BookFusion's UI, which the user
wants the app's overall look to move toward) with three destinations: **Scan &
Review** (existing screen, unchanged behavior) and a **Logs** section containing
**Operations** and **Warnings** — each a new, read-only view over its respective
log file.

## Context / constraints

- Read-only for this round. `operations.Manager.UndoBatch`/`RedoBatch` already
  exist in the backend but are explicitly **out of scope** here — this spec is
  visibility only, not the full "Organiser & History" view from the original v1
  design spec.
- No changes to `internal/*` backend packages — `operations.Log.ReadAll()` and
  `operations.Log.ListBatches()` already provide everything needed for the
  Operations view; the category-warnings log has no reader yet and needs one
  (small addition, same file it's already written from).
- Visual direction confirmed via brainstorming-skill mockup: background
  `#F6F8FB`, card white, primary blue `#2F63E0`, success green `#22A35A`, warning
  amber `#C77A1F`, font Nunito (already bundled in the project — no new font
  asset/license needed).
- The user has said a **Catalogue** view is planned as a future top-level sidebar
  item alongside Scan & Review. Nothing in this spec builds it, but the sidebar
  component must be a plain list of items (not hardcoded to exactly two entries)
  so adding a new top-level item later is a one-line change, not a rework.

## Decisions made during brainstorming

- **Sidebar structure**: "Scan & Review" as a flat top-level item; "Logs" as a
  section label (not itself clickable/navigable) with "Operations" and
  "Warnings" indented underneath as the actual nav targets. Confirmed via
  mockup.
- **Operations view groups by batch**, not a flat per-file list — a batch of 16
  moved files should read as one row (batch ID, timestamp, file count), not 16
  near-identical rows. Individual file entries (old path → new path) are visible
  by expanding a batch.
- **Empty states are explicit.** Given this session's earlier bug (an empty scan
  result rendered as a blank screen indistinguishable from a broken button),
  both new views show "No operations yet" / "No warnings yet" text rather than
  nothing when their log is empty or missing.
- **App.svelte becomes a thin shell.** It currently does everything (scan state,
  card list, filters, apply). Adding two more views is the natural point to
  extract the existing body into its own `ScanReviewView.svelte`, since cramming
  three views' worth of state into one file would make it worse, not just bigger.

## Architecture

```
Svelte frontend (frontend/src/)
  App.svelte                  -- owns activeView state, renders Sidebar + the active view
  lib/Sidebar.svelte           -- nav list (Scan & Review / Logs > Operations, Warnings)
  lib/ScanReviewView.svelte    -- existing scan/filter/card-list/Apply body, moved as-is
  lib/OperationsLogView.svelte -- new: batch list, expandable to per-file entries
  lib/WarningsLogView.svelte   -- new: flat table of category-warning entries
  lib/BookCard.svelte, FilterBar.svelte, types.ts -- unchanged
        |
        | Wails-generated bindings
        v
Go backend adapter (internal/appapi, existing package)
  ListOperationBatches() ([]OperationBatchView, error)   -- new
  ListCategoryWarnings() ([]CategoryWarningView, error)  -- new
  Scan / Recompute / Apply / ConfigStatus                -- unchanged
        |
        v
internal/operations.Log (ReadAll, ListBatches -- both already exist, unmodified)
internal/pipeline -- new: ReadCategoryWarnings(path) ([]CategoryWarningEntry, error)
  (mirrors LogCategoryWarnings' JSONL format; that package already owns
  CategoryWarningEntry, so the reader belongs next to the writer)
```

## Data flow

- **`ListOperationBatches() ([]OperationBatchView, error)`** — loads config,
  opens `operations.NewLog(log_folder/ops.jsonl)`, calls `ListBatches()` for
  summaries and `ReadAll()` for entries, joins them by `BatchID` into one
  `OperationBatchView` per batch (`batchId`, `timestamp` of the batch's first
  entry, `entryCount`, `undoneCount`, `entries: []OperationEntryView` each with
  `oldPath`, `newPath`, `opType`, `undone`). Sorted newest-first. Missing log
  file → `[]`, not an error (matches `ReadAll`'s existing behavior).
- **`ListCategoryWarnings() ([]CategoryWarningView, error)`** — loads config,
  calls the new `pipeline.ReadCategoryWarnings(log_folder/category-warnings.jsonl)`,
  maps `[]pipeline.CategoryWarningEntry` to `[]CategoryWarningView`
  (`timestamp`, `sourcePath`, `category`, `subcategory`, `warning`) sorted
  newest-first. Missing file → `[]`, not an error.
- Both are called once when their sidebar item becomes active (not eagerly on
  app load), matching Scan's existing button-triggered-not-automatic precedent.

## Interaction details & error handling

- Sidebar selection is local Svelte state in `App.svelte` (`activeView`), no
  persistence across restarts needed for v1.
- Config-load failure (`ConfigStatus` banner) is shown regardless of active
  view, same as today — it's a startup-level concern, not per-view.
- A genuine read error (e.g. malformed JSONL line, permission error) shows an
  inline banner in that view only, same visual pattern as the existing
  `scanError` banner — the other views stay unaffected.
- Empty log → explicit "No operations yet" / "No warnings yet" message, per the
  brainstorming decision above.
- Operations view: clicking a batch row expands/collapses its entry list
  in-place (no navigation, no second API call — entries are already included in
  `OperationBatchView`).

## Testing / verification

- Go: `appapi` tests for `ListOperationBatches` (empty log, single batch,
  multiple batches, undone entries) and `ListCategoryWarnings` (empty file,
  populated file) — same table-test style as `scan_test.go`/`apply_test.go`.
  `pipeline` test for the new `ReadCategoryWarnings` (missing file → `nil`,
  populated file → parsed entries), mirroring `warnings_test.go`'s existing
  coverage of the writer.
- Svelte: component tests for `Sidebar` (clicking an item updates active
  state/emits the right event), `OperationsLogView` (empty state, batch
  rendering, expand/collapse), `WarningsLogView` (empty state, row rendering) —
  same Vitest + Testing Library setup as `BookCard.test.ts`.
- Manual verification: `wails build` (not `wails dev` — this session found a
  still-unresolved `wails dev`-specific IPC bug; production builds work), run
  the binary, Apply a batch, check it appears in the Operations view; trigger a
  category-warning rule mismatch, check it appears in the Warnings view.

## Out of scope for this spec

- Undo/Redo UI (buttons, batch selection for reversal) — backend already
  supports it, no UI yet.
- The planned Catalogue view — not built here, sidebar just designed to not
  block it.
- Config/rules editor UI.
- Duplicate-review UI.
- Fixing the underlying `wails dev` hang — tracked separately, worked around by
  using `wails build` for manual verification of this feature.
