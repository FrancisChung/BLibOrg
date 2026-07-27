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

  $: shelves = groupIntoShelves(books, category, sortMode);
  $: loadingMessage = progress
    ? `Loading library… ${progress.done} / ${progress.total} books · ${formatElapsed(elapsedSeconds)}`
    : `Loading library… ${formatElapsed(elapsedSeconds)}`;
</script>

<div class="library">
  <div class="topbar">
    <h2>{category ? `Library — ${category}` : 'Library — All categories'}</h2>
    <div class="topbar-controls">
      <button type="button" class="refresh" on:click={() => load(true)} disabled={loading}>Refresh</button>
      <div class="sort-toggle" role="group" aria-label="Sort by">
        <button type="button" class:active={sortMode === 'title'} on:click={() => (sortMode = 'title')}>Title</button>
        <button type="button" class:active={sortMode === 'author'} on:click={() => (sortMode = 'author')}>Author</button>
        <button type="button" class:active={sortMode === 'year'} on:click={() => (sortMode = 'year')}>Year</button>
      </div>
    </div>
  </div>

  {#if loadError}
    <div class="banner error">Error: {loadError}</div>
  {/if}
  {#if loading}
    <p>{loadingMessage}</p>
  {:else if shelves.length === 0}
    <p class="empty">No books found in the library folder yet.</p>
  {:else}
    {#each shelves as shelf (shelf.subcategory)}
      <ShelfRow subcategory={shelf.subcategory} books={shelf.books} />
    {/each}
  {/if}
</div>

<style>
  .library {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  .topbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .topbar-controls {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .refresh {
    border: 1px solid var(--bf-border);
    background: var(--bf-surface);
    color: var(--bf-text);
    border-radius: 6px;
    padding: 6px 12px;
    font-size: 12.5px;
    font-family: inherit;
    cursor: pointer;
  }
  .refresh:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .sort-toggle {
    display: inline-flex;
    border: 1px solid var(--bf-border);
    border-radius: 6px;
    overflow: hidden;
  }
  .sort-toggle button {
    border: none;
    background: none;
    font-family: inherit;
    padding: 6px 12px;
    font-size: 12.5px;
    cursor: pointer;
    color: var(--bf-text);
  }
  .sort-toggle button.active {
    background: var(--bf-blue);
    color: white;
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 13px;
  }
  .empty {
    color: var(--bf-text-muted);
    font-size: 14px;
  }
</style>
