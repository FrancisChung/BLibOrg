# Design: UI — Scan & Review (Views 1+2 merged)

## Problem

The backend pipeline (`internal/pipeline`, `internal/operations`, etc.) is complete,
tested, and verified against the real library — see
`docs/superpowers/plans/2026-07-08-backend-pipeline.md`. There is no way to use it
yet except the throwaway `cmd/manualtest` / `cmd/debugmeta` CLIs. The v1 design
(`docs/superpowers/specs/2026-07-08-book-organiser-design.md`) calls for a Wails
desktop UI with three views; this spec covers the first two — Scan & Review (View 1)
and Destination Preview (View 2) — merged into a single screen, since hands-on
inspection of the `book.Book` struct showed both views render off the same record.
View 3 (config editor, operation history/undo UI, duplicate-review UI) is explicitly
deferred to a later spec.

## Goal

A Wails desktop app that: loads config, scans the working folder via the existing
backend, shows every book as an editable card (current filename, editable
title/author/year, computed destination path, status), and applies the batch of
moves/renames on request — using the backend's existing `operations.Manager` so
undo/redo data is recorded even though there's no UI for it yet.

## Context / constraints

- Backend is done and not to be modified by this work except where `app.go` needs a
  small adapter — no changes to `internal/*` business logic.
- Config editing (View 3) is out of scope. Config is a hand-edited YAML file at a
  fixed OS-standard path; this app only reads it.
- Operation history / Undo / Redo UI is out of scope this round, even though the
  backend (`operations.Manager.UndoBatch`/`RedoBatch`) already supports it.
- Duplicate-review UI (opening/revealing files, deciding what to do with dupes) is
  out of scope this round — duplicates are shown as a badge only.
- Must remain buildable as a single small cross-platform binary (Wails constraint
  already established in the v1 design) — Windows (WebView2) and Linux (WebKitGTK).

## Decisions made during brainstorming

- **Views 1+2 are one screen**, not two. Every book is a compact card: an
  always-editable row of Title/Author/Year inputs, the struck-through original
  filename above it, the computed destination path below it, and a status pill.
  One "Apply" button processes the whole visible batch. (Confirmed via mockup —
  see "Option C" / "Option A" choices below.)
- **Editing is always-editable inputs** (spreadsheet-style), not click-to-edit.
  Every Title/Author/Year field is a live text input at all times.
- **Frontend stack: Svelte + TypeScript**, via Wails' `svelte-ts` template. Chosen
  over vanilla-ts (less structure for a stateful editable-card list) and React
  (heavier dependency tree) — Svelte compiles away at build time, keeping the
  shipped bundle close to vanilla's size while giving component/state ergonomics.
- **Config: fixed path, hand-edited YAML.** No config UI, no first-run picker.
  `os.UserConfigDir()/book-organiser/config.yaml` (Windows:
  `%AppData%\book-organiser\config.yaml`; Linux:
  `~/.config/book-organiser/config.yaml`). If absent, the app shows the resolved
  path in an error banner rather than scaffolding a default (a default config
  without real folder/category input isn't meaningful).
- **Scan is button-triggered**, not automatic on launch — opening the app never
  surprises the user with a rescan of a large folder.

## Architecture

```
Svelte frontend (frontend/src/)
  App.svelte          -- search/filter bar, card list, Apply button, error banners
  lib/BookCard.svelte  -- one book: editable fields, old-name, dest-path, status pill
  lib/FilterBar.svelte -- search input + status filter chips
  lib/types.ts          -- BookView type (mirrors the Go DTO)
        |
        | Wails-generated bindings (wailsjs/, auto-generated — not hand-written)
        v
Go backend adapter (app.go, new — Wails-bound struct)
  Scan() ([]BookView, error)
  Recompute(edited BookView) (BookView, error)
  Apply(books []BookView) (ApplyResult, error)
  ConfigStatus() (ConfigStatusView, error)
        |
        v
Existing internal/ packages (unmodified): config, pipeline, categorizer, rename,
operations, book
```

`app.go` is a thin adapter layer only: DTO conversion in and out, no business logic.
All path-building, categorization, and file-operation logic stays in `internal/`.

## Data flow — the four bound methods

- **`Scan() ([]BookView, error)`** — `config.Load(fixedPath)` →
  `pipeline.Run(cfg)` → convert `[]*book.Book` to `[]BookView` (flat,
  JSON-serializable: id, sourcePath, oldFilename, title/author/year values +
  their per-field source, status string, destPath, duplicate status + group id,
  size). Triggered by an explicit "Scan" button.
- **`Recompute(edited BookView) (BookView, error)`** — called on every field edit
  (frontend debounces, e.g. 300ms after the last keystroke). Converts the DTO back
  to `*book.Book`, marks whichever of Title/Author/Year changed as
  `book.SourceManual`, re-runs `categorizer.Categorize` + `rename.BuildPath`
  server-side, converts back to `BookView` with the updated `DestPath`/status.
  Keeps all path-building logic server-side; the frontend never reimplements it.
- **`Apply(books []BookView) (ApplyResult, error)`** — filters out rows whose
  status is `Unresolved` (Title never resolved — matches the backend's existing
  Apply-eligibility rule from `Book.Status()`). Converts the remainder back to
  `*book.Book`, builds one `operations.NewMoveCommand(batchID, oldPath, newPath)`
  per file, obtains a fresh batch ID via `operations.NextBatchID(log)`, runs
  `Manager.ExecuteBatch(batchID, commands)`. `ApplyResult` is per-file:
  `{sourcePath, ok bool, error string, skipped bool}`, so the UI can annotate each
  card individually rather than showing one aggregate result.
- **`ConfigStatus() (ConfigStatusView, error)`** — returns the resolved config
  path and, if loading failed, a human-readable reason. Called once on app
  startup so the UI can show "no config found at `<path>`" instead of a blank
  screen when there's nothing to scan yet.

## Interaction details & error handling

- **Apply confirmation**: a native confirm dialog (Wails' `runtime.MessageDialog`)
  summarizing the action — "Move N files into `<library_folder>`?" — since this
  is the one hard-to-reverse action in the flow.
- **Per-row Apply results**: after `Apply` returns, each card's status pill
  updates to reflect its `ApplyResult` entry — success (dimmed/collapsed, kept
  visible as a record of what happened), error (message shown inline on that
  card), or skipped (for rows excluded as `Unresolved`).
- **Scan errors**: `pipeline.Run` failing outright (e.g. missing working folder)
  shows a single error banner in place of the card list, not a broken/partial
  render.
- **Recompute failures**: on error, the card keeps its last-known-good
  `DestPath` and shows a small inline warning icon rather than blanking the
  field.
- **Live filtering**: search box (title/author/old-filename substring) and status
  filter chips (`All` / `Needs review` / `Duplicates` / `Heuristic` / `Metadata`)
  are pure frontend filters over the already-scanned list — no additional Go
  calls, consistent with how the earlier library-scan report behaved.

## Testing / verification

- Go: unit tests for `app.go`'s DTO conversions, `Recompute`'s field-marking
  logic (edited field → `SourceManual`, others untouched), and `Apply`'s
  filtering of `Unresolved` rows before building commands — same `go test`
  pattern as the rest of the backend.
- Svelte: component tests for `BookCard` (renders fields correctly, edit
  triggers the debounced `Recompute` call, status pill maps status → pill
  variant correctly) using Vitest (ships with Wails' `svelte-ts` template).
- Manual verification: `wails dev` against `.manual-test/` (existing fixture
  folder used by `cmd/manualtest`), covering scan → edit a `Partial` row →
  Apply → confirm the file actually moved on disk.

## Out of scope for this spec

- View 3: config editor UI, operation history + Undo/Redo UI, duplicate-review
  UI (opening/revealing files).
- Any change to `internal/*` backend packages.
- Automatic/on-launch scanning.
- First-run config creation or a config file picker.
