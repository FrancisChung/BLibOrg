# Design: Settings section + cover-cache reset

## Background

Today's cover-cache investigation (the "AI Engineering" wrong-cover bug)
required two backend fixes to self-heal: a version-stamped scan cache
(`internal/librarycache.Entry.CoverVersion`) and content-derived
cache-busting URLs (`internal/covercache`'s `?v=` query). Both are
*automatic* self-healing mechanisms tied to code changes. There's no
deliberate, user-triggered "start completely fresh" action for situations
where the cached cover art is simply wrong for some other reason (a
malformed PDF, a heuristic picking a bad image, etc.) and the user wants a
guaranteed clean rebuild without waiting on a version bump.

The app also has no Settings concept at all today:
`desktop/frontend/src/lib/types.ts`'s `SidebarView` is
`'scan' | 'library' | 'operations' | 'warnings'`, and `Sidebar.svelte`
drives its nav from two small arrays (`topLevelItems`, `logItems`).

## Goal

Add a Settings area to the app, structured so it can grow, containing one
control today: reset the cover cache (cached cover images + the library
scan cache), leaving manual cover overrides untouched, with a confirmation
step before it runs.

## Architecture

### `internal/librarycache`: new `Reset` function

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

### `internal/appapi`: new `ResetCoverCache`

```go
// ResetCoverCache deletes every cached cover image and the persisted
// library scan cache, forcing every book to be treated as new on the next
// Scan. cover-overrides.json is untouched -- this fixes bad
// auto-detection, it doesn't discard deliberate choices. Nothing is
// re-scanned here; the next Library view load or Refresh click naturally
// rebuilds from scratch since nothing is cached.
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

### `desktop/app.go`: confirmation dialog + wrapper

Mirrors the existing `ConfirmApply`/`ConfirmUndo` pattern exactly (same
file, since it's the one place with access to `a.ctx` for
`runtime.MessageDialog`; reuses the existing `isAffirmative` helper):

```go
func (a *App) ResetCoverCache() error {
	return a.api.ResetCoverCache()
}

// ConfirmResetCoverCache shows a native Yes/No dialog before
// ResetCoverCache runs, mirroring ConfirmApply/ConfirmUndo -- clearing
// hundreds of cached files and triggering a multi-minute rebuild on the
// next Library visit is disruptive enough to warrant a confirmation step,
// even though nothing genuinely irreplaceable is lost.
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

`isAffirmative`'s existing switch (`"Move files", "Undo", "Yes", "OK"`)
needs `"Reset"` added, for the same macOS-custom-label-vs-Linux/Windows-
default-Yes/No reason documented on that function today.

### Frontend

- `types.ts`: `SidebarView` gains `'settings'`.
- `Sidebar.svelte`: a new standalone nav item below the Logs section
  (visually separated, e.g. a divider), not nested under Library or Logs
  — it's app maintenance, not a content view:
  ```ts
  const settingsItems: { view: SidebarView; label: string }[] = [
    { view: 'settings', label: 'Settings' },
  ];
  ```
- New `SettingsView.svelte`: a page shell (`<h2>Settings</h2>` matching
  other views' heading style) containing one `<section class="settings-block">`
  per settings area. Today, exactly one block:
  - A heading ("Cover cache") and one sentence of description.
  - A "Reset cover cache" button: on click, calls the Wails-bound
    `ConfirmResetCoverCache()`; if it returns `true`, calls
    `ResetCoverCache()`, shows a success banner ("Cover cache reset. Open
    or refresh the Library view to rebuild it.") or an error banner
    (matching `LibraryView.svelte`'s existing `.banner.error` style) if it
    throws. Button is `disabled` while the call is in flight.
  - This block's shape (title, description, control, result banner) is
    the pattern a second settings feature would copy — not extracted into
    a separate `SettingsSection.svelte` component yet, since one real
    instance doesn't justify the abstraction (YAGNI); the convention is
    what's reusable, not a premature component.
- `App.svelte`: add `{:else if activeView === 'settings'}<SettingsView />` alongside the existing view switch.

## Testing

- `internal/librarycache`: `Reset` on a missing file is a no-op (no
  error); `Reset` after `Save` removes the file, and a subsequent `Load`
  returns an empty cache.
- `internal/appapi`: `ResetCoverCache` removes the covers directory and
  the library-cache.json file; a pre-existing `cover-overrides.json` is
  left byte-for-byte untouched. Config-load failure propagates as an
  error.
- `SettingsView.svelte`: clicking the button when the confirm dialog
  resolves `false` never calls `ResetCoverCache`; when it resolves `true`,
  `ResetCoverCache` is called and a success banner appears; a rejected
  `ResetCoverCache` call shows an error banner instead.
- `desktop/app.go`'s `ConfirmResetCoverCache`/`ResetCoverCache` wrappers
  need no dedicated test, matching `ConfirmApply`/`ConfirmUndo`'s existing
  precedent (thin delegation + a native dialog aren't unit-tested there
  either).

## Non-goals

- No progress indicator or in-Settings rebuild trigger — the reset itself
  is a near-instant filesystem operation; rebuilding is the existing
  Library/Refresh flow's job, unchanged.
- No editing of `config.yaml` fields (library folder, PDF cover page
  limit, etc.) through this Settings view yet — not requested, would be
  scope creep. The page structure (a list of `settings-block` sections)
  is what's designed to grow; no specific future control is being
  pre-built.
- No changes to `cover-overrides.json` or the override picker UI.
