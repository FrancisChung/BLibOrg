# Settings Section + Cover-Cache Reset Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Settings area to the desktop app with one control: a confirm-gated "Reset cover cache" action that clears cached cover images and the library scan cache (leaving manual cover overrides untouched), so a user can force a guaranteed clean rebuild without waiting on the automatic version-based self-healing already in place.

**Architecture:** A new `internal/librarycache.Reset` deletes the persisted scan-cache file; a new `appapi.App.ResetCoverCache` (in a new `internal/appapi/settings.go`) combines that with deleting `covercache.Dir`'s contents. `desktop/app.go` gets a thin wrapper plus a native-dialog `ConfirmResetCoverCache`, mirroring the existing `ConfirmApply`/`ConfirmUndo` pattern exactly. The frontend gains a `'settings'` `SidebarView`, a standalone nav item, and a new `SettingsView.svelte` built as a page of `settings-block` sections (one today, more later) rather than a premature reusable component.

**Tech Stack:** Go backend (`internal/librarycache`, `internal/appapi`, `desktop`), Svelte + TypeScript frontend (`desktop/frontend/src/lib`), Wails v2 bindings regenerated via `wails generate module`.

## Global Constraints

- Design spec: `docs/superpowers/specs/2026-07-24-settings-cover-cache-reset-design.md` — read it in full before starting.
- Resetting the cover cache must **never** touch `cover-overrides.json` (manual cover choices survive a reset).
- Reset itself is a near-instant filesystem operation only — it does **not** trigger a re-scan. Rebuilding happens the next time the existing Library view loads or its Refresh button is clicked (unchanged, already-shipped behavior).
- The confirmation dialog follows the exact existing `ConfirmApply`/`ConfirmUndo` pattern in `desktop/app.go`: native `runtime.MessageDialog`, `QuestionDialog` type, custom `Buttons`/`DefaultButton: "Cancel"`, routed through the existing `isAffirmative` helper (which needs a new case added, not a parallel helper).
- Never hand-edit `desktop/frontend/wailsjs/go/main/App.d.ts`, `App.js`, or `desktop/frontend/wailsjs/go/models.ts` — always regenerate via `cd desktop && wails generate module` after a Go-side Wails-bound signature change.
- Run `go build ./...`, `go vet ./...`, and `go test ./...` after every backend task; run `npx vitest run` (from `desktop/frontend`) after every frontend task. All must stay green.

---

### Task 1: `internal/librarycache.Reset`

**Files:**
- Modify: `internal/librarycache/librarycache.go`
- Modify: `internal/librarycache/librarycache_test.go`

**Interfaces:**
- Produces: `func Reset(logFolder string) error` — consumed by Task 2.

- [ ] **Step 1: Write the failing tests**

Append to `internal/librarycache/librarycache_test.go`:

```go
func TestReset_RemovesPersistedCacheFile(t *testing.T) {
	dir := t.TempDir()
	var c Cache
	c.Put("/book.epub", Entry{Title: "Foundation"})
	if err := c.Save(dir); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	if err := Reset(dir); err != nil {
		t.Fatalf("Reset returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "library-cache.json")); !os.IsNotExist(err) {
		t.Errorf("cache file still exists after Reset (stat err = %v), want removed", err)
	}
	reloaded := Load(dir)
	if _, ok := reloaded.Fresh("/book.epub", time.Time{}, 0); ok {
		t.Error("Fresh() = true after Reset, want false (cache should be empty)")
	}
}

func TestReset_MissingFileIsNotAnError(t *testing.T) {
	if err := Reset(t.TempDir()); err != nil {
		t.Errorf("Reset on a directory with no cache file returned error: %v, want nil", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/librarycache/... -run TestReset -v`
Expected: FAIL — `Reset` is undefined.

- [ ] **Step 3: Implement `Reset`**

Add `"errors"` to the existing import block in `internal/librarycache/librarycache.go` (alongside `"encoding/json"`, `"os"`, `"path/filepath"`, `"time"`), then append:

```go
// Reset deletes the persisted cache file entirely, forcing every book to
// be treated as new on the next Scan. A missing file is not an error --
// idempotent, matching Load's own fail-open convention.
func Reset(logFolder string) error {
	err := os.Remove(cachePath(logFolder))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/librarycache/... -v`
Expected: all PASS, including every pre-existing test.

- [ ] **Step 5: Build and vet**

Run: `go build ./... && go vet ./internal/librarycache/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/librarycache/librarycache.go internal/librarycache/librarycache_test.go
git commit -m "Add librarycache.Reset to delete the persisted scan cache"
```

---

### Task 2: `appapi.App.ResetCoverCache`

**Files:**
- Create: `internal/appapi/settings.go`
- Create: `internal/appapi/settings_test.go`

**Interfaces:**
- Consumes: `librarycache.Reset(logFolder string) error` (Task 1), `covercache.Dir(logFolder string) string` (existing, exported), `a.loadConfig() (config.Config, error)` (existing method on `App`).
- Produces: `func (a *App) ResetCoverCache() error` — consumed by Task 3.

- [ ] **Step 1: Write the failing tests**

Create `internal/appapi/settings_test.go`:

```go
package appapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
)

func writeTestConfigForSettings(t *testing.T, logFolder string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(configPath, config.Config{General: config.General{LogFolder: logFolder}}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return configPath
}

func TestResetCoverCache_RemovesCoversDirAndLibraryCache(t *testing.T) {
	logFolder := t.TempDir()
	coversDir := covercache.Dir(logFolder)
	if err := os.MkdirAll(coversDir, 0755); err != nil {
		t.Fatalf("mkdir covers: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coversDir, "abc123.jpg"), []byte("cached-cover"), 0644); err != nil {
		t.Fatalf("write fixture cover: %v", err)
	}
	libraryCachePath := filepath.Join(logFolder, "library-cache.json")
	if err := os.WriteFile(libraryCachePath, []byte(`{"/book.epub":{}}`), 0644); err != nil {
		t.Fatalf("write fixture library cache: %v", err)
	}

	app := NewApp()
	app.configPath = func() (string, error) { return writeTestConfigForSettings(t, logFolder), nil }

	if err := app.ResetCoverCache(); err != nil {
		t.Fatalf("ResetCoverCache returned error: %v", err)
	}

	if _, err := os.Stat(coversDir); !os.IsNotExist(err) {
		t.Errorf("covers dir still exists after reset (stat err = %v), want removed", err)
	}
	if _, err := os.Stat(libraryCachePath); !os.IsNotExist(err) {
		t.Errorf("library-cache.json still exists after reset (stat err = %v), want removed", err)
	}
}

func TestResetCoverCache_LeavesOverridesFileUntouched(t *testing.T) {
	logFolder := t.TempDir()
	overridesPath := filepath.Join(logFolder, "cover-overrides.json")
	overridesContent := []byte(`{"/book.pdf":{"type":"embedded","page":3}}`)
	if err := os.WriteFile(overridesPath, overridesContent, 0644); err != nil {
		t.Fatalf("write fixture overrides: %v", err)
	}

	app := NewApp()
	app.configPath = func() (string, error) { return writeTestConfigForSettings(t, logFolder), nil }

	if err := app.ResetCoverCache(); err != nil {
		t.Fatalf("ResetCoverCache returned error: %v", err)
	}

	got, err := os.ReadFile(overridesPath)
	if err != nil {
		t.Fatalf("read overrides file after reset: %v", err)
	}
	if string(got) != string(overridesContent) {
		t.Errorf("overrides file content = %q, want untouched %q", got, overridesContent)
	}
}

func TestResetCoverCache_MissingFilesIsNotAnError(t *testing.T) {
	logFolder := t.TempDir()
	app := NewApp()
	app.configPath = func() (string, error) { return writeTestConfigForSettings(t, logFolder), nil }

	if err := app.ResetCoverCache(); err != nil {
		t.Errorf("ResetCoverCache on an already-empty log folder returned error: %v, want nil", err)
	}
}

func TestResetCoverCache_PropagatesConfigLoadError(t *testing.T) {
	app := NewApp()
	app.configPath = func() (string, error) { return "", os.ErrNotExist }

	if err := app.ResetCoverCache(); err == nil {
		t.Error("ResetCoverCache returned nil error, want the config-load failure to propagate")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/appapi/... -run TestResetCoverCache -v`
Expected: FAIL — `ResetCoverCache` (and `writeTestConfigForSettings`'s target) is undefined.

- [ ] **Step 3: Implement `ResetCoverCache`**

Create `internal/appapi/settings.go`:

```go
// This file backs the Settings view's maintenance actions -- currently
// just resetting the cover cache.
package appapi

import (
	"os"

	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/librarycache"
)

// ResetCoverCache deletes every cached cover image and the persisted
// library scan cache, forcing every book to be treated as new on the next
// Scan. cover-overrides.json is untouched -- this fixes bad
// auto-detection, it doesn't discard deliberate choices made through the
// cover-override picker. Nothing is re-scanned here; the next Library
// view load or Refresh click naturally rebuilds from scratch since
// nothing is cached.
func (a *App) ResetCoverCache() error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(covercache.Dir(cfg.General.LogFolder)); err != nil {
		return err
	}
	return librarycache.Reset(cfg.General.LogFolder)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/appapi/... -v`
Expected: all PASS, including every pre-existing test.

- [ ] **Step 5: Build and vet**

Run: `go build ./... && go vet ./internal/appapi/...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/appapi/settings.go internal/appapi/settings_test.go
git commit -m "Add appapi.ResetCoverCache to clear cover cache + scan cache"
```

---

### Task 3: `desktop/app.go` wrapper, confirmation dialog, and Wails bindings

**Files:**
- Modify: `desktop/app.go`
- Modify: `desktop/app_test.go`

**Interfaces:**
- Consumes: `appapi.App.ResetCoverCache() error` (Task 2).
- Produces: `func (a *App) ResetCoverCache() error` and `func (a *App) ConfirmResetCoverCache() bool` on the Wails-bound `main.App` — consumed by Task 4's frontend.

- [ ] **Step 1: Write the failing test**

Add one row to the existing table in `TestIsAffirmative` in `desktop/app_test.go` — insert right after the `"macOS custom undo button label"` row:

```go
		{"macOS custom undo button label", "Undo", true},
		{"macOS custom reset button label", "Reset", true},
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./desktop/... -run TestIsAffirmative -v`
Expected: FAIL — the `"Reset"` case reports `false`, want `true`.

- [ ] **Step 3: Add the `ResetCoverCache` wrapper, `ConfirmResetCoverCache`, and the `isAffirmative` case**

In `desktop/app.go`, add the wrapper right after `ClearCoverOverride`:

```go
func (a *App) ResetCoverCache() error {
	return a.api.ResetCoverCache()
}
```

Add `ConfirmResetCoverCache` right after `ConfirmUndo`:

```go
// ConfirmResetCoverCache shows a native Yes/No dialog before
// ResetCoverCache runs, mirroring ConfirmApply/ConfirmUndo -- clearing
// hundreds of cached files and triggering a multi-minute rebuild on the
// next Library visit is disruptive enough to warrant a confirmation
// step, even though nothing genuinely irreplaceable is lost.
func (a *App) ConfirmResetCoverCache() bool {
	result, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:  runtime.QuestionDialog,
		Title: "Reset cover cache?",
		Message: "This clears every cached cover image and the library scan " +
			"cache, so the next time you open or refresh the Library, every " +
			"book will be re-scanned from scratch. This does not affect any " +
			"covers you've manually chosen.",
		Buttons:       []string{"Reset", "Cancel"},
		DefaultButton: "Cancel",
	})
	if err != nil {
		return false
	}
	return isAffirmative(result)
}
```

Update `isAffirmative`'s switch statement:

```go
func isAffirmative(result string) bool {
	switch result {
	case "Move files", "Undo", "Reset", "Yes", "OK":
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./desktop/... -run TestIsAffirmative -v`
Expected: PASS.

- [ ] **Step 5: Regenerate Wails bindings**

Run: `cd desktop && wails generate module`
Expected: exits cleanly; `desktop/frontend/wailsjs/go/main/App.d.ts` and `App.js` now declare `ResetCoverCache()` and `ConfirmResetCoverCache()`. Do not hand-edit these files or `models.ts`.

- [ ] **Step 6: Run the full repo build/vet/test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all PASS, clean vet.

- [ ] **Step 7: Commit**

```bash
git add desktop/app.go desktop/app_test.go \
        desktop/frontend/wailsjs/go/main/App.d.ts desktop/frontend/wailsjs/go/main/App.js \
        desktop/frontend/wailsjs/go/models.ts
git commit -m "Expose ResetCoverCache and a confirmation dialog to the frontend"
```

---

### Task 4: Frontend Settings view

**Files:**
- Modify: `desktop/frontend/src/lib/types.ts`
- Modify: `desktop/frontend/src/lib/Sidebar.svelte`
- Modify: `desktop/frontend/src/lib/Sidebar.test.ts`
- Create: `desktop/frontend/src/lib/SettingsView.svelte`
- Create: `desktop/frontend/src/lib/SettingsView.test.ts`
- Modify: `desktop/frontend/src/App.svelte`

**Interfaces:**
- Consumes: `ConfirmResetCoverCache(): Promise<boolean>`, `ResetCoverCache(): Promise<void>` from `../../wailsjs/go/main/App` (Task 3).
- Produces: `SidebarView` gains `'settings'`; `SettingsView` Svelte component with no props.

- [ ] **Step 1: Add `'settings'` to `SidebarView`**

In `desktop/frontend/src/lib/types.ts`, change:

```ts
export type SidebarView = 'scan' | 'library' | 'operations' | 'warnings';
```

to:

```ts
export type SidebarView = 'scan' | 'library' | 'operations' | 'warnings' | 'settings';
```

- [ ] **Step 2: Write the failing Sidebar tests**

Append to `desktop/frontend/src/lib/Sidebar.test.ts`, inside the existing `describe('Sidebar', ...)` block:

```ts
  it('renders a Settings nav item', () => {
    render(Sidebar, { active: 'operations' });
    expect(screen.getByRole('button', { name: 'Settings' })).toBeInTheDocument();
  });

  it('emits navigate with "settings" when Settings is clicked', async () => {
    const { component } = render(Sidebar, { active: 'library' });
    const handler = vi.fn();
    component.$on('navigate', handler);

    await fireEvent.click(screen.getByRole('button', { name: 'Settings' }));

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail).toBe('settings');
  });
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/Sidebar.test.ts`
Expected: FAIL — no "Settings" button exists yet.

- [ ] **Step 4: Add the Settings nav item to `Sidebar.svelte`**

Add the new array, right after the existing `logItems` declaration:

```ts
  const settingsItems: { view: SidebarView; label: string }[] = [
    { view: 'settings', label: 'Settings' },
  ];
```

Add the divider and nav item to the markup, right after the existing `{#each logItems as item (item.view)}...{/each}` block closes (i.e. as the last thing inside `<nav class="sidebar">`, after the Logs section):

```svelte
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
```

Add the divider style to the existing `<style>` block:

```css
  .nav-divider {
    height: 1px;
    background: var(--bf-border);
    margin: 10px 4px;
  }
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/Sidebar.test.ts`
Expected: all PASS, including every pre-existing test in this file.

- [ ] **Step 6: Write the failing SettingsView tests**

Create `desktop/frontend/src/lib/SettingsView.test.ts`:

```ts
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import SettingsView from './SettingsView.svelte';

vi.mock('../../wailsjs/go/main/App', () => ({
  ConfirmResetCoverCache: vi.fn(),
  ResetCoverCache: vi.fn(),
}));

import { ConfirmResetCoverCache, ResetCoverCache } from '../../wailsjs/go/main/App';

describe('SettingsView', () => {
  it('does not reset when the confirmation dialog is declined', async () => {
    vi.mocked(ConfirmResetCoverCache).mockResolvedValue(false);
    render(SettingsView);

    await fireEvent.click(screen.getByRole('button', { name: 'Reset cover cache' }));

    expect(ResetCoverCache).not.toHaveBeenCalled();
  });

  it('resets and shows a success banner when confirmed', async () => {
    vi.mocked(ConfirmResetCoverCache).mockResolvedValue(true);
    vi.mocked(ResetCoverCache).mockResolvedValue(undefined);
    render(SettingsView);

    await fireEvent.click(screen.getByRole('button', { name: 'Reset cover cache' }));

    expect(ResetCoverCache).toHaveBeenCalled();
    await screen.findByText('Cover cache reset. Open or refresh the Library view to rebuild it.');
  });

  it('shows an error banner when ResetCoverCache rejects', async () => {
    vi.mocked(ConfirmResetCoverCache).mockResolvedValue(true);
    vi.mocked(ResetCoverCache).mockRejectedValue(new Error('permission denied'));
    render(SettingsView);

    await fireEvent.click(screen.getByRole('button', { name: 'Reset cover cache' }));

    await screen.findByText('permission denied');
  });
});
```

- [ ] **Step 7: Run the test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/SettingsView.test.ts`
Expected: FAIL — `SettingsView.svelte` does not exist yet.

- [ ] **Step 8: Write `SettingsView.svelte`**

```svelte
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
```

- [ ] **Step 9: Run the test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/lib/SettingsView.test.ts`
Expected: all PASS.

- [ ] **Step 10: Wire `SettingsView` into `App.svelte`**

Add the import, alongside the existing view imports:

```svelte
  import SettingsView from './lib/SettingsView.svelte';
```

Add the branch, as the last `{:else if}` in the existing view switch (after the `warnings` branch):

```svelte
    {:else if activeView === 'settings'}
      <SettingsView />
```

- [ ] **Step 11: Run the full frontend suite**

Run: `cd desktop/frontend && npx vitest run`
Expected: all files PASS, including every pre-existing test.

- [ ] **Step 12: Commit**

```bash
git add desktop/frontend/src/lib/types.ts desktop/frontend/src/lib/Sidebar.svelte \
        desktop/frontend/src/lib/Sidebar.test.ts desktop/frontend/src/lib/SettingsView.svelte \
        desktop/frontend/src/lib/SettingsView.test.ts desktop/frontend/src/App.svelte
git commit -m "Add Settings view with a cover-cache reset control"
```

---

## Final step: whole-branch review

After Task 4, follow this repo's established pattern for finishing a small SDD sequence:

1. Run a final whole-branch code review across the full diff from this work's starting commit to `HEAD`.
2. Fix any Important/Critical findings; Minor findings may be accepted as-is with a rationale, matching this repo's established convention.
3. Merge to `main`.
