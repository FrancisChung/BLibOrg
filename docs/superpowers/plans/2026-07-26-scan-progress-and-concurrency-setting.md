# Scan Progress and Concurrency Setting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show elapsed time and a "done / total books" counter in the Library view's loading banner during a refresh, and let the user view/set the scan concurrency from Settings.

**Architecture:** `librarian.Scan` gains a nil-safe `onProgress func(done, total int)` parameter, called once per book (cache hit or fresh) under its own mutex so `done` is strictly increasing even under concurrent workers. `appapi.App.ListLibrary` threads it through unchanged; the desktop layer (the only place holding the Wails `ctx`) wraps it in a closure that emits a `library:scan-progress` Wails event -- this app's first use of Wails' event mechanism. `LibraryView.svelte` listens for that event and runs a client-side elapsed-time timer for the duration of a refresh. Separately, two new `appapi.App` methods (`GetScanConcurrency`/`SetScanConcurrency`) read/write `config.General.ScanConcurrency` via a plain `config.Load`/`config.Save` round trip, exposed through a new Settings section.

**Tech Stack:** Go standard library only (`sync.Mutex`, `runtime.NumCPU`), Wails' existing `runtime.EventsEmit`/frontend `EventsOn` (already vendored, never previously used in this codebase), Svelte/vitest for the frontend -- no new dependency.

## Global Constraints

- The progress counter counts every book that finishes, cache hit or fresh extraction alike (per design decision) -- `total` is `len(paths)`, known before extraction starts.
- `onProgress` is nil-safe throughout the call chain (`librarian.Scan`, `appapi.ListLibrary`); existing/test callers that don't care about progress pass `nil`.
- Settings persistence reuses the existing `config.Load` → mutate → `config.Save` round trip as-is (the full-rewrite approach, chosen over a comment-preserving targeted edit) -- `config.Save`'s `yaml.Marshal` stripping comments/reordering maps on every save is an accepted, pre-existing property of `config.Save`, not something to work around in this plan.
- `0` (or a blank input, submitted as `0`) explicitly means "auto" for the concurrency setting -- no separate reset control.
- No confirmation dialog for saving the concurrency setting (non-destructive, instantly reversible) -- unlike `ResetCoverCache`'s confirm dialog.
- No new external dependency, in Go or in the frontend.
- Calling Wails' `runtime.EventsEmit`/`EventsOn` with a `nil` or non-Wails `context.Context` calls `log.Fatalf` internally (an `os.Exit`, not a panic) -- every construction of the progress-emitting closure must be guarded so this can never be reached from a test or any code path before Wails' `startup(ctx)` has run.

---

### Task 1: Add a progress callback to `librarian.Scan`

**Files:**
- Modify: `internal/librarian/librarian.go`
- Test: `internal/librarian/librarian_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `Scan(cfg config.Config, forceRefresh bool, onProgress func(done, total int)) ([]Book, error)` -- the `onProgress` parameter, consumed by Task 2.

- [ ] **Step 1: Write the failing test**

Add this test to `internal/librarian/librarian_test.go` (after the existing `TestScan_ZeroScanConcurrencyStillWorks`):

```go
func TestScan_ReportsProgressForEveryBookInStrictOrder(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	for i := 0; i < 8; i++ {
		writeFixtureFile(t, libDir, filepath.Join("Fiction", "Book"+strconv.Itoa(i)+".epub"))
	}

	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		return metadata.Result{Title: "Title"}, nil
	}

	var mu sync.Mutex
	var doneValues []int
	var totalValues []int

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir, ScanConcurrency: 3}}
	onProgress := func(done, total int) {
		mu.Lock()
		defer mu.Unlock()
		doneValues = append(doneValues, done)
		totalValues = append(totalValues, total)
	}

	if _, err := Scan(cfg, false, onProgress); err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if len(doneValues) != 8 {
		t.Fatalf("onProgress was called %d times, want 8", len(doneValues))
	}
	for i, done := range doneValues {
		if done != i+1 {
			t.Errorf("doneValues[%d] = %d, want %d (done must be strictly increasing, one call at a time)", i, done, i+1)
		}
	}
	for i, total := range totalValues {
		if total != 8 {
			t.Errorf("totalValues[%d] = %d, want 8 (total must stay constant)", i, total)
		}
	}
}

func TestScan_NilProgressCallbackIsSafe(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	writeFixtureFile(t, libDir, filepath.Join("Fiction", "Foundation.epub"))

	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		return metadata.Result{Title: "Foundation"}, nil
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	if _, err := Scan(cfg, false, nil); err != nil {
		t.Fatalf("Scan with a nil onProgress returned error: %v", err)
	}
}
```

Add `"sync"` to this test file's import block, changing:

```go
import (
	"archive/zip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FrancisChung/BLibOrg/internal/config"
	"github.com/FrancisChung/BLibOrg/internal/covercache"
	"github.com/FrancisChung/BLibOrg/internal/librarycache"
	"github.com/FrancisChung/BLibOrg/internal/metadata"
)
```

to:

```go
import (
	"archive/zip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FrancisChung/BLibOrg/internal/config"
	"github.com/FrancisChung/BLibOrg/internal/covercache"
	"github.com/FrancisChung/BLibOrg/internal/librarycache"
	"github.com/FrancisChung/BLibOrg/internal/metadata"
)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/librarian/ -run TestScan_ReportsProgressForEveryBookInStrictOrder -v`
Expected: FAIL to compile (`Scan` doesn't accept a third argument yet). This also means every other existing call to `Scan(cfg, ...)` in this file will fail to compile once Step 3 changes the signature -- Step 4 fixes them all in the same task.

- [ ] **Step 3: Add the `onProgress` parameter to `Scan`**

In `internal/librarian/librarian.go`, change:

```go
func Scan(cfg config.Config, forceRefresh bool) ([]Book, error) {
	paths, err := scanner.Scan(cfg.General.LibraryFolder)
	if err != nil {
		return nil, err
	}

	cache := librarycache.Load(cfg.General.LogFolder)
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		seen[path] = true
	}

	concurrency := cfg.General.ScanConcurrency
	if concurrency <= 0 {
		concurrency = runtime.NumCPU()
	}

	results := make([]Book, len(paths))
	included := make([]bool, len(paths))
	var cacheMu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, path := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i], included[i] = scanOneBook(cfg, forceRefresh, &cache, &cacheMu, path)
		}(i, path)
	}
	wg.Wait()
```

to:

```go
func Scan(cfg config.Config, forceRefresh bool, onProgress func(done, total int)) ([]Book, error) {
	paths, err := scanner.Scan(cfg.General.LibraryFolder)
	if err != nil {
		return nil, err
	}

	cache := librarycache.Load(cfg.General.LogFolder)
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		seen[path] = true
	}

	concurrency := cfg.General.ScanConcurrency
	if concurrency <= 0 {
		concurrency = runtime.NumCPU()
	}

	results := make([]Book, len(paths))
	included := make([]bool, len(paths))
	var cacheMu sync.Mutex
	var progressMu sync.Mutex
	done := 0
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, path := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, path string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i], included[i] = scanOneBook(cfg, forceRefresh, &cache, &cacheMu, path)
			if onProgress != nil {
				progressMu.Lock()
				done++
				onProgress(done, len(paths))
				progressMu.Unlock()
			}
		}(i, path)
	}
	wg.Wait()
```

Update `Scan`'s doc comment, changing its final paragraph:

```go
// Every path's single-book work (scanOneBook, below) runs concurrently,
// bounded by cfg.General.ScanConcurrency (0/unset means
// runtime.NumCPU()) via a semaphore channel + WaitGroup -- safe because
// metadata.Extract is documented safe for concurrent use (see its own
// doc comment) and the one other piece of state shared across workers,
// the in-memory cache, is guarded by its own mutex (see scanOneBook).
// The returned slice preserves paths' original order regardless of which
// goroutine finishes first, matching the pre-parallel behavior exactly.
func Scan(cfg config.Config, forceRefresh bool) ([]Book, error) {
```

to:

```go
// Every path's single-book work (scanOneBook, below) runs concurrently,
// bounded by cfg.General.ScanConcurrency (0/unset means
// runtime.NumCPU()) via a semaphore channel + WaitGroup -- safe because
// metadata.Extract is documented safe for concurrent use (see its own
// doc comment) and the one other piece of state shared across workers,
// the in-memory cache, is guarded by its own mutex (see scanOneBook).
// The returned slice preserves paths' original order regardless of which
// goroutine finishes first, matching the pre-parallel behavior exactly.
//
// onProgress, if non-nil, is called once per path as it finishes --
// cache hit or fresh extraction both count -- under a dedicated mutex so
// done is strictly increasing in call order even though the workers
// producing those calls run concurrently; total is always len(paths).
// Passing nil (every caller that doesn't report progress) skips this
// entirely.
func Scan(cfg config.Config, forceRefresh bool, onProgress func(done, total int)) ([]Book, error) {
```

- [ ] **Step 4: Fix every other call site in this file**

Every other call to `Scan(cfg, ...)` in `internal/librarian/librarian_test.go` needs a third `nil` argument. Replace all occurrences of `Scan(cfg, false)` with `Scan(cfg, false, nil)`, and all occurrences of `Scan(cfg, true)` with `Scan(cfg, true, nil)`, throughout the file (24 call sites in total, not counting the two new tests added in Step 1, which already pass their own callback or `nil`).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/librarian/... -v`
Expected: PASS (all tests, including the two new ones and every pre-existing test).

- [ ] **Step 6: Run under the race detector**

Run: `go test -race ./internal/librarian/...`
Expected: PASS with no data race reports -- `progressMu` is new shared state accessed from concurrent goroutines, so this is the step that actually proves it's correctly synchronized.

- [ ] **Step 7: Commit**

```bash
git add internal/librarian/librarian.go internal/librarian/librarian_test.go
git commit -m "Add an onProgress callback to librarian.Scan"
```

---

### Task 2: Thread `onProgress` through `appapi.App.ListLibrary`

**Files:**
- Modify: `internal/appapi/library.go`
- Test: `internal/appapi/library_test.go`

**Interfaces:**
- Consumes: `librarian.Scan(cfg, forceRefresh, onProgress)` (Task 1).
- Produces: `(a *App) ListLibrary(forceRefresh bool, onProgress func(done, total int)) (LibraryView, error)`, consumed by Task 3.

- [ ] **Step 1: Write the failing test**

Add this test to `internal/appapi/library_test.go` (after the existing tests in that file):

```go
func TestListLibrary_ForwardsProgressToCallback(t *testing.T) {
	libDir := t.TempDir()
	for _, rel := range []string{
		filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"),
		filepath.Join("Fiction", "Fantasy", "Mistborn.epub"),
	} {
		path := filepath.Join(libDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte("not a real epub"), 0644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	configPath := writeTestConfigForLibrary(t, libDir, t.TempDir())
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	var calls int
	var lastTotal int
	_, err := app.ListLibrary(false, func(done, total int) {
		calls++
		lastTotal = total
	})
	if err != nil {
		t.Fatalf("ListLibrary returned error: %v", err)
	}

	if calls != 2 {
		t.Errorf("onProgress called %d times, want 2", calls)
	}
	if lastTotal != 2 {
		t.Errorf("last total = %d, want 2", lastTotal)
	}
}
```

This reuses `writeTestConfigForLibrary`, the helper already defined at the top of `internal/appapi/library_test.go` -- no new imports needed for this test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/appapi/ -run TestListLibrary_ForwardsProgressToCallback -v`
Expected: FAIL to compile (`ListLibrary` doesn't accept a second argument yet).

- [ ] **Step 3: Add the parameter**

In `internal/appapi/library.go`, change:

```go
func (a *App) ListLibrary(forceRefresh bool) (LibraryView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return LibraryView{}, err
	}

	books, err := librarian.Scan(cfg, forceRefresh)
	if err != nil {
		return LibraryView{}, err
	}
```

to:

```go
func (a *App) ListLibrary(forceRefresh bool, onProgress func(done, total int)) (LibraryView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return LibraryView{}, err
	}

	books, err := librarian.Scan(cfg, forceRefresh, onProgress)
	if err != nil {
		return LibraryView{}, err
	}
```

Update the two existing call sites in `internal/appapi/library_test.go` (`TestListLibrary_ReturnsBooksGroupedByCategory` and `TestListLibrary_EmptyLibraryReturnsEmptyView`), changing both occurrences of `app.ListLibrary(false)` to `app.ListLibrary(false, nil)`.

Update `ListLibrary`'s doc comment, changing:

```go
// forceRefresh bypasses librarian's scan cache for this call, re-extracting
// every book and repopulating the cache with fresh values -- the frontend's
// manual "Refresh" action.
func (a *App) ListLibrary(forceRefresh bool) (LibraryView, error) {
```

to:

```go
// forceRefresh bypasses librarian's scan cache for this call, re-extracting
// every book and repopulating the cache with fresh values -- the frontend's
// manual "Refresh" action. onProgress is passed straight through to
// librarian.Scan (nil-safe); this method has no progress-reporting logic
// of its own -- see librarian.Scan's doc comment for the actual contract.
func (a *App) ListLibrary(forceRefresh bool, onProgress func(done, total int)) (LibraryView, error) {
```

- [ ] **Step 4: Update `desktop/app.go`'s call site so the whole module still builds**

This task's own tests don't touch `desktop/app.go`, but changing `ListLibrary`'s signature breaks it immediately -- fix it now so `go build ./...` still passes, without yet adding the Wails-event logic Task 3 owns. In `desktop/app.go`, change:

```go
func (a *App) ListLibrary(forceRefresh bool) (appapi.LibraryView, error) {
	return a.api.ListLibrary(forceRefresh)
}
```

to:

```go
func (a *App) ListLibrary(forceRefresh bool) (appapi.LibraryView, error) {
	return a.api.ListLibrary(forceRefresh, nil)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/appapi/... ./desktop/... -v`
Expected: PASS (all tests). `go build ./...` should also succeed.

- [ ] **Step 6: Commit**

```bash
git add internal/appapi/library.go internal/appapi/library_test.go desktop/app.go
git commit -m "Thread onProgress through appapi.App.ListLibrary"
```

---

### Task 3: Emit a Wails progress event from the desktop layer

**Files:**
- Modify: `desktop/app.go`
- Test: `desktop/app_test.go`

**Interfaces:**
- Consumes: `a.api.ListLibrary(forceRefresh, onProgress)` (Task 2).
- Produces: the `"library:scan-progress"` Wails event, with payload `{done: number, total: number}` (JSON-serialized from a new unexported `libraryScanProgress` struct) -- consumed by Task 4's frontend code. Also produces `newScanProgressEmitter(ctx context.Context) func(done, total int)`, an unexported helper used only within this file.

- [ ] **Step 1: Write the failing tests**

Add these tests to `desktop/app_test.go`:

```go
func TestNewScanProgressEmitter_NilContextReturnsNil(t *testing.T) {
	if got := newScanProgressEmitter(nil); got != nil {
		t.Errorf("newScanProgressEmitter(nil) = %v, want nil", got)
	}
}

func TestNewScanProgressEmitter_NonNilContextReturnsCallback(t *testing.T) {
	if got := newScanProgressEmitter(context.Background()); got == nil {
		t.Error("newScanProgressEmitter(context.Background()) = nil, want a non-nil callback")
	}
}
```

If `desktop/app_test.go` doesn't already import `"context"`, add it.

**Why these two tests, and not one that verifies the actual event fires:** `runtime.EventsEmit(ctx, ...)` calls `log.Fatalf` (an `os.Exit`, not a recoverable panic) if `ctx` isn't a real Wails-provided context -- there is no way to fake a working one from outside the `wails` module (the `Events` interface it type-asserts against lives in `wails/v2/internal/frontend`, and one of its methods takes another internal-only type as a parameter, so it can't be implemented from this module even structurally). These two tests instead lock down the contract that actually matters for safety: a `nil` context (e.g. an `App` constructed directly in a test, before Wails' `startup` hook has ever run) must never reach `EventsEmit` at all. The real event's payload is verified manually, in this plan's final end-to-end check.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./desktop/ -run TestNewScanProgressEmitter -v`
Expected: FAIL to compile (`newScanProgressEmitter` doesn't exist yet).

- [ ] **Step 3: Add the emitter helper and wire it into `ListLibrary`**

In `desktop/app.go`, change:

```go
func (a *App) ListLibrary(forceRefresh bool) (appapi.LibraryView, error) {
	return a.api.ListLibrary(forceRefresh, nil)
}
```

to:

```go
// libraryScanProgress is the JSON payload of the "library:scan-progress"
// Wails event emitted while a Library refresh is running.
type libraryScanProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// newScanProgressEmitter returns a librarian.Scan-compatible onProgress
// callback that emits a "library:scan-progress" Wails event through ctx,
// or nil if ctx is nil. Wails' runtime.EventsEmit calls log.Fatalf (an
// os.Exit) if given a context that was never set up by Wails' own
// startup hook -- a.ctx is exactly such a context in any test that
// constructs an App directly without going through Wails, and would be
// nil there -- so nil is treated as "don't report progress" rather than
// ever risking that call.
func newScanProgressEmitter(ctx context.Context) func(done, total int) {
	if ctx == nil {
		return nil
	}
	return func(done, total int) {
		runtime.EventsEmit(ctx, "library:scan-progress", libraryScanProgress{Done: done, Total: total})
	}
}

func (a *App) ListLibrary(forceRefresh bool) (appapi.LibraryView, error) {
	return a.api.ListLibrary(forceRefresh, newScanProgressEmitter(a.ctx))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./desktop/... -v`
Expected: PASS (all tests, including the two new ones).

- [ ] **Step 5: Commit**

```bash
git add desktop/app.go desktop/app_test.go
git commit -m "Emit a Wails library:scan-progress event from ListLibrary"
```

---

### Task 4: Show elapsed time and a book counter in the Library loading banner

**Files:**
- Modify: `desktop/frontend/src/lib/types.ts`
- Modify: `desktop/frontend/src/lib/LibraryView.svelte`
- Test: `desktop/frontend/src/lib/LibraryView.test.ts`

**Interfaces:**
- Consumes: the `"library:scan-progress"` Wails event (Task 3), via `EventsOn` from `desktop/frontend/wailsjs/runtime/runtime` (already generated, unmodified by this plan).
- Produces: `ScanProgress` type in `types.ts`, consumed only within this task's own component.

- [ ] **Step 1: Add the `ScanProgress` type**

In `desktop/frontend/src/lib/types.ts`, add, near the other `LibraryBookView`/`LibraryShelf` declarations:

```ts
export interface ScanProgress {
  done: number;
  total: number;
}
```

- [ ] **Step 2: Write the failing test**

Add this to `desktop/frontend/src/lib/LibraryView.test.ts`. First, add the runtime mock and its import, changing:

```ts
vi.mock('../../wailsjs/go/main/App', () => ({
  ListLibrary: vi.fn(),
  OpenFile: vi.fn(),
}));

import { ListLibrary } from '../../wailsjs/go/main/App';
```

to:

```ts
vi.mock('../../wailsjs/go/main/App', () => ({
  ListLibrary: vi.fn(),
  OpenFile: vi.fn(),
}));
vi.mock('../../wailsjs/runtime/runtime', () => ({
  EventsOn: vi.fn(),
}));

import { ListLibrary } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
```

Then add this test at the end of the `describe('LibraryView', ...)` block:

```ts
  it('shows elapsed time immediately, then the book counter once a progress event lands, and unsubscribes when done', async () => {
    vi.useFakeTimers();

    let resolveList!: (v: { books: LibraryBookView[]; categories: string[] }) => void;
    const pending = new Promise<{ books: LibraryBookView[]; categories: string[] }>((resolve) => {
      resolveList = resolve;
    });
    vi.mocked(ListLibrary).mockReturnValue(pending);

    let progressHandler: (p: { done: number; total: number }) => void = () => {};
    const unsubscribe = vi.fn();
    vi.mocked(EventsOn).mockImplementation((_name, cb) => {
      progressHandler = cb;
      return unsubscribe;
    });

    render(LibraryView, { category: '' });

    await vi.advanceTimersByTimeAsync(2000);
    expect(screen.getByText('Loading library… 2s')).toBeInTheDocument();

    progressHandler({ done: 3, total: 10 });
    await vi.advanceTimersByTimeAsync(1000);
    expect(screen.getByText('Loading library… 3 / 10 books · 3s')).toBeInTheDocument();

    resolveList({ books: [], categories: [] });
    await screen.findByText('No books found in the library folder yet.');
    expect(unsubscribe).toHaveBeenCalledTimes(1);

    vi.useRealTimers();
  });
```

- [ ] **Step 3: Run test to verify it fails**

Run: `npx vitest run src/lib/LibraryView.test.ts` (from `desktop/frontend/`)
Expected: FAIL -- the loading banner still shows the static "Loading library…" text with no elapsed time or counter, and `EventsOn` is never called.

- [ ] **Step 4: Implement the elapsed timer and progress subscription**

In `desktop/frontend/src/lib/LibraryView.svelte`, change the script block:

```svelte
<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import ShelfRow from './ShelfRow.svelte';
  import { groupIntoShelves, type LibraryBookView, type LibrarySortMode } from './types';
  import { ListLibrary } from '../../wailsjs/go/main/App';

  export let category: string = '';

  const dispatch = createEventDispatcher<{ categoriesLoaded: string[] }>();

  let books: LibraryBookView[] = [];
  let loadError = '';
  let loading = false;
  let sortMode: LibrarySortMode = 'title';

  onMount(load);

  async function load(force: boolean = false) {
    loading = true;
    loadError = '';
    try {
      const view = await ListLibrary(force);
      books = view.books ?? [];
      dispatch('categoriesLoaded', view.categories ?? []);
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
      books = [];
    } finally {
      loading = false;
    }
  }

  $: shelves = groupIntoShelves(books, category, sortMode);
</script>
```

to:

```svelte
<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import ShelfRow from './ShelfRow.svelte';
  import { groupIntoShelves, type LibraryBookView, type LibrarySortMode, type ScanProgress } from './types';
  import { ListLibrary } from '../../wailsjs/go/main/App';
  import { EventsOn } from '../../wailsjs/runtime/runtime';

  export let category: string = '';

  const dispatch = createEventDispatcher<{ categoriesLoaded: string[] }>();

  let books: LibraryBookView[] = [];
  let loadError = '';
  let loading = false;
  let sortMode: LibrarySortMode = 'title';
  let elapsedSeconds = 0;
  let progress: ScanProgress | null = null;

  onMount(load);

  function formatElapsed(seconds: number): string {
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    const remainder = seconds % 60;
    return `${minutes}m ${remainder}s`;
  }

  async function load(force: boolean = false) {
    loading = true;
    loadError = '';
    elapsedSeconds = 0;
    progress = null;

    const tick = setInterval(() => {
      elapsedSeconds += 1;
    }, 1000);
    const unsubscribe = EventsOn('library:scan-progress', (p: ScanProgress) => {
      progress = p;
    });

    try {
      const view = await ListLibrary(force);
      books = view.books ?? [];
      dispatch('categoriesLoaded', view.categories ?? []);
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
      books = [];
    } finally {
      loading = false;
      clearInterval(tick);
      unsubscribe();
    }
  }

  $: shelves = groupIntoShelves(books, category, sortMode);
  $: loadingMessage = progress
    ? `Loading library… ${progress.done} / ${progress.total} books · ${formatElapsed(elapsedSeconds)}`
    : `Loading library… ${formatElapsed(elapsedSeconds)}`;
</script>
```

Then, in the same file's markup, change:

```svelte
  {#if loading}
    <p>Loading library…</p>
```

to:

```svelte
  {#if loading}
    <p>{loadingMessage}</p>
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `npx vitest run src/lib/LibraryView.test.ts` (from `desktop/frontend/`)
Expected: PASS (all tests, including the new one and every pre-existing test in this file).

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/lib/types.ts desktop/frontend/src/lib/LibraryView.svelte desktop/frontend/src/lib/LibraryView.test.ts
git commit -m "Show elapsed time and a book counter in the Library loading banner"
```

---

### Task 5: Add `GetScanConcurrency`/`SetScanConcurrency` to `appapi`

**Files:**
- Modify: `internal/appapi/settings.go`
- Test: `internal/appapi/settings_test.go`

**Interfaces:**
- Consumes: `config.General.ScanConcurrency` (already exists), `config.Load`/`config.Save` (already exist).
- Produces: `(a *App) GetScanConcurrency() (ScanConcurrencyView, error)`, `(a *App) SetScanConcurrency(n int) error`, and the `ScanConcurrencyView{Configured, Detected int}` struct -- all consumed by Task 6's wailsjs bindings.

- [ ] **Step 1: Write the failing tests**

Add `"runtime"` to `internal/appapi/settings_test.go`'s import block, changing:

```go
import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/BLibOrg/internal/config"
	"github.com/FrancisChung/BLibOrg/internal/covercache"
)
```

to:

```go
import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/FrancisChung/BLibOrg/internal/config"
	"github.com/FrancisChung/BLibOrg/internal/covercache"
)
```

Add these tests to `internal/appapi/settings_test.go`:

```go
func TestGetScanConcurrency_ReturnsConfiguredAndDetected(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(configPath, config.Config{General: config.General{ScanConcurrency: 4}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	got, err := app.GetScanConcurrency()
	if err != nil {
		t.Fatalf("GetScanConcurrency returned error: %v", err)
	}
	if got.Configured != 4 {
		t.Errorf("Configured = %d, want 4", got.Configured)
	}
	if got.Detected != runtime.NumCPU() {
		t.Errorf("Detected = %d, want %d (runtime.NumCPU())", got.Detected, runtime.NumCPU())
	}
}

func TestGetScanConcurrency_PropagatesConfigLoadError(t *testing.T) {
	app := NewApp()
	app.configPath = func() (string, error) { return "", os.ErrNotExist }

	if _, err := app.GetScanConcurrency(); err == nil {
		t.Error("GetScanConcurrency returned nil error, want the config-load failure to propagate")
	}
}

func TestSetScanConcurrency_PersistsValue(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(configPath, config.Config{}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	if err := app.SetScanConcurrency(6); err != nil {
		t.Fatalf("SetScanConcurrency returned error: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.General.ScanConcurrency != 6 {
		t.Errorf("General.ScanConcurrency = %d, want 6", cfg.General.ScanConcurrency)
	}
}

func TestSetScanConcurrency_ZeroMeansAuto(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(configPath, config.Config{General: config.General{ScanConcurrency: 4}}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	if err := app.SetScanConcurrency(0); err != nil {
		t.Fatalf("SetScanConcurrency returned error: %v", err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.General.ScanConcurrency != 0 {
		t.Errorf("General.ScanConcurrency = %d, want 0 (auto)", cfg.General.ScanConcurrency)
	}
}

func TestSetScanConcurrency_RejectsNegativeWithoutTouchingConfig(t *testing.T) {
	app := NewApp()
	app.configPath = func() (string, error) {
		t.Fatal("configPath should not be called for a rejected negative value")
		return "", nil
	}

	if err := app.SetScanConcurrency(-1); err == nil {
		t.Error("SetScanConcurrency(-1) returned nil error, want a validation error")
	}
}

func TestSetScanConcurrency_PropagatesConfigLoadError(t *testing.T) {
	app := NewApp()
	app.configPath = func() (string, error) { return "", os.ErrNotExist }

	if err := app.SetScanConcurrency(4); err == nil {
		t.Error("SetScanConcurrency returned nil error, want the config-load failure to propagate")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/appapi/ -run 'TestGetScanConcurrency|TestSetScanConcurrency' -v`
Expected: FAIL to compile (`GetScanConcurrency`/`SetScanConcurrency`/`ScanConcurrencyView` don't exist yet).

- [ ] **Step 3: Implement both methods**

In `internal/appapi/settings.go`, change:

```go
// This file backs the Settings view's maintenance actions -- currently
// just resetting the cover cache.
package appapi

import (
	"os"

	"github.com/FrancisChung/BLibOrg/internal/covercache"
	"github.com/FrancisChung/BLibOrg/internal/librarycache"
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

to:

```go
// This file backs the Settings view's maintenance actions -- resetting
// the cover cache, and viewing/setting the library-scan concurrency.
package appapi

import (
	"fmt"
	"os"
	"runtime"

	"github.com/FrancisChung/BLibOrg/internal/config"
	"github.com/FrancisChung/BLibOrg/internal/covercache"
	"github.com/FrancisChung/BLibOrg/internal/librarycache"
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

// ScanConcurrencyView is GetScanConcurrency's result: Configured is the
// raw cfg.General.ScanConcurrency value (0 means unset), Detected is
// runtime.NumCPU() -- the value librarian.Scan actually falls back to
// when Configured is 0. The Settings view pre-fills its input with
// Configured if it's > 0, else Detected, so the field always shows a
// concrete number rather than a blank/zero that reads as "unset."
type ScanConcurrencyView struct {
	Configured int `json:"configured"`
	Detected   int `json:"detected"`
}

// GetScanConcurrency reports the Settings view's current scan-concurrency
// state, for the concurrency input's pre-filled value.
func (a *App) GetScanConcurrency() (ScanConcurrencyView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return ScanConcurrencyView{}, err
	}
	return ScanConcurrencyView{Configured: cfg.General.ScanConcurrency, Detected: runtime.NumCPU()}, nil
}

// SetScanConcurrency persists n as cfg.General.ScanConcurrency via a
// plain config.Load/config.Save round trip -- accepted as-is even though
// config.Save's yaml.Marshal strips comments and may reorder map keys,
// per this plan's Global Constraints. n == 0 means "auto" (see
// ScanConcurrencyView's doc comment); n < 0 is rejected outright, before
// the config is even loaded, since librarian.Scan's own "<= 0 means
// auto" convention would otherwise silently treat a negative typo the
// same as 0.
func (a *App) SetScanConcurrency(n int) error {
	if n < 0 {
		return fmt.Errorf("scan concurrency must be 0 or greater, got %d", n)
	}
	path, err := a.configPath()
	if err != nil {
		return err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	cfg.General.ScanConcurrency = n
	return config.Save(path, cfg)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/appapi/... -v`
Expected: PASS (all tests, including the six new ones).

- [ ] **Step 5: Commit**

```bash
git add internal/appapi/settings.go internal/appapi/settings_test.go
git commit -m "Add GetScanConcurrency/SetScanConcurrency to appapi"
```

---

### Task 6: Expose the new methods through Wails' generated bindings

**Files:**
- Modify: `desktop/frontend/wailsjs/go/main/App.d.ts`
- Modify: `desktop/frontend/wailsjs/go/main/App.js`
- Modify: `desktop/frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: `appapi.GetScanConcurrency`/`appapi.SetScanConcurrency`/`appapi.ScanConcurrencyView` (Task 5).
- Produces: the `GetScanConcurrency()`/`SetScanConcurrency(n)` JS functions and `appapi.ScanConcurrencyView` TS class, consumed by Task 7.

These three files are Wails' generated bindings, hand-maintained in this repo (confirmed by prior commits like `101e6c5`, which added `ResetCoverCache`'s bindings by hand in the same style) -- there is no `wails generate` step to run here, just matching the existing pattern exactly.

- [ ] **Step 1: Add the TypeScript declarations**

In `desktop/frontend/wailsjs/go/main/App.d.ts`, change:

```ts
export function ConfirmUndo(arg1:number):Promise<boolean>;

export function ListCategoryWarnings():Promise<Array<appapi.CategoryWarningView>>;
```

to:

```ts
export function ConfirmUndo(arg1:number):Promise<boolean>;

export function GetScanConcurrency():Promise<appapi.ScanConcurrencyView>;

export function ListCategoryWarnings():Promise<Array<appapi.CategoryWarningView>>;
```

And change:

```ts
export function SetCoverOverrideCustomFromFile(arg1:string,arg2:string):Promise<string>;

export function UndoBatch(arg1:string):Promise<void>;
```

to:

```ts
export function SetCoverOverrideCustomFromFile(arg1:string,arg2:string):Promise<string>;

export function SetScanConcurrency(arg1:number):Promise<void>;

export function UndoBatch(arg1:string):Promise<void>;
```

- [ ] **Step 2: Add the JS implementations**

In `desktop/frontend/wailsjs/go/main/App.js`, change:

```js
export function ConfirmUndo(arg1) {
  return window['go']['main']['App']['ConfirmUndo'](arg1);
}

export function ListCategoryWarnings() {
  return window['go']['main']['App']['ListCategoryWarnings']();
}
```

to:

```js
export function ConfirmUndo(arg1) {
  return window['go']['main']['App']['ConfirmUndo'](arg1);
}

export function GetScanConcurrency() {
  return window['go']['main']['App']['GetScanConcurrency']();
}

export function ListCategoryWarnings() {
  return window['go']['main']['App']['ListCategoryWarnings']();
}
```

And change:

```js
export function SetCoverOverrideCustomFromFile(arg1, arg2) {
  return window['go']['main']['App']['SetCoverOverrideCustomFromFile'](arg1, arg2);
}

export function UndoBatch(arg1) {
  return window['go']['main']['App']['UndoBatch'](arg1);
}
```

to:

```js
export function SetCoverOverrideCustomFromFile(arg1, arg2) {
  return window['go']['main']['App']['SetCoverOverrideCustomFromFile'](arg1, arg2);
}

export function SetScanConcurrency(arg1) {
  return window['go']['main']['App']['SetScanConcurrency'](arg1);
}

export function UndoBatch(arg1) {
  return window['go']['main']['App']['UndoBatch'](arg1);
}
```

- [ ] **Step 3: Add the `ScanConcurrencyView` model class**

In `desktop/frontend/wailsjs/go/models.ts`, find the closing of the `appapi` namespace (the last class in the file, `OperationBatchView`, followed by the namespace's closing `}`), and add a new class before that closing brace:

```ts
	export class ScanConcurrencyView {
	    configured: number;
	    detected: number;

	    static createFrom(source: any = {}) {
	        return new ScanConcurrencyView(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configured = source["configured"];
	        this.detected = source["detected"];
	    }
	}
```

- [ ] **Step 4: Verify the frontend still typechecks**

Run: `cd desktop/frontend && npx tsc --noEmit`
Expected: no new type errors (the two new functions/class are additive; nothing else references them yet, so this step is purely a syntax/typo check on the hand-written bindings).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/wailsjs/go/main/App.d.ts desktop/frontend/wailsjs/go/main/App.js desktop/frontend/wailsjs/go/models.ts
git commit -m "Expose GetScanConcurrency/SetScanConcurrency through Wails bindings"
```

---

### Task 7: Add a scan-concurrency control to the Settings view

**Files:**
- Modify: `desktop/frontend/src/lib/SettingsView.svelte`
- Test: `desktop/frontend/src/lib/SettingsView.test.ts`

**Interfaces:**
- Consumes: `GetScanConcurrency()`/`SetScanConcurrency(n)` (Task 6).
- Produces: nothing consumed elsewhere -- this is the plan's final, user-facing task.

- [ ] **Step 1: Write the failing tests**

In `desktop/frontend/src/lib/SettingsView.test.ts`, change:

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
```

to:

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import SettingsView from './SettingsView.svelte';

vi.mock('../../wailsjs/go/main/App', () => ({
  ConfirmResetCoverCache: vi.fn(),
  ResetCoverCache: vi.fn(),
  GetScanConcurrency: vi.fn(),
  SetScanConcurrency: vi.fn(),
}));

import {
  ConfirmResetCoverCache,
  ResetCoverCache,
  GetScanConcurrency,
  SetScanConcurrency,
} from '../../wailsjs/go/main/App';

describe('SettingsView', () => {
  beforeEach(() => {
    vi.mocked(GetScanConcurrency).mockResolvedValue({ configured: 0, detected: 8 });
  });

  it('does not reset when the confirmation dialog is declined', async () => {
```

The `beforeEach` covers every pre-existing test in this file too (they all render `SettingsView`, which will now call `GetScanConcurrency` on mount regardless of which feature the test is about) -- without it, those tests would break since `GetScanConcurrency`'s mock would otherwise resolve `undefined`.

Add these new tests at the end of the `describe('SettingsView', ...)` block:

```ts
  it('pre-fills the concurrency field with the detected core count when unset', async () => {
    vi.mocked(GetScanConcurrency).mockResolvedValue({ configured: 0, detected: 8 });
    render(SettingsView);

    const input = await screen.findByRole('spinbutton', { name: 'Scan concurrency' });
    expect(input).toHaveValue(8);
  });

  it('pre-fills the concurrency field with the configured value when set', async () => {
    vi.mocked(GetScanConcurrency).mockResolvedValue({ configured: 3, detected: 8 });
    render(SettingsView);

    const input = await screen.findByRole('spinbutton', { name: 'Scan concurrency' });
    expect(input).toHaveValue(3);
  });

  it('saves the concurrency value and shows a success banner', async () => {
    vi.mocked(GetScanConcurrency).mockResolvedValue({ configured: 0, detected: 8 });
    vi.mocked(SetScanConcurrency).mockResolvedValue(undefined);
    render(SettingsView);

    const input = await screen.findByRole('spinbutton', { name: 'Scan concurrency' });
    await fireEvent.input(input, { target: { value: '2' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(SetScanConcurrency).toHaveBeenCalledWith(2);
    await screen.findByText('Saved. Takes effect on the next Library refresh.');
  });

  it('shows an error banner when SetScanConcurrency rejects', async () => {
    vi.mocked(GetScanConcurrency).mockResolvedValue({ configured: 0, detected: 8 });
    vi.mocked(SetScanConcurrency).mockRejectedValue(new Error('permission denied'));
    render(SettingsView);

    await screen.findByRole('spinbutton', { name: 'Scan concurrency' });
    await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await screen.findByText('permission denied');
  });
```

- [ ] **Step 2: Run tests to verify the new ones fail**

Run: `npx vitest run src/lib/SettingsView.test.ts` (from `desktop/frontend/`)
Expected: FAIL -- there's no "Scan concurrency" input or "Save" button yet.

- [ ] **Step 3: Add the concurrency section**

In `desktop/frontend/src/lib/SettingsView.svelte`, change the script block:

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
```

to:

```svelte
<script lang="ts">
  import { onMount } from 'svelte';
  import {
    ConfirmResetCoverCache,
    ResetCoverCache,
    GetScanConcurrency,
    SetScanConcurrency,
  } from '../../wailsjs/go/main/App';

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

  let concurrencyLoaded = false;
  let concurrencyValue = 0;
  let concurrencySaving = false;
  let concurrencyError = '';
  let concurrencySuccess = '';

  onMount(async () => {
    try {
      const view = await GetScanConcurrency();
      concurrencyValue = view.configured > 0 ? view.configured : view.detected;
    } catch (e) {
      concurrencyError = e instanceof Error ? e.message : String(e);
    } finally {
      concurrencyLoaded = true;
    }
  });

  async function handleSaveConcurrency() {
    concurrencySaving = true;
    concurrencyError = '';
    concurrencySuccess = '';
    try {
      await SetScanConcurrency(concurrencyValue);
      concurrencySuccess = 'Saved. Takes effect on the next Library refresh.';
    } catch (e) {
      concurrencyError = e instanceof Error ? e.message : String(e);
    } finally {
      concurrencySaving = false;
    }
  }
</script>
```

Then add a new section to the markup, after the existing cover-cache `<section class="settings-block">...</section>` block and before the `<style>` block:

```svelte
<section class="settings-block">
  <h3>Library scan concurrency</h3>
  <p>
    How many books are processed in parallel during a Library refresh.
    Pre-filled with your machine's detected core count; lower it if a
    full-speed refresh competes too much with other work.
  </p>
  {#if concurrencyError}
    <div class="banner error">{concurrencyError}</div>
  {/if}
  {#if concurrencySuccess}
    <div class="banner success">{concurrencySuccess}</div>
  {/if}
  {#if concurrencyLoaded}
    <div class="concurrency-row">
      <input
        type="number"
        min="0"
        bind:value={concurrencyValue}
        disabled={concurrencySaving}
        aria-label="Scan concurrency"
      />
      <button
        type="button"
        class="reset-button"
        disabled={concurrencySaving}
        on:click={handleSaveConcurrency}
      >
        {concurrencySaving ? 'Saving…' : 'Save'}
      </button>
    </div>
  {/if}
</section>
```

Finally, add styling for `.concurrency-row` to the `<style>` block, after the existing `.reset-button:disabled` rule:

```css
  .concurrency-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .concurrency-row input {
    width: 70px;
    padding: 6px 8px;
    border-radius: 6px;
    border: 1px solid var(--bf-border);
    background: var(--bf-surface);
    color: var(--bf-text);
    font-family: inherit;
    font-size: 13px;
  }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `npx vitest run src/lib/SettingsView.test.ts` (from `desktop/frontend/`)
Expected: PASS (all tests, including the four new ones and every pre-existing test in this file).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/SettingsView.svelte desktop/frontend/src/lib/SettingsView.test.ts
git commit -m "Add a scan-concurrency control to the Settings view"
```

---

## Manual Verification (after all tasks complete)

1. Run `go build ./... && go vet ./...` at the repo root, and `cd desktop/frontend && npx vitest run && npx tsc --noEmit` to confirm everything still builds and every test passes end to end.
2. `cd desktop && ./build.sh` to produce a real binary, then run it (e.g. under `xvfb-run` if no display is attached) and open the Library view: click "Refresh" against the real library and confirm the loading banner shows increasing elapsed time and, once extraction starts, a "`<done>` / `<total>` books" counter that counts up to the real total -- this is the only way to observe the actual `library:scan-progress` Wails event firing, per Task 3's note that it isn't unit-testable from outside the `wails` module.
3. In the same running app, open Settings and confirm the "Library scan concurrency" field is pre-filled with a real detected core count, change it, click Save, confirm the success banner appears, then re-open Settings (or restart the app) and confirm the value persisted.
4. Inspect the real `config.yaml` after Step 3's save to confirm `scan_concurrency` was written correctly -- and note (not a bug to fix) whether comments/formatting were affected, as an observed consequence of the accepted full-rewrite approach.
