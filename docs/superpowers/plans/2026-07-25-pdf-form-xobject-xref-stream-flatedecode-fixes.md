# PDF Form XObject Recursion, XRef-Stream Metadata Lookup, FlateDecode Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix three confirmed, independent bugs in `internal/metadata`'s PDF scanner that together caused "Programming with Types (2019) - Vlad Riscutia.pdf" to show no cover and no Title/Author: cover images nested inside Form XObjects aren't found, Title/Author can't be located in PDF 1.5+ cross-reference-stream files, and the whole-file fallback scanner only recognizes JPEG images.

**Architecture:** Each bug gets a small, targeted fix to one existing function, following that function's existing regex/index-scanning conventions (no rewrite, no new abstraction layer). A new `metadata.MetadataExtractorVersion` constant (parallel to the existing `CoverExtractorVersion`) plus a `librarycache.Entry.MetadataVersion` field let the Title/Author fix self-heal already-cached books, the same way `CoverVersion` already does for covers.

**Tech Stack:** Go, this package's existing dependency-free regex/byte-scanning PDF parser (no new dependencies).

## Global Constraints

- Form XObject recursion (Bug A fix) is capped at 4 levels of nesting, with a `visited` set of already-seen object numbers guarding against a malformed/cyclic PDF (mirrors `collectPDFPages`' existing Kids-cycle guard in `internal/metadata/pdf_pages.go`).
- The XRef-stream Info-dict fallback (Bug B fix) takes the *last* matching `/Type /XRef` object in file order, mirroring `findInfoDictBody`'s existing "last trailer wins" handling for incrementally-updated files.
- The FlateDecode whole-file fallback (Bug C fix) passes `resources=nil` to `decodeFlatePDFImage` — inline/indirect colorspaces resolve fine; a named colorspace requiring Resources-dict lookup fails gracefully (image skipped), an accepted limitation since this path has no page context to scope Resources from.
- `metadata.CoverExtractorVersion` bumps from 2 to 3 once (covering both Bug A and Bug C, which both change cover-selection bytes for already-cached books) — not twice.
- `metadata.MetadataExtractorVersion` is new, starts at 1 (Bug B only affects Title/Author, tracked independently of `CoverExtractorVersion` so a future Title/Author-only change doesn't force needless cover re-extraction, and vice versa).
- No new external dependencies; no changes to `pageContentSuggestsCompositeCover` or the PDFium full-page-render path (already correct once Bug A's lookup gap is fixed).

---

### Task 1: Recurse into Form XObjects when finding page cover images (Bug A)

**Files:**
- Modify: `internal/metadata/pdf_images.go`
- Test: `internal/metadata/pdf_images_test.go`

**Interfaces:**
- Consumes: `resolveDictValue`, `pdfObjIndex.lookup`, `splitPDFObjectBody`, `decodePDFImageStream`, `pdfSubtypeImageRe` (all pre-existing, unchanged).
- Produces: `findPDFPageImages(idx *pdfObjIndex, pages []pdfPage, stopAtFirst bool) []pdfPageImage` — same signature as before, callers (`findPDFCoverPageAware` in pdf.go, `ListPDFCoverCandidates`/`ExtractPDFPageCover` in pdf_override.go) need no changes. New unexported `pdfSubtypeFormRe` regex and `findImagesInXObjects` helper, for Task 2's and Task 3's awareness only (neither task touches this file).

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/metadata/pdf_images_test.go`, after the existing `TestFindPDFPageImages_StopAtFirstFalseCollectsAll` test:

```go
func TestFindPDFPageImages_FindsImageNestedInsideFormXObject(t *testing.T) {
	// Reproduces the real "Programming with Types" bug: the page's own
	// XObject entry is a Form (/Subtype /Form), and the actual image is
	// nested inside THAT form's own Resources/XObject, not directly on
	// the page.
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Fm1 10 0 R >> >> >>\nendobj\n" +
			"10 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Im0 11 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"11 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, true)
	if len(images) != 1 {
		t.Fatalf("len(images) = %d, want 1 (image nested inside the page's Form XObject)", len(images))
	}
	if string(images[0].bytes) != string(jpegData) {
		t.Errorf("images[0].bytes = %q, want %q", images[0].bytes, jpegData)
	}
}

func TestFindPDFPageImages_FindsImageFourFormsDeep(t *testing.T) {
	// Four chained Form XObjects (Fm1 -> Fm2 -> Fm3 -> Fm4), with the
	// image directly inside Fm4's own Resources -- exactly at the 4-level
	// depth cap, must still be found.
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Fm1 10 0 R >> >> >>\nendobj\n" +
			"10 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm2 11 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"11 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm3 12 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"12 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm4 13 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"13 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Im0 14 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"14 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, true)
	if len(images) != 1 {
		t.Fatalf("len(images) = %d, want 1 (image exactly 4 forms deep, at the depth cap)", len(images))
	}
}

func TestFindPDFPageImages_DoesNotFindImageFiveFormsDeep(t *testing.T) {
	// Same shape as the four-forms-deep test, but with one more Form
	// (Fm5) in the chain -- the image is now 5 forms deep and must NOT
	// be found (depth cap enforced, protecting against pathological
	// nesting).
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Fm1 10 0 R >> >> >>\nendobj\n" +
			"10 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm2 11 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"11 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm3 12 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"12 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm4 13 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"13 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm5 14 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"14 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Im0 15 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"15 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, true)
	if len(images) != 0 {
		t.Fatalf("len(images) = %d, want 0 (image 5 forms deep, past the depth cap)", len(images))
	}
}

func TestFindPDFPageImages_FormCycleDoesNotHang(t *testing.T) {
	// Fm1 references Fm2, which references Fm1 back -- a malformed
	// cyclic PDF. The visited-set guard must stop the recursion; test
	// completing at all (without hanging) is proof it worked.
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Fm1 10 0 R >> >> >>\nendobj\n" +
			"10 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm2 11 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"11 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm1 10 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, false)
	if len(images) != 0 {
		t.Fatalf("len(images) = %d, want 0 (pure cycle, no images anywhere)", len(images))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/ -run TestFindPDFPageImages -v`
Expected: the four new tests FAIL. `TestFindPDFPageImages_FindsImageNestedInsideFormXObject` and `TestFindPDFPageImages_FindsImageFourFormsDeep` fail with `len(images) = 0, want 1` (Form XObjects are currently skipped entirely). `TestFindPDFPageImages_DoesNotFindImageFiveFormsDeep` and `TestFindPDFPageImages_FormCycleDoesNotHang` already pass trivially today (0 images found, since Forms are skipped) — that's expected; they'll stay green once implemented and exist to lock in the depth-cap/cycle behavior going forward, not to prove a regression right now.

- [ ] **Step 3: Implement Form XObject recursion**

Replace the whole of `internal/metadata/pdf_images.go` with:

```go
// Per-page image enumeration for PDF cover selection: given an ordered
// page list from pdf_pages.go, finds qualifying image XObjects on each
// page in turn -- recursing into Form XObjects (/Subtype /Form) a page
// references, since real-world PDFs (particularly prepress/OPI-workflow
// output) commonly nest the actual cover image one or more levels inside
// a Form rather than directly in the page's own Resources.
package metadata

import (
	"bytes"
	"regexp"
	"strconv"
)

// pdfPageImage is one candidate cover image found while walking the page
// tree: which page (1-based, matching pdfPage.number) it came from, plus
// its already-decoded, display-ready bytes.
type pdfPageImage struct {
	page        int
	bytes       []byte
	contentType string
}

var pdfXObjectEntryRe = regexp.MustCompile(`/\w+\s+(\d+)\s+\d+\s+R`)
var pdfSubtypeFormRe = regexp.MustCompile(`/Subtype\s*/Form`)

// maxFormXObjectDepth caps how many levels of Form XObject nesting
// findImagesInXObjects will recurse into. A real cover image is 1 level
// deep (a single Form wrapping the image, common in prepress/OPI
// workflows); 4 is generous headroom without risking runaway recursion
// on a malformed/pathologically-nested PDF. Combined with the visited
// set (also passed to findImagesInXObjects), a cyclic Form reference
// terminates immediately regardless of this cap.
const maxFormXObjectDepth = 4

// findPDFPageImages returns every qualifying image found across pages (in
// page, then XObject, order). When stopAtFirst is true, the walk returns
// as soon as one qualifying image is found -- the normal auto-detect
// path used by findPDFCoverPageAware (pdf.go). A later plan's override
// picker calls this with stopAtFirst=false to collect every candidate for
// its thumbnail grid.
func findPDFPageImages(idx *pdfObjIndex, pages []pdfPage, stopAtFirst bool) []pdfPageImage {
	var found []pdfPageImage
	for _, p := range pages {
		resources, ok := resolveDictValue(idx, p.dict, "Resources")
		if !ok {
			continue
		}
		xobjects, ok := resolveDictValue(idx, resources, "XObject")
		if !ok {
			continue
		}
		visited := map[int]bool{}
		if findImagesInXObjects(idx, p.number, resources, xobjects, 0, visited, stopAtFirst, &found) {
			return found
		}
	}
	return found
}

// findImagesInXObjects scans xobjects (a page's or a Form XObject's own
// /XObject dict) for qualifying images, recursing into any /Subtype
// /Form entries found up to maxFormXObjectDepth levels. visited guards
// against a malformed/cyclic Form reference (shared across the whole
// recursion tree for one page, the same way collectPDFPages' visited map
// guards Kids cycles in pdf_pages.go). Found images are appended to
// *found, tagged with pageNumber -- the page they were ultimately
// reached from, regardless of how many Form levels deep they were
// nested, since pdfPageImage's contract is "which page to show this
// cover for," not "which object declared it." Returns true once
// stopAtFirst is satisfied, signalling the caller to stop walking
// further pages too.
func findImagesInXObjects(idx *pdfObjIndex, pageNumber int, resources, xobjects []byte, depth int, visited map[int]bool, stopAtFirst bool, found *[]pdfPageImage) bool {
	for _, ref := range pdfXObjectEntryRe.FindAllSubmatch(xobjects, -1) {
		objNum, err := strconv.Atoi(string(ref[1]))
		if err != nil {
			continue
		}
		if visited[objNum] {
			continue
		}
		body, ok := idx.lookup(objNum)
		if !ok {
			continue
		}
		dict, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream {
			continue
		}

		if pdfSubtypeImageRe.Match(dict) {
			data, contentType, ok := decodePDFImageStream(idx, resources, dict, stream)
			if !ok {
				continue
			}
			*found = append(*found, pdfPageImage{page: pageNumber, bytes: data, contentType: contentType})
			if stopAtFirst {
				return true
			}
			continue
		}

		if pdfSubtypeFormRe.Match(dict) && depth < maxFormXObjectDepth {
			formResources, ok := resolveDictValue(idx, dict, "Resources")
			if !ok {
				continue
			}
			formXObjects, ok := resolveDictValue(idx, formResources, "XObject")
			if !ok {
				continue
			}
			visited[objNum] = true
			if findImagesInXObjects(idx, pageNumber, formResources, formXObjects, depth+1, visited, stopAtFirst, found) {
				return true
			}
		}
	}
	return false
}

// decodePDFImageStream turns an image XObject's raw stream bytes into
// display-ready image bytes. DCTDecode streams are already a complete
// JPEG file and pass through unchanged. FlateDecode streams are
// reconstructed via decodeFlatePDFImage (pdf_flate.go) -- predictor undo,
// colorspace mapping, and PNG re-encoding. Any other filter (or a
// FlateDecode image this package can't fully resolve) returns ok=false.
func decodePDFImageStream(idx *pdfObjIndex, resources, dict, stream []byte) (data []byte, contentType string, ok bool) {
	if pdfDCTDecodeRe.Match(dict) {
		// splitPDFObjectBody may leave "endstream" in the stream if there's a
		// trailing newline before it, so we trim it here and then any remaining
		// whitespace.
		trimmed := bytes.TrimSuffix(stream, []byte("endstream"))
		trimmed = bytes.TrimRight(trimmed, "\r\n")
		if len(trimmed) == 0 {
			return nil, "", false
		}
		return trimmed, "image/jpeg", true
	}
	return decodeFlatePDFImage(idx, resources, dict, stream)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/ -v`
Expected: PASS (all tests in the package, including the four new ones and every pre-existing `findPDFPageImages`/`findPDFCoverPageAware`/`extractPDF` test — this task doesn't change the found-image byte format or the `resources` scoping used for colorspace resolution, only which XObject entries get inspected).

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_images.go internal/metadata/pdf_images_test.go
git commit -m "Recurse into Form XObjects when finding page cover images"
```

---

### Task 2: Decode FlateDecode images in the whole-file cover fallback (Bug C)

**Files:**
- Modify: `internal/metadata/pdf.go`
- Test: `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: `decodeFlatePDFImage(idx *pdfObjIndex, resources, dict, stream []byte) (data []byte, contentType string, ok bool)` (pre-existing, unchanged, from `internal/metadata/pdf_flate.go`), `buildPDFObjIndex` (pre-existing).
- Produces: `findPDFCover(idx *pdfObjIndex, data []byte) ([]byte, string, bool)` — signature changes (gains the `idx` parameter). Its one production caller, `findPDFCoverPageAware` (same file), is updated in this task. Task 3 (Bug B) does not call `findPDFCover` and is unaffected by this signature change.

- [ ] **Step 1: Write the failing test**

Add this test to `internal/metadata/pdf_test.go`, after the existing `TestExtractPDF_PageAwareCoverPrefersPageOrderOverByteOrder` test:

```go
func TestExtractPDF_WholeFileFallbackDecodesFlateDecodeImage(t *testing.T) {
	// No page tree at all (no /Type /Catalog), forcing findPDFCoverPageAware
	// to fall through to findPDFCover's whole-file scan -- which must now
	// recognize a FlateDecode image, not just DCTDecode/JPEG.
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write([]byte{0x10, 0x20, 0x30}); err != nil { // 1x1 RGB pixel
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	pdf := "%PDF-1.4\n" +
		"1 0 obj\n<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /Filter /FlateDecode /Length " +
		strconv.Itoa(compressed.Len()) + " >>\nstream\n" + compressed.String() + "\nendstream\nendobj\n"
	path := writePDFFixture(t, pdf)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if len(result.CoverBytes) == 0 {
		t.Fatal("CoverBytes is empty, want the decoded FlateDecode image")
	}
	if result.CoverContentType != "image/png" {
		t.Errorf("CoverContentType = %q, want image/png (FlateDecode images are re-encoded as PNG)", result.CoverContentType)
	}
}
```

Add `"compress/zlib"` and `"strconv"` to `internal/metadata/pdf_test.go`'s import block (it currently imports `bytes`, `os`, `path/filepath`, `testing`, `unicode/utf16`, `unicode/utf8`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/ -run TestExtractPDF_WholeFileFallbackDecodesFlateDecodeImage -v`
Expected: FAIL with `CoverBytes is empty, want the decoded FlateDecode image` (`findPDFCover` currently only recognizes `/Filter /DCTDecode`).

- [ ] **Step 3: Implement FlateDecode support in the whole-file fallback**

In `internal/metadata/pdf.go`, replace `findPDFCover`'s doc comment and body (lines 127-149):

```go
// findPDFCover scans data for the first qualifying image XObject stream
// (a "<<...>>\nstream ... endstream" block whose dictionary declares
// /Subtype /Image) and returns its display-ready bytes: a /Filter
// /DCTDecode stream's raw bytes are already a complete, valid JPEG, per
// the PDF spec; any other filter is attempted via decodeFlatePDFImage
// (pdf_flate.go), passing resources=nil since this whole-file scan has
// no page context to resolve a named colorspace against -- inline and
// indirect colorspaces (the common case) still resolve fine. This is a
// textual scan, not a real PDF parser: it does not handle a dictionary
// containing its own nested "<<...>>" (e.g. a /DecodeParms
// sub-dictionary), matching the rest of this file's deliberately
// best-effort approach.
func findPDFCover(idx *pdfObjIndex, data []byte) ([]byte, string, bool) {
	for _, m := range pdfImageStreamRe.FindAllSubmatch(data, -1) {
		dict := m[1]
		if !pdfSubtypeImageRe.Match(dict) {
			continue
		}
		stream := bytes.TrimRight(m[2], "\r\n")
		if len(stream) == 0 {
			continue
		}
		if pdfDCTDecodeRe.Match(dict) {
			return stream, "image/jpeg", true
		}
		if flateBytes, contentType, ok := decodeFlatePDFImage(idx, nil, dict, stream); ok {
			return flateBytes, contentType, true
		}
	}
	return nil, "", false
}
```

Update `findPDFCoverPageAware`'s single call site (the last line of the function) from:

```go
	return findPDFCover(data)
```

to:

```go
	return findPDFCover(idx, data)
```

(`idx` is already in scope — it's built at the top of `findPDFCoverPageAware` via `idx := buildPDFObjIndex(data)`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/ -v`
Expected: PASS (all tests, including the new one and every pre-existing `findPDFCover`/`findPDFCoverPageAware`/`extractPDF` test — DCTDecode behavior is unchanged, only the fallback for non-DCTDecode images is new).

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_test.go
git commit -m "Decode FlateDecode images in the whole-file cover fallback scan"
```

---

### Task 3: Locate the Info dictionary via PDF 1.5+ cross-reference streams (Bug B)

**Files:**
- Modify: `internal/metadata/pdf.go`
- Test: `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: `buildPDFObjIndex`, `splitPDFObjectBody`, `pdfInfoRefRe` (pre-existing, unchanged).
- Produces: `findInfoDictBody(data []byte) ([]byte, bool)` — same signature as before, no callers need changes. New unexported `pdfXRefTypeRe` regex and `findXRefStreamTrailerDict` helper, for no other task's awareness (this task is independent of Tasks 1 and 2).

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/metadata/pdf_test.go`, after the existing `TestExtractPDF_UsesLatestIncrementalUpdateOfInfoObject` test:

```go
const testPDFFixtureXRefStreamTrailer = `%PDF-1.5
3 0 obj
<< /Title (XRef Stream Book) /Author (Jane Author) /CreationDate (D:20220101000000) >>
endobj
20 0 obj
<< /Type /XRef /Info 3 0 R /Root 4 0 R /Size 21 /W [1 1 1] /Length 3 >>
stream
abc
endstream
endobj
%%EOF`

func TestExtractPDF_FindsInfoDictViaXRefStreamWhenNoClassicTrailer(t *testing.T) {
	path := writePDFFixture(t, testPDFFixtureXRefStreamTrailer)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "XRef Stream Book" {
		t.Errorf("Title = %q, want %q (Info dict located via the XRef stream's own /Info entry, not a classic trailer)", result.Title, "XRef Stream Book")
	}
	if result.Author != "Jane Author" {
		t.Errorf("Author = %q, want %q", result.Author, "Jane Author")
	}
	if result.Year != "2022" {
		t.Errorf("Year = %q, want 2022", result.Year)
	}
}

const testPDFFixtureMultipleXRefStreams = `%PDF-1.5
3 0 obj
<< /Title (Old Title) /Author (Old Author) >>
endobj
5 0 obj
<< /Title (New Title) /Author (New Author) >>
endobj
20 0 obj
<< /Type /XRef /Info 3 0 R /Root 4 0 R /Size 21 /W [1 1 1] /Length 3 >>
stream
abc
endstream
endobj
%%EOF
21 0 obj
<< /Type /XRef /Info 5 0 R /Root 4 0 R /Size 22 /W [1 1 1] /Length 3 >>
stream
def
endstream
endobj
%%EOF`

func TestExtractPDF_UsesLastXRefStreamWhenMultiplePresent(t *testing.T) {
	// Simulates an incrementally-updated PDF using cross-reference
	// streams: two /Type /XRef objects present, pointing /Info at TWO
	// DIFFERENT objects (20 -> object 3 "Old Title", the earlier one; 21
	// -> object 5 "New Title", the later one). Deliberately different
	// targets (not the same object rewritten) so this test actually
	// distinguishes "last XRef wins" from "first XRef wins" -- if the
	// code picked the first /Type /XRef match instead of the last, it
	// would resolve to the old title instead.
	path := writePDFFixture(t, testPDFFixtureMultipleXRefStreams)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "New Title" {
		t.Errorf("Title = %q, want %q (the latest incremental update, via the latest XRef stream)", result.Title, "New Title")
	}
	if result.Author != "New Author" {
		t.Errorf("Author = %q, want %q", result.Author, "New Author")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/ -run TestExtractPDF_FindsInfoDictViaXRefStream -v` and `go test ./internal/metadata/ -run TestExtractPDF_UsesLastXRefStreamWhenMultiplePresent -v`
Expected: both FAIL with `Title = "", want "XRef Stream Book"` / `Title = "", want "New Title"` (no classic `trailer` keyword exists in either fixture, so `findInfoDictBody` currently returns `ok=false` and Title/Author stay empty).

- [ ] **Step 3: Implement the XRef-stream fallback**

In `internal/metadata/pdf.go`, add `"sort"` to the import block (currently `bytes`, `os`, `regexp`, `strings`, `unicode/utf16`):

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

Add a new regex next to the existing `pdfInfoRefRe` declaration (line 16):

```go
var pdfXRefTypeRe = regexp.MustCompile(`/Type\s*/XRef\b`)
```

Replace `findInfoDictBody`'s doc comment and body (lines 88-125) with:

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
// one is used -- and, for the same "most recent update wins" reason, if
// object N itself was rewritten by an incremental update (common for
// PDFs edited by annotation/signing/metadata tools, which append rather
// than rewrite), the LAST "N ... obj ... endobj" block in the file is
// used too, not the first (now-superseded) one. If no classic "trailer
// <<...>>" keyword block exists at all, falls back to
// findXRefStreamTrailerDict for PDFs using a PDF 1.5+ cross-reference
// stream instead (which carries the same /Info key directly in its own
// dictionary). Returns ok=false (caller falls back to a whole-file scan)
// if neither a trailer nor an XRef stream trailer-equivalent, no /Info
// reference, or no matching object is found -- preserving prior
// best-effort behavior for atypical PDFs rather than erroring.
func findInfoDictBody(data []byte) ([]byte, bool) {
	var trailerDict []byte
	trailers := pdfTrailerRe.FindAllSubmatch(data, -1)
	if len(trailers) > 0 {
		trailerDict = trailers[len(trailers)-1][1]
	} else {
		trailerDict = findXRefStreamTrailerDict(data)
		if trailerDict == nil {
			return nil, false
		}
	}
	infoMatch := pdfInfoRefRe.FindSubmatch(trailerDict)
	if infoMatch == nil {
		return nil, false
	}
	objNum := string(infoMatch[1])
	objRe := regexp.MustCompile(`(?s)\b` + objNum + `\s+\d+\s+obj(.*?)endobj`)
	objMatches := objRe.FindAllSubmatch(data, -1)
	if objMatches == nil {
		return nil, false
	}
	return objMatches[len(objMatches)-1][1], true
}

// findXRefStreamTrailerDict locates the trailer-equivalent dict for PDFs
// using a PDF 1.5+ cross-reference stream instead of a classic "trailer
// <<...>>" keyword block: the XRef stream object (/Type /XRef) carries
// the same /Root, /Info, /Size keys directly in its own dictionary, per
// the PDF spec. Returns the LAST such object's dict in file order
// (mirroring findInfoDictBody's "most recent update wins" handling for
// classic trailers, since a later XRef stream supersedes an earlier
// one), or nil if no /Type /XRef object exists anywhere in data.
func findXRefStreamTrailerDict(data []byte) []byte {
	idx := buildPDFObjIndex(data)
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

Run: `go test ./internal/metadata/ -v`
Expected: PASS (all tests, including the two new ones and every pre-existing `findInfoDictBody`/`extractPDF` metadata test — in particular `TestExtractPDF_NoTrailerLeavesTitleAuthorEmptyButStillFindsYear`, whose fixture has no `trailer` keyword AND no `/Type /XRef` object, so `findXRefStreamTrailerDict` correctly returns nil there too, preserving that test's existing "Title/Author stay empty" expectation).

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_test.go
git commit -m "Locate the Info dictionary via PDF 1.5+ cross-reference streams"
```

---

### Task 4: Version bumps and cache invalidation wiring

**Files:**
- Modify: `internal/metadata/extractor.go`
- Modify: `internal/librarycache/librarycache.go`
- Modify: `internal/librarian/librarian.go`
- Test: `internal/librarian/librarian_test.go`

**Interfaces:**
- Consumes: `metadata.CoverExtractorVersion` (existing), adds `metadata.MetadataExtractorVersion` (new const, consumed by `librarian.go`). Consumes `librarycache.Entry` (existing struct, gains a field).
- Produces: nothing consumed by a later task (this is the last task).

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/librarian/librarian_test.go`, after the existing `TestScan_ReExtractedEntryIsCachedWithCurrentCoverVersion` test:

```go
func TestScan_StaleMetadataVersionForcesReExtractionDespiteMatchingCoverVersion(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"))
	info, _ := os.Stat(path)

	// Simulates an entry cached before the XRef-stream Info-dict fix:
	// ModTime, Size, and CoverVersion all match (the book file and its
	// cover-extraction logic haven't changed), but MetadataVersion is
	// stale (0, the zero value any pre-this-fix persisted entry
	// unmarshals to).
	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{
		ModTime: info.ModTime(), Size: info.Size(),
		Title: "Stale Title", CoverVersion: metadata.CoverExtractorVersion, MetadataVersion: 0,
	})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	called := false
	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		called = true
		return metadata.Result{Title: "Fresh Title"}, nil
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !called {
		t.Error("extractFunc was not called for a MetadataVersion-stale entry despite matching ModTime/Size/CoverVersion -- a book cached under an old metadata-extraction algorithm would be stuck forever")
	}
	if len(books) != 1 || books[0].Title != "Fresh Title" {
		t.Errorf("books = %+v, want [{Title: Fresh Title}]", books)
	}
}

func TestScan_ReExtractedEntryIsCachedWithCurrentMetadataVersion(t *testing.T) {
	libDir := t.TempDir()
	logDir := t.TempDir()
	epubPath := filepath.Join(libDir, "Fiction", "Sci-Fi", "Foundation.epub")
	writeEpubWithCover(t, epubPath, []byte{0xFF, 0xD8, 0xFF, 0xE0})

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	if _, err := Scan(cfg, false); err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	reloaded := librarycache.Load(logDir)
	info, err := os.Stat(epubPath)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}
	entry, ok := reloaded.Fresh(epubPath, info.ModTime(), info.Size())
	if !ok {
		t.Fatal("Fresh() = false after a fresh Scan wrote the entry, want true")
	}
	if entry.MetadataVersion != metadata.MetadataExtractorVersion {
		t.Errorf("MetadataVersion = %d, want %d (metadata.MetadataExtractorVersion)", entry.MetadataVersion, metadata.MetadataExtractorVersion)
	}
}
```

Update the two existing tests that construct a cache `Entry` expecting a genuine cache HIT — they need `MetadataVersion: metadata.MetadataExtractorVersion` added, or the new field's zero value will make them incorrectly behave as a cache miss:

In `TestScan_UsesCachedFieldsAndSkipsExtractOnCacheHit` (around line 286), change:

```go
	cache.Put(path, librarycache.Entry{
		ModTime: info.ModTime(), Size: info.Size(),
		Title: "Foundation", Author: "Isaac Asimov", Year: "1951",
		Category: "Fiction", Subcategory: "Sci-Fi", CoverPath: "/covers/abc.jpg",
		CoverVersion: metadata.CoverExtractorVersion,
	})
```

to:

```go
	cache.Put(path, librarycache.Entry{
		ModTime: info.ModTime(), Size: info.Size(),
		Title: "Foundation", Author: "Isaac Asimov", Year: "1951",
		Category: "Fiction", Subcategory: "Sci-Fi", CoverPath: "/covers/abc.jpg",
		CoverVersion: metadata.CoverExtractorVersion, MetadataVersion: metadata.MetadataExtractorVersion,
	})
```

In `TestScan_CachedCoverOverriddenIsServedWithoutRecheckingOverrideStore` (around line 328), change:

```go
	cache.Put(path, librarycache.Entry{
		ModTime: info.ModTime(), Size: info.Size(),
		Title: "Foundation", CoverPath: "/covers/override-xyz.jpg", CoverOverridden: true,
		CoverVersion: metadata.CoverExtractorVersion,
	})
```

to:

```go
	cache.Put(path, librarycache.Entry{
		ModTime: info.ModTime(), Size: info.Size(),
		Title: "Foundation", CoverPath: "/covers/override-xyz.jpg", CoverOverridden: true,
		CoverVersion: metadata.CoverExtractorVersion, MetadataVersion: metadata.MetadataExtractorVersion,
	})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/librarian/ -v`
Expected: `TestScan_StaleMetadataVersionForcesReExtractionDespiteMatchingCoverVersion` and `TestScan_ReExtractedEntryIsCachedWithCurrentMetadataVersion` FAIL (compile error: `librarycache.Entry` has no field `MetadataVersion`, and `metadata.MetadataExtractorVersion` doesn't exist yet). The two updated existing tests also fail to compile for the same reason.

- [ ] **Step 3: Add MetadataExtractorVersion, the cache field, and wire up librarian.Scan**

In `internal/metadata/extractor.go`, change the `CoverExtractorVersion` line (line 24) from:

```go
const CoverExtractorVersion = 2 // bumped: findPDFCoverPageAware can now render a full composite-cover page (pdf_render.go), producing different bytes than before for the same book.
```

to:

```go
const CoverExtractorVersion = 3 // bumped: findPDFPageImages now recurses into Form XObjects (pdf_images.go), and findPDFCover's whole-file fallback now decodes FlateDecode images (pdf.go) -- both can produce different/additional cover bytes than before for the same book.

// MetadataExtractorVersion identifies the current Title/Author/Year
// extraction logic, parallel to CoverExtractorVersion but tracked
// separately since a change to one rarely affects the other -- bumping
// CoverExtractorVersion for a Title/Author-only change would force every
// book's already-correct cover to be needlessly re-extracted and
// re-cached, and vice versa. internal/librarian.Scan stores this
// alongside each cached book the same way CoverVersion is stored,
// forcing exactly one re-extraction whenever it's stale relative to a
// cached entry. Bump this whenever a change here could cause an
// already-scanned file to yield a different Title/Author/Year than
// before -- not for changes that only affect cover bytes.
const MetadataExtractorVersion = 1 // findInfoDictBody can now locate the Info dict via a PDF 1.5+ cross-reference stream, not just a classic trailer.
```

In `internal/librarycache/librarycache.go`, add a field to `Entry` (around line 43), changing:

```go
type Entry struct {
	ModTime         time.Time `json:"modTime"`
	Size            int64     `json:"size"`
	Title           string    `json:"title"`
	Author          string    `json:"author"`
	Year            string    `json:"year"`
	Category        string    `json:"category"`
	Subcategory     string    `json:"subcategory"`
	CoverPath       string    `json:"coverPath"`
	CoverOverridden bool      `json:"coverOverridden"`
	CoverVersion    int       `json:"coverVersion"`
}
```

to:

```go
type Entry struct {
	ModTime         time.Time `json:"modTime"`
	Size            int64     `json:"size"`
	Title           string    `json:"title"`
	Author          string    `json:"author"`
	Year            string    `json:"year"`
	Category        string    `json:"category"`
	Subcategory     string    `json:"subcategory"`
	CoverPath       string    `json:"coverPath"`
	CoverOverridden bool      `json:"coverOverridden"`
	CoverVersion    int       `json:"coverVersion"`
	MetadataVersion int       `json:"metadataVersion"`
}
```

In `internal/librarian/librarian.go`, update the cache-hit condition (around line 117), changing:

```go
		if !forceRefresh {
			if entry, ok := cache.Fresh(path, info.ModTime(), info.Size()); ok && entry.CoverVersion == metadata.CoverExtractorVersion {
```

to:

```go
		if !forceRefresh {
			if entry, ok := cache.Fresh(path, info.ModTime(), info.Size()); ok && entry.CoverVersion == metadata.CoverExtractorVersion && entry.MetadataVersion == metadata.MetadataExtractorVersion {
```

And update the `cache.Put(...)` call (around line 165), changing:

```go
			cache.Put(path, librarycache.Entry{
				ModTime:         info.ModTime(),
				Size:            info.Size(),
				Title:           b.Title,
				Author:          b.Author,
				Year:            b.Year,
				Category:        b.Category,
				Subcategory:     b.Subcategory,
				CoverPath:       b.CoverPath,
				CoverOverridden: b.CoverOverridden,
				CoverVersion:    metadata.CoverExtractorVersion,
			})
```

to:

```go
			cache.Put(path, librarycache.Entry{
				ModTime:         info.ModTime(),
				Size:            info.Size(),
				Title:           b.Title,
				Author:          b.Author,
				Year:            b.Year,
				Category:        b.Category,
				Subcategory:     b.Subcategory,
				CoverPath:       b.CoverPath,
				CoverOverridden: b.CoverOverridden,
				CoverVersion:    metadata.CoverExtractorVersion,
				MetadataVersion: metadata.MetadataExtractorVersion,
			})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/librarian/ ./internal/librarycache/ ./internal/metadata/ -v`
Expected: PASS (all tests across all three packages).

- [ ] **Step 5: Run the full build and test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS with no errors (this task touches a cache contract three packages depend on: `internal/metadata`, `internal/librarycache`, `internal/librarian`, plus anything importing them, e.g. `internal/appapi`).

- [ ] **Step 6: Commit**

```bash
git add internal/metadata/extractor.go internal/librarycache/librarycache.go internal/librarian/librarian.go internal/librarian/librarian_test.go
git commit -m "Add MetadataExtractorVersion and bump CoverExtractorVersion for the Form XObject/XRef-stream/FlateDecode fixes"
```

---

## Manual Verification (after all tasks complete)

The real file used throughout this investigation is available locally for a final end-to-end check (not committed as a test fixture, per this package's existing-fixture convention):

1. Delete any existing cache entry for the book, or just run a normal library scan (the version bumps mean it self-heals automatically — no manual cache-clear needed).
2. In the desktop app, navigate to the book "Programming with Types (2019) - Vlad Riscutia" in the Library view.
3. Confirm its cover now shows correctly (the illustrated cover with "Programming with Types" title baked into the image, plus "MANNING / Vlad Riscutia / Examples in TypeScript" rendered on top via the composite-cover PDFium full-page render).
4. Confirm its Title shows "Programming with Types" and Author shows "Vlad Riscutia" (previously both blank).
