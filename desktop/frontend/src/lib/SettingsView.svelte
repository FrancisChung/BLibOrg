<script lang="ts">
  import { onMount } from 'svelte';
  import {
    ConfirmResetCoverCache,
    ResetCoverCache,
    GetScanConcurrency,
    SetScanConcurrency,
  } from '../../wailsjs/go/main/App';

  let resetting = false;
  let resetError = '';
  let resetSuccess = '';

  async function handleResetCoverCache() {
    const confirmed = await ConfirmResetCoverCache();
    if (!confirmed) return;

    resetting = true;
    resetError = '';
    resetSuccess = '';
    try {
      await ResetCoverCache();
      resetSuccess = 'Cover cache reset. Open or refresh the Library view to rebuild it.';
    } catch (e) {
      resetError = e instanceof Error ? e.message : String(e);
    } finally {
      resetting = false;
    }
  }

  let concurrencyLoaded = false;
  let concurrencyValue = 0;
  let concurrencySaving = false;
  let concurrencyError = '';
  let concurrencySuccess = '';

  onMount(async () => {
    try {
      const view = await GetScanConcurrency();
      concurrencyValue = view.configured > 0 ? view.configured : view.detected;
    } catch (e) {
      concurrencyError = e instanceof Error ? e.message : String(e);
    } finally {
      concurrencyLoaded = true;
    }
  });

  async function handleSaveConcurrency() {
    concurrencySaving = true;
    concurrencyError = '';
    concurrencySuccess = '';
    try {
      await SetScanConcurrency(concurrencyValue);
      concurrencySuccess = 'Saved. Takes effect on the next Library refresh.';
    } catch (e) {
      concurrencyError = e instanceof Error ? e.message : String(e);
    } finally {
      concurrencySaving = false;
    }
  }
</script>

<h2>Settings</h2>

<section class="settings-block">
  <h3>Cover cache</h3>
  <p>
    Clears every cached cover image and the library scan cache, so every book
    is re-detected from scratch the next time you open or refresh the
    Library. Covers you've manually chosen via "Choose cover" are not
    affected.
  </p>
  {#if resetError}
    <div class="banner error">{resetError}</div>
  {/if}
  {#if resetSuccess}
    <div class="banner success">{resetSuccess}</div>
  {/if}
  <button
    type="button"
    class="reset-button"
    disabled={resetting}
    on:click={handleResetCoverCache}
  >
    {resetting ? 'Resetting…' : 'Reset cover cache'}
  </button>
</section>

<section class="settings-block">
  <h3>Library scan concurrency</h3>
  <p>
    How many books are processed in parallel during a Library refresh.
    Pre-filled with your machine's detected core count; lower it if a
    full-speed refresh competes too much with other work.
  </p>
  {#if concurrencyError}
    <div class="banner error">{concurrencyError}</div>
  {/if}
  {#if concurrencySuccess}
    <div class="banner success">{concurrencySuccess}</div>
  {/if}
  {#if concurrencyLoaded}
    <div class="concurrency-row">
      <input
        type="number"
        min="0"
        bind:value={concurrencyValue}
        disabled={concurrencySaving}
        aria-label="Scan concurrency"
      />
      <button
        type="button"
        class="reset-button"
        disabled={concurrencySaving}
        on:click={handleSaveConcurrency}
      >
        {concurrencySaving ? 'Saving…' : 'Save'}
      </button>
    </div>
  {/if}
</section>

<style>
  h2 {
    font-size: 20px;
    font-weight: 800;
    color: var(--bf-text);
    margin: 0 0 20px;
  }
  .settings-block {
    background: var(--bf-surface);
    border: 1px solid var(--bf-border);
    border-radius: 10px;
    padding: 16px;
    max-width: 480px;
  }
  .settings-block h3 {
    font-size: 15px;
    font-weight: 700;
    color: var(--bf-text);
    margin: 0 0 8px;
  }
  .settings-block p {
    font-size: 13px;
    color: var(--bf-text-muted);
    margin: 0 0 14px;
    line-height: 1.5;
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 13px;
    margin-bottom: 14px;
  }
  .banner.success {
    background: var(--bf-green-soft);
    color: var(--bf-green);
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 13px;
    margin-bottom: 14px;
  }
  .reset-button {
    background: var(--bf-blue-soft);
    color: var(--bf-blue);
    border: none;
    padding: 8px 16px;
    border-radius: 999px;
    font-weight: 700;
    font-size: 13px;
    font-family: inherit;
    cursor: pointer;
  }
  .reset-button:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .concurrency-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .concurrency-row input {
    width: 70px;
    padding: 6px 8px;
    border-radius: 6px;
    border: 1px solid var(--bf-border);
    background: var(--bf-surface);
    color: var(--bf-text);
    font-family: inherit;
    font-size: 13px;
  }
</style>
