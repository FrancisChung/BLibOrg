# Design: In-app Undo for Apply batches

## Problem

`ConfirmApply`'s dialog claims a batch "can be undone later from the command
line," but no such command exists on `main` — `operations.Manager.UndoBatch`
is fully implemented and tested, but nothing in `appapi`, the Wails bindings,
or the frontend calls it. The only way to undo an Apply today is to move
files back by hand. `OperationsLogView.svelte` already lists every batch
(with `entryCount`/`undoneCount`/per-file `Undone` state) but is read-only.

## Goal

A single "Undo" button per batch row in the Operations Log view that reverses
every not-yet-undone entry in that batch, with a confirmation step first.

## Context / constraints

- Backend is already done: `operations.Manager.UndoBatch(batchID) error`
  (`internal/operations/manager.go:96`) reverses a batch's entries most-recent
  first, persists each entry's `Undone` flag immediately after it's reversed
  (so a crash or failure mid-undo is resumable — a retry only re-attempts
  entries still needing it), and is a no-op (returns `nil`) for an unknown
  batch ID or one that's already fully undone. Covered by
  `manager_test.go`'s `TestManager_ExecuteBatchThenUndoThenRedo`,
  `UndoBatchUnknownIDIsNoOp`, `UndoBatchTwiceIsIdempotent`,
  `UndoBatchRetryAfterPartialFailureRecovers`.
- **Redo is explicitly out of scope for this spec**, despite
  `Manager.RedoBatch` existing and being equally tested. Decided during
  brainstorming: ship Undo alone now; Redo is a cheap follow-up later since
  the backend already supports it and the UI pattern (a second button next to
  Undo) would be near-identical.
- **Undo is per-batch only** — the only granularity `UndoBatch` supports, and
  it matches Apply being the unit of action in the first place. No per-file
  undo.
- Explicitly decided against a shortcut button on the Scan & Review screen
  ("undo my last Apply") — one button, in the Operations Log view, undoing
  the whole batch, matching what's actually been tested against the backend
  so far.
- `UndoBatch` returns a single `error`, not a per-file result list (unlike
  `Apply`, which continues through every file and reports per-file
  ok/error/skipped). It stops at the first failing entry. This is fine
  because the log already persisted every entry reversed before the failure
  — re-fetching `ListOperationBatches()` after the call surfaces exactly what
  succeeded via the existing per-entry `Undone` pills, and the button stays
  clickable to retry (a retry only touches the entries still needing it).

## Decisions made during brainstorming

- Confirm before undoing: a native Yes/No dialog, same pattern as
  `ConfirmApply`, since without Redo yet an accidental Undo click has no
  built-in way back other than re-running Scan/Apply (which isn't guaranteed
  to reproduce the exact same destinations if rules or manual edits changed
  in between).
- One button, one location: per-batch row in `OperationsLogView.svelte`.
  Hidden once `undoneCount === entryCount` (nothing left to undo).

## Architecture

```
Svelte frontend
  lib/OperationsLogView.svelte  -- add Undo button per batch row
        |
        | Wails-generated bindings
        v
Go backend adapter (internal/appapi)
  UndoBatch(batchID string) error   -- new, internal/appapi/undo.go
        |
        v
internal/operations.Manager.UndoBatch (already exists, unmodified)

desktop/app.go (Wails-bound struct)
  UndoBatch(batchID string) error        -- new, delegates to a.api.UndoBatch
  ConfirmUndo(fileCount int) bool        -- new, native Yes/No dialog,
                                             structurally identical to
                                             ConfirmApply, reuses isAffirmative
```

## Data flow

- **`appapi.App.UndoBatch(batchID string) error`** (new, `internal/appapi/undo.go`):
  loads config, constructs `operations.NewLog(log_folder/ops.jsonl)` and
  `operations.NewManager(opsLog)`, calls `mgr.UndoBatch(batchID)`, returns its
  error directly. No new response type — mirrors the existing
  config/log-wiring pattern in `ListOperationBatches`/`Apply`.
- **`desktop.App.ConfirmUndo(fileCount int) bool`** (new): native
  `runtime.MessageDialog` with message `"Move {fileCount} file(s) back to
  their original location?"`, buttons `["Undo", "Cancel"]`, default button
  `"Cancel"`, returns `isAffirmative(result)` — same shape as `ConfirmApply`.
  Requires adding `"Undo"` to `isAffirmative`'s recognized-labels list
  (`desktop/app.go`) alongside `"Move files"`, or `ConfirmUndo` would repeat
  the exact bug just fixed for `ConfirmApply`: on macOS the custom label
  would never match, silently making confirmation always fail.
- **`desktop.App.UndoBatch(batchID string) error`** (new): thin delegate to
  `a.api.UndoBatch(batchID)`, same as every other bound method in `app.go`.
- **Frontend flow** in `OperationsLogView.svelte`: click Undo on a batch row
  → `ConfirmUndo(entryCount - undoneCount)` → if confirmed, call
  `UndoBatch(batchId)` → regardless of success or failure, re-call
  `ListOperationBatches()` and replace the local batch list, so the row's
  `Undone` pills/counts always reflect exactly what the backend actually did.

## Interaction details & error handling

- Undo button is visible per batch row only when `undoneCount < entryCount`.
- On `UndoBatch` error, show an inline error banner in the Operations Log
  view (same `.banner.error` visual pattern as `scanError`/`applyError` in
  `ScanReviewView.svelte`), e.g. `Undo failed: <message>`. The row's own
  counts (refreshed from the re-fetch) show how far it got; the button
  remains visible/clickable to retry the remainder.
- No loading spinner beyond disabling the clicked button while the call is
  in flight, consistent with existing `scanning`/`applying` boolean patterns.

## Testing / verification

- Go (`internal/appapi/undo_test.go`, TDD): `UndoBatch` on a batch with
  unreversed entries succeeds and the log reflects `Undone: true`; on an
  unknown batch ID is a no-op returning `nil`; propagates a manager error
  (e.g. destination path now occupied) without swallowing it.
- Go (`desktop/app_test.go`): extend `TestIsAffirmative`'s table with an
  `"Undo"` case expecting `true`, alongside the existing `"Move files"` case.
- Svelte (`OperationsLogView.test.ts`): Undo button hidden when
  `undoneCount === entryCount`; visible otherwise; clicking it calls
  `ConfirmUndo` then `UndoBatch` then re-fetches batches on both success and
  failure; an `UndoBatch` rejection renders the error banner.
- Manual verification: `wails build` (per the prior sidebar-logs-ui spec,
  `wails dev` has a known unrelated IPC issue — use a production build),
  Apply a batch, go to Operations, click Undo, confirm the dialog, verify the
  files move back and the row shows fully undone.

## Out of scope for this spec

- Redo UI (backend exists, deferred — noted above).
- Per-file (as opposed to per-batch) undo.
- An "undo my last Apply" shortcut on the Scan & Review screen.
- A shipped CLI undo command (the `ConfirmApply` dialog's "from the command
  line" claim is currently unbacked; not addressed here since this spec
  covers only the in-app UI).
