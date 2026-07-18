# UI Enhancements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add three independent UI enhancements to the Scan & Review view: per-item checkbox selection (only checked items get Applied), a title/author swap button, and a double-click-to-open link on the original filename.

**Architecture:** All three features are additive changes to `desktop/frontend/src/lib/BookCard.svelte` and (for checkbox selection only) `ScanReviewView.svelte`. Checkbox selection narrows the list `doApply()` sends to the existing `Apply()` binding — no backend change. Swap reuses the existing `edited` event / `Recompute()` round-trip already wired for manual field edits. Opening a file is the only piece touching Go: one new thin Wails-bound method, `(*App).OpenFile`, following the same no-`appapi`-involvement pattern as `ConfirmApply`/`ConfirmUndo`.

**Tech Stack:** Go 1.21+ (Wails v2 backend), Svelte 5 + TypeScript (frontend), Vitest + @testing-library/svelte (frontend tests), Go `testing` package (backend tests).

## Global Constraints

- Design spec: `docs/superpowers/specs/2026-07-17-ui-enhancments-design.md` — every task below implements one section of it verbatim; consult it for the "why" behind each decision.
- TDD required throughout: write the failing test first, verify it fails for the expected reason, then implement.
- A fresh `Scan()` resets every returned book's checkbox to checked (`true`) — there is no "remember my last selection" behavior across scans.
- Checkbox selection only narrows what's sent to `Apply()`; it never hides rows from the list (unchecked items stay visible, just excluded from the next Apply).
- The select-all checkbox only affects currently *visible* (filtered) books, never ones hidden by the active search/status filter.
- The swap button marks both title and author as `source: 'Edited'` and dispatches immediately (no debounce) — a swap is one atomic action, not a stream of keystrokes.
- File paths must be converted to a proper `file://` URI (e.g. via `net/url`), never naive string concatenation — book filenames routinely contain spaces and parentheses that require percent-encoding.
- `runtime.BrowserOpenURL` (Wails v2.13.0, confirmed in `pkg/runtime/browser.go`) has signature `func BrowserOpenURL(ctx context.Context, url string)` — it returns nothing, so `OpenFile`'s only reportable failure is the file no longer existing at the recorded path (checked via `os.Stat` before calling it).

---

### Task 1: Checkbox selection

**Files:**
- Modify: `desktop/frontend/src/lib/BookCard.svelte`
- Modify: `desktop/frontend/src/lib/BookCard.test.ts`
- Modify: `desktop/frontend/src/lib/ScanReviewView.svelte`
- Modify: `desktop/frontend/src/lib/ScanReviewView.test.ts`

**Interfaces:**
- Consumes: nothing new — reuses existing `Scan()`, `Apply()`, `ConfirmApply()` Wails bindings and `matchesFilter`/`matchesQuery` from `./types`.
- Produces: `BookCard` gains prop `checked: boolean` (default `true`) and dispatches a new `toggled: boolean` event. `ScanReviewView` gains a `checked: Record<string, boolean>` state map keyed by `sourcePath` and a derived `selectedBooks` list — both are internal to `ScanReviewView`, not consumed by later tasks.

- [ ] **Step 1: Write the failing BookCard checkbox tests**

Add to the end of the `describe('BookCard', ...)` block in `desktop/frontend/src/lib/BookCard.test.ts` (after the existing three `it(...)` blocks, before the closing `});`):

```typescript
  it('checkbox reflects the checked prop and defaults to checked', () => {
    render(BookCard, { book: makeBook() });
    expect(screen.getByRole('checkbox', { name: 'Select book.epub' })).toBeChecked();
  });

  it('renders unchecked when checked prop is false', () => {
    render(BookCard, { book: makeBook(), checked: false });
    expect(screen.getByRole('checkbox', { name: 'Select book.epub' })).not.toBeChecked();
  });

  it('dispatches "toggled" with the new value when the checkbox is clicked', async () => {
    const { component } = render(BookCard, { book: makeBook(), checked: true });
    const handler = vi.fn();
    component.$on('toggled', handler);

    await fireEvent.click(screen.getByRole('checkbox', { name: 'Select book.epub' }));

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail).toBe(false);
  });
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser/desktop/frontend && npx vitest run src/lib/BookCard.test.ts`
Expected: the 3 pre-existing tests PASS; the 3 new tests FAIL (no checkbox with an accessible name "Select book.epub" exists yet).

- [ ] **Step 3: Implement the checkbox in BookCard**

Replace `desktop/frontend/src/lib/BookCard.svelte` in full:

```svelte
<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import type { BookView } from './types';

  export let book: BookView;
  export let checked: boolean = true;

  const dispatch = createEventDispatcher<{ edited: BookView; toggled: boolean }>();

  const STATUS_LABEL: Record<string, string> = {
    Metadata: 'Metadata',
    Heuristic: 'Heuristic',
    Edited: 'Edited',
    Partial: 'Needs review',
    Unresolved: 'Unresolved',
  };

  const DUP_LABEL: Record<string, string> = {
    LikelyDuplicate: 'Likely dup',
    PossibleDuplicate: 'Possible dup',
  };

  let debounceHandle: ReturnType<typeof setTimeout> | undefined;

  function scheduleEdit(field: 'title' | 'author' | 'year', value: string) {
    book = { ...book, [field]: { value, source: 'Edited' } };
    if (debounceHandle) clearTimeout(debounceHandle);
    debounceHandle = setTimeout(() => {
      dispatch('edited', book);
    }, 300);
  }

  function toggleChecked(e: Event) {
    dispatch('toggled', (e.target as HTMLInputElement).checked);
  }
</script>

<div class="card">
  <div class="card-header">
    <input
      type="checkbox"
      class="select"
      {checked}
      on:change={toggleChecked}
      aria-label="Select {book.oldFilename}"
    />
    <div class="old-name">{book.oldFilename}</div>
  </div>
  <div class="fields">
    <input
      class="title"
      value={book.title.value}
      on:input={(e) => scheduleEdit('title', (e.target as HTMLInputElement).value)}
    />
    <input
      class="author"
      value={book.author.value}
      on:input={(e) => scheduleEdit('author', (e.target as HTMLInputElement).value)}
    />
    <input
      class="year"
      value={book.year.value}
      on:input={(e) => scheduleEdit('year', (e.target as HTMLInputElement).value)}
    />
  </div>
  <div class="dest-path">→ {book.destPath}</div>
  <div class="badges">
    <span class="pill status-{book.status}">{STATUS_LABEL[book.status] ?? book.status}</span>
    {#if book.duplicateStatus !== 'NotDuplicate'}
      <span class="pill dup">{DUP_LABEL[book.duplicateStatus] ?? book.duplicateStatus}</span>
    {/if}
  </div>
</div>

<style>
  .card {
    background: var(--bf-surface);
    border: 1px solid var(--bf-border);
    border-radius: 10px;
    padding: 10px 14px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .card-header {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .select {
    flex-shrink: 0;
  }
  .old-name {
    font-size: 11px;
    color: var(--bf-text-muted);
  }
  .fields {
    display: flex;
    gap: 8px;
  }
  .fields input {
    padding: 6px 8px;
    border: 1px solid var(--bf-border);
    border-radius: 6px;
    font-size: 13px;
    font-family: inherit;
    color: var(--bf-text);
    background: var(--bf-surface);
  }
  .title { flex: 2; }
  .author { flex: 2; }
  .year { flex: 1; }
  .dest-path {
    font-size: 11.5px;
    color: var(--bf-text-muted);
    word-break: break-word;
  }
  .badges {
    display: flex;
    gap: 6px;
  }
  .pill {
    display: inline-flex;
    padding: 2px 9px;
    border-radius: 100px;
    font-size: 11px;
    font-weight: 600;
  }
  .status-Metadata,
  .status-Edited {
    background: var(--bf-green-soft);
    color: var(--bf-green);
  }
  .status-Heuristic,
  .status-Partial,
  .status-Unresolved {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
  }
  .dup {
    background: var(--bf-blue-soft);
    color: var(--bf-blue);
  }
</style>
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser/desktop/frontend && npx vitest run src/lib/BookCard.test.ts`
Expected: PASS (all 6 tests)

- [ ] **Step 5: Write the failing ScanReviewView checkbox tests**

Add to the end of the `describe('ScanReviewView', ...)` block in `desktop/frontend/src/lib/ScanReviewView.test.ts` (after the existing two `it(...)` blocks, before the closing `});`):

```typescript
  it('defaults every scanned book to checked and includes all of them in Apply', async () => {
    vi.mocked(Scan).mockResolvedValue([makeBook()]);
    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: 'Select book.epub' })).toBeChecked();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => {
      expect(Apply).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(Apply).mock.calls[0][0]).toHaveLength(1);
  });

  it('unchecking a book excludes it from the Apply payload', async () => {
    const bookA = makeBook({ id: '/inbox/a.epub', sourcePath: '/inbox/a.epub', oldFilename: 'a.epub' });
    const bookB = makeBook({ id: '/inbox/b.epub', sourcePath: '/inbox/b.epub', oldFilename: 'b.epub' });
    vi.mocked(Scan).mockResolvedValue([bookA, bookB]);
    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: 'Select a.epub' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('checkbox', { name: 'Select a.epub' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => {
      expect(Apply).toHaveBeenCalledTimes(1);
    });
    const applied = vi.mocked(Apply).mock.calls[0][0];
    expect(applied).toHaveLength(1);
    expect(applied[0].sourcePath).toBe('/inbox/b.epub');
  });

  it('select-all only affects currently visible (filtered) books', async () => {
    const bookA = makeBook({
      id: '/inbox/a.epub',
      sourcePath: '/inbox/a.epub',
      oldFilename: 'a.epub',
      title: { value: 'Atomic Kotlin', source: 'Heuristic' },
    });
    const bookB = makeBook({
      id: '/inbox/b.epub',
      sourcePath: '/inbox/b.epub',
      oldFilename: 'b.epub',
      title: { value: 'Some Other Book', source: 'Heuristic' },
    });
    vi.mocked(Scan).mockResolvedValue([bookA, bookB]);
    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: 'Select a.epub' })).toBeInTheDocument();
    });

    const search = screen.getByPlaceholderText('Search title, author, or filename…');
    await fireEvent.input(search, { target: { value: 'Atomic' } });
    await waitFor(() => {
      expect(screen.queryByRole('checkbox', { name: 'Select b.epub' })).not.toBeInTheDocument();
    });

    // Only "a" is visible now; unchecking select-all must only affect "a".
    await fireEvent.click(screen.getByRole('checkbox', { name: 'Select all' }));

    await fireEvent.input(search, { target: { value: '' } });
    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: 'Select b.epub' })).toBeInTheDocument();
    });

    expect(screen.getByRole('checkbox', { name: 'Select a.epub' })).not.toBeChecked();
    expect(screen.getByRole('checkbox', { name: 'Select b.epub' })).toBeChecked();
  });
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser/desktop/frontend && npx vitest run src/lib/ScanReviewView.test.ts`
Expected: the 2 pre-existing tests PASS; the 3 new tests FAIL (no "Select all"/"Select a.epub" checkboxes exist yet, and `Apply` isn't narrowed by any checked state).

- [ ] **Step 7: Wire checkbox selection into ScanReviewView**

Replace `desktop/frontend/src/lib/ScanReviewView.svelte` in full:

```svelte
<script lang="ts">
  import FilterBar from './FilterBar.svelte';
  import BookCard from './BookCard.svelte';
  import { matchesFilter, matchesQuery, type BookView, type StatusFilter } from './types';
  import { Scan, Recompute, Apply, ConfirmApply } from '../../wailsjs/go/main/App';

  let books: BookView[] = [];
  let query = '';
  let activeFilter: StatusFilter = 'all';
  let scanError = '';
  let applyError = '';
  let scanning = false;
  let applying = false;
  let resultBySourcePath: Record<string, { ok: boolean; error: string; skipped: boolean }> = {};
  let recomputeWarning: Record<string, boolean> = {};
  let checked: Record<string, boolean> = {};

  async function doScan() {
    scanning = true;
    scanError = '';
    resultBySourcePath = {};
    recomputeWarning = {};
    try {
      books = await Scan();
      checked = Object.fromEntries(books.map((b) => [b.sourcePath, true]));
    } catch (e) {
      scanError = String(e);
      books = [];
      checked = {};
    } finally {
      scanning = false;
    }
  }

  async function onEdited(e: CustomEvent<BookView>) {
    const edited = e.detail;
    try {
      const updated = await Recompute(edited);
      books = books.map((b) => (b.sourcePath === updated.sourcePath ? updated : b));
      recomputeWarning = { ...recomputeWarning, [edited.sourcePath]: false };
    } catch (err) {
      // Recompute failed (rare -- mostly I/O): keep the card's last-known-good
      // DestPath by leaving `books` untouched, and flag it with a warning
      // instead of letting the error propagate and blank the card.
      recomputeWarning = { ...recomputeWarning, [edited.sourcePath]: true };
      console.error('Recompute failed for', edited.sourcePath, err);
    }
  }

  function onToggled(sourcePath: string, value: boolean) {
    checked = { ...checked, [sourcePath]: value };
  }

  function toggleAllVisible(e: Event) {
    const value = (e.target as HTMLInputElement).checked;
    const updates: Record<string, boolean> = {};
    for (const b of visibleBooks) {
      updates[b.sourcePath] = value;
    }
    checked = { ...checked, ...updates };
  }

  async function doApply() {
    const eligible = selectedBooks.filter((b) => b.status !== 'Unresolved');
    const confirmed = await ConfirmApply(eligible.length, '');
    if (!confirmed) return;

    applying = true;
    applyError = '';
    try {
      const result = await Apply(selectedBooks);
      const byPath: typeof resultBySourcePath = {};
      for (const r of result.results) {
        byPath[r.sourcePath] = { ok: r.ok, error: r.error, skipped: r.skipped };
      }
      resultBySourcePath = byPath;
    } catch (e) {
      applyError = String(e);
    } finally {
      applying = false;
    }
  }

  $: visibleBooks = books.filter((b) => matchesFilter(b, activeFilter) && matchesQuery(b, query));
  $: selectedBooks = visibleBooks.filter((b) => checked[b.sourcePath]);
  $: allVisibleChecked = visibleBooks.length > 0 && visibleBooks.every((b) => checked[b.sourcePath]);
</script>

<div class="topbar">
  <h2>Scan &amp; Review</h2>
  <div>
    <button class="secondary" on:click={doScan} disabled={scanning}>{scanning ? 'Scanning…' : 'Scan'}</button>
    <button on:click={doApply} disabled={applying || selectedBooks.length === 0}>
      {applying ? 'Applying…' : 'Apply'}
    </button>
  </div>
</div>

{#if scanError}
  <div class="banner error">{scanError}</div>
{/if}
{#if applyError}
  <div class="banner error">{applyError}</div>
{/if}
{#if books.length > 0}
  <FilterBar
    {query}
    {activeFilter}
    on:queryChange={(e) => (query = e.detail)}
    on:filterChange={(e) => (activeFilter = e.detail)}
  />

  <label class="select-all">
    <input type="checkbox" checked={allVisibleChecked} on:change={toggleAllVisible} />
    Select all
  </label>

  <div class="cards">
    {#each visibleBooks as book (book.id)}
      <div class="card-row">
        <BookCard
          {book}
          checked={checked[book.sourcePath]}
          on:edited={onEdited}
          on:toggled={(e) => onToggled(book.sourcePath, e.detail)}
        />
        {#if recomputeWarning[book.sourcePath]}
          <div class="recompute-warning">⚠ couldn't update the destination path — showing the last known value</div>
        {/if}
        {#if resultBySourcePath[book.sourcePath]}
          {@const r = resultBySourcePath[book.sourcePath]}
          <div class="apply-result" class:ok={r.ok} class:error={!r.ok && !r.skipped}>
            {r.skipped ? 'Skipped' : r.ok ? 'Moved ✓' : `Error: ${r.error}`}
          </div>
        {/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .topbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .topbar h2 {
    font-size: 20px;
    font-weight: 800;
    color: var(--bf-text);
    margin: 0;
  }
  button {
    background: var(--bf-blue);
    color: white;
    border: none;
    padding: 9px 18px;
    border-radius: 999px;
    font-weight: 700;
    font-size: 13px;
    font-family: inherit;
    cursor: pointer;
  }
  button.secondary {
    background: var(--bf-blue-soft);
    color: var(--bf-blue);
    margin-right: 8px;
  }
  button:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 13px;
  }
  .select-all {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12.5px;
    color: var(--bf-text-muted);
    cursor: pointer;
    width: fit-content;
  }
  .cards {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .card-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .apply-result {
    font-size: 12px;
    padding: 2px 4px;
  }
  .apply-result.ok {
    color: var(--bf-green);
  }
  .apply-result.error {
    color: var(--bf-amber);
  }
  .recompute-warning {
    font-size: 11.5px;
    color: var(--bf-amber);
  }
</style>
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser/desktop/frontend && npx vitest run src/lib/ScanReviewView.test.ts src/lib/BookCard.test.ts`
Expected: PASS (11 tests total: 5 pre-existing + 6 new)

- [ ] **Step 9: Run the full frontend test suite to check for regressions**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser/desktop/frontend && npm test -- --run`
Expected: all test files pass (was 7 files / 26 tests before this plan; now 7 files / 32 tests)

- [ ] **Step 10: Commit**

```bash
cd /media/francis/Data2/Source/Organisers/book-organiser
git add desktop/frontend/src/lib/BookCard.svelte desktop/frontend/src/lib/BookCard.test.ts desktop/frontend/src/lib/ScanReviewView.svelte desktop/frontend/src/lib/ScanReviewView.test.ts
git commit -m "$(cat <<'EOF'
Add per-item checkbox selection to Scan & Review

Only checked items are sent to Apply -- Apply currently moves every
visible (filtered) book, but testing has shown that's not always
desirable. A fresh Scan defaults every book to checked, matching
today's "apply everything" behavior unless the user opts out. The
select-all checkbox only toggles currently visible (filtered) books,
never ones hidden by the active search/status filter, so narrowing the
list and bulk-toggling never silently touches something you can't see.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Title/Author swap button

**Files:**
- Modify: `desktop/frontend/src/lib/BookCard.svelte`
- Modify: `desktop/frontend/src/lib/BookCard.test.ts`

**Interfaces:**
- Consumes: nothing new.
- Produces: nothing new consumed elsewhere — the swap reuses the existing `edited` event contract.

- [ ] **Step 1: Write the failing test**

Add to the end of the `describe('BookCard', ...)` block in `desktop/frontend/src/lib/BookCard.test.ts`:

```typescript
  it('swap button exchanges title and author, marks both Edited, and dispatches immediately', async () => {
    const { component } = render(BookCard, { book: makeBook() });
    const handler = vi.fn();
    component.$on('edited', handler);

    await fireEvent.click(screen.getByRole('button', { name: 'Swap title and author' }));

    expect(handler).toHaveBeenCalledTimes(1);
    const detail = handler.mock.calls[0][0].detail;
    expect(detail.title.value).toBe('Bruce Eckel, Svetlana Isakova');
    expect(detail.title.source).toBe('Edited');
    expect(detail.author.value).toBe('Atomic Kotlin');
    expect(detail.author.source).toBe('Edited');
    expect(detail.year.value).toBe('2021');
  });
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser/desktop/frontend && npx vitest run src/lib/BookCard.test.ts`
Expected: the 6 pre-existing tests PASS; the new test FAILS (no button named "Swap title and author" exists yet).

- [ ] **Step 3: Add the swap button**

In `desktop/frontend/src/lib/BookCard.svelte`, add a `swapTitleAuthor` function after `scheduleEdit`:

```svelte
  function swapTitleAuthor() {
    if (debounceHandle) clearTimeout(debounceHandle);
    book = {
      ...book,
      title: { value: book.author.value, source: 'Edited' },
      author: { value: book.title.value, source: 'Edited' },
    };
    dispatch('edited', book);
  }
```

Add the button between the title and author inputs in the `.fields` div:

```svelte
  <div class="fields">
    <input
      class="title"
      value={book.title.value}
      on:input={(e) => scheduleEdit('title', (e.target as HTMLInputElement).value)}
    />
    <button type="button" class="swap" on:click={swapTitleAuthor} aria-label="Swap title and author">
      &lt;&nbsp;&gt;
    </button>
    <input
      class="author"
      value={book.author.value}
      on:input={(e) => scheduleEdit('author', (e.target as HTMLInputElement).value)}
    />
    <input
      class="year"
      value={book.year.value}
      on:input={(e) => scheduleEdit('year', (e.target as HTMLInputElement).value)}
    />
  </div>
```

Add `.swap` styling to the `<style>` block, after the existing `.fields input` rule:

```svelte
  .swap {
    flex: 0 0 auto;
    align-self: center;
    background: none;
    border: 1px solid var(--bf-border);
    border-radius: 6px;
    color: var(--bf-text-muted);
    font-family: inherit;
    font-size: 11px;
    padding: 4px 6px;
    cursor: pointer;
  }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser/desktop/frontend && npx vitest run src/lib/BookCard.test.ts`
Expected: PASS (all 7 tests)

- [ ] **Step 5: Run the full frontend test suite to check for regressions**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser/desktop/frontend && npm test -- --run`
Expected: all test files pass (was 7 files / 32 tests after Task 1; now 7 files / 33 tests)

- [ ] **Step 6: Commit**

```bash
cd /media/francis/Data2/Source/Organisers/book-organiser
git add desktop/frontend/src/lib/BookCard.svelte desktop/frontend/src/lib/BookCard.test.ts
git commit -m "$(cat <<'EOF'
Add a title/author swap button to BookCard

A "< >" button between the title and author fields exchanges their
values -- metadata extraction occasionally puts the author in the
title field and vice versa, and this fixes it in one click instead of
manually retyping both. Marks both fields as Edited and dispatches the
same 'edited' event manual typing already uses, so dest path
recomputes exactly like it would from a manual edit. Dispatches
immediately (no debounce) since a swap is one atomic action, not a
stream of keystrokes -- and cancels any pending debounced edit first,
so a stale keystroke-triggered dispatch can't overwrite the swap
afterward.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Open original file

**Files:**
- Modify: `desktop/app.go`
- Modify: `desktop/app_test.go`
- Modify: `desktop/frontend/src/lib/BookCard.svelte`
- Modify: `desktop/frontend/src/lib/BookCard.test.ts`
- Regenerate: `desktop/frontend/wailsjs/go/main/App.js`, `desktop/frontend/wailsjs/go/main/App.d.ts`

**Interfaces:**
- Consumes: `runtime.BrowserOpenURL(ctx context.Context, url string)` (existing, `github.com/wailsapp/wails/v2/pkg/runtime`, already imported in `desktop/app.go`; confirmed signature above in Global Constraints).
- Produces: `(*App).OpenFile(path string) error`, a Wails-bound method used by this task's own frontend step (no later task depends on it).

- [ ] **Step 1: Write the failing Go tests**

Add to `desktop/app_test.go` (needs `os` and `path/filepath` imports added to the existing `import "testing"` line — `net/url` is not needed here, `fileURI`'s use of it lives in `app.go`, not in the test file):

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsAffirmative(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   bool
	}{
		{"macOS custom button label", "Move files", true},
		{"macOS custom undo button label", "Undo", true},
		{"Linux/Windows default Yes (Buttons option is ignored there)", "Yes", true},
		{"generic OK fallback", "OK", true},
		{"macOS custom cancel label", "Cancel", false},
		{"Linux/Windows default No", "No", false},
		{"dialog dismissed with no selection", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAffirmative(tt.result); got != tt.want {
				t.Errorf("isAffirmative(%q) = %v, want %v", tt.result, got, tt.want)
			}
		})
	}
}

func TestFileURI_EscapesSpacesAndParens(t *testing.T) {
	got := fileURI("/library/Atomic Kotlin (2021) - Bruce Eckel.epub")
	want := "file:///library/Atomic%20Kotlin%20%282021%29%20-%20Bruce%20Eckel.epub"
	if got != want {
		t.Errorf("fileURI() = %q, want %q", got, want)
	}
}

func TestOpenFile_NonExistentFileReturnsError(t *testing.T) {
	app := NewApp()
	err := app.OpenFile(filepath.Join(t.TempDir(), "does-not-exist.epub"))
	if err == nil {
		t.Fatal("expected an error for a nonexistent file, got nil")
	}
}

func TestOpenFile_ExistingFilePathIsAccepted(t *testing.T) {
	// This only verifies OpenFile gets past its existence check for a real
	// file without erroring on that check -- it cannot verify the actual
	// runtime.BrowserOpenURL call, which requires a live Wails runtime
	// context this test doesn't have (matching the existing untested-below-
	// the-dialog-call precedent set by ConfirmApply/ConfirmUndo). A nil/zero
	// context is safe here specifically because os.Stat succeeding means
	// fileURI (pure string building) runs, and the real runtime call
	// underneath BrowserOpenURL is a fire-and-forget frontend dispatch that
	// no-ops harmlessly without a real Wails frontend attached to the
	// context -- verified by this test not panicking.
	dir := t.TempDir()
	path := filepath.Join(dir, "book.epub")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	app := NewApp()
	if err := app.OpenFile(path); err != nil {
		t.Errorf("OpenFile(%q) returned error: %v", path, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser && go test ./desktop/... -v`
Expected: `TestIsAffirmative` passes; `TestFileURI_EscapesSpacesAndParens`, `TestOpenFile_NonExistentFileReturnsError`, `TestOpenFile_ExistingFilePathIsAccepted` all FAIL to compile with `undefined: fileURI` / `app.OpenFile undefined (type *App has no field or method OpenFile)`.

- [ ] **Step 3: Implement `fileURI` and `OpenFile`**

In `desktop/app.go`, update the import block:

```go
import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/FrancisChung/book-organiser/internal/appapi"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)
```

Add `OpenFile` immediately after `UndoBatch`:

```go
func (a *App) UndoBatch(batchID string) error {
	return a.api.UndoBatch(batchID)
}

// OpenFile opens the file at path in the OS default application for its
// type. runtime.BrowserOpenURL is fire-and-forget (it returns nothing), so
// the one failure this can actually report is the file no longer existing
// at the recorded path -- a real scenario since Scan's results can go
// stale if the file is moved or deleted outside the app before Open is
// clicked.
func (a *App) OpenFile(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	runtime.BrowserOpenURL(a.ctx, fileURI(path))
	return nil
}

// fileURI converts an absolute filesystem path into a file:// URI, safely
// escaping characters (spaces, parentheses, etc.) that book filenames
// routinely contain but that aren't valid unescaped in a URI.
func fileURI(path string) string {
	u := url.URL{Scheme: "file", Path: path}
	return u.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser && go test ./desktop/... -v`
Expected: PASS — `TestIsAffirmative`'s 7 subtests, plus `TestFileURI_EscapesSpacesAndParens`, `TestOpenFile_NonExistentFileReturnsError`, and `TestOpenFile_ExistingFilePathIsAccepted`.

- [ ] **Step 5: Build and vet**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser && go build ./... && go vet ./...`
Expected: no output, exit code 0

- [ ] **Step 6: Regenerate Wails bindings**

Run:
```bash
cd /media/francis/Data2/Source/Organisers/book-organiser/desktop
wails build -tags webkit2_41
```
Expected: build succeeds; `desktop/frontend/wailsjs/go/main/App.js` and `App.d.ts` now include an `OpenFile` export (`export function OpenFile(arg1:string):Promise<void>;` in the `.d.ts`, and the matching `window['go']['main']['App']['OpenFile']` wrapper in the `.js`). Diff both files to confirm only the `OpenFile` addition appears — no unrelated churn.

- [ ] **Step 7: Write the failing frontend tests**

In `desktop/frontend/src/lib/BookCard.test.ts`, add a Wails binding mock at the top (after the existing imports, before `function makeBook`):

```typescript
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import BookCard from './BookCard.svelte';
import type { BookView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  OpenFile: vi.fn(),
}));

import { OpenFile } from '../../wailsjs/go/main/App';
```

(Note: `waitFor` is a new import here — the existing top of the file only imports `render, fireEvent, screen` from `@testing-library/svelte`; add `waitFor` to that same import.)

Add to the end of the `describe('BookCard', ...)` block:

```typescript
  it('double-clicking the filename opens the original file', async () => {
    vi.mocked(OpenFile).mockResolvedValue(undefined);
    render(BookCard, { book: makeBook() });

    await fireEvent.dblClick(screen.getByText('book.epub'));

    await waitFor(() => {
      expect(OpenFile).toHaveBeenCalledWith('/inbox/book.epub');
    });
  });

  it('shows an error banner when OpenFile rejects', async () => {
    vi.mocked(OpenFile).mockRejectedValue(new Error('no application registered for this file type'));
    render(BookCard, { book: makeBook() });

    await fireEvent.dblClick(screen.getByText('book.epub'));

    await waitFor(() => {
      expect(screen.getByText('Error: no application registered for this file type')).toBeInTheDocument();
    });
  });
```

- [ ] **Step 8: Run tests to verify the new ones fail**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser/desktop/frontend && npx vitest run src/lib/BookCard.test.ts`
Expected: the 7 pre-existing tests PASS; the 2 new tests FAIL (double-clicking the filename doesn't call `OpenFile` yet).

- [ ] **Step 9: Wire up open-file in BookCard**

Replace `desktop/frontend/src/lib/BookCard.svelte` in full:

```svelte
<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import type { BookView } from './types';
  import { OpenFile } from '../../wailsjs/go/main/App';

  export let book: BookView;
  export let checked: boolean = true;

  const dispatch = createEventDispatcher<{ edited: BookView; toggled: boolean }>();

  const STATUS_LABEL: Record<string, string> = {
    Metadata: 'Metadata',
    Heuristic: 'Heuristic',
    Edited: 'Edited',
    Partial: 'Needs review',
    Unresolved: 'Unresolved',
  };

  const DUP_LABEL: Record<string, string> = {
    LikelyDuplicate: 'Likely dup',
    PossibleDuplicate: 'Possible dup',
  };

  let debounceHandle: ReturnType<typeof setTimeout> | undefined;
  let openError = '';

  function scheduleEdit(field: 'title' | 'author' | 'year', value: string) {
    book = { ...book, [field]: { value, source: 'Edited' } };
    if (debounceHandle) clearTimeout(debounceHandle);
    debounceHandle = setTimeout(() => {
      dispatch('edited', book);
    }, 300);
  }

  function toggleChecked(e: Event) {
    dispatch('toggled', (e.target as HTMLInputElement).checked);
  }

  function swapTitleAuthor() {
    if (debounceHandle) clearTimeout(debounceHandle);
    book = {
      ...book,
      title: { value: book.author.value, source: 'Edited' },
      author: { value: book.title.value, source: 'Edited' },
    };
    dispatch('edited', book);
  }

  async function openOriginal() {
    openError = '';
    try {
      await OpenFile(book.sourcePath);
    } catch (e) {
      openError = String(e);
    }
  }
</script>

<div class="card">
  <div class="card-header">
    <input
      type="checkbox"
      class="select"
      {checked}
      on:change={toggleChecked}
      aria-label="Select {book.oldFilename}"
    />
    <div class="old-name" on:dblclick={openOriginal}>{book.oldFilename}</div>
  </div>
  {#if openError}
    <div class="banner error">{openError}</div>
  {/if}
  <div class="fields">
    <input
      class="title"
      value={book.title.value}
      on:input={(e) => scheduleEdit('title', (e.target as HTMLInputElement).value)}
    />
    <button type="button" class="swap" on:click={swapTitleAuthor} aria-label="Swap title and author">
      &lt;&nbsp;&gt;
    </button>
    <input
      class="author"
      value={book.author.value}
      on:input={(e) => scheduleEdit('author', (e.target as HTMLInputElement).value)}
    />
    <input
      class="year"
      value={book.year.value}
      on:input={(e) => scheduleEdit('year', (e.target as HTMLInputElement).value)}
    />
  </div>
  <div class="dest-path">→ {book.destPath}</div>
  <div class="badges">
    <span class="pill status-{book.status}">{STATUS_LABEL[book.status] ?? book.status}</span>
    {#if book.duplicateStatus !== 'NotDuplicate'}
      <span class="pill dup">{DUP_LABEL[book.duplicateStatus] ?? book.duplicateStatus}</span>
    {/if}
  </div>
</div>

<style>
  .card {
    background: var(--bf-surface);
    border: 1px solid var(--bf-border);
    border-radius: 10px;
    padding: 10px 14px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .card-header {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .select {
    flex-shrink: 0;
  }
  .old-name {
    font-size: 11px;
    color: var(--bf-text-muted);
    cursor: pointer;
    text-decoration: underline dotted;
  }
  .old-name:hover {
    color: var(--bf-blue);
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 6px 10px;
    border-radius: 8px;
    font-size: 12px;
  }
  .fields {
    display: flex;
    gap: 8px;
  }
  .fields input {
    padding: 6px 8px;
    border: 1px solid var(--bf-border);
    border-radius: 6px;
    font-size: 13px;
    font-family: inherit;
    color: var(--bf-text);
    background: var(--bf-surface);
  }
  .title { flex: 2; }
  .author { flex: 2; }
  .year { flex: 1; }
  .swap {
    flex: 0 0 auto;
    align-self: center;
    background: none;
    border: 1px solid var(--bf-border);
    border-radius: 6px;
    color: var(--bf-text-muted);
    font-family: inherit;
    font-size: 11px;
    padding: 4px 6px;
    cursor: pointer;
  }
  .dest-path {
    font-size: 11.5px;
    color: var(--bf-text-muted);
    word-break: break-word;
  }
  .badges {
    display: flex;
    gap: 6px;
  }
  .pill {
    display: inline-flex;
    padding: 2px 9px;
    border-radius: 100px;
    font-size: 11px;
    font-weight: 600;
  }
  .status-Metadata,
  .status-Edited {
    background: var(--bf-green-soft);
    color: var(--bf-green);
  }
  .status-Heuristic,
  .status-Partial,
  .status-Unresolved {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
  }
  .dup {
    background: var(--bf-blue-soft);
    color: var(--bf-blue);
  }
</style>
```

- [ ] **Step 10: Run tests to verify they pass**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser/desktop/frontend && npx vitest run src/lib/BookCard.test.ts`
Expected: PASS (all 9 tests)

- [ ] **Step 11: Run the full frontend test suite to check for regressions**

Run: `cd /media/francis/Data2/Source/Organisers/book-organiser/desktop/frontend && npm test -- --run`
Expected: all test files pass (was 7 files / 33 tests after Task 2; now 7 files / 35 tests)

- [ ] **Step 12: Commit**

```bash
cd /media/francis/Data2/Source/Organisers/book-organiser
git add desktop/app.go desktop/app_test.go desktop/frontend/src/lib/BookCard.svelte desktop/frontend/src/lib/BookCard.test.ts desktop/frontend/wailsjs/go/main/App.js desktop/frontend/wailsjs/go/main/App.d.ts
git commit -m "$(cat <<'EOF'
Double-click the original filename to open it in the OS default app

Adds (*App).OpenFile, a thin Wails-bound method (no appapi involvement,
matching ConfirmApply/ConfirmUndo -- this touches neither config, books,
nor the operations log) that opens a file via runtime.BrowserOpenURL.
BrowserOpenURL is fire-and-forget and returns nothing, so the one real
failure OpenFile can report is the file no longer existing at the
recorded path -- checked via os.Stat first, since Scan's results can go
stale if a file is moved or deleted outside the app before Open is
clicked. Paths are converted to a proper file:// URI via net/url rather
than string concatenation, since book filenames routinely contain
spaces and parentheses that need percent-encoding.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Manual end-to-end verification

**Files:** none (verification only)

**Interfaces:** none

- [ ] **Step 1: Build the production binary**

Run:
```bash
cd /media/francis/Data2/Source/Organisers/book-organiser/desktop
./build.sh -tags webkit2_41
```
Expected: build succeeds (this also syncs the repo-root `config.yaml` into the real config location, per `desktop/build.sh`).

- [ ] **Step 2: Run the full test suite one more time end to end**

Run:
```bash
cd /media/francis/Data2/Source/Organisers/book-organiser
go build ./... && go vet ./... && go test ./...
cd desktop/frontend && npm test -- --run
```
Expected: everything passes, matching Tasks 1-3's individual results (35 frontend tests total).

- [ ] **Step 3: Manually exercise all three features**

Launch `desktop/build/bin/desktop` against a config pointed at scratch/test folders (not real library data — set up an isolated sandbox via `XDG_CONFIG_HOME`, matching the precedent from the Undo feature's manual verification). Scan a folder with at least 2 files, then verify:

- Every scanned item's checkbox starts checked.
- Unchecking one item, then clicking Apply, moves only the still-checked items (confirm on disk).
- The "select all" checkbox, after filtering the list via search, only toggles the currently visible items — an item hidden by the filter keeps its previous checked state.
- Clicking the "< >" button between title and author swaps their displayed values, and the destination path updates to match (confirming the Recompute round-trip fired).
- Double-clicking a filename opens the file in the OS default application for its type.
- Double-clicking a filename whose underlying file has been deleted since the scan shows an inline error instead of silently doing nothing.

- [ ] **Step 4: Report results to the user**

No commit for this task — it's verification only. Summarize what was checked and any issues found.
