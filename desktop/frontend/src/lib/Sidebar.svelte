<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import type { SidebarView } from './types';

  export let active: SidebarView;
  export let libraryCategories: string[] = [];
  export let activeLibraryCategory: string = '';
  // Mirrors App.svelte's SIDEBAR_DEFAULT_WIDTH -- App always passes width
  // explicitly, so this default only matters for standalone/test renders.
  export let width: number = 220;

  const dispatch = createEventDispatcher<{ navigate: SidebarView; selectCategory: string }>();

  // Array-driven (not hardcoded markup) so a future top-level item is a
  // one-line addition here, not a rework.
  const topLevelItems: { view: SidebarView; label: string }[] = [
    { view: 'library', label: 'Library' },
    { view: 'scan', label: 'Scan & Review' },
  ];

  const logItems: { view: SidebarView; label: string }[] = [
    { view: 'operations', label: 'Operations' },
    { view: 'warnings', label: 'Warnings' },
  ];

  const settingsItems: { view: SidebarView; label: string }[] = [
    { view: 'settings', label: 'Settings' },
  ];

  function go(view: SidebarView) {
    dispatch('navigate', view);
  }

  function selectCategory(category: string) {
    dispatch('navigate', 'library');
    dispatch('selectCategory', category);
  }
</script>

<nav class="sidebar" style="width: {width}px">
  {#each topLevelItems as item (item.view)}
    <button
      type="button"
      class="nav-item"
      class:active={active === item.view}
      on:click={() => go(item.view)}
    >
      {item.label}
    </button>
    {#if item.view === 'library' && libraryCategories.length > 0}
      <button
        type="button"
        class="nav-sub"
        class:active={active === 'library' && activeLibraryCategory === ''}
        on:click={() => selectCategory('')}
      >
        All
      </button>
      {#each libraryCategories as category (category)}
        <button
          type="button"
          class="nav-sub"
          class:active={active === 'library' && activeLibraryCategory === category}
          on:click={() => selectCategory(category)}
        >
          {category}
        </button>
      {/each}
    {/if}
  {/each}

  <div class="nav-section">Logs</div>
  {#each logItems as item (item.view)}
    <button
      type="button"
      class="nav-sub"
      class:active={active === item.view}
      on:click={() => go(item.view)}
    >
      {item.label}
    </button>
  {/each}

  <div class="nav-divider"></div>
  {#each settingsItems as item (item.view)}
    <button
      type="button"
      class="nav-item"
      class:active={active === item.view}
      on:click={() => go(item.view)}
    >
      {item.label}
    </button>
  {/each}
</nav>

<style>
  .sidebar {
    flex-shrink: 0;
    position: sticky;
    top: 0;
    align-self: flex-start;
    height: 100vh;
    overflow-y: auto;
    background: var(--bf-surface);
    border-right: 1px solid var(--bf-border);
    padding: 28px 16px 20px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .nav-item,
  .nav-sub {
    display: block;
    width: 100%;
    text-align: left;
    border: none;
    background: none;
    font-family: inherit;
    padding: 10px 12px;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 600;
    color: var(--bf-text-muted);
    cursor: pointer;
  }
  .nav-sub {
    padding-left: 30px;
    font-size: 13.5px;
  }
  .nav-item.active,
  .nav-sub.active {
    background: var(--bf-blue-soft);
    color: var(--bf-blue);
  }
  .nav-section {
    padding: 10px 12px 4px;
    font-size: 12px;
    font-weight: 800;
    letter-spacing: 0.03em;
    text-transform: uppercase;
    color: var(--bf-text-muted);
    margin-top: 10px;
  }
  .nav-divider {
    height: 1px;
    background: var(--bf-border);
    margin: 10px 4px;
  }
</style>
