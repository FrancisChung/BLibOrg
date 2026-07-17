# Design: UI Enhancements

## Feature Request

1) Can we have a checkbox against each item in the Scan results. Only checked box should be moved. Check all entries by default when returned by scan
2) Can we have a little "< >" button between the title the author field in the scan results. The button will exchange the values of the Author field and Title field with each other.
3) Can we have a file link on the original file name that is displayed. double clicking on the link should open up the file

## Context / constraints

1) Currently the apply button is applied to all entries, but testing has revealed that it is not always desirable
2) I have found on occasions that the author field is in the title field and vice versa
3) There has been numerous times where I needed to open the original file, and it would be great to be able to do it from app

## Current architecture (relevant pieces)

- `ScanReviewView.svelte` holds `books: BookView[]` and derives
  `visibleBooks` by filtering `books` against the search query and status
  filter (`FilterBar`). `doApply()` currently sends `visibleBooks` — the
  full filtered set, no per-item selection — to `Apply()`. Books with
  `status === 'Unresolved'` are still included in that payload; the backend
  (`appapi.App.Apply`) skips them server-side and reports them as `skipped:
  true` in the result, rather than the frontend excluding them itself.
- `BookCard.svelte` renders one book: old filename (plain text), editable
  title/author/year inputs (debounced 300ms, dispatching an `edited` event
  on change), computed dest path, and status/duplicate badges.
- Edits flow: `BookCard` dispatches `edited` with the full updated
  `BookView` → `ScanReviewView.onEdited` calls `Recompute(edited)` → the
  backend recomputes `Status`/`DestPath` from the given title/author/year →
  the returned `BookView` replaces the old one in `books` by `sourcePath`.
  This is the single path by which any field edit (typed or otherwise)
  updates a card's computed state.
- `desktop/app.go` has two existing Wails-bound methods that talk to the
  native OS layer directly, bypassing `appapi` entirely because they don't
  touch config/books/operations: `ConfirmApply` and `ConfirmUndo`, both
  wrapping `runtime.MessageDialog`.

## Feature 1: Checkbox selection

**State:** `ScanReviewView.svelte` gains `checked: Record<string, boolean>`
keyed by `sourcePath`, following the same keyed-by-sourcePath pattern
already used for `resultBySourcePath` and `recomputeWarning`. On every
`Scan()` call, `checked` is reset so every returned book defaults to
checked (`true`) — matching "assume all items are checked when scanned."

**UI:**
- `BookCard` gains a checkbox, bound to `checked[book.sourcePath]`,
  dispatching a `toggled` event (analogous to `edited`) on change so the
  parent stays the single owner of selection state.
- `ScanReviewView`'s topbar gains a "select all" checkbox. Checking it sets
  every *currently visible* book's `checked` to `true`; unchecking it sets
  every currently visible book's `checked` to `false`. It does not force
  items outside the current filter. Its own checked/indeterminate state is
  derived (not stored): checked if every visible book is checked, empty (no
  visible books) hides it, otherwise unchecked. Svelte doesn't have a
  built-in indeterminate binding for `<input type="checkbox">`, so a partial
  selection (some but not all visible checked) will render the header
  checkbox unchecked rather than in a native indeterminate state, keeping
  the implementation to plain reactive bindings rather than manual DOM
  access to set `.indeterminate`.

**Apply:** `doApply()` changes its source list from `visibleBooks` to
`visibleBooks.filter(b => checked[b.sourcePath])`. Everything downstream
(the `ConfirmApply` count, the `Apply()` call, the per-book result
rendering) operates on that narrowed list exactly as it does today — no
backend change, since `appapi.App.Apply` already just processes whatever
slice of `BookView` it's given. Unresolved books remain includable/checked
(consistent with current behavior); the backend still skips them
server-side regardless of checked state.

**Interaction with filtering:** unchecked items stay in the list (still
rendered, not hidden) — matches your answer that checked-but-filtered-out
items aren't applied either, and avoids a confusing "item vanished" UX from
unchecking.

## Feature 2: Title/Author swap button

A `< >` button is added to `BookCard`'s `.fields` row, between the title
and author inputs (currently `flex: 2` / `flex: 2` / `flex: 1` for
title/author/year — the swap button becomes a narrow fourth flex item
between title and author; year is unaffected).

**Behavior:** on click, swap `book.title.value` and `book.author.value`,
set both fields' `source` to `'Edited'`, and dispatch the same `edited`
event `scheduleEdit` already dispatches for typed changes — reusing
`ScanReviewView.onEdited` → `Recompute()` → dest-path-update with no new
wiring on the parent side. Unlike typing, a swap is a single atomic action,
so it dispatches immediately with no debounce.

**No backend change:** `Recompute` already accepts a full `BookView` and
recomputes purely from whatever title/author/year values it's given, so a
swap is indistinguishable from the user retyping both fields by hand.

## Feature 3: Open original file

**Backend:** a new Wails-bound method, `(*App).OpenFile(path string)
error` in `desktop/app.go`, delegating to `runtime.BrowserOpenURL`. This
follows the existing `ConfirmApply`/`ConfirmUndo` precedent of a thin
Wails-bound method that talks to the OS/runtime layer directly with no
`appapi` involvement, since opening a file touches neither config, books,
nor the operations log. `runtime.BrowserOpenURL`'s exact signature (whether
it returns an error synchronously, or is fire-and-forget) will be confirmed
against the installed Wails version during implementation; `OpenFile`'s own
signature stays `error` regardless, so the frontend contract doesn't change
if the underlying call turns out not to report failures synchronously.

Book paths routinely contain spaces and other characters that aren't valid
unescaped in a URI (real library filenames in this app include `:`, `,`,
parentheses), so the path must be converted to a proper `file://` URI (e.g.
via `url.URL{Scheme: "file", Path: path}`, not naive string
concatenation) — implementation must include a test with a path containing
a space to catch a regression here.

**Frontend:** `BookCard`'s existing `.old-name` div (currently plain text
showing `book.oldFilename`) becomes a clickable, link-styled element. A
`dblclick` handler calls `OpenFile(book.sourcePath)` (the full original
path, not just the displayed filename). On rejection, an inline error
banner appears scoped to that card (reusing the existing `.banner.error`
style already used for `scanError`/`applyError` at the view level, but
rendered per-card so one card's open failure doesn't blank the whole
screen).

## Testing strategy

- **Checkbox** (`ScanReviewView.test.ts`): a fresh scan defaults every book
  to checked; unchecking one excludes it from `Apply`'s payload while
  leaving others included; the select-all checkbox only affects currently
  visible (filtered) books, not ones hidden by the active filter/search.
- **Swap** (`BookCard.test.ts`): clicking the swap button exchanges the
  title/author input values, dispatches `edited` with both fields'
  `source: 'Edited'` and the values swapped, and leaves `year` untouched.
- **Open file** (`BookCard.test.ts`): double-clicking the filename calls
  the mocked `OpenFile` with the book's `sourcePath`; a rejected `OpenFile`
  shows the card-scoped error banner. `OpenFile` itself has one narrow
  Go-side unit test covering the file-URI construction (a path containing a
  space converts to a correctly escaped `file://` URI) — the one piece of
  actual logic in an otherwise direct Wails-runtime passthrough, unlike
  `ConfirmApply`/`ConfirmUndo` which have no non-trivial logic to isolate.

## Out of scope

- Persisting checkbox state across a Scan (a fresh Scan always resets every
  item to checked; there's no "remember my last selection" behavior).
- Any bulk action beyond Apply (e.g. a "delete selected" or "hide selected"
  button) — checkboxes exist solely to narrow what Apply acts on.
- Any other field-swap combination (e.g. title/subtitle, or a general
  "swap any two fields" control) — this is specifically title↔author, the
  one mix-up you've actually hit.
- Editing or previewing the file's contents in-app — "open" always hands
  off to the OS default application for that file type; no in-app viewer.
