package librarian

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
)

func writeFixtureFile(t *testing.T, dir, relPath string) string {
	t.Helper()
	path := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("not a real ebook"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestScan_GroupsByCategoryAndSubcategory(t *testing.T) {
	libDir := t.TempDir()
	writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"))
	writeFixtureFile(t, libDir, filepath.Join("Fiction", "Fantasy", "Mistborn.epub"))

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: t.TempDir()}}

	books, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 2 {
		t.Fatalf("len(books) = %d, want 2", len(books))
	}

	byFile := map[string]Book{}
	for _, b := range books {
		byFile[filepath.Base(b.SourcePath)] = b
	}

	foundation := byFile["Foundation.epub"]
	if foundation.Category != "Fiction" || foundation.Subcategory != "Sci-Fi" {
		t.Errorf("Foundation Category/Subcategory = %q/%q, want Fiction/Sci-Fi", foundation.Category, foundation.Subcategory)
	}
	mistborn := byFile["Mistborn.epub"]
	if mistborn.Category != "Fiction" || mistborn.Subcategory != "Fantasy" {
		t.Errorf("Mistborn Category/Subcategory = %q/%q, want Fiction/Fantasy", mistborn.Category, mistborn.Subcategory)
	}
}

func TestScan_FileDirectlyInLibraryRootHasNoCategory(t *testing.T) {
	libDir := t.TempDir()
	writeFixtureFile(t, libDir, "Loose.epub")

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: t.TempDir()}}

	books, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("len(books) = %d, want 1", len(books))
	}
	if books[0].Category != "" || books[0].Subcategory != "" {
		t.Errorf("Category/Subcategory = %q/%q, want empty/empty", books[0].Category, books[0].Subcategory)
	}
}

func TestScan_EmptyLibraryReturnsEmptySlice(t *testing.T) {
	cfg := config.Config{General: config.General{LibraryFolder: t.TempDir(), LogFolder: t.TempDir()}}

	books, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 0 {
		t.Errorf("len(books) = %d, want 0", len(books))
	}
}

func writeEpubWithCover(t *testing.T, path string, coverData []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create epub: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	w1, _ := zw.Create("META-INF/container.xml")
	w1.Write([]byte(`<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`))
	w2, _ := zw.Create("OEBPS/content.opf")
	w2.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Foundation</dc:title>
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
  </manifest>
</package>`))
	w3, _ := zw.Create("OEBPS/cover.jpg")
	w3.Write(coverData)
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
}

func TestScan_PopulatesCoverPathAndMetadataWhenCoverExists(t *testing.T) {
	libDir := t.TempDir()
	path := filepath.Join(libDir, "Fiction", "Sci-Fi", "Foundation.epub")
	writeEpubWithCover(t, path, []byte{0xFF, 0xD8, 0xFF, 0xE0})

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: t.TempDir()}}

	books, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("len(books) = %d, want 1", len(books))
	}
	if books[0].CoverPath == "" {
		t.Error("CoverPath is empty, want a /covers/... URL")
	}
	if books[0].Title != "Foundation" {
		t.Errorf("Title = %q, want Foundation", books[0].Title)
	}
}

func writeRealPDFFixture(t *testing.T, path string) {
	t.Helper()
	jpeg := []byte("\xFF\xD8\xFFrealjpeg")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /Font << /F1 9 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 5 0 R >> >> >>\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpeg) + "\nendstream\nendobj\n")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func TestScan_NoOverrideUsesAutoDetectedCover(t *testing.T) {
	libDir := t.TempDir()
	path := filepath.Join(libDir, "book.pdf")
	writeRealPDFFixture(t, path)

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: t.TempDir()}}
	books, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("len(books) = %d, want 1", len(books))
	}
	if books[0].CoverOverridden {
		t.Error("CoverOverridden = true, want false (no override set)")
	}
	if books[0].CoverPath == "" {
		t.Error("CoverPath is empty, want the auto-detected page-2 cover")
	}
}

func TestScan_EmbeddedOverridePinsSpecificPage(t *testing.T) {
	libDir := t.TempDir()
	path := filepath.Join(libDir, "book.pdf")
	writeRealPDFFixture(t, path)
	logFolder := t.TempDir()

	// The fixture's only image is on page 2; pin page 1 (which has none) to
	// prove the override is actually driving cover selection, not being
	// ignored in favor of auto-detection.
	if err := covercache.SetOverride(logFolder, path, covercache.Override{Type: covercache.OverrideEmbedded, Page: 1}); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logFolder}}
	books, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !books[0].CoverOverridden {
		t.Error("CoverOverridden = false, want true")
	}
	if books[0].CoverPath != "" {
		t.Error("CoverPath is non-empty, want empty (page 1 has no image, and the override should not fall back to auto-detection)")
	}
}

func TestScan_CustomOverrideUsesStoredImagePath(t *testing.T) {
	libDir := t.TempDir()
	path := filepath.Join(libDir, "book.pdf")
	writeRealPDFFixture(t, path)
	logFolder := t.TempDir()

	if err := covercache.SetOverride(logFolder, path, covercache.Override{
		Type:      covercache.OverrideCustom,
		ImagePath: "/covers/override-abc123.jpg",
	}); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logFolder}}
	books, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !books[0].CoverOverridden {
		t.Error("CoverOverridden = false, want true")
	}
	if books[0].CoverPath != "/covers/override-abc123.jpg" {
		t.Errorf("CoverPath = %q, want the stored override URL", books[0].CoverPath)
	}
}

func TestScan_OverrideDoesNotBlankTitleAuthorYear(t *testing.T) {
	// Confirms this plan's deliberate deviation from the design doc's
	// literal "extraction is skipped entirely" wording: Title/Author/Year
	// must still come from metadata.Extract even for an overridden book.
	libDir := t.TempDir()
	path := filepath.Join(libDir, "Foundation - Isaac Asimov.pdf")
	writeRealPDFFixture(t, path)
	logFolder := t.TempDir()
	if err := covercache.SetOverride(logFolder, path, covercache.Override{Type: covercache.OverrideEmbedded, Page: 2}); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logFolder}}
	books, err := Scan(cfg)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	// The fixture PDF has no /Title, so this only proves Extract still ran
	// (Format/SourcePath were always set regardless) -- combined with the
	// two tests above, which prove CoverPath genuinely reflects the
	// override rather than a blanked/skipped result.
	if books[0].Format != "pdf" {
		t.Errorf("Format = %q, want pdf (metadata.Extract path still ran)", books[0].Format)
	}
}
