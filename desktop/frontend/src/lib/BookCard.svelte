<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import type { BookView, DestinationView } from './types';
  import { OpenFile } from '../../wailsjs/go/main/App';

  export let book: BookView;
  export let checked: boolean = true;
  export let moved: boolean = false;

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

  export let destinations: DestinationView[] = [];

  $: selectedDestinationIndex = destinations.findIndex(
    (d) => d.category === book.category && d.subcategory === book.subcategory,
  );

  function onDestinationChange(e: Event) {
    const idx = Number((e.target as HTMLSelectElement).value);
    const dest = destinations[idx];
    if (!dest) return;
    book = { ...book, category: dest.category, subcategory: dest.subcategory, categoryManual: true };
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

  // destPath is always <libraryFolder>/<category>[/<subcategory>]/<filename>
  // (see rename.BuildPath), so the category/subcategory segment is always
  // the last 1-2 path components before the filename. Discard those parsed
  // segments and use book.category/subcategory as the label instead -- they
  // aren't sanitized on the way into destPath, so this is equivalent, and
  // it keeps the highlighted label consistent with the category pill even
  // if destPath is ever out of sync (e.g. mid-edit recompute).
  function splitDestPath(b: BookView): { prefix: string; folder: string; filename: string } {
    const parts = b.destPath.split(/[\\/]+/).filter(Boolean);
    const filename = parts.pop() ?? '';
    const depth = b.subcategory ? 2 : b.category ? 1 : 0;
    parts.splice(parts.length - depth, depth);
    const folder = b.subcategory ? `${b.category}/${b.subcategory}` : b.category;
    return { prefix: parts.join('/'), folder, filename };
  }

  $: destParts = splitDestPath(book);
</script>

<div class="card" class:moved>
  <div class="card-header">
    <input
      type="checkbox"
      class="select"
      {checked}
      on:change={toggleChecked}
      aria-label="Select {book.oldFilename}"
    />
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <!-- svelte-ignore a11y_click_events_have_key_events -- click-to-open is a
         supplementary affordance (like a file manager icon), not the row's primary
         interaction, so it intentionally has no keyboard equivalent and no button/link role -->
    <div class="old-name" on:click={openOriginal}>{book.oldFilename}</div>
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
  <div class="dest-path">
    →
    {#if destParts.prefix}{destParts.prefix}/{/if}<span
      class="dest-folder"
      class:uncategorized={book.category === 'Uncategorized'}>{destParts.folder}</span
    >{#if destParts.folder}/{/if}{destParts.filename}
  </div>
  <div class="badges">
    <div class="badges-left">
      <span class="pill status-{book.status}">{STATUS_LABEL[book.status] ?? book.status}</span>
      {#if book.duplicateStatus !== 'NotDuplicate'}
        <span class="pill dup">{DUP_LABEL[book.duplicateStatus] ?? book.duplicateStatus}</span>
      {/if}
      {#if book.category === 'Uncategorized'}
        <span class="pill uncategorized">Uncategorized</span>
      {/if}
    </div>
    <select
      class="destination-picker"
      aria-label="Choose a destination"
      value={String(selectedDestinationIndex)}
      on:change={onDestinationChange}
    >
      <option value="-1" disabled>Choose a destination…</option>
      {#each destinations as dest, i}
        <option value={String(i)}>{dest.subcategory ? `${dest.category} / ${dest.subcategory}` : dest.category}</option>
      {/each}
    </select>
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
  .card.moved {
    background: var(--bf-green-soft);
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
  .dest-folder {
    font-weight: 700;
    color: var(--bf-blue);
  }
  .dest-folder.uncategorized {
    color: var(--bf-amber);
  }
  .badges {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
  }
  .badges-left {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .destination-picker {
    padding: 4px 8px;
    border: 1px solid var(--bf-border);
    border-radius: 6px;
    font-size: 11px;
    font-family: inherit;
    color: var(--bf-text);
    background: var(--bf-surface);
    max-width: 220px;
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
  .uncategorized {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
  }
</style>
