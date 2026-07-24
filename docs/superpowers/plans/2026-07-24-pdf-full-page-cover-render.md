# Full-Page PDF Cover Rendering Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a PDF's cover is a composite (an embedded illustration plus separately-drawn title/author/logo text), render that page in full via PDFium instead of returning just the illustration, so the extracted cover matches what a human sees.

**Architecture:** A new `internal/metadata/pdf_render.go` wraps `github.com/klippa-app/go-pdfium`'s WebAssembly backend (a lazily-initialized, package-level singleton instance) to render one page to a PNG. A heuristic in the same file inspects the page's already-decompressed content stream for a text-show operator (`Tj`/`TJ`) alongside the found image — the same signal used by hand to diagnose the motivating bug. `findPDFCoverPageAware` (`pdf.go`) is wired to call the renderer instead of using the raw image bytes when the heuristic fires. `metadata.CoverExtractorVersion` bumps, so every previously-cached book self-heals via the version-stamped cache this session already built.

**Tech Stack:** Go, `github.com/klippa-app/go-pdfium` v1.19.4 (WASM/wazero backend, no CGo).

## Global Constraints

- Design spec: `docs/superpowers/specs/2026-07-24-pdf-full-page-cover-render-design.md` — read it in full before starting.
- This is the project's first external Go dependency. Every prior file in `internal/metadata` is a dependency-free textual scanner; this new capability is additive, not a replacement — the existing image extraction stays the default path for every book (per the design's "fallback only" scope).
- Never let a single book's rendering failure propagate as an error out of `metadata.Extract` — matches this package's pervasive "a corrupt/unusual PDF degrades gracefully, never fails the whole scan" convention. `renderPDFPageAsCover` returns `ok=false`, never an error, on any failure.
- Already verified working end-to-end in this exact repo before this plan was written (do not re-litigate feasibility): rendering the real "AI Engineering" PDF via `github.com/klippa-app/go-pdfium`'s WASM backend produces a byte-for-byte-recognizable, pixel-perfect match of the book's actual designed cover (owl illustration + O'Reilly logo + title + author, all composited). The embedded PDFium WASM blob is 5,223,101 bytes (~5.0 MiB) — this is the dependency's actual size contribution, not an estimate.
- Already verified: this package's *existing* hand-built minimal PDF test fixtures (e.g. `writeRealPDFFixture` in `internal/librarian/librarian_test.go`, and similar helpers already in `internal/metadata`'s own tests) are **rejected by PDFium** ("incorrect format") because they lack a real xref table and trailer. Do not reuse that fixture style for this plan's tests. Use `buildMinimalValidPDF` (defined in Task 1, a verified-working fixture builder with a real xref table/trailer) instead.
- Adding the dependency requires **two** `go get` commands, not one — `github.com/klippa-app/go-pdfium`'s `webassembly` subpackage pulls in `github.com/jolestar/go-commons-pool/v2` as an indirect dependency that needs its own `go.sum` entry, which a single `go get github.com/klippa-app/go-pdfium@v1.19.4` does not add (confirmed by hitting this exact "missing go.sum entry" build error while researching this plan). Run both:
  ```bash
  go get github.com/klippa-app/go-pdfium@v1.19.4
  go get github.com/klippa-app/go-pdfium/webassembly@v1.19.4
  ```
- Run `go build ./...`, `go vet ./...`, and `go test ./...` after every task; all must stay green. (`go test ./...` will be slower than usual once this dependency lands — WASM runtime startup has real cost — this is expected, not a regression.)

---

### Task 1: `renderPDFPageAsCover` — the PDFium render primitive

**Files:**
- Create: `internal/metadata/pdf_render.go`
- Create: `internal/metadata/pdf_render_test.go`
- Modify: `go.mod`, `go.sum` (via `go get`, not hand-edited)

**Interfaces:**
- Produces: `func renderPDFPageAsCover(data []byte, pageNum int) (imageBytes []byte, contentType string, ok bool)` — `pageNum` is 1-based, matching this package's existing `pdfPage.number` convention (`pdf_pages.go`). Consumed by Task 3. Also produces the test-file-local `buildMinimalValidPDF(withText bool) []byte` fixture helper, consumed by Task 2 and Task 3's tests (same package, no import needed — Go test files in one package share helpers across files in that package's test binary).

- [ ] **Step 1: Measure the baseline binary size, before any changes**

Run:
```bash
cd desktop && go build -o /tmp/desktop-before-pdfium . && ls -la /tmp/desktop-before-pdfium && cd ..
```
Record the byte size shown — you'll compare against it in Step 8.

- [ ] **Step 2: Write the failing tests**

Create `internal/metadata/pdf_render_test.go`:

```go
package metadata

import (
	"bytes"
	"fmt"
	"testing"
)

// buildMinimalValidPDF constructs a syntactically complete single-page PDF
// (a real xref table + trailer, unlike this package's existing hand-built
// test fixtures, which PDFium rejects outright with "incorrect format")
// with one embedded DCTDecode image XObject and, if withText is true, a
// separate BT/Tj text-show block after the image draw -- for testing the
// composite-cover heuristic (Task 2) and the end-to-end wiring (Task 3).
func buildMinimalValidPDF(withText bool) []byte {
	jpeg := []byte("\xFF\xD8\xFFrealjpeg")
	contentOps := "q 200 0 0 200 0 0 cm /Im0 Do Q"
	if withText {
		contentOps += "\nBT /F1 12 Tf 10 10 Td (Author Name) Tj ET"
	}

	var buf bytes.Buffer
	offsets := make([]int, 0, 6)
	buf.WriteString("%PDF-1.4\n")

	writeObj := func(n int, body string) {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}

	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] "+
		"/Resources << /XObject << /Im0 4 0 R >> /Font << /F1 6 0 R >> >> /Contents 5 0 R >>")
	writeObj(4, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 "+
		"/ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", len(jpeg), jpeg))
	writeObj(5, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(contentOps), contentOps))
	writeObj(6, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(offsets)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xrefOffset)

	return buf.Bytes()
}

func TestRenderPDFPageAsCover_RendersAValidPage(t *testing.T) {
	data := buildMinimalValidPDF(true)

	imageBytes, contentType, ok := renderPDFPageAsCover(data, 1)
	if !ok {
		t.Fatal("renderPDFPageAsCover ok=false, want true for a valid single-page PDF")
	}
	if contentType != "image/png" {
		t.Errorf("contentType = %q, want image/png", contentType)
	}
	if len(imageBytes) == 0 {
		t.Error("imageBytes is empty, want non-empty PNG data")
	}
	// A real PNG file starts with this fixed 8-byte signature.
	pngSignature := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if !bytes.HasPrefix(imageBytes, pngSignature) {
		t.Error("imageBytes does not start with the PNG file signature")
	}
}

func TestRenderPDFPageAsCover_MalformedPDFReturnsNotOK(t *testing.T) {
	imageBytes, contentType, ok := renderPDFPageAsCover([]byte("not a pdf at all"), 1)
	if ok {
		t.Error("renderPDFPageAsCover ok=true for garbage input, want false")
	}
	if imageBytes != nil || contentType != "" {
		t.Errorf("got imageBytes=%v contentType=%q on failure, want nil/\"\"", imageBytes, contentType)
	}
}

func TestRenderPDFPageAsCover_OutOfRangePageReturnsNotOK(t *testing.T) {
	data := buildMinimalValidPDF(false)

	_, _, ok := renderPDFPageAsCover(data, 99)
	if ok {
		t.Error("renderPDFPageAsCover ok=true for an out-of-range page, want false")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail to compile**

Run: `go test ./internal/metadata/... -run TestRenderPDFPageAsCover -v`
Expected: FAIL — a compile error, `undefined: renderPDFPageAsCover` (the dependency isn't added yet and the function doesn't exist). This is the expected RED state for a task introducing both a new dependency and a new function together.

- [ ] **Step 4: Add the dependency**

```bash
go get github.com/klippa-app/go-pdfium@v1.19.4
go get github.com/klippa-app/go-pdfium/webassembly@v1.19.4
```

- [ ] **Step 5: Implement `renderPDFPageAsCover`**

Create `internal/metadata/pdf_render.go`:

```go
// This file adds a fallback capability for PDF covers this package's
// otherwise dependency-free textual scanner structurally cannot provide:
// a page whose visual cover is a composite of an embedded raster image
// plus separately-drawn vector text/graphics (title, author, publisher
// logo) layered on top -- common in professionally-designed technical
// book covers (confirmed on a real O'Reilly title). No amount of
// image-XObject extraction can recover text that was never part of any
// raster image. renderPDFPageAsCover renders the whole page instead, via
// PDFium's WebAssembly build (github.com/klippa-app/go-pdfium) -- no CGo,
// no external binary the end user needs to install, the WASM blob is
// embedded in this Go binary by that library itself.
package metadata

import (
	"bytes"
	"image/png"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// pdfRenderDPI is the resolution renderPDFPageAsCover renders at. 150 DPI
// on this feature's motivating real-world example (a 504x661.5pt page)
// produces a 1050x1379px PNG -- large enough to be a genuinely usable
// cover image, not so large that it's wasteful to cache.
const pdfRenderDPI = 150

var (
	pdfiumInstance pdfium.Pdfium
	pdfiumInitOnce sync.Once
	pdfiumInitErr  error
)

// getPdfiumInstance lazily starts exactly one PDFium WASM instance for the
// life of the process and reuses it for every render call -- WASM runtime
// startup has real cost, so this must not happen per book.
func getPdfiumInstance() (pdfium.Pdfium, error) {
	pdfiumInitOnce.Do(func() {
		pool, err := webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: 1, MaxTotal: 1})
		if err != nil {
			pdfiumInitErr = err
			return
		}
		pdfiumInstance, pdfiumInitErr = pool.GetInstance(30 * time.Second)
	})
	return pdfiumInstance, pdfiumInitErr
}

// renderPDFPageAsCover renders pageNum (1-based, matching this package's
// pdfPage.number convention) of the PDF in data to a full-page PNG image,
// compositing embedded images with any vector text/graphics drawn on top
// of them -- unlike this package's usual image-XObject extraction, which
// can only ever recover an embedded raster image on its own. ok is false
// (never an error) on any failure -- an unopenable/corrupt PDF, an
// out-of-range page, or a PDFium rendering failure -- matching this
// package's pervasive "one book's failure never fails the whole scan"
// convention.
func renderPDFPageAsCover(data []byte, pageNum int) (imageBytes []byte, contentType string, ok bool) {
	instance, err := getPdfiumInstance()
	if err != nil {
		return nil, "", false
	}

	doc, err := instance.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return nil, "", false
	}
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	resp, err := instance.RenderPageInDPI(&requests.RenderPageInDPI{
		DPI: pdfRenderDPI,
		Page: requests.Page{
			ByIndex: &requests.PageByIndex{
				Document: doc.Document,
				Index:    pageNum - 1, // PDFium pages are 0-indexed.
			},
		},
	})
	if err != nil {
		return nil, "", false
	}
	defer resp.Cleanup()

	var buf bytes.Buffer
	if err := png.Encode(&buf, resp.Result.Image); err != nil {
		return nil, "", false
	}
	return buf.Bytes(), "image/png", true
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run TestRenderPDFPageAsCover -v`
Expected: all PASS. (This will take a few seconds — WASM instance startup.)

- [ ] **Step 7: Run the full package suite**

Run: `go test ./internal/metadata/... -v 2>&1 | tail -60`
Expected: every pre-existing test still passes, alongside the three new ones.

- [ ] **Step 8: Measure the binary size delta**

```bash
cd desktop && go build -o /tmp/desktop-after-pdfium . && ls -la /tmp/desktop-after-pdfium && cd ..
```
Compare against Step 1's size. Report the exact before/after byte counts and the delta in this task's report — this is the concrete number the design's "go/no-go checkpoint" needed. (Expected to be in the same ballpark as the 5,223,101-byte WASM blob itself, plus a small amount of Go wrapper code — if the delta is wildly different from that, say so explicitly in the report rather than silently proceeding.)

- [ ] **Step 9: Build and vet the whole repo**

Run: `go build ./... && go vet ./...`
Expected: clean.

- [ ] **Step 10: Commit**

```bash
git add go.mod go.sum internal/metadata/pdf_render.go internal/metadata/pdf_render_test.go
git commit -m "Add renderPDFPageAsCover: full-page PDF rendering via PDFium/WASM"
```

---

### Task 2: `pageContentSuggestsCompositeCover` heuristic

**Files:**
- Modify: `internal/metadata/pdf_render.go`
- Modify: `internal/metadata/pdf_render_test.go`

**Interfaces:**
- Consumes: `buildMinimalValidPDF(withText bool) []byte` (Task 1, same test file), `buildPDFObjIndex(data []byte) *pdfObjIndex`, `walkPDFPageTree(idx *pdfObjIndex, limit int) ([]pdfPage, bool)`, `idx.lookup(objNum int) ([]byte, bool)`, `splitPDFObjectBody(body []byte) (dict, stream []byte, hasStream bool)` (all existing, `pdf_objects.go`/`pdf_pages.go`).
- Produces: `func pageContentSuggestsCompositeCover(idx *pdfObjIndex, page pdfPage) bool` — consumed by Task 3.

- [ ] **Step 1: Write the failing tests**

Append to `internal/metadata/pdf_render_test.go`:

```go
func TestPageContentSuggestsCompositeCover_TrueWhenPageHasTextShowOperator(t *testing.T) {
	data := buildMinimalValidPDF(true)
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok || len(pages) != 1 {
		t.Fatalf("walkPDFPageTree ok=%v pages=%d, want ok=true pages=1", ok, len(pages))
	}

	if !pageContentSuggestsCompositeCover(idx, pages[0]) {
		t.Error("pageContentSuggestsCompositeCover = false, want true (fixture has a Tj text-show block)")
	}
}

func TestPageContentSuggestsCompositeCover_FalseWhenPageIsImageOnly(t *testing.T) {
	data := buildMinimalValidPDF(false)
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok || len(pages) != 1 {
		t.Fatalf("walkPDFPageTree ok=%v pages=%d, want ok=true pages=1", ok, len(pages))
	}

	if pageContentSuggestsCompositeCover(idx, pages[0]) {
		t.Error("pageContentSuggestsCompositeCover = true, want false (fixture has only an image Do operator, no text)")
	}
}

func TestPageContentSuggestsCompositeCover_MatchesTJWithNoPrecedingSpace(t *testing.T) {
	// The real content stream that motivated this whole feature has "]TJ"
	// with no space before the operator (InDesign's typical output for
	// the array-form text-show operator) -- a naive "preceded by
	// whitespace" check would miss exactly this case.
	idx := buildPDFObjIndex(buildMinimalValidPDF(false)) // only need a valid idx/page shape
	pages, _ := walkPDFPageTree(idx, 10)
	page := pages[0]

	if !matchesTextShowOperator([]byte("[(H)-7 (i)]TJ")) {
		t.Error("matchesTextShowOperator = false for \"]TJ\" with no preceding space, want true")
	}
	_ = page // page isn't used by this specific sub-check; kept for clarity that it's the same fixture family
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run TestPageContentSuggestsCompositeCover -v`
Expected: FAIL — compile error, `undefined: pageContentSuggestsCompositeCover` and `undefined: matchesTextShowOperator`.

- [ ] **Step 3: Implement the heuristic**

Append to `internal/metadata/pdf_render.go`:

```go
var pdfContentsRefRe = regexp.MustCompile(`/Contents\s+(\d+)\s+\d+\s+R`)
var pdfContentsArrayRe = regexp.MustCompile(`/Contents\s*\[([^\]]*)\]`)
var pdfTextShowOperatorRe = regexp.MustCompile(`\b(Tj|TJ)\b`)

// matchesTextShowOperator reports whether stream contains a PDF text-show
// operator (Tj, the single-string form; or TJ, the array-with-kerning
// form) as a standalone token. Word-boundary matched rather than requiring
// a preceding space: real-world PDFs (InDesign's output, confirmed on
// this feature's motivating example) commonly write the array form as
// "]TJ" with no space between the closing bracket and the operator.
func matchesTextShowOperator(stream []byte) bool {
	return pdfTextShowOperatorRe.Match(stream)
}

// pageContentSuggestsCompositeCover reports whether page's content
// stream(s) contain a text-show operator, alongside whatever image is
// drawn there -- the signal that the page's true visual cover is a
// composite of that image plus separately-drawn text (a title, author
// name, or similar), which plain image-XObject extraction can never
// recover. A page's /Contents may be a single indirect reference or an
// array of them (multiple content-stream objects concatenated in order,
// as PDF producers commonly emit); every one of them is checked.
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
		_, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream {
			continue
		}
		r, err := zlib.NewReader(bytes.NewReader(stream))
		if err != nil {
			continue
		}
		decompressed, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			continue
		}
		if matchesTextShowOperator(decompressed) {
			return true
		}
	}
	return false
}
```

Add `"compress/zlib"`, `"io"`, `"regexp"`, and `"strconv"` to `pdf_render.go`'s import block (alongside the existing `"bytes"`, `"image/png"`, `"sync"`, `"time"`, and the three `go-pdfium` imports from Task 1).

`pdfKidRefRe` is an existing package-level regexp already defined in `pdf_pages.go` (`(\d+)\s+\d+\s+R`) — reused here as-is, not redefined.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run "TestPageContentSuggestsCompositeCover|TestRenderPDFPageAsCover" -v`
Expected: all PASS.

- [ ] **Step 5: Run the full package suite**

Run: `go test ./internal/metadata/... -v 2>&1 | tail -80`
Expected: every test passes.

- [ ] **Step 6: Build and vet**

Run: `go build ./... && go vet ./internal/metadata/...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add internal/metadata/pdf_render.go internal/metadata/pdf_render_test.go
git commit -m "Add pageContentSuggestsCompositeCover heuristic for composite PDF covers"
```

---

### Task 3: Wire into `findPDFCoverPageAware`, bump `CoverExtractorVersion`

**Files:**
- Modify: `internal/metadata/pdf.go`
- Modify: `internal/metadata/pdf_test.go`
- Modify: `internal/metadata/extractor.go`

**Interfaces:**
- Consumes: `renderPDFPageAsCover(data []byte, pageNum int) ([]byte, string, bool)` and `pageContentSuggestsCompositeCover(idx *pdfObjIndex, page pdfPage) bool` (Task 1/2), `buildMinimalValidPDF(withText bool) []byte` (Task 1, test helper).
- Produces: a test-only seam `var renderPDFPageAsCoverFunc = renderPDFPageAsCover` in `pdf.go`, matching the existing `internal/librarian`'s `extractFunc` pattern, so tests can verify whether rendering was invoked without needing a real PDFium call in every test.

- [ ] **Step 1: Write the failing tests**

Append to `internal/metadata/pdf_test.go`:

```go
func TestFindPDFCoverPageAware_RendersFullPageWhenCompositeCoverDetected(t *testing.T) {
	orig := renderPDFPageAsCoverFunc
	defer func() { renderPDFPageAsCoverFunc = orig }()

	called := false
	var calledWithPage int
	renderPDFPageAsCoverFunc = func(data []byte, pageNum int) ([]byte, string, bool) {
		called = true
		calledWithPage = pageNum
		return []byte("RENDERED-PNG-BYTES"), "image/png", true
	}

	data := buildMinimalValidPDF(true) // withText=true -- heuristic should fire
	imageBytes, contentType, ok := findPDFCoverPageAware(data, 10)
	if !ok {
		t.Fatal("findPDFCoverPageAware ok=false, want true")
	}
	if !called {
		t.Fatal("renderPDFPageAsCoverFunc was not called despite a composite-cover page")
	}
	if calledWithPage != 1 {
		t.Errorf("called with page %d, want 1", calledWithPage)
	}
	if string(imageBytes) != "RENDERED-PNG-BYTES" || contentType != "image/png" {
		t.Errorf("got %q/%q, want the rendered stand-in bytes/content-type", imageBytes, contentType)
	}
}

func TestFindPDFCoverPageAware_UsesRawImageWhenNoCompositeCoverSignal(t *testing.T) {
	orig := renderPDFPageAsCoverFunc
	defer func() { renderPDFPageAsCoverFunc = orig }()

	renderPDFPageAsCoverFunc = func(data []byte, pageNum int) ([]byte, string, bool) {
		t.Fatal("renderPDFPageAsCoverFunc should not be called for an image-only page")
		return nil, "", false
	}

	data := buildMinimalValidPDF(false) // no text -- heuristic should not fire
	_, contentType, ok := findPDFCoverPageAware(data, 10)
	if !ok {
		t.Fatal("findPDFCoverPageAware ok=false, want true")
	}
	if contentType != "image/jpeg" {
		t.Errorf("contentType = %q, want image/jpeg (raw extraction, not rendered)", contentType)
	}
}

func TestFindPDFCoverPageAware_FallsBackToRawImageWhenRenderFails(t *testing.T) {
	orig := renderPDFPageAsCoverFunc
	defer func() { renderPDFPageAsCoverFunc = orig }()

	renderPDFPageAsCoverFunc = func(data []byte, pageNum int) ([]byte, string, bool) {
		return nil, "", false // simulate a PDFium failure
	}

	data := buildMinimalValidPDF(true) // heuristic fires, but rendering fails
	imageBytes, contentType, ok := findPDFCoverPageAware(data, 10)
	if !ok {
		t.Fatal("findPDFCoverPageAware ok=false, want true (should still return the raw image)")
	}
	if contentType != "image/jpeg" || len(imageBytes) == 0 {
		t.Errorf("got %q/%d bytes, want the raw image/jpeg fallback when rendering fails", contentType, len(imageBytes))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run TestFindPDFCoverPageAware -v`
Expected: FAIL — `undefined: renderPDFPageAsCoverFunc` (compile error).

- [ ] **Step 3: Wire the heuristic and render seam into `findPDFCoverPageAware`**

In `internal/metadata/pdf.go`, replace:

```go
func findPDFCoverPageAware(data []byte, pageLimit int) ([]byte, string, bool) {
	idx := buildPDFObjIndex(data)
	if pages, ok := walkPDFPageTree(idx, pageLimit); ok {
		if images := findPDFPageImages(idx, pages, true); len(images) > 0 {
			return images[0].bytes, images[0].contentType, true
		}
	}
	return findPDFCover(data)
}
```

with:

```go
// renderPDFPageAsCoverFunc is a seam so tests can verify whether
// full-page rendering was invoked without a real PDFium call in every
// test; production code always uses renderPDFPageAsCover (pdf_render.go).
var renderPDFPageAsCoverFunc = renderPDFPageAsCover

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
	return findPDFCover(data)
}

// findPageByNumber returns the page in pages whose 1-based number matches,
// for looking up the page a candidate image was found on (pdfPageImage
// only carries the page number, not the full pdfPage, since
// findPDFPageImages predates this file's need for it).
func findPageByNumber(pages []pdfPage, number int) (pdfPage, bool) {
	for _, p := range pages {
		if p.number == number {
			return p, true
		}
	}
	return pdfPage{}, false
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run TestFindPDFCoverPageAware -v`
Expected: all PASS.

- [ ] **Step 5: Bump `CoverExtractorVersion`**

In `internal/metadata/extractor.go`, change:

```go
const CoverExtractorVersion = 1
```

to:

```go
const CoverExtractorVersion = 2 // bumped: findPDFCoverPageAware can now render a full composite-cover page (pdf_render.go), producing different bytes than before for the same book.
```

- [ ] **Step 6: Run the full package suite**

Run: `go test ./internal/metadata/... -v 2>&1 | tail -100`
Expected: every test passes, including the pre-existing `TestCoverExtractorVersion_IsSetAndPositive` (still passes, it only checks `>= 1`).

- [ ] **Step 7: Full repo build/vet/test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all green across every package. This will be noticeably slower than before this plan (WASM instance startup happens once per test binary run, not per test) — expected, not a regression.

- [ ] **Step 8: Manual verification against the real file**

No committed test depends on a specific machine's local library path — this step is throwaway, matching this session's established pattern (write a temporary test, run it once, capture evidence, delete it before committing).

Create a temporary file `internal/metadata/scratch_manual_verify_test.go`:

```go
package metadata

import (
	"fmt"
	"os"
	"testing"
)

func TestScratchManualVerifyAIEngineeringFullCover(t *testing.T) {
	path := "/media/francis/Data1/Books/Library/Technology/AI/AI Engineering (2025) - Chip Huyen.pdf"
	if _, err := os.Stat(path); err != nil {
		t.Skip("real library not present in this environment")
	}

	res, err := Extract(path, nil, 10)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	fmt.Printf("CoverContentType=%q CoverBytes=%d\n", res.CoverContentType, len(res.CoverBytes))
	if res.CoverContentType != "image/png" {
		t.Errorf("CoverContentType = %q, want image/png (rendered, not raw-extracted image/jpeg)", res.CoverContentType)
	}

	out := "/tmp/ai-engineering-full-cover-verify.png"
	if err := os.WriteFile(out, res.CoverBytes, 0644); err != nil {
		t.Fatalf("write out: %v", err)
	}
	fmt.Printf("wrote %s -- open it and visually confirm it shows the title, author, and O'Reilly logo, not just the owl illustration\n", out)
}
```

Run: `go test ./internal/metadata/... -run TestScratchManualVerifyAIEngineeringFullCover -v`
Expected: PASS, `CoverContentType=image/png`. Open the written PNG file and visually confirm it shows the full designed cover (title "AI Engineering", author "Chip Huyen", O'Reilly logo, and the owl illustration, composited together) — not just the isolated illustration.

Then delete the scratch file — it is not part of the committed suite:
```bash
rm internal/metadata/scratch_manual_verify_test.go
```

- [ ] **Step 9: Confirm the working tree is clean after removing the scratch test**

Run: `git status`
Expected: only the intended files (`pdf.go`, `pdf_test.go`, `extractor.go`) show as modified — the scratch test must not appear.

- [ ] **Step 10: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_test.go internal/metadata/extractor.go
git commit -m "Wire full-page rendering into findPDFCoverPageAware, bump CoverExtractorVersion"
```

---

## Final step: whole-branch review and merge

After Task 3, follow this repo's established pattern for finishing a small SDD sequence:

1. Run a final whole-branch code review across the full diff from this work's starting commit to `HEAD`. Pay particular attention to the new external dependency's footprint (does `go.sum` only add what's actually needed, no stray unrelated upgrades) and to the fallback-on-failure behavior (Task 3, Step 1's third test) actually being exercised, not just asserted.
2. Fix any Important/Critical findings; Minor findings may be accepted as-is with a rationale, matching this repo's established convention.
3. Merge to `main`.
4. Report the Task 1 binary-size delta and the Task 3 manual-verification result back — those are the two concrete pieces of evidence answering "did this actually work and was the cost acceptable."
