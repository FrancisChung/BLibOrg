<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { STATUS_FILTERS, type StatusFilter } from './types';

  export let query: string;
  export let activeFilter: StatusFilter;

  const dispatch = createEventDispatcher<{ queryChange: string; filterChange: StatusFilter }>();

  function onInput(e: Event) {
    const value = (e.target as HTMLInputElement).value;
    dispatch('queryChange', value);
  }

  function selectFilter(key: StatusFilter) {
    dispatch('filterChange', key);
  }
</script>

<div class="filter-bar">
  <input
    class="search"
    type="text"
    placeholder="Search title, author, or filename…"
    value={query}
    on:input={onInput}
  />
  <div class="chips">
    {#each STATUS_FILTERS as f (f.key)}
      <button
        type="button"
        class:active={f.key === activeFilter}
        on:click={() => selectFilter(f.key)}
      >
        {f.label}
      </button>
    {/each}
  </div>
</div>

<style>
  .filter-bar {
    display: flex;
    gap: 10px;
    align-items: center;
    flex-wrap: wrap;
  }
  .search {
    flex: 1;
    min-width: 220px;
    padding: 8px 12px;
    border-radius: 8px;
    border: 1px solid #ccc;
    font-size: 13.5px;
  }
  .chips {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  button {
    border: 1px solid #ccc;
    background: #fff;
    border-radius: 100px;
    padding: 6px 12px;
    font-size: 12.5px;
    font-weight: 600;
    cursor: pointer;
  }
  button.active {
    background: #2f6f5e;
    border-color: #2f6f5e;
    color: #fff;
  }
</style>
