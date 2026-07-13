<script lang="ts">
  import { onMount } from 'svelte';
  import type { CategoryWarningView } from './types';
  import { ListCategoryWarnings } from '../../wailsjs/go/main/App';

  let warnings: CategoryWarningView[] = [];
  let loadError = '';
  let loading = true;

  onMount(async () => {
    try {
      warnings = await ListCategoryWarnings();
    } catch (e) {
      loadError = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<h2>Category Warnings</h2>

{#if loadError}
  <div class="banner error">{loadError}</div>
{:else if loading}
  <p class="empty">Loading…</p>
{:else if warnings.length === 0}
  <p class="empty">No warnings yet.</p>
{:else}
  <div class="rows">
    {#each warnings as w, i (w.sourcePath + i)}
      <div class="row">
        <div class="source">{w.sourcePath}</div>
        <div class="detail">
          {new Date(w.timestamp).toLocaleString()} &middot; {w.category}{w.subcategory ? ` / ${w.subcategory}` : ''}
        </div>
        <div class="warning">{w.warning}</div>
      </div>
    {/each}
  </div>
{/if}

<style>
  h2 {
    font-size: 20px;
    font-weight: 800;
    color: var(--bf-text);
    margin: 0;
  }
  .empty {
    color: var(--bf-text-muted);
    font-size: 13.5px;
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 13px;
  }
  .rows {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .row {
    background: var(--bf-surface);
    border: 1px solid var(--bf-border);
    border-radius: 10px;
    padding: 12px 16px;
  }
  .source {
    font-weight: 700;
    font-size: 13px;
    color: var(--bf-text);
  }
  .detail {
    font-size: 12px;
    color: var(--bf-text-muted);
    margin-top: 2px;
  }
  .warning {
    font-size: 12.5px;
    color: var(--bf-amber);
    margin-top: 6px;
  }
</style>
