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

	result, err := extractPDF(path)
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

	result, err := extractPDF(path)
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

	result, err := extractPDF(path)
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

func TestExtractPDF_NoMetadata(t *testing.T) {
	path := writePDFFixture(t, "%PDF-1.4\n1 0 obj\n<< >>\nendobj\n%%EOF")

	result, err := extractPDF(path)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.Title != "" || result.Author != "" || result.Year != "" {
		t.Errorf("expected empty result for metadata-free PDF, got %+v", result)
	}
}
