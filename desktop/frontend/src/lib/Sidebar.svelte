<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import type { SidebarView } from './types';
  import appIcon from '../../../build/appicon.png';

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
    { view: 'scan', label: 'Scrub & Move' },
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

  function iconFor(view: SidebarView): string {
    return { library: '', scan: '⌁', operations: '↗', warnings: '△', settings: '⚙' }[view];
  }
</script>

<nav class="sidebar" style="width: {width}px">
  <div class="brand">
    <img class="brand-mark" src={appIcon} alt="" aria-hidden="true" />
    <div>
      <div class="brand-name">BLib<span>Org</span></div>
      <div class="brand-subtitle">Your personal library</div>
    </div>
  </div>

  <div class="nav-section first">Main</div>
  {#each topLevelItems as item (item.view)}
    <button
      type="button"
      class="nav-item"
      class:active={active === item.view}
      on:click={() => go(item.view)}
    >
      {#if item.view === 'library'}
        <img class="nav-icon app-icon" src={appIcon} alt="" aria-hidden="true" />
      {:else if item.view === 'scan'}
        <svg class="nav-icon move-icon" viewBox="0 0 22 18" aria-hidden="true">
          <path d="M1 9h7m-3-3 3 3-3 3M11 6V4h5l2 2h3v8H11z" />
        </svg>
      {:else}
        <span class="nav-icon" aria-hidden="true">{iconFor(item.view)}</span>
      {/if}
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
      <span class="nav-icon" aria-hidden="true">{iconFor(item.view)}</span>
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
      <span class="nav-icon" aria-hidden="true">{iconFor(item.view)}</span>
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
    background: var(--bf-sidebar);
    border-right: 1px solid rgba(255, 255, 255, 0.06);
    padding: 24px 14px 20px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 2px 10px 28px;
  }
  .brand-mark {
    width: 34px;
    height: 34px;
    object-fit: contain;
    border-radius: 10px;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.16);
  }
  .brand-name {
    color: #fff;
    font-family: Georgia, serif;
    font-size: 21px;
    font-weight: 700;
    letter-spacing: -0.03em;
  }
  .brand-name span { color: #E0B46F; }
  .brand-subtitle {
    margin-top: 2px;
    color: var(--bf-sidebar-muted);
    font-size: 10px;
    letter-spacing: 0.02em;
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
    color: var(--bf-sidebar-muted);
    cursor: pointer;
  }
  .nav-sub {
    padding-left: 42px;
    font-size: 13.5px;
  }
  .nav-item.active,
  .nav-sub.active {
    background: rgba(71, 126, 225, 0.30);
    color: #fff;
    box-shadow: inset 3px 0 0 var(--bf-gold);
  }
  .nav-item:hover, .nav-sub:hover { background: rgba(255, 255, 255, 0.08); color: #fff; }
  .nav-icon {
    display: inline-flex;
    width: 22px;
    margin-right: 7px;
    justify-content: center;
    color: currentColor;
    font-size: 18px;
    font-weight: 400;
    vertical-align: -2px;
  }
  .app-icon {
    height: 22px;
    object-fit: contain;
    border-radius: 5px;
    vertical-align: -6px;
  }
  .move-icon {
    height: 18px;
    overflow: visible;
    box-sizing: border-box;
    padding: 2px;
    background: #fff;
    border-radius: 5px;
    stroke: currentColor;
    stroke-linecap: round;
    stroke-linejoin: round;
    stroke-width: 1.5;
  }
  .nav-section {
    padding: 10px 12px 4px;
    font-size: 12px;
    font-weight: 800;
    letter-spacing: 0.03em;
    text-transform: uppercase;
    color: var(--bf-sidebar-muted);
    margin-top: 10px;
  }
  .nav-divider {
    height: 1px;
    background: rgba(255, 255, 255, 0.10);
    margin: 10px 4px;
  }
</style>
