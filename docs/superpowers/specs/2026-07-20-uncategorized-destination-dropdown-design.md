# Design: Manual destination dropdown for Uncategorized cards

## Feature Request

For uncategorised items, add a dropdown at the bottom row of the scan
result card. It should be right-justified. The dropdown should contain all
the possible destinations we can move the file to, as per the `categories`
section of `config.yaml`. Selecting a destination should change the card's
status to "Edited" and the destination path shown below should update to
the new destination. Pressing Apply should move the file to the
user-selected destination.

## Context / constraints

- Some scanned books can't be auto-categorized by the existing rules/subject
  matching in `categorizer.Categorize` and fall through to `Uncategorized`.
  This gives the user a way to manually route those into a real category
  from the card itself, instead of only being able to fix it by editing
  `config.yaml` rules and re-scanning.
- Clarified during brainstorming:
  - Dropdown is a single flat list of "Category / Subcategory" leaf
    destinations (just "Category" for one with no subcategories) — not a
    two-step category-then-subcategory picker.
  - A manual pick sticks even if the user subsequently edits Title/Author —
    it does not get silently reverted by the next auto-categorization pass,
    matching how a manually-edited Title/Author already stays "Edited"
    through further recomputes.
  - The dropdown stays visible after a pick (now showing the picked
    destination), rather than disappearing once `category` is no longer
    literally `"Uncategorized"`.
  - It shares the existing badges row (status/dup/Uncategorized pills),
    right-aligned in that row, rather than getting its own new row.

## Current architecture (relevant pieces)

- `internal/categorizer.Categorize(b *book.Book, cfg config.Config)` always
  recomputes `b.Category`/`b.Subcategory`/`b.CategoryWarning` from
  `cfg.Rules` (then embedded-subject matching, then `Uncategorized`
  fallback). It has no concept of "already decided, don't touch."
- `internal/book.Book.Status()` derives the row's overall status purely from
  `Title`/`Author`/`Year` `Field.Source` precedence
  (`Unresolved > Partial > Manual > Heuristic > Metadata`). `Category` isn't
  a `book.Field` at all today — it's a plain `string`, with no `Source`.
- `appapi.Recompute` (`internal/appapi/recompute.go`) is the single path any
  card edit goes through: `viewToBook` → `categorizer.Categorize` →
  `rename.BuildPath` → `bookToView`. `viewToBook`'s doc comment is explicit
  that `Category`/`Subcategory`/`DestPath`/`Status` are "outputs, not
  carried over — callers recompute them," so a `BookView` sent in from the
  frontend has its incoming category info discarded and freshly rederived
  every time.
- `internal/rename.BuildPath` renders `DestPath` from
  `cfg.General.LibraryFolder`, `b.Category`, `b.Subcategory`, and the
  title/year/author template — it doesn't care *how* `Category`/
  `Subcategory` were decided, only their current value.
- `BookCard.svelte`'s `scheduleEdit`/swap-button pattern: any local edit
  updates the local `book` object, sets the relevant field's `source` to
  `'Edited'`, and dispatches `edited` with the whole `BookView`.
  `ScanReviewView.onEdited` calls `Recompute(edited)` and replaces the book
  in `books` with whatever comes back. This is the one mechanism by which
  any field change (typed, swapped, or otherwise) ends up reflected in
  `status`/`destPath`.
- No existing Wails-bound method exposes `cfg.Categories` to the frontend.

## Backend changes

**`internal/book/book.go`**
- `Book` gains `CategoryManual bool` (zero value `false`, matching every
  other field's "not yet touched" default).
- `Status()` gains a check for `CategoryManual` at the same precedence slot
  as a manually-edited Title/Author/Year: after the `Unresolved`/`Partial`
  early returns, `if b.CategoryManual { return SourceManual }`, before the
  existing Title/Author/Year Manual/Heuristic/Metadata loop. A manual
  category pick always means "Edited," regardless of how Title/Author/Year
  were resolved — mirroring existing precedence (Unresolved/Partial for
  Title/Author/Year still wins, since Apply can't proceed on an unresolved
  Title regardless of category).

**`internal/categorizer/categorizer.go`**
- `Categorize` returns immediately, before touching
  `Category`/`Subcategory`/`CategoryWarning`, when `b.CategoryManual` is
  true. This is the mechanism that makes a manual pick "stick" through
  later recomputes triggered by Title/Author/Year edits.

**`internal/appapi/dto.go`**
- `BookView` gains `CategoryManual bool` (json `categoryManual`).
- `bookToView`/`viewToBook` carry `Category`, `Subcategory`, and
  `CategoryManual` through in both directions (currently `viewToBook` drops
  incoming category info entirely). This is safe when `CategoryManual` is
  false — `Categorize` immediately overwrites whatever came in.

**`internal/appapi/app.go`** (new)
- `DestinationView struct { Category string; Subcategory string }` (JSON
  `category`/`subcategory`).
- `func (a *App) Categories() ([]DestinationView, error)`: loads config,
  flattens `cfg.Categories` into one `DestinationView` per leaf destination
  (subcategory-less categories produce one entry with `Subcategory: ""`),
  **excludes the `"Uncategorized"` category itself** (never a valid manual
  destination), and sorts the result by `Category` then `Subcategory`
  (plain case-sensitive `sort.Slice` — this list is only for display order,
  not matching, so a simple stable sort is enough).

**`desktop/app.go`**
- `func (a *App) Categories() ([]appapi.DestinationView, error) { return a.api.Categories() }`,
  following the existing thin-passthrough pattern used for `Scan`/`Apply`/
  `Recompute`.

**Wails bindings**
- Regenerate `desktop/frontend/wailsjs/go/main/App.d.ts` (and the JS
  counterpart) via `wails generate module` so `Categories()` is callable
  from the frontend, and `appapi.DestinationView` appears in
  `wailsjs/go/models.ts`.

## Frontend changes

**`desktop/frontend/src/lib/types.ts`**
- `BookView` gains `categoryManual: boolean`.
- New `DestinationOption { category: string; subcategory: string }` (or
  reuse the generated `appapi.DestinationView` type directly).

**`desktop/frontend/src/lib/ScanReviewView.svelte`**
- On mount, calls `Categories()` once and stores the result in a
  `destinations: DestinationView[]` variable. Fetched once per app session
  (categories don't change without editing `config.yaml`, which isn't a
  live-reload scenario this app supports today), not re-fetched per `Scan`.
- Passes `destinations` down to every `BookCard` as a prop.

**`desktop/frontend/src/lib/BookCard.svelte`**
- New `export let destinations: DestinationView[] = [];` prop.
- In the `.badges` row: add `justify-content: space-between` (badges
  already render left; nothing needs to move). Add a `<select>` rendered
  when `book.category === 'Uncategorized' || book.categoryManual`, right
  side of that row.
- Options: one per `destinations` entry, label `"${category} / ${subcategory}"`
  when `subcategory` is non-empty else just `category`; the option's
  `value` is its index into `destinations` (as a string) rather than an
  encoded category/subcategory string, so there's no risk of a delimiter
  colliding with a category/subcategory name that happens to contain it. A
  leading disabled placeholder option ("Choose a destination…") is selected
  when `!book.categoryManual`, so nothing is picked without explicit user
  action; once `book.categoryManual` is true, the `<select>`'s value is set
  to the index of the `destinations` entry matching
  `book.category`/`book.subcategory`, so the control shows the current pick
  and can be changed again.
- `onDestinationChange`: looks up `destinations[selectedIndex]`, updates
  local `book = { ...book, category, subcategory, categoryManual: true }`, and
  dispatches `edited` immediately (no debounce — an atomic pick, same as
  the existing title/author swap button), reusing
  `ScanReviewView.onEdited` → `Recompute()` → dest-path/status update with
  no new parent-side wiring.

**Apply**
- No changes anywhere in the apply path (`doApply`, `Apply` Wails call,
  `appapi.App.Apply`). Once `Recompute` has set `DestPath` from the picked
  category, Apply already just moves `sourcePath` → `DestPath` for whatever
  books are checked, exactly as it does for every other field edit today.

## Testing strategy

- **`book.Status()`** (`internal/book/book_test.go`): a row with resolved
  Title/Author/Year (any source) and `CategoryManual: true` reports
  `SourceManual`; an `Unresolved` Title still wins over `CategoryManual`
  (Apply-blocking takes precedence).
- **`categorizer.Categorize`** (`internal/categorizer/categorizer_test.go`):
  with `CategoryManual: true` and pre-set `Category`/`Subcategory`, calling
  `Categorize` with a `cfg` whose rules would otherwise match something
  else leaves `Category`/`Subcategory`/`CategoryWarning` untouched.
- **`appapi.App.Categories()`** (new `internal/appapi/app_test.go` case):
  given a `cfg.Categories` map, returns every leaf destination sorted, and
  excludes `"Uncategorized"`.
- **`appapi.Recompute` round trip** (`internal/appapi/recompute_test.go`):
  a `BookView` with `categoryManual: true` and a category that wouldn't
  match any rule keeps that category after `Recompute`, and `destPath`
  reflects it.
- **`dto` round trip** (`internal/appapi/dto_roundtrip_test.go`):
  `category`/`subcategory`/`categoryManual` survive `bookToView` →
  `viewToBook` → `bookToView`.
- **`BookCard.test.ts`**: dropdown is absent for a categorized, non-manual
  book; present for `category === 'Uncategorized'`; selecting an option
  dispatches `edited` with `category`/`subcategory` set from the option and
  `categoryManual: true`; dropdown remains present and reflects the new
  value after `book.categoryManual` becomes true even though `category` is
  no longer `'Uncategorized'`.

## Out of scope

- No way to *revert* a manual pick back to auto-categorization from the UI
  (no "Uncategorized" or "Auto" entry in the dropdown) — picking is
  one-directional for now; reverting means editing Title/Author enough to
  trigger a rescan, or a future feature.
- No live-reload of `config.yaml`'s categories while the app is running —
  `Categories()` is fetched once per session, same freshness assumption the
  rest of the app already makes about config.
- No validation that a hand-edited `config.yaml`'s categories list changed
  between when `Categories()` was fetched and when `Apply` runs — same
  class of staleness the app already accepts elsewhere (e.g. rules can
  change between scan and apply today too).
