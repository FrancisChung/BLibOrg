# Design: Title field formatting (separators + Chicago-style Title Case)

## Feature Request

For the Title field:
1. Convert `_` or `-` characters into spaces.
2. Convert casing into Title Case (Chicago style).

## Context / constraints

- Clarified during brainstorming:
  - Applies to titles resolved from embedded book metadata (EPUB/PDF/MOBI)
    and to titles the user manually edits in the UI. Filename-heuristic-
    derived titles are left untouched for now — hyphens there are
    currently load-bearing (the title/author separator, and legitimate
    compound words like "Test-Driven Development" that the heuristic
    parser is specifically tested to preserve). May extend to heuristic
    titles later if it proves useful in practice.
  - Not every hyphen should become a space: some are genuine compound
    words. A `title_formatting.hyphen_exceptions` list in `config.yaml`
    (same pattern as `heuristics.known_junk_tags`) holds hyphenated tokens
    to keep hyphenated, matched case-insensitively, substituted with the
    list's exact stored casing. Seeded by scanning the real library
    (`/media/francis/Data1/Books/Library`) for genuine compound-word
    titles, filtering out filename noise (publisher-name concatenation
    like `-O'Reilly`/`-Pragmatic`/`-McGraw-Hill`) and person names:
    `Anti-Patterns, Cloud-Native, Domain-Driven, Hands-On, High-Impact,
    High-Performance, High-Scale, High-Value, How-To, Model-Driven,
    Step-by-Step, Trade-Off, Well-Grounded, Cutting-Edge, Container-Based,
    Python-Powered`.
  - Underscores have no such exception list — they're never a meaningful
    title character, always converted.
  - A word that already contains an uppercase letter past its first
    position (`iOS`, `DevOps`, `C++`, `API`) is left completely untouched
    by the Title Case step — not run through minor-word lowercasing or
    first-letter capitalization at all. This is a literal, simple rule:
    it also preserves a fully-shouty ALL-CAPS title/word verbatim (no
    dictionary-based way to distinguish "API" from "PYTHON" by casing
    pattern alone); that's an accepted, understood trade-off, not an
    oversight.

## Algorithm

New `textutil.FormatTitle(title string, hyphenExceptions []string) string`:

1. Replace every `_` with a space.
2. Find each hyphen-joined run of word characters (`word-word` or
   `word-word-word`, no surrounding spaces). If it case-insensitively
   matches an entry in `hyphenExceptions`, replace the run with that
   entry's exact stored casing (hyphens kept). Otherwise replace the `-`
   characters within that run with spaces.
3. Split on whitespace. For each word, in order:
   - If the word contains an uppercase letter past its first letter
     position, leave it untouched.
   - Else if it's not the first or last word of the title, and its
     lowercase form is in the small-words set below, lowercase the whole
     word.
   - Else, uppercase its first letter and lowercase the rest (handles
     `GUIDE` → `Guide`, `consultant's` → `Consultant's`).
4. Rejoin with single spaces.

**Small-words set** (Chicago-style articles/conjunctions/short
prepositions, lowercased unless first/last word): `a, an, and, as, at,
but, by, en, for, from, if, in, into, is, nor, of, off, on, onto, or, out,
over, per, so, than, that, the, to, up, via, vs, when, with, yet`.

## Where it's applied

- **`internal/config/config.go`**: `Config` gains `TitleFormatting
  TitleFormatting \`yaml:"title_formatting"\`` where `TitleFormatting
  struct { HyphenExceptions []string \`yaml:"hyphen_exceptions"\` }`.
  `config.yaml` gets the seeded list under `title_formatting:`.
- **`internal/metadata/extractor.go`**: `Extract`'s signature gains a
  `hyphenExceptions []string` parameter (mirroring
  `heuristics.Parse(filenameStem string, knownJunkTags []string)`'s
  existing pattern of taking just the list it needs, not the whole
  config) and calls `textutil.FormatTitle` on `result.Title` alongside
  the existing `CleanField` call.
- **`internal/pipeline/pipeline.go`**: its one call site,
  `metadata.Extract(path)`, becomes `metadata.Extract(path,
  cfg.TitleFormatting.HyphenExceptions)`.
- **`internal/appapi/recompute.go`**: after `b := viewToBook(edited)`, if
  `b.Title.Source == book.SourceManual`, run `b.Title.Value =
  textutil.FormatTitle(b.Title.Value, cfg.TitleFormatting.HyphenExceptions)`
  before `categorizer.Categorize`/`rename.BuildPath` — so a
  `match_field: title` rule and the rendered `DestPath` both see the
  final formatted value, and the frontend gets the formatted title back
  in the same round trip that already updates `status`/`destPath` for
  every edit.

## Testing strategy

- `textutil.FormatTitle` unit tests: underscore→space, hyphen→space,
  hyphen-exception preserved with canonical casing (case-insensitive
  match), small-word lowercased mid-title but capitalized as first/last
  word, mixed-case word preserved untouched, ALL-CAPS word preserved
  untouched (documenting the accepted trade-off).
- `metadata.Extract`: a fixture title with underscores/hyphens/mixed
  casing comes out formatted; `hyphenExceptions` threaded through
  correctly.
- `appapi.Recompute`: a `BookView` with `title.source: "Edited"` and an
  unformatted value comes back formatted, and `destPath` reflects it; a
  `Metadata`/`Heuristic` sourced title is NOT reformatted by Recompute
  (only extraction-time formatting applies there, and Recompute doesn't
  re-run extraction).

## Out of scope

- Filename-heuristic-derived titles (may revisit later).
- Author field formatting (this request is Title-only).
- A fully CMOS-accurate title caser (e.g. context-dependent adverbial
  preposition capitalization like "Look Up") — the small-words list above
  is the standard practical approximation most "Chicago style" title-case
  tools use, not a full implementation of every CMOS 8.159 nuance.
