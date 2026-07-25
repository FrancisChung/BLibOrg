package metadata

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

// buildMinimalValidPDFWithSplitContents is like buildMinimalValidPDF, but
// splits the page's content across TWO /Contents objects referenced via
// an array ("/Contents [N 0 R M 0 R]"), as real-world PDF producers
// commonly emit -- testing pageContentSuggestsCompositeCover's array
// /Contents resolution branch, which buildMinimalValidPDF's single-ref
// /Contents never exercises.
func buildMinimalValidPDFWithSplitContents() []byte {
	jpeg := []byte("\xFF\xD8\xFFrealjpeg")
	imageOps := "q 200 0 0 200 0 0 cm /Im0 Do Q"
	textOps := "BT /F1 12 Tf 10 10 Td (Author Name) Tj ET"

	var buf bytes.Buffer
	offsets := make([]int, 0, 7)
	buf.WriteString("%PDF-1.4\n")

	writeObj := func(n int, body string) {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}

	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] "+
		"/Resources << /XObject << /Im0 4 0 R >> /Font << /F1 6 0 R >> >> /Contents [5 0 R 7 0 R] >>")
	writeObj(4, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 "+
		"/ColorSpace /DeviceGray /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", len(jpeg), jpeg))
	writeObj(5, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(imageOps), imageOps))
	writeObj(6, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	writeObj(7, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(textOps), textOps))

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(offsets)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xrefOffset)

	return buf.Bytes()
}

func TestPageContentSuggestsCompositeCover_ChecksAllArrayContentsStreams(t *testing.T) {
	data := buildMinimalValidPDFWithSplitContents()
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok || len(pages) != 1 {
		t.Fatalf("walkPDFPageTree ok=%v pages=%d, want ok=true pages=1", ok, len(pages))
	}

	if !pageContentSuggestsCompositeCover(idx, pages[0]) {
		t.Error("pageContentSuggestsCompositeCover = false, want true -- the text-show operator is in the SECOND of two array-referenced /Contents streams, and must still be found")
	}
}

func TestPageContentSuggestsCompositeCover_DetectsArrayFormTJWithNoPrecedingSpace(t *testing.T) {
	// buildMinimalValidPDF's withText=true fixture already writes a
	// single-string Tj, not the array-form TJ with no preceding space
	// that real-world PDFs (InDesign) actually emit -- this test proves
	// the full pageContentSuggestsCompositeCover path handles that exact
	// shape, not just the standalone regex helper.
	jpeg := []byte("\xFF\xD8\xFFrealjpeg")
	contentOps := "q 200 0 0 200 0 0 cm /Im0 Do Q\nBT /F1 12 Tf 10 10 Td [(H)-7 (i)]TJ ET"

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
	data := buf.Bytes()

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok || len(pages) != 1 {
		t.Fatalf("walkPDFPageTree ok=%v pages=%d, want ok=true pages=1", ok, len(pages))
	}

	if !pageContentSuggestsCompositeCover(idx, pages[0]) {
		t.Error("pageContentSuggestsCompositeCover = false, want true -- must detect \"]TJ\" with no preceding space through the full function, not just the standalone regex helper")
	}
}

func TestPDFiumMutex_SerializesConcurrentAccess(t *testing.T) {
	// Directly exercises pdfiumMu itself (same package, so the unexported
	// mutex is reachable) rather than driving real, slow PDFium render
	// calls through renderPDFPageAsCover -- the property being verified
	// is "the mutex actually serializes overlapping callers," which
	// doesn't require a real PDF or a real render to prove.
	const goroutines = 20
	var active int32
	var maxActive int32
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pdfiumMu.Lock()
			n := atomic.AddInt32(&active, 1)
			for {
				m := atomic.LoadInt32(&maxActive)
				if n <= m || atomic.CompareAndSwapInt32(&maxActive, m, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			atomic.AddInt32(&active, -1)
			pdfiumMu.Unlock()
		}()
	}
	wg.Wait()
	if maxActive != 1 {
		t.Errorf("maxActive = %d, want 1 (pdfiumMu did not serialize concurrent access)", maxActive)
	}
}
