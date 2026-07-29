# PDF Cover Selection: Five Root-Cause Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix five confirmed, independent bugs in `internal/metadata`'s PDF scanner, root-caused against `docs/superpowers/specs/2026-07-27-bug-books-with-bad-covers-final-list`'s 25 real books: an unresolved indirect `/Kids` reference, undecoded octal-escape sequences in literal strings, two gaps in composite-cover detection (multiple images on a page; text nested inside a Form XObject), and an unbounded legacy fallback that returns an unrelated interior image instead of a graceful page-1 render.

**Architecture:** All five fixes live in `internal/metadata`'s existing dependency-free PDF scanner, each isolated to the specific function that mishandles its case: `pdf_pages.go`'s `collectPDFPages` (Kids resolution), `pdf.go`'s `unescapePDFBytes` (octal escapes) and `findPDFCoverPageAware` (fallback ordering), and `pdf_render.go`'s `pageContentSuggestsCompositeCover` plus a new sibling `pageHasMultipleImages` in `pdf_images.go` (composite-cover detection). Every fix was verified against the real files during root-cause investigation: rendering the page PDFium would produce, post-fix, confirmed a genuine, correctly-designed cover in every case.

**Tech Stack:** Go standard library only, this package's existing regex/index-based PDF scanner, and the already-integrated PDFium renderer (`pdf_render.go`) -- no new dependencies.

## Global Constraints

- Each fix is independently scoped to the function that owns the bug; none of the five require touching more than one or two files.
- `metadata.CoverExtractorVersion` bumps once per task that changes cover-selection behavior (Tasks 1, 3, 4, 5); `metadata.MetadataExtractorVersion` bumps once for the task that changes Title/Author extraction (Task 2). Bump each in the exact sequence the tasks are executed in -- every task's Step 3 states the exact before/after value, assuming all earlier tasks in this plan are already committed.
- None of these fixes may regress `ListPDFCoverCandidates`/`ExtractPDFPageCover` (`pdf_override.go`, the manual cover-override picker's backend) -- they are unaffected by any of these five tasks; nothing here touches that file.
- Every fallback tier in `findPDFCoverPageAware` must still fall through gracefully on failure (a failed render falls through to the next tier, never erroring) -- this is the existing contract every prior PDF-cover plan has preserved, and these tasks extend it, not replace it.
- No new external dependency, in Go or otherwise.

---

### Task 1: Resolve an indirect `/Kids` reference in the page tree walk

**Files:**
- Modify: `internal/metadata/pdf_pages.go`
- Test: `internal/metadata/pdf_pages_test.go`, `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: `pdfObjIndex.lookup` (pre-existing, unchanged).
- Produces: `resolvePDFKidsContent(idx *pdfObjIndex, dict []byte) ([]byte, bool)`, a new unexported helper used only within `pdf_pages.go`. `walkPDFPageTree`'s existing public signature and behavior for every previously-working PDF are unchanged.

- [ ] **Step 1: Write the failing tests**

Add this test to `internal/metadata/pdf_pages_test.go`, after the existing `TestWalkPDFPageTree_NestedKids` test:

```go
func TestWalkPDFPageTree_ResolvesIndirectKidsReference(t *testing.T) {
	// Reproduces the real "Designing for AI"/"Grokking Software
	// Architecture"/"Grokking Streaming Systems" bug: /Kids is an
	// indirect reference to a separate object holding the array ("/Kids
	// 6 0 R"), not inlined ("/Kids [...]") -- pdfKidsRe alone never
	// matches this shape, which previously made the whole page-tree walk
	// fail (ok=false), forcing cover selection all the way back to
	// findPDFCover's unbounded whole-file scan.
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids 6 0 R /Count 2 >>\nendobj\n" +
			"6 0 obj\n[3 0 R 4 0 R]\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok, want true (indirect /Kids reference should resolve)")
	}
	if len(pages) != 2 {
		t.Fatalf("len(pages) = %d, want 2", len(pages))
	}
}
```

Add this test to `internal/metadata/pdf_test.go`, after the existing `TestFindPDFCoverPageAware_FallsBackToWholeFileScanWhenUndecodableImageRenderFails` test:

```go
func TestFindPDFCoverPageAware_ResolvesCoverWhenKidsIsIndirectReference(t *testing.T) {
	// Without this fix, this page tree fails to resolve entirely
	// (ok=false from walkPDFPageTree), so findPDFCoverPageAware falls
	// all the way back to findPDFCover's unbounded whole-file scan --
	// which, in the real bug, returned a small unrelated logo image
	// (object 1 here) that happened to have a lower object number than
	// the true cover (object 5, correctly reached via the page tree).
	jpegData := []byte("\xFF\xD8\xFFrealcoverbytes")
	logoData := []byte("\xFF\xD8\xFFlogobytes12")
	data := []byte(
		"1 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 14 >>\nstream\n" + string(logoData) + "\nendstream\nendobj\n" +
			"2 0 obj\n<< /Type /Catalog /Pages 3 0 R >>\nendobj\n" +
			"3 0 obj\n<< /Type /Pages /Kids 6 0 R /Count 1 >>\nendobj\n" +
			"6 0 obj\n[4 0 R]\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Parent 3 0 R /Resources << /XObject << /Im0 5 0 R >> >> >>\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 17 >>\nstream\n" + string(jpegData) + "\nendstream\nendobj\n")

	imageBytes, contentType, ok := findPDFCoverPageAware(data, 10)
	if !ok {
		t.Fatal("findPDFCoverPageAware ok=false, want true")
	}
	if contentType != "image/jpeg" || string(imageBytes) != string(jpegData) {
		t.Errorf("got %q (%d bytes), want the page's own cover image %q, not object 1's unrelated logo", imageBytes, len(imageBytes), jpegData)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/ -run 'TestWalkPDFPageTree_ResolvesIndirectKidsReference|TestFindPDFCoverPageAware_ResolvesCoverWhenKidsIsIndirectReference' -v`
Expected: Both FAIL -- `walkPDFPageTree` returns `ok=false` for the first (no inline `/Kids [...]` array to match), and the second gets the logo bytes instead of the real cover (the current code falls through to `findPDFCover`'s whole-file scan, which finds object 1 first).

- [ ] **Step 3: Add the indirect-Kids resolution helper**

In `internal/metadata/pdf_pages.go`, change:

```go
// collectPDFPages recursively walks the Pages tree node at objNum,
// appending every /Type /Page leaf found (in Kids order) to pages, until
// limit have been collected. visited guards against a malformed/cyclic
// Kids reference looping forever; one unresolvable Kids entry is skipped
// rather than aborting the whole walk.
func collectPDFPages(idx *pdfObjIndex, objNum int, pages *[]pdfPage, visited map[int]bool, limit int) {
	if len(*pages) >= limit || visited[objNum] {
		return
	}
	visited[objNum] = true

	body, ok := idx.lookup(objNum)
	if !ok {
		return
	}
	dict, _, _ := splitPDFObjectBody(body)

	if pdfTypePageLeafRe.Match(dict) {
		*pages = append(*pages, pdfPage{number: len(*pages) + 1, dict: dict})
		return
	}

	kidsMatch := pdfKidsRe.FindSubmatch(dict)
	if kidsMatch == nil {
		return
	}
	for _, ref := range pdfKidRefRe.FindAllSubmatch(kidsMatch[1], -1) {
		if len(*pages) >= limit {
			return
		}
		kidNum, err := strconv.Atoi(string(ref[1]))
		if err != nil {
			continue
		}
		collectPDFPages(idx, kidNum, pages, visited, limit)
	}
}
```

to:

```go
// collectPDFPages recursively walks the Pages tree node at objNum,
// appending every /Type /Page leaf found (in Kids order) to pages, until
// limit have been collected. visited guards against a malformed/cyclic
// Kids reference looping forever; one unresolvable Kids entry is skipped
// rather than aborting the whole walk.
func collectPDFPages(idx *pdfObjIndex, objNum int, pages *[]pdfPage, visited map[int]bool, limit int) {
	if len(*pages) >= limit || visited[objNum] {
		return
	}
	visited[objNum] = true

	body, ok := idx.lookup(objNum)
	if !ok {
		return
	}
	dict, _, _ := splitPDFObjectBody(body)

	if pdfTypePageLeafRe.Match(dict) {
		*pages = append(*pages, pdfPage{number: len(*pages) + 1, dict: dict})
		return
	}

	kidsContent, ok := resolvePDFKidsContent(idx, dict)
	if !ok {
		return
	}
	for _, ref := range pdfKidRefRe.FindAllSubmatch(kidsContent, -1) {
		if len(*pages) >= limit {
			return
		}
		kidNum, err := strconv.Atoi(string(ref[1]))
		if err != nil {
			continue
		}
		collectPDFPages(idx, kidNum, pages, visited, limit)
	}
}

// resolvePDFKidsContent returns the byte range holding a Pages node's
// Kids array entries -- either the inline "[...]" array's own content
// (pdfKidsRe's usual case), or, when /Kids is instead an indirect
// reference to a separate object holding the array (e.g. "/Kids 6 0 R",
// rather than "/Kids [...]" inlined in this dict -- a pattern some PDF
// producers use, apparently to keep large Pages dicts smaller), that
// object's own body resolved via idx.lookup. Either shape's result feeds
// pdfKidRefRe.FindAllSubmatch the same way: pdfKidRefRe matches "N G R"
// patterns anywhere in its input, so the resolved object's raw body --
// brackets and all -- works without needing to re-extract just the
// bracketed interior. ok is false if neither shape matches, or the
// indirect object can't be resolved.
func resolvePDFKidsContent(idx *pdfObjIndex, dict []byte) ([]byte, bool) {
	if m := pdfKidsRe.FindSubmatch(dict); m != nil {
		return m[1], true
	}
	m := pdfKidsIndirectRe.FindSubmatch(dict)
	if m == nil {
		return nil, false
	}
	objNum, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return nil, false
	}
	return idx.lookup(objNum)
}
```

Then, in the same file, change the regex var block:

```go
var pdfCatalogTypeRe = regexp.MustCompile(`/Type\s*/Catalog\b`)
var pdfPagesRefRe = regexp.MustCompile(`/Pages\s+(\d+)\s+\d+\s+R`)
var pdfTypePageLeafRe = regexp.MustCompile(`/Type\s*/Page\b`)
var pdfKidsRe = regexp.MustCompile(`/Kids\s*\[([^\]]*)\]`)
var pdfKidRefRe = regexp.MustCompile(`(\d+)\s+\d+\s+R`)
```

to:

```go
var pdfCatalogTypeRe = regexp.MustCompile(`/Type\s*/Catalog\b`)
var pdfPagesRefRe = regexp.MustCompile(`/Pages\s+(\d+)\s+\d+\s+R`)
var pdfTypePageLeafRe = regexp.MustCompile(`/Type\s*/Page\b`)
var pdfKidsRe = regexp.MustCompile(`/Kids\s*\[([^\]]*)\]`)
var pdfKidsIndirectRe = regexp.MustCompile(`/Kids\s+(\d+)\s+\d+\s+R`)
var pdfKidRefRe = regexp.MustCompile(`(\d+)\s+\d+\s+R`)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the two new ones and every pre-existing test).

- [ ] **Step 5: Bump CoverExtractorVersion**

In `internal/metadata/extractor.go`, change:

```go
const CoverExtractorVersion = 5 // bumped: findPDFCoverPageAware can now fall back to a full-page PDFium render when a page's only image uses an unsupported filter (e.g. JPXDecode/JPEG 2000), producing a cover where none existed before for the same book.
```

to:

```go
const CoverExtractorVersion = 6 // bumped: the page-tree walker now resolves an indirect "/Kids N 0 R" reference (not just an inline "/Kids [...]" array), so PDFs using that shape no longer silently fall back to the legacy whole-file scan and pick up an unrelated small image (e.g. a publisher logo) instead of the true, page-tree-associated cover.
```

- [ ] **Step 6: Run tests to verify they still pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/metadata/pdf_pages.go internal/metadata/pdf_pages_test.go internal/metadata/pdf_test.go internal/metadata/extractor.go
git commit -m "Resolve an indirect /Kids reference in the PDF page-tree walk"
```

---

### Task 2: Decode octal-escape sequences in PDF literal strings

**Files:**
- Modify: `internal/metadata/pdf.go`
- Test: `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `unescapePDFBytes(s string) []byte`'s existing signature is unchanged; only its handling of `\ddd` sequences changes. No other task depends on this one.

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/metadata/pdf_test.go`, after the existing `TestExtractPDF_UTF16BEEncodedStrings` test:

```go
func TestExtractPDF_OctalEscapedUTF16BETitle(t *testing.T) {
	// Reproduces the real "Build Your Own Database From Scratch in
	// Go"/"Practical FP in Scala" bug: some PDF producers write a
	// literal string's UTF-16BE BOM and content bytes as \ddd octal
	// escapes (the only way to embed a raw NUL or 0xFE/0xFF byte inside
	// "(...)" syntax) rather than embedding them unescaped -- unlike
	// TestExtractPDF_UTF16BEEncodedStrings' fixture, which writes those
	// same byte values directly, never exercising escape parsing at all.
	// Before this fix, unescapePDFBytes only recognized \n/\r/\t; any
	// other character after a backslash (including an octal digit) fell
	// through to its default case, which appended the backslash's target
	// byte unchanged -- turning \376\377\000B... into the literal digit
	// text "376377000B...".
	fixture := "%PDF-1.4\n" +
		"1 0 obj\n<< /Title (\\376\\377\\000B\\000o\\000o\\000k) /Author (Plain Author) >>\nendobj\n" +
		"trailer\n<< /Root 1 0 R /Info 1 0 R >>\n%%EOF"
	path := writePDFFixture(t, fixture)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "Book" {
		t.Errorf("Title = %q, want %q", result.Title, "Book")
	}
}

func TestExtractPDF_OctalEscapedASCIIPunctuation(t *testing.T) {
	// Reproduces the real "AI Product Management" bug: a single
	// octal-escaped ASCII punctuation character (a colon, \072 = 0x3A)
	// mixed into an otherwise-plain-ASCII title.
	fixture := "%PDF-1.4\n" +
		"1 0 obj\n<< /Title (AI Product Management\\072 A Practical Guide) /Author (Plain Author) >>\nendobj\n" +
		"trailer\n<< /Root 1 0 R /Info 1 0 R >>\n%%EOF"
	path := writePDFFixture(t, fixture)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "AI Product Management: A Practical Guide" {
		t.Errorf("Title = %q, want %q", result.Title, "AI Product Management: A Practical Guide")
	}
}

func TestUnescapePDFBytes_OctalEscapeOverflowTruncatesToOneByte(t *testing.T) {
	// The PDF spec requires "high-order overflow shall be ignored" for a
	// \ddd value greater than 255 (octal \777 = decimal 511) -- Go's
	// byte(511) conversion already truncates to the low 8 bits (255),
	// which is exactly equivalent to mod 256, so this pins that the
	// straightforward implementation has the spec-correct behavior
	// without needing an explicit modulo.
	got := unescapePDFBytes(`\777`)
	if len(got) != 1 || got[0] != 255 {
		t.Errorf("unescapePDFBytes(`\\777`) = %v, want a single byte 255", got)
	}
}

func TestUnescapePDFBytes_PreservesExistingEscapes(t *testing.T) {
	// Regression check: \n, \r, \t, and a backslash-escaped literal
	// character (e.g. "\(" for a literal paren) must keep working
	// exactly as before -- this fix only adds a new case (octal digits),
	// it must not change the existing ones.
	got := unescapePDFBytes(`a\nb\rc\td\(e\)f`)
	want := "a\nb\rc\td(e)f"
	if string(got) != want {
		t.Errorf("unescapePDFBytes(...) = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/ -run 'TestExtractPDF_OctalEscaped|TestUnescapePDFBytes' -v`
Expected: `TestExtractPDF_OctalEscapedUTF16BETitle` and `TestExtractPDF_OctalEscapedASCIIPunctuation` FAIL with a garbled `Title` (the literal digit text described above). `TestUnescapePDFBytes_OctalEscapeOverflowTruncatesToOneByte` FAILS because `\7` alone (the first digit, with `77` falling through as literal text) doesn't consume the remaining two digits into one byte. `TestUnescapePDFBytes_PreservesExistingEscapes` currently PASSES already (its own escapes aren't octal) -- confirm it stays passing after the fix in Step 4, not just before.

- [ ] **Step 3: Implement octal-escape decoding**

In `internal/metadata/pdf.go`, change:

```go
func unescapePDFBytes(s string) []byte {
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
	return out
}
```

to:

```go
func unescapePDFBytes(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch {
			case s[i] == 'n':
				out = append(out, '\n')
			case s[i] == 'r':
				out = append(out, '\r')
			case s[i] == 't':
				out = append(out, '\t')
			case s[i] >= '0' && s[i] <= '7':
				// PDF literal strings support a \ddd escape (1-3 octal
				// digits) for embedding an arbitrary byte value -- the
				// only way to write a control byte like a UTF-16BE
				// byte-order-mark (0xFE 0xFF) or a raw NUL inside
				// "(...)" syntax, and real producers also use it for
				// stray ASCII punctuation (e.g. \072 for a colon).
				// Consume up to 2 more octal digits beyond the one
				// already at s[i]; a value over 255 (e.g. \777 = 511)
				// truncates to its low 8 bits via the byte() conversion
				// below, matching the spec's "high-order overflow shall
				// be ignored" rule.
				val := int(s[i] - '0')
				for digits := 1; digits < 3 && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '7'; digits++ {
					i++
					val = val*8 + int(s[i]-'0')
				}
				out = append(out, byte(val))
			default:
				out = append(out, s[i])
			}
			continue
		}
		out = append(out, s[i])
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the four new ones and every pre-existing test -- in particular, `TestExtractPDF_UTF16BEEncodedStrings` and every other existing literal-string test must still pass unchanged).

- [ ] **Step 5: Bump MetadataExtractorVersion**

In `internal/metadata/extractor.go`, change:

```go
const MetadataExtractorVersion = 4 // bumped: findInfoDictBody now prefers whichever classic trailer actually declares /Info, not simply the byte-order-last one -- a linearized PDF's tail trailer commonly lacks /Info entirely, which could change which Info dict object (and hence Title/Author/Subject) is used for an already-scanned file.
```

to:

```go
const MetadataExtractorVersion = 5 // bumped: unescapePDFBytes now decodes \ddd octal-escape sequences in literal strings (previously only \n/\r/\t were recognized; any other escaped character, including an octal digit, passed through as literal text) -- a Title/Author using this escape shape now decodes correctly instead of showing garbled digit text.
```

- [ ] **Step 6: Run tests to verify they still pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_test.go internal/metadata/extractor.go
git commit -m "Decode \\ddd octal-escape sequences in PDF literal strings"
```

---

### Task 3: Treat multiple images on one page as a composite-cover signal

**Files:**
- Modify: `internal/metadata/pdf.go`, `internal/metadata/pdf_images.go`
- Test: `internal/metadata/pdf_images_test.go`, `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: `findPDFPageImages` (pre-existing, unchanged).
- Produces: `pageHasMultipleImages(idx *pdfObjIndex, page pdfPage) bool`, a new exported-within-package helper in `pdf_images.go`, consumed by `findPDFCoverPageAware` (`pdf.go`) -- no other task depends on it.

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/metadata/pdf_images_test.go`, at the end of the file:

```go
func TestPageHasMultipleImages_TrueWhenPageHasTwoOrMoreImages(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 4 0 R /Im1 5 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" + string(jpegData) + "\nendstream\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" + string(jpegData) + "\nendstream\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok || len(pages) != 1 {
		t.Fatalf("walkPDFPageTree ok=%v pages=%d, want ok=true pages=1", ok, len(pages))
	}
	if !pageHasMultipleImages(idx, pages[0]) {
		t.Error("pageHasMultipleImages = false, want true (page has 2 image XObjects)")
	}
}

func TestPageHasMultipleImages_FalseWhenPageHasOneImage(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 4 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" + string(jpegData) + "\nendstream\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok || len(pages) != 1 {
		t.Fatalf("walkPDFPageTree ok=%v pages=%d, want ok=true pages=1", ok, len(pages))
	}
	if pageHasMultipleImages(idx, pages[0]) {
		t.Error("pageHasMultipleImages = true, want false (page has only 1 image XObject)")
	}
}
```

Add this test to `internal/metadata/pdf_test.go`, after the existing `TestFindPDFCoverPageAware_UsesRawImageWhenNoCompositeCoverSignal` test:

```go
func TestFindPDFCoverPageAware_RendersFullPageWhenMultipleImagesFoundEvenWithoutText(t *testing.T) {
	// Reproduces the real "The New Consultant's Quick Start Guide"/
	// "Practical FP in Scala" bug: the cover page is composed from
	// several layered/tiled images (a background texture plus separate
	// graphic elements), with the title itself flattened into one of
	// those images rather than drawn via a real text-show operator --
	// pageContentSuggestsCompositeCover alone never fires, so plain
	// image-XObject extraction previously returned whichever one of the
	// several images happened to be found first (an arbitrary background
	// tile or overlay layer, sometimes rendering as near-black), not the
	// full composited page.
	orig := renderPDFPageAsCoverFunc
	defer func() { renderPDFPageAsCoverFunc = orig }()

	called := false
	renderPDFPageAsCoverFunc = func(data []byte, pageNum int) ([]byte, string, bool) {
		called = true
		return []byte("RENDERED-PNG-BYTES"), "image/png", true
	}

	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 4 0 R /Im1 5 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" + string(jpegData) + "\nendstream\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" + string(jpegData) + "\nendstream\nendobj\n")

	imageBytes, contentType, ok := findPDFCoverPageAware(data, 10)
	if !ok {
		t.Fatal("findPDFCoverPageAware ok=false, want true")
	}
	if !called {
		t.Fatal("renderPDFPageAsCoverFunc was not called despite the page having 2 images and no text-show operator")
	}
	if string(imageBytes) != "RENDERED-PNG-BYTES" || contentType != "image/png" {
		t.Errorf("got %q/%q, want the rendered stand-in bytes/content-type", imageBytes, contentType)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/ -run 'TestPageHasMultipleImages|TestFindPDFCoverPageAware_RendersFullPageWhenMultipleImagesFoundEvenWithoutText' -v`
Expected: `TestPageHasMultipleImages_*` FAIL to compile (`pageHasMultipleImages` doesn't exist yet). `TestFindPDFCoverPageAware_RendersFullPageWhenMultipleImagesFoundEvenWithoutText` FAILS with `renderPDFPageAsCoverFunc was not called` (the current code returns the first raw image directly since there's no text-show operator).

- [ ] **Step 3: Add `pageHasMultipleImages` and wire it in**

In `internal/metadata/pdf_images.go`, add this function immediately after `findPDFPageImages`:

```go
// pageHasMultipleImages reports whether page contains more than one
// qualifying image XObject -- a second signal (independent of
// pageContentSuggestsCompositeCover's text-operator check, pdf_render.go)
// that the page's true visual cover can't be recovered by extracting a
// single image alone: real-world cover art is sometimes composed from
// several layered/tiled images (a background texture plus a separate
// logo, or several texture tiles), with no text-show operator at all if
// the title itself is flattened into one of those images. Either signal
// alone is sufficient for findPDFCoverPageAware (pdf.go) to prefer a
// full-page render over the first raw image found.
func pageHasMultipleImages(idx *pdfObjIndex, page pdfPage) bool {
	return len(findPDFPageImages(idx, []pdfPage{page}, false)) > 1
}
```

In `internal/metadata/pdf.go`, change:

```go
func findPDFCoverPageAware(data []byte, pageLimit int) ([]byte, string, bool) {
	idx := buildPDFObjIndex(data)
	if pages, ok := walkPDFPageTree(idx, pageLimit); ok {
		if images := findPDFPageImages(idx, pages, true); len(images) > 0 {
			img := images[0]
			if page, found := findPageByNumber(pages, img.page); found && pageContentSuggestsCompositeCover(idx, page) {
				if renderedBytes, renderedContentType, ok := renderPDFPageAsCoverFunc(data, img.page); ok {
					return renderedBytes, renderedContentType, true
				}
			}
			return img.bytes, img.contentType, true
		}
```

to:

```go
func findPDFCoverPageAware(data []byte, pageLimit int) ([]byte, string, bool) {
	idx := buildPDFObjIndex(data)
	if pages, ok := walkPDFPageTree(idx, pageLimit); ok {
		if images := findPDFPageImages(idx, pages, true); len(images) > 0 {
			img := images[0]
			if page, found := findPageByNumber(pages, img.page); found && (pageContentSuggestsCompositeCover(idx, page) || pageHasMultipleImages(idx, page)) {
				if renderedBytes, renderedContentType, ok := renderPDFPageAsCoverFunc(data, img.page); ok {
					return renderedBytes, renderedContentType, true
				}
			}
			return img.bytes, img.contentType, true
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the three new ones and every pre-existing test).

- [ ] **Step 5: Bump CoverExtractorVersion**

In `internal/metadata/extractor.go`, change:

```go
const CoverExtractorVersion = 6 // bumped: the page-tree walker now resolves an indirect "/Kids N 0 R" reference (not just an inline "/Kids [...]" array), so PDFs using that shape no longer silently fall back to the legacy whole-file scan and pick up an unrelated small image (e.g. a publisher logo) instead of the true, page-tree-associated cover.
```

to:

```go
const CoverExtractorVersion = 7 // bumped: a page with more than one qualifying image XObject now also triggers a full-page composite render (previously only a text-show operator did), so cover art composed from several layered/tiled images no longer returns an arbitrary single layer.
```

- [ ] **Step 6: Run tests to verify they still pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_images.go internal/metadata/pdf_images_test.go internal/metadata/pdf_test.go internal/metadata/extractor.go
git commit -m "Treat multiple images on one page as a composite-cover signal"
```

---

### Task 4: Recurse into Form XObjects when detecting a composite cover's text

**Files:**
- Modify: `internal/metadata/pdf_render.go`
- Test: `internal/metadata/pdf_render_test.go`

**Interfaces:**
- Consumes: `pdfXObjectEntryRe`, `pdfSubtypeFormRe`, `maxFormXObjectDepth`, `resolveDictValue` (all pre-existing, from `pdf_images.go`/`pdf_objects.go`, unchanged).
- Produces: `decodePDFContentStream(dict, stream []byte) []byte` and `formXObjectContainsTextShowOperator(idx *pdfObjIndex, xobjects []byte, depth int, visited map[int]bool) bool`, both new unexported helpers used only within `pdf_render.go`. `pageContentSuggestsCompositeCover`'s existing public signature and behavior for every previously-working PDF are unchanged (this task only adds a new case it previously missed).

- [ ] **Step 1: Write the failing test**

Add this test to `internal/metadata/pdf_render_test.go`, after the existing `TestPageContentSuggestsCompositeCover_DetectsArrayFormTJWithNoPrecedingSpace` test:

```go
func TestPageContentSuggestsCompositeCover_TrueWhenTextIsInsideFormXObject(t *testing.T) {
	// Reproduces the real "Understanding Distributed Systems" bug: the
	// page's own /Contents stream only invokes a single top-level Form
	// XObject ("/Fm0 Do"); the actual image draw AND the title text-show
	// operator are both inside THAT form's own content stream, not the
	// page's. The page-level /Contents check alone never sees the text,
	// so pageContentSuggestsCompositeCover always returned false for this
	// shape, and plain image-XObject extraction returned just the lone
	// embedded image (e.g. a stock photo used partway down the real
	// cover) instead of the full composited page.
	pageContent := "/Fm0 Do"
	formContent := "q 200 0 0 200 0 0 cm /Im0 Do Q BT /F1 12 Tf 10 10 Td (Title Text) Tj ET"
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /Resources << /XObject << /Fm0 6 0 R >> >> /Contents 5 0 R >>\nendobj\n" +
			"5 0 obj\n<< /Length 7 >>\nstream\n" + pageContent + "\nendstream\nendobj\n" +
			"6 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Im0 7 0 R >> /Font << /F1 8 0 R >> >> /Length 71 >>\nstream\n" + formContent + "\nendstream\nendobj\n" +
			"7 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n\xFF\xD8\xFFfakejpegbytes\nendstream\nendobj\n" +
			"8 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok || len(pages) != 1 {
		t.Fatalf("walkPDFPageTree ok=%v pages=%d, want ok=true pages=1", ok, len(pages))
	}

	if !pageContentSuggestsCompositeCover(idx, pages[0]) {
		t.Error("pageContentSuggestsCompositeCover = false, want true -- the text-show operator lives inside the page's Form XObject, not its own /Contents stream")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/ -run TestPageContentSuggestsCompositeCover_TrueWhenTextIsInsideFormXObject -v`
Expected: FAIL with `pageContentSuggestsCompositeCover = false, want true` (the page's own `/Contents` stream is just `/Fm0 Do`, no text-show operator; the current code never looks inside the Form XObject it invokes).

- [ ] **Step 3: Extract the shared decompression helper and add Form-XObject recursion**

In `internal/metadata/pdf_render.go`, change:

```go
func pageContentSuggestsCompositeCover(idx *pdfObjIndex, page pdfPage) bool {
	var objNums []int
	if m := pdfContentsArrayRe.FindSubmatch(page.dict); m != nil {
		for _, ref := range pdfKidRefRe.FindAllSubmatch(m[1], -1) {
			if n, err := strconv.Atoi(string(ref[1])); err == nil {
				objNums = append(objNums, n)
			}
		}
	} else if m := pdfContentsRefRe.FindSubmatch(page.dict); m != nil {
		if n, err := strconv.Atoi(string(m[1])); err == nil {
			objNums = append(objNums, n)
		}
	}

	for _, n := range objNums {
		body, ok := idx.lookup(n)
		if !ok {
			continue
		}
		dict, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream {
			continue
		}
		// Content streams aren't required to declare a /Filter -- only
		// attempt zlib decompression when one is explicitly present
		// (the same check decodeFlatePDFImage uses for image streams,
		// pdf_flate.go), otherwise treat the stream as already-plain
		// content-operator bytes. This avoids ever running the
		// text-operator regex over corrupted/truncated compressed
		// binary, which a blind try/fallback could do.
		content := stream
		if pdfFlateDecodeRe.Match(dict) {
			r, err := zlib.NewReader(bytes.NewReader(stream))
			if err != nil {
				continue
			}
			decompressed, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				continue
			}
			content = decompressed
		}
		if matchesTextShowOperator(content) {
			return true
		}
	}
	return false
}
```

to:

```go
func pageContentSuggestsCompositeCover(idx *pdfObjIndex, page pdfPage) bool {
	var objNums []int
	if m := pdfContentsArrayRe.FindSubmatch(page.dict); m != nil {
		for _, ref := range pdfKidRefRe.FindAllSubmatch(m[1], -1) {
			if n, err := strconv.Atoi(string(ref[1])); err == nil {
				objNums = append(objNums, n)
			}
		}
	} else if m := pdfContentsRefRe.FindSubmatch(page.dict); m != nil {
		if n, err := strconv.Atoi(string(m[1])); err == nil {
			objNums = append(objNums, n)
		}
	}

	for _, n := range objNums {
		body, ok := idx.lookup(n)
		if !ok {
			continue
		}
		dict, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream {
			continue
		}
		if matchesTextShowOperator(decodePDFContentStream(dict, stream)) {
			return true
		}
	}

	// The page's own /Contents stream(s) may draw nothing but "/Fm0 Do"
	// -- a single top-level Form XObject wrapping everything (image and
	// text alike), the shape real prepress/print-registration output
	// commonly produces. In that case the loop above finds no text-show
	// operator at all, even though the page visually has one, because
	// it's nested inside the Form's own content stream instead.
	if resources, ok := resolveDictValue(idx, page.dict, "Resources"); ok {
		if xobjects, ok := resolveDictValue(idx, resources, "XObject"); ok {
			return formXObjectContainsTextShowOperator(idx, xobjects, 0, map[int]bool{})
		}
	}
	return false
}

// decodePDFContentStream returns stream's plain content-operator bytes,
// inflating it first if dict declares FlateDecode -- the shared
// decompression step pageContentSuggestsCompositeCover needs for both a
// page's own /Contents stream(s) and a Form XObject's own stream.
// Content streams aren't required to declare a /Filter -- only attempt
// zlib decompression when one is explicitly present (the same check
// decodeFlatePDFImage uses for image streams, pdf_flate.go), otherwise
// treat the stream as already-plain bytes. This avoids ever running the
// text-operator regex over corrupted/truncated compressed binary, which
// a blind try/fallback could do. Returns stream unchanged if inflation
// fails.
func decodePDFContentStream(dict, stream []byte) []byte {
	if !pdfFlateDecodeRe.Match(dict) {
		return stream
	}
	r, err := zlib.NewReader(bytes.NewReader(stream))
	if err != nil {
		return stream
	}
	decompressed, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		return stream
	}
	return decompressed
}

// formXObjectContainsTextShowOperator recurses into xobjects (a page's or
// nested Form's own /XObject dict) looking for a /Subtype /Form entry
// whose own content stream contains a text-show operator -- the
// composite-cover signal pageContentSuggestsCompositeCover's own
// /Contents check can't see when a PDF producer wraps a whole page's
// content inside a single top-level Form XObject invoked via "/Fm0 Do".
// Mirrors findImagesInXObjects' traversal (pdf_images.go): same
// maxFormXObjectDepth cap and cycle-guarding visited set, but checking
// for a text-show operator instead of collecting images.
func formXObjectContainsTextShowOperator(idx *pdfObjIndex, xobjects []byte, depth int, visited map[int]bool) bool {
	if depth >= maxFormXObjectDepth {
		return false
	}
	for _, ref := range pdfXObjectEntryRe.FindAllSubmatch(xobjects, -1) {
		objNum, err := strconv.Atoi(string(ref[1]))
		if err != nil || visited[objNum] {
			continue
		}
		body, ok := idx.lookup(objNum)
		if !ok {
			continue
		}
		dict, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream || !pdfSubtypeFormRe.Match(dict) {
			continue
		}
		visited[objNum] = true
		if matchesTextShowOperator(decodePDFContentStream(dict, stream)) {
			return true
		}
		if formResources, ok := resolveDictValue(idx, dict, "Resources"); ok {
			if formXObjects, ok := resolveDictValue(idx, formResources, "XObject"); ok {
				if formXObjectContainsTextShowOperator(idx, formXObjects, depth+1, visited) {
					return true
				}
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the new one and every pre-existing test -- in particular every existing `TestPageContentSuggestsCompositeCover_*` test, since the page-level `/Contents` loop's behavior is unchanged, only refactored to call the new shared helper).

- [ ] **Step 5: Bump CoverExtractorVersion**

In `internal/metadata/extractor.go`, change:

```go
const CoverExtractorVersion = 7 // bumped: a page with more than one qualifying image XObject now also triggers a full-page composite render (previously only a text-show operator did), so cover art composed from several layered/tiled images no longer returns an arbitrary single layer.
```

to:

```go
const CoverExtractorVersion = 8 // bumped: composite-cover detection now recurses into a page's Form XObjects to find a text-show operator nested inside one (previously only the page's own /Contents stream was checked directly), so a page that wraps its whole visual content in a single top-level Form no longer returns just its lone embedded image instead of the full composited page.
```

- [ ] **Step 6: Run tests to verify they still pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/metadata/pdf_render.go internal/metadata/pdf_render_test.go internal/metadata/extractor.go
git commit -m "Recurse into Form XObjects when detecting a composite cover's text"
```

---

### Task 5: Render page 1 in full before falling back to the unbounded whole-file scan

**Files:**
- Modify: `internal/metadata/pdf.go`
- Test: `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: `renderPDFPageAsCoverFunc` (pre-existing seam, unchanged).
- Produces: no new function; `findPDFCoverPageAware`'s existing public signature is unchanged, only its internal fallback ordering gains one more tier.

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/metadata/pdf_test.go`, after the existing `TestFindPDFCoverPageAware_ResolvesCoverWhenKidsIsIndirectReference` test (Task 1's test, which by this point is already the last test in that section of the file):

```go
func TestFindPDFCoverPageAware_RendersPage1WhenNoImageSignalFoundAtAll(t *testing.T) {
	// Reproduces the real "Distributed Systems Principles and
	// Paradigms"/"Effective Sprint Planning"/"Cleaner Python"/"AI Product
	// Management"/"Build Your Own Database From Scratch" bug: these
	// books' real cover art is entirely vector-drawn (no image XObject
	// anywhere within the page limit, and so no undecodable-image page
	// either), so both earlier fallback tiers find nothing. Previously
	// this fell all the way through to findPDFCover's unbounded
	// whole-file scan, which -- when the file has interior diagram
	// images elsewhere, as a real technical book does -- returned an
	// arbitrary interior image instead of a graceful best-effort
	// rendering of the real title page.
	orig := renderPDFPageAsCoverFunc
	defer func() { renderPDFPageAsCoverFunc = orig }()

	called := false
	var calledWithPage int
	renderPDFPageAsCoverFunc = func(data []byte, pageNum int) ([]byte, string, bool) {
		called = true
		calledWithPage = pageNum
		return []byte("RENDERED-PNG-BYTES"), "image/png", true
	}

	// Page 1 has no /Resources /XObject entry at all -- no image,
	// decodable or otherwise. An unrelated image exists elsewhere in the
	// file (object 9, not referenced by any page in the tree) standing
	// in for a real book's interior diagram, to prove the whole-file
	// scan is NOT what produced this test's result.
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n" +
			"9 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n\xFF\xD8\xFFfakejpegbytes\nendstream\nendobj\n")

	imageBytes, contentType, ok := findPDFCoverPageAware(data, 10)
	if !ok {
		t.Fatal("findPDFCoverPageAware ok=false, want true")
	}
	if !called {
		t.Fatal("renderPDFPageAsCoverFunc was not called; want a page-1 full render when no image signal exists anywhere within the page limit")
	}
	if calledWithPage != 1 {
		t.Errorf("called with page %d, want 1", calledWithPage)
	}
	if string(imageBytes) != "RENDERED-PNG-BYTES" || contentType != "image/png" {
		t.Errorf("got %q/%q, want the rendered stand-in bytes/content-type", imageBytes, contentType)
	}
}

func TestFindPDFCoverPageAware_FallsBackToWholeFileScanWhenPage1RenderFails(t *testing.T) {
	// If PDFium itself can't render page 1 (e.g. a truly pathological
	// page), the whole-file scan remains the final safety net -- same
	// contract as the other two render tiers above it.
	orig := renderPDFPageAsCoverFunc
	defer func() { renderPDFPageAsCoverFunc = orig }()

	renderPDFPageAsCoverFunc = func(data []byte, pageNum int) ([]byte, string, bool) {
		return nil, "", false
	}

	jpegData := []byte("\xFF\xD8\xFFfallbackjpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n" +
			"9 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 20 >>\nstream\n" + string(jpegData) + "\nendstream\nendobj\n")

	imageBytes, contentType, ok := findPDFCoverPageAware(data, 10)
	if !ok {
		t.Fatal("findPDFCoverPageAware ok=false, want true (whole-file fallback should still find the DCTDecode image)")
	}
	if contentType != "image/jpeg" || string(imageBytes) != string(jpegData) {
		t.Errorf("got %q/%q, want the whole-file-scan-found DCTDecode image", imageBytes, contentType)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/ -run 'TestFindPDFCoverPageAware_RendersPage1WhenNoImageSignalFoundAtAll|TestFindPDFCoverPageAware_FallsBackToWholeFileScanWhenPage1RenderFails' -v`
Expected: `TestFindPDFCoverPageAware_RendersPage1WhenNoImageSignalFoundAtAll` FAILS with `renderPDFPageAsCoverFunc was not called` (the current code falls straight to `findPDFCover`'s whole-file scan, finding object 9's image instead of ever attempting a page-1 render). `TestFindPDFCoverPageAware_FallsBackToWholeFileScanWhenPage1RenderFails` currently PASSES already (the whole-file scan already finds object 9 today, just via a different path) -- confirm it stays passing after Step 3, for the right reason (re-read its assertions against the new code path once Step 3 lands).

- [ ] **Step 3: Add the page-1 render fallback tier**

In `internal/metadata/pdf.go`, change:

```go
func findPDFCoverPageAware(data []byte, pageLimit int) ([]byte, string, bool) {
	idx := buildPDFObjIndex(data)
	if pages, ok := walkPDFPageTree(idx, pageLimit); ok {
		if images := findPDFPageImages(idx, pages, true); len(images) > 0 {
			img := images[0]
			if page, found := findPageByNumber(pages, img.page); found && (pageContentSuggestsCompositeCover(idx, page) || pageHasMultipleImages(idx, page)) {
				if renderedBytes, renderedContentType, ok := renderPDFPageAsCoverFunc(data, img.page); ok {
					return renderedBytes, renderedContentType, true
				}
			}
			return img.bytes, img.contentType, true
		}
		if pageNum, ok := findFirstPageWithUndecodableImage(idx, pages); ok {
			if renderedBytes, renderedContentType, ok := renderPDFPageAsCoverFunc(data, pageNum); ok {
				return renderedBytes, renderedContentType, true
			}
		}
	}
	return findPDFCover(idx, data)
}
```

to:

```go
func findPDFCoverPageAware(data []byte, pageLimit int) ([]byte, string, bool) {
	idx := buildPDFObjIndex(data)
	if pages, ok := walkPDFPageTree(idx, pageLimit); ok {
		if images := findPDFPageImages(idx, pages, true); len(images) > 0 {
			img := images[0]
			if page, found := findPageByNumber(pages, img.page); found && (pageContentSuggestsCompositeCover(idx, page) || pageHasMultipleImages(idx, page)) {
				if renderedBytes, renderedContentType, ok := renderPDFPageAsCoverFunc(data, img.page); ok {
					return renderedBytes, renderedContentType, true
				}
			}
			return img.bytes, img.contentType, true
		}
		if pageNum, ok := findFirstPageWithUndecodableImage(idx, pages); ok {
			if renderedBytes, renderedContentType, ok := renderPDFPageAsCoverFunc(data, pageNum); ok {
				return renderedBytes, renderedContentType, true
			}
		}
		// Neither tier above found anything at all within the page
		// limit: no image XObject on any scanned page (decodable or
		// not). Some real covers are entirely vector-drawn (text and
		// line art, no raster image whatsoever) -- rendering page 1 in
		// full recovers exactly that cover, rather than falling all the
		// way through to findPDFCover's unbounded whole-file scan, which
		// has no notion of "page 1" and can return an arbitrary interior
		// image (e.g. a chapter diagram) instead.
		if renderedBytes, renderedContentType, ok := renderPDFPageAsCoverFunc(data, pages[0].number); ok {
			return renderedBytes, renderedContentType, true
		}
	}
	return findPDFCover(idx, data)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the two new ones and every pre-existing test -- in particular `TestFindPDFCoverPageAware_FallsBackToWholeFileScanWhenUndecodableImageRenderFails`, whose mocked `renderPDFPageAsCoverFunc` now gets called an extra time by this new tier but still returns `ok=false` every time, so its final assertion -- that the whole-file scan's DCTDecode image comes back -- is unaffected).

- [ ] **Step 5: Bump CoverExtractorVersion**

In `internal/metadata/extractor.go`, change:

```go
const CoverExtractorVersion = 8 // bumped: composite-cover detection now recurses into a page's Form XObjects to find a text-show operator nested inside one (previously only the page's own /Contents stream was checked directly), so a page that wraps its whole visual content in a single top-level Form no longer returns just its lone embedded image instead of the full composited page.
```

to:

```go
const CoverExtractorVersion = 9 // bumped: when no image (decodable or otherwise) is found anywhere within the page limit, findPDFCoverPageAware now renders page 1 in full before falling back to findPDFCover's unbounded whole-file scan -- recovering an entirely vector-drawn cover instead of returning an arbitrary interior image (or nothing) for books with no raster cover art at all.
```

- [ ] **Step 6: Run tests to verify they still pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_test.go internal/metadata/extractor.go
git commit -m "Render page 1 in full before falling back to the whole-file cover scan"
```

---

## Manual Verification (after all tasks complete)

1. Run `go build ./... && go vet ./... && go test ./...` at the repo root, and `go test -race ./internal/metadata/...` to confirm the whole module still builds, vets clean, and every test passes (race-clean) end to end.
2. In the running desktop app (`cd desktop && ./build.sh`, or via the dev flow), click "Reset Cover Cache" in Settings (forces every book to be re-extracted under the new `CoverExtractorVersion`/`MetadataExtractorVersion`), then refresh the Library view and confirm each of these real files -- every one independently confirmed during root-cause investigation to have a genuine, correctly-designed cover once the right page is rendered -- now shows its real cover, not the previously-wrong one:
   - `/media/francis/Data1/Books/Library/Technology/AI/Designing for AI (2026) - Arash Sadr.pdf` (Task 1: was the O'Reilly logo)
   - `/media/francis/Data1/Books/Library/Technology/Architecture/Grokking Software Architecture (2026) - Matt Erman.pdf` (Task 1: was the Manning "M" logo)
   - `/media/francis/Data1/Books/Library/Technology/Real Time/Grokking Streaming Systems (2024) - Josh Fischer & Ning Wang.pdf` (Task 1: was an interior diagram)
   - `/media/francis/Data1/Books/Library/Technology/Golang/Build Your Own Database From Scratch in Go From B Tree To SQL in 3000 Lines (2024) - James Smith.pdf` (Tasks 2 + 5: garbled Title/Author, and no cover)
   - `/media/francis/Data1/Books/Library/Technology/Product/AI Product Management (2025) - Nilesh Dhage.pdf` (Tasks 2 + 5: garbled Title, and no cover)
   - `/media/francis/Data1/Books/Library/Technology/Scala/Practical FP in Scala (2020) - Gabriel Volpe.pdf` (Tasks 2 + 3: garbled Title/Author, and a cropped background-texture cover)
   - `/media/francis/Data1/Books/Library/Technology/Consulting/The New Consultants Quick Start Guide.pdf` (Task 3: was a near-black cropped layer)
   - `/media/francis/Data1/Books/Library/Technology/Distributed Systems/Understanding Distributed Systems - 2nd Edition (2022) - Roberto Vitillo.pdf` (Task 4: was just the lone embedded stock photo)
   - `/media/francis/Data1/Books/Library/Technology/Distributed Systems/Distributed Systems Principles and Paradigms-Maarten van Steen (2023) - Maarten van Steen, Andrew S Tanenbaum.pdf` (Task 5: was an unrelated interior diagram)
   - `/media/francis/Data1/Books/Library/Technology/Leadership/Effective Sprint Planning (2021) - Clayton Lengel-Zigich.pdf` (Task 5: had no cover)
   - `/media/francis/Data1/Books/Library/Technology/Python/Cleaner Python - Ezzeddin Abdullah.pdf` (Task 5: had no cover)
3. Confirm the following books are **unaffected by this plan on purpose** (per root-cause investigation, they have no cover art embedded at all -- only a small platform logo -- so the plain title-page rendering they already show is the correct best-effort result, not a bug): every other Leanpub-published book in the original bad-covers list (Auth Considerations for Kubernetes, Identity/Authentication and Gaming, The Modern Guide to OAuth, The Ultimate Guide to Outsourcing Your Auth, High Performance Applications, Atomic Kotlin, Talking with Tech Leads, Digital Products Leadership, both Wardley Mapping books, Breaking down JSON Web Tokens), plus *Team Guide to Metrics for Business Decisions* (its own actual page 1 is a "more in this series" promo page, not a malformed extraction).
