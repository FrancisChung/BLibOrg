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
