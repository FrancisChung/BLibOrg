# ISBN Search (narrow scope) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user fill in Title/Author/Year for books still Unresolved/Partial after a normal scan, by detecting an ISBN (embedded structured metadata or filename) and looking it up via the free Google Books API, triggered by an explicit opt-in batch action.

**Architecture:** A pure Go, offline ISBN detector (`textutil.ExtractISBN` + extensions to the existing EPUB/MOBI extractors) feeds a new, isolated networked package (`internal/googlebooks`) that the pipeline layer orchestrates (`pipeline.ResolveViaISBN`) and the Wails-bound `appapi` layer exposes to the frontend. No existing struct (`book.Book`, `appapi.BookView`) changes shape — the ISBN is re-derived on demand at batch-action time rather than persisted.

**Tech Stack:** Go 1.25, standard library only (`net/http`, `encoding/json`, `encoding/xml`, `regexp`) — no new third-party dependencies.

## Global Constraints

- No calibre, 7z, Tesseract/OCR, or Amazon/Goodreads scraping — Google Books API only, official and free.
- No PDF body-text extraction. PDFs get an ISBN only via filename regex; `internal/metadata/pdf.go` stays unchanged (see design doc's Non-goals — it's documented as deliberately not a full PDF parser).
- The whole feature is opt-in via `config.ISBNLookup.Enabled` (default `false`/zero value) — no network call happens unless explicitly enabled.
- Google Books lookups are sequential (one call per book), never concurrent.
- No changes to `book.Book` or `appapi.BookView` — ISBN is re-derived per invocation via `metadata.Extract` + filename fallback, never persisted.
- Filled fields use `book.SourceMetadata` (not a new Source tier) and only fill gaps — never overwrite an already-resolved field.
- No retry/backoff on rate limiting (HTTP 429) or any other request failure for this iteration — treat as a normal failure, count it, move to the next book.

---

## Task 1: `textutil.ExtractISBN`

**Files:**
- Create: `internal/textutil/isbn.go`
- Test: `internal/textutil/isbn_test.go`

**Interfaces:**
- Produces: `func ExtractISBN(s string) (isbn string, ok bool)` — finds the first valid ISBN-10 or ISBN-13 in `s`, returned normalized to 13 digits (no hyphens/spaces, ISBN-10 converted via the standard 978-prefix + recomputed check digit). Used by later tasks against embedded-metadata field values and filenames.

- [ ] **Step 1: Write the failing tests**

```go
// internal/textutil/isbn_test.go
package textutil

import "testing"

func TestExtractISBN(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "isbn13 with hyphens",
			in:   "ISBN 978-0-13-468599-1",
			want: "9780134685991",
			ok:   true,
		},
		{
			name: "isbn13 no separators",
			in:   "9780134685991",
			want: "9780134685991",
			ok:   true,
		},
		{
			name: "isbn10 with hyphens converts to isbn13",
			in:   "0-13-468599-7",
			want: "9780134685991",
			ok:   true,
		},
		{
			name: "isbn10 no separators",
			in:   "0134685997",
			want: "9780134685991",
			ok:   true,
		},
		{
			name: "isbn10 with X check digit",
			in:   "080442957X",
			want: "9780804429573",
			ok:   true,
		},
		{
			name: "embedded in filename",
			in:   "Some Book Title - 9780134685991.epub",
			want: "9780134685991",
			ok:   true,
		},
		{
			name: "invalid check digit rejected",
			in:   "9780134685992",
			want: "",
			ok:   false,
		},
		{
			name: "blacklisted junk rejected",
			in:   "0123456789",
			want: "",
			ok:   false,
		},
		{
			name: "all zeros rejected",
			in:   "0000000000",
			want: "",
			ok:   false,
		},
		{
			name: "no isbn present",
			in:   "just a regular filename.pdf",
			want: "",
			ok:   false,
		},
		{
			name: "empty string",
			in:   "",
			want: "",
			ok:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExtractISBN(tt.in)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("ExtractISBN(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
```

Note: `080442957X` is a real ISBN-10 (Fahrenheit 451, Simon & Schuster edition) used here purely as a check-digit-with-X fixture; its ISBN-13 conversion `9780804429573` was computed with the same algorithm implemented below.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/textutil/... -run TestExtractISBN -v`
Expected: FAIL with `undefined: ExtractISBN`

- [ ] **Step 3: Write the implementation**

```go
// internal/textutil/isbn.go
package textutil

import (
	"regexp"
	"strings"
)

// isbnCandidateRe finds runs of digits, optionally interleaved with
// hyphens/spaces as group separators and with an optional trailing X check
// digit, long enough to plausibly be an ISBN-10 (10 raw characters) or
// ISBN-13 (13 raw characters, up to ~17 with separators). It is
// deliberately permissive -- ExtractISBN validates every match against the
// real ISBN-10/13 check-digit algorithm afterward, so a permissive regex
// paired with a strict validator is safer than a strict regex that might
// miss a real ISBN formatted in an unexpected way.
var isbnCandidateRe = regexp.MustCompile(`\b[0-9][0-9\- ]{8,16}[0-9Xx]\b`)

// isbnBlacklist rejects strings that pass the check-digit algorithm by
// coincidence but are well-known placeholder/junk values, not real ISBNs.
var isbnBlacklist = map[string]bool{
	"0000000000":    true, // all zeros -- checksum trivially satisfied
	"0000000000000": true, // all zeros, isbn13 form
	"0123456789":    true, // sequential digits -- a common false positive
}

// ExtractISBN finds the first valid ISBN-10 or ISBN-13 in s -- an embedded
// metadata field value, or a filename -- and returns it normalized to
// ISBN-13 (hyphens/spaces stripped, ISBN-10 converted per the standard
// 978-prefix + recomputed check digit algorithm), so every caller has a
// single consistent lookup key regardless of which form was found.
func ExtractISBN(s string) (string, bool) {
	for _, candidate := range isbnCandidateRe.FindAllString(s, -1) {
		digits := stripISBNSeparators(candidate)
		if isbnBlacklist[digits] {
			continue
		}
		switch len(digits) {
		case 10:
			if isValidISBN10(digits) {
				return isbn10To13(digits), true
			}
		case 13:
			if isValidISBN13(digits) {
				return digits, true
			}
		}
	}
	return "", false
}

func stripISBNSeparators(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

func isValidISBN10(s string) bool {
	sum := 0
	for i := 0; i < 10; i++ {
		var v int
		switch {
		case s[i] == 'X':
			if i != 9 {
				return false
			}
			v = 10
		case s[i] >= '0' && s[i] <= '9':
			v = int(s[i] - '0')
		default:
			return false
		}
		sum += v * (10 - i)
	}
	return sum%11 == 0
}

func isValidISBN13(s string) bool {
	sum := 0
	for i := 0; i < 13; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
		v := int(s[i] - '0')
		if i%2 == 0 {
			sum += v
		} else {
			sum += v * 3
		}
	}
	return sum%10 == 0
}

// isbn10To13 converts a valid, already-checked ISBN-10 to ISBN-13 by
// dropping its own check digit, prefixing "978", and recomputing the
// ISBN-13 check digit.
func isbn10To13(isbn10 string) string {
	core := "978" + isbn10[:9]
	sum := 0
	for i := 0; i < 12; i++ {
		v := int(core[i] - '0')
		if i%2 == 0 {
			sum += v
		} else {
			sum += v * 3
		}
	}
	check := (10 - sum%10) % 10
	return core + string(rune('0'+check))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/textutil/... -run TestExtractISBN -v`
Expected: PASS (all subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/textutil/isbn.go internal/textutil/isbn_test.go
git commit -m "Add textutil.ExtractISBN: ISBN-10/13 detection and validation"
```

---

## Task 2: EPUB ISBN extraction

**Files:**
- Modify: `internal/metadata/result.go`
- Modify: `internal/metadata/epub.go`
- Test: `internal/metadata/epub_test.go`

**Interfaces:**
- Consumes: `textutil.ExtractISBN(s string) (string, bool)` (Task 1).
- Produces: `metadata.Result.ISBN string` field, populated by `extractEpub` when the OPF's `<dc:identifier>` contains a valid ISBN.

- [ ] **Step 1: Write the failing test**

Add to `internal/metadata/epub_test.go`:

```go
func TestExtractEpub_FindsISBNFromIdentifier(t *testing.T) {
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf">
    <dc:title>Foundation</dc:title>
    <dc:creator opf:role="aut">Isaac Asimov</dc:creator>
    <dc:identifier id="uuid_id" opf:scheme="uuid">urn:uuid:not-an-isbn</dc:identifier>
    <dc:identifier id="isbn_id" opf:scheme="ISBN">urn:isbn:9780134685991</dc:identifier>
  </metadata>
</package>`
	path := writeEpubFixture(t, opf)

	result, err := extractEpub(path)
	if err != nil {
		t.Fatalf("extractEpub returned error: %v", err)
	}
	if result.ISBN != "9780134685991" {
		t.Errorf("ISBN = %q, want 9780134685991", result.ISBN)
	}
}

func TestExtractEpub_NoISBNLeavesFieldEmpty(t *testing.T) {
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Foundation</dc:title>
    <dc:identifier id="uuid_id">urn:uuid:not-an-isbn</dc:identifier>
  </metadata>
</package>`
	path := writeEpubFixture(t, opf)

	result, err := extractEpub(path)
	if err != nil {
		t.Fatalf("extractEpub returned error: %v", err)
	}
	if result.ISBN != "" {
		t.Errorf("ISBN = %q, want empty", result.ISBN)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/... -run TestExtractEpub_ -v`
Expected: FAIL — `result.ISBN` doesn't compile (`Result` has no field `ISBN`) until Step 3.

- [ ] **Step 3: Write the implementation**

In `internal/metadata/result.go`, add the field:

```go
package metadata

// Result holds whatever metadata an extractor could resolve. Any field left
// empty means "not found by this extractor" -- callers fall back accordingly.
type Result struct {
	Title   string
	Author  string
	Year    string
	Subject string
	// ISBN is the first valid ISBN-10/13 found in the file's structured
	// metadata (EPUB dc:identifier, MOBI/AZW3 EXTH record 104), normalized
	// to ISBN-13 by textutil.ExtractISBN. Empty if none was found -- PDF
	// never populates this field (no structured ISBN metadata exists in
	// the format; see internal/metadata/pdf.go's extractPDF doc comment
	// for why body-text scanning is deliberately out of scope).
	ISBN string
}
```

In `internal/metadata/epub.go`, extend `epubPackage.Metadata` and `extractEpub`:

```go
type epubPackage struct {
	Metadata struct {
		Title      string   `xml:"title"`
		Creator    string   `xml:"creator"`
		Date       string   `xml:"date"`
		Subject    string   `xml:"subject"`
		Identifier []string `xml:"identifier"`
	} `xml:"metadata"`
}
```

```go
func extractEpub(path string) (Result, error) {
	// ... unchanged up through parsing p ...

	result := Result{
		Title:   p.Metadata.Title,
		Author:  p.Metadata.Creator,
		Subject: p.Metadata.Subject,
	}
	if year, ok := textutil.ExtractYear(p.Metadata.Date); ok {
		result.Year = year
	}
	for _, id := range p.Metadata.Identifier {
		if isbn, ok := textutil.ExtractISBN(id); ok {
			result.ISBN = isbn
			break
		}
	}
	return result, nil
}
```

(Only the new `Identifier` field and the loop before `return result, nil` are additions — everything else in `extractEpub` is unchanged from the existing file.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -run TestExtractEpub -v`
Expected: PASS (all `TestExtractEpub*` tests, including the pre-existing ones)

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/result.go internal/metadata/epub.go internal/metadata/epub_test.go
git commit -m "Extract ISBN from EPUB dc:identifier metadata"
```

---

## Task 3: MOBI/AZW3 ISBN extraction

**Files:**
- Modify: `internal/metadata/mobi.go`
- Test: `internal/metadata/mobi_test.go`

**Interfaces:**
- Consumes: `textutil.ExtractISBN` (Task 1), `metadata.Result.ISBN` (Task 2).
- Produces: `extractMobi` populates `Result.ISBN` from EXTH record type 104.

- [ ] **Step 1: Write the failing test**

`writeMobiFixture` currently hardcodes which EXTH records it writes (100, 105, 106, 503). Extend it to accept an ISBN and add record 104 when non-empty:

```go
// Replace the existing writeMobiFixture signature and its records slice.
func writeMobiFixture(t *testing.T, fullName, author, subject, pubdate, isbn string) string {
	t.Helper()
	// ... unchanged setup through the "records" slice declaration ...

	records := []exthRecord{
		{100, []byte(author)},
		{105, []byte(subject)},
		{106, []byte(pubdate)},
		{503, []byte(fullName)},
	}
	if isbn != "" {
		records = append(records, exthRecord{104, []byte(isbn)})
	}
	// ... unchanged from here ...
}
```

Update the two existing call sites (`TestExtractMobi`) to pass `""` for the new parameter, and add:

```go
func TestExtractMobi_FindsISBNFromEXTH104(t *testing.T) {
	path := writeMobiFixture(t, "Foundation", "Isaac Asimov", "Sci-Fi", "1951-01-01", "978-0-13-468599-1")

	result, err := extractMobi(path)
	if err != nil {
		t.Fatalf("extractMobi returned error: %v", err)
	}
	if result.ISBN != "9780134685991" {
		t.Errorf("ISBN = %q, want 9780134685991", result.ISBN)
	}
}

func TestExtractMobi_NoISBNLeavesFieldEmpty(t *testing.T) {
	path := writeMobiFixture(t, "Foundation", "Isaac Asimov", "Sci-Fi", "1951-01-01", "")

	result, err := extractMobi(path)
	if err != nil {
		t.Fatalf("extractMobi returned error: %v", err)
	}
	if result.ISBN != "" {
		t.Errorf("ISBN = %q, want empty", result.ISBN)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/... -run TestExtractMobi -v`
Expected: FAIL — call sites of `writeMobiFixture` have the wrong argument count until Step 3's fixture change, and `TestExtractMobi_FindsISBNFromEXTH104` asserts a field that's still always empty.

- [ ] **Step 3: Write the implementation**

In `internal/metadata/mobi.go`, add a case to the EXTH switch:

```go
		switch recType {
		case 100:
			result.Author = string(recData)
		case 104:
			if isbn, ok := textutil.ExtractISBN(string(recData)); ok {
				result.ISBN = isbn
			}
		case 105:
			result.Subject = string(recData)
		case 106:
			pubdate = string(recData)
		case 503:
			result.Title = string(recData) // updated title overrides PalmDOC full name
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -run TestExtractMobi -v`
Expected: PASS (all `TestExtractMobi*` tests)

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/mobi.go internal/metadata/mobi_test.go
git commit -m "Extract ISBN from MOBI/AZW3 EXTH record 104"
```

---

## Task 4: `internal/googlebooks` package

**Files:**
- Create: `internal/googlebooks/googlebooks.go`
- Test: `internal/googlebooks/googlebooks_test.go`

**Interfaces:**
- Consumes: `textutil.ExtractYear(s string) (string, bool)` (existing, `internal/textutil/year.go`).
- Produces: `googlebooks.Result{Title, Author, Year string}`, `googlebooks.ErrNotFound error`, `func Lookup(isbn, apiKey string) (Result, error)`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/googlebooks/googlebooks_test.go
package googlebooks

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func withTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	original := apiBase
	apiBase = server.URL
	t.Cleanup(func() { apiBase = original })
}

func TestLookup_ReturnsFirstMatch(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[{"volumeInfo":{"title":"Foundation","authors":["Isaac Asimov"],"publishedDate":"1951-01-01"}}]}`))
	})

	res, err := Lookup("9780134685991", "")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if res.Title != "Foundation" {
		t.Errorf("Title = %q, want Foundation", res.Title)
	}
	if res.Author != "Isaac Asimov" {
		t.Errorf("Author = %q, want Isaac Asimov", res.Author)
	}
	if res.Year != "1951" {
		t.Errorf("Year = %q, want 1951", res.Year)
	}
}

func TestLookup_JoinsMultipleAuthors(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[{"volumeInfo":{"title":"Good Omens","authors":["Terry Pratchett","Neil Gaiman"]}}]}`))
	})

	res, err := Lookup("9780060853976", "")
	if err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if res.Author != "Terry Pratchett, Neil Gaiman" {
		t.Errorf("Author = %q, want %q", res.Author, "Terry Pratchett, Neil Gaiman")
	}
}

func TestLookup_NoItemsReturnsErrNotFound(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"items":[]}`))
	})

	_, err := Lookup("0000000000000", "")
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestLookup_NonOKStatusReturnsError(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	if _, err := Lookup("9780134685991", ""); err == nil {
		t.Error("expected error for non-200 status, got nil")
	}
}

func TestLookup_MalformedJSONReturnsError(t *testing.T) {
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	})

	if _, err := Lookup("9780134685991", ""); err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

func TestLookup_SendsAPIKeyWhenSet(t *testing.T) {
	var gotKey string
	withTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("key")
		w.Write([]byte(`{"items":[{"volumeInfo":{"title":"Foundation"}}]}`))
	})

	if _, err := Lookup("9780134685991", "test-key-123"); err != nil {
		t.Fatalf("Lookup returned error: %v", err)
	}
	if gotKey != "test-key-123" {
		t.Errorf("key query param = %q, want test-key-123", gotKey)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/googlebooks/... -v`
Expected: FAIL to compile — package `googlebooks` doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```go
// internal/googlebooks/googlebooks.go

// Package googlebooks queries the public Google Books API by ISBN. It is
// the only package in book-organiser that makes network calls -- kept
// separate from internal/metadata so that package stays fully offline and
// unit-testable against fixture files with no HTTP involved.
package googlebooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

// ErrNotFound is returned when the API responds successfully but has no
// matching volume for the given ISBN.
var ErrNotFound = errors.New("googlebooks: no matching volume")

// apiBase is a var, not a const, so tests can point it at an httptest
// server instead of the real API.
var apiBase = "https://www.googleapis.com/books/v1/volumes"

var httpClient = &http.Client{Timeout: 10 * time.Second}

// Result holds whatever fields a Google Books volume record provided. Any
// empty field means the API response didn't include it -- callers fall
// back accordingly, the same convention as metadata.Result.
type Result struct {
	Title  string
	Author string
	Year   string
}

type volumesResponse struct {
	Items []struct {
		VolumeInfo struct {
			Title         string   `json:"title"`
			Authors       []string `json:"authors"`
			PublishedDate string   `json:"publishedDate"`
		} `json:"volumeInfo"`
	} `json:"items"`
}

// Lookup queries the Google Books API for isbn and returns the first
// matching volume's Title/Author(s)/Year. apiKey may be empty -- the API
// works on its public unauthenticated quota without one. Multiple authors
// are joined with ", ", matching this app's Author-field convention (see
// textutil.NormalizeAuthorSeparators). Returns ErrNotFound (distinguishable
// via errors.Is) when the API responds successfully but has no matching
// volume, as opposed to a request/decode failure.
func Lookup(isbn, apiKey string) (Result, error) {
	q := url.Values{}
	q.Set("q", "isbn:"+isbn)
	if apiKey != "" {
		q.Set("key", apiKey)
	}
	reqURL := apiBase + "?" + q.Encode()

	resp, err := httpClient.Get(reqURL)
	if err != nil {
		return Result{}, fmt.Errorf("googlebooks: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("googlebooks: unexpected status %d", resp.StatusCode)
	}

	var parsed volumesResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return Result{}, fmt.Errorf("googlebooks: decode response: %w", err)
	}
	if len(parsed.Items) == 0 {
		return Result{}, ErrNotFound
	}

	vi := parsed.Items[0].VolumeInfo
	year, _ := textutil.ExtractYear(vi.PublishedDate)
	return Result{
		Title:  vi.Title,
		Author: strings.Join(vi.Authors, ", "),
		Year:   year,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/googlebooks/... -v`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add internal/googlebooks/googlebooks.go internal/googlebooks/googlebooks_test.go
git commit -m "Add internal/googlebooks: Google Books API ISBN lookup"
```

---

## Task 5: `config.ISBNLookup`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config.ISBNLookup config.ISBNLookup`, `config.ISBNLookup{Enabled bool; APIKey string}`. Consumed by Task 6/7.

- [ ] **Step 1: Write the failing test**

Add an `isbn_lookup` block to `sampleYAML` in `internal/config/config_test.go` (insert after the `rules:` block, keeping everything else in the constant unchanged):

```go
const sampleYAML = `
general:
  working_folder: "/inbox"
  library_folder: "/library"
  log_folder: "/library/.book-organiser-logs"
  filename_format: "{title} ({year}) - {author}"

heuristics:
  known_junk_tags: ["OceanofPDF.com", "libgen.li"]

title_formatting:
  hyphen_exceptions: ["High-Performance", "Domain-Driven"]

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

isbn_lookup:
  enabled: true
  api_key: "test-key"
`
```

Add assertions to the existing `TestLoad` function (after its existing checks, before its closing brace):

```go
	if !cfg.ISBNLookup.Enabled {
		t.Error("ISBNLookup.Enabled = false, want true")
	}
	if cfg.ISBNLookup.APIKey != "test-key" {
		t.Errorf("ISBNLookup.APIKey = %q, want test-key", cfg.ISBNLookup.APIKey)
	}
```

Add a new test for the default/absent case:

```go
func TestLoad_ISBNLookupDefaultsToDisabled(t *testing.T) {
	yaml := `
general:
  working_folder: "/inbox"
  library_folder: "/library"
categories:
  Uncategorized:
    subcategories: []
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ISBNLookup.Enabled {
		t.Error("ISBNLookup.Enabled = true, want false (default) for a config predating this field")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -v`
Expected: FAIL to compile — `Config` has no field `ISBNLookup`.

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`:

```go
type Config struct {
	General         General             `yaml:"general"`
	Heuristics      Heuristics          `yaml:"heuristics"`
	TitleFormatting TitleFormatting     `yaml:"title_formatting"`
	Categories      map[string]Category `yaml:"categories"`
	Rules           []Rule              `yaml:"rules"`
	ISBNLookup      ISBNLookup          `yaml:"isbn_lookup"`
}
```

```go
// ISBNLookup configures the opt-in Google Books API lookup used to fill
// Title/Author/Year gaps for books still Unresolved/Partial after the
// normal offline scan (see pipeline.ResolveViaISBN). Disabled by default
// (zero value) -- an existing config.yaml that predates this field loads
// with the feature off, not silently on.
type ISBNLookup struct {
	Enabled bool   `yaml:"enabled"`
	APIKey  string `yaml:"api_key"`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "Add config.ISBNLookup: opt-in flag and API key for ISBN lookup"
```

---

## Task 6: `pipeline.ResolveViaISBN`

**Files:**
- Create: `internal/pipeline/isbnresolve.go`
- Test: `internal/pipeline/isbnresolve_test.go`

**Interfaces:**
- Consumes: `metadata.Extract(path string, hyphenExceptions []string) (metadata.Result, error)` (existing), `metadata.Result.ISBN` (Task 2/3), `textutil.ExtractISBN` (Task 1), `googlebooks.Lookup(isbn, apiKey string) (googlebooks.Result, error)` and `googlebooks.ErrNotFound` (Task 4), `config.Config.ISBNLookup` (Task 5), `categorizer.Categorize(b *book.Book, cfg config.Config)`, `rename.BuildPath(b *book.Book, cfg config.Config)`, `duplicates.Detect(books []*book.Book)`, `disambiguateDestPaths(books []*book.Book)` (unexported, same package, defined in `internal/pipeline/pipeline.go`), `book.Field{Value, Source}`, `book.SourceMetadata`, `book.SourceUnresolved`, `book.SourcePartial`, `(book.Book).Status() book.Source`.
- Produces: `pipeline.ISBNResolveSummary{Attempted, Resolved, NotFound, NoISBN int}`, `func ResolveViaISBN(books []*book.Book, cfg config.Config) ISBNResolveSummary`. Consumed by Task 7.

- [ ] **Step 1: Write the failing tests**

```go
// internal/pipeline/isbnresolve_test.go
package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/googlebooks"
)

func writeUnresolvedFixture(t *testing.T, dir, filename string) *book.Book {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte("not a real ebook file"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return &book.Book{SourcePath: path, Format: "epub"}
}

func TestResolveViaISBN_DisabledIsNoOp(t *testing.T) {
	orig := lookupFunc
	defer func() { lookupFunc = orig }()
	lookupFunc = func(isbn, apiKey string) (googlebooks.Result, error) {
		t.Fatal("lookupFunc should not be called when ISBNLookup is disabled")
		return googlebooks.Result{}, nil
	}

	dir := t.TempDir()
	b := writeUnresolvedFixture(t, dir, "Some Book - 9780134685991.epub")
	cfg := baseConfig(dir, t.TempDir())

	summary := ResolveViaISBN([]*book.Book{b}, cfg)

	if summary != (ISBNResolveSummary{}) {
		t.Errorf("summary = %+v, want zero value", summary)
	}
	if b.Title.Value != "" {
		t.Errorf("Title.Value = %q, want unchanged empty", b.Title.Value)
	}
}

func TestResolveViaISBN_FillsGapsAndRecomputes(t *testing.T) {
	orig := lookupFunc
	defer func() { lookupFunc = orig }()
	lookupFunc = func(isbn, apiKey string) (googlebooks.Result, error) {
		if isbn != "9780134685991" {
			t.Fatalf("lookupFunc called with isbn = %q, want 9780134685991", isbn)
		}
		return googlebooks.Result{Title: "Foundation", Author: "Isaac Asimov", Year: "1951"}, nil
	}

	dir := t.TempDir()
	libDir := t.TempDir()
	b := writeUnresolvedFixture(t, dir, "Some Book - 9780134685991.epub")
	cfg := baseConfig(dir, libDir)
	cfg.ISBNLookup = config.ISBNLookup{Enabled: true}

	summary := ResolveViaISBN([]*book.Book{b}, cfg)

	if summary.Attempted != 1 || summary.Resolved != 1 || summary.NotFound != 0 || summary.NoISBN != 0 {
		t.Errorf("summary = %+v, want {Attempted:1 Resolved:1}", summary)
	}
	if b.Title.Value != "Foundation" || b.Title.Source != book.SourceMetadata {
		t.Errorf("Title = %+v, want {Foundation SourceMetadata}", b.Title)
	}
	if b.Author.Value != "Isaac Asimov" || b.Author.Source != book.SourceMetadata {
		t.Errorf("Author = %+v, want {Isaac Asimov SourceMetadata}", b.Author)
	}
	if b.Year.Value != "1951" || b.Year.Source != book.SourceMetadata {
		t.Errorf("Year = %+v, want {1951 SourceMetadata}", b.Year)
	}
	if b.DestPath == "" {
		t.Error("expected DestPath to be recomputed after fields were filled")
	}
}

func TestResolveViaISBN_NoISBNFoundSkipsLookup(t *testing.T) {
	orig := lookupFunc
	defer func() { lookupFunc = orig }()
	lookupFunc = func(isbn, apiKey string) (googlebooks.Result, error) {
		t.Fatal("lookupFunc should not be called when no ISBN was found")
		return googlebooks.Result{}, nil
	}

	dir := t.TempDir()
	b := writeUnresolvedFixture(t, dir, "A Book With No ISBN In The Name.epub")
	cfg := baseConfig(dir, t.TempDir())
	cfg.ISBNLookup = config.ISBNLookup{Enabled: true}

	summary := ResolveViaISBN([]*book.Book{b}, cfg)

	if summary.NoISBN != 1 || summary.Attempted != 0 {
		t.Errorf("summary = %+v, want {NoISBN:1 Attempted:0}", summary)
	}
}

func TestResolveViaISBN_LookupErrorLeavesFieldsUnchanged(t *testing.T) {
	orig := lookupFunc
	defer func() { lookupFunc = orig }()
	lookupFunc = func(isbn, apiKey string) (googlebooks.Result, error) {
		return googlebooks.Result{}, googlebooks.ErrNotFound
	}

	dir := t.TempDir()
	b := writeUnresolvedFixture(t, dir, "Some Book - 9780134685991.epub")
	cfg := baseConfig(dir, t.TempDir())
	cfg.ISBNLookup = config.ISBNLookup{Enabled: true}

	summary := ResolveViaISBN([]*book.Book{b}, cfg)

	if summary.NotFound != 1 || summary.Resolved != 0 {
		t.Errorf("summary = %+v, want {NotFound:1 Resolved:0}", summary)
	}
	if b.Title.Value != "" {
		t.Errorf("Title.Value = %q, want unchanged empty", b.Title.Value)
	}
}

func TestResolveViaISBN_SkipsAlreadyResolvedRows(t *testing.T) {
	orig := lookupFunc
	defer func() { lookupFunc = orig }()
	lookupFunc = func(isbn, apiKey string) (googlebooks.Result, error) {
		t.Fatal("lookupFunc should not be called for an already-resolved row")
		return googlebooks.Result{}, nil
	}

	dir := t.TempDir()
	b := writeUnresolvedFixture(t, dir, "Some Book - 9780134685991.epub")
	b.Title = book.Field{Value: "Already Resolved", Source: book.SourceMetadata}
	b.Author = book.Field{Value: "Someone", Source: book.SourceMetadata}
	b.Year = book.Field{Value: "2020", Source: book.SourceMetadata}
	cfg := baseConfig(dir, t.TempDir())
	cfg.ISBNLookup = config.ISBNLookup{Enabled: true}

	summary := ResolveViaISBN([]*book.Book{b}, cfg)

	if summary != (ISBNResolveSummary{}) {
		t.Errorf("summary = %+v, want zero value", summary)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pipeline/... -run TestResolveViaISBN -v`
Expected: FAIL to compile — `lookupFunc`, `ISBNResolveSummary`, and `ResolveViaISBN` don't exist yet.

- [ ] **Step 3: Write the implementation**

```go
// internal/pipeline/isbnresolve.go
package pipeline

import (
	"path/filepath"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/categorizer"
	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/duplicates"
	"github.com/FrancisChung/book-organiser/internal/googlebooks"
	"github.com/FrancisChung/book-organiser/internal/metadata"
	"github.com/FrancisChung/book-organiser/internal/rename"
	"github.com/FrancisChung/book-organiser/internal/textutil"
)

// lookupFunc is a seam so tests can stub the network call without hitting
// the real Google Books API; production code always uses googlebooks.Lookup.
var lookupFunc = googlebooks.Lookup

// ISBNResolveSummary reports what ResolveViaISBN did across a batch, so
// callers can show the user e.g. "Resolved 4 of 7 (2 had no ISBN, 1 not
// found)".
type ISBNResolveSummary struct {
	Attempted int // rows with a locally-found ISBN that were queried
	Resolved  int // rows where at least one field got filled
	NotFound  int // ISBN found, but the lookup had no match or failed
	NoISBN    int // no ISBN found locally -- skipped, no lookup made
}

// ResolveViaISBN fills Title/Author/Year gaps for every book in books that
// is still Unresolved or Partial, by finding an ISBN (embedded metadata,
// falling back to the filename) and looking it up via the Google Books
// API. It is a no-op (zero-value summary, no network calls) unless
// cfg.ISBNLookup.Enabled is true. Mutates books in place; for any book a
// field actually got filled on, re-runs categorization and destination-path
// building immediately (mirroring appapi.Recompute), then re-runs
// destination-path disambiguation and duplicate detection over the whole
// batch once at the end if anything changed -- a newly-filled Title/Author
// can create a fresh DestPath collision or duplicate match against an
// untouched book too, the same reasoning pipeline.Run's
// disambiguateDestPaths doc comment gives for running those steps last.
func ResolveViaISBN(books []*book.Book, cfg config.Config) ISBNResolveSummary {
	var summary ISBNResolveSummary
	if !cfg.ISBNLookup.Enabled {
		return summary
	}

	var touchedAny bool
	for _, b := range books {
		status := b.Status()
		if status != book.SourceUnresolved && status != book.SourcePartial {
			continue
		}

		isbn, ok := findISBN(b.SourcePath)
		if !ok {
			summary.NoISBN++
			continue
		}

		summary.Attempted++
		res, err := lookupFunc(isbn, cfg.ISBNLookup.APIKey)
		if err != nil {
			summary.NotFound++
			continue
		}

		filled := false
		if b.Title.Value == "" && res.Title != "" {
			b.Title = book.Field{Value: res.Title, Source: book.SourceMetadata}
			filled = true
		}
		if b.Author.Value == "" && res.Author != "" {
			b.Author = book.Field{Value: res.Author, Source: book.SourceMetadata}
			filled = true
		}
		if b.Year.Value == "" && res.Year != "" {
			b.Year = book.Field{Value: res.Year, Source: book.SourceMetadata}
			filled = true
		}
		if filled {
			summary.Resolved++
			categorizer.Categorize(b, cfg)
			rename.BuildPath(b, cfg)
			touchedAny = true
		}
	}

	if touchedAny {
		disambiguateDestPaths(books)
		duplicates.Detect(books)
	}
	return summary
}

// findISBN looks for an ISBN in path's embedded structured metadata first
// (EPUB dc:identifier, MOBI/AZW3 EXTH 104 -- see metadata.Result.ISBN),
// falling back to the filename. It re-derives this on every call rather
// than reading a persisted value, since book.Book intentionally carries no
// ISBN field (see the design doc for why).
func findISBN(path string) (string, bool) {
	if res, err := metadata.Extract(path, nil); err == nil && res.ISBN != "" {
		return res.ISBN, true
	}
	return textutil.ExtractISBN(filepath.Base(path))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pipeline/... -v`
Expected: PASS (all tests, including pre-existing `pipeline_test.go` / `warnings_test.go` / `checkfolders_test.go` tests)

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/isbnresolve.go internal/pipeline/isbnresolve_test.go
git commit -m "Add pipeline.ResolveViaISBN: opt-in batch ISBN lookup and gap-fill"
```

---

## Task 7: `appapi.ResolveViaISBN`

**Files:**
- Create: `internal/appapi/isbn.go`
- Test: `internal/appapi/isbn_test.go`

**Interfaces:**
- Consumes: `pipeline.ResolveViaISBN(books []*book.Book, cfg config.Config) pipeline.ISBNResolveSummary` (Task 6), `(a *App).loadConfig() (config.Config, error)`, `viewToBook(v BookView) *book.Book`, `bookToView(b *book.Book) BookView` (existing, `internal/appapi/dto.go`).
- Produces: `appapi.ISBNResolveResultView{Books []BookView; Attempted, Resolved, NotFound, NoISBN int}`, `appapi.ErrISBNLookupDisabled error`, `func (a *App) ResolveViaISBN(books []BookView) (ISBNResolveResultView, error)` — the Wails-bound entry point the frontend calls.

- [ ] **Step 1: Write the failing tests**

```go
// internal/appapi/isbn_test.go
package appapi

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
)

func writeTestConfigWithISBNLookup(t *testing.T, working, library, logDir string, isbnLookup config.ISBNLookup) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Config{
		General: config.General{
			WorkingFolder:  working,
			LibraryFolder:  library,
			LogFolder:      logDir,
			FilenameFormat: "{title} ({year}) - {author}",
		},
		Categories: map[string]config.Category{"Uncategorized": {}},
		ISBNLookup: isbnLookup,
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	return path
}

func TestResolveViaISBN_DisabledReturnsError(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfigWithISBNLookup(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"), config.ISBNLookup{Enabled: false})

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	_, err := app.ResolveViaISBN([]BookView{{SourcePath: filepath.Join(working, "some.epub")}})
	if !errors.Is(err, ErrISBNLookupDisabled) {
		t.Errorf("err = %v, want ErrISBNLookupDisabled", err)
	}
}

func TestResolveViaISBN_EnabledWithNoISBNRoundTrips(t *testing.T) {
	working := t.TempDir()
	configPath := writeTestConfigWithISBNLookup(t, working, filepath.Join(t.TempDir(), "library"), filepath.Join(t.TempDir(), "logs"), config.ISBNLookup{Enabled: true})

	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	// No network call should happen: the filename has no ISBN, so
	// pipeline.ResolveViaISBN's findISBN step fails before ever reaching
	// lookupFunc -- this keeps the test network-free without needing to
	// stub anything.
	view := BookView{
		SourcePath: filepath.Join(working, "A Book With No ISBN In The Name.epub"),
		Title:      Field{Value: "", Source: "Unresolved"},
	}

	result, err := app.ResolveViaISBN([]BookView{view})
	if err != nil {
		t.Fatalf("ResolveViaISBN returned error: %v", err)
	}
	if result.NoISBN != 1 || result.Attempted != 0 {
		t.Errorf("result = %+v, want {NoISBN:1 Attempted:0}", result)
	}
	if len(result.Books) != 1 || result.Books[0].SourcePath != view.SourcePath {
		t.Errorf("Books = %+v, want the same single book round-tripped", result.Books)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/appapi/... -run TestResolveViaISBN -v`
Expected: FAIL to compile — `ErrISBNLookupDisabled`, `(*App).ResolveViaISBN`, and `ISBNResolveResultView` don't exist yet.

- [ ] **Step 3: Write the implementation**

```go
// internal/appapi/isbn.go
package appapi

import (
	"errors"

	"github.com/FrancisChung/book-organiser/internal/book"
	"github.com/FrancisChung/book-organiser/internal/pipeline"
)

// ErrISBNLookupDisabled is returned by ResolveViaISBN when
// cfg.ISBNLookup.Enabled is false. The frontend is expected to keep the
// "Resolve via ISBN" action hidden/disabled in that case -- this is
// defense in depth, not the primary UX gate.
var ErrISBNLookupDisabled = errors.New("isbn lookup is not enabled in config")

// ISBNResolveResultView is the JSON view of pipeline.ResolveViaISBN's
// outcome: the (possibly updated) books plus counts for a summary such as
// "Resolved 4 of 7 (2 had no ISBN, 1 not found)".
type ISBNResolveResultView struct {
	Books     []BookView `json:"books"`
	Attempted int        `json:"attempted"`
	Resolved  int        `json:"resolved"`
	NotFound  int        `json:"notFound"`
	NoISBN    int        `json:"noIsbn"`
}

// ResolveViaISBN re-derives an ISBN for every still-Unresolved/Partial book
// in books and looks it up via the Google Books API to fill Title/Author/
// Year gaps, re-running categorization/destination-path/duplicate
// detection as needed. It never touches the filesystem. Returns
// ErrISBNLookupDisabled without making any lookups if the loaded config
// has ISBNLookup.Enabled == false.
func (a *App) ResolveViaISBN(books []BookView) (ISBNResolveResultView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return ISBNResolveResultView{}, err
	}
	if !cfg.ISBNLookup.Enabled {
		return ISBNResolveResultView{}, ErrISBNLookupDisabled
	}

	bs := make([]*book.Book, len(books))
	for i, v := range books {
		bs[i] = viewToBook(v)
	}

	summary := pipeline.ResolveViaISBN(bs, cfg)

	views := make([]BookView, len(bs))
	for i, b := range bs {
		views[i] = bookToView(b)
	}
	return ISBNResolveResultView{
		Books:     views,
		Attempted: summary.Attempted,
		Resolved:  summary.Resolved,
		NotFound:  summary.NotFound,
		NoISBN:    summary.NoISBN,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/appapi/... -v`
Expected: PASS (all tests, including pre-existing `recompute_test.go` / `apply_test.go` / `scan_test.go` / etc.)

- [ ] **Step 5: Commit**

```bash
git add internal/appapi/isbn.go internal/appapi/isbn_test.go
git commit -m "Expose appapi.ResolveViaISBN for the frontend's Resolve via ISBN action"
```

---

## Final verification

- [ ] **Run the full test suite**

Run: `go build ./... && go test ./...`
Expected: build succeeds, all packages PASS.

This plan deliberately stops at the Go backend (`appapi.ResolveViaISBN` is the Wails-bound entry point). Wiring a "Resolve via ISBN" button into `desktop/frontend` and an ISBN-lookup section into the config UI/settings screen (if one exists) is frontend work outside this plan's scope and should be scoped separately once the backend lands.
