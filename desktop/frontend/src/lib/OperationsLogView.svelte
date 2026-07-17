<script lang="ts">
  import { onMount } from 'svelte';
  import type { OperationBatchView } from './types';
  import { ListOperationBatches, ConfirmUndo, UndoBatch } from '../../wailsjs/go/main/App';

  let batches: OperationBatchView[] = [];
  let loadError = '';
  let undoError = '';
  let loading = true;
  let expanded: Record<string, boolean> = {};
  let undoingBatchId: string | null = null;

  onMount(loadBatches);

  async function loadBatches() {
    try {
      batches = await ListOperationBatches();
    } catch (e) {
      loadError = String(e);
    } finally {
      loading = false;
    }
  }

  function toggle(batchId: string) {
    expanded = { ...expanded, [batchId]: !expanded[batchId] };
  }

  async function handleUndo(batch: OperationBatchView) {
    const remaining = batch.entryCount - batch.undoneCount;
    const confirmed = await ConfirmUndo(remaining);
    if (!confirmed) return;

    undoingBatchId = batch.batchId;
    undoError = '';
    try {
      await UndoBatch(batch.batchId);
    } catch (e) {
      undoError = String(e);
    } finally {
      undoingBatchId = null;
    }
    await loadBatches();
  }
</script>

<h2>Operations Log</h2>

{#if loadError}
  <div class="banner error">{loadError}</div>
{:else if loading}
  <p class="empty">Loading…</p>
{:else}
  {#if undoError}
    <div class="banner error">{undoError}</div>
  {/if}
  {#if batches.length === 0}
    <p class="empty">No operations yet.</p>
  {:else}
    <div class="batches">
      {#each batches as batch (batch.batchId)}
        <div class="batch">
          <div class="batch-row">
            <button type="button" class="batch-toggle" on:click={() => toggle(batch.batchId)}>
              <div class="batch-id">Batch {batch.batchId}</div>
              <div class="batch-meta">
                {new Date(batch.timestamp).toLocaleString()} &middot; {batch.entryCount} file{batch.entryCount === 1 ? '' : 's'} moved{batch.undoneCount > 0 ? ` · ${batch.undoneCount} undone` : ''}
              </div>
            </button>
            {#if batch.undoneCount < batch.entryCount}
              <button
                type="button"
                class="undo-button"
                disabled={undoingBatchId === batch.batchId}
                on:click={() => handleUndo(batch)}
              >
                {undoingBatchId === batch.batchId ? 'Undoing…' : 'Undo'}
              </button>
            {/if}
          </div>
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
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 16px;
  }
  .batch-toggle {
    flex: 1;
    text-align: left;
    background: none;
    border: none;
    font-family: inherit;
    padding: 0;
    cursor: pointer;
  }
  .undo-button {
    flex-shrink: 0;
    background: var(--bf-blue-soft);
    color: var(--bf-blue);
    border: none;
    padding: 6px 14px;
    border-radius: 999px;
    font-weight: 700;
    font-size: 12px;
    font-family: inherit;
    cursor: pointer;
  }
  .undo-button:disabled {
    opacity: 0.6;
    cursor: default;
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
