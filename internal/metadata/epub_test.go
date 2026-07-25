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

// writeEpubFixtureWithFiles builds an epub fixture like writeEpubFixture,
// plus each entry in files (zip path -> raw bytes) -- used by tests that
// need more than one extra zip entry (e.g. a spine document plus its
// embedded image), unlike writeEpubFixtureWithCover's single extra entry.
func writeEpubFixtureWithFiles(t *testing.T, opfXML string, files map[string][]byte) string {
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
	for zipPath, data := range files {
		w, _ := zw.Create(zipPath)
		w.Write(data)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return path
}

func TestExtractEpub_FallsBackToFirstSpineImageWhenNoCoverDeclared(t *testing.T) {
	// Reproduces the real "Cloud Native Microservices Cookbook" bug: no
	// EPUB3 or EPUB2 cover convention anywhere, but the first spine
	// document is a near-empty page whose entire body is one <img>.
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Some Book</dc:title>
  </metadata>
  <manifest>
    <item id="page1" href="page1.html" media-type="application/xhtml+xml"/>
    <item id="img1" href="cover-like.jpg" media-type="image/jpeg"/>
  </manifest>
  <spine>
    <itemref idref="page1"/>
  </spine>
</package>`
	pageHTML := `<html><body><div style="text-align:center"><img src="cover-like.jpg"/></div></body></html>`
	coverBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 'f', 'a', 'k', 'e'}
	path := writeEpubFixtureWithFiles(t, opf, map[string][]byte{
		"OEBPS/page1.html":     []byte(pageHTML),
		"OEBPS/cover-like.jpg": coverBytes,
	})

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

func TestExtractEpub_FirstSpineImageGuessesMediaTypeWhenNotInManifest(t *testing.T) {
	// A more malformed case than the primary fallback test: the <img>'s
	// target isn't declared in the manifest at all, so the media-type
	// must be guessed from the file extension instead.
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Some Book</dc:title>
  </metadata>
  <manifest>
    <item id="page1" href="page1.html" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="page1"/>
  </spine>
</package>`
	pageHTML := `<html><body><img src="undeclared.png"/></body></html>`
	coverBytes := []byte{0x89, 'P', 'N', 'G', 'f', 'a', 'k', 'e'}
	path := writeEpubFixtureWithFiles(t, opf, map[string][]byte{
		"OEBPS/page1.html":     []byte(pageHTML),
		"OEBPS/undeclared.png": coverBytes,
	})

	result, err := extractEpub(path)
	if err != nil {
		t.Fatalf("extractEpub returned error: %v", err)
	}
	if string(result.CoverBytes) != string(coverBytes) {
		t.Errorf("CoverBytes = %v, want %v", result.CoverBytes, coverBytes)
	}
	if result.CoverContentType != "image/png" {
		t.Errorf("CoverContentType = %q, want image/png (guessed from the .png extension)", result.CoverContentType)
	}
}

func TestExtractEpub_FirstSpineImageResolvesRelativeToSpineDocumentNotOPF(t *testing.T) {
	// A common real-world layout (Sigil/calibre-authored EPUBs): the OPF
	// sits at "OEBPS/content.opf", but the spine document and its image
	// live one directory deeper, in "OEBPS/Text/" and "OEBPS/Images/"
	// respectively, referenced from the spine document via a
	// "../Images/..." relative path. The image's in-zip path must be
	// resolved relative to the SPINE DOCUMENT's own directory
	// ("OEBPS/Text/"), not the OPF's ("OEBPS/") -- those differ here, so
	// resolving against the wrong base would silently fail to find the
	// image (a regression this test would catch that the flat single-
	// directory fixtures above cannot, since their spine doc and OPF
	// happen to share a directory).
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Some Book</dc:title>
  </metadata>
  <manifest>
    <item id="page1" href="Text/page1.xhtml" media-type="application/xhtml+xml"/>
    <item id="img1" href="Images/cover.jpg" media-type="image/jpeg"/>
  </manifest>
  <spine>
    <itemref idref="page1"/>
  </spine>
</package>`
	pageHTML := `<html><body><img src="../Images/cover.jpg"/></body></html>`
	coverBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 'f', 'a', 'k', 'e'}
	path := writeEpubFixtureWithFiles(t, opf, map[string][]byte{
		"OEBPS/Text/page1.xhtml": []byte(pageHTML),
		"OEBPS/Images/cover.jpg": coverBytes,
	})

	result, err := extractEpub(path)
	if err != nil {
		t.Fatalf("extractEpub returned error: %v", err)
	}
	if string(result.CoverBytes) != string(coverBytes) {
		t.Errorf("CoverBytes = %v, want %v (image resolved relative to the spine document's directory)", result.CoverBytes, coverBytes)
	}
	if result.CoverContentType != "image/jpeg" {
		t.Errorf("CoverContentType = %q, want image/jpeg", result.CoverContentType)
	}
}

func TestExtractEpub_CombinedNoCoverAndPlaceholderMetadata(t *testing.T) {
	// Reproduces the exact real-world bug shape end to end: a calibre
	// conversion with no OPF cover convention at all (cover fallback,
	// this file), AND a bare-numeric placeholder title plus "Unknown"
	// author (placeholder blanking, a separate fix) -- both problems
	// present in the same book, not just each in isolation.
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>728310488</dc:title>
    <dc:creator>Unknown</dc:creator>
  </metadata>
  <manifest>
    <item id="page1" href="page1.html" media-type="application/xhtml+xml"/>
    <item id="img1" href="cover-like.jpg" media-type="image/jpeg"/>
  </manifest>
  <spine>
    <itemref idref="page1"/>
  </spine>
</package>`
	pageHTML := `<html><body><div style="text-align:center"><img src="cover-like.jpg"/></div></body></html>`
	coverBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 'f', 'a', 'k', 'e'}
	path := writeEpubFixtureWithFiles(t, opf, map[string][]byte{
		"OEBPS/page1.html":     []byte(pageHTML),
		"OEBPS/cover-like.jpg": coverBytes,
	})

	result, err := extractEpub(path)
	if err != nil {
		t.Fatalf("extractEpub returned error: %v", err)
	}
	if result.Title != "" {
		t.Errorf("Title = %q, want empty (numeric placeholder blanked)", result.Title)
	}
	if result.Author != "" {
		t.Errorf("Author = %q, want empty (\"Unknown\" placeholder blanked)", result.Author)
	}
	if string(result.CoverBytes) != string(coverBytes) {
		t.Errorf("CoverBytes = %v, want %v (spine-image fallback still finds the cover)", result.CoverBytes, coverBytes)
	}
	if result.CoverContentType != "image/jpeg" {
		t.Errorf("CoverContentType = %q, want image/jpeg", result.CoverContentType)
	}
}

func TestExtractEpub_NumericTitlePlaceholderTreatedAsUnresolved(t *testing.T) {
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>728310488</dc:title>
    <dc:creator>Real Author</dc:creator>
  </metadata>
</package>`
	path := writeEpubFixture(t, opf)

	result, err := extractEpub(path)
	if err != nil {
		t.Fatalf("extractEpub returned error: %v", err)
	}
	if result.Title != "" {
		t.Errorf("Title = %q, want empty (a bare numeric ID is a known placeholder, not a real title)", result.Title)
	}
	if result.Author != "Real Author" {
		t.Errorf("Author = %q, want %q (unaffected by the Title placeholder check)", result.Author, "Real Author")
	}
}

func TestExtractEpub_UnknownAuthorPlaceholderTreatedAsUnresolved(t *testing.T) {
	tests := []struct {
		name   string
		author string
	}{
		{"lowercase", "unknown"},
		{"uppercase", "UNKNOWN"},
		{"surrounding whitespace", "  Unknown  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Real Title</dc:title>
    <dc:creator>` + tt.author + `</dc:creator>
  </metadata>
</package>`
			path := writeEpubFixture(t, opf)

			result, err := extractEpub(path)
			if err != nil {
				t.Fatalf("extractEpub returned error: %v", err)
			}
			if result.Author != "" {
				t.Errorf("Author = %q, want empty (%q is a known placeholder, not a real author)", result.Author, tt.author)
			}
			if result.Title != "Real Title" {
				t.Errorf("Title = %q, want %q (unaffected by the Author placeholder check)", result.Title, "Real Title")
			}
		})
	}
}
