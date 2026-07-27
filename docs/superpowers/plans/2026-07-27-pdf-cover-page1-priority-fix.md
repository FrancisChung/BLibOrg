# PDF Cover Selection: Page 1 Priority Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix a confirmed bug in `internal/metadata`'s PDF cover scanner: when page 1 has no qualifying image at all (a common shape for professionally-designed, entirely vector-drawn covers with no embedded raster image), the scanner currently keeps walking later pages for the first one that happens to have *any* image -- even an unrelated small icon on a publisher's boilerplate title page -- and renders that wrong page instead of page 1's real cover.

**Architecture:** A single, minimal change to `internal/metadata/pdf.go`'s `findPDFCoverPageAware`: check page 1 specifically, first, before the existing "first page in document order with any image" scan. If page 1 itself has zero qualifying images, render it in full immediately; only if that render fails (or page 1 does have an image) does the existing tier sequence run unchanged. This also removes a final fallback tier added by a previous plan that is provably dead code once this fix lands (see Global Constraints) -- the same behavior it existed to provide is now handled earlier and unconditionally.

**Tech Stack:** Go standard library only, this package's existing regex/index-based PDF scanner, the already-integrated PDFium renderer (`pdf_render.go`) -- no new dependencies.

## Global Constraints

- This was found by re-investigating 12 books a previous plan had wrongly classified as "no cover art exists, not fixable" -- direct PDFium renders of their real page 1 (not previously checked) proved every one of them has a genuine, well-designed cover. Root cause: `findPDFPageImages(idx, pages, true)` walks pages in order and returns the first page with *any* qualifying image, even when that page (e.g. a Leanpub/publisher boilerplate title page with a tiny logo, or an interior page with a small icon) is not the real cover and page 1 -- built entirely from vector graphics, no raster image at all -- would otherwise never be considered.
- The new page-1-first check must use `findPDFPageImages(idx, []pdfPage{firstPage}, true)` (not `pageHasMultipleImages` or any other helper) -- checking specifically "does page 1 have zero qualifying images," nothing more nuanced. Verified sufficient and correct against all 12 real books plus every existing test in the package; do not add the `pageContentSuggestsCompositeCover` check some exploratory versions of this fix used -- it is unnecessary and would miss legitimate cases where page 1 has real vector art but no text-show operator that our textual scanner can detect either.
- Task 5 of the previous plan (`2026-07-27-pdf-cover-selection-five-root-cause-fixes.md`) added a final tier to `findPDFCoverPageAware`: `if renderedBytes, ... := renderPDFPageAsCoverFunc(data, pages[0].number); ok { return ... }`, reached only when neither the decoded-image tier nor the undecodable-image tier found anything at all. Once this plan's new page-1-first check runs, that final tier becomes unreachable in any case where it could ever succeed: the only way execution reaches it is if page 1 had zero qualifying images (this plan's own new check already tried, and failed, to render exactly that same page with the exact same function) AND no other page had any image either -- so its own render attempt would retry the identical failed call and fail identically, every time, deterministically (`renderPDFPageAsCover` has no retry-sensitive state; confirmed by calling it twice back-to-back against the same real file and diffing the output -- byte-identical). Remove it as part of this same change rather than leave provably dead code behind.
- The two existing tests written for that now-removed tier (`TestFindPDFCoverPageAware_RendersPage1WhenNoImageSignalFoundAtAll` and `TestFindPDFCoverPageAware_FallsBackToWholeFileScanWhenPage1RenderFails`) describe genuinely correct, still-true observable behavior -- their scenarios are now satisfied by this plan's new earlier tier instead. Leave them completely unmodified; do not delete or rewrite them.
- `metadata.CoverExtractorVersion` bumps once (9 -> 10); `MetadataExtractorVersion` is untouched (this fix only affects cover selection, not Title/Author/Year extraction).
- No new external dependency.

---

### Task 1: Check page 1 first, and remove the now-dead final fallback tier

**Files:**
- Modify: `internal/metadata/pdf.go`
- Test: `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: `findPDFPageImages`, `renderPDFPageAsCoverFunc` (all pre-existing, unchanged).
- Produces: no new function; `findPDFCoverPageAware`'s existing public signature is unchanged, only its internal tier ordering changes. Nothing else depends on this task.

- [ ] **Step 1: Write the failing tests**

Add these two tests to `internal/metadata/pdf_test.go`, after the existing `TestFindPDFCoverPageAware_FallsBackToWholeFileScanWhenPage1RenderFails` test (the last test in the file):

```go
func TestFindPDFCoverPageAware_PrefersPage1RenderOverALaterPagesIncidentalImage(t *testing.T) {
	// Reproduces the real "Atomic Kotlin"/"Auth Considerations for
	// Kubernetes"/"Team Guide to Metrics for Business Decisions" (and 9
	// other real books') bug: page 1 is the real, entirely vector-drawn
	// cover (no raster image at all), but a LATER page -- here, a
	// publisher's standard boilerplate title page -- has a small,
	// unrelated image plus real title text. Before this fix,
	// findPDFPageImages's page-order scan walked right past page 1 (it
	// has zero images) and found that later page's icon instead, then
	// rendered THAT page in full (since it also has a text-show
	// operator) -- the wrong page. This test pins that page 1 wins.
	orig := renderPDFPageAsCoverFunc
	defer func() { renderPDFPageAsCoverFunc = orig }()

	var renderedPages []int
	renderPDFPageAsCoverFunc = func(data []byte, pageNum int) ([]byte, string, bool) {
		renderedPages = append(renderedPages, pageNum)
		if pageNum == 1 {
			return []byte("PAGE-1-REAL-COVER"), "image/png", true
		}
		return []byte("WRONG-PAGE-RENDERED"), "image/png", true
	}

	logoData := []byte("\xFF\xD8\xFFtinylogo12")
	textOps := "BT /F1 12 Tf 10 10 Td (Boilerplate Title Page) Tj ET"
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n" + // page 1: no images, no text -- the real vector cover
			"4 0 obj\n<< /Type /Page /Parent 2 0 R /Resources << /XObject << /Im0 5 0 R >> /Font << /F1 6 0 R >> >> /Contents 7 0 R >>\nendobj\n" + // page 2: has a small image + real text
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 13 >>\nstream\n" + string(logoData) + "\nendstream\nendobj\n" +
			"6 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
			"7 0 obj\n<< /Length 53 >>\nstream\n" + textOps + "\nendstream\nendobj\n")

	imageBytes, contentType, ok := findPDFCoverPageAware(data, 10)
	if !ok {
		t.Fatal("findPDFCoverPageAware ok=false, want true")
	}
	if len(renderedPages) == 0 || renderedPages[0] != 1 {
		t.Fatalf("renderedPages = %v, want the first render attempt to be page 1", renderedPages)
	}
	if string(imageBytes) != "PAGE-1-REAL-COVER" || contentType != "image/png" {
		t.Errorf("got %q/%q, want page 1's rendered cover, not page 2's incidental image", imageBytes, contentType)
	}
}

func TestFindPDFCoverPageAware_SkipsPage1PriorityCheckWhenPage1HasItsOwnImage(t *testing.T) {
	// Regression guard: when page 1 already has a qualifying image (the
	// common, already-correct case -- e.g. a real designer's cover with
	// a raster background), the new page-1-priority check must not
	// intervene at all. This is exactly the shape of the real "How to
	// use OAuth to add Authentication to your React App" book, which
	// already worked correctly before this fix.
	orig := renderPDFPageAsCoverFunc
	defer func() { renderPDFPageAsCoverFunc = orig }()

	renderPDFPageAsCoverFunc = func(data []byte, pageNum int) ([]byte, string, bool) {
		t.Fatal("renderPDFPageAsCoverFunc should not be called: page 1 has its own decodable image and no composite-cover signal, so the raw image should be returned directly")
		return nil, "", false
	}

	jpegData := []byte("\xFF\xD8\xFFrealcoverbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /Resources << /XObject << /Im0 4 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 17 >>\nstream\n" + string(jpegData) + "\nendstream\nendobj\n")

	imageBytes, contentType, ok := findPDFCoverPageAware(data, 10)
	if !ok {
		t.Fatal("findPDFCoverPageAware ok=false, want true")
	}
	if contentType != "image/jpeg" || string(imageBytes) != string(jpegData) {
		t.Errorf("got %q/%q, want page 1's own raw image returned directly, unrendered", imageBytes, contentType)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/ -run 'TestFindPDFCoverPageAware_PrefersPage1RenderOverALaterPagesIncidentalImage|TestFindPDFCoverPageAware_SkipsPage1PriorityCheckWhenPage1HasItsOwnImage' -v`
Expected: `TestFindPDFCoverPageAware_PrefersPage1RenderOverALaterPagesIncidentalImage` FAILS -- the current code's `renderedPages` would start with `2` (or the render never happens at all if page 2's raw image is returned directly without a composite check succeeding first -- either way, not page 1 first). `TestFindPDFCoverPageAware_SkipsPage1PriorityCheckWhenPage1HasItsOwnImage` PASSES already today (nothing to fix for that shape) -- this confirms it as a regression guard, not a new requirement.

- [ ] **Step 3: Implement the page-1-priority check and remove the dead tier**

In `internal/metadata/pdf.go`, change:

```go
// findPDFCoverPageAware is the primary cover-selection entry point: it
// walks the page tree (walkPDFPageTree, pdf_pages.go) and returns the
// first qualifying image found within the first pageLimit pages, in page
// order. If that image's page also shows signs of being a composite
// cover -- a text-show operator alongside the image, or more than one
// qualifying image on the page, per
// pageContentSuggestsCompositeCover/pageHasMultipleImages -- the whole
// page is rendered in full instead (renderPDFPageAsCover, pdf_render.go),
// falling back to the raw image if rendering fails. If no image could be
// decoded at all but an early page has an image XObject this package's
// decoders don't understand (e.g. JPXDecode/JPEG 2000 -- see
// findFirstPageWithUndecodableImage, pdf_images.go), that page is
// rendered in full the same way. If neither of those signals turns up
// anything at all within the page limit -- no decodable or undecodable
// image on any scanned page -- page 1 is rendered in full as a last
// resort, recovering entirely vector-drawn covers (covers with only text
// and line art, no raster images). If the page tree can't be resolved at
// all, or the page-1 render also fails, it falls back to findPDFCover's
// whole-file byte-order scan -- so this is never worse than the
// pre-page-aware behavior, only better when a real page tree is present.
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

to:

```go
// findPDFCoverPageAware is the primary cover-selection entry point: it
// walks the page tree (walkPDFPageTree, pdf_pages.go), and first checks
// page 1 specifically -- if page 1 itself has zero qualifying images, it
// is rendered in full immediately (renderPDFPageAsCover, pdf_render.go),
// before any other page is ever considered. This matters because some
// real covers are entirely vector-drawn (text and line art, no raster
// image whatsoever), while the page-order image scan below, left
// unchecked, would keep walking past such a page-1 to whatever LATER
// page happens to have any image at all -- a publisher's standard
// boilerplate title page with a small logo, or an interior page with an
// incidental icon -- and wrongly treat that as "the cover" instead.
//
// If page 1 does have its own qualifying image (the common, already-
// correct case), this check is skipped entirely and the function falls
// through to its original behavior: return the first qualifying image
// found within the first pageLimit pages, in page order. If that image's
// page also shows signs of being a composite cover -- a text-show
// operator alongside the image, or more than one qualifying image on the
// page, per pageContentSuggestsCompositeCover/pageHasMultipleImages --
// the whole page is rendered in full instead, falling back to the raw
// image if rendering fails. If no image could be decoded at all but an
// early page has an image XObject this package's decoders don't
// understand (e.g. JPXDecode/JPEG 2000 -- see
// findFirstPageWithUndecodableImage, pdf_images.go), that page is
// rendered in full the same way. If the page tree can't be resolved at
// all, or every render attempt fails, it falls back to findPDFCover's
// whole-file byte-order scan -- so this is never worse than the
// pre-page-aware behavior, only better when a real page tree is present.
func findPDFCoverPageAware(data []byte, pageLimit int) ([]byte, string, bool) {
	idx := buildPDFObjIndex(data)
	if pages, ok := walkPDFPageTree(idx, pageLimit); ok {
		firstPage := pages[0]
		if len(findPDFPageImages(idx, []pdfPage{firstPage}, true)) == 0 {
			if renderedBytes, renderedContentType, ok := renderPDFPageAsCoverFunc(data, firstPage.number); ok {
				return renderedBytes, renderedContentType, true
			}
		}
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the two new ones and every pre-existing test -- in particular, `TestFindPDFCoverPageAware_RendersPage1WhenNoImageSignalFoundAtAll` and `TestFindPDFCoverPageAware_FallsBackToWholeFileScanWhenPage1RenderFails` must still pass unmodified, now satisfied by the new earlier tier instead of the removed one).

- [ ] **Step 5: Bump CoverExtractorVersion**

In `internal/metadata/extractor.go`, change:

```go
const CoverExtractorVersion = 9 // bumped: when no image (decodable or otherwise) is found anywhere within the page limit, findPDFCoverPageAware now renders page 1 in full before falling back to findPDFCover's unbounded whole-file scan -- recovering an entirely vector-drawn cover instead of returning an arbitrary interior image (or nothing) for books with no raster cover art at all.
```

to:

```go
const CoverExtractorVersion = 10 // bumped: findPDFCoverPageAware now checks page 1 specifically, first -- if page 1 has zero qualifying images, it's rendered in full immediately, before the page-order image scan ever gets a chance to wander off to a later page's small, unrelated image (e.g. a publisher's boilerplate title page or an interior icon) and wrongly treat that page as the cover instead.
```

- [ ] **Step 6: Run tests to verify they still pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_test.go internal/metadata/extractor.go
git commit -m "Check page 1 first in findPDFCoverPageAware, ahead of the page-order image scan"
```

---

## Manual Verification (after task complete)

1. Run `go build ./... && go vet ./... && go test ./...` at the repo root, and `go test -race ./internal/metadata/...` to confirm the whole module still builds, vets clean, and every test passes (race-clean) end to end.
2. In the running desktop app (`cd desktop && ./build.sh`, or via the dev flow), click "Reset Cover Cache" in Settings, then refresh the Library view and confirm each of these real files -- every one independently confirmed during this fix's investigation to have a genuine, well-designed cover on page 1 that PDFium correctly renders -- now shows it instead of a publisher's boilerplate title page or an unrelated interior icon:
   - `/media/francis/Data1/Books/Library/Technology/Kotlin/Atomic Kotlin (2021) - Bruce Eckel, Svetlana Isakova.pdf`
   - `/media/francis/Data1/Books/Library/Technology/Authentication/Auth Considerations for Kubernetes.pdf`
   - `/media/francis/Data1/Books/Library/Technology/Authentication/Identity Authentication and Gaming.pdf`
   - `/media/francis/Data1/Books/Library/Technology/Authentication/The Modern Guide to Oauth (2024).pdf`
   - `/media/francis/Data1/Books/Library/Technology/Authentication/The Ultimate Guide to out Sourcing Your Auth (2024).pdf`
   - `/media/francis/Data1/Books/Library/Technology/Craftsmanship/High Performance Applications (2019) - Josef Mayrhofer.pdf`
   - `/media/francis/Data1/Books/Library/Technology/Leadership/Talking with Tech Leads (2021) - Patrick Kua.pdf`
   - `/media/francis/Data1/Books/Library/Technology/Product/Digital Products Leadership (2020) - Joaquim Torres.pdf`
   - `/media/francis/Data1/Books/Library/Technology/Wardley Mapping/Practical Introduction to Wardley Mapping (2019) - E Alex Hudson.pdf`
   - `/media/francis/Data1/Books/Library/Technology/Wardley Mapping/Wardley Maps (2018) - Stuart Gunter.pdf`
   - `/media/francis/Data1/Books/Library/Technology/Web/Breaking down JSON Web Tokens (2024).pdf`
   - `/media/francis/Data1/Books/Library/Technology/Leadership/Team Guide to Metrics for Business Decisions (2021) - Mattia Battiston, Chris Young.pdf` (this one was previously misdiagnosed as "not a bug, its own page 1 legitimately is a promo page" -- it is NOT; page 1 is a real designed cover, and what was rendered before was a different, later page)
3. Confirm no regression on books already correct before this fix, by spot-checking at least one of each shape from the previous plan: `How to use OAuth to add Authentication to your React App (2022).pdf` (page 1 already has its own real image), `The New Consultants Quick Start Guide.pdf` (Task 3's multiple-images fix), `Understanding Distributed Systems - 2nd Edition (2022) - Roberto Vitillo.pdf` (Task 4's Form XObject fix), and `Distributed Systems Principles and Paradigms-Maarten van Steen (2023)...pdf` (Task 5's original all-vector fix) -- all four should show the exact same cover as immediately after the previous plan merged, unchanged by this one.
