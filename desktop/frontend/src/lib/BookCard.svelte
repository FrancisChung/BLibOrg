<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import type { BookView } from './types';

  export let book: BookView;

  const dispatch = createEventDispatcher<{ edited: BookView }>();

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

  function scheduleEdit(field: 'title' | 'author' | 'year', value: string) {
    book = { ...book, [field]: { value, source: 'Edited' } };
    if (debounceHandle) clearTimeout(debounceHandle);
    debounceHandle = setTimeout(() => {
      dispatch('edited', book);
    }, 300);
  }
</script>

<div class="card">
  <div class="old-name">{book.oldFilename}</div>
  <div class="fields">
    <input
      class="title"
      value={book.title.value}
      on:input={(e) => scheduleEdit('title', (e.target as HTMLInputElement).value)}
    />
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
  <div class="dest-path">→ {book.destPath}</div>
  <div class="badges">
    <span class="pill status-{book.status}">{STATUS_LABEL[book.status] ?? book.status}</span>
    {#if book.duplicateStatus !== 'NotDuplicate'}
      <span class="pill dup">{DUP_LABEL[book.duplicateStatus] ?? book.duplicateStatus}</span>
    {/if}
  </div>
</div>

<style>
  .card {
    border: 1px solid #ddd;
    border-radius: 8px;
    padding: 10px 14px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .old-name {
    font-size: 11px;
    color: #888;
    text-decoration: line-through;
  }
  .fields {
    display: flex;
    gap: 8px;
  }
  .fields input {
    padding: 6px 8px;
    border: 1px solid #ccc;
    border-radius: 6px;
    font-size: 13px;
  }
  .title { flex: 2; }
  .author { flex: 2; }
  .year { flex: 1; }
  .dest-path {
    font-size: 11.5px;
    color: #666;
    word-break: break-word;
  }
  .badges {
    display: flex;
    gap: 6px;
  }
  .pill {
    display: inline-flex;
    padding: 2px 9px;
    border-radius: 100px;
    font-size: 11px;
    font-weight: 600;
  }
  .status-Metadata { background: #e1f0e5; color: #2f7d53; }
  .status-Heuristic { background: #f5e9d2; color: #9a6b10; }
  .status-Partial { background: #f7e2d3; color: #b4501f; }
  .status-Unresolved { background: #f7e2d3; color: #b4501f; }
  .status-Edited { background: #dceae4; color: #2f6f5e; }
  .dup { background: #f3deea; color: #8c3d68; }
</style>
