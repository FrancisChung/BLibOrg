package metadata

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

const testPDFFixture = `%PDF-1.4
1 0 obj
<< /Title (Foundation) /Author (Isaac Asimov \(revised\)) /Subject (Sci-Fi) /CreationDate (D:19510101000000) >>
endobj
trailer
<< /Root 1 0 R /Info 1 0 R >>
%%EOF`

func writePDFFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "book.pdf")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write pdf fixture: %v", err)
	}
	return path
}

func TestExtractPDF(t *testing.T) {
	path := writePDFFixture(t, testPDFFixture)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "Foundation" {
		t.Errorf("Title = %q, want Foundation", result.Title)
	}
	if result.Author != "Isaac Asimov (revised)" {
		t.Errorf("Author = %q, want %q", result.Author, "Isaac Asimov (revised)")
	}
	if result.Subject != "Sci-Fi" {
		t.Errorf("Subject = %q, want Sci-Fi", result.Subject)
	}
	if result.Year != "1951" {
		t.Errorf("Year = %q, want 1951", result.Year)
	}
}

// utf16BEBytes encodes s as UTF-16BE with a leading BOM (0xFE 0xFF), the PDF
// spec's standard encoding for text strings containing non-ASCII characters
// -- real-world producers (Chromium print-to-PDF, calibre) write /Title and
// /Author this way whenever the text isn't pure PDFDocEncoding-safe ASCII.
func utf16BEBytes(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := []byte{0xFE, 0xFF}
	for _, u := range units {
		out = append(out, byte(u>>8), byte(u))
	}
	return out
}

func TestExtractPDF_UTF16BEEncodedStrings(t *testing.T) {
	title := utf16BEBytes("The TOGAF® Standard, 10th Edition")
	author := utf16BEBytes("Josey, Andrew, Hornford, Dave")

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n1 0 obj\n<< /Title (")
	buf.Write(title)
	buf.WriteString(") /Author (")
	buf.Write(author)
	buf.WriteString(") /CreationDate (D:20220728222540+00'00') >>\nendobj\ntrailer\n<< /Root 1 0 R /Info 1 0 R >>\n%%EOF")

	path := writePDFFixture(t, buf.String())

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "The TOGAF® Standard, 10th Edition" {
		t.Errorf("Title = %q, want %q", result.Title, "The TOGAF® Standard, 10th Edition")
	}
	if result.Author != "Josey, Andrew, Hornford, Dave" {
		t.Errorf("Author = %q, want %q", result.Author, "Josey, Andrew, Hornford, Dave")
	}
	if !utf8.ValidString(result.Title) {
		t.Errorf("Title is not valid UTF-8: %q", result.Title)
	}
	if !utf8.ValidString(result.Author) {
		t.Errorf("Author is not valid UTF-8: %q", result.Author)
	}
}

func TestExtractPDF_Latin1EncodedAuthor(t *testing.T) {
	// No UTF-16BE BOM here -- this is how a single non-ASCII character
	// (e.g. "Jörg") shows up in real PDFs as a raw PDFDocEncoding/Latin-1
	// byte (0xF6 for 'ö'), not a UTF-16BE string.
	fixture := "%PDF-1.4\n1 0 obj\n<< /Author (Simon Harrer,J\xf6rg Lenhard,Linus Dietz) >>\nendobj\ntrailer\n<< /Root 1 0 R /Info 1 0 R >>\n%%EOF"
	path := writePDFFixture(t, fixture)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Author != "Simon Harrer,Jörg Lenhard,Linus Dietz" {
		t.Errorf("Author = %q, want %q", result.Author, "Simon Harrer,Jörg Lenhard,Linus Dietz")
	}
	if !utf8.ValidString(result.Author) {
		t.Errorf("Author is not valid UTF-8: %q", result.Author)
	}
}

// TestExtractPDF_IgnoresEmbeddedGraphicMetadataBeforeRealInfoDict reproduces
// a real incident: a technical book PDF embedded several graphics (a
// CorelDRAW logo, an Illustrator diagram), each carrying its own /Title and
// /Author describing that graphic, not the book -- and those objects
// appeared earlier in the file than the book's actual Info dictionary. A
// naive first-match-anywhere scan picked up the embedded logo's /Title
// ("E:\GRAPHICS\manlogo.eps") and a diagram's /Author ("Marija Tudor")
// instead of the real book metadata. The trailer's /Info N 0 R reference is
// the authoritative pointer to the real Info dictionary; extraction must
// use only that object.
const testPDFFixtureWithDecoyObjects = `%PDF-1.4
1 0 obj
<< /Type /XObject /Title (E:\GRAPHICS\decoy-logo.eps) /Creator (CorelDRAW 8) >>
endobj
2 0 obj
<< /Type /XObject /Author (Some Graphic Designer) /Creator (Adobe Illustrator) >>
endobj
3 0 obj
<< /Title (Think Like a CTO) /Author (Alan Williamson) /CreationDate (D:20230213170533Z) >>
endobj
trailer
<< /Root 4 0 R /Info 3 0 R >>
%%EOF`

func TestExtractPDF_IgnoresEmbeddedGraphicMetadataBeforeRealInfoDict(t *testing.T) {
	path := writePDFFixture(t, testPDFFixtureWithDecoyObjects)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "Think Like a CTO" {
		t.Errorf("Title = %q, want %q (the real Info dict's title, not the decoy embedded graphic's)", result.Title, "Think Like a CTO")
	}
	if result.Author != "Alan Williamson" {
		t.Errorf("Author = %q, want %q (the real Info dict's author, not the decoy embedded graphic's)", result.Author, "Alan Williamson")
	}
	if result.Year != "2023" {
		t.Errorf("Year = %q, want 2023", result.Year)
	}
}

// TestExtractPDF_NoTrailerLeavesTitleAuthorEmptyButStillFindsYear reproduces
// a second real incident: a book PDF with no locatable Info dictionary (here,
// no trailer at all) but MANY internal outline-bookmark "/Title" entries (one
// per chapter/section) scattered through the file. A whole-file scan for
// /Title would grab one of those bookmark titles and confidently report it
// as high-confidence "Metadata" -- which then blocks the filename heuristic
// parser from ever running, since a non-empty Title already "won". Title and
// Author are therefore left empty (not whole-file-scanned) whenever the real
// Info dict can't be located, so heuristics get a clean chance to run
// instead. Subject and CreationDate are still whole-file-scanned in this
// case: unlike /Title, those two keys essentially never appear on bookmark
// or embedded-graphic objects in practice, so they remain safe.
func TestExtractPDF_NoTrailerLeavesTitleAuthorEmptyButStillFindsYear(t *testing.T) {
	fixture := "%PDF-1.4\n" +
		"10 0 obj\n<< /Title (1. Making Changes) >>\nendobj\n" +
		"11 0 obj\n<< /Title (Retrospective) >>\nendobj\n" +
		"12 0 obj\n<< /Title (Index) >>\nendobj\n" +
		"1 0 obj\n<< /CreationDate (D:19510101000000) >>\nendobj\n" +
		"%%EOF"
	path := writePDFFixture(t, fixture)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "" {
		t.Errorf("Title = %q, want empty (must not pick up a bookmark title when the real Info dict can't be located)", result.Title)
	}
	if result.Author != "" {
		t.Errorf("Author = %q, want empty", result.Author)
	}
	if result.Year != "1951" {
		t.Errorf("Year = %q, want 1951 (CreationDate is still safe to whole-file-scan)", result.Year)
	}
}

// TestExtractPDF_UsesLatestIncrementalUpdateOfInfoObject reproduces a
// second real-world shape: a PDF edited by an annotation/signing/metadata
// tool after creation, which appends an incremental update (a new revision
// of object 3, plus a new trailer) rather than rewriting the file. The
// object-body lookup must use the LAST "3 ... obj ... endobj" block for
// consistency with "last trailer wins" -- otherwise it would confidently
// return the pre-edit (stale) title/author.
const testPDFFixtureWithIncrementalUpdate = `%PDF-1.4
3 0 obj
<< /Title (Old Title Before Edit) /Author (Old Author) >>
endobj
trailer
<< /Root 4 0 R /Info 3 0 R >>
%%EOF
3 0 obj
<< /Title (New Title After Edit) /Author (New Author) >>
endobj
trailer
<< /Root 4 0 R /Info 3 0 R >>
%%EOF`

func TestExtractPDF_UsesLatestIncrementalUpdateOfInfoObject(t *testing.T) {
	path := writePDFFixture(t, testPDFFixtureWithIncrementalUpdate)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "New Title After Edit" {
		t.Errorf("Title = %q, want %q (the latest incremental update, not the superseded original)", result.Title, "New Title After Edit")
	}
	if result.Author != "New Author" {
		t.Errorf("Author = %q, want %q (the latest incremental update, not the superseded original)", result.Author, "New Author")
	}
}

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

func TestExtractPDF_UsesXRefStreamWithInfoEvenWhenNotLastInFileOrder(t *testing.T) {
	// The XRef-stream analog of the test above: a linearized PDF using
	// PDF 1.5+ cross-reference streams instead of classic trailers has
	// the exact same failure shape -- the first (byte-order) XRef stream
	// object has /Info, the last (byte-order) one, associated with the
	// tail/base cross-reference data, does not. This is arguably the more
	// common real-world shape, since most modern linearized PDFs are
	// 1.5+ and use XRef streams rather than classic trailers.
	fixture := "%PDF-1.5\n" +
		"3 0 obj\n<< /Title (XRef Stream Linearized Book) /Author (Some Author) >>\nendobj\n" +
		"20 0 obj\n<< /Type /XRef /Info 3 0 R /Root 4 0 R /Size 21 /W [1 1 1] /Length 3 >>\nstream\nabc\nendstream\nendobj\n%%EOF\n" +
		"21 0 obj\n<< /Type /XRef /Root 4 0 R /Size 22 /W [1 1 1] /Length 3 >>\nstream\ndef\nendstream\nendobj\n%%EOF"
	path := writePDFFixture(t, fixture)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "XRef Stream Linearized Book" {
		t.Errorf("Title = %q, want %q (the XRef stream that actually has /Info, not the byte-order-last one)", result.Title, "XRef Stream Linearized Book")
	}
	if result.Author != "Some Author" {
		t.Errorf("Author = %q, want %q", result.Author, "Some Author")
	}
}

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

	called := false
	renderPDFPageAsCoverFunc = func(data []byte, pageNum int) ([]byte, string, bool) {
		called = true
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
	if !called {
		// Without this check, deleting the undecodable-image fallback
		// entirely would still pass this test via findPDFCover's
		// whole-file scan alone -- this pins that the fallback chain
		// (attempt PDFium render, then fall through on failure) is what
		// actually ran, not a coincidental pass.
		t.Fatal("renderPDFPageAsCoverFunc was not called; this test doesn't exercise the undecodable-image fallback at all")
	}
	if !ok {
		t.Fatal("findPDFCoverPageAware ok=false, want true (whole-file fallback should still find the DCTDecode image)")
	}
	if contentType != "image/jpeg" || string(imageBytes) != string(jpegData) {
		t.Errorf("got %q/%q, want the whole-file-scan-found DCTDecode image", imageBytes, contentType)
	}
}

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

func TestExtractPDF_HexStringTitleAndAuthorCompressedInsideObjStm(t *testing.T) {
	// Reproduces the exact real "Domain-Driven Design in PHP" shape end
	// to end: the Info dictionary is compressed inside an ObjStm (Task
	// 1's fix) AND its /Title and /Author are hex strings (Task 2's fix)
	// -- both problems in the same book, not just each in isolation.
	titleHex := fmt.Sprintf("%x", utf16BEBytes("Domain-Driven Design in PHP"))
	authorHex := fmt.Sprintf("%x", utf16BEBytes("Carlos Buenosvinos, Christian Soronellas and Keyvan Akbary"))

	infoObj := "<</Title<" + titleHex + ">/Author<" + authorHex + ">>>"
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
	if result.Title != "Domain-Driven Design in PHP" {
		t.Errorf("Title = %q, want %q (hex-string value inside an ObjStm-compressed Info dict)", result.Title, "Domain-Driven Design in PHP")
	}
	if result.Author != "Carlos Buenosvinos, Christian Soronellas and Keyvan Akbary" {
		t.Errorf("Author = %q, want %q", result.Author, "Carlos Buenosvinos, Christian Soronellas and Keyvan Akbary")
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

func TestExtractPDF_TitleAuthorEmptyWhenInfoReferenceMissing(t *testing.T) {
	// A trailer exists but has no /Info entry at all -- same "can't confirm
	// the real Info dict" situation as no trailer at all.
	fixture := "%PDF-1.4\n1 0 obj\n<< /Title (Decoy Bookmark) /Author (Someone) /CreationDate (D:20200101000000) >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF"
	path := writePDFFixture(t, fixture)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "" || result.Author != "" {
		t.Errorf("Title/Author = %q/%q, want both empty (no /Info reference means the real Info dict can't be confirmed)", result.Title, result.Author)
	}
	if result.Year != "2020" {
		t.Errorf("Year = %q, want 2020", result.Year)
	}
}

func TestExtractPDF_TitleAuthorEmptyWhenInfoObjectMissing(t *testing.T) {
	// The trailer references an object number that doesn't exist anywhere
	// in the file (e.g. a corrupted/truncated PDF).
	fixture := "%PDF-1.4\n1 0 obj\n<< /Title (Decoy Bookmark) /Author (Someone) /CreationDate (D:20200101000000) >>\nendobj\ntrailer\n<< /Root 1 0 R /Info 99 0 R >>\n%%EOF"
	path := writePDFFixture(t, fixture)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "" || result.Author != "" {
		t.Errorf("Title/Author = %q/%q, want both empty (the referenced Info object doesn't exist, so it can't be confirmed)", result.Title, result.Author)
	}
	if result.Year != "2020" {
		t.Errorf("Year = %q, want 2020", result.Year)
	}
}

func TestExtractPDF_NoMetadata(t *testing.T) {
	path := writePDFFixture(t, "%PDF-1.4\n1 0 obj\n<< >>\nendobj\n%%EOF")

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "" || result.Author != "" || result.Year != "" {
		t.Errorf("expected empty result for metadata-free PDF, got %+v", result)
	}
}

// TestExtractPDF_PlaceholderTitleTreatedAsUnresolved reproduces a real
// incident: a Pragmatic Bookshelf-produced PDF whose real, located Info
// dict has a literal /Title of "Untitled" (a leftover default from
// whatever tool generated the file) plus a legitimate Subject and
// CreationDate. Reporting "Untitled" as resolved SourceMetadata blocks the
// filename heuristic parser from ever running for Title (it only runs for
// fields that come back empty), so the placeholder string survives all the
// way to the final filename. Treating it as not-found lets heuristics
// supply the real title instead.
func TestExtractPDF_PlaceholderTitleTreatedAsUnresolved(t *testing.T) {
	fixture := "%PDF-1.4\n" +
		"1 0 obj\n<< /Title (Untitled) /Subject (IT eBooks) /CreationDate (D:20130710000000) >>\nendobj\n" +
		"trailer\n<< /Root 1 0 R /Info 1 0 R >>\n%%EOF"
	path := writePDFFixture(t, fixture)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "" {
		t.Errorf("Title = %q, want empty (placeholder \"Untitled\" must not be reported as resolved metadata)", result.Title)
	}
	if result.Subject != "IT eBooks" {
		t.Errorf("Subject = %q, want IT eBooks (unrelated fields must still resolve)", result.Subject)
	}
	if result.Year != "2013" {
		t.Errorf("Year = %q, want 2013", result.Year)
	}
}

func TestExtractPDF_FindsCoverImage(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	pdf := "%PDF-1.4\n" +
		"1 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Width 100 /Height 150 /Length 16 >>\nstream\n" +
		string(jpegData) + "\nendstream\nendobj\n"
	path := writePDFFixture(t, pdf)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if string(result.CoverBytes) != string(jpegData) {
		t.Errorf("CoverBytes = %q, want %q", result.CoverBytes, jpegData)
	}
	if result.CoverContentType != "image/jpeg" {
		t.Errorf("CoverContentType = %q, want image/jpeg", result.CoverContentType)
	}
}

func TestExtractPDF_NoCoverLeavesFieldEmpty(t *testing.T) {
	path := writePDFFixture(t, "%PDF-1.4\n1 0 obj\n<< /Title (Foo) >>\nendobj\n")

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.CoverBytes != nil {
		t.Errorf("CoverBytes = %v, want nil", result.CoverBytes)
	}
}

func TestExtractPDF_PageAwareCoverPrefersPageOrderOverByteOrder(t *testing.T) {
	// Page 2's image object (5 0 obj) appears EARLIER in the file's byte
	// order than page 1's image object (7 0 obj) -- reproducing a real
	// case where file object order doesn't match page order. The
	// page-aware walk must still pick page 1's image, not whichever
	// happens to come first in the file.
	page1JPEG := []byte("\xFF\xD8\xFFpage1cover")
	page2JPEG := []byte("\xFF\xD8\xFFpage2diagram")
	pdf := "%PDF-1.4\n" +
		"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 17 >>\nstream\n" + string(page2JPEG) + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 15 >>\nstream\n" + string(page1JPEG) + "\nendstream\nendobj\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 7 0 R >> >> >>\nendobj\n" +
		"4 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 5 0 R >> >> >>\nendobj\n" +
		"trailer\n<< /Root 1 0 R >>\n%%EOF"
	path := writePDFFixture(t, pdf)

	result, err := extractPDF(path, 10)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if string(result.CoverBytes) != string(page1JPEG) {
		t.Errorf("CoverBytes = %q, want page 1's cover %q (not page 2's, even though it appears first in file byte order)", result.CoverBytes, page1JPEG)
	}
}

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
