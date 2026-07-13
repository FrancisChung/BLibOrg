<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import type { SidebarView } from './types';

  export let active: SidebarView;

  const dispatch = createEventDispatcher<{ navigate: SidebarView }>();

  // Array-driven (not hardcoded markup) so a future top-level item (a
  // planned "Catalogue" view) is a one-line addition here, not a rework.
  const topLevelItems: { view: SidebarView; label: string }[] = [
    { view: 'scan', label: 'Scan & Review' },
  ];

  const logItems: { view: SidebarView; label: string }[] = [
    { view: 'operations', label: 'Operations' },
    { view: 'warnings', label: 'Warnings' },
  ];

  function go(view: SidebarView) {
    dispatch('navigate', view);
  }
</script>

<nav class="sidebar">
  <div class="logo">Book Organiser</div>

  {#each topLevelItems as item (item.view)}
    <button
      type="button"
      class="nav-item"
      class:active={active === item.view}
      on:click={() => go(item.view)}
    >
      {item.label}
    </button>
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
</nav>

<style>
  .sidebar {
    width: 220px;
    flex-shrink: 0;
    background: var(--bf-surface);
    border-right: 1px solid var(--bf-border);
    padding: 20px 16px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .logo {
    font-weight: 800;
    font-size: 16px;
    color: var(--bf-text);
    margin-bottom: 22px;
    padding: 0 8px;
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
</style>
