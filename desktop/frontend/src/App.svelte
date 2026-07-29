<script lang="ts">
  import { onMount } from 'svelte';
  import Sidebar from './lib/Sidebar.svelte';
  import ScanReviewView from './lib/ScanReviewView.svelte';
  import LibraryView from './lib/LibraryView.svelte';
  import OperationsLogView from './lib/OperationsLogView.svelte';
  import WarningsLogView from './lib/WarningsLogView.svelte';
  import SettingsView from './lib/SettingsView.svelte';
  import type { SidebarView } from './lib/types';
  import { ConfigStatus } from '../wailsjs/go/main/App';

  let activeView: SidebarView = 'library';
  let configError = '';
  let configWarnings: string[] = [];
  let libraryCategories: string[] = [];
  let activeLibraryCategory = '';

  const SIDEBAR_WIDTH_KEY = 'sidebarWidth';
  const SIDEBAR_MIN_WIDTH = 160;
  const SIDEBAR_MAX_WIDTH = 400;
  const SIDEBAR_DEFAULT_WIDTH = 220;

  function loadSidebarWidth(): number {
    const stored = Number(localStorage.getItem(SIDEBAR_WIDTH_KEY));
    return stored >= SIDEBAR_MIN_WIDTH && stored <= SIDEBAR_MAX_WIDTH ? stored : SIDEBAR_DEFAULT_WIDTH;
  }

  let sidebarWidth = loadSidebarWidth();
  let resizingSidebar = false;

  onMount(async () => {
    const status = await ConfigStatus();
    if (status.error) {
      configError = `No usable config at ${status.path}: ${status.error}`;
    }
    configWarnings = status.warnings ?? [];
  });

  function onNavigate(e: CustomEvent<SidebarView>) {
    activeView = e.detail;
  }

  function onSelectCategory(e: CustomEvent<string>) {
    activeLibraryCategory = e.detail;
  }

  function onCategoriesLoaded(e: CustomEvent<string[]>) {
    libraryCategories = e.detail;
  }

  function onResizeMouseDown() {
    resizingSidebar = true;
    window.addEventListener('mousemove', onResizeMouseMove);
    window.addEventListener('mouseup', onResizeMouseUp);
  }

  function onResizeMouseMove(e: MouseEvent) {
    sidebarWidth = Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, e.clientX));
  }

  function onResizeMouseUp() {
    resizingSidebar = false;
    window.removeEventListener('mousemove', onResizeMouseMove);
    window.removeEventListener('mouseup', onResizeMouseUp);
    localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth));
  }
</script>

<div class="shell">
  <Sidebar
    active={activeView}
    {libraryCategories}
    {activeLibraryCategory}
    width={sidebarWidth}
    on:navigate={onNavigate}
    on:selectCategory={onSelectCategory}
  />
  <div
    class="resize-handle"
    class:resizing={resizingSidebar}
    role="separator"
    aria-orientation="vertical"
    aria-label="Resize sidebar"
    on:mousedown={onResizeMouseDown}
  ></div>
  <main>
    {#if configError}
      <div class="banner error">{configError}</div>
    {/if}
    {#each configWarnings as warning}
      <div class="banner warning">Config: {warning}</div>
    {/each}
    {#if activeView === 'scan'}
      <ScanReviewView />
    {:else if activeView === 'library'}
      <LibraryView category={activeLibraryCategory} on:categoriesLoaded={onCategoriesLoaded} />
    {:else if activeView === 'operations'}
      <OperationsLogView />
    {:else if activeView === 'warnings'}
      <WarningsLogView />
    {:else if activeView === 'settings'}
      <SettingsView />
    {/if}
  </main>
</div>

<style>
  .shell {
    display: flex;
    min-height: 100vh;
  }
  .resize-handle {
    width: 6px;
    flex-shrink: 0;
    cursor: col-resize;
    position: sticky;
    top: 0;
    align-self: flex-start;
    height: 100vh;
  }
  .resize-handle:hover::after,
  .resize-handle.resizing::after {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    left: 2px;
    width: 2px;
    background: var(--bf-blue);
  }
  main {
    flex: 1;
    min-width: 0;
    padding: 30px 36px 44px;
    text-align: left;
    display: flex;
    flex-direction: column;
    gap: 16px;
    background: var(--bf-bg);
  }
  .banner.error,
  .banner.warning {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 13px;
  }
</style>
