# PDF Linearized-Trailer Selection and JPXDecode Cover Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix two confirmed, independent bugs in `internal/metadata`'s PDF scanner that caused "Build Your API with Spring (2022)" to show no cover: linearized-PDF trailer selection picking a trailer with no `/Info`, and `JPXDecode` (JPEG 2000) cover images being silently unsupported.

**Architecture:** Bug A is a small correction to `findInfoDictBody`'s trailer-selection rule (`internal/metadata/pdf.go`). Bug B adds a new page-scanning function, `findFirstPageWithUndecodableImage` (`internal/metadata/pdf_images.go`), and wires it into `findPDFCoverPageAware` as a second fallback tier, alongside the existing composite-cover PDFium render.

**Tech Stack:** Go, this package's existing dependency-free regex/index-based PDF scanner, plus the already-integrated PDFium renderer (`pdf_render.go`, from a prior session) -- no new dependencies.

## Global Constraints

- Bug A's fix preserves the existing "last trailer wins" behavior for the already-tested genuine-incremental-update case (every trailer has `/Info`) -- it only changes behavior when the byte-order-last trailer lacks `/Info` while an earlier one has it.
- Bug B's fix is scoped to `findPDFCoverPageAware`'s auto-detect path only -- `ListPDFCoverCandidates`/`ExtractPDFPageCover` (the manual cover-picker's backend, `pdf_override.go`) are unaffected.
- Bug B's fallback is only tried when the primary decoded-image search finds nothing at all (`len(images) == 0`) -- it never overrides a page that already has a successfully-decoded image.
- If the PDFium render also fails (or no undecodable-image page exists), `findPDFCoverPageAware` still falls through to `findPDFCover`'s whole-file scan, same as before this plan -- never worse than the pre-existing behavior.
- `metadata.CoverExtractorVersion` bumps once (Bug B changes cover bytes); `metadata.MetadataExtractorVersion` bumps once (Bug A changes which Info dict object is used).

---

### Task 1: Prefer the trailer that actually declares /Info

**Files:**
- Modify: `internal/metadata/pdf.go`
- Test: `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: `pdfTrailerRe`, `pdfInfoRefRe` (pre-existing, unchanged).
- Produces: `findTrailerDictWithInfo(data []byte) []byte`, a new unexported helper. `findInfoDictBody(data []byte) ([]byte, bool)` keeps its existing public signature -- no callers outside this file need changes.

- [ ] **Step 1: Write the failing test**

Add this test to `internal/metadata/pdf_test.go`, after the existing `TestExtractPDF_UsesLastXRefStreamWhenMultiplePresent` test:

```go
func TestExtractPDF_UsesTrailerWithInfoEvenWhenNotLastInFileOrder(t *testing.T) {
	// Reproduces a real linearized PDF's structure: the FIRST (byte-order)
	// trailer, prepended near the start of the file for fast web view, is
	// the complete, authoritative one (with /Info and /Prev pointing to
	// the base xref table); the LAST (byte-order) trailer, associated
	// with that base/original xref table at the tail of the file, has
	// been stripped of /Root and /Info by the linearization tool. Blindly
	// picking "the byte-order-last trailer" would miss the real Info
	// dict entirely.
	fixture := "%PDF-1.4\n" +
		"3 0 obj\n<< /Title (Linearized Book) /Author (Some Author) >>\nendobj\n" +
		"trailer\n<< /Root 4 0 R /Info 3 0 R /Prev 9999 >>\n%%EOF\n" +
		"trailer\n<< /Root 4 0 R /Size 10 >>\n%%EOF"
	path := writePDFFixture(t, fixture)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "Linearized Book" {
		t.Errorf("Title = %q, want %q (the trailer that actually has /Info, not the byte-order-last one)", result.Title, "Linearized Book")
	}
	if result.Author != "Some Author" {
		t.Errorf("Author = %q, want %q", result.Author, "Some Author")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metadata/ -run TestExtractPDF_UsesTrailerWithInfoEvenWhenNotLastInFileOrder -v`
Expected: FAIL with `Title = "", want "Linearized Book"` (the current code takes the byte-order-last trailer unconditionally, which in this fixture has no `/Info`).

- [ ] **Step 3: Implement the trailer-selection fix**

In `internal/metadata/pdf.go`, replace `findInfoDictBody`'s doc comment and body (and add the new helper immediately after it), changing:

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

to:

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
// file. If the file has multiple classic trailers, the last one that
// actually declares /Info is used (see findTrailerDictWithInfo) -- not
// simply the byte-order-last trailer, since a linearized PDF's tail
// trailer commonly lacks /Info entirely. The object lookup below resolves
// via pdfObjIndex.lookup, which already prefers the LAST literal
// occurrence of a given object number (see buildPDFObjIndex) and
// additionally resolves an object compressed inside a /Type /ObjStm
// container (common in XeTeX/LaTeX-produced PDFs, which never appears as
// literal "N ... obj ... endobj" text in the file at all). If no classic
// trailer declares /Info at all, falls back to findXRefStreamTrailerDict
// for PDFs using a PDF 1.5+ cross-reference stream instead (which carries
// the same /Info key directly in its own dictionary). Returns ok=false
// (caller falls back to a whole-file scan) if neither a trailer nor an
// XRef stream trailer-equivalent, no /Info reference, or no matching
// object is found -- preserving prior best-effort behavior for atypical
// PDFs rather than erroring.
func findInfoDictBody(data []byte) ([]byte, bool) {
	idx := buildPDFObjIndex(data)
	trailerDict := findTrailerDictWithInfo(data)
	if trailerDict == nil {
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

// findTrailerDictWithInfo scans classic "trailer <<...>>" blocks in file
// order and returns the LAST one that actually declares an /Info key --
// not simply the byte-order-last trailer. A linearized PDF's tail
// trailer (associated with the base/original cross-reference table
// preserved near the end of the file) is commonly stripped of /Root and
// /Info by the linearization tool, while the first trailer in file order
// (prepended for fast web view) is the complete, authoritative one --
// blindly trusting "last in file order" would pick the wrong one.
// Scanning in reverse and returning the first /Info-bearing match found
// preserves "last wins" for the already-supported genuine
// incremental-update case (where every trailer has /Info, and the reverse
// scan's first hit IS the true last one), while correctly skipping a
// linearized tail trailer that lacks /Info. Returns nil if no classic
// trailer declares /Info at all, or if data has no classic trailer.
func findTrailerDictWithInfo(data []byte) []byte {
	trailers := pdfTrailerRe.FindAllSubmatch(data, -1)
	for i := len(trailers) - 1; i >= 0; i-- {
		if pdfInfoRefRe.Match(trailers[i][1]) {
			return trailers[i][1]
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the new one and every pre-existing `findInfoDictBody`/`extractPDF` test -- in particular `TestExtractPDF_UsesLatestIncrementalUpdateOfInfoObject` (both trailers have `/Info`, so the reverse scan's first hit is still the true last one), `TestExtractPDF_TitleAuthorEmptyWhenInfoReferenceMissing` (the one trailer has no `/Info`, so `findTrailerDictWithInfo` correctly returns nil), and `TestExtractPDF_NoTrailerLeavesTitleAuthorEmptyButStillFindsYear` (no trailer at all) all still pass unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_test.go
git commit -m "Prefer the trailer that actually declares /Info over the byte-order-last one"
```

---

### Task 2: Fall back to full-page PDFium rendering for undecodable-filter images

**Files:**
- Modify: `internal/metadata/pdf_images.go`
- Modify: `internal/metadata/pdf.go`
- Test: `internal/metadata/pdf_images_test.go`
- Test: `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: `resolveDictValue`, `pdfXObjectEntryRe`, `pdfSubtypeImageRe`, `decodePDFImageStream` (all pre-existing, from `pdf_images.go`); `renderPDFPageAsCoverFunc` (pre-existing seam, `pdf.go`).
- Produces: `findFirstPageWithUndecodableImage(idx *pdfObjIndex, pages []pdfPage) (pageNumber int, ok bool)`, a new unexported function in `pdf_images.go`, consumed only by `findPDFCoverPageAware` in this same task.

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/metadata/pdf_images_test.go`, after the existing `TestFindPDFPageImages_FormCycleDoesNotHang` test:

```go
func TestFindFirstPageWithUndecodableImage_FindsPageWithUnsupportedFilter(t *testing.T) {
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /Font << /F1 9 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 5 0 R >> >> >>\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /JPXDecode /Length 10 >>\nstream\njpxbytes12\nendstream\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	pageNum, ok := findFirstPageWithUndecodableImage(idx, pages)
	if !ok {
		t.Fatal("findFirstPageWithUndecodableImage ok=false, want true")
	}
	if pageNum != 2 {
		t.Errorf("pageNum = %d, want 2 (page 1 has no image at all, page 2 has the undecodable one)", pageNum)
	}
}

func TestFindFirstPageWithUndecodableImage_NoImagesReturnsNotOK(t *testing.T) {
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /Font << /F1 9 0 R >> >> >>\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	if _, ok := findFirstPageWithUndecodableImage(idx, pages); ok {
		t.Error("findFirstPageWithUndecodableImage ok=true, want false (no image XObjects anywhere)")
	}
}

func TestFindFirstPageWithUndecodableImage_SkipsDecodableImage(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 4 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	if _, ok := findFirstPageWithUndecodableImage(idx, pages); ok {
		t.Error("findFirstPageWithUndecodableImage ok=true, want false (the only image is a decodable DCTDecode one)")
	}
}
```

Add these tests to `internal/metadata/pdf_test.go`, after the new test added in Task 1:

```go
func TestFindPDFCoverPageAware_RendersFullPageWhenOnlyImageHasUnsupportedFilter(t *testing.T) {
	orig := renderPDFPageAsCoverFunc
	defer func() { renderPDFPageAsCoverFunc = orig }()

	called := false
	var calledWithPage int
	renderPDFPageAsCoverFunc = func(data []byte, pageNum int) ([]byte, string, bool) {
		called = true
		calledWithPage = pageNum
		return []byte("RENDERED-PNG-BYTES"), "image/png", true
	}

	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 4 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /XObject /Subtype /Image /Filter /JPXDecode /Length 10 >>\nstream\njpxbytes12\nendstream\nendobj\n")

	imageBytes, contentType, ok := findPDFCoverPageAware(data, 10)
	if !ok {
		t.Fatal("findPDFCoverPageAware ok=false, want true")
	}
	if !called {
		t.Fatal("renderPDFPageAsCoverFunc was not called despite an undecodable-filter image")
	}
	if calledWithPage != 1 {
		t.Errorf("called with page %d, want 1", calledWithPage)
	}
	if string(imageBytes) != "RENDERED-PNG-BYTES" || contentType != "image/png" {
		t.Errorf("got %q/%q, want the rendered stand-in bytes/content-type", imageBytes, contentType)
	}
}

func TestFindPDFCoverPageAware_FallsBackToWholeFileScanWhenUndecodableImageRenderFails(t *testing.T) {
	orig := renderPDFPageAsCoverFunc
	defer func() { renderPDFPageAsCoverFunc = orig }()

	renderPDFPageAsCoverFunc = func(data []byte, pageNum int) ([]byte, string, bool) {
		return nil, "", false // simulate a PDFium failure
	}

	jpegData := []byte("\xFF\xD8\xFFfallbackjpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 4 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /XObject /Subtype /Image /Filter /JPXDecode /Length 10 >>\nstream\njpxbytes12\nendstream\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 20 >>\nstream\n" + string(jpegData) + "\nendstream\nendobj\n")

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

Run: `go test ./internal/metadata/ -run TestFindFirstPageWithUndecodableImage -v`
Expected: FAIL to compile (`findFirstPageWithUndecodableImage` doesn't exist yet).

Run: `go test ./internal/metadata/ -run TestFindPDFCoverPageAware_RendersFullPageWhenOnlyImageHasUnsupportedFilter -v`
Expected: FAIL with `renderPDFPageAsCoverFunc was not called` (no fallback wired up yet, so `findPDFCoverPageAware` falls straight through to `findPDFCover`'s whole-file scan, which also can't decode JPXDecode, so `ok=false` too).

- [ ] **Step 3: Implement the undecodable-image fallback**

In `internal/metadata/pdf_images.go`, add `findFirstPageWithUndecodableImage` after the existing `decodePDFImageStream` function (at the end of the file):

```go
// findFirstPageWithUndecodableImage walks pages' own (not nested-Form)
// XObject entries looking for a /Subtype /Image entry that
// decodePDFImageStream couldn't decode (e.g. an unsupported filter like
// JPXDecode/JPEG 2000) -- a signal that the page's true cover exists but
// this package's image decoders don't understand its encoding, so
// findPDFCoverPageAware (pdf.go) falls back to a full-page PDFium render
// (which does understand it) for such a page. Deliberately standalone
// rather than sharing findImagesInXObjects' traversal: it doesn't need
// that function's Form-XObject recursion, depth cap, or visited-set (an
// undecodable image directly on the page is the only shape this bug
// needs to detect), and keeping it separate avoids changing
// findPDFPageImages' behavior for ListPDFCoverCandidates/
// ExtractPDFPageCover (pdf_override.go), which should stay unaffected.
// Returns ok=false if no page in pages has any /Subtype /Image XObject
// that fails to decode.
func findFirstPageWithUndecodableImage(idx *pdfObjIndex, pages []pdfPage) (pageNumber int, ok bool) {
	for _, p := range pages {
		resources, ok := resolveDictValue(idx, p.dict, "Resources")
		if !ok {
			continue
		}
		xobjects, ok := resolveDictValue(idx, resources, "XObject")
		if !ok {
			continue
		}
		for _, ref := range pdfXObjectEntryRe.FindAllSubmatch(xobjects, -1) {
			objNum, err := strconv.Atoi(string(ref[1]))
			if err != nil {
				continue
			}
			body, ok := idx.lookup(objNum)
			if !ok {
				continue
			}
			dict, stream, hasStream := splitPDFObjectBody(body)
			if !hasStream || !pdfSubtypeImageRe.Match(dict) {
				continue
			}
			if _, _, ok := decodePDFImageStream(idx, resources, dict, stream); !ok {
				return p.number, true
			}
		}
	}
	return 0, false
}
```

In `internal/metadata/pdf.go`, replace `findPDFCoverPageAware`'s doc comment and body, changing:

```go
// findPDFCoverPageAware is the primary cover-selection entry point: it
// walks the page tree (walkPDFPageTree, pdf_pages.go) and returns the
// first qualifying image found within the first pageLimit pages, in page
// order. If that image's page also shows signs of being a composite
// cover -- a text-show operator alongside the image, per
// pageContentSuggestsCompositeCover -- the whole page is rendered in
// full instead (renderPDFPageAsCover, pdf_render.go), falling back to the
// raw image if rendering fails. If the page tree can't be resolved at
// all, or no qualifying image turns up within the page limit, it falls
// back to findPDFCover's whole-file byte-order scan -- so this is never
// worse than the pre-page-aware behavior, only better when a real page
// tree is present.
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
	}
	return findPDFCover(idx, data)
}
```

to:

```go
// findPDFCoverPageAware is the primary cover-selection entry point: it
// walks the page tree (walkPDFPageTree, pdf_pages.go) and returns the
// first qualifying image found within the first pageLimit pages, in page
// order. If that image's page also shows signs of being a composite
// cover -- a text-show operator alongside the image, per
// pageContentSuggestsCompositeCover -- the whole page is rendered in
// full instead (renderPDFPageAsCover, pdf_render.go), falling back to the
// raw image if rendering fails. If no image could be decoded at all but
// an early page has an image XObject this package's decoders don't
// understand (e.g. JPXDecode/JPEG 2000 -- see
// findFirstPageWithUndecodableImage, pdf_images.go), that page is
// rendered in full the same way. If the page tree can't be resolved at
// all, or neither of those signals turns up anything within the page
// limit, it falls back to findPDFCover's whole-file byte-order scan --
// so this is never worse than the pre-page-aware behavior, only better
// when a real page tree is present.
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
		if pageNum, ok := findFirstPageWithUndecodableImage(idx, pages); ok {
			if renderedBytes, renderedContentType, ok := renderPDFPageAsCoverFunc(data, pageNum); ok {
				return renderedBytes, renderedContentType, true
			}
		}
	}
	return findPDFCover(idx, data)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the five new ones and every pre-existing `findPDFPageImages`/`findPDFCoverPageAware`/`extractPDF` test -- in particular `TestFindPDFCoverPageAware_UsesRawImageWhenNoCompositeCoverSignal` and `TestFindPDFCoverPageAware_FallsBackToRawImageWhenRenderFails`, which use `buildMinimalValidPDF`'s DCTDecode image and so never reach the new undecodable-image branch at all -- `len(images) > 0` is already true for them).

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_images.go internal/metadata/pdf.go internal/metadata/pdf_images_test.go internal/metadata/pdf_test.go
git commit -m "Fall back to full-page PDFium rendering for undecodable-filter cover images"
```

---

### Task 3: Version bumps so already-cached books self-heal

**Files:**
- Modify: `internal/metadata/extractor.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `metadata.CoverExtractorVersion` changes from `4` to `5`; `metadata.MetadataExtractorVersion` changes from `3` to `4`. `internal/librarian`'s existing cache-hit condition already compares against both constants by reference, not hardcoded numbers, so no changes are needed in `internal/librarian/librarian.go` or its tests.

- [ ] **Step 1: Bump both constants**

In `internal/metadata/extractor.go`, change:

```go
const CoverExtractorVersion = 4 // bumped: extractEpub can now fall back to the first spine document's first <img> when no OPF cover convention (EPUB3 properties="cover-image" or EPUB2 meta name="cover") is present, producing a cover where none existed before for the same book.
```

to:

```go
const CoverExtractorVersion = 5 // bumped: findPDFCoverPageAware can now fall back to a full-page PDFium render when a page's only image uses an unsupported filter (e.g. JPXDecode/JPEG 2000), producing a cover where none existed before for the same book.
```

And change:

```go
const MetadataExtractorVersion = 3 // bumped: extractEpub now blanks placeholder Title/Author values (a bare numeric ID, or a literal "Unknown" author) instead of returning them as-is, and internal/librarian.Scan now falls back to filename heuristics (matching internal/pipeline.Run's existing behavior) whenever Title/Author/Year comes back empty.
```

to:

```go
const MetadataExtractorVersion = 4 // bumped: findInfoDictBody now prefers whichever classic trailer actually declares /Info, not simply the byte-order-last one -- a linearized PDF's tail trailer commonly lacks /Info entirely, which could change which Info dict object (and hence Title/Author/Subject) is used for an already-scanned file.
```

- [ ] **Step 2: Run the full metadata and librarian test suites to confirm no regressions**

Run: `go test ./internal/metadata/... ./internal/librarian/... ./internal/librarycache/... -v`
Expected: PASS (all tests -- in particular, confirm the existing `TestScan_StaleCoverVersionForcesReExtractionDespiteMatchingModTimeAndSize`, `TestScan_StaleMetadataVersionForcesReExtractionDespiteMatchingCoverVersion`, `TestScan_ReExtractedEntryIsCachedWithCurrentCoverVersion`, and `TestScan_ReExtractedEntryIsCachedWithCurrentMetadataVersion` tests all still pass, since all four compare against the live constants directly rather than hardcoded numbers).

- [ ] **Step 3: Run the full build and vet**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS with no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/metadata/extractor.go
git commit -m "Bump CoverExtractorVersion and MetadataExtractorVersion for the trailer-selection and JPXDecode fixes"
```

---

## Manual Verification (after all tasks complete)

The real file used throughout this investigation is available locally for a final end-to-end check (not committed as a test fixture, per this package's existing-fixture convention):

1. Run a normal library scan (the version bumps mean this book's stale cache entry self-heals automatically -- no manual cache-clear needed).
2. In the desktop app, navigate to "Build Your API with Spring (2022)" in the Library view.
3. Confirm its cover now shows correctly (the Baeldung-branded cover with the JPEG 2000 photo, rendered via PDFium).
