# Sticky Sidebar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep the sidebar and its resize handle pinned to the viewport while the user scrolls the main content, instead of scrolling away with it.

**Architecture:** Two small, additive CSS changes — `position: sticky` on the sidebar's root `<nav>` (`Sidebar.svelte`) and the matching resize handle (`App.svelte`) — no JS/layout logic changes.

**Tech Stack:** Svelte 3 + TypeScript (CSS only for this plan).

## Global Constraints

- Sidebar keeps its fixed `100vh` height via `align-self: flex-start` (overriding the flex row's default stretch-to-match-`main` behavior) and pins via `position: sticky; top: 0`.
- Sidebar gains `overflow-y: auto` as a safety net for the case where its own nav content (many library categories) exceeds one viewport's height.
- The resize handle gets the same `position: sticky; top: 0; align-self: flex-start; height: 100vh;` treatment (no `overflow-y`, it has no scrollable content), so it stays full-height and grabbable at any scroll position, matching the sidebar's now-persistent height.
- No change to the sidebar's resizable-width behavior or to how the page/`main` scrolls at the model level (still page-level scroll, not an internal `main`-only scrollbar).

---

### Task 1: Make the sidebar and resize handle sticky

**Files:**
- Modify: `desktop/frontend/src/lib/Sidebar.svelte:98-106`
- Modify: `desktop/frontend/src/App.svelte:112-117`

**Interfaces:**
- Consumes: nothing new.
- Produces: nothing consumed by a later task (this is the only task).

- [ ] **Step 1: Add sticky positioning to the sidebar**

In `desktop/frontend/src/lib/Sidebar.svelte`, change the `.sidebar` CSS rule from:

```css
  .sidebar {
    flex-shrink: 0;
    background: var(--bf-surface);
    border-right: 1px solid var(--bf-border);
    padding: 28px 16px 20px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
```

to:

```css
  .sidebar {
    flex-shrink: 0;
    position: sticky;
    top: 0;
    align-self: flex-start;
    height: 100vh;
    overflow-y: auto;
    background: var(--bf-surface);
    border-right: 1px solid var(--bf-border);
    padding: 28px 16px 20px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
```

- [ ] **Step 2: Add matching sticky positioning to the resize handle**

In `desktop/frontend/src/App.svelte`, change the `.resize-handle` CSS rule from:

```css
  .resize-handle {
    width: 6px;
    flex-shrink: 0;
    cursor: col-resize;
    position: relative;
  }
```

to:

```css
  .resize-handle {
    width: 6px;
    flex-shrink: 0;
    cursor: col-resize;
    position: sticky;
    top: 0;
    align-self: flex-start;
    height: 100vh;
  }
```

(The existing `.resize-handle:hover::after`/`.resize-handle.resizing::after` rules, which add the highlight line, are unaffected — they already use `position: absolute` relative to this element and `top: 0; bottom: 0;`, which continues to span the handle's own box regardless of how that box is positioned.)

- [ ] **Step 3: Run the existing frontend test suite to confirm no regressions**

Run: `cd desktop/frontend && npx vitest run`
Expected: PASS (all existing test files, including `Sidebar.test.ts` and `App.test.ts` — `position`/`overflow-y` are visual-only CSS properties with no observable behavior in jsdom's test environment, per the design spec's Testing section, so no test assertions change; this step only confirms the edit didn't break anything else).

- [ ] **Step 4: Manually verify the sticky behavior in the real app**

Use the `run` skill (or `cd desktop && wails dev`, or build via `npm run build` in `desktop/frontend` then `wails build`) to launch the real desktop app. Navigate to the Library view with enough books/categories that `main`'s content is taller than one viewport, then scroll down. Confirm:
- The sidebar (menu items, categories, Settings) stays visible and pinned at the top of the viewport as you scroll.
- The resize handle (the thin divider between sidebar and main) stays visible and remains draggable at any scroll position.
- Dragging the resize handle to resize the sidebar still works correctly (unaffected by this change).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/Sidebar.svelte desktop/frontend/src/App.svelte
git commit -m "Keep the sidebar and resize handle pinned to the viewport while scrolling"
```
