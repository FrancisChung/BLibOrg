package metadata

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

const testContainerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

func writeEpubFixture(t *testing.T, opfXML string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "book.epub")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create epub fixture: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	w1, _ := zw.Create("META-INF/container.xml")
	w1.Write([]byte(testContainerXML))
	w2, _ := zw.Create("OEBPS/content.opf")
	w2.Write([]byte(opfXML))
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return path
}

func TestExtractEpub(t *testing.T) {
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Foundation</dc:title>
    <dc:creator opf:role="aut" xmlns:opf="http://www.idpf.org/2007/opf">Isaac Asimov</dc:creator>
    <dc:date>1951-01-01</dc:date>
    <dc:subject>Sci-Fi</dc:subject>
  </metadata>
</package>`
	path := writeEpubFixture(t, opf)

	result, err := extractEpub(path)
	if err != nil {
		t.Fatalf("extractEpub returned error: %v", err)
	}
	if result.Title != "Foundation" {
		t.Errorf("Title = %q, want Foundation", result.Title)
	}
	if result.Author != "Isaac Asimov" {
		t.Errorf("Author = %q, want Isaac Asimov", result.Author)
	}
	if result.Year != "1951" {
		t.Errorf("Year = %q, want 1951", result.Year)
	}
	if result.Subject != "Sci-Fi" {
		t.Errorf("Subject = %q, want Sci-Fi", result.Subject)
	}
}

func TestExtractEpub_MissingContainer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.epub")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	zw := zip.NewWriter(f)
	zw.Close()
	f.Close()

	if _, err := extractEpub(path); err == nil {
		t.Error("expected error for epub missing META-INF/container.xml, got nil")
	}
}

// writeEpubFixtureWithCover builds an epub fixture like writeEpubFixture,
// plus one extra zip entry at coverZipPath (the full in-zip path, e.g.
// "OEBPS/images/cover.jpg") containing coverData.
func writeEpubFixtureWithCover(t *testing.T, opfXML, coverZipPath string, coverData []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "book.epub")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create epub fixture: %v", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	w1, _ := zw.Create("META-INF/container.xml")
	w1.Write([]byte(testContainerXML))
	w2, _ := zw.Create("OEBPS/content.opf")
	w2.Write([]byte(opfXML))
	w3, _ := zw.Create(coverZipPath)
	w3.Write(coverData)
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return path
}

func TestExtractEpub_FindsCoverImage(t *testing.T) {
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Foundation</dc:title>
  </metadata>
  <manifest>
    <item id="cover-img" href="images/cover.jpg" media-type="image/jpeg" properties="cover-image"/>
  </manifest>
</package>`
	coverBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'f', 'a', 'k', 'e'}
	path := writeEpubFixtureWithCover(t, opf, "OEBPS/images/cover.jpg", coverBytes)

	result, err := extractEpub(path)
	if err != nil {
		t.Fatalf("extractEpub returned error: %v", err)
	}
	if string(result.CoverBytes) != string(coverBytes) {
		t.Errorf("CoverBytes = %v, want %v", result.CoverBytes, coverBytes)
	}
	if result.CoverContentType != "image/jpeg" {
		t.Errorf("CoverContentType = %q, want image/jpeg", result.CoverContentType)
	}
}

func TestExtractEpub_NoCoverLeavesFieldEmpty(t *testing.T) {
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Foundation</dc:title>
  </metadata>
</package>`
	path := writeEpubFixture(t, opf)

	result, err := extractEpub(path)
	if err != nil {
		t.Fatalf("extractEpub returned error: %v", err)
	}
	if result.CoverBytes != nil {
		t.Errorf("CoverBytes = %v, want nil", result.CoverBytes)
	}
}
