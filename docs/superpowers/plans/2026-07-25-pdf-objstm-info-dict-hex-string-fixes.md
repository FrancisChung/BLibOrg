# PDF ObjStm-Aware Info Dict Lookup + Hex-String Field Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix two confirmed, compounding bugs in `internal/metadata/pdf.go` that caused "Domain-Driven Design in PHP (2022)" (and any other XeTeX/LaTeX-produced PDF with the same shape) to show empty Title/Author: the Info dictionary lookup can't resolve an object compressed inside a PDF object stream (ObjStm), and the field-scraper can't recognize hex-string-syntax field values.

**Architecture:** Both fixes are small, targeted changes to `internal/metadata/pdf.go` (no other files change except a version bump). Bug 1 replaces `findInfoDictBody`'s ad-hoc raw-regex object lookup with the package's existing ObjStm-aware `pdfObjIndex.lookup()`. Bug 2 adds a second field-extraction regex (for hex-string syntax) alongside the existing literal-string one, feeding into the same shared string-decoding step.

**Tech Stack:** Go, this package's existing dependency-free regex/byte-scanning PDF parser (`encoding/hex` from the standard library, no new dependencies).

## Global Constraints

- No new external dependencies.
- The Info dict object lookup must preserve the existing "last occurrence in file order wins" behavior for incrementally-updated PDFs (already tested by `TestExtractPDF_UsesLatestIncrementalUpdateOfInfoObject`) while gaining ObjStm resolution.
- Hex-string field extraction only covers the same four keys already scraped from literal strings: Title, Author, Subject, CreationDate.
- When a key's value appears via both literal-string and hex-string syntax in the same dict (a contrived edge case, not expected in real PDFs), literal-string syntax takes precedence.
- `metadata.MetadataExtractorVersion` bumps from 1 to 2 (this only affects Title/Author extraction, not cover bytes, so `CoverExtractorVersion` is unaffected) so already-cached books with empty Title/Author self-heal on their next scan.

---

### Task 1: Resolve the Info dictionary even when compressed inside an ObjStm

**Files:**
- Modify: `internal/metadata/pdf.go`
- Test: `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: `buildPDFObjIndex`, `pdfObjIndex.lookup`, `splitPDFObjectBody` (all pre-existing, unchanged, from `internal/metadata/pdf_objects.go`).
- Produces: `findInfoDictBody(data []byte) ([]byte, bool)` — same public signature as before, no callers outside this file need changes. `findXRefStreamTrailerDict`'s signature changes from `(data []byte) []byte` to `(idx *pdfObjIndex) []byte` — its only caller is `findInfoDictBody`, updated in this same task.

- [ ] **Step 1: Write the failing tests**

Add `"fmt"` to `internal/metadata/pdf_test.go`'s import block (currently `bytes`, `compress/zlib`, `os`, `path/filepath`, `strconv`, `testing`, `unicode/utf16`, `unicode/utf8`):

```go
import (
	"bytes"
	"compress/zlib"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)
```

Add these two tests to `internal/metadata/pdf_test.go`, after the existing `TestExtractPDF_UsesLastXRefStreamWhenMultiplePresent` test:

```go
func TestExtractPDF_FindsInfoDictCompressedInsideObjStm(t *testing.T) {
	// Reproduces the real "Domain-Driven Design in PHP" bug's first half:
	// the Info dictionary is compressed inside a /Type /ObjStm object
	// (common in XeTeX/LaTeX-produced PDFs), not present anywhere as a
	// literal "3 0 obj ... endobj" block. Fixture layout follows the same
	// real, byte-offset-computed pattern as
	// TestPDFObjIndex_ResolvesObjectInsideObjStm (pdf_objects_test.go).
	infoObj := "<</Title(Compressed Book)/Author(Compressed Author)>>"
	header := "3 0"
	content := header + infoObj
	first := len(header)

	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n")
	fmt.Fprintf(&pdf, "9 0 obj\n<< /Type /ObjStm /N 1 /First %d /Filter /FlateDecode /Length %d >>\nstream\n", first, compressed.Len())
	pdf.Write(compressed.Bytes())
	pdf.WriteString("\nendstream\nendobj\n")
	pdf.WriteString("trailer\n<< /Root 1 0 R /Info 3 0 R >>\n%%EOF")

	path := writePDFFixture(t, pdf.String())

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "Compressed Book" {
		t.Errorf("Title = %q, want %q (Info dict located even though compressed inside an ObjStm)", result.Title, "Compressed Book")
	}
	if result.Author != "Compressed Author" {
		t.Errorf("Author = %q, want %q", result.Author, "Compressed Author")
	}
}

func TestExtractPDF_FindsInfoDictCompressedInsideObjStmViaXRefStreamTrailer(t *testing.T) {
	// Same shape as the classic-trailer version above, but via an XRef
	// stream trailer instead -- exercises findXRefStreamTrailerDict's
	// updated signature (now takes the shared *pdfObjIndex) end-to-end.
	infoObj := "<</Title(XRef Compressed Book)/Author(XRef Compressed Author)>>"
	header := "3 0"
	content := header + infoObj
	first := len(header)

	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n")
	fmt.Fprintf(&pdf, "9 0 obj\n<< /Type /ObjStm /N 1 /First %d /Filter /FlateDecode /Length %d >>\nstream\n", first, compressed.Len())
	pdf.Write(compressed.Bytes())
	pdf.WriteString("\nendstream\nendobj\n")
	pdf.WriteString("20 0 obj\n<< /Type /XRef /Info 3 0 R /Root 1 0 R /Size 21 /W [1 1 1] /Length 3 >>\nstream\nabc\nendstream\nendobj\n%%EOF")

	path := writePDFFixture(t, pdf.String())

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "XRef Compressed Book" {
		t.Errorf("Title = %q, want %q (Info dict located via XRef-stream trailer even though compressed inside an ObjStm)", result.Title, "XRef Compressed Book")
	}
	if result.Author != "XRef Compressed Author" {
		t.Errorf("Author = %q, want %q", result.Author, "XRef Compressed Author")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/ -run TestExtractPDF_FindsInfoDictCompressedInsideObjStm -v`
Expected: both FAIL with `Title = "", want "Compressed Book"` / `Title = "", want "XRef Compressed Book"` (the Info object is only reachable via ObjStm resolution, which `findInfoDictBody`'s current raw-regex lookup can't do).

- [ ] **Step 3: Implement the ObjStm-aware lookup**

In `internal/metadata/pdf.go`, add `"strconv"` to the import block, changing:

```go
import (
	"bytes"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/FrancisChung/BLibOrg/internal/textutil"
)
```

to:

```go
import (
	"bytes"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/FrancisChung/BLibOrg/internal/textutil"
)
```

Replace `findInfoDictBody`'s doc comment and body with:

```go
// findInfoDictBody locates the byte range of the PDF's real Info
// dictionary object, via the trailer's authoritative "/Info N 0 R"
// reference, so metadata extraction reads only the document's actual
// Title/Author/Subject/CreationDate instead of whichever matching pattern
// happens to appear first anywhere in the file. This matters because PDFs
// commonly embed graphics (logos, diagrams) that carry their own /Title,
// /Author, /Creator describing that graphic -- e.g. a CorelDRAW logo's own
// /Title, or an Illustrator diagram's own /Author -- and a naive
// first-match-anywhere scan can pick up a graphic's metadata instead of
// the book's if that graphic's object happens to appear earlier in the
// file. If the file has multiple trailers (incremental updates), the last
// one is used -- and, for the same "most recent update wins" reason, the
// object lookup below resolves via pdfObjIndex.lookup, which already
// prefers the LAST literal occurrence of a given object number (see
// buildPDFObjIndex) and additionally resolves an object compressed inside
// a /Type /ObjStm container (common in XeTeX/LaTeX-produced PDFs, which
// never appears as literal "N ... obj ... endobj" text in the file at
// all). If no classic "trailer <<...>>" keyword block exists at all,
// falls back to findXRefStreamTrailerDict for PDFs using a PDF 1.5+
// cross-reference stream instead (which carries the same /Info key
// directly in its own dictionary). Returns ok=false (caller falls back to
// a whole-file scan) if neither a trailer nor an XRef stream
// trailer-equivalent, no /Info reference, or no matching object is found
// -- preserving prior best-effort behavior for atypical PDFs rather than
// erroring.
func findInfoDictBody(data []byte) ([]byte, bool) {
	idx := buildPDFObjIndex(data)
	var trailerDict []byte
	trailers := pdfTrailerRe.FindAllSubmatch(data, -1)
	if len(trailers) > 0 {
		trailerDict = trailers[len(trailers)-1][1]
	} else {
		trailerDict = findXRefStreamTrailerDict(idx)
		if trailerDict == nil {
			return nil, false
		}
	}
	infoMatch := pdfInfoRefRe.FindSubmatch(trailerDict)
	if infoMatch == nil {
		return nil, false
	}
	objNum, err := strconv.Atoi(string(infoMatch[1]))
	if err != nil {
		return nil, false
	}
	body, ok := idx.lookup(objNum)
	if !ok {
		return nil, false
	}
	dict, _, _ := splitPDFObjectBody(body)
	return dict, true
}
```

Replace `findXRefStreamTrailerDict`'s doc comment and signature (keep its body's logic identical, just remove the now-redundant internal `buildPDFObjIndex` call and take `idx` as a parameter instead):

```go
// findXRefStreamTrailerDict locates the trailer-equivalent dict for PDFs
// using a PDF 1.5+ cross-reference stream instead of a classic "trailer
// <<...>>" keyword block: the XRef stream object (/Type /XRef) carries
// the same /Root, /Info, /Size keys directly in its own dictionary, per
// the PDF spec. Returns the LAST such object's dict in file order
// (mirroring findInfoDictBody's "most recent update wins" handling for
// classic trailers, since a later XRef stream supersedes an earlier
// one), or nil if no /Type /XRef object exists anywhere in idx. Takes an
// already-built *pdfObjIndex (from findInfoDictBody, its only caller)
// rather than building its own, avoiding a second full-file re-scan.
func findXRefStreamTrailerDict(idx *pdfObjIndex) []byte {
	objNums := make([]int, 0, len(idx.literal))
	for n := range idx.literal {
		objNums = append(objNums, n)
	}
	sort.Slice(objNums, func(i, j int) bool {
		return idx.literalOrder[objNums[i]] < idx.literalOrder[objNums[j]]
	})
	var found []byte
	for _, n := range objNums {
		dict, _, _ := splitPDFObjectBody(idx.literal[n])
		if pdfXRefTypeRe.Match(dict) {
			found = dict
		}
	}
	return found
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the two new ones and every pre-existing `findInfoDictBody`/`extractPDF` test — in particular `TestExtractPDF_UsesLatestIncrementalUpdateOfInfoObject` and `TestExtractPDF_UsesLastXRefStreamWhenMultiplePresent`, whose "last occurrence wins" expectations are preserved by `idx.literal`'s existing overwrite-on-each-occurrence behavior).

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_test.go
git commit -m "Resolve the Info dictionary even when compressed inside an ObjStm"
```

---

### Task 2: Recognize hex-string Title/Author/Subject/CreationDate values

**Files:**
- Modify: `internal/metadata/pdf.go`
- Test: `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: `decodePDFString` (pre-existing, unchanged) -- the shared raw-bytes-to-string decoding step both literal-string and hex-string syntax now feed into.
- Produces: `decodePDFHexBytes(h []byte) []byte`, a new unexported helper, sibling to the existing `unescapePDFBytes`. No other task depends on it.

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/metadata/pdf_test.go`, after the two ObjStm tests added in Task 1:

```go
func TestExtractPDF_HexStringTitleAndAuthor(t *testing.T) {
	// Reproduces the real "Domain-Driven Design in PHP" bug's second half:
	// XeTeX writes /Title and /Author as PDF hex strings (UTF-16BE with a
	// BOM), not literal parenthesized strings.
	titleHex := fmt.Sprintf("%x", utf16BEBytes("Domain-Driven Design in PHP"))
	authorHex := fmt.Sprintf("%x", utf16BEBytes("Carlos Buenosvinos, Christian Soronellas and Keyvan Akbary"))

	fixture := "%PDF-1.4\n1 0 obj\n<< /Title <" + titleHex + "> /Author <" + authorHex + "> /CreationDate (D:20220523070329) >>\nendobj\ntrailer\n<< /Root 1 0 R /Info 1 0 R >>\n%%EOF"
	path := writePDFFixture(t, fixture)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "Domain-Driven Design in PHP" {
		t.Errorf("Title = %q, want %q", result.Title, "Domain-Driven Design in PHP")
	}
	if result.Author != "Carlos Buenosvinos, Christian Soronellas and Keyvan Akbary" {
		t.Errorf("Author = %q, want %q", result.Author, "Carlos Buenosvinos, Christian Soronellas and Keyvan Akbary")
	}
	if result.Year != "2022" {
		t.Errorf("Year = %q, want 2022", result.Year)
	}
}

func TestExtractPDF_LiteralStringTakesPrecedenceOverHexStringForSameKey(t *testing.T) {
	// Contrived (a real Info dict wouldn't mix syntaxes for the same
	// key), but locks in the precedence rule deterministically rather
	// than leaving it to undefined map/regex-ordering behavior.
	hexTitle := fmt.Sprintf("%x", utf16BEBytes("Hex Title"))
	fixture := "%PDF-1.4\n1 0 obj\n<< /Title (Literal Title) /Title <" + hexTitle + "> >>\nendobj\ntrailer\n<< /Root 1 0 R /Info 1 0 R >>\n%%EOF"
	path := writePDFFixture(t, fixture)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "Literal Title" {
		t.Errorf("Title = %q, want %q (literal-string syntax takes precedence over hex-string)", result.Title, "Literal Title")
	}
}

func TestDecodePDFHexBytes_PadsOddLengthWithImplicitTrailingZero(t *testing.T) {
	even := decodePDFHexBytes([]byte("901FA3"))
	if string(even) != "\x90\x1F\xA3" {
		t.Errorf("even-length decode = %q, want %q", even, "\x90\x1F\xA3")
	}
	odd := decodePDFHexBytes([]byte("901FA"))
	if string(odd) != "\x90\x1F\xA0" {
		t.Errorf("odd-length decode = %q, want %q (PDF spec: an odd trailing digit gets an implicit trailing 0)", odd, "\x90\x1F\xA0")
	}
}

func TestDecodePDFHexBytes_StripsWhitespace(t *testing.T) {
	// PDF hex strings may contain whitespace between digit pairs.
	got := decodePDFHexBytes([]byte("90 1F A3"))
	if string(got) != "\x90\x1F\xA3" {
		t.Errorf("decode with whitespace = %q, want %q", got, "\x90\x1F\xA3")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/ -run TestExtractPDF_HexStringTitleAndAuthor -v` and `go test ./internal/metadata/ -run TestDecodePDFHexBytes -v`
Expected: `TestExtractPDF_HexStringTitleAndAuthor` FAILS with `Title = "", want "Domain-Driven Design in PHP"` (no hex-string extraction exists yet). `TestDecodePDFHexBytes_*` FAIL to compile (`decodePDFHexBytes` doesn't exist yet). `TestExtractPDF_LiteralStringTakesPrecedenceOverHexStringForSameKey` passes trivially today (Title already correctly resolves to the literal value, since no hex-string extraction runs at all yet to interfere) -- that's expected; it stays green once implemented and exists to lock in the precedence rule going forward, not to prove a regression right now.

- [ ] **Step 3: Implement hex-string field support**

In `internal/metadata/pdf.go`, add `"encoding/hex"` to the import block, changing:

```go
import (
	"bytes"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/FrancisChung/BLibOrg/internal/textutil"
)
```

to:

```go
import (
	"bytes"
	"encoding/hex"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/FrancisChung/BLibOrg/internal/textutil"
)
```

Add a new regex next to the existing `pdfLiteralStringRe` declaration (line 14):

```go
var pdfHexStringRe = regexp.MustCompile(`/(Title|Author|Subject|CreationDate)\s*<([0-9A-Fa-f\s]*)>`)
```

Add `decodePDFHexBytes` after the existing `unescapePDFBytes` function:

```go
// decodePDFHexBytes decodes h (the content of a PDF hex string, between
// its enclosing < and >) into raw bytes: whitespace and any non-hex
// character are stripped first (PDF permits whitespace inside a hex
// string), then an odd number of remaining digits gets an implicit
// trailing "0" appended per the spec (e.g. <901FA> means <901FA0>)
// before pairing. Sibling to unescapePDFBytes, which does the equivalent
// job for literal-string source syntax -- both feed their raw-byte
// result into decodePDFString for the shared encoding-detection step.
func decodePDFHexBytes(h []byte) []byte {
	digits := make([]byte, 0, len(h))
	for _, b := range h {
		if (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F') {
			digits = append(digits, b)
		}
	}
	if len(digits)%2 != 0 {
		digits = append(digits, '0')
	}
	decoded, err := hex.DecodeString(string(digits))
	if err != nil {
		return nil
	}
	return decoded
}
```

In `extractPDF`, add a second field-population loop right after the existing one, changing:

```go
	fields := map[string]string{}
	for _, m := range pdfLiteralStringRe.FindAllSubmatch(scope, -1) {
		key := string(m[1])
		if !foundInfo && (key == "Title" || key == "Author") {
			continue
		}
		if _, exists := fields[key]; exists {
			continue // keep first match only
		}
		fields[key] = decodePDFString(unescapePDFBytes(string(m[2])))
	}

	title := fields["Title"]
```

to:

```go
	fields := map[string]string{}
	for _, m := range pdfLiteralStringRe.FindAllSubmatch(scope, -1) {
		key := string(m[1])
		if !foundInfo && (key == "Title" || key == "Author") {
			continue
		}
		if _, exists := fields[key]; exists {
			continue // keep first match only
		}
		fields[key] = decodePDFString(unescapePDFBytes(string(m[2])))
	}
	for _, m := range pdfHexStringRe.FindAllSubmatch(scope, -1) {
		key := string(m[1])
		if !foundInfo && (key == "Title" || key == "Author") {
			continue
		}
		if _, exists := fields[key]; exists {
			continue // literal-string match (if any) takes precedence
		}
		fields[key] = decodePDFString(decodePDFHexBytes(m[2]))
	}

	title := fields["Title"]
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the four new ones and every pre-existing `extractPDF` metadata test -- literal-string extraction is untouched, hex-string extraction is purely additive).

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_test.go
git commit -m "Recognize hex-string Title/Author/Subject/CreationDate values"
```

---

### Task 3: Bump MetadataExtractorVersion so cached books self-heal

**Files:**
- Modify: `internal/metadata/extractor.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `metadata.MetadataExtractorVersion` changes from `1` to `2`. `internal/librarian`'s existing cache-hit condition (`entry.MetadataVersion == metadata.MetadataExtractorVersion`, added in the prior fix) already compares against this constant by reference, not a hardcoded number, so no changes are needed in `internal/librarian/librarian.go` or `internal/librarian/librarian_test.go` -- the existing tests `TestScan_StaleMetadataVersionForcesReExtractionDespiteMatchingCoverVersion` and `TestScan_ReExtractedEntryIsCachedWithCurrentMetadataVersion` already assert against the live constant value, whatever it is, and continue to pass unchanged after this bump.

- [ ] **Step 1: Bump the constant**

In `internal/metadata/extractor.go`, change:

```go
const MetadataExtractorVersion = 1 // findInfoDictBody can now locate the Info dict via a PDF 1.5+ cross-reference stream, not just a classic trailer.
```

to:

```go
const MetadataExtractorVersion = 2 // bumped: findInfoDictBody can now resolve an Info dict compressed inside an ObjStm, and extractPDF can now decode hex-string (not just literal-string) Title/Author/Subject/CreationDate values.
```

- [ ] **Step 2: Run the full metadata and librarian test suites to confirm no regressions**

Run: `go test ./internal/metadata/... ./internal/librarian/... ./internal/librarycache/... -v`
Expected: PASS (all tests -- in particular, confirm `TestScan_StaleMetadataVersionForcesReExtractionDespiteMatchingCoverVersion` and `TestScan_ReExtractedEntryIsCachedWithCurrentMetadataVersion` still pass, since both compare against `metadata.MetadataExtractorVersion` directly rather than a hardcoded `1`).

- [ ] **Step 3: Run the full build and vet**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS with no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/metadata/extractor.go
git commit -m "Bump MetadataExtractorVersion for the ObjStm/hex-string Info dict fixes"
```

---

## Manual Verification (after all tasks complete)

The real file used throughout this investigation is available locally for a final end-to-end check (not committed as a test fixture, per this package's existing-fixture convention):

1. Run a normal library scan (the version bump means this book's stale cache entry self-heals automatically -- no manual cache-clear needed).
2. In the desktop app, navigate to "Domain-Driven Design in PHP (2022)" in the Library view.
3. Confirm its Title now shows "Domain-Driven Design in PHP" and Author shows "Carlos Buenosvinos, Christian Soronellas and Keyvan Akbary" (previously both blank), and its cover still shows correctly (unaffected by this plan).
