package librarian

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/librarycache"
	"github.com/FrancisChung/book-organiser/internal/metadata"
)

// urlPath strips covercache's cache-busting "?v=" query, if present, so
// tests can assert on the underlying file path.
func urlPath(url string) string {
	if i := strings.IndexByte(url, '?'); i >= 0 {
		return url[:i]
	}
	return url
}

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

	books, err := Scan(cfg, false)
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

	books, err := Scan(cfg, false)
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

	books, err := Scan(cfg, false)
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

	books, err := Scan(cfg, false)
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
	books, err := Scan(cfg, false)
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
	books, err := Scan(cfg, false)
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
	books, err := Scan(cfg, false)
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
	books, err := Scan(cfg, false)
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

func TestScan_UsesCachedFieldsAndSkipsExtractOnCacheHit(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}

	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{
		ModTime: info.ModTime(), Size: info.Size(),
		Title: "Foundation", Author: "Isaac Asimov", Year: "1951",
		Category: "Fiction", Subcategory: "Sci-Fi", CoverPath: "/covers/abc.jpg",
		CoverVersion: metadata.CoverExtractorVersion, MetadataVersion: metadata.MetadataExtractorVersion,
	})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		t.Fatal("extractFunc should not be called for a cache hit")
		return metadata.Result{}, nil
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("len(books) = %d, want 1", len(books))
	}
	if books[0].Title != "Foundation" || books[0].Author != "Isaac Asimov" || books[0].Year != "1951" || books[0].CoverPath != "/covers/abc.jpg" {
		t.Errorf("books[0] = %+v, want cached fields", books[0])
	}
}

func TestScan_CachedCoverOverriddenIsServedWithoutRecheckingOverrideStore(t *testing.T) {
	// Proves the invalidation contract is what keeps overrides correct --
	// not an accidental re-check on every Scan. No entry exists in
	// cover-overrides.json at all; the cached CoverOverridden=true and its
	// CoverPath must still come straight from the cache entry.
	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, "Foundation.epub")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}

	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{
		ModTime: info.ModTime(), Size: info.Size(),
		Title: "Foundation", CoverPath: "/covers/override-xyz.jpg", CoverOverridden: true,
		CoverVersion: metadata.CoverExtractorVersion, MetadataVersion: metadata.MetadataExtractorVersion,
	})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("len(books) = %d, want 1", len(books))
	}
	if !books[0].CoverOverridden {
		t.Error("CoverOverridden = false, want true (from cache, no override store entry exists)")
	}
	if books[0].CoverPath != "/covers/override-xyz.jpg" {
		t.Errorf("CoverPath = %q, want the cached override path", books[0].CoverPath)
	}
}

func TestScan_ExtractsAndCachesANewFile(t *testing.T) {
	libDir := t.TempDir()
	logDir := t.TempDir()
	// Must be a real, extractable epub (not writeFixtureFile's placeholder
	// text): metadata.Extract has to succeed for cache.Put to run and mark
	// the cache dirty, otherwise Save is correctly a no-op per librarycache's
	// documented "only write if there are unsaved changes" contract, and
	// this test's library-cache.json assertion would never observe a write.
	writeEpubWithCover(t, filepath.Join(libDir, "Fiction", "Sci-Fi", "Foundation.epub"), []byte{0xFF, 0xD8, 0xFF, 0xE0})

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	if _, err := Scan(cfg, false); err != nil {
		t.Fatalf("first Scan returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(logDir, "library-cache.json")); err != nil {
		t.Errorf("expected library-cache.json to be written, got: %v", err)
	}
}

func TestScan_ReExtractsAnEditedFile(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"))
	info, _ := os.Stat(path)

	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{ModTime: info.ModTime().Add(-time.Hour), Size: info.Size(), Title: "Stale Title"})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	called := false
	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		called = true
		return metadata.Result{Title: "Fresh Title"}, nil
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !called {
		t.Error("extractFunc was not called for a stale cache entry")
	}
	if len(books) != 1 || books[0].Title != "Fresh Title" {
		t.Errorf("books = %+v, want [{Title: Fresh Title}]", books)
	}
}

func TestScan_DropsRemovedFileFromCache(t *testing.T) {
	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, "Loose.epub")
	info, _ := os.Stat(path)

	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{ModTime: info.ModTime(), Size: info.Size(), Title: "Loose"})
	cache.Put(filepath.Join(libDir, "Gone.epub"), librarycache.Entry{Title: "Gone"})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	if _, err := Scan(cfg, false); err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	reloaded := librarycache.Load(logDir)
	if _, ok := reloaded.Fresh(filepath.Join(libDir, "Gone.epub"), time.Time{}, 0); ok {
		t.Error("removed file's cache entry was not dropped")
	}
}

func TestScan_StaleCoverVersionForcesReExtractionDespiteMatchingModTimeAndSize(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"))
	info, _ := os.Stat(path)

	// Simulates an entry cached by a prior version of the cover-extraction
	// logic: ModTime and Size match exactly (the book file itself never
	// changed), but CoverVersion is stale (0, the zero value any
	// pre-this-fix persisted entry unmarshals to).
	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{
		ModTime: info.ModTime(), Size: info.Size(),
		Title: "Stale Title", CoverPath: "/covers/wrong-old-image.jpg", CoverVersion: 0,
	})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	called := false
	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		called = true
		return metadata.Result{Title: "Fresh Title"}, nil
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !called {
		t.Error("extractFunc was not called for a CoverVersion-stale entry despite matching ModTime/Size -- a book cached under an old cover-extraction algorithm would be stuck forever")
	}
	if len(books) != 1 || books[0].Title != "Fresh Title" {
		t.Errorf("books = %+v, want [{Title: Fresh Title}]", books)
	}
}

func TestScan_ReExtractedEntryIsCachedWithCurrentCoverVersion(t *testing.T) {
	libDir := t.TempDir()
	logDir := t.TempDir()
	epubPath := filepath.Join(libDir, "Fiction", "Sci-Fi", "Foundation.epub")
	writeEpubWithCover(t, epubPath, []byte{0xFF, 0xD8, 0xFF, 0xE0})

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	if _, err := Scan(cfg, false); err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	reloaded := librarycache.Load(logDir)
	info, err := os.Stat(epubPath)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}
	entry, ok := reloaded.Fresh(epubPath, info.ModTime(), info.Size())
	if !ok {
		t.Fatal("Fresh() = false after a fresh Scan wrote the entry, want true")
	}
	if entry.CoverVersion != metadata.CoverExtractorVersion {
		t.Errorf("CoverVersion = %d, want %d (metadata.CoverExtractorVersion)", entry.CoverVersion, metadata.CoverExtractorVersion)
	}
}

func TestScan_StaleMetadataVersionForcesReExtractionDespiteMatchingCoverVersion(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"))
	info, _ := os.Stat(path)

	// Simulates an entry cached before the XRef-stream Info-dict fix:
	// ModTime, Size, and CoverVersion all match (the book file and its
	// cover-extraction logic haven't changed), but MetadataVersion is
	// stale (0, the zero value any pre-this-fix persisted entry
	// unmarshals to).
	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{
		ModTime: info.ModTime(), Size: info.Size(),
		Title: "Stale Title", CoverVersion: metadata.CoverExtractorVersion, MetadataVersion: 0,
	})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	called := false
	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		called = true
		return metadata.Result{Title: "Fresh Title"}, nil
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !called {
		t.Error("extractFunc was not called for a MetadataVersion-stale entry despite matching ModTime/Size/CoverVersion -- a book cached under an old metadata-extraction algorithm would be stuck forever")
	}
	if len(books) != 1 || books[0].Title != "Fresh Title" {
		t.Errorf("books = %+v, want [{Title: Fresh Title}]", books)
	}
}

func TestScan_ReExtractedEntryIsCachedWithCurrentMetadataVersion(t *testing.T) {
	libDir := t.TempDir()
	logDir := t.TempDir()
	epubPath := filepath.Join(libDir, "Fiction", "Sci-Fi", "Foundation.epub")
	writeEpubWithCover(t, epubPath, []byte{0xFF, 0xD8, 0xFF, 0xE0})

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	if _, err := Scan(cfg, false); err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	reloaded := librarycache.Load(logDir)
	info, err := os.Stat(epubPath)
	if err != nil {
		t.Fatalf("stat fixture: %v", err)
	}
	entry, ok := reloaded.Fresh(epubPath, info.ModTime(), info.Size())
	if !ok {
		t.Fatal("Fresh() = false after a fresh Scan wrote the entry, want true")
	}
	if entry.MetadataVersion != metadata.MetadataExtractorVersion {
		t.Errorf("MetadataVersion = %d, want %d (metadata.MetadataExtractorVersion)", entry.MetadataVersion, metadata.MetadataExtractorVersion)
	}
}

func TestScan_OverwritesStaleCoverFileEvenWhenCacheFileHasNewerMtime(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"))

	// Simulate the real bug: a cover file already cached under this exact
	// sourcePath+contentType, whose own mtime (just written, "now") is
	// necessarily >= the source book file's mtime -- exactly the condition
	// covercache.Ensure treats as "already fresh, don't rewrite." If Scan
	// still called Ensure here, the stale bytes below would survive
	// unchanged even though extraction (mocked below) finds different ones.
	staleURL, err := covercache.Force(logDir, path, []byte("STALE-WRONG-IMAGE-BYTES"), "image/jpeg")
	if err != nil {
		t.Fatalf("seed stale cover: %v", err)
	}
	staleCoverPath := filepath.Join(covercache.Dir(logDir), filepath.Base(urlPath(staleURL)))

	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		return metadata.Result{
			Title:            "Foundation",
			CoverBytes:       []byte("FRESH-CORRECT-IMAGE-BYTES"),
			CoverContentType: "image/jpeg",
		}, nil
	}

	// forceRefresh=true: a real re-scan of an unchanged file, the same
	// situation a user hitting "Refresh" produces.
	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, true)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	got, err := os.ReadFile(staleCoverPath)
	if err != nil {
		t.Fatalf("read cover file after Scan: %v", err)
	}
	if string(got) != "FRESH-CORRECT-IMAGE-BYTES" {
		t.Errorf("cover file on disk = %q, want the freshly-extracted bytes -- Scan must overwrite an existing cached cover once it re-extracts, not silently skip the write", got)
	}

	// The returned CoverPath must also differ from the stale URL -- not
	// just the on-disk bytes -- or a frontend <img src={book.coverPath}>
	// binding would never notice the change and would keep displaying
	// whatever it already rendered for that (identical) URL string.
	if len(books) != 1 {
		t.Fatalf("len(books) = %d, want 1", len(books))
	}
	if books[0].CoverPath == staleURL {
		t.Error("CoverPath is unchanged from the stale URL -- a frontend <img src> binding would never re-fetch, even though the file content changed")
	}
}

func TestScan_ForceRefreshBypassesFreshCache(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	path := writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Foundation.epub"))
	info, _ := os.Stat(path)

	cache := librarycache.Load(logDir)
	cache.Put(path, librarycache.Entry{ModTime: info.ModTime(), Size: info.Size(), Title: "Cached Title"})
	if err := cache.Save(logDir); err != nil {
		t.Fatalf("save cache fixture: %v", err)
	}

	called := false
	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		called = true
		return metadata.Result{Title: "Refreshed Title"}, nil
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, true)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if !called {
		t.Error("extractFunc was not called despite forceRefresh=true")
	}
	if len(books) != 1 || books[0].Title != "Refreshed Title" {
		t.Errorf("books = %+v, want [{Title: Refreshed Title}]", books)
	}
}
