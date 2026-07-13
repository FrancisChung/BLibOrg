<script lang="ts">
  import { onMount } from 'svelte';
  import Sidebar from './lib/Sidebar.svelte';
  import ScanReviewView from './lib/ScanReviewView.svelte';
  import OperationsLogView from './lib/OperationsLogView.svelte';
  import WarningsLogView from './lib/WarningsLogView.svelte';
  import type { SidebarView } from './lib/types';
  import { ConfigStatus } from '../wailsjs/go/main/App';

  let activeView: SidebarView = 'scan';
  let configError = '';

  onMount(async () => {
    const status = await ConfigStatus();
    if (status.error) {
      configError = `No usable config at ${status.path}: ${status.error}`;
    }
  });

  function onNavigate(e: CustomEvent<SidebarView>) {
    activeView = e.detail;
  }
</script>

<div class="shell">
  <Sidebar active={activeView} on:navigate={onNavigate} />
  <main>
    {#if configError}
      <div class="banner error">{configError}</div>
    {/if}
    {#if activeView === 'scan'}
      <ScanReviewView />
    {:else if activeView === 'operations'}
      <OperationsLogView />
    {:else if activeView === 'warnings'}
      <WarningsLogView />
    {/if}
  </main>
</div>

<style>
  .shell {
    display: flex;
    min-height: 100vh;
  }
  main {
    flex: 1;
    padding: 24px 28px;
    text-align: left;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 13px;
  }
</style>
