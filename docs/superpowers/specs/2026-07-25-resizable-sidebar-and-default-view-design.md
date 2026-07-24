# Design: Resizable sidebar + Library as default view

## Background

Two small GUI requests: (1) the divider between the sidebar (menu options)
and the main detail pane is currently fixed-width and not user-adjustable;
(2) the app currently opens on the Scan & Review view by default, but
Library is the more commonly-wanted starting point.

## Goal

Let the user drag the sidebar/main boundary to resize the sidebar, with
the chosen width persisting across app restarts. Change the app's default
starting view from Scan & Review to Library.

## Architecture

### Default view (trivial)

`desktop/frontend/src/App.svelte`: `let activeView: SidebarView = 'scan';`
becomes `let activeView: SidebarView = 'library';`. No other changes.

### Resizable sidebar

**Ownership:** `App.svelte` owns the resize state and drag logic, since
it's the parent laying out both siblings (`Sidebar` and `main`) inside
`.shell` — the interaction sits between them, not inside either.
`Sidebar.svelte` gains a new prop:

```ts
export let width: number = 220;
```

applied via an inline style on its root `<nav>` element
(`style="width: {width}px"`), replacing the current hardcoded
`width: 220px` in `Sidebar.svelte`'s own `.sidebar` CSS class (which keeps
`flex-shrink: 0`, just drops the fixed width). The default of `220` means
`Sidebar.svelte` renders identically to today when no `width` prop is
passed — no existing test or standalone usage breaks.

**The handle:** a new `<div class="resize-handle">` in `App.svelte`,
positioned between `<Sidebar>` and `<main>` inside `.shell`. ~6px wide,
`cursor: col-resize`, transparent background at rest. On `:hover` and
while actively dragging, a ~2px vertical highlight line appears
(centered in the 6px strip) via a background-color change — no visible
element at rest beyond the cursor change on hover, matching the "thin
invisible strip" pattern common to VS Code/Slack-style resizable panes.

**Drag mechanics**, in `App.svelte`:

```ts
let sidebarWidth = loadSidebarWidth();
let resizing = false;

function loadSidebarWidth(): number {
  const stored = Number(localStorage.getItem('sidebarWidth'));
  return stored >= 160 && stored <= 400 ? stored : 220;
}

function startResize() {
  resizing = true;
  window.addEventListener('mousemove', onResize);
  window.addEventListener('mouseup', stopResize);
}

function onResize(e: MouseEvent) {
  sidebarWidth = Math.min(400, Math.max(160, e.clientX));
}

function stopResize() {
  resizing = false;
  window.removeEventListener('mousemove', onResize);
  window.removeEventListener('mouseup', stopResize);
  localStorage.setItem('sidebarWidth', String(sidebarWidth));
}
```

`e.clientX` directly gives the new width because the sidebar starts flush
against the window's left edge (`.shell` has no left padding/offset) — no
need to track a start-width + delta, the cursor's X position *is* the
sidebar's width at any point during the drag. Width is clamped to
**160–400px** on every mousemove (160px: roughly the narrowest before
category submenu labels/icons start wrapping awkwardly; 400px: leaves
reasonable room for the detail pane on a typical laptop screen), so the
handle can never produce an out-of-bounds value even from a fast/overshot
drag — no separate clamp-on-drop step needed.

**Persistence:** `localStorage`, a new pattern for this app's frontend
(nothing else uses it yet) — the right fit here since this is a pure
client-side UI preference with no reason to round-trip through
`config.yaml`/the Go backend. Read once on `App.svelte` mount (module-level
`let` initializer, evaluated when the component instance is created);
written once per drag, on `mouseup` (not on every `mousemove`, to avoid
excessive writes during a single drag gesture).

## Testing

- `App.test.ts` (new): dragging the handle (`fireEvent.mouseDown` on
  `.resize-handle`, `fireEvent.mouseMove(window, {clientX: N})`,
  `fireEvent.mouseUp(window)`) updates the rendered `Sidebar`'s width;
  dragging past 400 clamps at 400, dragging below 160 clamps at 160;
  after `mouseUp`, `localStorage.getItem('sidebarWidth')` reflects the
  final value; on a fresh mount with a pre-set `localStorage` value, the
  sidebar starts at that width instead of the 220 default; an invalid/
  out-of-range stored value (corrupted or hand-edited) falls back to 220
  rather than propagating a bad width.
- `Sidebar.test.ts`: one new test confirming a passed `width` prop is
  applied as the rendered element's inline width style; existing tests
  (which don't pass `width` at all) keep passing unchanged, proving the
  default-220 behavior is preserved.
- `App.test.ts` default-view test: mounting `App` with no prior
  `activeView` override shows the Library view first (existing
  `ScanReviewView`-shows-first assumption, if any test currently asserts
  that, is updated to assert `LibraryView` instead).

## Non-goals

- No resize handle for any other pane split (e.g. within Library's
  category submenu) — sidebar/main only.
- No config.yaml/backend involvement — this is frontend-only state.
- No animation/transition on the resize itself (direct 1:1 cursor
  tracking, no smoothing) — matches how the reference apps (VS Code,
  Slack) behave.
