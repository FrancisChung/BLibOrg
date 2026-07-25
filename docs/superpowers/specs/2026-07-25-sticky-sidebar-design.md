# Design: Sticky sidebar during main-content scroll

## Background

`App.svelte`'s `.shell` is a plain flex row (`display: flex; min-height: 100vh;`) with no height constraint, so when `main`'s content is taller than the viewport, the whole page scrolls -- and the sidebar, having no special positioning, scrolls away with it instead of staying visible.

## Goal

Keep the sidebar (and the resize handle beside it) pinned to the viewport while the user scrolls the main content, matching the standard "app shell" pattern (VS Code, Slack, etc.) where the left rail persists regardless of how long the main content is.

## Design

Two small, additive CSS changes, no JS/layout logic changes:

**`desktop/frontend/src/lib/Sidebar.svelte`'s `.sidebar` class** gains:

```css
position: sticky;
top: 0;
align-self: flex-start;
height: 100vh;
overflow-y: auto;
```

`align-self: flex-start` overrides the flex row's default stretch-to-match-`main`'s-height behavior, so the sidebar keeps a fixed `100vh` height instead of growing as tall as the page's content. `position: sticky; top: 0` then pins that fixed-height box to the top of the viewport as the page scrolls -- it keeps its current background/border, so the left panel now stays visually persistent (full viewport height, consistent background) at any scroll position, not just its nav items floating with a hard edge partway down a long page. `overflow-y: auto` is a safety net for the case where the nav's own content (many library categories) exceeds one viewport's height -- it scrolls internally rather than clipping, rather than assuming it always fits.

**`desktop/frontend/src/App.svelte`'s `.resize-handle` class** gains the same four properties (`position: sticky; top: 0; align-self: flex-start; height: 100vh;` -- no `overflow-y` needed, it has no scrollable content of its own), so the draggable divider stays full-height and grabbable at any scroll position, matching the sidebar's now-persistent height. Without this, the resize handle (still stretched to match `main`'s full, possibly much taller, content height) would visually run past the bottom of the now-shorter, pinned sidebar it's supposed to divide.

## Testing

Existing `Sidebar.test.ts` and `App.test.ts` tests use `@testing-library/svelte` + jsdom, which don't lay out CSS (no real box model, no scroll simulation) -- `position: sticky`/`overflow-y` are visual-only properties with no observable behavior in that test environment, so no new automated test is meaningful here. Verification is manual: run the app, navigate to a Library view with enough books to make `main` scroll taller than one viewport, scroll down, and confirm the sidebar and resize handle stay visible/pinned rather than scrolling away.

## Non-goals

- No change to the sidebar's resizable-width behavior (already works, unaffected by this change).
- No change to how `main` itself scrolls (the page-level scroll, not an internal `main`-only scrollbar) -- this fix works within that existing model rather than introducing a new one.
