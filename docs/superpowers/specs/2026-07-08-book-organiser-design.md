# Design: Book Organiser v1

## Problem

Ebooks accumulate with wildly inconsistent filenames — site-tag prefixes/suffixes
(`_OceanofPDF.com_`, `libgen.li`), delimiter noise (`+`/`_` instead of spaces), and
garbage bracketed text mixed with real metadata. Examples from the actual library:

- `Build+Your+API+with+Spring.pdf`
- `Building Resilient Distributed Systems (for dagfhhhhh dfafaf){Sam Newman}(2024, O&_039_Reilly Media, Inc.){115667237} libgen.li.pdf`
- `_OceanofPDF.com_Dissecting_the_Dark_Web_-_Lindsay_Kaye.pdf`

This makes it hard to find books at a glance. No single regex reliably parses all
of these, so the tool must combine embedded metadata, best-effort filename
heuristics, and mandatory manual review/correction.

## Goal

A tool that renames ebooks into a consistent format and files them into a
configured category/subcategory folder structure, with a review step so bad
parses can be corrected before anything is moved.

## Context / constraints

- Must work equally well on Linux and Windows (NTFS) filesystems.
- Reference (not clone) [na--/ebook-tools](https://github.com/na--/ebook-tools) —
  it has far more functionality than needed here.
- Solo-maintained personal tool, deployed to the user's own Windows/Linux
  workstations. Easy to build and ship as a single binary matters more than
  enterprise-grade robustness.
- v1 formats: **EPUB, PDF, MOBI, AZW3**. (CBZ/CBR, DJVU, FB2, and plain
  archives are explicitly out of scope for v1.)
- Metadata strategy: **local only** — embedded file metadata + filename
  heuristics. No online lookups (Google Books/Open Library/etc.) in v1; this
  keeps the tool fully offline with no API keys, rate limits, or ambiguous
  match resolution to handle.

## Language & tech stack

- **Go**, chosen for comfort/familiarity over other cross-platform options
  considered (Rust+Tauri, C#/.NET+Avalonia, Python).
- **Wails** for the desktop GUI: Go backend + HTML/CSS/JS frontend, rendered
  in a native OS webview. Produces a single small binary (~10-25MB) per
  platform with no bundled Chromium, unlike Electron.

## Architecture

```
Scanner → Metadata Extractors → Heuristic Filename Parser → Book Record
                                                                  |
                                        Categorizer (rules -> genre -> Uncategorized)
                                                                  |
                                              Duplicate Detector (title+author+size)
                                                                  |
                                    UI: review/edit list (View 1) -> preview destinations (View 2)
                                                                  |
                                         Apply -> Command-based Operation Log -> Undo/Redo
```

Flow: scan the working folder → for each file, try format-specific metadata
extraction → wherever metadata is missing/unreliable, fall back to filename
heuristics → build a `Book` record (title, author, year, source path) → run
categorization rules → flag likely duplicates → show everything in View 1 for
manual correction → View 2 previews final destination paths → on Apply,
perform the moves/renames as Commands, logging each for undo/redo.

## Backend components

- **Scanner** — recursively walks the working folder, filters to
  `.epub`, `.pdf`, `.mobi`, `.azw3`.
- **Metadata extractors** — one per format: EPUB (OPF in the container),
  MOBI/AZW3 (EXTH header records), PDF (Info dictionary + XMP if present).
  Each returns whatever fields it finds; fields may be partial or empty.
- **Filename heuristic parser** — fallback used only for fields the metadata
  extractor didn't resolve. Passes: strip known site tags (configurable list,
  e.g. `OceanofPDF.com`, `libgen.li`), normalize `+`/`_`/`.` runs to spaces,
  strip bracketed/parenthesized noise that isn't a 4-digit year, extract a
  year via regex if present. Explicitly best-effort — this is why manual
  editing in View 1 is mandatory, not optional.
- **Categorizer** — evaluates `rules` from config top-to-bottom (first match
  wins); falls back to embedded genre/subject metadata if no rule matches;
  falls back to `Uncategorized` if neither applies.
- **Duplicate detector** — fuzzy-matches title+author (normalized
  casing/punctuation) across the scanned batch, and additionally compares
  file size for same-format pairs: same title+author+format+~same size ⇒
  near-certain duplicate; same title+author but different size/format ⇒
  flagged as "possible duplicate, different edition/scan". Never auto-deletes
  — always surfaced for manual review (View 3).
- **Renamer/path builder** — applies the `filename_format` template, then
  sanitizes for cross-platform safety: strips characters illegal on NTFS
  (`< > : " / \ | ? *`), avoids Windows reserved names (`CON`, `PRN`, `NUL`,
  `COM1`...). If the resulting path exceeds path-length limits, truncation
  order is: drop the author first and retry with `Title (Year)` alone; only
  truncate the title itself as a last resort.
- **Operation log + undo/redo** — each file operation (rename/move) is a
  `Command` object with `Execute()` / `Undo()` / `Redo()`. A batch is a slice
  of Commands executed in order. Command data (old path, new path, op type,
  batch ID, timestamp) is persisted to an append-only log so undo/redo
  survives an app restart, not just an in-memory stack.

## UI views (Wails frontend)

- **View 1 — Scan & Review**: table of every book found in the working
  folder. Columns: current filename, extracted/parsed title/author/year (all
  editable inline), and a **Status** column showing provenance:
  - `Metadata` — all fields resolved from embedded metadata, high confidence
  - `Heuristic` — one or more fields came from filename-parsing fallback,
    worth a glance
  - `Edited` — user manually corrected at least one field (overrides prior
    status)
  - `Unresolved` — a required field couldn't be determined at all; excluded
    from Apply until fixed

  Rows also carry a duplicate-group indicator when flagged by the duplicate
  detector.
- **View 2 — Destination Preview**: computed target path for each
  (corrected) book — `library_folder/Category/Subcategory/Title (Year) -
  Author.ext` — grouped by category. "Apply" runs the batch through the
  Command-based operation log; per-file success/skipped/error shows inline.
- **View 3 — Organiser & History**: config editor for
  categories/subcategories/rules (backed by the YAML config, not requiring
  hand-editing YAML directly), operation history with Undo/Redo controls per
  batch, and the duplicate-review list (groups with an option to
  open/reveal a file so the user can manually decide what to do — v1 never
  auto-deletes).

## Config schema (YAML)

```yaml
general:
  working_folder: "D:/Ebooks/Inbox"
  library_folder: "D:/Ebooks/Library"
  filename_format: "{title} ({year}) - {author}"
  fallbacks:
    year: "Unknown"
    author: "Unknown Author"

heuristics:
  known_junk_tags: ["OceanofPDF.com", "libgen.li", "libgen.rs", "z-lib.org"]

categories:
  Fiction:
    subcategories: [Sci-Fi, Fantasy, Mystery, General]
  NonFiction:
    subcategories: [Technology, History, Biography, General]
  Uncategorized:
    subcategories: []

# Rules are checked in order; first match wins.
# match_field can be: author, title, filename, metadata_subject
rules:
  - match_field: author
    match_value: "Isaac Asimov"
    category: Fiction
    subcategory: Sci-Fi
  - match_field: metadata_subject
    match_value: "Fantasy"
    category: Fiction
    subcategory: Fantasy
  - match_field: filename
    match_value: "(?i)docker|kubernetes|golang"
    category: NonFiction
    subcategory: Technology
```

## Testing / verification

- Unit tests for each metadata extractor against sample EPUB/PDF/MOBI/AZW3
  fixture files, including ones with missing/partial metadata.
- Unit tests for the filename heuristic parser against the real bad-filename
  examples above, plus edge cases (no year found, no delimiter at all).
- Unit tests for the categorizer (rule priority/first-match-wins, fallback to
  genre, fallback to Uncategorized) and the path sanitizer (illegal chars,
  reserved Windows names, truncation-drops-author-first behavior).
- Unit tests for duplicate detection (same title+author+size ⇒ duplicate;
  same title+author different size/format ⇒ possible-duplicate).
- Command/undo tests: execute a batch, undo it, verify filesystem state
  matches original; redo, verify it matches the applied state; verify the log
  persists across a simulated app restart.
- Integration test: end-to-end scan → categorize → apply → undo cycle against
  a temp directory seeded with fixture files covering both formats and messy
  names.

## Rollout

- Deployed to the user's own Windows and Linux workstations only.
- Must be easily compiled, executed, and deployed on either OS — a single
  Wails-built binary per platform, no installed prerequisites beyond the OS's
  built-in webview (WebView2 on Windows 10/11, WebKitGTK on Linux).

## Out of scope for v1

- CBZ/CBR, DJVU, FB2, and plain archive formats.
- Online metadata lookups (Google Books, Open Library, ISBN databases).
- Automatic duplicate deletion — always a manual, reviewed action.
