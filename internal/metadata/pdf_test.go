package metadata

import (
	"bytes"
	"os"
	"path/filepath"
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
