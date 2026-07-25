# EPUB Cover Fallback, Placeholder Metadata, and Library-View Heuristic Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix two confirmed bugs affecting calibre-converted EPUBs with no real cover/title/author metadata (like "Cloud Native Microservices Cookbook (2024) - Varun Yadav.epub"): no cover extracted when neither standard OPF cover convention is present, and garbage placeholder Title/Author values that block the filename-heuristic fallback -- which itself turns out to be missing entirely from the Library view's own scan path.

**Architecture:** Two independent fixes to `internal/metadata/epub.go` (a new spine-image cover fallback; placeholder-value blanking for Title/Author), plus a third fix to `internal/librarian/librarian.go` that wires the existing `internal/heuristics.Parse` filename fallback (already used by `internal/pipeline.Run`) into the Library view's own scan path, which currently lacks it entirely. A final task bumps both version constants so already-cached books self-heal.

**Tech Stack:** Go, `internal/metadata`'s existing dependency-free zip/XML/regex-based EPUB parser (no new dependencies), `internal/heuristics` (pre-existing, unchanged).

## Global Constraints

- No new external dependencies.
- The EPUB cover fallback (`findEpubFirstSpineImage`) is only used when `findEpubCoverItem` finds neither the EPUB3 (`properties="cover-image"`) nor EPUB2 (`<meta name="cover">`) convention -- it never overrides a properly-declared cover.
- The fallback never sets `CoverContentType` to an unknown/empty value -- if the image isn't declared in the manifest and its file extension isn't recognized, it gives up (`ok=false`) rather than guessing wrong.
- `librarian.Scan`'s new heuristic fallback only fills in Title/Author/Year fields that come back empty after extraction -- a non-empty metadata-sourced value always takes precedence, matching `internal/pipeline.Run`'s existing behavior exactly.
- `metadata.CoverExtractorVersion` bumps once (the EPUB cover fallback changes cover bytes for already-cached EPUBs).
- `metadata.MetadataExtractorVersion` bumps once (both the placeholder-blanking and the new `librarian.Scan` heuristic wiring change Title/Author/Year for already-cached books, of any format, not just EPUBs).

---

### Task 1: Fall back to the first spine document's image when no cover is declared

**Files:**
- Modify: `internal/metadata/epub.go`
- Test: `internal/metadata/epub_test.go`

**Interfaces:**
- Consumes: `findZipFile`, `epubPathJoin` (pre-existing, unchanged).
- Produces: `findEpubFirstSpineImage(r *zip.ReadCloser, opfFullPath string, p epubPackage) (zipPath, mediaType string, ok bool)` and `readEpubCoverBytes(r *zip.ReadCloser, zipPath, mediaType string, result *Result)`, both new. `epubPackage` gains a `Spine` field. No other task depends on these names.

- [ ] **Step 1: Write the failing tests**

Add this helper to `internal/metadata/epub_test.go`, after the existing `writeEpubFixtureWithCover` function:

```go
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
```

Add these two tests, after `TestExtractEpub_NoCoverLeavesFieldEmpty`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/ -run TestExtractEpub_FallsBackToFirstSpineImage -v` and `go test ./internal/metadata/ -run TestExtractEpub_FirstSpineImageGuessesMediaType -v`
Expected: both FAIL with `CoverBytes = [], want [...]` (no fallback exists yet, so `CoverBytes` stays nil for both fixtures).

- [ ] **Step 3: Implement the spine-image fallback**

In `internal/metadata/epub.go`, add `"regexp"` and `"strings"` to the import block, changing:

```go
import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)
```

to:

```go
import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)
```

Add a `Spine` field to `epubPackage`, changing:

```go
type epubPackage struct {
	Metadata struct {
		Title   string `xml:"title"`
		Creator string `xml:"creator"`
		Date    string `xml:"date"`
		Subject string `xml:"subject"`
		Meta    []struct {
			Name    string `xml:"name,attr"`
			Content string `xml:"content,attr"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest struct {
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			MediaType  string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"item"`
	} `xml:"manifest"`
}
```

to:

```go
type epubPackage struct {
	Metadata struct {
		Title   string `xml:"title"`
		Creator string `xml:"creator"`
		Date    string `xml:"date"`
		Subject string `xml:"subject"`
		Meta    []struct {
			Name    string `xml:"name,attr"`
			Content string `xml:"content,attr"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest struct {
		Items []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			MediaType  string `xml:"media-type,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"item"`
	} `xml:"manifest"`
	Spine struct {
		ItemRefs []struct {
			IDRef string `xml:"idref,attr"`
		} `xml:"itemref"`
	} `xml:"spine"`
}
```

Add a regex next to the top of the file (after the `epubPackage` type, before `findZipFile`):

```go
var epubImgSrcRe = regexp.MustCompile(`(?i)<img[^>]*\ssrc=["']([^"']*)["']`)
```

Add `findEpubFirstSpineImage` and `epubGuessMediaType` after the existing `splitEpubProperties` function:

```go
// findEpubFirstSpineImage is the fallback used when findEpubCoverItem
// finds neither the EPUB3 nor EPUB2 cover convention: many older,
// malformed, or auto-converted EPUBs (e.g. a calibre conversion with no
// cover metadata at all) still put the cover image alone on an
// otherwise-empty first page, so the first <img> tag in the first spine
// document is, in practice, the cover. Unlike findEpubCoverItem, this
// returns the fully-resolved in-zip path (not an OPF-relative href),
// since the image's path is computed relative to the spine document's
// own location, which may differ from the OPF's. Returns ok=false if the
// spine is empty, its first item can't be resolved to a manifest href,
// that document can't be opened, or it contains no <img> tag at all.
func findEpubFirstSpineImage(r *zip.ReadCloser, opfFullPath string, p epubPackage) (zipPath, mediaType string, ok bool) {
	if len(p.Spine.ItemRefs) == 0 {
		return "", "", false
	}
	firstID := p.Spine.ItemRefs[0].IDRef
	var spineHref string
	for _, item := range p.Manifest.Items {
		if item.ID == firstID {
			spineHref = item.Href
			break
		}
	}
	if spineHref == "" {
		return "", "", false
	}
	spineZipPath := epubPathJoin(opfFullPath, spineHref)
	sf, found := findZipFile(r, spineZipPath)
	if !found {
		return "", "", false
	}
	src, err := sf.Open()
	if err != nil {
		return "", "", false
	}
	defer src.Close()
	data, err := io.ReadAll(src)
	if err != nil {
		return "", "", false
	}
	m := epubImgSrcRe.FindSubmatch(data)
	if m == nil {
		return "", "", false
	}
	imgZipPath := epubPathJoin(spineZipPath, string(m[1]))

	for _, item := range p.Manifest.Items {
		if epubPathJoin(opfFullPath, item.Href) == imgZipPath {
			return imgZipPath, item.MediaType, true
		}
	}
	if guessed := epubGuessMediaType(imgZipPath); guessed != "" {
		return imgZipPath, guessed, true
	}
	return "", "", false
}

// epubGuessMediaType infers a media type from a zip path's file
// extension, for the rare case findEpubFirstSpineImage locates an <img>
// tag whose target isn't itself declared in the manifest (a more
// malformed EPUB than the manifest-declared common case). Returns "" for
// an unrecognized extension, so callers can treat that as "give up"
// rather than guessing wrong.
func epubGuessMediaType(zipPath string) string {
	switch strings.ToLower(path.Ext(zipPath)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	default:
		return ""
	}
}

// readEpubCoverBytes opens zipPath within r and, if successful, sets
// result.CoverBytes/CoverContentType -- the shared "open, read, assign"
// step both findEpubCoverItem's result and findEpubFirstSpineImage's
// fallback result go through once a candidate cover has been located.
func readEpubCoverBytes(r *zip.ReadCloser, zipPath, mediaType string, result *Result) {
	cfile, found := findZipFile(r, zipPath)
	if !found {
		return
	}
	crf, err := cfile.Open()
	if err != nil {
		return
	}
	defer crf.Close()
	data, err := io.ReadAll(crf)
	if err != nil {
		return
	}
	result.CoverBytes = data
	result.CoverContentType = mediaType
}
```

Replace `extractEpub`'s cover-resolution block, changing:

```go
	if href, mediaType, ok := findEpubCoverItem(p); ok {
		// href is relative to the OPF's own directory, not the zip root
		// (e.g. opf at "OEBPS/content.opf", href "images/cover.jpg" ->
		// "OEBPS/images/cover.jpg"). Zip entry names always use "/", so this
		// must use the "path" package, not "path/filepath" (which uses "\"
		// on Windows and would silently fail to match).
		coverZipPath := epubPathJoin(c.Rootfiles.Rootfile.FullPath, href)
		if cfile, found := findZipFile(r, coverZipPath); found {
			if crf, err := cfile.Open(); err == nil {
				if data, err := io.ReadAll(crf); err == nil {
					result.CoverBytes = data
					result.CoverContentType = mediaType
				}
				crf.Close()
			}
		}
	}
```

to:

```go
	if href, mediaType, ok := findEpubCoverItem(p); ok {
		// href is relative to the OPF's own directory, not the zip root
		// (e.g. opf at "OEBPS/content.opf", href "images/cover.jpg" ->
		// "OEBPS/images/cover.jpg"). Zip entry names always use "/", so this
		// must use the "path" package, not "path/filepath" (which uses "\"
		// on Windows and would silently fail to match).
		coverZipPath := epubPathJoin(c.Rootfiles.Rootfile.FullPath, href)
		readEpubCoverBytes(r, coverZipPath, mediaType, &result)
	} else if zipPath, mediaType, ok := findEpubFirstSpineImage(r, c.Rootfiles.Rootfile.FullPath, p); ok {
		readEpubCoverBytes(r, zipPath, mediaType, &result)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the two new ones and every pre-existing `extractEpub` test -- in particular `TestExtractEpub_FindsCoverImage`, whose EPUB3 `properties="cover-image"` fixture is found by `findEpubCoverItem` first, so the new fallback is never reached; and `TestExtractEpub_NoCoverLeavesFieldEmpty`, whose fixture has no `<spine>` at all, so `findEpubFirstSpineImage` correctly returns `ok=false` immediately).

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/epub.go internal/metadata/epub_test.go
git commit -m "Fall back to the first spine document's image when no EPUB cover is declared"
```

---

### Task 2: Treat placeholder EPUB Title/Author values as unresolved

**Files:**
- Modify: `internal/metadata/epub.go`
- Test: `internal/metadata/epub_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: nothing consumed by a later task.

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/metadata/epub_test.go`, after the two tests added in Task 1:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/ -run TestExtractEpub_NumericTitlePlaceholder -v` and `go test ./internal/metadata/ -run TestExtractEpub_UnknownAuthorPlaceholder -v`
Expected: both FAIL -- `Title = "728310488", want empty` and (all three subtests) `Author = "unknown"/"UNKNOWN"/"  Unknown  ", want empty`.

- [ ] **Step 3: Implement the placeholder checks**

In `internal/metadata/epub.go`, add two package-level vars after the `epubImgSrcRe` declaration added in Task 1:

```go
var epubPlaceholderTitleRe = regexp.MustCompile(`^[0-9]+$`)

var epubPlaceholderAuthors = map[string]bool{
	"unknown": true,
}
```

In `extractEpub`, change:

```go
	result := Result{
		Title:   p.Metadata.Title,
		Author:  p.Metadata.Creator,
		Subject: p.Metadata.Subject,
	}
	if year, ok := textutil.ExtractYear(p.Metadata.Date); ok {
		result.Year = year
	}
```

to:

```go
	result := Result{
		Title:   p.Metadata.Title,
		Author:  p.Metadata.Creator,
		Subject: p.Metadata.Subject,
	}
	if epubPlaceholderTitleRe.MatchString(strings.TrimSpace(result.Title)) {
		result.Title = ""
	}
	if epubPlaceholderAuthors[strings.ToLower(strings.TrimSpace(result.Author))] {
		result.Author = ""
	}
	if year, ok := textutil.ExtractYear(p.Metadata.Date); ok {
		result.Year = year
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -v`
Expected: PASS (all tests, including the new ones and every pre-existing `extractEpub` test -- in particular `TestExtractEpub`, whose fixture's Title "Foundation" and Author "Isaac Asimov" match neither placeholder pattern).

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/epub.go internal/metadata/epub_test.go
git commit -m "Treat placeholder EPUB Title/Author values as unresolved"
```

---

### Task 3: Fall back to filename heuristics in the Library view's scan

**Files:**
- Modify: `internal/librarian/librarian.go`
- Test: `internal/librarian/librarian_test.go`

**Interfaces:**
- Consumes: `heuristics.Parse(filenameStem string, knownJunkTags []string) heuristics.Result` (pre-existing, unchanged, from `internal/heuristics`), `cfg.Heuristics.KnownJunkTags` (pre-existing config field).
- Produces: nothing consumed by a later task.

- [ ] **Step 1: Write the failing tests**

Add these tests to `internal/librarian/librarian_test.go`, after the existing `TestScan_ExtractsAndCachesANewFile` test:

```go
func TestScan_FallsBackToFilenameHeuristicsWhenMetadataFieldsAreEmpty(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	writeFixtureFile(t, libDir, filepath.Join("Technology", "Cloud", "Cloud Native Microservices Cookbook (2024) - Varun Yadav.epub"))

	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		return metadata.Result{}, nil // Title/Author/Year all empty, as if extraction found nothing usable
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("len(books) = %d, want 1", len(books))
	}
	if books[0].Title != "Cloud Native Microservices Cookbook" {
		t.Errorf("Title = %q, want %q (derived from the filename via heuristics.Parse)", books[0].Title, "Cloud Native Microservices Cookbook")
	}
	if books[0].Author != "Varun Yadav" {
		t.Errorf("Author = %q, want %q", books[0].Author, "Varun Yadav")
	}
	if books[0].Year != "2024" {
		t.Errorf("Year = %q, want 2024", books[0].Year)
	}
}

func TestScan_MetadataFieldsTakePrecedenceOverFilenameHeuristics(t *testing.T) {
	orig := extractFunc
	defer func() { extractFunc = orig }()

	libDir := t.TempDir()
	logDir := t.TempDir()
	writeFixtureFile(t, libDir, filepath.Join("Fiction", "Sci-Fi", "Some Random Filename.epub"))

	extractFunc = func(path string, hyphenExceptions []string, pdfCoverPageLimit int) (metadata.Result, error) {
		return metadata.Result{Title: "Foundation", Author: "Isaac Asimov", Year: "1951"}, nil
	}

	cfg := config.Config{General: config.General{LibraryFolder: libDir, LogFolder: logDir}}
	books, err := Scan(cfg, false)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("len(books) = %d, want 1", len(books))
	}
	if books[0].Title != "Foundation" || books[0].Author != "Isaac Asimov" || books[0].Year != "1951" {
		t.Errorf("books[0] = %+v, want the metadata-sourced fields, not filename heuristics", books[0])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/librarian/ -run TestScan_FallsBackToFilenameHeuristics -v`
Expected: FAIL with `Title = "", want "Cloud Native Microservices Cookbook"` (no heuristic fallback exists yet in `librarian.Scan`). `TestScan_MetadataFieldsTakePrecedenceOverFilenameHeuristics` passes trivially today (nothing overrides non-empty fields yet) -- expected, it stays green once implemented and exists to lock in the precedence rule going forward.

- [ ] **Step 3: Implement the heuristic fallback**

In `internal/librarian/librarian.go`, add `"github.com/FrancisChung/book-organiser/internal/heuristics"` to the import block, changing:

```go
import (
	"os"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/librarycache"
	"github.com/FrancisChung/book-organiser/internal/metadata"
	"github.com/FrancisChung/book-organiser/internal/scanner"
)
```

to:

```go
import (
	"os"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/heuristics"
	"github.com/FrancisChung/book-organiser/internal/librarycache"
	"github.com/FrancisChung/book-organiser/internal/metadata"
	"github.com/FrancisChung/book-organiser/internal/scanner"
)
```

In `Scan`, change:

```go
		if res, err := extractFunc(path, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit); err == nil {
			b.Title = res.Title
			b.Author = res.Author
			b.Year = res.Year

			coverBytes, coverContentType := res.CoverBytes, res.CoverContentType
```

to:

```go
		if res, err := extractFunc(path, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit); err == nil {
			b.Title = res.Title
			b.Author = res.Author
			b.Year = res.Year

			// Mirrors internal/pipeline.Run's existing filename-heuristic
			// fallback: embedded metadata sometimes resolves to nothing
			// usable (a missing field, or extractEpub/extractPDF's own
			// placeholder-value checks blanking a known-junk value), and
			// the Library view previously had no fallback at all for that
			// case, unlike Scan & Review.
			if b.Title == "" || b.Author == "" || b.Year == "" {
				stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
				h := heuristics.Parse(stem, cfg.Heuristics.KnownJunkTags)
				if b.Title == "" && h.Title != "" {
					b.Title = h.Title
				}
				if b.Author == "" && h.Author != "" {
					b.Author = h.Author
				}
				if b.Year == "" && h.Year != "" {
					b.Year = h.Year
				}
			}

			coverBytes, coverContentType := res.CoverBytes, res.CoverContentType
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/librarian/... -v`
Expected: PASS (all tests, including the two new ones and every pre-existing `librarian.Scan` test -- in particular the existing cache-hit tests, which never reach this new code path at all since they return early on a cache hit).

- [ ] **Step 5: Commit**

```bash
git add internal/librarian/librarian.go internal/librarian/librarian_test.go
git commit -m "Fall back to filename heuristics in the Library view's scan, matching Scan & Review"
```

---

### Task 4: Version bumps so already-cached books self-heal

**Files:**
- Modify: `internal/metadata/extractor.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `metadata.CoverExtractorVersion` changes from `3` to `4`; `metadata.MetadataExtractorVersion` changes from `2` to `3`. `internal/librarian`'s existing cache-hit condition already compares against both constants by reference, not hardcoded numbers, so no changes are needed in `internal/librarian/librarian.go` or `internal/librarian/librarian_test.go` beyond what Task 3 already added.

- [ ] **Step 1: Bump both constants**

In `internal/metadata/extractor.go`, change:

```go
const CoverExtractorVersion = 3 // bumped: findPDFPageImages now recurses into Form XObjects (pdf_images.go), and findPDFCover's whole-file fallback now decodes FlateDecode images (pdf.go) -- both can produce different/additional cover bytes than before for the same book.
```

to:

```go
const CoverExtractorVersion = 4 // bumped: extractEpub can now fall back to the first spine document's first <img> when no OPF cover convention (EPUB3 properties="cover-image" or EPUB2 meta name="cover") is present, producing a cover where none existed before for the same book.
```

And change:

```go
const MetadataExtractorVersion = 2 // bumped: findInfoDictBody can now resolve an Info dict compressed inside an ObjStm, and extractPDF can now decode hex-string (not just literal-string) Title/Author/Subject/CreationDate values.
```

to:

```go
const MetadataExtractorVersion = 3 // bumped: extractEpub now blanks placeholder Title/Author values (a bare numeric ID, or a literal "Unknown" author) instead of returning them as-is, and internal/librarian.Scan now falls back to filename heuristics (matching internal/pipeline.Run's existing behavior) whenever Title/Author/Year comes back empty.
```

- [ ] **Step 2: Run the full metadata and librarian test suites to confirm no regressions**

Run: `go test ./internal/metadata/... ./internal/librarian/... ./internal/librarycache/... -v`
Expected: PASS (all tests -- in particular, confirm the existing `TestScan_StaleCoverVersionForcesReExtractionDespiteMatchingModTimeAndSize`, `TestScan_StaleMetadataVersionForcesReExtractionDespiteMatchingCoverVersion`, `TestScan_ReExtractedEntryIsCachedWithCurrentCoverVersion`, and `TestScan_ReExtractedEntryIsCachedWithCurrentMetadataVersion` tests all still pass, since all four compare against the live constants directly rather than hardcoded numbers).

- [ ] **Step 3: Run the full build and vet**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS with no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/metadata/extractor.go
git commit -m "Bump CoverExtractorVersion and MetadataExtractorVersion for the EPUB fallback and heuristics fixes"
```

---

## Manual Verification (after all tasks complete)

The real file used throughout this investigation is available locally for a final end-to-end check (not committed as a test fixture, per this package's existing-fixture convention):

1. Run a normal library scan (the version bumps mean this book's stale cache entry self-heals automatically -- no manual cache-clear needed).
2. In the desktop app, navigate to "Cloud Native Microservices Cookbook (2024) - Varun Yadav" in the Library view.
3. Confirm its cover now shows (the image embedded in the first spine page).
4. Confirm its Title now shows "Cloud Native Microservices Cookbook" (previously "728310488") -- via the new `librarian.Scan` heuristic fallback, not just a blank field falling through to the raw filename.
