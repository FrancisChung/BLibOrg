# Sidebar Shell + Operations/Warnings Log Views Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persistent BookFusion-styled sidebar (Scan & Review, plus a Logs section with Operations and Warnings) and two new read-only views over the existing `ops.jsonl` and `category-warnings.jsonl` log files.

**Architecture:** Two new read-only `appapi` methods backed by the existing `operations.Log` and a new `pipeline.ReadCategoryWarnings` reader; `App.svelte` becomes a thin shell owning sidebar navigation state, with the current scan/review body extracted into its own `ScanReviewView.svelte` alongside two new view components.

**Tech Stack:** Go 1.25 (backend, unchanged), Svelte 5 + TypeScript (frontend, unchanged). No new dependencies.

Spec: `docs/superpowers/specs/2026-07-13-sidebar-logs-ui-design.md`

## Global Constraints

- Read-only in this round — no Undo/Redo UI, even though `operations.Manager.UndoBatch`/`RedoBatch` already exist in the backend.
- No changes to `internal/*` business logic except one small addition: `pipeline.ReadCategoryWarnings`, a reader mirroring the existing `LogCategoryWarnings` writer in the same file.
- Visual tokens (from the approved mockup): background `#F6F8FB`, card/surface `#FFFFFF`, border `#EDF0F5`, text `#1F2937`, muted text `#8A93A3`, primary blue `#2F63E0` (soft `#EEF3FF`), success green `#22A35A` (soft `#E9F9EF`), warning amber `#C77A1F` (soft `#FFF3E0`). Font: Nunito (already bundled at `desktop/frontend/src/assets/fonts/nunito-v16-latin-regular.woff2`, already wired in `style.css` — no new font asset).
- Sidebar's top-level item list must be array-driven, not hardcoded markup, so a future "Catalogue" top-level item (already planned by the user, not built in this plan) is a one-line addition later.
- Missing log file / empty log → explicit "No operations yet." / "No warnings yet." text in the view. Never a blank screen with no message — this is a direct lesson from a bug earlier this project where an empty Scan result rendered as nothing, indistinguishable from a broken button.
- Manual verification uses `wails build` (production binary), not `wails dev` — this session found a still-unresolved `wails dev`-specific IPC hang unrelated to this feature.

---

### Task 1: Backend — `pipeline.ReadCategoryWarnings`

**Files:**
- Modify: `internal/pipeline/warnings.go`
- Test: `internal/pipeline/warnings_test.go`

**Interfaces:**
- Produces: `pipeline.ReadCategoryWarnings(path string) ([]CategoryWarningEntry, error)` — reads back every entry written by the existing `LogCategoryWarnings`, in file order. A missing file returns `(nil, nil)`, matching `operations.Log.ReadAll`'s existing missing-file behavior.

- [ ] **Step 1: Write the failing tests**

Add to `internal/pipeline/warnings_test.go` (append after the existing tests, before the `countLines`/`splitLines` helpers at the bottom):

```go
func TestReadCategoryWarnings_MissingFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.jsonl")
	entries, err := ReadCategoryWarnings(path)
	if err != nil {
		t.Fatalf("ReadCategoryWarnings error: %v", err)
	}
	if entries != nil {
		t.Errorf("entries = %v, want nil for a missing file", entries)
	}
}

func TestReadCategoryWarnings_ReadsBackWhatWasWritten(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	books := []*book.Book{
		{SourcePath: "/inbox/a.epub", Category: "Fiction", Subcategory: "SpaceOpera", CategoryWarning: `rule matched undeclared subcategory "SpaceOpera" under category "Fiction"`},
		{SourcePath: "/inbox/b.epub", Category: "NonFiction", Subcategory: "History2", CategoryWarning: `rule matched undeclared subcategory "History2" under category "NonFiction"`},
	}
	if err := LogCategoryWarnings(books, warningsTestConfig(logDir)); err != nil {
		t.Fatalf("LogCategoryWarnings error: %v", err)
	}

	entries, err := ReadCategoryWarnings(filepath.Join(logDir, "category-warnings.jsonl"))
	if err != nil {
		t.Fatalf("ReadCategoryWarnings error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].SourcePath != "/inbox/a.epub" || entries[1].SourcePath != "/inbox/b.epub" {
		t.Errorf("entries in unexpected order/content: %+v", entries)
	}
	if entries[0].Warning != books[0].CategoryWarning {
		t.Errorf("Warning = %q, want %q", entries[0].Warning, books[0].CategoryWarning)
	}
	if entries[0].Timestamp.IsZero() {
		t.Error("expected a non-zero Timestamp")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg && go test ./internal/pipeline/... -run TestReadCategoryWarnings -v`
Expected: FAIL with `undefined: ReadCategoryWarnings`

- [ ] **Step 3: Implement `ReadCategoryWarnings`**

Add to `internal/pipeline/warnings.go`, after `LogCategoryWarnings` (the file already imports `encoding/json`, `os`, `fmt`, `path/filepath` — no new imports needed):

```go
// ReadCategoryWarnings reads every entry from a category-warnings log file
// written by LogCategoryWarnings, in file order (oldest first). A missing
// file is treated as an empty log, not an error -- the log may not exist
// yet if no scan has ever produced a warning, matching operations.Log's
// existing ReadAll behavior for the same not-written-yet case.
func ReadCategoryWarnings(path string) ([]CategoryWarningEntry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open log %s: %w", path, err)
	}
	defer f.Close()

	var entries []CategoryWarningEntry
	dec := json.NewDecoder(f)
	for dec.More() {
		var e CategoryWarningEntry
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("parse log entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pipeline/... -v`
Expected: PASS, all tests in the package including the two new ones and the existing `LogCategoryWarnings` tests

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/warnings.go internal/pipeline/warnings_test.go
git commit -m "Add pipeline.ReadCategoryWarnings to read back the category-warnings log"
```

---

### Task 2: Backend — `appapi.ListCategoryWarnings`

**Files:**
- Create: `internal/appapi/logs.go`
- Test: `internal/appapi/logs_test.go`

**Interfaces:**
- Consumes: `pipeline.ReadCategoryWarnings(path string) ([]pipeline.CategoryWarningEntry, error)` (Task 1); `(a *App) loadConfig() (config.Config, error)` (existing, `internal/appapi/app.go`).
- Produces: `appapi.CategoryWarningView{Timestamp, SourcePath, Category, Subcategory, Warning string}`; `(a *App) ListCategoryWarnings() ([]CategoryWarningView, error)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/appapi/logs_test.go`:

```go
package appapi

import (
	"path/filepath"
	"testing"

	"github.com/FrancisChung/BLibOrg/internal/book"
	"github.com/FrancisChung/BLibOrg/internal/pipeline"
)

func TestListCategoryWarnings_EmptyFileReturnsEmptySlice(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"))
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	warnings, err := app.ListCategoryWarnings()
	if err != nil {
		t.Fatalf("ListCategoryWarnings error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("got %d warnings, want 0 for a log that's never been written", len(warnings))
	}
}

func TestListCategoryWarnings_ReturnsWrittenEntries(t *testing.T) {
	working := t.TempDir()
	logDir := filepath.Join(t.TempDir(), "logs")
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), logDir)
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	cfg, err := app.loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	books := []*book.Book{
		{SourcePath: "/inbox/a.epub", Category: "Fiction", Subcategory: "SpaceOpera", CategoryWarning: `rule matched undeclared subcategory "SpaceOpera" under category "Fiction"`},
	}
	if err := pipeline.LogCategoryWarnings(books, cfg); err != nil {
		t.Fatalf("LogCategoryWarnings: %v", err)
	}

	warnings, err := app.ListCategoryWarnings()
	if err != nil {
		t.Fatalf("ListCategoryWarnings error: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1", len(warnings))
	}
	if warnings[0].SourcePath != "/inbox/a.epub" {
		t.Errorf("SourcePath = %q, want /inbox/a.epub", warnings[0].SourcePath)
	}
	if warnings[0].Category != "Fiction" || warnings[0].Subcategory != "SpaceOpera" {
		t.Errorf("Category/Subcategory = %s/%s, want Fiction/SpaceOpera", warnings[0].Category, warnings[0].Subcategory)
	}
	if warnings[0].Warning != books[0].CategoryWarning {
		t.Errorf("Warning = %q, want %q", warnings[0].Warning, books[0].CategoryWarning)
	}
	if warnings[0].Timestamp == "" {
		t.Error("expected a non-empty Timestamp")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/appapi/... -run TestListCategoryWarnings -v`
Expected: FAIL with `undefined: CategoryWarningView` / `app.ListCategoryWarnings undefined`

- [ ] **Step 3: Implement**

Create `internal/appapi/logs.go`:

```go
package appapi

import (
	"path/filepath"
	"time"

	"github.com/FrancisChung/BLibOrg/internal/pipeline"
)

// CategoryWarningView is the JSON-serializable view of a
// pipeline.CategoryWarningEntry sent to the frontend.
type CategoryWarningView struct {
	Timestamp   string `json:"timestamp"`
	SourcePath  string `json:"sourcePath"`
	Category    string `json:"category"`
	Subcategory string `json:"subcategory"`
	Warning     string `json:"warning"`
}

// ListCategoryWarnings returns every entry ever logged to
// log_folder/category-warnings.jsonl, newest first. An empty or
// never-written log returns an empty (non-nil) slice, not an error.
func (a *App) ListCategoryWarnings() ([]CategoryWarningView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(cfg.General.LogFolder, "category-warnings.jsonl")
	entries, err := pipeline.ReadCategoryWarnings(path)
	if err != nil {
		return nil, err
	}

	views := make([]CategoryWarningView, 0, len(entries))
	for _, e := range entries {
		views = append(views, CategoryWarningView{
			Timestamp:   e.Timestamp.Format(time.RFC3339),
			SourcePath:  e.SourcePath,
			Category:    e.Category,
			Subcategory: e.Subcategory,
			Warning:     e.Warning,
		})
	}
	for i, j := 0, len(views)-1; i < j; i, j = i+1, j-1 {
		views[i], views[j] = views[j], views[i]
	}
	return views, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/appapi/... -v`
Expected: PASS, all tests in the package

- [ ] **Step 5: Commit**

```bash
git add internal/appapi/logs.go internal/appapi/logs_test.go
git commit -m "Add appapi.ListCategoryWarnings"
```

---

### Task 3: Backend — `appapi.ListOperationBatches`

**Files:**
- Modify: `internal/appapi/logs.go`
- Modify: `internal/appapi/logs_test.go`

**Interfaces:**
- Consumes: `operations.Log.ListBatches() ([]operations.BatchSummary, error)`, `operations.Log.ReadAll() ([]operations.LogEntry, error)`, `operations.NewLog(path string) *operations.Log` (all existing, `internal/operations`).
- Produces: `appapi.OperationEntryView{OldPath, NewPath, OpType string, Undone bool}`; `appapi.OperationBatchView{BatchID, Timestamp string, EntryCount, UndoneCount int, Entries []OperationEntryView}`; `(a *App) ListOperationBatches() ([]OperationBatchView, error)`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/appapi/logs_test.go` (add `"time"` and `"github.com/FrancisChung/BLibOrg/internal/operations"` to the import block):

```go
func TestListOperationBatches_EmptyLogReturnsEmptySlice(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"))
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	batches, err := app.ListOperationBatches()
	if err != nil {
		t.Fatalf("ListOperationBatches error: %v", err)
	}
	if len(batches) != 0 {
		t.Errorf("got %d batches, want 0 for a log that's never been written", len(batches))
	}
}

func TestListOperationBatches_GroupsEntriesByBatchNewestFirst(t *testing.T) {
	working := t.TempDir()
	logDir := filepath.Join(t.TempDir(), "logs")
	configPath := writeTestConfig(t, working, filepath.Join(t.TempDir(), "library"), logDir)
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	opsLog := operations.NewLog(filepath.Join(logDir, "ops.jsonl"))
	older := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	if err := opsLog.Append([]operations.LogEntry{
		{BatchID: "20260701-1", Timestamp: older, OpType: operations.OpMove, OldPath: "/inbox/a.epub", NewPath: "/library/a.epub"},
		{BatchID: "20260701-1", Timestamp: older, OpType: operations.OpMove, OldPath: "/inbox/b.epub", NewPath: "/library/b.epub", Undone: true},
	}); err != nil {
		t.Fatalf("seed batch 1: %v", err)
	}
	if err := opsLog.Append([]operations.LogEntry{
		{BatchID: "20260713-1", Timestamp: newer, OpType: operations.OpMove, OldPath: "/inbox/c.epub", NewPath: "/library/c.epub"},
	}); err != nil {
		t.Fatalf("seed batch 2: %v", err)
	}

	batches, err := app.ListOperationBatches()
	if err != nil {
		t.Fatalf("ListOperationBatches error: %v", err)
	}
	if len(batches) != 2 {
		t.Fatalf("got %d batches, want 2", len(batches))
	}
	if batches[0].BatchID != "20260713-1" {
		t.Errorf("batches[0].BatchID = %q, want 20260713-1 (newest first)", batches[0].BatchID)
	}
	if batches[1].BatchID != "20260701-1" {
		t.Errorf("batches[1].BatchID = %q, want 20260701-1", batches[1].BatchID)
	}

	olderBatch := batches[1]
	if olderBatch.EntryCount != 2 {
		t.Errorf("EntryCount = %d, want 2", olderBatch.EntryCount)
	}
	if olderBatch.UndoneCount != 1 {
		t.Errorf("UndoneCount = %d, want 1", olderBatch.UndoneCount)
	}
	if len(olderBatch.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(olderBatch.Entries))
	}
	if olderBatch.Entries[0].OldPath != "/inbox/a.epub" || olderBatch.Entries[0].NewPath != "/library/a.epub" {
		t.Errorf("Entries[0] = %+v, want a.epub move", olderBatch.Entries[0])
	}
	if !olderBatch.Entries[1].Undone {
		t.Errorf("Entries[1].Undone = %v, want true", olderBatch.Entries[1].Undone)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/appapi/... -run TestListOperationBatches -v`
Expected: FAIL with `undefined: OperationBatchView` / `app.ListOperationBatches undefined`

- [ ] **Step 3: Implement**

Modify `internal/appapi/logs.go` — update the import block and add the new types/method:

```go
package appapi

import (
	"path/filepath"
	"time"

	"github.com/FrancisChung/BLibOrg/internal/operations"
	"github.com/FrancisChung/BLibOrg/internal/pipeline"
)
```

Add after `ListCategoryWarnings`:

```go
// OperationEntryView is the JSON-serializable view of one move operation
// within a batch.
type OperationEntryView struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
	OpType  string `json:"opType"`
	Undone  bool   `json:"undone"`
}

// OperationBatchView groups every operations.LogEntry with the same
// BatchID into one row for the UI -- a batch of 16 moved files should read
// as one entry, not 16 near-identical rows.
type OperationBatchView struct {
	BatchID     string               `json:"batchId"`
	Timestamp   string               `json:"timestamp"`
	EntryCount  int                  `json:"entryCount"`
	UndoneCount int                  `json:"undoneCount"`
	Entries     []OperationEntryView `json:"entries"`
}

// ListOperationBatches returns every batch ever recorded to
// log_folder/ops.jsonl, newest first, each with its individual file
// entries attached. An empty or never-written log returns an empty
// (non-nil) slice, not an error.
func (a *App) ListOperationBatches() ([]OperationBatchView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	opsLog := operations.NewLog(filepath.Join(cfg.General.LogFolder, "ops.jsonl"))

	summaries, err := opsLog.ListBatches()
	if err != nil {
		return nil, err
	}
	entries, err := opsLog.ReadAll()
	if err != nil {
		return nil, err
	}

	entriesByBatch := map[string][]operations.LogEntry{}
	for _, e := range entries {
		entriesByBatch[e.BatchID] = append(entriesByBatch[e.BatchID], e)
	}

	views := make([]OperationBatchView, 0, len(summaries))
	for _, s := range summaries {
		view := OperationBatchView{
			BatchID:     s.BatchID,
			Timestamp:   s.Timestamp.Format(time.RFC3339),
			EntryCount:  s.EntryCount,
			UndoneCount: s.UndoneCount,
		}
		for _, e := range entriesByBatch[s.BatchID] {
			view.Entries = append(view.Entries, OperationEntryView{
				OldPath: e.OldPath,
				NewPath: e.NewPath,
				OpType:  string(e.OpType),
				Undone:  e.Undone,
			})
		}
		views = append(views, view)
	}

	// ListBatches returns chronological (oldest-first) order; reverse for newest-first.
	for i, j := 0, len(views)-1; i < j; i, j = i+1, j-1 {
		views[i], views[j] = views[j], views[i]
	}
	return views, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/appapi/... -v`
Expected: PASS, all tests in the package

- [ ] **Step 5: Commit**

```bash
git add internal/appapi/logs.go internal/appapi/logs_test.go
git commit -m "Add appapi.ListOperationBatches"
```

---

### Task 4: Backend — wire `desktop/app.go`

**Files:**
- Modify: `desktop/app.go`

**Interfaces:**
- Consumes: `appapi.ListOperationBatches`, `appapi.ListCategoryWarnings`, `appapi.OperationBatchView`, `appapi.CategoryWarningView` (Tasks 2-3).
- Produces: Wails-bound `(a *App) ListOperationBatches() ([]appapi.OperationBatchView, error)`, `(a *App) ListCategoryWarnings() ([]appapi.CategoryWarningView, error)` — callable from the frontend once bindings regenerate (`wails build`/`wails dev` do this automatically, no manual step).

- [ ] **Step 1: Add the wrapper methods**

Add to `desktop/app.go`, after the existing `ConfigStatus` method:

```go
func (a *App) ListOperationBatches() ([]appapi.OperationBatchView, error) {
	return a.api.ListOperationBatches()
}

func (a *App) ListCategoryWarnings() ([]appapi.CategoryWarningView, error) {
	return a.api.ListCategoryWarnings()
}
```

- [ ] **Step 2: Verify the whole module still builds**

Run: `cd /media/francis/Data2/Source/Organisers/BLibOrg && go build ./... && go vet ./...`
Expected: both commands exit 0 with no output

- [ ] **Step 3: Commit**

```bash
git add desktop/app.go
git commit -m "Wire ListOperationBatches/ListCategoryWarnings into the Wails-bound App"
```

---

### Task 5: Frontend — global theme tokens + new types

**Files:**
- Modify: `desktop/frontend/src/style.css`
- Modify: `desktop/frontend/src/lib/types.ts`

**Interfaces:**
- Produces: CSS custom properties (`--bf-bg`, `--bf-surface`, `--bf-border`, `--bf-text`, `--bf-text-muted`, `--bf-blue`, `--bf-blue-soft`, `--bf-green`, `--bf-green-soft`, `--bf-amber`, `--bf-amber-soft`) on `:root`; TypeScript types `OperationEntryView`, `OperationBatchView`, `CategoryWarningView`, `SidebarView` for later tasks to import from `./types`.

- [ ] **Step 1: Replace `style.css`'s theme**

Replace the full contents of `desktop/frontend/src/style.css`:

```css
:root {
    --bf-bg: #F6F8FB;
    --bf-surface: #FFFFFF;
    --bf-border: #EDF0F5;
    --bf-text: #1F2937;
    --bf-text-muted: #8A93A3;
    --bf-blue: #2F63E0;
    --bf-blue-soft: #EEF3FF;
    --bf-green: #22A35A;
    --bf-green-soft: #E9F9EF;
    --bf-amber: #C77A1F;
    --bf-amber-soft: #FFF3E0;
}

html {
    background-color: var(--bf-bg);
    text-align: left;
    color: var(--bf-text);
}

body {
    margin: 0;
    color: var(--bf-text);
    font-family: "Nunito", -apple-system, BlinkMacSystemFont, "Segoe UI", "Roboto",
    "Oxygen", "Ubuntu", "Cantarell", "Fira Sans", "Droid Sans", "Helvetica Neue",
    sans-serif;
}

@font-face {
    font-family: "Nunito";
    font-style: normal;
    font-weight: 400;
    src: local(""),
    url("assets/fonts/nunito-v16-latin-regular.woff2") format("woff2");
}

#app {
    min-height: 100vh;
    text-align: left;
}
```

- [ ] **Step 2: Add the new types**

Add to `desktop/frontend/src/lib/types.ts`, at the end of the file:

```typescript
export interface OperationEntryView {
  oldPath: string;
  newPath: string;
  opType: string;
  undone: boolean;
}

export interface OperationBatchView {
  batchId: string;
  timestamp: string;
  entryCount: number;
  undoneCount: number;
  entries: OperationEntryView[];
}

export interface CategoryWarningView {
  timestamp: string;
  sourcePath: string;
  category: string;
  subcategory: string;
  warning: string;
}

export type SidebarView = 'scan' | 'operations' | 'warnings';
```

- [ ] **Step 3: Verify the frontend still builds and typechecks**

Run: `cd desktop/frontend && npm run build && npm run check`
Expected: both exit 0 (the app still looks like the old dark-navy theme visually at this point since no component uses the new tokens yet — that's expected, later tasks apply them)

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/style.css desktop/frontend/src/lib/types.ts
git commit -m "Add BookFusion-derived theme tokens and log-view TypeScript types"
```

---

### Task 6: Frontend — `Sidebar.svelte`

**Files:**
- Create: `desktop/frontend/src/lib/Sidebar.svelte`
- Test: `desktop/frontend/src/lib/Sidebar.test.ts`

**Interfaces:**
- Consumes: `SidebarView` (Task 5, `./types`).
- Produces: `Sidebar` component — prop `active: SidebarView`; dispatches `navigate: SidebarView` on item click. Rendered nav item labels: "Scan & Review" (top-level), "Operations" and "Warnings" (indented under a "Logs" section label, which is not itself clickable).

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/lib/Sidebar.test.ts`:

```typescript
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import Sidebar from './Sidebar.svelte';

describe('Sidebar', () => {
  it('highlights the active item and not the others', () => {
    render(Sidebar, { active: 'operations' });
    expect(screen.getByRole('button', { name: 'Operations' }).className).toContain('active');
    expect(screen.getByRole('button', { name: 'Scan & Review' }).className).not.toContain('active');
    expect(screen.getByRole('button', { name: 'Warnings' }).className).not.toContain('active');
  });

  it('emits navigate with "scan" when Scan & Review is clicked', async () => {
    const { component } = render(Sidebar, { active: 'operations' });
    const handler = vi.fn();
    component.$on('navigate', handler);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan & Review' }));

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail).toBe('scan');
  });

  it('emits navigate with "warnings" when Warnings is clicked', async () => {
    const { component } = render(Sidebar, { active: 'scan' });
    const handler = vi.fn();
    component.$on('navigate', handler);

    await fireEvent.click(screen.getByRole('button', { name: 'Warnings' }));

    expect(handler).toHaveBeenCalledTimes(1);
    expect(handler.mock.calls[0][0].detail).toBe('warnings');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/Sidebar.test.ts`
Expected: FAIL — `Sidebar.svelte` does not exist

- [ ] **Step 3: Implement `Sidebar.svelte`**

Create `desktop/frontend/src/lib/Sidebar.svelte`:

```svelte
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/lib/Sidebar.test.ts`
Expected: PASS, 3 tests

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/Sidebar.svelte desktop/frontend/src/lib/Sidebar.test.ts
git commit -m "Add Sidebar component"
```

---

### Task 7: Frontend — extract `ScanReviewView.svelte`

**Files:**
- Create: `desktop/frontend/src/lib/ScanReviewView.svelte`
- Create: `desktop/frontend/src/lib/ScanReviewView.test.ts`

**Interfaces:**
- Consumes: `Scan`, `Recompute`, `Apply`, `ConfirmApply` (existing Wails bindings, `../../wailsjs/go/main/App`); `BookView`, `StatusFilter`, `matchesFilter`, `matchesQuery` (existing, `./types`); `FilterBar`, `BookCard` (existing, `./FilterBar.svelte`, `./BookCard.svelte`).
- Produces: `ScanReviewView` component — no props, self-contained (identical scan/filter/apply behavior to the current `App.svelte`, minus the app-level config-status banner, which moves to the shell in Task 10).

This is a refactor, not new behavior: `App.svelte`'s current scan/filter/card/apply logic moves into this new file essentially unchanged (dropping the temporary debug `console.log` and the `configError`/`ConfigStatus` handling, which becomes the shell's job). `App.svelte` itself is untouched until Task 10 — until then the app still runs on the old single-screen `App.svelte`, so this task is purely additive and doesn't break anything.

- [ ] **Step 1: Create the component**

Create `desktop/frontend/src/lib/ScanReviewView.svelte`:

```svelte
<script lang="ts">
  import FilterBar from './FilterBar.svelte';
  import BookCard from './BookCard.svelte';
  import { matchesFilter, matchesQuery, type BookView, type StatusFilter } from './types';
  import { Scan, Recompute, Apply, ConfirmApply } from '../../wailsjs/go/main/App';

  let books: BookView[] = [];
  let query = '';
  let activeFilter: StatusFilter = 'all';
  let scanError = '';
  let scanning = false;
  let applying = false;
  let resultBySourcePath: Record<string, { ok: boolean; error: string; skipped: boolean }> = {};
  let recomputeWarning: Record<string, boolean> = {};

  async function doScan() {
    scanning = true;
    scanError = '';
    resultBySourcePath = {};
    recomputeWarning = {};
    try {
      books = await Scan();
    } catch (e) {
      scanError = String(e);
      books = [];
    } finally {
      scanning = false;
    }
  }

  async function onEdited(e: CustomEvent<BookView>) {
    const edited = e.detail;
    try {
      const updated = await Recompute(edited);
      books = books.map((b) => (b.sourcePath === updated.sourcePath ? updated : b));
      recomputeWarning = { ...recomputeWarning, [edited.sourcePath]: false };
    } catch (err) {
      // Recompute failed (rare -- mostly I/O): keep the card's last-known-good
      // DestPath by leaving `books` untouched, and flag it with a warning
      // instead of letting the error propagate and blank the card.
      recomputeWarning = { ...recomputeWarning, [edited.sourcePath]: true };
      console.error('Recompute failed for', edited.sourcePath, err);
    }
  }

  async function doApply() {
    const eligible = visibleBooks.filter((b) => b.status !== 'Unresolved');
    const confirmed = await ConfirmApply(eligible.length, '');
    if (!confirmed) return;

    applying = true;
    try {
      const result = await Apply(visibleBooks);
      const byPath: typeof resultBySourcePath = {};
      for (const r of result.results) {
        byPath[r.sourcePath] = { ok: r.ok, error: r.error, skipped: r.skipped };
      }
      resultBySourcePath = byPath;
    } finally {
      applying = false;
    }
  }

  $: visibleBooks = books.filter((b) => matchesFilter(b, activeFilter) && matchesQuery(b, query));
</script>

<div class="topbar">
  <h2>Scan &amp; Review</h2>
  <div>
    <button class="secondary" on:click={doScan} disabled={scanning}>{scanning ? 'Scanning…' : 'Scan'}</button>
    <button on:click={doApply} disabled={applying || books.length === 0}>
      {applying ? 'Applying…' : 'Apply'}
    </button>
  </div>
</div>

{#if scanError}
  <div class="banner error">{scanError}</div>
{/if}

{#if books.length > 0}
  <FilterBar
    {query}
    {activeFilter}
    on:queryChange={(e) => (query = e.detail)}
    on:filterChange={(e) => (activeFilter = e.detail)}
  />

  <div class="cards">
    {#each visibleBooks as book (book.id)}
      <div class="card-row">
        <BookCard {book} on:edited={onEdited} />
        {#if recomputeWarning[book.sourcePath]}
          <div class="recompute-warning">⚠ couldn't update the destination path — showing the last known value</div>
        {/if}
        {#if resultBySourcePath[book.sourcePath]}
          {@const r = resultBySourcePath[book.sourcePath]}
          <div class="apply-result" class:ok={r.ok} class:error={!r.ok && !r.skipped}>
            {r.skipped ? 'Skipped' : r.ok ? 'Moved ✓' : `Error: ${r.error}`}
          </div>
        {/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .topbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .topbar h2 {
    font-size: 20px;
    font-weight: 800;
    color: var(--bf-text);
    margin: 0;
  }
  button {
    background: var(--bf-blue);
    color: white;
    border: none;
    padding: 9px 18px;
    border-radius: 999px;
    font-weight: 700;
    font-size: 13px;
    font-family: inherit;
    cursor: pointer;
  }
  button.secondary {
    background: var(--bf-blue-soft);
    color: var(--bf-blue);
    margin-right: 8px;
  }
  button:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 13px;
  }
  .cards {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .card-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .apply-result {
    font-size: 12px;
    padding: 2px 4px;
  }
  .apply-result.ok {
    color: var(--bf-green);
  }
  .apply-result.error {
    color: var(--bf-amber);
  }
  .recompute-warning {
    font-size: 11.5px;
    color: var(--bf-amber);
  }
</style>
```

- [ ] **Step 2: Create the test (moved and adapted from the current `App.test.ts`)**

Create `desktop/frontend/src/lib/ScanReviewView.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import type { BookView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  Scan: vi.fn(),
  Recompute: vi.fn(),
  Apply: vi.fn(),
  ConfirmApply: vi.fn(),
}));

import ScanReviewView from './ScanReviewView.svelte';
import { Scan, Apply, ConfirmApply } from '../../wailsjs/go/main/App';

function makeBook(overrides: Partial<BookView> = {}): BookView {
  return {
    id: '/inbox/book.epub',
    sourcePath: '/inbox/book.epub',
    oldFilename: 'book.epub',
    format: 'epub',
    sizeBytes: 1024,
    subject: '',
    title: { value: 'Atomic Kotlin', source: 'Heuristic' },
    author: { value: 'Bruce Eckel', source: 'Heuristic' },
    year: { value: '2021', source: 'Heuristic' },
    status: 'Heuristic',
    category: 'Uncategorized',
    subcategory: '',
    categoryWarning: '',
    destPath: '/library/Uncategorized/Atomic Kotlin (2021) - Bruce Eckel.epub',
    duplicateStatus: 'NotDuplicate',
    duplicateGroupId: '',
    ...overrides,
  };
}

describe('ScanReviewView', () => {
  beforeEach(() => {
    vi.mocked(ConfirmApply).mockResolvedValue(true);
    vi.mocked(Apply).mockResolvedValue({ batchId: 'b1', results: [] });
  });

  it('Apply only moves the currently visible (filtered) books, not the whole scan', async () => {
    const visibleBook = makeBook({
      id: '/inbox/atomic-kotlin.epub',
      sourcePath: '/inbox/atomic-kotlin.epub',
      title: { value: 'Atomic Kotlin', source: 'Heuristic' },
    });
    const hiddenBook = makeBook({
      id: '/inbox/other-book.epub',
      sourcePath: '/inbox/other-book.epub',
      oldFilename: 'other-book.epub',
      title: { value: 'Some Other Book', source: 'Heuristic' },
    });
    vi.mocked(Scan).mockResolvedValue([visibleBook, hiddenBook]);

    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));
    await waitFor(() => {
      expect(screen.getByText('other-book.epub')).toBeInTheDocument();
    });

    const search = screen.getByPlaceholderText('Search title, author, or filename…');
    await fireEvent.input(search, { target: { value: 'Atomic' } });

    await waitFor(() => {
      expect(screen.queryByText('other-book.epub')).not.toBeInTheDocument();
    });
    expect(screen.getByText('book.epub', { exact: false })).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

    await waitFor(() => {
      expect(Apply).toHaveBeenCalledTimes(1);
    });

    expect(ConfirmApply).toHaveBeenCalledWith(1, '');

    const appliedBooks = vi.mocked(Apply).mock.calls[0][0];
    expect(appliedBooks).toHaveLength(1);
    expect(appliedBooks[0].sourcePath).toBe('/inbox/atomic-kotlin.epub');
  });
});
```

- [ ] **Step 3: Run the test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/lib/ScanReviewView.test.ts`
Expected: PASS, 1 test (this is a moved/renamed test exercising already-working logic, so it should pass immediately, not fail-then-pass)

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/lib/ScanReviewView.svelte desktop/frontend/src/lib/ScanReviewView.test.ts
git commit -m "Extract ScanReviewView from App.svelte"
```

---

### Task 8: Frontend — `OperationsLogView.svelte`

**Files:**
- Create: `desktop/frontend/src/lib/OperationsLogView.svelte`
- Create: `desktop/frontend/src/lib/OperationsLogView.test.ts`

**Interfaces:**
- Consumes: `OperationBatchView` (Task 5, `./types`); `ListOperationBatches` (Wails binding, `../../wailsjs/go/main/App` — exists after Task 4's Go changes regenerate bindings via `wails build`; frontend unit tests mock the module directly and don't need real generated bindings present).
- Produces: `OperationsLogView` component — no props, self-contained. Fetches batches on mount, shows "No operations yet." when empty, an error banner on failure, otherwise a list of batch rows that expand on click to show individual file moves.

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/lib/OperationsLogView.test.ts`:

```typescript
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import type { OperationBatchView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  ListOperationBatches: vi.fn(),
}));

import OperationsLogView from './OperationsLogView.svelte';
import { ListOperationBatches } from '../../wailsjs/go/main/App';

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
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/OperationsLogView.test.ts`
Expected: FAIL — `OperationsLogView.svelte` does not exist

- [ ] **Step 3: Implement `OperationsLogView.svelte`**

Create `desktop/frontend/src/lib/OperationsLogView.svelte`:

```svelte
<script lang="ts">
  import { onMount } from 'svelte';
  import type { OperationBatchView } from './types';
  import { ListOperationBatches } from '../../wailsjs/go/main/App';

  let batches: OperationBatchView[] = [];
  let loadError = '';
  let loading = true;
  let expanded: Record<string, boolean> = {};

  onMount(async () => {
    try {
      batches = await ListOperationBatches();
    } catch (e) {
      loadError = String(e);
    } finally {
      loading = false;
    }
  });

  function toggle(batchId: string) {
    expanded = { ...expanded, [batchId]: !expanded[batchId] };
  }
</script>

<h2>Operations Log</h2>

{#if loadError}
  <div class="banner error">{loadError}</div>
{:else if loading}
  <p class="empty">Loading…</p>
{:else if batches.length === 0}
  <p class="empty">No operations yet.</p>
{:else}
  <div class="batches">
    {#each batches as batch (batch.batchId)}
      <div class="batch">
        <button type="button" class="batch-row" on:click={() => toggle(batch.batchId)}>
          <div>
            <div class="batch-id">Batch {batch.batchId}</div>
            <div class="batch-meta">
              {new Date(batch.timestamp).toLocaleString()} &middot; {batch.entryCount} file{batch.entryCount === 1 ? '' : 's'} moved{batch.undoneCount > 0 ? ` · ${batch.undoneCount} undone` : ''}
            </div>
          </div>
        </button>
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
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    font-family: inherit;
    padding: 12px 16px;
    cursor: pointer;
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

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/lib/OperationsLogView.test.ts`
Expected: PASS, 3 tests

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/OperationsLogView.svelte desktop/frontend/src/lib/OperationsLogView.test.ts
git commit -m "Add OperationsLogView"
```

---

### Task 9: Frontend — `WarningsLogView.svelte`

**Files:**
- Create: `desktop/frontend/src/lib/WarningsLogView.svelte`
- Create: `desktop/frontend/src/lib/WarningsLogView.test.ts`

**Interfaces:**
- Consumes: `CategoryWarningView` (Task 5, `./types`); `ListCategoryWarnings` (Wails binding, `../../wailsjs/go/main/App`).
- Produces: `WarningsLogView` component — no props, self-contained. Fetches warnings on mount, shows "No warnings yet." when empty, an error banner on failure, otherwise a flat list of warning rows.

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/lib/WarningsLogView.test.ts`:

```typescript
import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/svelte';
import type { CategoryWarningView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  ListCategoryWarnings: vi.fn(),
}));

import WarningsLogView from './WarningsLogView.svelte';
import { ListCategoryWarnings } from '../../wailsjs/go/main/App';

function makeWarning(overrides: Partial<CategoryWarningView> = {}): CategoryWarningView {
  return {
    timestamp: '2026-07-13T12:00:00Z',
    sourcePath: '/inbox/a.epub',
    category: 'Fiction',
    subcategory: 'SpaceOpera',
    warning: 'rule matched undeclared subcategory "SpaceOpera" under category "Fiction"',
    ...overrides,
  };
}

describe('WarningsLogView', () => {
  it('shows an empty state when there are no warnings', async () => {
    vi.mocked(ListCategoryWarnings).mockResolvedValue([]);
    render(WarningsLogView);

    await waitFor(() => {
      expect(screen.getByText('No warnings yet.')).toBeInTheDocument();
    });
  });

  it('renders a warning row with source path, category, and message', async () => {
    vi.mocked(ListCategoryWarnings).mockResolvedValue([makeWarning()]);
    render(WarningsLogView);

    await waitFor(() => {
      expect(screen.getByText('/inbox/a.epub')).toBeInTheDocument();
    });
    expect(screen.getByText(/Fiction \/ SpaceOpera/)).toBeInTheDocument();
    expect(screen.getByText(/rule matched undeclared subcategory/)).toBeInTheDocument();
  });

  it('shows an error banner when the load fails', async () => {
    vi.mocked(ListCategoryWarnings).mockRejectedValue(new Error('boom'));
    render(WarningsLogView);

    await waitFor(() => {
      expect(screen.getByText('Error: boom')).toBeInTheDocument();
    });
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/WarningsLogView.test.ts`
Expected: FAIL — `WarningsLogView.svelte` does not exist

- [ ] **Step 3: Implement `WarningsLogView.svelte`**

Create `desktop/frontend/src/lib/WarningsLogView.svelte`:

```svelte
<script lang="ts">
  import { onMount } from 'svelte';
  import type { CategoryWarningView } from './types';
  import { ListCategoryWarnings } from '../../wailsjs/go/main/App';

  let warnings: CategoryWarningView[] = [];
  let loadError = '';
  let loading = true;

  onMount(async () => {
    try {
      warnings = await ListCategoryWarnings();
    } catch (e) {
      loadError = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<h2>Category Warnings</h2>

{#if loadError}
  <div class="banner error">{loadError}</div>
{:else if loading}
  <p class="empty">Loading…</p>
{:else if warnings.length === 0}
  <p class="empty">No warnings yet.</p>
{:else}
  <div class="rows">
    {#each warnings as w, i (w.sourcePath + i)}
      <div class="row">
        <div class="source">{w.sourcePath}</div>
        <div class="detail">
          {new Date(w.timestamp).toLocaleString()} &middot; {w.category}{w.subcategory ? ` / ${w.subcategory}` : ''}
        </div>
        <div class="warning">{w.warning}</div>
      </div>
    {/each}
  </div>
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
  .rows {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .row {
    background: var(--bf-surface);
    border: 1px solid var(--bf-border);
    border-radius: 10px;
    padding: 12px 16px;
  }
  .source {
    font-weight: 700;
    font-size: 13px;
    color: var(--bf-text);
  }
  .detail {
    font-size: 12px;
    color: var(--bf-text-muted);
    margin-top: 2px;
  }
  .warning {
    font-size: 12.5px;
    color: var(--bf-amber);
    margin-top: 6px;
  }
</style>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/lib/WarningsLogView.test.ts`
Expected: PASS, 3 tests

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/WarningsLogView.svelte desktop/frontend/src/lib/WarningsLogView.test.ts
git commit -m "Add WarningsLogView"
```

---

### Task 10: Frontend — rewrite `App.svelte` as the sidebar shell

**Files:**
- Modify: `desktop/frontend/src/App.svelte`
- Modify: `desktop/frontend/src/App.test.ts`

**Interfaces:**
- Consumes: `Sidebar` (Task 6), `ScanReviewView` (Task 7), `OperationsLogView` (Task 8), `WarningsLogView` (Task 9), `SidebarView` (Task 5), `ConfigStatus` (existing Wails binding).
- Produces: `App` — owns `activeView: SidebarView` state (default `'scan'`) and the app-wide `configError` banner (moved here from `ScanReviewView`, shown regardless of active view).

- [ ] **Step 1: Write the failing tests**

Replace the full contents of `desktop/frontend/src/App.test.ts`:

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';

vi.mock('../wailsjs/go/main/App', () => ({
  ConfigStatus: vi.fn(),
  Scan: vi.fn(),
  Recompute: vi.fn(),
  Apply: vi.fn(),
  ConfirmApply: vi.fn(),
  ListOperationBatches: vi.fn(),
  ListCategoryWarnings: vi.fn(),
}));

import App from './App.svelte';
import { ConfigStatus, ListOperationBatches, ListCategoryWarnings } from '../wailsjs/go/main/App';

describe('App', () => {
  beforeEach(() => {
    vi.mocked(ConfigStatus).mockResolvedValue({ path: '', error: '' });
    vi.mocked(ListOperationBatches).mockResolvedValue([]);
    vi.mocked(ListCategoryWarnings).mockResolvedValue([]);
  });

  it('shows Scan & Review by default', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan' })).toBeInTheDocument();
    });
  });

  it('switches to the Operations Log view when its sidebar item is clicked', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Operations' }));

    await waitFor(() => {
      expect(screen.getByText('Operations Log')).toBeInTheDocument();
    });
  });

  it('switches to the Category Warnings view when its sidebar item is clicked', async () => {
    render(App);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Warnings' }));

    await waitFor(() => {
      expect(screen.getByText('Category Warnings')).toBeInTheDocument();
    });
  });

  it('shows a config error banner regardless of active view', async () => {
    vi.mocked(ConfigStatus).mockResolvedValue({ path: '/fake/config.yaml', error: 'not found' });
    render(App);

    await waitFor(() => {
      expect(screen.getByText(/No usable config at/)).toBeInTheDocument();
    });

    await fireEvent.click(screen.getByRole('button', { name: 'Operations' }));
    expect(screen.getByText(/No usable config at/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/App.test.ts`
Expected: FAIL — `App.svelte` still renders the old single-screen layout with no Sidebar, so "Operations"/"Warnings" buttons don't exist yet

- [ ] **Step 3: Rewrite `App.svelte`**

Replace the full contents of `desktop/frontend/src/App.svelte`:

```svelte
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `npx vitest run src/App.test.ts`
Expected: PASS, 4 tests

- [ ] **Step 5: Run the full frontend test suite to check for regressions**

Run: `npm test -- --run`
Expected: PASS, all test files including `ScanReviewView.test.ts` (Task 7), `Sidebar.test.ts` (Task 6), `OperationsLogView.test.ts` (Task 8), `WarningsLogView.test.ts` (Task 9), `App.test.ts`, `BookCard.test.ts`, `FilterBar.test.ts`

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/App.svelte desktop/frontend/src/App.test.ts
git commit -m "Rewrite App.svelte as a sidebar shell over Scan/Operations/Warnings views"
```

---

### Task 11: Manual verification

Not a code change — confirms the whole feature end-to-end via a production build, since this session found a `wails dev`-specific IPC hang unrelated to this feature (tracked separately; use `wails build` for manual verification).

- [ ] **Step 1: Full automated check**

Run from the repo root:

```bash
go build ./... && go vet ./... && go test ./...
cd desktop/frontend && npm run build && npm run check && npm test -- --run
```

Expected: everything passes (this repeats Tasks 1-10's individual checks as one final pass).

- [ ] **Step 2: Build and run the production binary**

```bash
cd desktop && wails build -tags webkit2_41
./build/bin/desktop
```

- [ ] **Step 3: Exercise the Scan & Review → Operations Log path**

In the running app: click **Scan** (uses whatever `working_folder` is in `~/.config/BLibOrg/config.yaml`), then **Apply**. Click **Operations** in the sidebar — confirm the batch you just applied appears with the correct file count, and clicking it expands to show the individual old→new path entries.

- [ ] **Step 4: Exercise the Warnings path**

Temporarily add a rule to `~/.config/BLibOrg/config.yaml` that points at a category not listed under `categories:` (e.g. add a rule with `category: TestUndeclared`), re-run Scan, then click **Warnings** in the sidebar — confirm the mismatch appears with the source file, category/subcategory, and warning message. Revert the temporary config change afterward.

- [ ] **Step 5: Confirm the empty states**

With a freshly configured `log_folder` that has no `ops.jsonl`/`category-warnings.jsonl` yet (e.g. point `log_folder` at a brand-new empty directory in the config, or delete both files from the existing one), click **Operations** and **Warnings** — confirm each shows its "No operations yet." / "No warnings yet." message rather than a blank screen.

No commit for this task — it's verification only, not a code change.
