# In-app Undo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a single "Undo" button per batch row in the Operations Log view that reverses that batch's file moves, backed by the already-implemented `operations.Manager.UndoBatch`.

**Architecture:** A thin `appapi.App.UndoBatch(batchID string) error` wraps the existing `operations.Manager.UndoBatch`, a new `desktop.App.ConfirmUndo(fileCount int) bool` shows a native confirm dialog mirroring `ConfirmApply`, and `OperationsLogView.svelte` gets an Undo button per batch that calls confirm → undo → re-fetches the batch list so the UI always reflects exactly what the backend did (including partial failures).

**Tech Stack:** Go (backend, Wails bindings), Svelte + TypeScript (frontend), Vitest + Testing Library (frontend tests), Go `testing` package (backend tests).

## Global Constraints

- Undo only in this pass — no Redo UI (backend `RedoBatch` exists but is out of scope; spec: `docs/superpowers/specs/2026-07-17-undo-feature-design.md`).
- Undo is per-batch only — no per-file undo, no "undo my last Apply" shortcut on the Scan & Review screen. One button, in the Operations Log view.
- Undo must be confirmed via a native dialog before it runs, same UX pattern as `ConfirmApply`.
- `isAffirmative` (in `desktop/app.go`) must recognize `"Undo"` as an affirmative label, or `ConfirmUndo` will repeat the exact cross-platform bug that was just fixed for `ConfirmApply` (custom button labels are macOS-only; other platforms fall back to `"Yes"`/`"No"`).
- `appapi.UndoBatch` returns a single `error` (not a per-file result list) — this is a property of the underlying `Manager.UndoBatch`, not something to work around. The frontend must re-fetch `ListOperationBatches()` after every Undo attempt (success or failure) so the UI reflects real state.
- This environment builds with `wails build -tags webkit2_41` (Ubuntu 24.04/webkit2gtk-4.1; see README Prerequisites). Generated Wails bindings under `desktop/frontend/wailsjs/` are committed to the repo (established convention — see commit `f521d74`) and must be regenerated whenever the Go `App` struct's bound methods change.
- TDD throughout: write the failing test, watch it fail, write minimal code, watch it pass, commit.

---

### Task 1: `appapi.App.UndoBatch`

**Files:**
- Create: `internal/appapi/undo.go`
- Test: `internal/appapi/undo_test.go`

**Interfaces:**
- Consumes: `operations.NewLog(path string) *operations.Log`, `operations.NewManager(log *operations.Log) *operations.Manager`, `(*operations.Manager).UndoBatch(batchID string) error` (all existing, `internal/operations/manager.go` / `log.go`), `(*App).loadConfig() (config.Config, error)` (existing, `internal/appapi/app.go`), `writeTestConfig(t *testing.T, working, library, logDir string) string` (existing test helper, `internal/appapi/app_test.go`), `App.Apply(books []BookView) (ApplyResult, error)` (existing, `internal/appapi/apply.go`), `Field{Value, Source string}` and `BookView{SourcePath, Title, Author, Year, DestPath}` (existing, `internal/appapi`).
- Produces: `(*App).UndoBatch(batchID string) error` — used by Task 2's Wails binding.

- [ ] **Step 1: Write the failing tests**

Create `internal/appapi/undo_test.go`:

```go
package appapi

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/BLibOrg/internal/operations"
)

func TestUndoBatch_RestoresFileToOriginalLocation(t *testing.T) {
	working := t.TempDir()
	library := filepath.Join(t.TempDir(), "library")
	logDir := filepath.Join(t.TempDir(), "logs")

	srcPath := filepath.Join(working, "book.epub")
	if err := os.WriteFile(srcPath, []byte("x"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	destPath := filepath.Join(library, "Uncategorized", "Foundation (1951) - Isaac Asimov.epub")

	configPath := writeTestConfig(t, working, library, logDir)
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	books := []BookView{
		{
			SourcePath: srcPath,
			Title:      Field{Value: "Foundation", Source: "Edited"},
			Author:     Field{Value: "Isaac Asimov", Source: "Edited"},
			Year:       Field{Value: "1951", Source: "Edited"},
			DestPath:   destPath,
		},
	}
	result, err := app.Apply(books)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if _, err := os.Stat(destPath); err != nil {
		t.Fatalf("expected file at destPath before undo: %v", err)
	}

	if err := app.UndoBatch(result.BatchID); err != nil {
		t.Fatalf("UndoBatch returned error: %v", err)
	}

	if _, err := os.Stat(srcPath); err != nil {
		t.Errorf("expected file restored to %s, stat error: %v", srcPath, err)
	}
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Errorf("expected %s to no longer exist after undo, stat error: %v", destPath, err)
	}
}

func TestUndoBatch_UnknownBatchIDIsNoOp(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"))
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	if err := app.UndoBatch("no-such-batch"); err != nil {
		t.Fatalf("UndoBatch on unknown batch should be a no-op, got error: %v", err)
	}
}

func TestUndoBatch_PropagatesManagerError(t *testing.T) {
	working := t.TempDir()
	library := filepath.Join(t.TempDir(), "library")
	logDir := filepath.Join(t.TempDir(), "logs")

	srcPath := filepath.Join(working, "book.epub")
	if err := os.WriteFile(srcPath, []byte("x"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	destPath := filepath.Join(library, "Uncategorized", "Foundation (1951) - Isaac Asimov.epub")

	configPath := writeTestConfig(t, working, library, logDir)
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	books := []BookView{
		{
			SourcePath: srcPath,
			Title:      Field{Value: "Foundation", Source: "Edited"},
			Author:     Field{Value: "Isaac Asimov", Source: "Edited"},
			Year:       Field{Value: "1951", Source: "Edited"},
			DestPath:   destPath,
		},
	}
	result, err := app.Apply(books)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}

	// Recreate a file at the original source path so undo's attempt to move
	// the file back there fails with ErrDestinationExists instead of
	// silently succeeding or being swallowed.
	if err := os.WriteFile(srcPath, []byte("something else now lives here"), 0644); err != nil {
		t.Fatalf("write blocking fixture: %v", err)
	}

	err = app.UndoBatch(result.BatchID)
	if err == nil {
		t.Fatal("expected UndoBatch to return an error when the original path is occupied")
	}
	if !errors.Is(err, operations.ErrDestinationExists) {
		t.Errorf("expected error to wrap ErrDestinationExists, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg && go test ./internal/appapi/... -run TestUndoBatch -v`
Expected: FAIL with `app.UndoBatch undefined (type *App has no field or method UndoBatch)`

- [ ] **Step 3: Write minimal implementation**

Create `internal/appapi/undo.go`:

```go
package appapi

import (
	"path/filepath"

	"github.com/FrancisChung/BLibOrg/internal/operations"
)

// UndoBatch reverses every not-yet-undone entry in batchID via the existing
// operation log, restoring each file to its original location. It is a
// no-op returning nil for an unknown batch ID or one that's already fully
// undone -- see operations.Manager.UndoBatch for the underlying semantics,
// including why it stops at the first failing entry rather than continuing
// through the rest (each entry's Undone flag is persisted as it succeeds,
// so a retry only re-attempts what's left).
func (a *App) UndoBatch(batchID string) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	opsLog := operations.NewLog(filepath.Join(cfg.General.LogFolder, "ops.jsonl"))
	mgr := operations.NewManager(opsLog)
	return mgr.UndoBatch(batchID)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg && go test ./internal/appapi/... -run TestUndoBatch -v`
Expected: PASS (all 3 subtests)

- [ ] **Step 5: Run the full appapi package test suite to check for regressions**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg && go test ./internal/appapi/...`
Expected: `ok`

- [ ] **Step 6: Commit**

```bash
cd /media/francis/Data2/Source/Organisers/BLibOrg
git add internal/appapi/undo.go internal/appapi/undo_test.go
git commit -m "$(cat <<'EOF'
Add appapi.App.UndoBatch, wrapping operations.Manager.UndoBatch

Thin wrapper following the existing loadConfig/NewLog/NewManager
pattern used by Apply and ListOperationBatches -- no new logic, the
reversal itself already exists and is tested in internal/operations.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Wails binding + confirm dialog (`desktop/app.go`)

**Files:**
- Modify: `desktop/app.go`
- Modify: `desktop/app_test.go`

**Interfaces:**
- Consumes: `(*appapi.App).UndoBatch(batchID string) error` (Task 1), `runtime.MessageDialog` / `runtime.MessageDialogOptions` / `runtime.QuestionDialog` (existing, `github.com/wailsapp/wails/v2/pkg/runtime`, already imported in this file), `isAffirmative(result string) bool` (existing, this file).
- Produces: `(*App).UndoBatch(batchID string) error` and `(*App).ConfirmUndo(fileCount int) bool` — both Wails-bound methods, used by Task 3 (binding regeneration) and Task 4 (frontend).

- [ ] **Step 1: Write the failing test**

In `desktop/app_test.go`, add an `"Undo"` case to the existing `TestIsAffirmative` table (the table currently starts at the `"macOS custom button label", "Move files", true` row):

```go
func TestIsAffirmative(t *testing.T) {
	tests := []struct {
		name   string
		result string
		want   bool
	}{
		{"macOS custom button label", "Move files", true},
		{"macOS custom undo button label", "Undo", true},
		{"Linux/Windows default Yes (Buttons option is ignored there)", "Yes", true},
		{"generic OK fallback", "OK", true},
		{"macOS custom cancel label", "Cancel", false},
		{"Linux/Windows default No", "No", false},
		{"dialog dismissed with no selection", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAffirmative(tt.result); got != tt.want {
				t.Errorf("isAffirmative(%q) = %v, want %v", tt.result, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg && go test ./desktop/... -run TestIsAffirmative -v`
Expected: FAIL on the `"macOS custom undo button label"` subtest: `isAffirmative("Undo") = false, want true`

- [ ] **Step 3: Write minimal implementation**

In `desktop/app.go`, update `isAffirmative`'s case line (currently `case "Move files", "Yes", "OK":`):

```go
func isAffirmative(result string) bool {
	switch result {
	case "Move files", "Undo", "Yes", "OK":
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg && go test ./desktop/... -run TestIsAffirmative -v`
Expected: PASS (all subtests)

- [ ] **Step 5: Add the UndoBatch delegate and ConfirmUndo dialog**

There is no separate test for these two methods themselves (same precedent as `ConfirmApply`, which also has no direct test — it needs a live Wails runtime context; `isAffirmative`, the part with real logic, is what's unit tested). Add both to `desktop/app.go`.

Add the delegate immediately after the existing `ListCategoryWarnings` method:

```go
func (a *App) ListCategoryWarnings() ([]appapi.CategoryWarningView, error) {
	return a.api.ListCategoryWarnings()
}

func (a *App) UndoBatch(batchID string) error {
	return a.api.UndoBatch(batchID)
}
```

Add `ConfirmUndo` immediately after `ConfirmApply` (before the `isAffirmative` function):

```go
// ConfirmUndo shows a native Yes/No dialog before UndoBatch runs, mirroring
// ConfirmApply -- undoing moves files again, and without a Redo UI yet
// there's no quick way back from an accidental click.
func (a *App) ConfirmUndo(fileCount int) bool {
	message := fmt.Sprintf(
		"Move %d file(s) back to their original location?",
		fileCount,
	)
	result, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:          runtime.QuestionDialog,
		Title:         "Undo this batch?",
		Message:       message,
		Buttons:       []string{"Undo", "Cancel"},
		DefaultButton: "Cancel",
	})
	if err != nil {
		return false
	}
	return isAffirmative(result)
}
```

- [ ] **Step 6: Build to verify it compiles**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg && go build ./... && go vet ./...`
Expected: no output, exit code 0

- [ ] **Step 7: Run the full desktop package test suite to check for regressions**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg && go test ./desktop/...`
Expected: `ok`

- [ ] **Step 8: Commit**

```bash
cd /media/francis/Data2/Source/Organisers/BLibOrg
git add desktop/app.go desktop/app_test.go
git commit -m "$(cat <<'EOF'
Bind UndoBatch and add a ConfirmUndo dialog to the desktop app

ConfirmUndo mirrors ConfirmApply's native Yes/No dialog pattern.
isAffirmative gains "Undo" as a recognized affirmative label -- it's
the custom button text this dialog uses on macOS, and without it
ConfirmUndo would repeat the exact cross-platform bug just fixed for
ConfirmApply (custom Buttons are macOS-only; other platforms fall
back to a default "Yes"/"No" dialog).

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: Regenerate Wails bindings

**Files:**
- Modify (generated): `desktop/frontend/wailsjs/go/main/App.js`
- Modify (generated): `desktop/frontend/wailsjs/go/main/App.d.ts`
- Modify (generated, if changed): `desktop/frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: the `App` struct's exported methods from Task 2 (`UndoBatch`, `ConfirmUndo`).
- Produces: `UndoBatch(batchId: string): Promise<void>` and `ConfirmUndo(fileCount: number): Promise<boolean>`, importable from `../../wailsjs/go/main/App` — used by Task 4.

- [ ] **Step 1: Regenerate the bindings**

Run:
```bash
cd /media/francis/Data2/Source/Organisers/BLibOrg/desktop
wails build -tags webkit2_41
```
Expected: build succeeds (this also produces `desktop/build/bin/desktop`, which is gitignored and not part of this commit).

- [ ] **Step 2: Verify the new bindings were generated**

Run: `grep -n "UndoBatch\|ConfirmUndo" /media/francis/Data2/Source/Organisers/BLibOrg/desktop/frontend/wailsjs/go/main/App.js /media/francis/Data2/Source/Organisers/BLibOrg/desktop/frontend/wailsjs/go/main/App.d.ts`
Expected: both files list `UndoBatch` and `ConfirmUndo`, e.g. `App.d.ts` containing:
```
export function ConfirmUndo(arg1:number):Promise<boolean>;
export function UndoBatch(arg1:string):Promise<void>;
```

- [ ] **Step 3: Check what changed**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg && git status --porcelain desktop/frontend/wailsjs`
Expected: `App.js` and `App.d.ts` modified; `models.ts` modified only if it changed (it may not, since neither new method introduces a new named type).

- [ ] **Step 4: Commit**

```bash
cd /media/francis/Data2/Source/Organisers/BLibOrg
git add desktop/frontend/wailsjs
git commit -m "$(cat <<'EOF'
Regenerate Wails bindings for UndoBatch/ConfirmUndo

Produced by wails build, which regenerates desktop/frontend/wailsjs/
from the Go App struct's exported methods -- same convention as
commit f521d74. Needed before OperationsLogView.svelte can import
these two functions.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Undo button in `OperationsLogView.svelte`

**Files:**
- Modify: `desktop/frontend/src/lib/OperationsLogView.svelte`
- Modify: `desktop/frontend/src/lib/OperationsLogView.test.ts`

**Interfaces:**
- Consumes: `ListOperationBatches(): Promise<OperationBatchView[]>` (existing), `ConfirmUndo(fileCount: number): Promise<boolean>` and `UndoBatch(batchId: string): Promise<void>` (Task 3), `OperationBatchView { batchId, timestamp, entryCount, undoneCount, entries }` (existing, `./types`).
- Produces: nothing consumed elsewhere — this is the UI leaf.

- [ ] **Step 1: Write the failing tests**

Replace the top of `desktop/frontend/src/lib/OperationsLogView.test.ts` (the `vi.mock` call and imports) and add five new tests. Full new file content:

```typescript
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import type { OperationBatchView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  ListOperationBatches: vi.fn(),
  ConfirmUndo: vi.fn(),
  UndoBatch: vi.fn(),
}));

import OperationsLogView from './OperationsLogView.svelte';
import { ListOperationBatches, ConfirmUndo, UndoBatch } from '../../wailsjs/go/main/App';

function makeBatch(overrides: Partial<OperationBatchView> = {}): OperationBatchView {
  return {
    batchId: '20260713-1',
    timestamp: '2026-07-13T12:00:00Z',
    entryCount: 1,
    undoneCount: 0,
    entries: [{ oldPath: '/inbox/a.epub', newPath: '/library/a.epub', opType: 'move', undone: false }],
    ...overrides,
  };
}

describe('OperationsLogView', () => {
  it('shows an empty state when there are no batches', async () => {
    vi.mocked(ListOperationBatches).mockResolvedValue([]);
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByText('No operations yet.')).toBeInTheDocument();
    });
  });

  it('renders a batch row with file count and expands to show entries on click', async () => {
    vi.mocked(ListOperationBatches).mockResolvedValue([makeBatch()]);
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByText('Batch 20260713-1')).toBeInTheDocument();
    });
    expect(screen.queryByText('/inbox/a.epub')).not.toBeInTheDocument();

    await fireEvent.click(screen.getByText('Batch 20260713-1'));

    expect(screen.getByText('/inbox/a.epub')).toBeInTheDocument();
    expect(screen.getByText('/library/a.epub')).toBeInTheDocument();
  });

  it('shows an error banner when the load fails', async () => {
    vi.mocked(ListOperationBatches).mockRejectedValue(new Error('boom'));
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByText('Error: boom')).toBeInTheDocument();
    });
  });

  it('hides the Undo button once a batch is fully undone', async () => {
    vi.mocked(ListOperationBatches).mockResolvedValue([makeBatch({ entryCount: 1, undoneCount: 1 })]);
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByText('Batch 20260713-1')).toBeInTheDocument();
    });
    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument();
  });

  it('shows the Undo button when a batch has entries left to undo', async () => {
    vi.mocked(ListOperationBatches).mockResolvedValue([makeBatch()]);
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument();
    });
  });

  it('confirms, undoes the batch, and refreshes the list', async () => {
    vi.mocked(ListOperationBatches)
      .mockResolvedValueOnce([makeBatch()])
      .mockResolvedValueOnce([makeBatch({ undoneCount: 1 })]);
    vi.mocked(ConfirmUndo).mockResolvedValue(true);
    vi.mocked(UndoBatch).mockResolvedValue(undefined);
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }));

    await waitFor(() => {
      expect(ConfirmUndo).toHaveBeenCalledWith(1);
      expect(UndoBatch).toHaveBeenCalledWith('20260713-1');
    });
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument();
    });
  });

  it('does not call UndoBatch when the confirmation is declined', async () => {
    vi.mocked(ListOperationBatches).mockResolvedValue([makeBatch()]);
    vi.mocked(ConfirmUndo).mockResolvedValue(false);
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }));

    await waitFor(() => {
      expect(ConfirmUndo).toHaveBeenCalled();
    });
    expect(UndoBatch).not.toHaveBeenCalled();
  });

  it('shows an error banner when UndoBatch rejects', async () => {
    vi.mocked(ListOperationBatches).mockResolvedValue([makeBatch()]);
    vi.mocked(ConfirmUndo).mockResolvedValue(true);
    vi.mocked(UndoBatch).mockRejectedValue(new Error('boom'));
    render(OperationsLogView);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Undo' }));

    await waitFor(() => {
      expect(screen.getByText('Error: boom')).toBeInTheDocument();
    });
  });
});
```

- [ ] **Step 2: Run tests to verify the new ones fail**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg/desktop/frontend && npx vitest run src/lib/OperationsLogView.test.ts`
Expected: the 3 pre-existing tests still PASS; the 5 new tests FAIL (no "Undo" button exists yet, `ConfirmUndo`/`UndoBatch` never called).

- [ ] **Step 3: Write minimal implementation**

Replace `desktop/frontend/src/lib/OperationsLogView.svelte` in full:

```svelte
<script lang="ts">
  import { onMount } from 'svelte';
  import type { OperationBatchView } from './types';
  import { ListOperationBatches, ConfirmUndo, UndoBatch } from '../../wailsjs/go/main/App';

  let batches: OperationBatchView[] = [];
  let loadError = '';
  let undoError = '';
  let loading = true;
  let expanded: Record<string, boolean> = {};
  let undoingBatchId: string | null = null;

  onMount(loadBatches);

  async function loadBatches() {
    try {
      batches = await ListOperationBatches();
    } catch (e) {
      loadError = String(e);
    } finally {
      loading = false;
    }
  }

  function toggle(batchId: string) {
    expanded = { ...expanded, [batchId]: !expanded[batchId] };
  }

  async function handleUndo(batch: OperationBatchView) {
    const remaining = batch.entryCount - batch.undoneCount;
    const confirmed = await ConfirmUndo(remaining);
    if (!confirmed) return;

    undoingBatchId = batch.batchId;
    undoError = '';
    try {
      await UndoBatch(batch.batchId);
    } catch (e) {
      undoError = String(e);
    } finally {
      undoingBatchId = null;
    }
    await loadBatches();
  }
</script>

<h2>Operations Log</h2>

{#if loadError}
  <div class="banner error">{loadError}</div>
{:else if loading}
  <p class="empty">Loading…</p>
{:else}
  {#if undoError}
    <div class="banner error">{undoError}</div>
  {/if}
  {#if batches.length === 0}
    <p class="empty">No operations yet.</p>
  {:else}
    <div class="batches">
      {#each batches as batch (batch.batchId)}
        <div class="batch">
          <div class="batch-row">
            <button type="button" class="batch-toggle" on:click={() => toggle(batch.batchId)}>
              <div class="batch-id">Batch {batch.batchId}</div>
              <div class="batch-meta">
                {new Date(batch.timestamp).toLocaleString()} &middot; {batch.entryCount} file{batch.entryCount === 1 ? '' : 's'} moved{batch.undoneCount > 0 ? ` · ${batch.undoneCount} undone` : ''}
              </div>
            </button>
            {#if batch.undoneCount < batch.entryCount}
              <button
                type="button"
                class="undo-button"
                disabled={undoingBatchId === batch.batchId}
                on:click={() => handleUndo(batch)}
              >
                {undoingBatchId === batch.batchId ? 'Undoing…' : 'Undo'}
              </button>
            {/if}
          </div>
          {#if expanded[batch.batchId]}
            <div class="entries">
              {#each batch.entries as entry}
                <div class="entry">
                  <span class="old-path">{entry.oldPath}</span>
                  <span class="arrow">→</span>
                  <span class="new-path">{entry.newPath}</span>
                  {#if entry.undone}<span class="undone-pill">Undone</span>{/if}
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
{/if}

<style>
  h2 {
    font-size: 20px;
    font-weight: 800;
    color: var(--bf-text);
    margin: 0;
  }
  .empty {
    color: var(--bf-text-muted);
    font-size: 13.5px;
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 13px;
  }
  .batches {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .batch {
    background: var(--bf-surface);
    border: 1px solid var(--bf-border);
    border-radius: 10px;
  }
  .batch-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 16px;
  }
  .batch-toggle {
    flex: 1;
    text-align: left;
    background: none;
    border: none;
    font-family: inherit;
    padding: 0;
    cursor: pointer;
  }
  .undo-button {
    flex-shrink: 0;
    background: var(--bf-blue-soft);
    color: var(--bf-blue);
    border: none;
    padding: 6px 14px;
    border-radius: 999px;
    font-weight: 700;
    font-size: 12px;
    font-family: inherit;
    cursor: pointer;
  }
  .undo-button:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .batch-id {
    font-weight: 700;
    font-size: 13px;
    color: var(--bf-text);
  }
  .batch-meta {
    font-size: 12px;
    color: var(--bf-text-muted);
    margin-top: 2px;
  }
  .entries {
    border-top: 1px solid var(--bf-border);
    padding: 10px 16px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .entry {
    font-size: 12px;
    color: var(--bf-text-muted);
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .old-path {
    text-decoration: line-through;
    color: var(--bf-text-muted);
  }
  .arrow {
    color: var(--bf-border);
  }
  .undone-pill {
    margin-left: auto;
    font-size: 11px;
    font-weight: 700;
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
  }
</style>
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg/desktop/frontend && npx vitest run src/lib/OperationsLogView.test.ts`
Expected: PASS (all 8 tests)

- [ ] **Step 5: Run the full frontend test suite to check for regressions**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg/desktop/frontend && npm test -- --run`
Expected: all test files pass (was 7 files / 21 tests before this plan; now 7 files / 26 tests)

- [ ] **Step 6: Commit**

```bash
cd /media/francis/Data2/Source/Organisers/BLibOrg
git add desktop/frontend/src/lib/OperationsLogView.svelte desktop/frontend/src/lib/OperationsLogView.test.ts
git commit -m "$(cat <<'EOF'
Add an Undo button per batch to the Operations Log view

Confirms via the new ConfirmUndo dialog, calls UndoBatch, then always
re-fetches ListOperationBatches so the row's Undone pills/counts
reflect exactly what the backend did -- including a partial failure,
since UndoBatch stops at the first failing entry rather than
continuing through the rest. Button is hidden once a batch is fully
undone. The row's toggle is no longer the whole-row button (it can't
nest inside a button alongside the new Undo button), just the
label/meta text -- same click behavior as before.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Manual end-to-end verification

**Files:** none (verification only)

**Interfaces:** none

- [ ] **Step 1: Build the production binary**

Run:
```bash
cd /media/francis/Data2/Source/Organisers/BLibOrg/desktop
wails build -tags webkit2_41
```
Expected: build succeeds.

- [ ] **Step 2: Run the full test suite one more time end to end**

Run:
```bash
cd /media/francis/Data2/Source/Organisers/BLibOrg
go build ./... && go vet ./... && go test ./...
cd desktop/frontend && npm test -- --run
```
Expected: everything passes, matching Tasks 1, 2, and 4's individual results.

- [ ] **Step 3: Manually exercise the feature**

Launch `desktop/build/bin/desktop` against a config pointed at scratch/test folders (not real library data — see the earlier session precedent for setting up an isolated sandbox via `XDG_CONFIG_HOME` if the default `~/.config/BLibOrg/config.yaml` still points at real data). Scan, Apply a batch, go to the Operations tab, click Undo, confirm the dialog, and verify:
- the files actually move back to their original location on disk
- the batch row's file count updates to show fully undone
- the Undo button disappears for that row
- clicking Undo on a batch, then declining the confirmation dialog, leaves the files untouched

- [ ] **Step 4: Report results to the user**

No commit for this task — it's verification only. Summarize what was checked and any issues found.
