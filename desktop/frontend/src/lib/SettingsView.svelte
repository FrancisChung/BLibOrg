<script lang="ts">
  import { ConfirmResetCoverCache, ResetCoverCache } from '../../wailsjs/go/main/App';

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
</style>
