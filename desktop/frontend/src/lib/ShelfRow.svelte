<script lang="ts">
  import { onMount } from 'svelte';
  import LibraryBookCard from './LibraryBookCard.svelte';
  import type { LibraryBookView } from './types';

  export let subcategory: string;
  export let books: LibraryBookView[];

  let rowEl: HTMLDivElement;
  let atStart = true;
  let atEnd = true;

  function updateEdges() {
    if (!rowEl) return;
    atStart = rowEl.scrollLeft <= 0;
    atEnd = rowEl.scrollLeft + rowEl.clientWidth >= rowEl.scrollWidth;
  }

  function scrollByPage(direction: 1 | -1) {
    if (!rowEl) return;
    rowEl.scrollBy({ left: direction * rowEl.clientWidth * 0.9, behavior: 'smooth' });
  }

  onMount(() => {
    updateEdges();
    window.addEventListener('resize', updateEdges);
    return () => window.removeEventListener('resize', updateEdges);
  });
</script>

<div class="shelf-section">
  <div class="shelf-heading">
    <div>
      <h3>{subcategory}</h3>
      <span>{books.length} {books.length === 1 ? 'book' : 'books'}</span>
    </div>
  </div>
  <div class="shelf-wrap">
    <button
      type="button"
      class="shelf-nav prev"
      aria-label="Scroll shelf left"
      disabled={atStart}
      on:click={() => scrollByPage(-1)}
    >
      ‹
    </button>
    <div class="shelf-row" bind:this={rowEl} on:scroll={updateEdges}>
      {#each books as book (book.sourcePath)}
        <LibraryBookCard {book} />
      {/each}
    </div>
    <button
      type="button"
      class="shelf-nav next"
      aria-label="Scroll shelf right"
      disabled={atEnd}
      on:click={() => scrollByPage(1)}
    >
      ›
    </button>
  </div>
</div>

<style>
  .shelf-section { margin-bottom: 8px; }
  .shelf-heading {
    display: flex;
    align-items: end;
    justify-content: space-between;
    margin-bottom: 10px;
  }
  .shelf-heading > div { display: flex; align-items: baseline; gap: 10px; }
  h3 {
    margin: 0;
    color: #203653;
    font-family: Georgia, serif;
    font-size: 21px;
    letter-spacing: -0.02em;
  }
  .shelf-heading span { color: var(--bf-text-muted); font-size: 12px; }
  .shelf-wrap { position: relative; }
  .shelf-row {
    display: flex;
    gap: 14px;
    padding: 2px 2px 18px;
    border-bottom: 7px solid #D6A665;
    box-shadow: 0 5px 7px rgba(139, 95, 42, 0.14);
    overflow-x: auto;
    flex: 1;
    min-width: 0;
  }
  .shelf-nav {
    flex-shrink: 0;
    border: 1px solid var(--bf-border);
    background: var(--bf-surface);
    color: var(--bf-text);
    border-radius: 50%;
    width: 32px;
    height: 32px;
    font-size: 16px;
    line-height: 1;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    position: absolute;
    top: 40%;
    z-index: 1;
    box-shadow: var(--bf-shadow-sm);
  }
  .shelf-nav.prev { left: -12px; }
  .shelf-nav.next { right: -12px; }
  .shelf-nav:disabled {
    opacity: 0.35;
    cursor: default;
  }
</style>
