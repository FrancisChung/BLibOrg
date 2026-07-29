<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import ShelfRow from './ShelfRow.svelte';
  import { groupIntoShelves, type LibraryBookView, type LibrarySortMode, type ScanProgress } from './types';
  import { ListLibrary } from '../../wailsjs/go/main/App';
  import { EventsOn } from '../../wailsjs/runtime/runtime';

  export let category: string = '';

  const dispatch = createEventDispatcher<{ categoriesLoaded: string[] }>();

  let books: LibraryBookView[] = [];
  let loadError = '';
  let loading = false;
  let sortMode: LibrarySortMode = 'title';
  let searchQuery = '';
  let subcategoryFilter = '';
  let elapsedSeconds = 0;
  let progress: ScanProgress | null = null;

  onMount(load);

  function formatElapsed(seconds: number): string {
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    const remainder = seconds % 60;
    return `${minutes}m ${remainder}s`;
  }

  async function load(force: boolean = false) {
    loading = true;
    loadError = '';
    elapsedSeconds = 0;
    progress = null;

    const tick = setInterval(() => {
      elapsedSeconds += 1;
    }, 1000);
    const unsubscribe = EventsOn('library:scan-progress', (p: ScanProgress) => {
      progress = p;
    });

    try {
      const view = await ListLibrary(force);
      books = view.books ?? [];
      dispatch('categoriesLoaded', view.categories ?? []);
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
      books = [];
    } finally {
      loading = false;
      clearInterval(tick);
      unsubscribe();
    }
  }

  $: categoryBooks = books.filter((book) => !category || book.category === category);
  $: availableSubcategories = [...new Set(categoryBooks.map((book) => book.subcategory).filter(Boolean))].sort((a, b) =>
    a.localeCompare(b),
  );
  $: if (subcategoryFilter && !availableSubcategories.includes(subcategoryFilter)) subcategoryFilter = '';
  $: filteredBooks = categoryBooks.filter((book) => {
    const query = searchQuery.trim().toLowerCase();
    if (subcategoryFilter && book.subcategory !== subcategoryFilter) return false;
    if (!query) return true;
    return [book.title, book.author, book.category, book.subcategory].some((value) =>
      value.toLowerCase().includes(query),
    );
  });
  $: shelves = groupIntoShelves(filteredBooks, category, sortMode);
  $: viewTitle = category ? category : 'Your Library';
  $: viewSubtitle = category ? `Books filed under ${category}` : 'A calm place for your collection';
  $: loadingMessage = progress
    ? `Loading library… ${progress.done} / ${progress.total} books · ${formatElapsed(elapsedSeconds)}`
    : `Loading library… ${formatElapsed(elapsedSeconds)}`;
</script>

<div class="library">
  <div class="page-heading">
    <div>
      <div class="eyebrow">LIBRARY</div>
      <h2>{viewTitle}</h2>
      <p>{viewSubtitle}</p>
    </div>
    <button type="button" class="primary-action" on:click={() => load(true)} disabled={loading}>
      <span aria-hidden="true">⌁</span>
      {loading ? 'Scanning…' : 'Scan for books'}
    </button>
  </div>

  <div class="toolbar">
    <label class="search-box">
      <span class="search-icon" aria-hidden="true">⌕</span>
      <span class="sr-only">Search your library</span>
      <input bind:value={searchQuery} placeholder="Search your library…" />
    </label>
    <label class="subcategory-picker">
      <span class="filter-icon" aria-hidden="true">≡</span>
      <span class="sr-only">Filter by subcategory</span>
      <select bind:value={subcategoryFilter} aria-label="Filter by subcategory">
        <option value="">All subcategories</option>
        {#each availableSubcategories as subcategory}
          <option value={subcategory}>{subcategory}</option>
        {/each}
      </select>
    </label>
    <div class="toolbar-spacer"></div>
    <div class="sort-label">Sort by</div>
    <div class="sort-toggle" role="group" aria-label="Sort by">
      <button type="button" class:active={sortMode === 'title'} on:click={() => (sortMode = 'title')}>Title</button>
      <button type="button" class:active={sortMode === 'author'} on:click={() => (sortMode = 'author')}>Author</button>
      <button type="button" class:active={sortMode === 'year'} on:click={() => (sortMode = 'year')}>Year</button>
    </div>
    <button type="button" class="refresh" on:click={() => load(true)} disabled={loading} aria-label="Refresh">↻</button>
  </div>

  {#if loadError}
    <div class="banner error">Error: {loadError}</div>
  {/if}
  {#if loading}
    <div class="loading-state">
      <div class="loading-spinner" aria-hidden="true"></div>
      <strong>Refreshing your library</strong>
      <span>{loadingMessage}</span>
    </div>
  {:else if shelves.length === 0}
    <div class="empty-state">
      <div class="empty-icon" aria-hidden="true">▤</div>
      <strong>{searchQuery ? 'No matching books' : 'Your shelves are waiting'}</strong>
      <p>{searchQuery ? 'Try a different title, author, or category.' : 'No books found in the library folder yet.'}</p>
    </div>
  {:else}
    <div class="result-summary"><strong>{filteredBooks.length}</strong> {filteredBooks.length === 1 ? 'book' : 'books'}{searchQuery ? ' matching your search' : ''}</div>
    {#each shelves as shelf (shelf.subcategory)}
      <ShelfRow subcategory={shelf.subcategory} books={shelf.books} />
    {/each}
  {/if}
</div>

<style>
  .library {
    display: flex;
    flex-direction: column;
    gap: 22px;
  }
  .page-heading {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 24px;
  }
  .eyebrow {
    color: var(--bf-gold);
    font-size: 11px;
    font-weight: 800;
    letter-spacing: 0.14em;
  }
  h2 {
    margin: 4px 0 2px;
    color: #172B46;
    font-family: Georgia, serif;
    font-size: clamp(28px, 3vw, 38px);
    letter-spacing: -0.035em;
  }
  .page-heading p {
    margin: 0;
    color: var(--bf-text-muted);
    font-size: 14px;
  }
  .primary-action {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    border: 0;
    border-radius: 9px;
    background: var(--bf-blue);
    color: #fff;
    padding: 11px 16px;
    font-size: 13px;
    font-weight: 700;
    cursor: pointer;
    box-shadow: 0 4px 10px rgba(53, 106, 230, 0.22);
  }
  .primary-action:hover { background: var(--bf-blue-dark); }
  .primary-action:disabled { opacity: 0.65; cursor: default; }
  .toolbar {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 52px;
    padding: 8px;
    border: 1px solid var(--bf-border);
    border-radius: var(--bf-radius-md);
    background: rgba(255, 255, 255, 0.78);
    box-shadow: var(--bf-shadow-sm);
  }
  .search-box {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 220px;
    flex: 1;
    padding: 0 10px;
  }
  .search-icon { color: var(--bf-text-muted); font-size: 24px; line-height: 1; }
  .search-box input {
    min-width: 0;
    flex: 1;
    border: 0;
    outline: 0;
    background: transparent;
    color: var(--bf-text);
    font-size: 13px;
  }
  .search-box input::placeholder { color: #9AA5B5; }
  .subcategory-picker {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    min-width: 180px;
    padding: 0 12px;
    border-left: 1px solid var(--bf-border);
  }
  .filter-icon { color: var(--bf-blue); font-size: 18px; }
  .subcategory-picker select {
    min-width: 0;
    border: 0;
    outline: 0;
    background: transparent;
    color: var(--bf-text);
    font-size: 13px;
    cursor: pointer;
  }
  .toolbar-spacer { flex: 0.2; }
  .sort-label { color: var(--bf-text-muted); font-size: 12px; white-space: nowrap; }
  .refresh {
    width: 34px;
    height: 34px;
    border: 1px solid var(--bf-border);
    background: var(--bf-surface);
    color: var(--bf-text-muted);
    border-radius: 8px;
    font-size: 19px;
    line-height: 1;
    cursor: pointer;
  }
  .refresh:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .sort-toggle {
    display: inline-flex;
    border: 1px solid var(--bf-border);
    border-radius: 8px;
    overflow: hidden;
  }
  .sort-toggle button {
    border: none;
    background: none;
    font-family: inherit;
    padding: 7px 11px;
    font-size: 12px;
    cursor: pointer;
    color: var(--bf-text);
  }
  .sort-toggle button.active {
    background: var(--bf-blue);
    color: white;
  }
  .result-summary { color: var(--bf-text-muted); font-size: 12px; margin: -5px 0 -6px; }
  .result-summary strong { color: var(--bf-text); }
  .loading-state, .empty-state {
    display: flex;
    align-items: center;
    flex-direction: column;
    gap: 8px;
    padding: 70px 20px;
    border: 1px dashed #CBD5E2;
    border-radius: var(--bf-radius-md);
    color: var(--bf-text-muted);
    text-align: center;
  }
  .loading-state strong, .empty-state strong { color: var(--bf-text); font-size: 15px; }
  .loading-state span, .empty-state p { margin: 0; font-size: 13px; }
  .loading-spinner {
    width: 26px;
    height: 26px;
    border: 3px solid var(--bf-blue-soft);
    border-top-color: var(--bf-blue);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  .empty-icon {
    display: grid;
    place-items: center;
    width: 48px;
    height: 48px;
    border-radius: 14px;
    background: var(--bf-gold-soft);
    color: var(--bf-gold);
    font-size: 28px;
  }
  .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (max-width: 760px) {
    .page-heading { align-items: flex-start; flex-direction: column; }
    .toolbar { flex-wrap: wrap; }
    .search-box { flex-basis: 100%; }
    .subcategory-picker { border-left: 0; }
    .toolbar-spacer { display: none; }
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 13px;
  }
</style>
