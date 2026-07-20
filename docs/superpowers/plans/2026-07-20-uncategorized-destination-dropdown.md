# Uncategorized Destination Dropdown Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user manually pick a real category/subcategory destination for an Uncategorized book card from a right-justified dropdown, with the pick surviving further edits and Apply moving the file there.

**Architecture:** Add a `CategoryManual bool` flag to `book.Book` that makes `categorizer.Categorize` a no-op and makes `book.Book.Status()` report `SourceManual` ("Edited"). Thread that flag through the `appapi` DTO layer so it survives the existing `Recompute` round trip. Expose `config.yaml`'s categories to the frontend via a new `Categories()` read. In `BookCard.svelte`, a `<select>` in the existing badges row lets the user pick a destination, reusing the existing `edited` → `Recompute()` flow that title/author edits already use — no changes to Apply itself.

**Tech Stack:** Go (backend, `internal/*`, `desktop/app.go`), Wails v2 bindings, Svelte + TypeScript (`desktop/frontend`), Go `testing`, Vitest + `@testing-library/svelte`.

## Global Constraints

- A manually-picked category/subcategory must survive subsequent `Recompute` calls triggered by Title/Author/Year edits (does not get silently reverted by rule-based auto-categorization).
- `Categories()` must exclude the `"Uncategorized"` category itself and return results sorted by Category then Subcategory (Go map iteration order is not stable — this must be sorted server-side).
- The dropdown shares the card's existing badges row (right-aligned via `justify-content: space-between`) — no new row is added to the card.
- The dropdown is a single flat list of `"Category / Subcategory"` entries (plain `"Category"` when a category has no subcategories) — not a two-step category-then-subcategory picker.
- The dropdown stays visible (and reflects the pick) once `categoryManual` is true, even though `category` is no longer literally `"Uncategorized"`.
- No "revert to auto" option in the dropdown — out of scope per the spec.

---

## Task 1: `book.Book` gains `CategoryManual` and `Status()` precedence

**Files:**
- Modify: `internal/book/book.go:46-94`
- Test: `internal/book/book_test.go`

**Interfaces:**
- Produces: `book.Book.CategoryManual bool` field; `book.Book.Status()` returns `SourceManual` when `CategoryManual` is true (unless Title/Author/Year unresolved state takes precedence, per existing rules).

- [ ] **Step 1: Write the failing tests**

Add two new cases to the `tests` table in `TestBookStatus` in `internal/book/book_test.go`, right after the last existing case (`"title manually edited but author unresolved -- Partial beats Edited"`, currently ending at line 100 with its closing `},`):

```go
		{
			name: "manual category pick reports Edited even when title/author/year are all Metadata",
			book: Book{
				Title:          Field{"T", SourceMetadata},
				Author:         Field{"A", SourceMetadata},
				Year:           Field{"2024", SourceMetadata},
				CategoryManual: true,
			},
			want: SourceManual,
		},
		{
			name: "manual category pick does not override an unresolved title",
			book: Book{
				Title:          Field{"", SourceUnresolved},
				Author:         Field{"A", SourceMetadata},
				Year:           Field{"2024", SourceMetadata},
				CategoryManual: true,
			},
			want: SourceUnresolved,
		},
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/book/... -run TestBookStatus -v`
Expected: FAIL to compile — `unknown field CategoryManual in struct literal of type Book` (the field doesn't exist yet).

- [ ] **Step 3: Implement the field and Status() precedence**

In `internal/book/book.go`, add `CategoryManual` to the `Book` struct (currently lines 46-63):

```go
type Book struct {
	SourcePath string
	Format     string
	SizeBytes  int64

	Title  Field
	Author Field
	Year   Field

	Subject         string
	Category        string
	Subcategory     string
	CategoryManual  bool
	CategoryWarning string
	DestPath        string

	DuplicateGroupID string
	DuplicateStatus  DuplicateStatus
}
```

Then update `Status()` (currently lines 74-94) to check it right after the `Partial` check, before the Manual/Heuristic/Metadata precedence loop:

```go
func (b Book) Status() Source {
	if b.Title.Source == SourceUnresolved {
		return SourceUnresolved
	}
	if b.Author.Source == SourceUnresolved || b.Year.Source == SourceUnresolved {
		return SourcePartial
	}
	if b.CategoryManual {
		return SourceManual
	}

	fields := []Field{b.Title, b.Author, b.Year}
	for _, f := range fields {
		if f.Source == SourceManual {
			return SourceManual
		}
	}
	for _, f := range fields {
		if f.Source == SourceHeuristic {
			return SourceHeuristic
		}
	}
	return SourceMetadata
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/book/... -v`
Expected: PASS, all `TestBookStatus` subtests including the two new ones.

- [ ] **Step 5: Commit**

```bash
git add internal/book/book.go internal/book/book_test.go
git commit -m "$(cat <<'EOF'
Add CategoryManual to book.Book, feeding Status() precedence

Lets a manually-picked category report the row as Edited, the same
precedence slot a manually-edited Title/Author/Year already occupies.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `categorizer.Categorize` skips re-categorization when `CategoryManual`

**Files:**
- Modify: `internal/categorizer/categorizer.go:20-21`
- Test: `internal/categorizer/categorizer_test.go`

**Interfaces:**
- Consumes: `book.Book.CategoryManual` (Task 1).
- Produces: `Categorize` is now a no-op (leaves `Category`/`Subcategory`/`CategoryWarning` untouched) whenever `b.CategoryManual` is true.

- [ ] **Step 1: Write the failing test**

Add to `internal/categorizer/categorizer_test.go` (uses the existing `testConfig()` helper already in this file):

```go
func TestCategorize_SkipsRecategorizationWhenCategoryManual(t *testing.T) {
	cfg := testConfig()
	b := &book.Book{
		SourcePath:     "/inbox/foundation.epub",
		Author:         book.Field{Value: "Isaac Asimov"}, // would normally match Fiction/Sci-Fi
		Category:       "NonFiction",
		Subcategory:    "Technology",
		CategoryManual: true,
	}
	Categorize(b, cfg)
	if b.Category != "NonFiction" || b.Subcategory != "Technology" {
		t.Errorf("Category/Subcategory = %s/%s, want NonFiction/Technology (manual pick preserved, not overwritten by the author rule)", b.Category, b.Subcategory)
	}
	if b.CategoryWarning != "" {
		t.Errorf("CategoryWarning = %q, want empty (Categorize should not run at all when CategoryManual)", b.CategoryWarning)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/categorizer/... -run TestCategorize_SkipsRecategorizationWhenCategoryManual -v`
Expected: FAIL — `Category/Subcategory = Fiction/Sci-Fi, want NonFiction/Technology` (the author rule currently always wins).

- [ ] **Step 3: Implement the skip**

In `internal/categorizer/categorizer.go`, add the early return as the first line of `Categorize` (currently starting at line 20):

```go
func Categorize(b *book.Book, cfg config.Config) {
	if b.CategoryManual {
		return
	}

	filename := filepath.Base(b.SourcePath)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/categorizer/... -v`
Expected: PASS, all tests in the package including the new one and every pre-existing `TestCategorize_*` case (unaffected — none of them set `CategoryManual`).

- [ ] **Step 5: Commit**

```bash
git add internal/categorizer/categorizer.go internal/categorizer/categorizer_test.go
git commit -m "$(cat <<'EOF'
Skip Categorize when a book's category was manually picked

Categorize always ran, so a rule-based recompute (e.g. triggered by a
later title/author edit) would silently discard a user's manual
destination pick. A CategoryManual book now short-circuits the whole
function, leaving Category/Subcategory/CategoryWarning as-is.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `appapi` DTO carries `Category`/`Subcategory`/`CategoryManual` through

**Files:**
- Modify: `internal/appapi/dto.go:23-124`
- Test: `internal/appapi/dto_test.go`, `internal/appapi/dto_roundtrip_test.go`

**Interfaces:**
- Consumes: `book.Book.CategoryManual` (Task 1).
- Produces: `appapi.BookView.CategoryManual bool` (json `categoryManual`); `viewToBook` now carries `Category`/`Subcategory`/`CategoryManual` from the incoming view (previously dropped); `bookToView` now carries `CategoryManual` out (it already carried `Category`/`Subcategory`).

- [ ] **Step 1: Write the failing tests**

Add to `internal/appapi/dto_test.go`:

```go
func TestBookToView_CarriesCategoryManual(t *testing.T) {
	b := &book.Book{
		SourcePath:     "/inbox/foundation.epub",
		Title:          book.Field{Value: "Foundation", Source: book.SourceMetadata},
		Category:       "Fiction",
		Subcategory:    "Sci-Fi",
		CategoryManual: true,
	}

	v := bookToView(b)

	if !v.CategoryManual {
		t.Error("CategoryManual = false, want true")
	}
	if v.Category != "Fiction" || v.Subcategory != "Sci-Fi" {
		t.Errorf("Category/Subcategory = %q/%q, want Fiction/Sci-Fi", v.Category, v.Subcategory)
	}
}
```

Add to `internal/appapi/dto_roundtrip_test.go`:

```go
func TestViewToBook_RoundTripsCategoryFields(t *testing.T) {
	v := BookView{
		SourcePath:     "/inbox/foundation.epub",
		Category:       "Fiction",
		Subcategory:    "Sci-Fi",
		CategoryManual: true,
	}

	b := viewToBook(v)

	if b.Category != "Fiction" || b.Subcategory != "Sci-Fi" || !b.CategoryManual {
		t.Errorf("Category/Subcategory/CategoryManual = %q/%q/%v, want Fiction/Sci-Fi/true", b.Category, b.Subcategory, b.CategoryManual)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/appapi/... -run 'TestBookToView_CarriesCategoryManual|TestViewToBook_RoundTripsCategoryFields' -v`
Expected: FAIL to compile — `unknown field CategoryManual in struct literal of type appapi.BookView` (the field doesn't exist on `BookView` yet, and `viewToBook` doesn't carry `Category`/`Subcategory` yet either).

- [ ] **Step 3: Implement the DTO changes**

In `internal/appapi/dto.go`, add `CategoryManual` to the `BookView` struct (currently lines 23-40), right after `CategoryWarning`:

```go
type BookView struct {
	ID               string `json:"id"`
	SourcePath       string `json:"sourcePath"`
	OldFilename      string `json:"oldFilename"`
	Format           string `json:"format"`
	SizeBytes        int64  `json:"sizeBytes"`
	Subject          string `json:"subject"`
	Title            Field  `json:"title"`
	Author           Field  `json:"author"`
	Year             Field  `json:"year"`
	Status           string `json:"status"`
	Category         string `json:"category"`
	Subcategory      string `json:"subcategory"`
	CategoryWarning  string `json:"categoryWarning"`
	CategoryManual   bool   `json:"categoryManual"`
	DestPath         string `json:"destPath"`
	DuplicateStatus  string `json:"duplicateStatus"`
	DuplicateGroupID string `json:"duplicateGroupId"`
}
```

Update `bookToView` (currently lines 59-78) to include `CategoryManual`:

```go
func bookToView(b *book.Book) BookView {
	return BookView{
		ID:               b.SourcePath,
		SourcePath:       b.SourcePath,
		OldFilename:      filepath.Base(b.SourcePath),
		Format:           b.Format,
		SizeBytes:        b.SizeBytes,
		Subject:          b.Subject,
		Title:            fieldToView(b.Title),
		Author:           fieldToView(b.Author),
		Year:             fieldToView(b.Year),
		Status:           b.Status().String(),
		Category:         b.Category,
		Subcategory:      b.Subcategory,
		CategoryWarning:  b.CategoryWarning,
		CategoryManual:   b.CategoryManual,
		DestPath:         b.DestPath,
		DuplicateStatus:  duplicateStatusToView(b.DuplicateStatus),
		DuplicateGroupID: b.DuplicateGroupID,
	}
}
```

Update `viewToBook` (currently lines 112-124) to carry `Category`/`Subcategory`/`CategoryManual` in:

```go
func viewToBook(v BookView) *book.Book {
	return &book.Book{
		SourcePath:       v.SourcePath,
		Format:           v.Format,
		SizeBytes:        v.SizeBytes,
		Subject:          v.Subject,
		Title:            fieldFromView(v.Title),
		Author:           fieldFromView(v.Author),
		Year:             fieldFromView(v.Year),
		Category:         v.Category,
		Subcategory:      v.Subcategory,
		CategoryManual:   v.CategoryManual,
		DuplicateGroupID: v.DuplicateGroupID,
		DuplicateStatus:  duplicateStatusFromView(v.DuplicateStatus),
	}
}
```

The doc comment above `viewToBook` currently says `Category, Subcategory, DestPath, and Status itself are outputs, not carried over -- callers recompute them`; update it to reflect the new behavior:

```go
// viewToBook converts a BookView back into a book.Book carrying only the
// fields that are genuine inputs to Categorize/BuildPath/Status. DestPath
// and Status itself are pure outputs, never carried over. Category and
// Subcategory ARE carried over (as of CategoryManual support) so a manual
// destination pick survives into Categorize, which itself immediately
// returns without touching them when CategoryManual is true; when it's
// false these values are just overwritten by Categorize as before.
func viewToBook(v BookView) *book.Book {
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/appapi/... -v`
Expected: PASS, all tests in the package (including the two new ones and every pre-existing `dto_test.go`/`dto_roundtrip_test.go` case, which don't set `CategoryManual` and are unaffected).

- [ ] **Step 5: Commit**

```bash
git add internal/appapi/dto.go internal/appapi/dto_test.go internal/appapi/dto_roundtrip_test.go
git commit -m "$(cat <<'EOF'
Thread Category/Subcategory/CategoryManual through the BookView DTO

viewToBook previously discarded incoming Category/Subcategory entirely
(Categorize always overwrote them). Carrying them through is required
for a manual destination pick to survive a Recompute round trip.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `appapi.App.Categories()` exposes `config.yaml`'s categories

**Files:**
- Modify: `internal/appapi/app.go`
- Test: `internal/appapi/app_test.go`

**Interfaces:**
- Produces: `appapi.DestinationView { Category string; Subcategory string }` (json `category`/`subcategory`); `func (a *App) Categories() ([]DestinationView, error)`, sorted by Category then Subcategory, excluding `"Uncategorized"`.

- [ ] **Step 1: Write the failing test**

Add to `internal/appapi/app_test.go` (uses `writeTestConfigWithRules`, already defined in `internal/appapi/recompute_test.go` in the same package):

```go
func TestCategories_ReturnsSortedDestinationsExcludingUncategorized(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfigWithRules(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"),
		nil,
		map[string]config.Category{
			"Technology":    {Subcategories: []string{"Python", "Java"}},
			"Food":          {Subcategories: []string{"Mexican"}},
			"Fiction":       {},
			"Uncategorized": {},
		},
	)

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	got, err := app.Categories()
	if err != nil {
		t.Fatalf("Categories returned error: %v", err)
	}
	want := []DestinationView{
		{Category: "Fiction", Subcategory: ""},
		{Category: "Food", Subcategory: "Mexican"},
		{Category: "Technology", Subcategory: "Java"},
		{Category: "Technology", Subcategory: "Python"},
	}
	if len(got) != len(want) {
		t.Fatalf("Categories() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Categories()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/appapi/... -run TestCategories_ReturnsSortedDestinationsExcludingUncategorized -v`
Expected: FAIL to compile — `undefined: DestinationView` / `app.Categories undefined`.

- [ ] **Step 3: Implement `DestinationView` and `Categories()`**

In `internal/appapi/app.go`, add `"sort"` and the `categorizer` package to the imports:

```go
import (
	"os"
	"path/filepath"
	"sort"

	"github.com/FrancisChung/book-organiser/internal/categorizer"
	"github.com/FrancisChung/book-organiser/internal/config"
)
```

Then append at the end of the file:

```go
// DestinationView is one selectable category/subcategory leaf a book can be
// manually routed to, as declared under config.yaml's categories section.
// Subcategory is "" for a category with no subcategories declared.
type DestinationView struct {
	Category    string `json:"category"`
	Subcategory string `json:"subcategory"`
}

// Categories returns every category/subcategory leaf declared in
// config.yaml, sorted by Category then Subcategory, excluding
// "Uncategorized" itself (never a valid manual destination). This backs
// the Scan & Review UI's destination-picker dropdown for Uncategorized
// books. Go map iteration order is randomized, so the sort is required for
// a stable, non-jumping dropdown across calls.
func (a *App) Categories() ([]DestinationView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	var dests []DestinationView
	for name, cat := range cfg.Categories {
		if name == categorizer.UncategorizedName {
			continue
		}
		if len(cat.Subcategories) == 0 {
			dests = append(dests, DestinationView{Category: name})
			continue
		}
		for _, sub := range cat.Subcategories {
			dests = append(dests, DestinationView{Category: name, Subcategory: sub})
		}
	}
	sort.Slice(dests, func(i, j int) bool {
		if dests[i].Category != dests[j].Category {
			return dests[i].Category < dests[j].Category
		}
		return dests[i].Subcategory < dests[j].Subcategory
	})
	return dests, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/appapi/... -v`
Expected: PASS, all tests in the package.

- [ ] **Step 5: Commit**

```bash
git add internal/appapi/app.go internal/appapi/app_test.go
git commit -m "$(cat <<'EOF'
Add appapi.App.Categories(), exposing config.yaml's categories

Flattens cfg.Categories into a sorted list of selectable
category/subcategory destinations, excluding Uncategorized, for the
frontend's new destination-picker dropdown.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Integration test — manual category pick survives `Recompute`

**Files:**
- Test: `internal/appapi/recompute_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1-4. This task adds no new production code — it's a composition check confirming the pieces built in Tasks 1-4 actually work together through the one code path (`Recompute`) the frontend will call.

- [ ] **Step 1: Write the test**

Add to `internal/appapi/recompute_test.go`:

```go
func TestRecompute_ManualCategoryPickSurvivesRecompute(t *testing.T) {
	working := t.TempDir()
	library := filepath.Join(t.TempDir(), "library")
	logDir := filepath.Join(t.TempDir(), "logs")
	configPath := writeTestConfigWithRules(t, working, library, logDir,
		nil, // no rules that would match -- would fall back to Uncategorized without the manual override
		map[string]config.Category{
			"Fiction":       {Subcategories: []string{"Sci-Fi"}},
			"Uncategorized": {},
		},
	)

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	edited := BookView{
		SourcePath:     filepath.Join(working, "some.epub"),
		Title:          Field{Value: "Foundation", Source: "Metadata"},
		Author:         Field{Value: "Someone Unmatched", Source: "Metadata"},
		Year:           Field{Value: "1951", Source: "Metadata"},
		Category:       "Fiction",
		Subcategory:    "Sci-Fi",
		CategoryManual: true,
	}

	got, err := app.Recompute(edited)
	if err != nil {
		t.Fatalf("Recompute returned error: %v", err)
	}
	if got.Category != "Fiction" || got.Subcategory != "Sci-Fi" {
		t.Errorf("Category/Subcategory = %q/%q, want Fiction/Sci-Fi (manual pick preserved)", got.Category, got.Subcategory)
	}
	if got.Status != "Edited" {
		t.Errorf("Status = %q, want Edited", got.Status)
	}
	wantDestFragment := filepath.Join("Fiction", "Sci-Fi", "Foundation (1951) - Someone Unmatched.epub")
	if !strings.HasSuffix(got.DestPath, wantDestFragment) {
		t.Errorf("DestPath = %q, want suffix %q", got.DestPath, wantDestFragment)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./internal/appapi/... -run TestRecompute_ManualCategoryPickSurvivesRecompute -v`
Expected: PASS immediately. Unlike Tasks 1-4, this isn't a red/green cycle over new production code — it's a regression check confirming `viewToBook` → `Categorize` → `rename.BuildPath` → `bookToView` compose correctly end-to-end. If it fails, it means Tasks 1-4 didn't wire together correctly; stop and re-check those tasks' diffs before continuing.

- [ ] **Step 3: Commit**

```bash
git add internal/appapi/recompute_test.go
git commit -m "$(cat <<'EOF'
Add integration test for manual category pick surviving Recompute

Confirms Tasks 1-4 compose correctly through the one path the
frontend calls on every card edit.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `desktop/app.go` passthrough + regenerate Wails bindings

**Files:**
- Modify: `desktop/app.go:50-52`
- Generated (do not hand-edit beyond running the generator): `desktop/frontend/wailsjs/go/main/App.d.ts`, `desktop/frontend/wailsjs/go/main/App.js`, `desktop/frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: `appapi.App.Categories()` / `appapi.DestinationView` (Task 4).
- Produces: `func (a *App) Categories() ([]appapi.DestinationView, error)` on the Wails-bound `main.App`, and its generated TypeScript binding `Categories(): Promise<appapi.DestinationView[]>` plus the generated `appapi.DestinationView` class in `models.ts`.

No new Go test: every other pure-passthrough method on this struct (`ConfigStatus`, `ListOperationBatches`, `Scan`, `Recompute`, `Apply`) has none either — `desktop/app_test.go` only covers methods with actual logic (`isAffirmative`, `openCommand`, `OpenFile`).

- [ ] **Step 1: Add the passthrough method**

In `desktop/app.go`, add next to the existing `ListOperationBatches`/`ListCategoryWarnings` passthroughs (currently lines 46-52):

```go
func (a *App) ListOperationBatches() ([]appapi.OperationBatchView, error) {
	return a.api.ListOperationBatches()
}

func (a *App) ListCategoryWarnings() ([]appapi.CategoryWarningView, error) {
	return a.api.ListCategoryWarnings()
}

func (a *App) Categories() ([]appapi.DestinationView, error) {
	return a.api.Categories()
}
```

- [ ] **Step 2: Verify the Go side compiles**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 3: Regenerate the Wails bindings**

Run: `cd desktop && wails generate module`
Expected: exits 0; `git diff --stat desktop/frontend/wailsjs` shows changes to `App.d.ts`, `App.js`, and `models.ts`.

- [ ] **Step 4: Verify the generated output**

Run: `grep -n "Categories" desktop/frontend/wailsjs/go/main/App.d.ts desktop/frontend/wailsjs/go/main/App.js desktop/frontend/wailsjs/go/models.ts`
Expected: a `Categories(): Promise<appapi.DestinationView[]>` export in `App.d.ts`, a matching `Categories()` function in `App.js` calling `window['go']['main']['App']['Categories']()`, and an `appapi.DestinationView` class in `models.ts` with `category`/`subcategory` string fields.

If `wails generate module` isn't available in this environment, hand-edit the three generated files to match the existing entries for e.g. `ConfigStatus`/`ConfigStatusView`, substituting `Categories`/`DestinationView` and its two string fields — but prefer the generator; hand-editing generated files risks drifting from what a real `wails build` would produce.

- [ ] **Step 5: Commit**

```bash
git add desktop/app.go desktop/frontend/wailsjs
git commit -m "$(cat <<'EOF'
Expose Categories() to the frontend via a Wails-bound passthrough

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Frontend types gain `categoryManual` and `DestinationView`

**Files:**
- Modify: `desktop/frontend/src/lib/types.ts:6-22`

**Interfaces:**
- Produces: `BookView.categoryManual: boolean`; `DestinationView { category: string; subcategory: string }`.

- [ ] **Step 1: Add the field and interface**

In `desktop/frontend/src/lib/types.ts`, add `categoryManual` to `BookView` (currently lines 6-22), and add a new `DestinationView` interface right after it:

```ts
export interface BookView {
  id: string;
  sourcePath: string;
  oldFilename: string;
  format: string;
  sizeBytes: number;
  subject: string;
  title: FieldView;
  author: FieldView;
  year: FieldView;
  status: string;
  category: string;
  subcategory: string;
  categoryWarning: string;
  categoryManual: boolean;
  destPath: string;
  duplicateStatus: string;
  duplicateGroupId: string;
}

export interface DestinationView {
  category: string;
  subcategory: string;
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd desktop/frontend && npm run check`
Expected: no new type errors. (Existing object literals typed as `BookView` elsewhere — e.g. in test files — will now be missing `categoryManual` until Tasks 8-9 update them; this step is just confirming `types.ts` itself is syntactically valid. Any resulting "missing property" errors in `.test.ts` files are expected here and get fixed in Tasks 8-9.)

- [ ] **Step 3: Commit**

```bash
git add desktop/frontend/src/lib/types.ts
git commit -m "$(cat <<'EOF'
Add categoryManual to BookView and a DestinationView type

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: `BookCard.svelte` renders the destination dropdown

**Files:**
- Modify: `desktop/frontend/src/lib/BookCard.svelte`
- Test: `desktop/frontend/src/lib/BookCard.test.ts`

**Interfaces:**
- Consumes: `DestinationView` (Task 7).
- Produces: `BookCard` gains an `export let destinations: DestinationView[] = [];` prop; dispatches the existing `edited` event (with `category`/`subcategory`/`categoryManual` set) when the user picks a destination.

- [ ] **Step 1: Update the test helper and write the failing tests**

In `desktop/frontend/src/lib/BookCard.test.ts`, add `categoryManual: false` to the `makeBook()` helper's returned object (it currently ends with `duplicateGroupId: '', ...overrides,`):

```ts
function makeBook(overrides: Partial<BookView> = {}): BookView {
  return {
    id: '/inbox/book.epub',
    sourcePath: '/inbox/book.epub',
    oldFilename: 'book.epub',
    format: 'epub',
    sizeBytes: 1024,
    subject: '',
    title: { value: 'Atomic Kotlin', source: 'Heuristic' },
    author: { value: 'Bruce Eckel, Svetlana Isakova', source: 'Heuristic' },
    year: { value: '2021', source: 'Heuristic' },
    status: 'Heuristic',
    category: 'Uncategorized',
    subcategory: '',
    categoryWarning: '',
    categoryManual: false,
    destPath: '/library/Uncategorized/Atomic Kotlin (2021) - Bruce Eckel, Svetlana Isakova.epub',
    duplicateStatus: 'NotDuplicate',
    duplicateGroupId: '',
    ...overrides,
  };
}

const destinations = [
  { category: 'Fiction', subcategory: 'Sci-Fi' },
  { category: 'Food', subcategory: '' },
];
```

Add these tests to the `describe('BookCard', ...)` block:

```ts
  it('shows a destination dropdown for an Uncategorized book', () => {
    render(BookCard, { book: makeBook(), destinations });
    const select = screen.getByRole('combobox', { name: 'Choose a destination' });
    expect(select).toBeInTheDocument();
    expect(screen.getByText('Fiction / Sci-Fi')).toBeInTheDocument();
    expect(screen.getByText('Food')).toBeInTheDocument();
  });

  it('does not show a destination dropdown for a categorized, non-manual book', () => {
    render(BookCard, { book: makeBook({ category: 'Technology' }), destinations });
    expect(screen.queryByRole('combobox', { name: 'Choose a destination' })).not.toBeInTheDocument();
  });

  it('selecting a destination dispatches edited immediately with category/subcategory and categoryManual set', async () => {
    const { component } = render(BookCard, { book: makeBook(), destinations });
    const handler = vi.fn();
    component.$on('edited', handler);

    const select = screen.getByRole('combobox', { name: 'Choose a destination' });
    await fireEvent.change(select, { target: { value: '0' } });

    expect(handler).toHaveBeenCalledTimes(1);
    const detail = handler.mock.calls[0][0].detail;
    expect(detail.category).toBe('Fiction');
    expect(detail.subcategory).toBe('Sci-Fi');
    expect(detail.categoryManual).toBe(true);
  });

  it('keeps the destination dropdown visible and shows the picked value once categoryManual is true', () => {
    render(BookCard, {
      book: makeBook({ category: 'Fiction', subcategory: 'Sci-Fi', categoryManual: true }),
      destinations,
    });
    const select = screen.getByRole('combobox', { name: 'Choose a destination' }) as HTMLSelectElement;
    expect(select.value).toBe('0');
  });
```

- [ ] **Step 2: Run tests to verify the new ones fail**

Run: `cd desktop/frontend && npx vitest run src/lib/BookCard.test.ts`
Expected: the 4 new tests FAIL (`Unable to find role="combobox" with name "Choose a destination"` / similar) — the dropdown doesn't exist yet. All pre-existing `BookCard.test.ts` tests should still PASS (the `categoryManual: false` addition to `makeBook()` is additive).

- [ ] **Step 3: Implement the dropdown**

In `desktop/frontend/src/lib/BookCard.svelte`, update the type import (currently line 3):

```ts
  import type { BookView, DestinationView } from './types';
```

Add the new prop and derived state, and the change handler, right after the existing `swapTitleAuthor` function (currently ending at line 47):

```ts
  export let destinations: DestinationView[] = [];

  $: selectedDestinationIndex = destinations.findIndex(
    (d) => d.category === book.category && d.subcategory === book.subcategory,
  );

  function onDestinationChange(e: Event) {
    const idx = Number((e.target as HTMLSelectElement).value);
    const dest = destinations[idx];
    if (!dest) return;
    book = { ...book, category: dest.category, subcategory: dest.subcategory, categoryManual: true };
    dispatch('edited', book);
  }
```

Replace the `.badges` markup (currently lines 122-130):

```svelte
  <div class="badges">
    <span class="pill status-{book.status}">{STATUS_LABEL[book.status] ?? book.status}</span>
    {#if book.duplicateStatus !== 'NotDuplicate'}
      <span class="pill dup">{DUP_LABEL[book.duplicateStatus] ?? book.duplicateStatus}</span>
    {/if}
    {#if book.category === 'Uncategorized'}
      <span class="pill uncategorized">Uncategorized</span>
    {/if}
  </div>
```

with:

```svelte
  <div class="badges">
    <div class="badges-left">
      <span class="pill status-{book.status}">{STATUS_LABEL[book.status] ?? book.status}</span>
      {#if book.duplicateStatus !== 'NotDuplicate'}
        <span class="pill dup">{DUP_LABEL[book.duplicateStatus] ?? book.duplicateStatus}</span>
      {/if}
      {#if book.category === 'Uncategorized'}
        <span class="pill uncategorized">Uncategorized</span>
      {/if}
    </div>
    {#if book.category === 'Uncategorized' || book.categoryManual}
      <select
        class="destination-picker"
        aria-label="Choose a destination"
        value={String(selectedDestinationIndex)}
        on:change={onDestinationChange}
      >
        <option value="-1" disabled>Choose a destination…</option>
        {#each destinations as dest, i}
          <option value={String(i)}>{dest.subcategory ? `${dest.category} / ${dest.subcategory}` : dest.category}</option>
        {/each}
      </select>
    {/if}
  </div>
```

Update the `.badges` CSS rule (currently lines 207-210) and add `.badges-left`/`.destination-picker`:

```css
  .badges {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
  }
  .badges-left {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .destination-picker {
    padding: 4px 8px;
    border: 1px solid var(--bf-border);
    border-radius: 6px;
    font-size: 11px;
    font-family: inherit;
    color: var(--bf-text);
    background: var(--bf-surface);
    max-width: 220px;
  }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/BookCard.test.ts`
Expected: PASS, all tests in the file — the 4 new ones and every pre-existing one (the `.badges-left` wrapper doesn't change any text/role/class the existing tests query for).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/BookCard.svelte desktop/frontend/src/lib/BookCard.test.ts
git commit -m "$(cat <<'EOF'
Add destination dropdown to BookCard for Uncategorized books

Right-justified in the existing badges row. Picking an option marks
categoryManual and dispatches the same edited event title/author
edits already use, so Recompute picks up the new destination with no
new parent-side wiring.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: `ScanReviewView.svelte` fetches and passes down destinations

**Files:**
- Modify: `desktop/frontend/src/lib/ScanReviewView.svelte`
- Test: `desktop/frontend/src/lib/ScanReviewView.test.ts`

**Interfaces:**
- Consumes: `BookCard`'s `destinations` prop (Task 8); the generated `Categories()` Wails binding (Task 6).
- Produces: every `BookCard` rendered by this view receives the fetched `destinations` list.

- [ ] **Step 1: Update the mock and write the failing test**

In `desktop/frontend/src/lib/ScanReviewView.test.ts`, update the `vi.mock` factory and its import (currently lines 5-13):

```ts
vi.mock('../../wailsjs/go/main/App', () => ({
  Scan: vi.fn(),
  Recompute: vi.fn(),
  Apply: vi.fn(),
  ConfirmApply: vi.fn(),
  Categories: vi.fn(),
}));

import ScanReviewView from './ScanReviewView.svelte';
import { Scan, Apply, ConfirmApply, Categories } from '../../wailsjs/go/main/App';
```

Add a default mock in `beforeEach` (currently lines 38-42) so every existing test keeps working unchanged:

```ts
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(ConfirmApply).mockResolvedValue(true);
    vi.mocked(Apply).mockResolvedValue({ batchId: 'b1', results: [] });
    vi.mocked(Categories).mockResolvedValue([]);
  });
```

Add `categoryManual: false` to this file's `makeBook()` helper (currently lines 15-35), mirroring Task 8's change to the same helper in `BookCard.test.ts`:

```ts
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
    categoryManual: false,
    destPath: '/library/Uncategorized/Atomic Kotlin (2021) - Bruce Eckel.epub',
    duplicateStatus: 'NotDuplicate',
    duplicateGroupId: '',
    ...overrides,
  };
}
```

Add a new test to the `describe('ScanReviewView', ...)` block:

```ts
  it('fetches destinations on mount and shows them in the picker for an Uncategorized book', async () => {
    vi.mocked(Categories).mockResolvedValue([{ category: 'Fiction', subcategory: 'Sci-Fi' }]);
    vi.mocked(Scan).mockResolvedValue([makeBook({ category: 'Uncategorized' })]);
    render(ScanReviewView);

    await fireEvent.click(screen.getByRole('button', { name: 'Scan' }));

    await waitFor(() => {
      expect(screen.getByText('Fiction / Sci-Fi')).toBeInTheDocument();
    });
  });
```

- [ ] **Step 2: Run tests to verify the new one fails**

Run: `cd desktop/frontend && npx vitest run src/lib/ScanReviewView.test.ts`
Expected: the new test FAILS (`Unable to find an element with text: Fiction / Sci-Fi` — times out in `waitFor`), since `Categories()` is never called and `destinations` is never passed to `BookCard`. Every pre-existing test in the file should still PASS.

- [ ] **Step 3: Implement the fetch and prop passthrough**

In `desktop/frontend/src/lib/ScanReviewView.svelte`, update the imports (currently lines 1-5):

```ts
  import { onMount } from 'svelte';
  import FilterBar from './FilterBar.svelte';
  import BookCard from './BookCard.svelte';
  import { matchesFilter, matchesQuery, type BookView, type DestinationView, type StatusFilter } from './types';
  import { Scan, Recompute, Apply, ConfirmApply, Categories } from '../../wailsjs/go/main/App';
```

Add state and an `onMount` fetch, right after the existing `let checked` declaration (currently line 16):

```ts
  let checked: Record<string, boolean> = {};
  let destinations: DestinationView[] = [];

  onMount(async () => {
    try {
      destinations = await Categories();
    } catch (e) {
      // Non-fatal: the destination dropdown just has no options if this
      // fails. Scan/Apply, this view's primary purpose, are unaffected.
      console.error('Categories failed', e);
    }
  });
```

Pass the prop down to `BookCard` (currently lines 121-126):

```svelte
        <BookCard
          {book}
          {destinations}
          checked={checked[book.sourcePath]}
          on:edited={onEdited}
          on:toggled={(e) => onToggled(book.sourcePath, e.detail)}
        />
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/ScanReviewView.test.ts`
Expected: PASS, all tests in the file.

- [ ] **Step 5: Run the full frontend and Go test suites**

Run: `cd desktop/frontend && npm run test`
Expected: PASS, every test file.

Run: `go build ./... && go test ./...`
Expected: PASS, every package.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/lib/ScanReviewView.svelte desktop/frontend/src/lib/ScanReviewView.test.ts
git commit -m "$(cat <<'EOF'
Fetch categories on mount and pass them to every BookCard

Wires appapi.App.Categories() (added in an earlier task) up to the
new destination-dropdown UI in BookCard, completing the feature end
to end: pick a destination on an Uncategorized card, status shows
Edited, dest path updates, Apply moves the file there.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

## Manual verification (after Task 9)

The plan's automated tests cover the logic; per this project's UI-change convention, also verify in the running app before considering the feature done:

1. Run the app (`cd desktop && wails dev`, or per this repo's `run` skill/convention).
2. Scan a folder containing at least one book that doesn't match any `config.yaml` rule (lands in Uncategorized).
3. Confirm the Uncategorized card shows a right-justified dropdown in the badges row, listing every category/subcategory from `config.yaml` (not "Uncategorized" itself).
4. Pick a destination. Confirm: the status pill changes to "Edited", the "Uncategorized" pill disappears, the dest-path line below updates to the new folder, and the dropdown still shows the picked value.
5. Edit the book's Title or Author afterward. Confirm the picked category/dest-path do NOT revert to Uncategorized.
6. Check the card and click Apply. Confirm the file actually moves to the picked destination on disk.
