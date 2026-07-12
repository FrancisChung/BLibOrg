# book-organiser

A personal tool for organising a messy ebook library: it scans a folder of
`.epub` / `.pdf` / `.mobi` / `.azw3` files, works out each book's title,
author, and year (from embedded metadata, or filename heuristics when
metadata is missing), and renames/moves them into a consistent
`Category/Subcategory/Title (Year) - Author.ext` structure — with a manual
review step before anything on disk changes.

## Problem

Ebooks pile up with wildly inconsistent filenames — site-tag junk
(`_OceanofPDF.com_`, `libgen.li`), `+`/`_` used instead of spaces, and
garbage bracketed text mixed in with real metadata, e.g.:

- `Build+Your+API+with+Spring.pdf`
- `Building Resilient Distributed Systems (for dagfhhhhh dfafaf){Sam Newman}(2024, O&_039_Reilly Media, Inc.){115667237} libgen.li.pdf`
- `_OceanofPDF.com_Dissecting_the_Dark_Web_-_Lindsay_Kaye.pdf`

No single regex parses all of these reliably, so the tool combines embedded
metadata, best-effort filename heuristics, and a mandatory review/edit step
before any file is renamed or moved.

## How it works

```
Scanner → Metadata Extractors → Heuristic Filename Parser → Book Record
                                                                 |
                                       Categorizer (rules -> genre -> Uncategorized)
                                                                 |
                                             Duplicate Detector (title+author+size)
                                                                 |
                                   UI: review/edit list (Scan & Review) -> destination preview
                                                                 |
                                        Apply -> Command-based Operation Log -> Undo/Redo
```

1. **Scan** the working folder for supported ebook formats.
2. **Extract metadata** per format (EPUB via OPF, MOBI/AZW3 via EXTH header,
   PDF via the Info dictionary) — local only, no network lookups.
3. **Fall back to filename heuristics** for whatever fields metadata
   couldn't resolve (strip known site tags, normalize delimiters, pull a
   year out of the text). This is best-effort by design, which is why manual
   review is mandatory, not optional.
4. **Categorize** against user-defined rules (first match wins), falling
   back to embedded genre metadata, then to `Uncategorized`.
5. **Flag likely duplicates** (matching title+author, plus file size for
   same-format pairs) — never auto-deleted, always surfaced for manual
   review.
6. **Review and edit** every book in a table before anything is touched.
7. **Preview destination paths**, then **Apply** — each move/rename is
   logged as a `Command` so batches can be undone/redone, even after an app
   restart.

## Status

The design is finalized (see [`design.md`](design.md) and
[`docs/superpowers/specs/2026-07-08-book-organiser-design.md`](docs/superpowers/specs/2026-07-08-book-organiser-design.md)).
The Go backend pipeline (scanning, metadata extraction, heuristics,
categorization, duplicate detection, path building, undo/redo) and a Wails +
Svelte desktop UI are implemented and merged into `main`, per the plans in
[`docs/superpowers/plans/2026-07-08-backend-pipeline.md`](docs/superpowers/plans/2026-07-08-backend-pipeline.md)
and
[`docs/superpowers/plans/2026-07-11-ui-scan-review.md`](docs/superpowers/plans/2026-07-11-ui-scan-review.md).
The UI currently covers a single Scan & Review screen — scan, inline edit,
destination path, and duplicate flags per book, then Apply. The backend
already supports undo/redo and config-driven categorization, but there's no
UI yet for undo/redo history or editing categories/rules outside the YAML
file directly (the design spec's "Organiser & History" view).

## Getting started

### Prerequisites

- [Go](https://go.dev/dl/) 1.25+
- [Node.js](https://nodejs.org/) 20+ (for the Svelte frontend)
- [Wails CLI](https://wails.io/docs/gettingstarted/installation) v2:
  `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- A native webview, needed only to run the desktop app (not for `go build`/`go test`):
  - **Linux**: `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` (or `libwebkit2gtk-4.0-dev` on older distros)
  - **Windows**: [WebView2](https://developer.microsoft.com/microsoft-edge/webview2/) (preinstalled on Windows 10/11)
  - **macOS**: Xcode command line tools

### Build & test the backend

```bash
go build ./...
go test ./...
```

### Run the desktop app in development mode

```bash
cd desktop
wails dev
```

This starts the app with hot reload for the Svelte frontend. Scan reads its
config from a fixed, OS-standard path — `<user config dir>/book-organiser/config.yaml`
(e.g. `~/.config/book-organiser/config.yaml` on Linux, `%AppData%\book-organiser\config.yaml`
on Windows) — see [Configuration](#configuration) for its contents.

### Build a production binary

```bash
cd desktop
wails build
```

Produces a single native binary under `desktop/build/bin/` for the host
platform — no installer or bundled runtime beyond the OS webview.

### Frontend-only commands

```bash
cd desktop/frontend
npm install
npm run build   # production build, output to dist/ (required before `go build ./...` picks up desktop/main.go's embed)
npm test        # vitest
```

## Tech stack

- **Go** for the backend — chosen for cross-platform builds and no runtime
  dependency.
- **Wails** for the desktop shell — a Go backend + HTML/CSS/JS frontend
  rendered in the OS's native webview, producing a single small binary per
  platform (no bundled Chromium, unlike Electron).
- **Svelte + TypeScript** for the frontend.
- Zero CGO, zero non-Go-toolchain build dependencies — the PDF and
  MOBI/AZW3 metadata extractors are small dependency-free parsers rather
  than wrappers around a full third-party library, so the app stays easy to
  cross-compile for Windows and Linux.

## Scope (v1)

- Supported formats: EPUB, PDF, MOBI, AZW3.
- Metadata is local only — embedded file metadata plus filename heuristics.
  No online lookups (Google Books, Open Library, ISBN databases, etc.).
- Duplicates are always surfaced for manual review; the tool never
  auto-deletes.
- Out of scope: CBZ/CBR, DJVU, FB2, plain archive formats.

## Configuration

Working folder, library folder, operation-log folder, filename format,
categories/subcategories, and categorization rules are all defined in a YAML
config file at `<user config dir>/book-organiser/config.yaml`:

```yaml
general:
  working_folder: "D:/Ebooks/Inbox"
  library_folder: "D:/Ebooks/Library"
  log_folder: "D:/Ebooks/.book-organiser-logs"
  filename_format: "{title} ({year}) - {author}"

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

## Documentation

- [`design.md`](design.md) — original v1 design notes.
- [`docs/superpowers/specs/2026-07-08-book-organiser-design.md`](docs/superpowers/specs/2026-07-08-book-organiser-design.md) — finalized design spec.
- [`docs/superpowers/plans/2026-07-08-backend-pipeline.md`](docs/superpowers/plans/2026-07-08-backend-pipeline.md) — backend implementation plan.

## Reference

[na--/ebook-tools](https://github.com/na--/ebook-tools) was used as prior
art, though this project deliberately doesn't replicate its full feature
set.
