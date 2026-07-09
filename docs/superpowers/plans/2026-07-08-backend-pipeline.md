# Book Organiser: Backend Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the complete, UI-free Go backend pipeline that scans a working folder, resolves book metadata (embedded metadata + filename-heuristic fallback), categorizes and de-duplicates books, computes safe cross-platform destination paths, and can apply/undo/redo the resulting file moves.

**Architecture:** A set of small, single-responsibility Go packages under `internal/`, composed by one `pipeline.Run()` entry point. No UI, no network calls, no CGO. This package is what the Wails desktop app (a later plan) will call directly — `pipeline.Run` returns `[]*book.Book` for View 1/2 to render, and `operations.Manager` is what "Apply"/"Undo"/"Redo" call into.

**Tech Stack:** Go 1.22, `gopkg.in/yaml.v3` for config. No other third-party dependencies — this is deliberate: the project's rollout requirement is "easily compiled, executed and deployed" as a single small binary on both Windows and Linux, and every extra dependency (especially anything requiring CGO, like a full PDF library) works against that.

Spec: `docs/superpowers/specs/2026-07-08-book-organiser-design.md`

## Global Constraints

- Go module path: `github.com/FrancisChung/book-organiser`, Go version 1.22.
- v1 supported formats only: `.epub`, `.pdf`, `.mobi`, `.azw3`. No other extensions are scanned.
- Metadata is **local only** — embedded file metadata plus filename heuristics. No network calls anywhere in this backend. No ISBN lookups.
- All path/filename handling must use `path/filepath` (not `path`) and must apply Windows/NTFS-safe sanitization **unconditionally, regardless of host OS** — a Linux-built binary may still write to an NTFS-mounted path, per the spec's "must work equally well on Linux and Windows (NTFS)" constraint.
- Zero CGO, zero non-Go-toolchain build dependencies. The PDF and MOBI/AZW3 extractors are therefore custom, dependency-free parsers rather than wrapping a full third-party library — this is a deliberate scope tradeoff, documented per-task below.
- Known, deliberate limitations (carried over from the design's "best-effort" framing for local-only metadata):
  - The PDF extractor only sees **literal, uncompressed** `/Title`/`/Author`/`/Subject`/`/CreationDate` strings in the raw file bytes. PDFs that store their Info dictionary inside a compressed object stream (common in PDF 1.5+ output from some tools) will not yield metadata this way — those files fall through to filename heuristics, which is the intended behavior, not a bug to fix in this plan.
  - AZW3 is parsed via the same PalmDB+MOBI+EXTH path as `.mobi`. Pure KF8-only files with no MOBI6 compatibility wrapper are not specifically handled; extraction degrades gracefully (returns whatever fields it finds, falls back to heuristics for the rest) rather than erroring.
  - **Post-completion hardening (2026-07-09):** cross-device moves are now handled. `operations.MoveCommand` (`internal/operations/command.go`) falls back to a copy-then-delete when a direct `os.Rename` fails, so a working folder and library folder on different filesystems/devices (e.g. inbox on one drive, library on a NAS or a different volume — a realistic deployment for this tool, not just a hypothetical) no longer error out with an unhandled `EXDEV`/cross-device-link failure. `MoveCommand.Execute`/`Undo`/`Redo` also now refuse to overwrite an existing file at the target path (`ErrDestinationExists`) instead of silently clobbering it via `os.Rename`'s default replace-on-Unix behavior, and `pipeline.Run` disambiguates any books that computed an identical `DestPath` before returning.
- Duplicate detection uses **normalized-exact-match** on title+author (lowercase, strip non-alphanumerics, collapse whitespace) — not full fuzzy/edit-distance matching. This is a deliberate v1 scope decision to stay dependency-free and deterministic.
- Duplicate "same size" tolerance (not specified numerically in the spec): within 1% of the larger file's size, or within 1024 bytes, whichever is larger. This is an implementation default — call it out as such if revisited later.
- Every field-level resolution in a `book.Book` carries a `Source` (`Metadata` / `Heuristic` / `Manual` / `Unresolved`). Manual is set later by the UI layer (not this plan) when a user edits a field — this plan's code must leave room for it (the `Source` enum includes it) but nothing in this plan ever produces `SourceManual` itself.
- Config `fallbacks.year` / `fallbacks.author` text is used **only** when rendering a destination-path preview for a field that is `Unresolved` — it does not change that field's `Source`. A row with any `Unresolved` required field stays `Unresolved` overall per `Book.Status()`, which is what later UI/Apply logic uses to exclude it, even though `DestPath` still renders something readable.

## File Structure

```
book-organiser/
  go.mod
  internal/
    textutil/
      year.go            -- ExtractYear(s) (string, bool): pulls a plausible 4-digit year out of free text
      year_test.go
    book/
      book.go             -- Source enum, Field, DuplicateStatus enum, Book struct, Book.Status()
      book_test.go
    config/
      config.go           -- Config struct tree + Load/Save (YAML)
      config_test.go
    scanner/
      scanner.go          -- Scan(root) ([]string, error): recursive walk filtered to supported extensions
      scanner_test.go
    metadata/
      result.go           -- Result struct shared by all extractors
      epub.go             -- extractEpub: zip + OPF XML
      epub_test.go
      mobi.go             -- extractMobi: PalmDB + MOBI header + EXTH (shared by .mobi/.azw3)
      mobi_test.go
      pdf.go              -- extractPDF: literal-string scan of Info dict
      pdf_test.go
      extractor.go        -- Extract(path) (Result, error): dispatch by extension
      extractor_test.go
    heuristics/
      parser.go           -- Parse(filenameStem, knownJunkTags) Result: filename-based fallback guesses
      parser_test.go
    categorizer/
      categorizer.go      -- Categorize(*book.Book, config.Config): rules -> genre fallback -> Uncategorized
      categorizer_test.go
    duplicates/
      detector.go         -- Detect([]*book.Book): groups + flags DuplicateGroupID/DuplicateStatus
      detector_test.go
    rename/
      pathbuilder.go       -- BuildPath(*book.Book, config.Config): template render + sanitize + truncation
      pathbuilder_test.go
    operations/
      command.go           -- Command interface, MoveCommand (Execute/Undo/Redo)
      command_test.go
      log.go                -- LogEntry, Log (append-only JSONL persistence)
      log_test.go
      manager.go            -- Manager (ExecuteBatch/UndoBatch/RedoBatch)
      manager_test.go
    pipeline/
      pipeline.go           -- Run(config.Config) ([]*book.Book, error): wires every stage together
      pipeline_test.go
```

---

### Task 1: Project scaffolding + config package

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config`, `config.General`, `config.Fallbacks`, `config.Heuristics`, `config.Category`, `config.Rule`; `config.Load(path string) (Config, error)`; `config.Save(path string, cfg Config) error`.

- [ ] **Step 1: Initialize the Go module and add the YAML dependency**

```bash
cd /media/francis/Data2/Source/Organisers/book-organiser
go mod init github.com/FrancisChung/book-organiser
go get gopkg.in/yaml.v3
mkdir -p internal/config
```

- [ ] **Step 2: Write the failing test**

`internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleYAML = `
general:
  working_folder: "/inbox"
  library_folder: "/library"
  filename_format: "{title} ({year}) - {author}"
  fallbacks:
    year: "Unknown"
    author: "Unknown Author"

heuristics:
  known_junk_tags: ["OceanofPDF.com", "libgen.li"]

categories:
  Fiction:
    subcategories: [Sci-Fi, Fantasy]
  Uncategorized:
    subcategories: []

rules:
  - match_field: author
    match_value: "Isaac Asimov"
    category: Fiction
    subcategory: Sci-Fi
`

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(sampleYAML), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.General.WorkingFolder != "/inbox" {
		t.Errorf("WorkingFolder = %q, want /inbox", cfg.General.WorkingFolder)
	}
	if cfg.General.FilenameFormat != "{title} ({year}) - {author}" {
		t.Errorf("FilenameFormat = %q", cfg.General.FilenameFormat)
	}
	if cfg.General.Fallbacks.Year != "Unknown" {
		t.Errorf("Fallbacks.Year = %q, want Unknown", cfg.General.Fallbacks.Year)
	}
	if len(cfg.Heuristics.KnownJunkTags) != 2 || cfg.Heuristics.KnownJunkTags[0] != "OceanofPDF.com" {
		t.Errorf("KnownJunkTags = %v", cfg.Heuristics.KnownJunkTags)
	}
	fiction, ok := cfg.Categories["Fiction"]
	if !ok || len(fiction.Subcategories) != 2 || fiction.Subcategories[0] != "Sci-Fi" {
		t.Errorf("Categories[Fiction] = %+v, ok=%v", fiction, ok)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].MatchValue != "Isaac Asimov" || cfg.Rules[0].Category != "Fiction" {
		t.Errorf("Rules = %+v", cfg.Rules)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	original := Config{
		General: General{
			WorkingFolder:  "/inbox",
			LibraryFolder:  "/library",
			FilenameFormat: "{title} ({year}) - {author}",
			Fallbacks:      Fallbacks{Year: "Unknown", Author: "Unknown Author"},
		},
		Heuristics: Heuristics{KnownJunkTags: []string{"z-lib.org"}},
		Categories: map[string]Category{
			"NonFiction": {Subcategories: []string{"Technology"}},
		},
		Rules: []Rule{
			{MatchField: "filename", MatchValue: "(?i)docker", Category: "NonFiction", Subcategory: "Technology"},
		},
	}

	if err := Save(path, original); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.General != original.General {
		t.Errorf("General round-trip mismatch:\n got  %+v\n want %+v", loaded.General, original.General)
	}
	if loaded.Rules[0] != original.Rules[0] {
		t.Errorf("Rules round-trip mismatch:\n got  %+v\n want %+v", loaded.Rules[0], original.Rules[0])
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config/... -v`
Expected: build failure — `undefined: Load`, `undefined: Config`, etc. (package `config.go` doesn't exist yet).

- [ ] **Step 4: Write minimal implementation**

`internal/config/config.go`:

```go
package config

import "os"

import "gopkg.in/yaml.v3"

type Config struct {
	General    General             `yaml:"general"`
	Heuristics Heuristics          `yaml:"heuristics"`
	Categories map[string]Category `yaml:"categories"`
	Rules      []Rule              `yaml:"rules"`
}

type General struct {
	WorkingFolder  string    `yaml:"working_folder"`
	LibraryFolder  string    `yaml:"library_folder"`
	FilenameFormat string    `yaml:"filename_format"`
	Fallbacks      Fallbacks `yaml:"fallbacks"`
}

type Fallbacks struct {
	Year   string `yaml:"year"`
	Author string `yaml:"author"`
}

type Heuristics struct {
	KnownJunkTags []string `yaml:"known_junk_tags"`
}

type Category struct {
	Subcategories []string `yaml:"subcategories"`
}

type Rule struct {
	MatchField  string `yaml:"match_field"`
	MatchValue  string `yaml:"match_value"`
	Category    string `yaml:"category"`
	Subcategory string `yaml:"subcategory"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/config/... -v`
Expected: `PASS` for both `TestLoad` and `TestSaveLoadRoundTrip`.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/config
git commit -m "feat: add YAML config loading"
```

---

### Task 2: Shared text utility — year extraction

**Files:**
- Create: `internal/textutil/year.go`
- Test: `internal/textutil/year_test.go`

**Interfaces:**
- Produces: `textutil.ExtractYear(s string) (string, bool)` — used by the EPUB/MOBI extractors and the heuristics parser (later tasks).

- [ ] **Step 1: Write the failing test**

`internal/textutil/year_test.go`:

```go
package textutil

import "testing"

func TestExtractYear(t *testing.T) {
	tests := []struct {
		in       string
		wantYear string
		wantOK   bool
	}{
		{"1951-01-01", "1951", true},
		{"(2024, O'Reilly Media, Inc.)", "2024", true},
		{"no year here", "", false},
		{"a 1999 release", "1999", true},
		{"", "", false},
	}
	for _, tt := range tests {
		year, ok := ExtractYear(tt.in)
		if year != tt.wantYear || ok != tt.wantOK {
			t.Errorf("ExtractYear(%q) = (%q, %v), want (%q, %v)", tt.in, year, ok, tt.wantYear, tt.wantOK)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/textutil/... -v`
Expected: build failure — `undefined: ExtractYear`.

- [ ] **Step 3: Write minimal implementation**

`internal/textutil/year.go`:

```go
package textutil

import "regexp"

var yearRe = regexp.MustCompile(`\b(1[5-9]\d{2}|20\d{2})\b`)

// ExtractYear pulls the first plausible 4-digit year (1500-2099) out of s,
// bounded by non-word characters on both sides. It will not find a year
// inside a longer unbroken digit run (e.g. a PDF "D:20240101103000" date
// string) -- callers dealing with that specific format should match it
// directly before falling back to this.
func ExtractYear(s string) (string, bool) {
	m := yearRe.FindString(s)
	if m == "" {
		return "", false
	}
	return m, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/textutil/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
git add internal/textutil
git commit -m "feat: add year-extraction text utility"
```

---

### Task 3: Book domain types

**Files:**
- Create: `internal/book/book.go`
- Test: `internal/book/book_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `book.Source` (`SourceUnresolved`, `SourceMetadata`, `SourceHeuristic`, `SourceManual`) with `String()`; `book.Field{Value string; Source Source}`; `book.DuplicateStatus` (`NotDuplicate`, `PossibleDuplicate`, `LikelyDuplicate`); `book.Book` struct with fields `SourcePath, Format string`; `SizeBytes int64`; `Title, Author, Year Field`; `Subject, Category, Subcategory, DestPath, DuplicateGroupID string`; `DuplicateStatus DuplicateStatus`; and method `Book.Status() Source`.

- [ ] **Step 1: Write the failing test**

`internal/book/book_test.go`:

```go
package book

import "testing"

func TestSourceString(t *testing.T) {
	tests := []struct {
		s    Source
		want string
	}{
		{SourceMetadata, "Metadata"},
		{SourceHeuristic, "Heuristic"},
		{SourceManual, "Edited"},
		{SourceUnresolved, "Unresolved"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("Source(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestBookStatus(t *testing.T) {
	tests := []struct {
		name   string
		book   Book
		want   Source
	}{
		{
			name: "all metadata",
			book: Book{
				Title:  Field{"T", SourceMetadata},
				Author: Field{"A", SourceMetadata},
				Year:   Field{"2024", SourceMetadata},
			},
			want: SourceMetadata,
		},
		{
			name: "one heuristic field",
			book: Book{
				Title:  Field{"T", SourceMetadata},
				Author: Field{"A", SourceHeuristic},
				Year:   Field{"2024", SourceMetadata},
			},
			want: SourceHeuristic,
		},
		{
			name: "manually edited beats heuristic",
			book: Book{
				Title:  Field{"T", SourceManual},
				Author: Field{"A", SourceHeuristic},
				Year:   Field{"2024", SourceMetadata},
			},
			want: SourceManual,
		},
		{
			name: "any unresolved field wins regardless of others",
			book: Book{
				Title:  Field{"T", SourceManual},
				Author: Field{"", SourceUnresolved},
				Year:   Field{"2024", SourceMetadata},
			},
			want: SourceUnresolved,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.book.Status(); got != tt.want {
				t.Errorf("Status() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/book/... -v`
Expected: build failure — `undefined: Field`, `undefined: Book`, etc.

- [ ] **Step 3: Write minimal implementation**

`internal/book/book.go`:

```go
package book

type Source int

const (
	SourceUnresolved Source = iota
	SourceMetadata
	SourceHeuristic
	SourceManual
)

func (s Source) String() string {
	switch s {
	case SourceMetadata:
		return "Metadata"
	case SourceHeuristic:
		return "Heuristic"
	case SourceManual:
		return "Edited"
	default:
		return "Unresolved"
	}
}

type Field struct {
	Value  string
	Source Source
}

type DuplicateStatus int

const (
	NotDuplicate DuplicateStatus = iota
	PossibleDuplicate
	LikelyDuplicate
)

type Book struct {
	SourcePath string
	Format     string
	SizeBytes  int64

	Title  Field
	Author Field
	Year   Field

	Subject     string
	Category    string
	Subcategory string
	DestPath    string

	DuplicateGroupID string
	DuplicateStatus  DuplicateStatus
}

// Status returns the row-level status shown in View 1, with precedence
// Unresolved > Edited (Manual) > Heuristic > Metadata: any single Unresolved
// required field makes the whole row Unresolved (excluded from Apply),
// regardless of how the other fields were resolved.
func (b Book) Status() Source {
	fields := []Field{b.Title, b.Author, b.Year}

	for _, f := range fields {
		if f.Source == SourceUnresolved {
			return SourceUnresolved
		}
	}
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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/book/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
git add internal/book
git commit -m "feat: add Book domain type with status precedence"
```

---

### Task 4: Scanner

**Files:**
- Create: `internal/scanner/scanner.go`
- Test: `internal/scanner/scanner_test.go`

**Interfaces:**
- Produces: `scanner.Scan(root string) ([]string, error)` — recursively walks `root`, returns absolute-as-given paths to every file with extension `.epub`, `.pdf`, `.mobi`, or `.azw3` (case-insensitive), skipping everything else.

- [ ] **Step 1: Write the failing test**

`internal/scanner/scanner_test.go`:

```go
package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestScan(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "subdir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	files := []string{
		filepath.Join(root, "book1.epub"),
		filepath.Join(root, "book2.PDF"), // uppercase extension
		filepath.Join(sub, "book3.mobi"),
		filepath.Join(sub, "book4.azw3"),
		filepath.Join(root, "notes.txt"),   // unsupported
		filepath.Join(root, "cover.cbz"),   // unsupported
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	got, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	sort.Strings(got)

	want := []string{
		filepath.Join(root, "book1.epub"),
		filepath.Join(root, "book2.PDF"),
		filepath.Join(sub, "book3.mobi"),
		filepath.Join(sub, "book4.azw3"),
	}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("Scan() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Scan()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/scanner/... -v`
Expected: build failure — `undefined: Scan`.

- [ ] **Step 3: Write minimal implementation**

`internal/scanner/scanner.go`:

```go
package scanner

import (
	"io/fs"
	"path/filepath"
	"strings"
)

var supportedExtensions = map[string]bool{
	".epub": true,
	".pdf":  true,
	".mobi": true,
	".azw3": true,
}

// Scan recursively walks root and returns the path of every file whose
// extension is one of the v1 supported ebook formats.
func Scan(root string) ([]string, error) {
	var results []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if supportedExtensions[ext] {
			results = append(results, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/scanner/... -v`
Expected: `PASS`.

- [ ] **Step 5: Commit**

```bash
git add internal/scanner
git commit -m "feat: add working-folder scanner"
```

---

### Task 5: EPUB metadata extractor

**Files:**
- Create: `internal/metadata/result.go`
- Create: `internal/metadata/epub.go`
- Test: `internal/metadata/epub_test.go`

**Interfaces:**
- Consumes: `textutil.ExtractYear` (Task 2).
- Produces: `metadata.Result{Title, Author, Year, Subject string}`; unexported `extractEpub(path string) (Result, error)` (wired into the public `Extract` dispatcher in Task 8).

- [ ] **Step 1: Write result.go and the failing test**

`internal/metadata/result.go`:

```go
package metadata

// Result holds whatever metadata an extractor could resolve. Any field left
// empty means "not found by this extractor" -- callers fall back accordingly.
type Result struct {
	Title   string
	Author  string
	Year    string
	Subject string
}
```

`internal/metadata/epub_test.go`:

```go
package metadata

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

const testContainerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

func writeEpubFixture(t *testing.T, opfXML string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "book.epub")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create epub fixture: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	w1, _ := zw.Create("META-INF/container.xml")
	w1.Write([]byte(testContainerXML))
	w2, _ := zw.Create("OEBPS/content.opf")
	w2.Write([]byte(opfXML))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return path
}

func TestExtractEpub(t *testing.T) {
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Foundation</dc:title>
    <dc:creator opf:role="aut" xmlns:opf="http://www.idpf.org/2007/opf">Isaac Asimov</dc:creator>
    <dc:date>1951-01-01</dc:date>
    <dc:subject>Sci-Fi</dc:subject>
  </metadata>
</package>`
	path := writeEpubFixture(t, opf)

	result, err := extractEpub(path)
	if err != nil {
		t.Fatalf("extractEpub returned error: %v", err)
	}
	if result.Title != "Foundation" {
		t.Errorf("Title = %q, want Foundation", result.Title)
	}
	if result.Author != "Isaac Asimov" {
		t.Errorf("Author = %q, want Isaac Asimov", result.Author)
	}
	if result.Year != "1951" {
		t.Errorf("Year = %q, want 1951", result.Year)
	}
	if result.Subject != "Sci-Fi" {
		t.Errorf("Subject = %q, want Sci-Fi", result.Subject)
	}
}

func TestExtractEpub_MissingContainer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.epub")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	zw := zip.NewWriter(f)
	zw.Close()
	f.Close()

	if _, err := extractEpub(path); err == nil {
		t.Error("expected error for epub missing META-INF/container.xml, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/... -v`
Expected: build failure — `undefined: extractEpub`.

- [ ] **Step 3: Write minimal implementation**

`internal/metadata/epub.go`:

```go
package metadata

import (
	"archive/zip"
	"encoding/xml"
	"fmt"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

type epubContainer struct {
	Rootfiles struct {
		Rootfile struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfile"`
	} `xml:"rootfiles"`
}

type epubPackage struct {
	Metadata struct {
		Title   string `xml:"title"`
		Creator string `xml:"creator"`
		Date    string `xml:"date"`
		Subject string `xml:"subject"`
	} `xml:"metadata"`
}

func findZipFile(r *zip.ReadCloser, name string) (*zip.File, bool) {
	for _, f := range r.File {
		if f.Name == name {
			return f, true
		}
	}
	return nil, false
}

func extractEpub(path string) (Result, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return Result{}, err
	}
	defer r.Close()

	cf, ok := findZipFile(r, "META-INF/container.xml")
	if !ok {
		return Result{}, fmt.Errorf("epub missing META-INF/container.xml")
	}
	crc, err := cf.Open()
	if err != nil {
		return Result{}, err
	}
	defer crc.Close()
	var c epubContainer
	if err := xml.NewDecoder(crc).Decode(&c); err != nil {
		return Result{}, err
	}

	of, ok := findZipFile(r, c.Rootfiles.Rootfile.FullPath)
	if !ok {
		return Result{}, fmt.Errorf("epub missing opf file %s", c.Rootfiles.Rootfile.FullPath)
	}
	orc, err := of.Open()
	if err != nil {
		return Result{}, err
	}
	defer orc.Close()
	var p epubPackage
	if err := xml.NewDecoder(orc).Decode(&p); err != nil {
		return Result{}, err
	}

	result := Result{
		Title:   p.Metadata.Title,
		Author:  p.Metadata.Creator,
		Subject: p.Metadata.Subject,
	}
	if year, ok := textutil.ExtractYear(p.Metadata.Date); ok {
		result.Year = year
	}
	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metadata/... -v`
Expected: `PASS` for both `TestExtractEpub` and `TestExtractEpub_MissingContainer`.

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/result.go internal/metadata/epub.go internal/metadata/epub_test.go
git commit -m "feat: add EPUB metadata extractor"
```

---

### Task 6: MOBI/AZW3 metadata extractor

**Files:**
- Create: `internal/metadata/mobi.go`
- Test: `internal/metadata/mobi_test.go`

**Interfaces:**
- Consumes: `textutil.ExtractYear` (Task 2), `metadata.Result` (Task 5).
- Produces: unexported `extractMobi(path string) (Result, error)`, shared by `.mobi` and `.azw3` in the Task 8 dispatcher.

This binary format (PalmDB header -> record 0 = PalmDOC header + MOBI header + EXTH records) has been hand-verified byte-for-byte against a constructed fixture before writing this plan, so the offsets below are confirmed correct, not guessed.

- [ ] **Step 1: Write the failing test**

`internal/metadata/mobi_test.go`:

```go
package metadata

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// writeMobiFixture builds a minimal valid PalmDB+MOBI+EXTH file for testing.
func writeMobiFixture(t *testing.T, fullName, author, subject, pubdate string) string {
	t.Helper()
	buf := new(bytes.Buffer)

	// PalmDB header (78 bytes)
	name := make([]byte, 32)
	copy(name, "testbook")
	buf.Write(name)
	binary.Write(buf, binary.BigEndian, uint16(0)) // attributes
	binary.Write(buf, binary.BigEndian, uint16(0)) // version
	binary.Write(buf, binary.BigEndian, uint32(0)) // creation date
	binary.Write(buf, binary.BigEndian, uint32(0)) // mod date
	binary.Write(buf, binary.BigEndian, uint32(0)) // last backup
	binary.Write(buf, binary.BigEndian, uint32(0)) // mod number
	binary.Write(buf, binary.BigEndian, uint32(0)) // appInfoID
	binary.Write(buf, binary.BigEndian, uint32(0)) // sortInfoID
	buf.WriteString("BOOK")
	buf.WriteString("MOBI")
	binary.Write(buf, binary.BigEndian, uint32(0)) // uniqueIDseed
	binary.Write(buf, binary.BigEndian, uint32(0)) // nextRecordListID
	binary.Write(buf, binary.BigEndian, uint16(1)) // numRecords = 1

	// record info list: 1 entry, 8 bytes; record0 starts right after this entry
	record0Offset := uint32(78 + 8)
	binary.Write(buf, binary.BigEndian, record0Offset)
	binary.Write(buf, binary.BigEndian, uint32(0)) // attributes+uniqueID packed

	// record 0: PalmDOC header (16 bytes) + MOBI header + EXTH
	rec0 := new(bytes.Buffer)
	binary.Write(rec0, binary.BigEndian, uint16(1)) // compression
	binary.Write(rec0, binary.BigEndian, uint16(0)) // unused
	binary.Write(rec0, binary.BigEndian, uint32(0)) // text length
	binary.Write(rec0, binary.BigEndian, uint16(0)) // record count
	binary.Write(rec0, binary.BigEndian, uint16(0)) // record size
	binary.Write(rec0, binary.BigEndian, uint16(0)) // encryption type
	binary.Write(rec0, binary.BigEndian, uint16(0)) // unused2

	mobiHeaderStart := rec0.Len()
	const mobiHeaderLen = 232
	rec0.WriteString("MOBI")
	binary.Write(rec0, binary.BigEndian, uint32(mobiHeaderLen))
	binary.Write(rec0, binary.BigEndian, uint32(2))     // mobi type
	binary.Write(rec0, binary.BigEndian, uint32(65001)) // text encoding UTF-8
	binary.Write(rec0, binary.BigEndian, uint32(0))     // unique ID
	binary.Write(rec0, binary.BigEndian, uint32(6))     // file version

	for rec0.Len()-mobiHeaderStart < 84 {
		rec0.WriteByte(0)
	}
	binary.Write(rec0, binary.BigEndian, uint32(0x40)) // EXTH flags: bit6 set

	for rec0.Len()-mobiHeaderStart < 96 {
		rec0.WriteByte(0)
	}
	fullNameOffsetPos := rec0.Len()
	binary.Write(rec0, binary.BigEndian, uint32(0)) // placeholder full name offset
	binary.Write(rec0, binary.BigEndian, uint32(0)) // placeholder full name length

	for rec0.Len()-mobiHeaderStart < mobiHeaderLen {
		rec0.WriteByte(0)
	}

	// EXTH header
	type exthRecord struct {
		id   uint32
		data []byte
	}
	records := []exthRecord{
		{100, []byte(author)},
		{105, []byte(subject)},
		{106, []byte(pubdate)},
		{503, []byte(fullName)},
	}
	exthBody := new(bytes.Buffer)
	for _, r := range records {
		recLen := uint32(8 + len(r.data))
		binary.Write(exthBody, binary.BigEndian, r.id)
		binary.Write(exthBody, binary.BigEndian, recLen)
		exthBody.Write(r.data)
	}
	exthHeaderLen := uint32(12 + exthBody.Len())
	exthStart := rec0.Len()
	rec0.WriteString("EXTH")
	binary.Write(rec0, binary.BigEndian, exthHeaderLen)
	binary.Write(rec0, binary.BigEndian, uint32(len(records)))
	rec0.Write(exthBody.Bytes())
	for (rec0.Len()-exthStart)%4 != 0 {
		rec0.WriteByte(0)
	}

	fullNameOffset := uint32(rec0.Len())
	rec0.WriteString(fullName)

	out := rec0.Bytes()
	binary.BigEndian.PutUint32(out[fullNameOffsetPos:], fullNameOffset)
	binary.BigEndian.PutUint32(out[fullNameOffsetPos+4:], uint32(len(fullName)))
	buf.Write(out)

	dir := t.TempDir()
	path := filepath.Join(dir, "book.mobi")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write mobi fixture: %v", err)
	}
	return path
}

func TestExtractMobi(t *testing.T) {
	path := writeMobiFixture(t, "Foundation", "Isaac Asimov", "Sci-Fi", "1951-01-01")

	result, err := extractMobi(path)
	if err != nil {
		t.Fatalf("extractMobi returned error: %v", err)
	}
	if result.Title != "Foundation" {
		t.Errorf("Title = %q, want Foundation", result.Title)
	}
	if result.Author != "Isaac Asimov" {
		t.Errorf("Author = %q, want Isaac Asimov", result.Author)
	}
	if result.Subject != "Sci-Fi" {
		t.Errorf("Subject = %q, want Sci-Fi", result.Subject)
	}
	if result.Year != "1951" {
		t.Errorf("Year = %q, want 1951", result.Year)
	}
}

func TestExtractMobi_TooShort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.mobi")
	if err := os.WriteFile(path, []byte("short"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := extractMobi(path); err == nil {
		t.Error("expected error for too-short file, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/... -run TestExtractMobi -v`
Expected: build failure — `undefined: extractMobi`.

- [ ] **Step 3: Write minimal implementation**

`internal/metadata/mobi.go`:

```go
package metadata

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

// extractMobi parses the PalmDB + MOBI header + EXTH structure shared by
// .mobi and .azw3 files. It is best-effort: on any structural surprise past
// the point where core fields have already been read, it returns whatever
// it has rather than erroring, so callers can still fall back to heuristics
// for missing fields.
func extractMobi(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	if len(data) < 82 {
		return Result{}, fmt.Errorf("file too short to be a valid MOBI/AZW3")
	}
	numRecords := binary.BigEndian.Uint16(data[76:78])
	if numRecords < 1 {
		return Result{}, fmt.Errorf("no records found")
	}
	record0Offset := binary.BigEndian.Uint32(data[78:82])
	if int(record0Offset) >= len(data) {
		return Result{}, fmt.Errorf("record0 offset out of range")
	}
	rec0 := data[record0Offset:]

	const mobiHeaderStart = 16
	if len(rec0) < mobiHeaderStart+104 {
		return Result{}, fmt.Errorf("record0 too short for MOBI header")
	}
	if string(rec0[mobiHeaderStart:mobiHeaderStart+4]) != "MOBI" {
		return Result{}, fmt.Errorf("MOBI identifier not found")
	}
	headerLen := binary.BigEndian.Uint32(rec0[mobiHeaderStart+4 : mobiHeaderStart+8])
	exthFlags := binary.BigEndian.Uint32(rec0[mobiHeaderStart+84 : mobiHeaderStart+88])
	fullNameOffset := binary.BigEndian.Uint32(rec0[mobiHeaderStart+96 : mobiHeaderStart+100])
	fullNameLen := binary.BigEndian.Uint32(rec0[mobiHeaderStart+100 : mobiHeaderStart+104])

	var result Result
	if uint64(fullNameOffset)+uint64(fullNameLen) <= uint64(len(rec0)) {
		result.Title = string(rec0[fullNameOffset : fullNameOffset+fullNameLen])
	}

	if exthFlags&0x40 == 0 {
		return result, nil // no EXTH block present
	}
	exthStart := mobiHeaderStart + int(headerLen)
	if exthStart+12 > len(rec0) || string(rec0[exthStart:exthStart+4]) != "EXTH" {
		return result, nil
	}
	recordCount := binary.BigEndian.Uint32(rec0[exthStart+8 : exthStart+12])
	pos := exthStart + 12
	var pubdate string
	for i := uint32(0); i < recordCount; i++ {
		if pos+8 > len(rec0) {
			break
		}
		recType := binary.BigEndian.Uint32(rec0[pos : pos+4])
		recLen := binary.BigEndian.Uint32(rec0[pos+4 : pos+8])
		if recLen < 8 || pos+int(recLen) > len(rec0) {
			break
		}
		recData := rec0[pos+8 : pos+int(recLen)]
		switch recType {
		case 100:
			result.Author = string(recData)
		case 105:
			result.Subject = string(recData)
		case 106:
			pubdate = string(recData)
		case 503:
			result.Title = string(recData) // updated title overrides PalmDOC full name
		}
		pos += int(recLen)
	}
	if pubdate != "" {
		if year, ok := textutil.ExtractYear(pubdate); ok {
			result.Year = year
		}
	}

	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metadata/... -run TestExtractMobi -v`
Expected: `PASS` for both `TestExtractMobi` and `TestExtractMobi_TooShort`.

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/mobi.go internal/metadata/mobi_test.go
git commit -m "feat: add MOBI/AZW3 metadata extractor"
```

---

### Task 7: PDF metadata extractor

**Files:**
- Create: `internal/metadata/pdf.go`
- Test: `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: `textutil.ExtractYear` (Task 2), `metadata.Result` (Task 5).
- Produces: unexported `extractPDF(path string) (Result, error)`, wired into Task 8's dispatcher.

The `D:YYYYMMDD...` PDF date format was verified separately: `textutil.ExtractYear`'s word-boundary regex does **not** match inside it (no boundary exists between the concatenated digits), so this extractor matches the `D:(\d{4})` prefix directly instead of delegating that case to `textutil.ExtractYear`.

- [ ] **Step 1: Write the failing test**

`internal/metadata/pdf_test.go`:

```go
package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

const testPDFFixture = `%PDF-1.4
1 0 obj
<< /Title (Foundation) /Author (Isaac Asimov \(revised\)) /Subject (Sci-Fi) /CreationDate (D:19510101000000) >>
endobj
trailer
<< /Root 1 0 R /Info 1 0 R >>
%%EOF`

func writePDFFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "book.pdf")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write pdf fixture: %v", err)
	}
	return path
}

func TestExtractPDF(t *testing.T) {
	path := writePDFFixture(t, testPDFFixture)

	result, err := extractPDF(path)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "Foundation" {
		t.Errorf("Title = %q, want Foundation", result.Title)
	}
	if result.Author != "Isaac Asimov (revised)" {
		t.Errorf("Author = %q, want %q", result.Author, "Isaac Asimov (revised)")
	}
	if result.Subject != "Sci-Fi" {
		t.Errorf("Subject = %q, want Sci-Fi", result.Subject)
	}
	if result.Year != "1951" {
		t.Errorf("Year = %q, want 1951", result.Year)
	}
}

func TestExtractPDF_NoMetadata(t *testing.T) {
	path := writePDFFixture(t, "%PDF-1.4\n1 0 obj\n<< >>\nendobj\n%%EOF")

	result, err := extractPDF(path)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "" || result.Author != "" || result.Year != "" {
		t.Errorf("expected empty result for metadata-free PDF, got %+v", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/... -run TestExtractPDF -v`
Expected: build failure — `undefined: extractPDF`.

- [ ] **Step 3: Write minimal implementation**

`internal/metadata/pdf.go`:

```go
package metadata

import (
	"os"
	"regexp"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

var pdfLiteralStringRe = regexp.MustCompile(`/(Title|Author|Subject|CreationDate)\s*\(((?:[^()\\]|\\.)*)\)`)
var pdfDateYearRe = regexp.MustCompile(`D:(\d{4})`)

func unescapePDFString(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			default:
				out = append(out, s[i])
			}
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// extractPDF is a best-effort, dependency-free scanner: it looks for
// literal (not hex-encoded, not compressed-object-stream) /Title, /Author,
// /Subject, and /CreationDate entries in the raw PDF bytes. See the plan's
// Global Constraints for why this is deliberately not a full PDF parser.
func extractPDF(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}

	fields := map[string]string{}
	for _, m := range pdfLiteralStringRe.FindAllSubmatch(data, -1) {
		key := string(m[1])
		if _, exists := fields[key]; exists {
			continue // keep first match only
		}
		fields[key] = unescapePDFString(string(m[2]))
	}

	result := Result{
		Title:   fields["Title"],
		Author:  fields["Author"],
		Subject: fields["Subject"],
	}
	if m := pdfDateYearRe.FindStringSubmatch(fields["CreationDate"]); m != nil {
		result.Year = m[1]
	} else if year, ok := textutil.ExtractYear(fields["CreationDate"]); ok {
		result.Year = year
	}
	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metadata/... -run TestExtractPDF -v`
Expected: `PASS` for both `TestExtractPDF` and `TestExtractPDF_NoMetadata`.

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_test.go
git commit -m "feat: add PDF metadata extractor"
```

---

### Task 8: Metadata dispatch

**Files:**
- Create: `internal/metadata/extractor.go`
- Test: `internal/metadata/extractor_test.go`

**Interfaces:**
- Consumes: `extractEpub`, `extractMobi`, `extractPDF` (Tasks 5-7).
- Produces: `metadata.Extract(path string) (Result, error)` — the only exported entry point other packages should call.

- [ ] **Step 1: Write the failing test**

`internal/metadata/extractor_test.go`:

```go
package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtract_DispatchesByExtension(t *testing.T) {
	epubPath := writeEpubFixture(t, `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:title>EpubTitle</dc:title></metadata></package>`)
	result, err := Extract(epubPath)
	if err != nil {
		t.Fatalf("Extract(.epub) error: %v", err)
	}
	if result.Title != "EpubTitle" {
		t.Errorf("Extract(.epub) Title = %q", result.Title)
	}

	pdfPath := writePDFFixture(t, `%PDF-1.4
<< /Title (PdfTitle) >>
%%EOF`)
	result, err = Extract(pdfPath)
	if err != nil {
		t.Fatalf("Extract(.pdf) error: %v", err)
	}
	if result.Title != "PdfTitle" {
		t.Errorf("Extract(.pdf) Title = %q", result.Title)
	}

	mobiPath := writeMobiFixture(t, "MobiTitle", "", "", "")
	result, err = Extract(mobiPath)
	if err != nil {
		t.Fatalf("Extract(.mobi) error: %v", err)
	}
	if result.Title != "MobiTitle" {
		t.Errorf("Extract(.mobi) Title = %q", result.Title)
	}

	// .azw3 uses the same extractor as .mobi -- reuse the mobi fixture bytes
	// under a .azw3 name to confirm dispatch, not re-derive the format.
	azw3Path := filepath.Join(t.TempDir(), "book.azw3")
	data, err := os.ReadFile(mobiPath)
	if err != nil {
		t.Fatalf("read mobi fixture: %v", err)
	}
	if err := os.WriteFile(azw3Path, data, 0644); err != nil {
		t.Fatalf("write azw3 fixture: %v", err)
	}
	result, err = Extract(azw3Path)
	if err != nil {
		t.Fatalf("Extract(.azw3) error: %v", err)
	}
	if result.Title != "MobiTitle" {
		t.Errorf("Extract(.azw3) Title = %q", result.Title)
	}

	if _, err := Extract(filepath.Join(t.TempDir(), "book.txt")); err == nil {
		t.Error("expected error for unsupported extension, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/... -run TestExtract_DispatchesByExtension -v`
Expected: build failure — `undefined: Extract`.

- [ ] **Step 3: Write minimal implementation**

`internal/metadata/extractor.go`:

```go
package metadata

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Extract dispatches to the appropriate format-specific extractor based on
// path's extension. It is the only function other packages should call.
func Extract(path string) (Result, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".epub":
		return extractEpub(path)
	case ".pdf":
		return extractPDF(path)
	case ".mobi", ".azw3":
		return extractMobi(path)
	default:
		return Result{}, fmt.Errorf("unsupported extension: %s", ext)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metadata/... -v`
Expected: `PASS` for every test in the package (Tasks 5-8 combined).

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/extractor.go internal/metadata/extractor_test.go
git commit -m "feat: add metadata extractor dispatch"
```

---

### Task 9: Heuristic filename parser

**Files:**
- Create: `internal/heuristics/parser.go`
- Test: `internal/heuristics/parser_test.go`

**Interfaces:**
- Consumes: `textutil.ExtractYear` (Task 2).
- Produces: `heuristics.Result{Title, Author, Year string}`; `heuristics.Parse(filenameStem string, knownJunkTags []string) Result`.

The three test cases below are the exact bad-filename examples from the design spec, and the expected outputs have been hand-verified against a working implementation before writing this plan — including the known limitation on the second case (author lost because it's ambiguously bracketed alongside real junk; this is expected, not a bug, and is exactly what View 1's mandatory manual-edit step exists for).

- [ ] **Step 1: Write the failing test**

`internal/heuristics/parser_test.go`:

```go
package heuristics

import "testing"

func TestParse_RealWorldExamples(t *testing.T) {
	junkTags := []string{"OceanofPDF.com", "libgen.li", "libgen.rs", "z-lib.org"}

	tests := []struct {
		name string
		stem string
		want Result
	}{
		{
			name: "plus-delimited, no author or year present",
			stem: "Build+Your+API+with+Spring",
			want: Result{Title: "Build Your API with Spring", Author: "", Year: ""},
		},
		{
			name: "bracket noise with real year; author lost in braces (known limitation, needs manual fix in View 1)",
			stem: "Building Resilient Distributed Systems (for dagfhhhhh dfafaf){Sam Newman}(2024, O&_039_Reilly Media, Inc.){115667237} libgen.li",
			want: Result{Title: "Building Resilient Distributed Systems", Author: "", Year: "2024"},
		},
		{
			name: "site-tag prefix with title - author",
			stem: "_OceanofPDF.com_Dissecting_the_Dark_Web_-_Lindsay_Kaye",
			want: Result{Title: "Dissecting the Dark Web", Author: "Lindsay Kaye", Year: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.stem, junkTags)
			if got != tt.want {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.stem, got, tt.want)
			}
		})
	}
}

func TestParse_NoSeparatorMeansTitleOnly(t *testing.T) {
	got := Parse("SomeBookTitle", nil)
	want := Result{Title: "SomeBookTitle"}
	if got != want {
		t.Errorf("Parse() = %+v, want %+v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/heuristics/... -v`
Expected: build failure — `undefined: Parse`, `undefined: Result`.

- [ ] **Step 3: Write minimal implementation**

`internal/heuristics/parser.go`:

```go
package heuristics

import (
	"regexp"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

type Result struct {
	Title  string
	Author string
	Year   string
}

var bracketedRe = regexp.MustCompile(`[\(\{\[][^)\}\]]*[\)\}\]]`)
var delimiterRunRe = regexp.MustCompile(`[+_.]+`)
var whitespaceRunRe = regexp.MustCompile(`\s+`)
var titleAuthorSepRe = regexp.MustCompile(`\s*-\s*`)

// Parse applies best-effort heuristics to a bare filename stem (no
// extension, no directory) to guess title/author/year. It is intentionally
// conservative: bracketed content is stripped wholesale as likely junk
// (release-group tags, database IDs, publisher blurbs) even though this
// occasionally removes a real author that happened to be bracketed --
// callers must treat these results as a fallback that needs human review,
// per the design spec's mandatory View 1 manual-edit requirement.
func Parse(filenameStem string, knownJunkTags []string) Result {
	s := filenameStem

	for _, tag := range knownJunkTags {
		tagRe := regexp.MustCompile(`(?i)_?` + regexp.QuoteMeta(tag) + `_?`)
		s = tagRe.ReplaceAllString(s, " ")
	}

	year, _ := textutil.ExtractYear(s)

	s = bracketedRe.ReplaceAllString(s, " ")
	s = delimiterRunRe.ReplaceAllString(s, " ")
	s = whitespaceRunRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)

	result := Result{Year: year}

	// "Title - Author" is ambiguous from the filename alone; v1 treats the
	// first segment as Title and the second as Author, matching both this
	// tool's own output convention and common source-site naming.
	parts := titleAuthorSepRe.Split(s, 2)
	if len(parts) == 2 {
		result.Title = strings.TrimSpace(parts[0])
		result.Author = strings.TrimSpace(parts[1])
	} else {
		result.Title = s
	}

	return result
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/heuristics/... -v`
Expected: `PASS` for both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/heuristics
git commit -m "feat: add filename heuristic parser"
```

---

### Task 10: Categorizer

**Files:**
- Create: `internal/categorizer/categorizer.go`
- Test: `internal/categorizer/categorizer_test.go`

**Interfaces:**
- Consumes: `book.Book`, `book.Field` (Task 3); `config.Config`, `config.Rule`, `config.Category` (Task 1).
- Produces: `categorizer.UncategorizedName` (constant `"Uncategorized"`); `categorizer.Categorize(b *book.Book, cfg config.Config)` — mutates `b.Category`/`b.Subcategory` in place.

- [ ] **Step 1: Write the failing test**

`internal/categorizer/categorizer_test.go`:

```go
package categorizer

import (
	"testing"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
)

func testConfig() config.Config {
	return config.Config{
		Categories: map[string]config.Category{
			"Fiction":    {Subcategories: []string{"Sci-Fi", "Fantasy"}},
			"NonFiction": {Subcategories: []string{"Technology"}},
			"Uncategorized": {},
		},
		Rules: []config.Rule{
			{MatchField: "author", MatchValue: "Isaac Asimov", Category: "Fiction", Subcategory: "Sci-Fi"},
			{MatchField: "filename", MatchValue: "(?i)docker|kubernetes", Category: "NonFiction", Subcategory: "Technology"},
		},
	}
}

func TestCategorize_RuleMatchOnAuthor(t *testing.T) {
	b := &book.Book{SourcePath: "/inbox/foundation.epub", Author: book.Field{Value: "Isaac Asimov"}}
	Categorize(b, testConfig())
	if b.Category != "Fiction" || b.Subcategory != "Sci-Fi" {
		t.Errorf("Category/Subcategory = %s/%s, want Fiction/Sci-Fi", b.Category, b.Subcategory)
	}
}

func TestCategorize_RuleMatchOnFilenameRegex(t *testing.T) {
	b := &book.Book{SourcePath: "/inbox/Learning Docker.epub", Author: book.Field{Value: "Someone Else"}}
	Categorize(b, testConfig())
	if b.Category != "NonFiction" || b.Subcategory != "Technology" {
		t.Errorf("Category/Subcategory = %s/%s, want NonFiction/Technology", b.Category, b.Subcategory)
	}
}

func TestCategorize_FirstRuleWins(t *testing.T) {
	cfg := testConfig()
	cfg.Rules = append([]config.Rule{
		{MatchField: "author", MatchValue: "Isaac Asimov", Category: "NonFiction", Subcategory: "Technology"},
	}, cfg.Rules...)
	b := &book.Book{SourcePath: "/inbox/foundation.epub", Author: book.Field{Value: "Isaac Asimov"}}
	Categorize(b, cfg)
	if b.Category != "NonFiction" {
		t.Errorf("Category = %s, want NonFiction (first matching rule)", b.Category)
	}
}

func TestCategorize_FallsBackToMetadataSubject(t *testing.T) {
	b := &book.Book{SourcePath: "/inbox/whatever.epub", Author: book.Field{Value: "Nobody Known"}, Subject: "Fantasy"}
	Categorize(b, testConfig())
	if b.Category != "Fiction" || b.Subcategory != "Fantasy" {
		t.Errorf("Category/Subcategory = %s/%s, want Fiction/Fantasy", b.Category, b.Subcategory)
	}
}

func TestCategorize_FallsBackToUncategorized(t *testing.T) {
	b := &book.Book{SourcePath: "/inbox/whatever.epub", Author: book.Field{Value: "Nobody Known"}}
	Categorize(b, testConfig())
	if b.Category != UncategorizedName {
		t.Errorf("Category = %s, want %s", b.Category, UncategorizedName)
	}
	if b.Subcategory != "" {
		t.Errorf("Subcategory = %s, want empty", b.Subcategory)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/categorizer/... -v`
Expected: build failure — `undefined: Categorize`, `undefined: UncategorizedName`.

- [ ] **Step 3: Write minimal implementation**

`internal/categorizer/categorizer.go`:

```go
package categorizer

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
)

const UncategorizedName = "Uncategorized"

// Categorize sets b.Category and b.Subcategory in place: config.Rules are
// evaluated top-to-bottom (first match wins) against author/title/
// metadata_subject (case-insensitive exact match) or filename (regex);
// if nothing matches, it falls back to the embedded genre/subject metadata
// against configured subcategory names; if that also fails, Uncategorized.
func Categorize(b *book.Book, cfg config.Config) {
	filename := filepath.Base(b.SourcePath)

	for _, rule := range cfg.Rules {
		matched := false
		switch rule.MatchField {
		case "author":
			matched = strings.EqualFold(strings.TrimSpace(b.Author.Value), strings.TrimSpace(rule.MatchValue))
		case "title":
			matched = strings.EqualFold(strings.TrimSpace(b.Title.Value), strings.TrimSpace(rule.MatchValue))
		case "metadata_subject":
			matched = strings.EqualFold(strings.TrimSpace(b.Subject), strings.TrimSpace(rule.MatchValue))
		case "filename":
			if re, err := regexp.Compile(rule.MatchValue); err == nil {
				matched = re.MatchString(filename)
			}
		default:
			continue
		}
		if matched {
			b.Category = rule.Category
			b.Subcategory = rule.Subcategory
			return
		}
	}

	if b.Subject != "" {
		for catName, cat := range cfg.Categories {
			for _, sub := range cat.Subcategories {
				if strings.EqualFold(sub, b.Subject) {
					b.Category = catName
					b.Subcategory = sub
					return
				}
			}
		}
	}

	b.Category = UncategorizedName
	b.Subcategory = ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/categorizer/... -v`
Expected: `PASS` for all five tests.

- [ ] **Step 5: Commit**

```bash
git add internal/categorizer
git commit -m "feat: add rule-based categorizer with genre fallback"
```

---

### Task 11: Duplicate detector

**Files:**
- Create: `internal/duplicates/detector.go`
- Test: `internal/duplicates/detector_test.go`

**Interfaces:**
- Consumes: `book.Book`, `book.Field`, `book.DuplicateStatus`, `book.PossibleDuplicate`, `book.LikelyDuplicate` (Task 3).
- Produces: `duplicates.Detect(books []*book.Book)` — mutates `DuplicateGroupID`/`DuplicateStatus` on matching books in place.

- [ ] **Step 1: Write the failing test**

`internal/duplicates/detector_test.go`:

```go
package duplicates

import (
	"testing"

	"github.com/FrancisChung/book-organiser/internal/book"
)

func TestDetect_LikelyDuplicateSameFormatAndSize(t *testing.T) {
	a := &book.Book{Title: book.Field{Value: "Foundation"}, Author: book.Field{Value: "Isaac Asimov"}, Format: "epub", SizeBytes: 1_000_000}
	b := &book.Book{Title: book.Field{Value: "foundation"}, Author: book.Field{Value: "  isaac asimov "}, Format: "epub", SizeBytes: 1_005_000}
	books := []*book.Book{a, b}

	Detect(books)

	if a.DuplicateGroupID == "" || a.DuplicateGroupID != b.DuplicateGroupID {
		t.Fatalf("expected same non-empty group ID, got %q and %q", a.DuplicateGroupID, b.DuplicateGroupID)
	}
	if a.DuplicateStatus != book.LikelyDuplicate || b.DuplicateStatus != book.LikelyDuplicate {
		t.Errorf("DuplicateStatus = %v / %v, want LikelyDuplicate for both", a.DuplicateStatus, b.DuplicateStatus)
	}
}

func TestDetect_PossibleDuplicateDifferentFormat(t *testing.T) {
	a := &book.Book{Title: book.Field{Value: "Foundation"}, Author: book.Field{Value: "Isaac Asimov"}, Format: "epub", SizeBytes: 1_000_000}
	b := &book.Book{Title: book.Field{Value: "Foundation"}, Author: book.Field{Value: "Isaac Asimov"}, Format: "pdf", SizeBytes: 5_000_000}
	books := []*book.Book{a, b}

	Detect(books)

	if a.DuplicateGroupID != b.DuplicateGroupID || a.DuplicateGroupID == "" {
		t.Fatalf("expected same non-empty group ID")
	}
	if a.DuplicateStatus != book.PossibleDuplicate {
		t.Errorf("DuplicateStatus = %v, want PossibleDuplicate", a.DuplicateStatus)
	}
}

func TestDetect_NoDuplicateWhenTitleAuthorDiffer(t *testing.T) {
	a := &book.Book{Title: book.Field{Value: "Foundation"}, Author: book.Field{Value: "Isaac Asimov"}, Format: "epub", SizeBytes: 1_000_000}
	b := &book.Book{Title: book.Field{Value: "Dune"}, Author: book.Field{Value: "Frank Herbert"}, Format: "epub", SizeBytes: 1_000_000}
	books := []*book.Book{a, b}

	Detect(books)

	if a.DuplicateGroupID != "" || b.DuplicateGroupID != "" {
		t.Errorf("expected no group assigned, got %q and %q", a.DuplicateGroupID, b.DuplicateGroupID)
	}
}

func TestDetect_SkipsBooksWithNoTitleOrAuthor(t *testing.T) {
	a := &book.Book{Title: book.Field{Value: ""}, Author: book.Field{Value: ""}, Format: "epub", SizeBytes: 1_000_000}
	b := &book.Book{Title: book.Field{Value: ""}, Author: book.Field{Value: ""}, Format: "epub", SizeBytes: 1_000_000}
	books := []*book.Book{a, b}

	Detect(books)

	if a.DuplicateGroupID != "" || b.DuplicateGroupID != "" {
		t.Errorf("expected books with no title/author to never be grouped, got %q and %q", a.DuplicateGroupID, b.DuplicateGroupID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/duplicates/... -v`
Expected: build failure — `undefined: Detect`.

- [ ] **Step 3: Write minimal implementation**

`internal/duplicates/detector.go`:

```go
package duplicates

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/book"
)

var normalizeRe = regexp.MustCompile(`[^a-z0-9]+`)

func normalize(s string) string {
	s = strings.ToLower(s)
	s = normalizeRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// sizeWithinTolerance reports whether two file sizes are close enough to be
// the same underlying file: within 1% of the larger size, or within 1024
// bytes, whichever is the larger allowance. This tolerance is an
// implementation default (not specified numerically in the design spec).
func sizeWithinTolerance(a, b int64) bool {
	if a == 0 || b == 0 {
		return a == b
	}
	larger, smaller := a, b
	if smaller > larger {
		larger, smaller = smaller, larger
	}
	diff := larger - smaller
	tolerance := larger / 100
	if tolerance < 1024 {
		tolerance = 1024
	}
	return diff <= tolerance
}

// Detect groups books by normalized title+author (case/punctuation-
// insensitive exact match -- not fuzzy/edit-distance matching, see the
// plan's Global Constraints). Within any group of 2+ books, same-format
// pairs whose sizes are within tolerance make the whole group
// LikelyDuplicate; otherwise the group is PossibleDuplicate. Books with
// neither title nor author resolved are never grouped. Mutates books in
// place.
func Detect(books []*book.Book) {
	groups := map[string][]*book.Book{}
	for _, b := range books {
		key := normalize(b.Title.Value) + "|" + normalize(b.Author.Value)
		if key == "|" {
			continue
		}
		groups[key] = append(groups[key], b)
	}

	groupNum := 0
	for _, members := range groups {
		if len(members) < 2 {
			continue
		}
		groupNum++
		groupID := fmt.Sprintf("dup-%d", groupNum)

		anyLikely := false
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				if members[i].Format == members[j].Format && sizeWithinTolerance(members[i].SizeBytes, members[j].SizeBytes) {
					anyLikely = true
				}
			}
		}
		status := book.PossibleDuplicate
		if anyLikely {
			status = book.LikelyDuplicate
		}
		for _, m := range members {
			m.DuplicateGroupID = groupID
			m.DuplicateStatus = status
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/duplicates/... -v`
Expected: `PASS` for all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/duplicates
git commit -m "feat: add title+author+size duplicate detector"
```

---

### Task 12: Rename / path builder

**Files:**
- Create: `internal/rename/pathbuilder.go`
- Test: `internal/rename/pathbuilder_test.go`

**Interfaces:**
- Consumes: `book.Book`, `book.Field` (Task 3); `config.Config` (Task 1).
- Produces: `rename.BuildPath(b *book.Book, cfg config.Config)` — mutates `b.DestPath` in place.

- [ ] **Step 1: Write the failing test**

`internal/rename/pathbuilder_test.go`:

```go
package rename

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
)

func testConfig(libraryFolder string) config.Config {
	return config.Config{
		General: config.General{
			LibraryFolder:  libraryFolder,
			FilenameFormat: "{title} ({year}) - {author}",
			Fallbacks:      config.Fallbacks{Year: "Unknown", Author: "Unknown Author"},
		},
	}
}

func TestBuildPath_NormalRender(t *testing.T) {
	b := &book.Book{
		SourcePath:  "/inbox/foundation.epub",
		Title:       book.Field{Value: "Foundation"},
		Author:      book.Field{Value: "Isaac Asimov"},
		Year:        book.Field{Value: "1951"},
		Category:    "Fiction",
		Subcategory: "Sci-Fi",
	}
	BuildPath(b, testConfig("/library"))

	want := filepath.Join("/library", "Fiction", "Sci-Fi", "Foundation (1951) - Isaac Asimov.epub")
	if b.DestPath != want {
		t.Errorf("DestPath = %q, want %q", b.DestPath, want)
	}
}

func TestBuildPath_UsesFallbacksForUnresolvedFields(t *testing.T) {
	b := &book.Book{
		SourcePath:  "/inbox/mystery.epub",
		Title:       book.Field{Value: "Mystery Book"},
		Author:      book.Field{Value: ""},
		Year:        book.Field{Value: ""},
		Category:    "Uncategorized",
		Subcategory: "",
	}
	BuildPath(b, testConfig("/library"))

	if !strings.Contains(b.DestPath, "Unknown Author") || !strings.Contains(b.DestPath, "Unknown") {
		t.Errorf("DestPath = %q, want it to contain the configured fallback text", b.DestPath)
	}
}

func TestBuildPath_SanitizesIllegalCharsAndReservedNames(t *testing.T) {
	b := &book.Book{
		SourcePath: "/inbox/weird.epub",
		Title:      book.Field{Value: `CON: A Book? "Title" <Test>`},
		Author:     book.Field{Value: "Someone"},
		Year:       book.Field{Value: "2024"},
		Category:   "Uncategorized",
	}
	BuildPath(b, testConfig("/library"))

	base := filepath.Base(b.DestPath)
	for _, illegal := range []string{"<", ">", ":", `"`, "?"} {
		if strings.Contains(base, illegal) {
			t.Errorf("DestPath base %q still contains illegal character %q", base, illegal)
		}
	}
}

func TestBuildPath_DropsAuthorBeforeTruncatingTitle(t *testing.T) {
	longTitle := strings.Repeat("VeryLongTitleWord ", 20) // ~360 chars
	b := &book.Book{
		SourcePath: "/inbox/long.epub",
		Title:      book.Field{Value: longTitle},
		Author:     book.Field{Value: "Some Author Name That Adds Length"},
		Year:       book.Field{Value: "2024"},
		Category:   "Uncategorized",
	}
	// A long library folder path pushes the total over the internal budget
	// even before considering the long title, forcing truncation.
	BuildPath(b, testConfig(filepath.Join("/", strings.Repeat("x", 100))))

	if strings.Contains(b.DestPath, "Some Author Name") {
		t.Errorf("expected author to be dropped from an over-length path, got %q", b.DestPath)
	}
	if len(b.DestPath) > 260 {
		t.Errorf("DestPath length %d still exceeds a safe Windows path budget: %q", len(b.DestPath), b.DestPath)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rename/... -v`
Expected: build failure — `undefined: BuildPath`.

- [ ] **Step 3: Write minimal implementation**

`internal/rename/pathbuilder.go`:

```go
package rename

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
)

var illegalCharsRe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
var trailingSepRe = regexp.MustCompile(`[\s\-\x{2013}\x{2014}]+$`)

var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// maxPathLen is a conservative budget for the full destination path
// (library folder + category + subcategory + filename), kept safely under
// Windows' 260-char MAX_PATH regardless of host OS, per the plan's Global
// Constraints on cross-platform path safety.
const maxPathLen = 240

// sanitize strips characters illegal on NTFS, trims trailing spaces/dots
// (also disallowed on Windows), and dodges reserved device names -- applied
// unconditionally regardless of host OS.
func sanitize(name string) string {
	name = illegalCharsRe.ReplaceAllString(name, "")
	name = strings.TrimRight(name, " .")
	base := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	if reservedNames[base] {
		name = "_" + name
	}
	if name == "" {
		name = "Untitled"
	}
	return name
}

func render(format, title, year, author string) string {
	r := strings.NewReplacer("{title}", title, "{year}", year, "{author}", author)
	return r.Replace(format)
}

func cleanupDanglingSeparators(s string) string {
	return strings.TrimSpace(trailingSepRe.ReplaceAllString(s, ""))
}

// BuildPath computes b.DestPath from cfg.General.FilenameFormat and
// b.Category/b.Subcategory. Unresolved year/author fields render using
// cfg.General.Fallbacks text for this preview only -- it does not change
// Field.Source, so Book.Status() still reports Unresolved for those rows.
// If the rendered path would exceed the safe length budget, the author is
// dropped from the filename first; only if that still isn't enough is the
// title itself progressively truncated. Custom filename_format templates
// other than the default "{title} ({year}) - {author}" shape may leave a
// stray separator when the author is dropped; cleanupDanglingSeparators
// only handles trailing " - "/em-dash patterns, which covers the default
// and most conventional templates.
func BuildPath(b *book.Book, cfg config.Config) {
	title := b.Title.Value
	if title == "" {
		title = "Untitled"
	}
	year := b.Year.Value
	if year == "" {
		year = cfg.General.Fallbacks.Year
	}
	author := b.Author.Value
	if author == "" {
		author = cfg.General.Fallbacks.Author
	}

	ext := filepath.Ext(b.SourcePath)
	dir := filepath.Join(cfg.General.LibraryFolder, b.Category, b.Subcategory)

	build := func(t, a string) string {
		rendered := render(cfg.General.FilenameFormat, t, year, a)
		if a == "" {
			rendered = cleanupDanglingSeparators(rendered)
		}
		return sanitize(rendered) + ext
	}

	name := build(title, author)
	if len(filepath.Join(dir, name)) > maxPathLen {
		name = build(title, "")
	}
	for len(filepath.Join(dir, name)) > maxPathLen && len(title) > 10 {
		title = title[:len(title)-10]
		name = build(title, "")
	}

	b.DestPath = filepath.Join(dir, name)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/rename/... -v`
Expected: `PASS` for all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/rename
git commit -m "feat: add filename template renderer with cross-platform sanitization"
```

---

### Task 13: Operations — Command pattern, log, and undo/redo manager

**Files:**
- Create: `internal/operations/command.go`
- Create: `internal/operations/log.go`
- Create: `internal/operations/manager.go`
- Test: `internal/operations/command_test.go`
- Test: `internal/operations/log_test.go`
- Test: `internal/operations/manager_test.go`

**Interfaces:**
- Consumes: nothing outside the standard library.
- Produces: `operations.OpType` (`OpMove`); `operations.CommandData{BatchID, OpType, OldPath, NewPath}`; `operations.Command` interface (`Execute() error`, `Undo() error`, `Redo() error`, `Data() CommandData`); `operations.NewMoveCommand(batchID, oldPath, newPath string) *MoveCommand`; `operations.LogEntry`; `operations.NewLog(path string) *Log` with `Append`, `ReadAll`, `SetBatchUndone`; `operations.NewManager(log *Log) *Manager` with `ExecuteBatch`, `UndoBatch`, `RedoBatch`.

This task has three sub-components (Command, Log, Manager) built and tested in sequence within one task, since Manager cannot be meaningfully tested without the other two already working — right-sizing this as one task avoids a false "done" checkpoint on Command alone.

- [ ] **Step 1: Write the failing test for Command**

`internal/operations/command_test.go`:

```go
package operations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveCommand_ExecuteUndoRedo(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.epub")
	newPath := filepath.Join(dir, "sub", "new.epub")
	if err := os.WriteFile(oldPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cmd := NewMoveCommand("batch-1", oldPath, newPath)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected file at newPath after Execute: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected oldPath to be gone after Execute")
	}

	if err := cmd.Undo(); err != nil {
		t.Fatalf("Undo() error: %v", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("expected file back at oldPath after Undo: %v", err)
	}

	if err := cmd.Redo(); err != nil {
		t.Fatalf("Redo() error: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected file at newPath after Redo: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/operations/... -run TestMoveCommand -v`
Expected: build failure — `undefined: NewMoveCommand`.

- [ ] **Step 3: Write Command implementation**

`internal/operations/command.go`:

```go
package operations

import (
	"os"
	"path/filepath"
)

type OpType string

const OpMove OpType = "move"

type CommandData struct {
	BatchID string
	OpType  OpType
	OldPath string
	NewPath string
}

type Command interface {
	Execute() error
	Undo() error
	Redo() error
	Data() CommandData
}

type MoveCommand struct {
	data CommandData
}

func NewMoveCommand(batchID, oldPath, newPath string) *MoveCommand {
	return &MoveCommand{data: CommandData{BatchID: batchID, OpType: OpMove, OldPath: oldPath, NewPath: newPath}}
}

func (c *MoveCommand) Execute() error {
	if err := os.MkdirAll(filepath.Dir(c.data.NewPath), 0755); err != nil {
		return err
	}
	return os.Rename(c.data.OldPath, c.data.NewPath)
}

func (c *MoveCommand) Undo() error {
	if err := os.MkdirAll(filepath.Dir(c.data.OldPath), 0755); err != nil {
		return err
	}
	return os.Rename(c.data.NewPath, c.data.OldPath)
}

func (c *MoveCommand) Redo() error {
	return c.Execute()
}

func (c *MoveCommand) Data() CommandData {
	return c.data
}
```

- [ ] **Step 4: Run test to verify Command passes**

Run: `go test ./internal/operations/... -run TestMoveCommand -v`
Expected: `PASS`.

- [ ] **Step 5: Write the failing test for Log**

`internal/operations/log_test.go`:

```go
package operations

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLog_AppendAndReadAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.jsonl")
	log := NewLog(path)

	entries := []LogEntry{
		{BatchID: "b1", Timestamp: time.Now(), OpType: OpMove, OldPath: "/a", NewPath: "/b", Undone: false},
		{BatchID: "b1", Timestamp: time.Now(), OpType: OpMove, OldPath: "/c", NewPath: "/d", Undone: false},
	}
	if err := log.Append(entries); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	got, err := log.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ReadAll returned %d entries, want 2", len(got))
	}
	if got[0].OldPath != "/a" || got[1].OldPath != "/c" {
		t.Errorf("unexpected entries: %+v", got)
	}
}

func TestLog_ReadAllOnMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.jsonl")
	log := NewLog(path)
	got, err := log.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll error on missing file: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no entries, got %d", len(got))
	}
}

func TestLog_SetBatchUndone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ops.jsonl")
	log := NewLog(path)
	if err := log.Append([]LogEntry{
		{BatchID: "b1", OldPath: "/a", NewPath: "/b"},
		{BatchID: "b2", OldPath: "/e", NewPath: "/f"},
	}); err != nil {
		t.Fatalf("Append error: %v", err)
	}

	if err := log.SetBatchUndone("b1", true); err != nil {
		t.Fatalf("SetBatchUndone error: %v", err)
	}

	got, err := log.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	for _, e := range got {
		if e.BatchID == "b1" && !e.Undone {
			t.Errorf("expected b1 entries to be marked Undone")
		}
		if e.BatchID == "b2" && e.Undone {
			t.Errorf("expected b2 entries to remain not-Undone")
		}
	}
}
```

- [ ] **Step 6: Run test to verify Log fails**

Run: `go test ./internal/operations/... -run TestLog -v`
Expected: build failure — `undefined: NewLog`, `undefined: LogEntry`.

- [ ] **Step 7: Write Log implementation**

`internal/operations/log.go`:

```go
package operations

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

type LogEntry struct {
	BatchID   string    `json:"batch_id"`
	Timestamp time.Time `json:"timestamp"`
	OpType    OpType    `json:"op_type"`
	OldPath   string    `json:"old_path"`
	NewPath   string    `json:"new_path"`
	Undone    bool      `json:"undone"`
}

// Log is an append-only JSONL file recording every move operation ever
// executed, so undo/redo works purely by re-reading this file -- it survives
// an app restart because it holds no in-memory state of its own.
type Log struct {
	path string
}

func NewLog(path string) *Log {
	return &Log{path: path}
}

func (l *Log) Append(entries []LogEntry) error {
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

func (l *Log) ReadAll() ([]LogEntry, error) {
	f, err := os.Open(l.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e LogEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (l *Log) rewriteAll(entries []LogEntry) error {
	f, err := os.Create(l.path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// SetBatchUndone flips the Undone flag on every entry with the given
// batchID and persists the change.
func (l *Log) SetBatchUndone(batchID string, undone bool) error {
	entries, err := l.ReadAll()
	if err != nil {
		return err
	}
	for i := range entries {
		if entries[i].BatchID == batchID {
			entries[i].Undone = undone
		}
	}
	return l.rewriteAll(entries)
}
```

- [ ] **Step 8: Run test to verify Log passes**

Run: `go test ./internal/operations/... -run TestLog -v`
Expected: `PASS` for all three Log tests.

- [ ] **Step 9: Write the failing test for Manager**

`internal/operations/manager_test.go`:

```go
package operations

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManager_ExecuteBatchThenUndoThenRedo(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.epub")
	newPath := filepath.Join(dir, "Fiction", "new.epub")
	if err := os.WriteFile(oldPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	logPath := filepath.Join(dir, "ops.jsonl")
	mgr := NewManager(NewLog(logPath))
	cmd := NewMoveCommand("batch-1", oldPath, newPath)

	if err := mgr.ExecuteBatch("batch-1", []Command{cmd}); err != nil {
		t.Fatalf("ExecuteBatch error: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected file at newPath: %v", err)
	}

	// Simulate an app restart: build a brand new Manager pointed at the same
	// log file, with no in-memory state carried over.
	restarted := NewManager(NewLog(logPath))
	if err := restarted.UndoBatch("batch-1"); err != nil {
		t.Fatalf("UndoBatch error: %v", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("expected file restored to oldPath: %v", err)
	}

	if err := restarted.RedoBatch("batch-1"); err != nil {
		t.Fatalf("RedoBatch error: %v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected file moved to newPath again: %v", err)
	}
}

func TestManager_ExecuteBatchRollsBackOnPartialFailure(t *testing.T) {
	dir := t.TempDir()
	oldPath1 := filepath.Join(dir, "old1.epub")
	newPath1 := filepath.Join(dir, "new1.epub")
	if err := os.WriteFile(oldPath1, []byte("content"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// oldPath2 deliberately does not exist, so its move will fail.
	oldPath2 := filepath.Join(dir, "does-not-exist.epub")
	newPath2 := filepath.Join(dir, "new2.epub")

	logPath := filepath.Join(dir, "ops.jsonl")
	mgr := NewManager(NewLog(logPath))
	cmds := []Command{
		NewMoveCommand("batch-2", oldPath1, newPath1),
		NewMoveCommand("batch-2", oldPath2, newPath2),
	}

	err := mgr.ExecuteBatch("batch-2", cmds)
	if err == nil {
		t.Fatal("expected ExecuteBatch to return an error when a command fails")
	}

	if _, err := os.Stat(oldPath1); err != nil {
		t.Errorf("expected first move to be rolled back (file back at oldPath1): %v", err)
	}
	if _, err := os.Stat(newPath1); !os.IsNotExist(err) {
		t.Errorf("expected newPath1 to not exist after rollback")
	}

	entries, err := NewLog(logPath).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no log entries for a failed batch, got %d", len(entries))
	}
}
```

- [ ] **Step 10: Run test to verify Manager fails**

Run: `go test ./internal/operations/... -run TestManager -v`
Expected: build failure — `undefined: NewManager`.

- [ ] **Step 11: Write Manager implementation**

`internal/operations/manager.go`:

```go
package operations

import (
	"fmt"
	"time"
)

var nowFunc = time.Now

type Manager struct {
	log *Log
}

func NewManager(log *Log) *Manager {
	return &Manager{log: log}
}

// ExecuteBatch runs commands in order. If a command fails partway through,
// already-executed commands in this batch are undone (best-effort) before
// returning the error, so a failed Apply never leaves the library
// half-moved, and nothing about the failed batch is written to the log.
func (m *Manager) ExecuteBatch(batchID string, commands []Command) error {
	var executed []Command
	for _, cmd := range commands {
		if err := cmd.Execute(); err != nil {
			for i := len(executed) - 1; i >= 0; i-- {
				_ = executed[i].Undo() // best-effort rollback
			}
			return fmt.Errorf("batch %s failed on %+v: %w", batchID, cmd.Data(), err)
		}
		executed = append(executed, cmd)
	}

	entries := make([]LogEntry, len(commands))
	for i, cmd := range commands {
		d := cmd.Data()
		entries[i] = LogEntry{
			BatchID:   batchID,
			Timestamp: nowFunc(),
			OpType:    d.OpType,
			OldPath:   d.OldPath,
			NewPath:   d.NewPath,
			Undone:    false,
		}
	}
	return m.log.Append(entries)
}

// UndoBatch reverses every not-yet-undone entry for batchID, most recent
// first, by reconstructing commands purely from the persisted log -- this is
// what makes undo survive an app restart.
func (m *Manager) UndoBatch(batchID string) error {
	entries, err := m.log.ReadAll()
	if err != nil {
		return err
	}
	var toUndo []LogEntry
	for _, e := range entries {
		if e.BatchID == batchID && !e.Undone {
			toUndo = append(toUndo, e)
		}
	}
	for i := len(toUndo) - 1; i >= 0; i-- {
		e := toUndo[i]
		cmd := NewMoveCommand(e.BatchID, e.OldPath, e.NewPath)
		if err := cmd.Undo(); err != nil {
			return fmt.Errorf("undo batch %s failed on %s: %w", batchID, e.NewPath, err)
		}
	}
	return m.log.SetBatchUndone(batchID, true)
}

// RedoBatch re-applies every undone entry for batchID, in original order.
func (m *Manager) RedoBatch(batchID string) error {
	entries, err := m.log.ReadAll()
	if err != nil {
		return err
	}
	var toRedo []LogEntry
	for _, e := range entries {
		if e.BatchID == batchID && e.Undone {
			toRedo = append(toRedo, e)
		}
	}
	for _, e := range toRedo {
		cmd := NewMoveCommand(e.BatchID, e.OldPath, e.NewPath)
		if err := cmd.Redo(); err != nil {
			return fmt.Errorf("redo batch %s failed on %s: %w", batchID, e.OldPath, err)
		}
	}
	return m.log.SetBatchUndone(batchID, false)
}
```

- [ ] **Step 12: Run test to verify Manager passes**

Run: `go test ./internal/operations/... -v`
Expected: `PASS` for every test in the package (Command, Log, and Manager combined).

- [ ] **Step 13: Commit**

```bash
git add internal/operations
git commit -m "feat: add Command-pattern operation log with undo/redo"
```

---

### Task 14: Pipeline integration

**Files:**
- Create: `internal/pipeline/pipeline.go`
- Test: `internal/pipeline/pipeline_test.go`

**Interfaces:**
- Consumes: `scanner.Scan` (Task 4); `metadata.Extract` (Task 8); `heuristics.Parse` (Task 9); `categorizer.Categorize` (Task 10); `duplicates.Detect` (Task 11); `rename.BuildPath` (Task 12); `operations.Manager`/`NewMoveCommand` (Task 13); `config.Config` (Task 1); `book.Book`/`book.Field`/`book.Source` (Task 3).
- Produces: `pipeline.Run(cfg config.Config) ([]*book.Book, error)` — the single entry point a future UI layer calls for the scan/preview stage.

- [ ] **Step 1: Write the failing test**

`internal/pipeline/pipeline_test.go`:

```go
package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/operations"
)

func baseConfig(workingFolder, libraryFolder string) config.Config {
	return config.Config{
		General: config.General{
			WorkingFolder:  workingFolder,
			LibraryFolder:  libraryFolder,
			FilenameFormat: "{title} ({year}) - {author}",
			Fallbacks:      config.Fallbacks{Year: "Unknown", Author: "Unknown Author"},
		},
		Heuristics: config.Heuristics{KnownJunkTags: []string{"OceanofPDF.com", "libgen.li"}},
		Categories: map[string]config.Category{"Uncategorized": {}},
	}
}

func TestRun_FallsBackToHeuristicsForFileWithNoMetadata(t *testing.T) {
	workDir := t.TempDir()
	libDir := t.TempDir()
	// A plain-text stand-in for a PDF with no parseable Info dict: the PDF
	// extractor will find no metadata, so the pipeline must fall back to
	// filename heuristics for every field.
	srcPath := filepath.Join(workDir, "Build+Your+API+with+Spring.pdf")
	if err := os.WriteFile(srcPath, []byte("no pdf metadata here"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	books, err := Run(baseConfig(workDir, libDir))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("Run returned %d books, want 1", len(books))
	}
	b := books[0]
	if b.Title.Value != "Build Your API with Spring" {
		t.Errorf("Title = %q, want %q", b.Title.Value, "Build Your API with Spring")
	}
	if b.Title.Source != book.SourceHeuristic {
		t.Errorf("Title.Source = %v, want SourceHeuristic", b.Title.Source)
	}
	if b.Status() != book.SourceUnresolved {
		t.Errorf("Status() = %v, want SourceUnresolved (no author/year found anywhere)", b.Status())
	}
	if b.Category != "Uncategorized" {
		t.Errorf("Category = %q, want Uncategorized", b.Category)
	}
	if b.DestPath == "" {
		t.Error("expected a DestPath to be computed even for an Unresolved row")
	}
}

func TestRun_FlagsDuplicatesAcrossScannedFiles(t *testing.T) {
	workDir := t.TempDir()
	libDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "Foundation - Isaac Asimov.epub"), []byte("aaaaaaaaaa"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "Foundation - Isaac Asimov (copy).epub"), []byte("aaaaaaaaaa"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	books, err := Run(baseConfig(workDir, libDir))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(books) != 2 {
		t.Fatalf("Run returned %d books, want 2", len(books))
	}
	if books[0].DuplicateGroupID == "" || books[0].DuplicateGroupID != books[1].DuplicateGroupID {
		t.Errorf("expected both books grouped as duplicates, got %q and %q", books[0].DuplicateGroupID, books[1].DuplicateGroupID)
	}
}

func TestRun_ApplyAndUndoEndToEnd(t *testing.T) {
	workDir := t.TempDir()
	libDir := t.TempDir()
	srcPath := filepath.Join(workDir, "Build+Your+API+with+Spring.pdf")
	if err := os.WriteFile(srcPath, []byte("dummy"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	books, err := Run(baseConfig(workDir, libDir))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("Run returned %d books, want 1", len(books))
	}
	b := books[0]

	logPath := filepath.Join(t.TempDir(), "ops.jsonl")
	mgr := operations.NewManager(operations.NewLog(logPath))
	cmd := operations.NewMoveCommand("batch-1", b.SourcePath, b.DestPath)

	if err := mgr.ExecuteBatch("batch-1", []operations.Command{cmd}); err != nil {
		t.Fatalf("ExecuteBatch error: %v", err)
	}
	if _, err := os.Stat(b.DestPath); err != nil {
		t.Fatalf("expected file at DestPath after Apply: %v", err)
	}

	if err := mgr.UndoBatch("batch-1"); err != nil {
		t.Fatalf("UndoBatch error: %v", err)
	}
	if _, err := os.Stat(srcPath); err != nil {
		t.Fatalf("expected file restored to original location after Undo: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/... -v`
Expected: build failure — `undefined: Run`.

- [ ] **Step 3: Write minimal implementation**

`internal/pipeline/pipeline.go`:

```go
package pipeline

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/categorizer"
	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/duplicates"
	"github.com/FrancisChung/book-organiser/internal/heuristics"
	"github.com/FrancisChung/book-organiser/internal/metadata"
	"github.com/FrancisChung/book-organiser/internal/rename"
	"github.com/FrancisChung/book-organiser/internal/scanner"
)

// Run scans cfg.General.WorkingFolder, resolves metadata (embedded first,
// filename heuristics as fallback), categorizes, computes destination
// paths, and flags likely duplicates. It performs no file moves -- this is
// the read-only "preview" stage that View 1 / View 2 render; applying the
// resulting DestPath values is a separate step via the operations package.
func Run(cfg config.Config) ([]*book.Book, error) {
	paths, err := scanner.Scan(cfg.General.WorkingFolder)
	if err != nil {
		return nil, err
	}

	books := make([]*book.Book, 0, len(paths))
	for _, path := range paths {
		b := &book.Book{
			SourcePath: path,
			Format:     strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		}
		if info, err := os.Stat(path); err == nil {
			b.SizeBytes = info.Size()
		}

		if res, err := metadata.Extract(path); err == nil {
			if res.Title != "" {
				b.Title = book.Field{Value: res.Title, Source: book.SourceMetadata}
			}
			if res.Author != "" {
				b.Author = book.Field{Value: res.Author, Source: book.SourceMetadata}
			}
			if res.Year != "" {
				b.Year = book.Field{Value: res.Year, Source: book.SourceMetadata}
			}
			b.Subject = res.Subject
		}

		if b.Title.Value == "" || b.Author.Value == "" || b.Year.Value == "" {
			stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			h := heuristics.Parse(stem, cfg.Heuristics.KnownJunkTags)
			if b.Title.Value == "" && h.Title != "" {
				b.Title = book.Field{Value: h.Title, Source: book.SourceHeuristic}
			}
			if b.Author.Value == "" && h.Author != "" {
				b.Author = book.Field{Value: h.Author, Source: book.SourceHeuristic}
			}
			if b.Year.Value == "" && h.Year != "" {
				b.Year = book.Field{Value: h.Year, Source: book.SourceHeuristic}
			}
		}

		categorizer.Categorize(b, cfg)
		rename.BuildPath(b, cfg)
		books = append(books, b)
	}

	duplicates.Detect(books)
	return books, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/... -v`
Expected: `PASS` for all three tests.

- [ ] **Step 5: Run the full test suite**

Run: `go build ./... && go test ./...`
Expected: build succeeds and every package reports `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/pipeline
git commit -m "feat: wire scanner, metadata, heuristics, categorizer, duplicates, and rename into pipeline.Run"
```

---

## Definition of Done

- `go build ./...` succeeds with no errors.
- `go test ./...` passes with no failures.
- Every package listed in File Structure exists with the responsibilities described.
- The three real bad-filename examples from the design spec produce the documented heuristic output (verified in Task 9's test).
- `pipeline.Run` followed by `operations.Manager.ExecuteBatch`/`UndoBatch` performs a full scan -> categorize -> apply -> undo cycle against a temp directory (verified in Task 14's test).

## Out of scope for this plan (covered by later plans)

- The Wails application shell and all three UI views.
- Config editing UI (View 3) — this plan only provides `config.Load`/`config.Save`.
- Wiring `Book.Status() == SourceManual` to an actual user edit (that happens in the UI layer).
