<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import LibraryBookCard from './LibraryBookCard.svelte';
  import { groupIntoShelves, type LibraryBookView, type LibrarySortMode } from './types';
  import { ListLibrary } from '../../wailsjs/go/main/App';

  export let category: string = '';

  const dispatch = createEventDispatcher<{ categoriesLoaded: string[] }>();

  let books: LibraryBookView[] = [];
  let loadError = '';
  let loading = false;
  let sortMode: LibrarySortMode = 'title';

  onMount(load);

  async function load() {
    loading = true;
    loadError = '';
    try {
      const view = await ListLibrary();
      books = view.books ?? [];
      dispatch('categoriesLoaded', view.categories ?? []);
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
      books = [];
    } finally {
      loading = false;
    }
  }

  $: shelves = groupIntoShelves(books, category, sortMode);
</script>

<div class="library">
  <div class="topbar">
    <h2>{category ? `Library — ${category}` : 'Library — All categories'}</h2>
    <div class="sort-toggle" role="group" aria-label="Sort by">
      <button type="button" class:active={sortMode === 'title'} on:click={() => (sortMode = 'title')}>Title</button>
      <button type="button" class:active={sortMode === 'author'} on:click={() => (sortMode = 'author')}>Author</button>
      <button type="button" class:active={sortMode === 'year'} on:click={() => (sortMode = 'year')}>Year</button>
    </div>
  </div>

  {#if loadError}
    <div class="banner error">Error: {loadError}</div>
  {/if}
  {#if loading}
    <p>Loading library…</p>
  {:else if shelves.length === 0}
    <p class="empty">No books found in the library folder yet.</p>
  {:else}
    {#each shelves as shelf (shelf.subcategory)}
      <div class="shelf-section">
        <div class="shelf-heading">{shelf.subcategory}</div>
        <div class="shelf-row">
          {#each shelf.books as book (book.sourcePath)}
            <LibraryBookCard {book} />
          {/each}
        </div>
      </div>
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
  .shelf-heading {
    font-size: 13px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--bf-text-muted);
    margin-bottom: 8px;
  }
  .shelf-row {
    display: flex;
    gap: 12px;
    padding-bottom: 14px;
    border-bottom: 8px solid var(--bf-border);
    overflow-x: auto;
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
