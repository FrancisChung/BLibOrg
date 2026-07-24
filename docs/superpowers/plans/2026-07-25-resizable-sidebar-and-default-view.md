# Resizable Sidebar + Library Default View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the desktop app open on the Library view by default, and let the user drag-resize the sidebar (persisted across restarts, clamped 160–400px).

**Architecture:** `App.svelte` owns all new state (`activeView` default, `sidebarWidth`, drag tracking) since it's the parent laying out `Sidebar` and `main` as flex siblings. `Sidebar.svelte` becomes a dumb consumer of a new `width` prop, defaulting to `220` so it renders identically when unset. A new `<div class="resize-handle">` sits between the two, driving `window`-level `mousemove`/`mouseup` listeners while dragging. Width persists to `localStorage` on drag-end only.

**Tech Stack:** Svelte 3 + TypeScript, Vitest + @testing-library/svelte + jest-dom matchers, `localStorage` (browser Web Storage API — no new dependency).

## Global Constraints

- Sidebar width clamp: **160px minimum, 400px maximum** (spec: "160px - 400px (Recommended)").
- Sidebar width persists across app restarts via `localStorage`, key `sidebarWidth` (spec: "Persist across restarts (Recommended)").
- Default sidebar width when nothing is stored (or the stored value is invalid/out-of-range): **220px** — the current hardcoded width, so existing layout is visually unchanged until the user drags.
- Resize handle is a thin (~6px) invisible-at-rest strip; a ~2px highlight line appears only on hover or while actively dragging (spec: "Thin invisible strip, cursor changes on hover (Recommended)"). `cursor: col-resize`.
- Default `activeView` on startup changes from `'scan'` to `'library'`.
- No backend/config.yaml involvement — this is frontend-only state.

---

### Task 1: Default view is Library

**Files:**
- Modify: `desktop/frontend/src/App.svelte:12`
- Modify: `desktop/frontend/src/App.test.ts`

**Interfaces:**
- Consumes: nothing new.
- Produces: `App.svelte`'s `activeView` now initializes to `'library'` — later tasks don't depend on this directly, but Task 3's tests render `<App>` and will see `LibraryView` mounted first, so `ListLibrary` must be mocked in every test's `beforeEach`, not per-test.

- [ ] **Step 1: Update the failing/changing tests first**

Open `desktop/frontend/src/App.test.ts`. Add `ListLibrary` to the shared `beforeEach` mock setup (currently only `ConfigStatus`, `ListOperationBatches`, `ListCategoryWarnings` are mocked there), and replace the first test (`'shows Scan & Review by default'`) with one that asserts Library renders first. Also strengthen `'switches to the Library view when its sidebar item is clicked'` so it still exercises an actual view *switch* now that Library is the default (it currently only proves the no-op case of clicking the already-active item).

Replace the whole file's `describe` block with:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';

vi.mock('../wailsjs/go/main/App', () => ({
  ConfigStatus: vi.fn(),
  Scan: vi.fn(),
  Recompute: vi.fn(),
  Apply: vi.fn(),
  ConfirmApply: vi.fn(),
  ListOperationBatches: vi.fn(),
  ListCategoryWarnings: vi.fn(),
  ListLibrary: vi.fn(),
}));

import App from './App.svelte';
import { ConfigStatus, ListOperationBatches, ListCategoryWarnings, ListLibrary } from '../wailsjs/go/main/App';

describe('App', () => {
  beforeEach(() => {
    vi.mocked(ConfigStatus).mockResolvedValue({ path: '', error: '' });
    vi.mocked(ListOperationBatches).mockResolvedValue([]);
    vi.mocked(ListCategoryWarnings).mockResolvedValue([]);
    vi.mocked(ListLibrary).mockResolvedValue({ books: [], categories: [] });
  });

  afterEach(() => {
    localStorage.clear();
  });

  it('shows Library by default', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByText('No books found in the library folder yet.')).toBeInTheDocument();
    });
  });

  it('switches to the Operations Log view when its sidebar item is clicked', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Operations' }));

    await waitFor(() => {
      expect(screen.getByText('Operations Log')).toBeInTheDocument();
    });
  });

  it('switches to the Category Warnings view when its sidebar item is clicked', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Warnings' }));

    await waitFor(() => {
      expect(screen.getByText('Category Warnings')).toBeInTheDocument();
    });
  });

  it('shows a config error banner regardless of active view', async () => {
    vi.mocked(ConfigStatus).mockResolvedValue({ path: '/fake/config.yaml', error: 'not found' });
    render(App);

    await waitFor(() => {
      expect(screen.getByText(/No usable config at/)).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Operations' }));
    expect(screen.getByText(/No usable config at/)).toBeInTheDocument();
  });

  it('shows a banner for each config rule warning', async () => {
    vi.mocked(ConfigStatus).mockResolvedValue({
      path: '/fake/config.yaml',
      error: '',
      warnings: ['rule 9 (match_value "(?i)\\bc++\\b"): invalid regex: invalid nested repetition operator: `++`'],
    });
    render(App);

    await waitFor(() => {
      expect(screen.getByText(/rule 9/)).toBeInTheDocument();
    });
  });

  it('switches to Scan & Review and back to Library when their sidebar items are clicked', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByText('No books found in the library folder yet.')).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Scan & Review' }));
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Library' }));
    await waitFor(() => {
      expect(screen.getByText('No books found in the library folder yet.')).toBeInTheDocument();
    });
  });
});
```

This replaces the old `'shows Scan & Review by default'` and `'switches to the Library view when its sidebar item is clicked'` tests with `'shows Library by default'` and `'switches to Scan & Review and back to Library...'` respectively. The `afterEach(() => localStorage.clear())` is added now (rather than in Task 3) so Task 3's tests don't need to touch this shared setup again.

- [ ] **Step 2: Run the updated tests to confirm they fail for the right reason**

Run: `cd desktop/frontend && npx vitest run src/App.test.ts`
Expected: FAIL — `'shows Library by default'` fails because `activeView` still defaults to `'scan'`, so `ScanReviewView` renders instead of `LibraryView` and the "No books found..." text is never shown.

- [ ] **Step 3: Change the default view**

In `desktop/frontend/src/App.svelte`, change line 12:

```ts
  let activeView: SidebarView = 'scan';
```

to:

```ts
  let activeView: SidebarView = 'library';
```

- [ ] **Step 4: Run the tests to confirm they pass**

Run: `cd desktop/frontend && npx vitest run src/App.test.ts`
Expected: PASS (all tests in the file).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/App.svelte desktop/frontend/src/App.test.ts
git commit -m "Default the desktop app to the Library view on startup"
```

---

### Task 2: Sidebar accepts a `width` prop

**Files:**
- Modify: `desktop/frontend/src/lib/Sidebar.svelte`
- Modify: `desktop/frontend/src/lib/Sidebar.test.ts`

**Interfaces:**
- Consumes: nothing new.
- Produces: `Sidebar.svelte` gains `export let width: number = 220;`, applied as an inline `style="width: {width}px"` on its root `<nav class="sidebar">` element. Task 3's `App.svelte` passes `width={sidebarWidth}` into this prop. The root `<nav>` remains queryable in tests via `screen.getByRole('navigation')` (implicit ARIA role of `<nav>`).

- [ ] **Step 1: Write the failing test**

Add this test to `desktop/frontend/src/lib/Sidebar.test.ts` (inside the existing `describe('Sidebar', ...)` block, e.g. after the `'renders a Settings nav item'` test):

```ts
  it('defaults to a width of 220px when no width prop is passed', () => {
    render(Sidebar, { active: 'library' });
    expect(screen.getByRole('navigation')).toHaveStyle({ width: '220px' });
  });

  it('applies a passed width prop as an inline style', () => {
    render(Sidebar, { active: 'library', width: 300 });
    expect(screen.getByRole('navigation')).toHaveStyle({ width: '300px' });
  });
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/Sidebar.test.ts`
Expected: FAIL — the rendered `<nav>` has no inline `width` style at all (width currently comes only from the fixed CSS rule, not an inline style), so `toHaveStyle({ width: '220px' })` fails on the first assertion.

- [ ] **Step 3: Add the width prop**

In `desktop/frontend/src/lib/Sidebar.svelte`, add the prop after the existing prop declarations (after line 7, `export let activeLibraryCategory: string = '';`):

```ts
  export let width: number = 220;
```

Change the root element (line 37) from:

```svelte
<nav class="sidebar">
```

to:

```svelte
<nav class="sidebar" style="width: {width}px">
```

Remove the now-redundant fixed width from the `.sidebar` CSS rule (lines 95-104), changing:

```css
  .sidebar {
    width: 220px;
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
    background: var(--bf-surface);
    border-right: 1px solid var(--bf-border);
    padding: 28px 16px 20px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/lib/Sidebar.test.ts`
Expected: PASS (all tests in the file, including the existing ones — none of them assert on `.sidebar`'s CSS width, so removing it from the stylesheet doesn't break them).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/Sidebar.svelte desktop/frontend/src/lib/Sidebar.test.ts
git commit -m "Let Sidebar accept a width prop instead of a fixed CSS width"
```

---

### Task 3: Drag handle, persistence, and wiring in App.svelte

**Files:**
- Modify: `desktop/frontend/src/App.svelte`
- Modify: `desktop/frontend/src/App.test.ts`

**Interfaces:**
- Consumes: `Sidebar.svelte`'s `width` prop (Task 2) — passed as `width={sidebarWidth}`.
- Produces: nothing consumed by later tasks (this is the last task).

- [ ] **Step 1: Write the failing tests**

Add these tests to `desktop/frontend/src/App.test.ts`, inside the existing `describe('App', ...)` block (e.g. at the end, after the `'switches to Scan & Review and back to Library...'` test added in Task 1):

```ts
  it('resizes the sidebar by dragging the resize handle, and persists the final width', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
    });

    const handle = screen.getByRole('separator', { name: 'Resize sidebar' });
    await fireEvent.mouseDown(handle);
    await fireEvent.mouseMove(window, { clientX: 300 });
    await fireEvent.mouseUp(window);

    expect(screen.getByRole('navigation')).toHaveStyle({ width: '300px' });
    expect(localStorage.getItem('sidebarWidth')).toBe('300');
  });

  it('clamps sidebar width to 160-400px while dragging past either bound', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
    });

    const handle = screen.getByRole('separator', { name: 'Resize sidebar' });
    await fireEvent.mouseDown(handle);

    await fireEvent.mouseMove(window, { clientX: 50 });
    expect(screen.getByRole('navigation')).toHaveStyle({ width: '160px' });

    await fireEvent.mouseMove(window, { clientX: 900 });
    expect(screen.getByRole('navigation')).toHaveStyle({ width: '400px' });

    await fireEvent.mouseUp(window);
    expect(localStorage.getItem('sidebarWidth')).toBe('400');
  });

  it('restores a previously persisted sidebar width on mount', async () => {
    localStorage.setItem('sidebarWidth', '275');
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('navigation')).toHaveStyle({ width: '275px' });
    });
  });

  it('falls back to the 220px default when the stored width is invalid or out of range', async () => {
    localStorage.setItem('sidebarWidth', 'not-a-number');
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('navigation')).toHaveStyle({ width: '220px' });
    });
  });

  it('stops tracking the drag after mouseup, so further mouse movement has no effect', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
    });

    const handle = screen.getByRole('separator', { name: 'Resize sidebar' });
    await fireEvent.mouseDown(handle);
    await fireEvent.mouseMove(window, { clientX: 300 });
    await fireEvent.mouseUp(window);

    await fireEvent.mouseMove(window, { clientX: 180 });
    expect(screen.getByRole('navigation')).toHaveStyle({ width: '300px' });
  });
```

(`localStorage.clear()` between tests is already handled by the `afterEach` added in Task 1, Step 1.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/App.test.ts`
Expected: FAIL — `screen.getByRole('separator', { name: 'Resize sidebar' })` throws (no such element exists yet) in every new test.

- [ ] **Step 3: Implement the resize handle and drag logic**

In `desktop/frontend/src/App.svelte`, add these constants and this state after the existing `let activeLibraryCategory = '';` line (line 16):

```ts
  const SIDEBAR_WIDTH_KEY = 'sidebarWidth';
  const SIDEBAR_MIN_WIDTH = 160;
  const SIDEBAR_MAX_WIDTH = 400;
  const SIDEBAR_DEFAULT_WIDTH = 220;

  function loadSidebarWidth(): number {
    const stored = Number(localStorage.getItem(SIDEBAR_WIDTH_KEY));
    return stored >= SIDEBAR_MIN_WIDTH && stored <= SIDEBAR_MAX_WIDTH ? stored : SIDEBAR_DEFAULT_WIDTH;
  }

  let sidebarWidth = loadSidebarWidth();
  let resizingSidebar = false;
```

Add these handler functions after the existing `onCategoriesLoaded` function (after line 36):

```ts
  function onResizeMouseDown() {
    resizingSidebar = true;
    window.addEventListener('mousemove', onResizeMouseMove);
    window.addEventListener('mouseup', onResizeMouseUp);
  }

  function onResizeMouseMove(e: MouseEvent) {
    sidebarWidth = Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, e.clientX));
  }

  function onResizeMouseUp() {
    resizingSidebar = false;
    window.removeEventListener('mousemove', onResizeMouseMove);
    window.removeEventListener('mouseup', onResizeMouseUp);
    localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth));
  }
```

Update the markup (lines 39-46) from:

```svelte
<div class="shell">
  <Sidebar
    active={activeView}
    {libraryCategories}
    {activeLibraryCategory}
    on:navigate={onNavigate}
    on:selectCategory={onSelectCategory}
  />
  <main>
```

to:

```svelte
<div class="shell">
  <Sidebar
    active={activeView}
    {libraryCategories}
    {activeLibraryCategory}
    width={sidebarWidth}
    on:navigate={onNavigate}
    on:selectCategory={onSelectCategory}
  />
  <div
    class="resize-handle"
    class:resizing={resizingSidebar}
    role="separator"
    aria-orientation="vertical"
    aria-label="Resize sidebar"
    on:mousedown={onResizeMouseDown}
  ></div>
  <main>
```

Add CSS for the handle to the `<style>` block, after the `.shell` rule (after line 72):

```css
  .resize-handle {
    width: 6px;
    flex-shrink: 0;
    cursor: col-resize;
    position: relative;
  }
  .resize-handle:hover::after,
  .resize-handle.resizing::after {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    left: 2px;
    width: 2px;
    background: var(--bf-blue);
  }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/App.test.ts`
Expected: PASS (all tests in the file).

- [ ] **Step 5: Run the full frontend test suite to check for regressions**

Run: `cd desktop/frontend && npx vitest run`
Expected: PASS (all test files, no regressions in `Sidebar.test.ts`, `LibraryView.test.ts`, or elsewhere).

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/App.svelte desktop/frontend/src/App.test.ts
git commit -m "Add a draggable, persisted resize handle for the sidebar"
```

---

## Manual Verification (after all tasks complete)

Since this is a GUI change, verify visually before considering the branch done:

1. `cd desktop/frontend && npm run dev` (or `wails dev` from `desktop/`) and open the app.
2. Confirm it opens on the Library view, not Scan & Review.
3. Hover over the boundary between the sidebar and the main pane — confirm the cursor changes to a resize cursor and a thin highlight line appears.
4. Drag the boundary left and right — confirm the sidebar visibly resizes, and that it stops shrinking/growing at the 160px/400px bounds.
5. Restart the app (or reload the dev server page) — confirm the sidebar reopens at the last-dragged width.
