<script lang="ts">
  import { onMount } from 'svelte';
  import type { OperationBatchView } from './types';
  import { ListOperationBatches } from '../../wailsjs/go/main/App';

  let batches: OperationBatchView[] = [];
  let loadError = '';
  let loading = true;
  let expanded: Record<string, boolean> = {};

  onMount(async () => {
    try {
      batches = await ListOperationBatches();
    } catch (e) {
      loadError = String(e);
    } finally {
      loading = false;
    }
  });

  function toggle(batchId: string) {
    expanded = { ...expanded, [batchId]: !expanded[batchId] };
  }
</script>

<h2>Operations Log</h2>

{#if loadError}
  <div class="banner error">{loadError}</div>
{:else if loading}
  <p class="empty">Loading…</p>
{:else if batches.length === 0}
  <p class="empty">No operations yet.</p>
{:else}
  <div class="batches">
    {#each batches as batch (batch.batchId)}
      <div class="batch">
        <button type="button" class="batch-row" on:click={() => toggle(batch.batchId)}>
          <div>
            <div class="batch-id">Batch {batch.batchId}</div>
            <div class="batch-meta">
              {new Date(batch.timestamp).toLocaleString()} &middot; {batch.entryCount} file{batch.entryCount === 1 ? '' : 's'} moved{batch.undoneCount > 0 ? ` · ${batch.undoneCount} undone` : ''}
            </div>
          </div>
        </button>
        {#if expanded[batch.batchId]}
          <div class="entries">
            {#each batch.entries as entry}
              <div class="entry">
                <span class="old-path">{entry.oldPath}</span>
                <span class="arrow">→</span>
                <span class="new-path">{entry.newPath}</span>
                {#if entry.undone}<span class="undone-pill">Undone</span>{/if}
              </div>
            {/each}
          </div>
        {/if}
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
  .batches {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .batch {
    background: var(--bf-surface);
    border: 1px solid var(--bf-border);
    border-radius: 10px;
  }
  .batch-row {
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    font-family: inherit;
    padding: 12px 16px;
    cursor: pointer;
  }
  .batch-id {
    font-weight: 700;
    font-size: 13px;
    color: var(--bf-text);
  }
  .batch-meta {
    font-size: 12px;
    color: var(--bf-text-muted);
    margin-top: 2px;
  }
  .entries {
    border-top: 1px solid var(--bf-border);
    padding: 10px 16px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .entry {
    font-size: 12px;
    color: var(--bf-text-muted);
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .old-path {
    text-decoration: line-through;
    color: var(--bf-text-muted);
  }
  .arrow {
    color: var(--bf-border);
  }
  .undone-pill {
    margin-left: auto;
    font-size: 11px;
    font-weight: 700;
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
  }
</style>
