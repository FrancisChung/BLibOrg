<script lang="ts">
  import { createEventDispatcher, onMount } from 'svelte';
  import type { CoverCandidateView } from './types';
  import {
    ListPDFCoverCandidates,
    SetCoverOverride,
    SetCoverOverrideCustomFromFile,
    ClearCoverOverride,
    PickCoverImageFile,
  } from '../../wailsjs/go/main/App';

  export let sourcePath: string;
  export let coverOverridden: boolean;

  const dispatch = createEventDispatcher<{
    close: void;
    updated: { coverPath: string; coverOverridden: boolean };
  }>();

  let candidates: CoverCandidateView[] = [];
  let loading = true;
  let busy = false;
  let error = '';

  onMount(load);

  async function load() {
    loading = true;
    error = '';
    try {
      candidates = await ListPDFCoverCandidates(sourcePath);
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  async function choosePage(page: number) {
    if (busy) return;
    busy = true;
    error = '';
    try {
      const coverPath = await SetCoverOverride(sourcePath, page);
      dispatch('updated', { coverPath, coverOverridden: true });
      dispatch('close');
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function uploadCustom() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      const picked = await PickCoverImageFile();
      if (!picked) return;
      const coverPath = await SetCoverOverrideCustomFromFile(sourcePath, picked);
      dispatch('updated', { coverPath, coverOverridden: true });
      dispatch('close');
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }

  async function resetToAuto() {
    if (busy) return;
    busy = true;
    error = '';
    try {
      const coverPath = await ClearCoverOverride(sourcePath);
      dispatch('updated', { coverPath, coverOverridden: false });
      dispatch('close');
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      busy = false;
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="backdrop" on:click={() => dispatch('close')}>
  <div class="modal" on:click|stopPropagation>
    <div class="header">
      <h3>Choose cover</h3>
      <button type="button" class="close" on:click={() => dispatch('close')} aria-label="Close">×</button>
    </div>

    {#if error}
      <div class="banner error">{error}</div>
    {/if}

    {#if loading}
      <p>Loading pages…</p>
    {:else}
      <div class="grid">
        {#each candidates as candidate (candidate.page)}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="tile" on:click={() => choosePage(candidate.page)}>
            <img src={candidate.thumbnailUrl} alt={`Page ${candidate.page}`} />
            <span class="page-label">Page {candidate.page}</span>
          </div>
        {/each}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="tile upload" on:click={uploadCustom}>
          <span>Upload custom image…</span>
        </div>
      </div>
    {/if}

    {#if coverOverridden}
      <button type="button" class="reset" on:click={resetToAuto} disabled={busy}>Reset to auto-detected</button>
    {/if}
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }
  .modal {
    background: var(--bf-surface);
    border-radius: 8px;
    padding: 16px;
    width: min(560px, 90vw);
    max-height: 80vh;
    overflow-y: auto;
  }
  .header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
  }
  .close {
    background: none;
    border: none;
    font-size: 20px;
    cursor: pointer;
    color: var(--bf-text);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(90px, 1fr));
    gap: 10px;
  }
  .tile {
    cursor: pointer;
    border: 1px solid var(--bf-border);
    border-radius: 4px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    align-items: center;
  }
  .tile img {
    width: 100%;
    height: 110px;
    object-fit: cover;
    display: block;
  }
  .page-label {
    font-size: 11px;
    padding: 2px;
    color: var(--bf-text-muted);
  }
  .tile.upload {
    min-height: 110px;
    justify-content: center;
    text-align: center;
    font-size: 12px;
    padding: 8px;
    color: var(--bf-text-muted);
  }
  .reset {
    margin-top: 12px;
    width: 100%;
    padding: 8px;
    border-radius: 6px;
    border: 1px solid var(--bf-border);
    background: none;
    color: var(--bf-text);
    cursor: pointer;
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 8px 10px;
    border-radius: 6px;
    font-size: 12px;
    margin-bottom: 10px;
  }
</style>
