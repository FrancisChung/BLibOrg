package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtract_DispatchesByExtension(t *testing.T) {
	epubPath := writeEpubFixture(t, `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:title>EpubTitle</dc:title></metadata></package>`)
	result, err := Extract(epubPath, nil)
	if err != nil {
		t.Fatalf("Extract(.epub) error: %v", err)
	}
	if result.Title != "EpubTitle" {
		t.Errorf("Extract(.epub) Title = %q", result.Title)
	}

	pdfPath := writePDFFixture(t, `%PDF-1.4
1 0 obj
<< /Title (PdfTitle) >>
endobj
trailer
<< /Root 1 0 R /Info 1 0 R >>
%%EOF`)
	result, err = Extract(pdfPath, nil)
	if err != nil {
		t.Fatalf("Extract(.pdf) error: %v", err)
	}
	if result.Title != "PdfTitle" {
		t.Errorf("Extract(.pdf) Title = %q", result.Title)
	}

	mobiPath := writeMobiFixture(t, "MobiTitle", "", "", "")
	result, err = Extract(mobiPath, nil)
	if err != nil {
		t.Fatalf("Extract(.mobi) error: %v", err)
	}
	if result.Title != "MobiTitle" {
		t.Errorf("Extract(.mobi) Title = %q", result.Title)
	}

	// .azw3 uses the same extractor as .mobi -- reuse the mobi fixture bytes
	// under a .azw3 name to confirm dispatch, not re-derive the format.
	azw3Path := filepath.Join(t.TempDir(), "book.azw3")
	data, err := os.ReadFile(mobiPath)
	if err != nil {
		t.Fatalf("read mobi fixture: %v", err)
	}
	if err := os.WriteFile(azw3Path, data, 0644); err != nil {
		t.Fatalf("write azw3 fixture: %v", err)
	}
	result, err = Extract(azw3Path, nil)
	if err != nil {
		t.Fatalf("Extract(.azw3) error: %v", err)
	}
	if result.Title != "MobiTitle" {
		t.Errorf("Extract(.azw3) Title = %q", result.Title)
	}

	if _, err := Extract(filepath.Join(t.TempDir(), "book.txt"), nil); err == nil {
		t.Error("expected error for unsupported extension, got nil")
	}
}

func TestExtract_CleansTitleAndAuthor(t *testing.T) {
	epubPath := writeEpubFixture(t, `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:title>Foundation.</dc:title>
<dc:creator>Bruce Eckel;Svetlana Isakova;</dc:creator>
</metadata></package>`)
	result, err := Extract(epubPath, nil)
	if err != nil {
		t.Fatalf("Extract(.epub) error: %v", err)
	}
	if result.Title != "Foundation" {
		t.Errorf("Title = %q, want trailing period stripped", result.Title)
	}
	if result.Author != "Bruce Eckel, Svetlana Isakova" {
		t.Errorf("Author = %q, want trailing semicolon stripped and internal semicolon normalized to comma", result.Author)
	}
}

func TestExtract_FormatsTitle(t *testing.T) {
	epubPath := writeEpubFixture(t, `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:title>high-performance_systems_for_the_cloud</dc:title></metadata></package>`)
	result, err := Extract(epubPath, []string{"High-Performance"})
	if err != nil {
		t.Fatalf("Extract(.epub) error: %v", err)
	}
	want := "High-Performance Systems for the Cloud"
	if result.Title != want {
		t.Errorf("Title = %q, want %q", result.Title, want)
	}
}
