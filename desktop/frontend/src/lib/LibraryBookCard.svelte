<script lang="ts">
  import type { LibraryBookView } from './types';
  import { OpenFile } from '../../wailsjs/go/main/App';
  import CoverPickerModal from './CoverPickerModal.svelte';

  export let book: LibraryBookView;

  let openError = '';
  let pickerOpen = false;

  function filenameNoExt(sourcePath: string): string {
    const base = sourcePath.split(/[\\/]+/).pop() ?? '';
    const dot = base.lastIndexOf('.');
    return dot > 0 ? base.slice(0, dot) : base;
  }

  async function open() {
    openError = '';
    try {
      await OpenFile(book.sourcePath);
    } catch (e) {
      openError = String(e);
    }
  }

  function onCoverUpdated(e: CustomEvent<{ coverPath: string; coverOverridden: boolean }>) {
    book = { ...book, coverPath: e.detail.coverPath, coverOverridden: e.detail.coverOverridden };
  }
</script>

<div class="tile">
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <!-- svelte-ignore a11y_click_events_have_key_events -- click-to-open is a
       supplementary affordance (like a file manager icon), matching
       BookCard.svelte's openOriginal pattern -->
  <div class="cover" on:click={open} title={filenameNoExt(book.sourcePath)}>
    {#if book.coverPath}
      <img src={book.coverPath} alt={book.title || filenameNoExt(book.sourcePath)} />
    {:else}
      <div class="placeholder">{book.title || filenameNoExt(book.sourcePath)}</div>
    {/if}
    <button
      type="button"
      class="cover-action"
      on:click|stopPropagation={() => (pickerOpen = true)}
    >
      {book.coverOverridden ? 'Change cover…' : 'Choose cover…'}
    </button>
  </div>
  {#if book.coverPath}
    <div class="book-title" title={book.title}>{book.title || filenameNoExt(book.sourcePath)}</div>
  {/if}
  <div class="book-meta">{book.author || 'Unknown author'}</div>
  {#if book.year}<div class="book-year">{book.year}</div>{/if}
  {#if openError}
    <div class="banner error">{openError}</div>
  {/if}
</div>

{#if pickerOpen}
  <CoverPickerModal
    sourcePath={book.sourcePath}
    coverOverridden={book.coverOverridden}
    on:close={() => (pickerOpen = false)}
    on:updated={onCoverUpdated}
  />
{/if}

<style>
  .tile {
    width: 142px;
    flex-shrink: 0;
  }
  .cover {
    position: relative;
    width: 142px;
    height: 204px;
    border-radius: 9px;
    overflow: hidden;
    cursor: pointer;
    box-shadow: 0 5px 12px rgba(24, 43, 70, 0.18);
    background: var(--bf-surface);
    border: 1px solid var(--bf-border);
  }
  .cover img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .placeholder {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: flex-end;
    padding: 10px;
    font-size: 13px;
    line-height: 1.2;
    color: var(--bf-text-muted);
    background: repeating-linear-gradient(
      45deg,
      var(--bf-surface),
      var(--bf-surface) 8px,
      var(--bf-border) 8px,
      var(--bf-border) 16px
    );
  }
  .cover-action {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    padding: 4px 2px;
    font-size: 9.5px;
    line-height: 1.2;
    text-align: center;
    background: rgba(0, 0, 0, 0.65);
    color: white;
    border: none;
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.15s;
  }
  .cover:hover .cover-action {
    opacity: 1;
  }
  .book-title {
    overflow: hidden;
    margin-top: 9px;
    color: var(--bf-text);
    font-size: 12.5px;
    font-weight: 700;
    line-height: 1.25;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .book-meta, .book-year {
    overflow: hidden;
    color: var(--bf-text-muted);
    font-size: 11px;
    line-height: 1.35;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .book-year { margin-top: 1px; }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 4px 6px;
    border-radius: 6px;
    font-size: 10px;
    margin-top: 4px;
  }
</style>
