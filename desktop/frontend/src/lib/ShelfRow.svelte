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
  <div class="shelf-heading">{subcategory}</div>
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
  .shelf-section {
    margin-bottom: 4px;
  }
  .shelf-heading {
    font-size: 13px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--bf-text-muted);
    margin-bottom: 8px;
  }
  .shelf-wrap {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .shelf-row {
    display: flex;
    gap: 12px;
    padding-bottom: 14px;
    border-bottom: 8px solid var(--bf-border);
    overflow-x: auto;
    flex: 1;
    min-width: 0;
  }
  .shelf-nav {
    flex-shrink: 0;
    border: 1px solid var(--bf-border);
    background: var(--bf-surface);
    color: var(--bf-text);
    border-radius: 6px;
    width: 28px;
    height: 28px;
    font-size: 16px;
    line-height: 1;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .shelf-nav:disabled {
    opacity: 0.35;
    cursor: default;
  }
</style>
