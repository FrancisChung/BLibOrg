<script lang="ts">
  import { onMount } from 'svelte';
  import FilterBar from './lib/FilterBar.svelte';
  import BookCard from './lib/BookCard.svelte';
  import { matchesFilter, matchesQuery, type BookView, type StatusFilter } from './lib/types';
  import { Scan, Recompute, Apply, ConfigStatus, ConfirmApply } from '../wailsjs/go/main/App';

  let books: BookView[] = [];
  let query = '';
  let activeFilter: StatusFilter = 'all';
  let scanError = '';
  let configError = '';
  let scanning = false;
  let applying = false;
  let resultBySourcePath: Record<string, { ok: boolean; error: string; skipped: boolean }> = {};

  onMount(async () => {
    const status = await ConfigStatus();
    if (status.error) {
      configError = `No usable config at ${status.path}: ${status.error}`;
    }
  });

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

  let recomputeWarning: Record<string, boolean> = {};

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
    const eligible = books.filter((b) => b.status !== 'Unresolved');
    const confirmed = await ConfirmApply(eligible.length, '');
    if (!confirmed) return;

    applying = true;
    try {
      const result = await Apply(books);
      const byPath: typeof resultBySourcePath = {};
      for (const r of result.results) {
        byPath[r.sourcePath] = { ok: r.ok, error: r.error, skipped: r.skipped };
      }
      resultBySourcePath = byPath;
    } finally {
      applying = false;
    }
  }

  $: visibleBooks = books.filter((b) => matchesFilter(b, activeFilter) && matchesQuery(b, query));
</script>

<main>
  <header>
    <h1>Book Organiser</h1>
    <button on:click={doScan} disabled={scanning}>{scanning ? 'Scanning…' : 'Scan'}</button>
    <button on:click={doApply} disabled={applying || books.length === 0}>
      {applying ? 'Applying…' : 'Apply'}
    </button>
  </header>

  {#if configError}
    <div class="banner error">{configError}</div>
  {/if}
  {#if scanError}
    <div class="banner error">{scanError}</div>
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
</main>

<style>
  main {
    max-width: 900px;
    margin: 0 auto;
    padding: 24px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  header {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  header h1 {
    flex: 1;
    font-size: 20px;
  }
  .banner.error {
    background: #f7e2d3;
    color: #b4501f;
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
  .apply-result.ok { color: #2f7d53; }
  .apply-result.error { color: #b4501f; }
  .recompute-warning {
    font-size: 11.5px;
    color: #9a6b10;
  }
</style>
