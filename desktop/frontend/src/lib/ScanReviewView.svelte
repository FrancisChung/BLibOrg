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
  let debugStep = '';

  async function doScan() {
    scanning = true;
    scanError = '';
    resultBySourcePath = {};
    recomputeWarning = {};
    try {
      books = await Scan();
    } catch (e) {
      scanError = String(e);
      books = [];
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

  async function doApply() {
    debugStep = '1: doApply() called';
    const eligible = visibleBooks.filter((b) => b.status !== 'Unresolved');
    debugStep = `2: calling ConfirmApply(${eligible.length}, '')`;
    const confirmed = await ConfirmApply(eligible.length, '');
    debugStep = `3: ConfirmApply returned ${confirmed}`;
    if (!confirmed) return;

    applying = true;
    applyError = '';
    debugStep = `4: calling Apply() with ${visibleBooks.length} books`;
    try {
      const result = await Apply(visibleBooks);
      debugStep = '5: Apply() resolved';
      const byPath: typeof resultBySourcePath = {};
      for (const r of result.results) {
        byPath[r.sourcePath] = { ok: r.ok, error: r.error, skipped: r.skipped };
      }
      resultBySourcePath = byPath;
    } catch (e) {
      debugStep = '5: Apply() rejected';
      applyError = String(e);
    } finally {
      applying = false;
    }
  }

  $: visibleBooks = books.filter((b) => matchesFilter(b, activeFilter) && matchesQuery(b, query));
</script>

<div class="topbar">
  <h2>Scan &amp; Review</h2>
  <div>
    <button class="secondary" on:click={doScan} disabled={scanning}>{scanning ? 'Scanning…' : 'Scan'}</button>
    <button on:click={doApply} disabled={applying || books.length === 0}>
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
{#if debugStep}
  <div class="debug-step">[debug] {debugStep}</div>
{/if}

{#if books.length > 0}
  <FilterBar
    {query}
    {activeFilter}
    on:queryChange={(e) => (query = e.detail)}
    on:filterChange={(e) => (activeFilter = e.detail)}
  />

  <div class="cards">
    {#each visibleBooks as book (book.id)}
      <div class="card-row">
        <BookCard {book} on:edited={onEdited} />
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
  .debug-step {
    font-family: monospace;
    font-size: 12px;
    background: #222;
    color: #0f0;
    padding: 8px 12px;
    border-radius: 6px;
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
