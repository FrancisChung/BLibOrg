# Library / Bookshelf Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user browse the already-organized library (`library_folder` in config.yaml) as one bookshelf per subcategory, with cover art, sorting, category filtering, and click-to-open — per `docs/superpowers/specs/2026-07-22-Bookshelf-design.md`.

**Architecture:** A new `internal/librarian` package walks `library_folder` and reports what's there (re-deriving Title/Author/Year/cover via the existing `metadata.Extract`, never persisting anything — the same convention `pipeline.Run` uses for the working folder). Each format extractor (`epub.go`, `mobi.go`, `pdf.go`) gains best-effort cover-image extraction. A new `internal/covercache` package writes extracted covers once to disk under the existing `LogFolder` app-data convention; a custom Wails `AssetServer` `Handler` serves them at `/covers/<file>` (Wails 2.13 blocks `file://` URLs, so a same-origin route replaces it). `appapi.ListLibrary` exposes this to a new `LibraryView.svelte`, reached via a new "Library" item in `Sidebar.svelte` with a category submenu.

**Tech Stack:** Go 1.25, standard library only (`archive/zip`, `encoding/binary`, `regexp`, `net/http`, `crypto/sha256` — no new Go dependencies). Svelte + TypeScript + Vitest, no new frontend dependencies.

## Global Constraints

- No new third-party Go or npm dependencies.
- Nothing new is persisted to disk except cached cover *image bytes* (under `LogFolder/covers/`, following the existing `ops.jsonl`/`category-warnings.jsonl` convention) — Title/Author/Year/Category/Subcategory are re-derived on every `ListLibrary()` call, never stored, matching `pipeline.Run`'s convention.
- `book.Book` and `appapi.BookView` are not touched — the Library feature is entirely additive, its own `librarian.Book`/`appapi.LibraryBookView` types.
- Cover extraction is best-effort per format and must never turn a successful metadata extraction into an error: EPUB via manifest `cover-image` (EPUB3 `properties="cover-image"` preferred, falling back to the EPUB2 `<meta name="cover">` convention), MOBI/AZW3 via a PDB-record image-signature scan (see Task 2's rationale for why this replaces an EXTH-offset approach), PDF via `DCTDecode` (JPEG) image XObject streams only — no `FlateDecode` raw-bitmap reconstruction. A book with no extractable cover gets `CoverPath == ""`, never an error.
- Sort control is one **global** Title/Author/Year toggle applied to every shelf at once (not per-shelf).
- Shelf overflow is native horizontal scroll (`overflow-x: auto`) only — no prev/next arrow buttons or pagination in this pass.
- Selecting a major category in the sidebar submenu **filters** the Library view to that category's subcategory shelves only; "All" shows every shelf across every category.
- Covers are served via a same-origin `/covers/<file>` route (a custom `assetserver.Options.Handler`), never `file://` paths or base64-inlined JSON.

---

## Task 1: `metadata.Result` cover fields + EPUB cover extraction

**Files:**
- Modify: `internal/metadata/result.go`
- Modify: `internal/metadata/epub.go`
- Test: `internal/metadata/epub_test.go`

**Interfaces:**
- Produces: `metadata.Result.CoverBytes []byte`, `metadata.Result.CoverContentType string` — populated by `extractEpub` (this task), `extractMobi` (Task 2), `extractPDF` (Task 3). Empty `CoverBytes` means "no cover found," never an error.

- [ ] **Step 1: Write the failing tests**

Add this helper and these two tests to `internal/metadata/epub_test.go` (the file already has `writeEpubFixture`, `testContainerXML`, and `TestExtractEpub*` — leave those untouched):

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/... -run TestExtractEpub -v`
Expected: FAIL to compile — `result.CoverBytes`/`result.CoverContentType` don't exist yet.

- [ ] **Step 3: Write the implementation**

In `internal/metadata/result.go`, add the two fields:

```go
package metadata

// Result holds whatever metadata an extractor could resolve. Any field left
// empty means "not found by this extractor" -- callers fall back accordingly.
type Result struct {
	Title   string
	Author  string
	Year    string
	Subject string

	// CoverBytes is the raw bytes of the first embedded cover image found
	// (EPUB manifest cover-image, MOBI/AZW3 PDB image record, PDF DCTDecode
	// image XObject -- see each extractor's doc comment), or nil if none was
	// found. Extraction is always best-effort: a missing cover is never an
	// error. CoverContentType is its MIME type (e.g. "image/jpeg"), set
	// whenever CoverBytes is non-empty.
	CoverBytes       []byte
	CoverContentType string
}
```

In `internal/metadata/epub.go`, extend the package struct and `extractEpub`:

```go
package metadata

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

type epubContainer struct {
	Rootfiles struct {
		Rootfile struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfile"`
	} `xml:"rootfiles"`
}

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

func findZipFile(r *zip.ReadCloser, name string) (*zip.File, bool) {
	for _, f := range r.File {
		if f.Name == name {
			return f, true
		}
	}
	return nil, false
}

// findEpubCoverItem returns the href/media-type of p's cover image, checked
// in priority order: the EPUB3 convention (a manifest <item> whose
// properties list includes "cover-image") first, falling back to the EPUB2
// convention (a <meta name="cover" content="ITEM_ID"/> pointing at a
// manifest item by id). Returns ok=false if neither convention is present.
func findEpubCoverItem(p epubPackage) (href, mediaType string, ok bool) {
	for _, item := range p.Manifest.Items {
		for _, prop := range splitEpubProperties(item.Properties) {
			if prop == "cover-image" {
				return item.Href, item.MediaType, true
			}
		}
	}

	var coverID string
	for _, m := range p.Metadata.Meta {
		if m.Name == "cover" {
			coverID = m.Content
			break
		}
	}
	if coverID == "" {
		return "", "", false
	}
	for _, item := range p.Manifest.Items {
		if item.ID == coverID {
			return item.Href, item.MediaType, true
		}
	}
	return "", "", false
}

func splitEpubProperties(properties string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(properties); i++ {
		if i == len(properties) || properties[i] == ' ' {
			if i > start {
				out = append(out, properties[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func extractEpub(path string) (Result, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return Result{}, err
	}
	defer r.Close()

	cf, ok := findZipFile(r, "META-INF/container.xml")
	if !ok {
		return Result{}, fmt.Errorf("epub missing META-INF/container.xml")
	}
	crc, err := cf.Open()
	if err != nil {
		return Result{}, err
	}
	defer crc.Close()
	var c epubContainer
	if err := xml.NewDecoder(crc).Decode(&c); err != nil {
		return Result{}, err
	}

	of, ok := findZipFile(r, c.Rootfiles.Rootfile.FullPath)
	if !ok {
		return Result{}, fmt.Errorf("epub missing opf file %s", c.Rootfiles.Rootfile.FullPath)
	}
	orc, err := of.Open()
	if err != nil {
		return Result{}, err
	}
	defer orc.Close()
	var p epubPackage
	if err := xml.NewDecoder(orc).Decode(&p); err != nil {
		return Result{}, err
	}

	result := Result{
		Title:   p.Metadata.Title,
		Author:  p.Metadata.Creator,
		Subject: p.Metadata.Subject,
	}
	if year, ok := textutil.ExtractYear(p.Metadata.Date); ok {
		result.Year = year
	}

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

	return result, nil
}

func epubPathJoin(opfFullPath, href string) string {
	return path.Join(path.Dir(opfFullPath), href)
}
```

**Note:** the parameter name `path` on `extractEpub(path string)` shadows the standard-library `path` package within that function's body — this is why the join logic is factored into the separate `epubPathJoin` helper (which has no such shadowing) rather than called inline inside `extractEpub`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -run TestExtractEpub -v`
Expected: PASS (all `TestExtractEpub*` tests, including the pre-existing ones)

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/result.go internal/metadata/epub.go internal/metadata/epub_test.go
git commit -m "Extract cover images from EPUB manifest cover-image items"
```

---

## Task 2: MOBI/AZW3 cover extraction

**Files:**
- Modify: `internal/metadata/mobi.go`
- Test: `internal/metadata/mobi_test.go`

**Interfaces:**
- Consumes: `metadata.Result.CoverBytes`/`CoverContentType` (Task 1).
- Produces: `extractMobi` populates `Result.CoverBytes`/`CoverContentType`.

**Rationale for the approach:** MOBI's documented convention is EXTH record 201 ("Cover Offset"), whose value is added to the MOBI header's "first image record index" field to get the PDB record number holding the cover. That header field's exact byte offset is inconsistently documented across sources, and this package's other MOBI header offsets (`exthFlags` at +84, `fullNameOffset` at +96 from `mobi.go`'s existing, tested `extractMobi`) don't match the offsets an initial documentation check returned for that same header layout — meaning that source is not reliable enough to build new binary-offset code on for this codebase's actual header layout. Rather than hardcode a number that would only be verified against a self-consistent test fixture (and could silently be wrong against every real-world file), this task scans the PDB records after record 0 for the first one whose leading bytes match a recognized image signature (JPEG/PNG/GIF) — MOBI stores the cover as one of the embedded image records, conventionally the first one encountered. This needs no uncertain offset at all.

- [ ] **Step 1: Write the failing tests**

Add this helper and these two tests to `internal/metadata/mobi_test.go` (leave `writeMobiFixture` and the existing `TestExtractMobi*` tests untouched):

```go
// writeMobiFixtureWithCover builds a minimal valid PalmDB+MOBI file with two
// PDB records: record 0 (a standard MOBI header, no EXTH block) and record 1
// containing coverData's raw bytes -- exercising the image-signature scan
// across multiple PDB records.
func writeMobiFixtureWithCover(t *testing.T, fullName string, coverData []byte) string {
	t.Helper()
	buf := new(bytes.Buffer)

	name := make([]byte, 32)
	copy(name, "testbook")
	buf.Write(name)
	binary.Write(buf, binary.BigEndian, uint16(0)) // attributes
	binary.Write(buf, binary.BigEndian, uint16(0)) // version
	binary.Write(buf, binary.BigEndian, uint32(0)) // creation date
	binary.Write(buf, binary.BigEndian, uint32(0)) // mod date
	binary.Write(buf, binary.BigEndian, uint32(0)) // last backup
	binary.Write(buf, binary.BigEndian, uint32(0)) // mod number
	binary.Write(buf, binary.BigEndian, uint32(0)) // appInfoID
	binary.Write(buf, binary.BigEndian, uint32(0)) // sortInfoID
	buf.WriteString("BOOK")
	buf.WriteString("MOBI")
	binary.Write(buf, binary.BigEndian, uint32(0)) // uniqueIDseed
	binary.Write(buf, binary.BigEndian, uint32(0)) // nextRecordListID
	binary.Write(buf, binary.BigEndian, uint16(2)) // numRecords = 2

	record0Offset := uint32(78 + 2*8) // header (78) + 2 record-info entries (8 bytes each)

	rec0 := new(bytes.Buffer)
	binary.Write(rec0, binary.BigEndian, uint16(1)) // compression
	binary.Write(rec0, binary.BigEndian, uint16(0)) // unused
	binary.Write(rec0, binary.BigEndian, uint32(0)) // text length
	binary.Write(rec0, binary.BigEndian, uint16(0)) // record count
	binary.Write(rec0, binary.BigEndian, uint16(0)) // record size
	binary.Write(rec0, binary.BigEndian, uint16(0)) // encryption type
	binary.Write(rec0, binary.BigEndian, uint16(0)) // unused2

	mobiHeaderStart := rec0.Len()
	const mobiHeaderLen = 232
	rec0.WriteString("MOBI")
	binary.Write(rec0, binary.BigEndian, uint32(mobiHeaderLen))
	binary.Write(rec0, binary.BigEndian, uint32(2))     // mobi type
	binary.Write(rec0, binary.BigEndian, uint32(65001)) // text encoding UTF-8
	binary.Write(rec0, binary.BigEndian, uint32(0))     // unique ID
	binary.Write(rec0, binary.BigEndian, uint32(6))     // file version

	for rec0.Len()-mobiHeaderStart < 84 {
		rec0.WriteByte(0)
	}
	binary.Write(rec0, binary.BigEndian, uint32(0)) // EXTH flags: bit6 clear, no EXTH block

	for rec0.Len()-mobiHeaderStart < 96 {
		rec0.WriteByte(0)
	}
	fullNameOffsetPos := rec0.Len()
	binary.Write(rec0, binary.BigEndian, uint32(0)) // placeholder full name offset
	binary.Write(rec0, binary.BigEndian, uint32(0)) // placeholder full name length

	for rec0.Len()-mobiHeaderStart < mobiHeaderLen {
		rec0.WriteByte(0)
	}

	fullNameOffset := uint32(rec0.Len())
	rec0.WriteString(fullName)

	out := rec0.Bytes()
	binary.BigEndian.PutUint32(out[fullNameOffsetPos:], fullNameOffset)
	binary.BigEndian.PutUint32(out[fullNameOffsetPos+4:], uint32(len(fullName)))

	record1Offset := record0Offset + uint32(len(out))

	binary.Write(buf, binary.BigEndian, record0Offset)
	binary.Write(buf, binary.BigEndian, uint32(0)) // attributes+uniqueID packed
	binary.Write(buf, binary.BigEndian, record1Offset)
	binary.Write(buf, binary.BigEndian, uint32(0)) // attributes+uniqueID packed

	buf.Write(out)
	buf.Write(coverData)

	dir := t.TempDir()
	path := filepath.Join(dir, "book.mobi")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write mobi fixture: %v", err)
	}
	return path
}

func TestExtractMobi_FindsCoverImage(t *testing.T) {
	coverData := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("fakejpegdata")...)
	path := writeMobiFixtureWithCover(t, "Foundation", coverData)

	result, err := extractMobi(path)
	if err != nil {
		t.Fatalf("extractMobi returned error: %v", err)
	}
	if string(result.CoverBytes) != string(coverData) {
		t.Errorf("CoverBytes = %v, want %v", result.CoverBytes, coverData)
	}
	if result.CoverContentType != "image/jpeg" {
		t.Errorf("CoverContentType = %q, want image/jpeg", result.CoverContentType)
	}
}

func TestExtractMobi_NoCoverLeavesFieldEmpty(t *testing.T) {
	path := writeMobiFixture(t, "Foundation", "Isaac Asimov", "Sci-Fi", "1951-01-01")

	result, err := extractMobi(path)
	if err != nil {
		t.Fatalf("extractMobi returned error: %v", err)
	}
	if result.CoverBytes != nil {
		t.Errorf("CoverBytes = %v, want nil", result.CoverBytes)
	}
}
```

Add `"bytes"` to `mobi_test.go`'s import block if not already present (it currently imports `bytes`, `encoding/binary`, `os`, `path/filepath`, `testing` — `bytes` is already there).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/... -run TestExtractMobi -v`
Expected: FAIL to compile — `result.CoverBytes` doesn't exist on the returned value from `extractMobi` yet (the field itself now exists from Task 1, but nothing sets it).

- [ ] **Step 3: Write the implementation**

Replace `internal/metadata/mobi.go` in full:

```go
package metadata

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"

	"github.com/FrancisChung/book-organiser/internal/textutil"
)

var (
	jpegMagic  = []byte{0xFF, 0xD8, 0xFF}
	pngMagic   = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	gif87Magic = []byte("GIF87a")
	gif89Magic = []byte("GIF89a")
)

func sniffImageContentType(data []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(data, jpegMagic):
		return "image/jpeg", true
	case bytes.HasPrefix(data, pngMagic):
		return "image/png", true
	case bytes.HasPrefix(data, gif87Magic), bytes.HasPrefix(data, gif89Magic):
		return "image/gif", true
	default:
		return "", false
	}
}

// findMobiCover scans the PDB records after record 0 (the MOBI/EXTH header
// and text record) for the first one whose leading bytes match a recognized
// image signature (JPEG/PNG/GIF), and returns it as the cover. See this
// task's plan entry for why this replaces computing the exact record via
// EXTH 201 "Cover Offset" plus the MOBI header's "first image record"
// field.
func findMobiCover(data []byte, numRecords uint16) ([]byte, string, bool) {
	offsets := make([]uint32, numRecords)
	for i := uint16(0); i < numRecords; i++ {
		pos := 78 + int(i)*8
		if pos+4 > len(data) {
			return nil, "", false
		}
		offsets[i] = binary.BigEndian.Uint32(data[pos : pos+4])
	}
	for i := 1; i < int(numRecords); i++ {
		start := int(offsets[i])
		end := len(data)
		if i+1 < int(numRecords) {
			end = int(offsets[i+1])
		}
		if start < 0 || start >= end || end > len(data) {
			continue
		}
		if ct, ok := sniffImageContentType(data[start:end]); ok {
			return data[start:end], ct, true
		}
	}
	return nil, "", false
}

// extractMobi parses the PalmDB + MOBI header + EXTH structure shared by
// .mobi and .azw3 files. It is best-effort: on any structural surprise past
// the point where core fields have already been read, it returns whatever
// it has rather than erroring, so callers can still fall back to heuristics
// for missing fields.
func extractMobi(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	if len(data) < 82 {
		return Result{}, fmt.Errorf("file too short to be a valid MOBI/AZW3")
	}
	numRecords := binary.BigEndian.Uint16(data[76:78])
	if numRecords < 1 {
		return Result{}, fmt.Errorf("no records found")
	}
	record0Offset := binary.BigEndian.Uint32(data[78:82])
	if int(record0Offset) >= len(data) {
		return Result{}, fmt.Errorf("record0 offset out of range")
	}
	rec0 := data[record0Offset:]

	const mobiHeaderStart = 16
	if len(rec0) < mobiHeaderStart+104 {
		return Result{}, fmt.Errorf("record0 too short for MOBI header")
	}
	if string(rec0[mobiHeaderStart:mobiHeaderStart+4]) != "MOBI" {
		return Result{}, fmt.Errorf("MOBI identifier not found")
	}
	headerLen := binary.BigEndian.Uint32(rec0[mobiHeaderStart+4 : mobiHeaderStart+8])
	exthFlags := binary.BigEndian.Uint32(rec0[mobiHeaderStart+84 : mobiHeaderStart+88])
	fullNameOffset := binary.BigEndian.Uint32(rec0[mobiHeaderStart+96 : mobiHeaderStart+100])
	fullNameLen := binary.BigEndian.Uint32(rec0[mobiHeaderStart+100 : mobiHeaderStart+104])

	var result Result
	if uint64(fullNameOffset)+uint64(fullNameLen) <= uint64(len(rec0)) {
		result.Title = string(rec0[fullNameOffset : fullNameOffset+fullNameLen])
	}

	if coverData, coverContentType, ok := findMobiCover(data, numRecords); ok {
		result.CoverBytes = coverData
		result.CoverContentType = coverContentType
	}

	if exthFlags&0x40 == 0 {
		return result, nil // no EXTH block present
	}
	exthStart := mobiHeaderStart + int(headerLen)
	if exthStart+12 > len(rec0) || string(rec0[exthStart:exthStart+4]) != "EXTH" {
		return result, nil
	}
	recordCount := binary.BigEndian.Uint32(rec0[exthStart+8 : exthStart+12])
	pos := exthStart + 12
	var pubdate string
	for i := uint32(0); i < recordCount; i++ {
		if pos+8 > len(rec0) {
			break
		}
		recType := binary.BigEndian.Uint32(rec0[pos : pos+4])
		recLen := binary.BigEndian.Uint32(rec0[pos+4 : pos+8])
		if recLen < 8 || pos+int(recLen) > len(rec0) {
			break
		}
		recData := rec0[pos+8 : pos+int(recLen)]
		switch recType {
		case 100:
			result.Author = string(recData)
		case 105:
			result.Subject = string(recData)
		case 106:
			pubdate = string(recData)
		case 503:
			result.Title = string(recData) // updated title overrides PalmDOC full name
		}
		pos += int(recLen)
	}
	if pubdate != "" {
		if year, ok := textutil.ExtractYear(pubdate); ok {
			result.Year = year
		}
	}

	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -run TestExtractMobi -v`
Expected: PASS (all `TestExtractMobi*` tests, including the pre-existing ones)

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/mobi.go internal/metadata/mobi_test.go
git commit -m "Extract cover images from MOBI/AZW3 PDB image records"
```

---

## Task 3: PDF cover extraction

**Files:**
- Modify: `internal/metadata/pdf.go`
- Test: `internal/metadata/pdf_test.go`

**Interfaces:**
- Consumes: `metadata.Result.CoverBytes`/`CoverContentType` (Task 1).
- Produces: `extractPDF` populates `Result.CoverBytes`/`CoverContentType`.

**Scope note:** PDF has no standard cover convention. This scans for the first image XObject stream using the `DCTDecode` filter (a raw, already-valid JPEG byte stream — the format real-world book-cover images in PDFs overwhelmingly use) anywhere in the file. `FlateDecode`-filtered images are out of scope: those are raw pixel samples, not a displayable image file, and reconstructing one would need a real PDF parser to read the image's `/Width`/`/Height`/`/ColorSpace`/`/BitsPerComponent` — well beyond this package's existing regex-based, dependency-free approach (see `extractPDF`'s existing doc comment on why it's deliberately not a full parser).

- [ ] **Step 1: Write the failing tests**

Add these two tests to `internal/metadata/pdf_test.go` (it already imports `bytes`, `os`, `path/filepath`, `testing`, and has a reusable `writePDFFixture(t, content string) string` helper — reuse it, don't redefine it):

```go
func TestExtractPDF_FindsCoverImage(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	pdf := "%PDF-1.4\n" +
		"1 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Width 100 /Height 150 /Length 16 >>\nstream\n" +
		string(jpegData) + "\nendstream\nendobj\n"
	path := writePDFFixture(t, pdf)

	result, err := extractPDF(path)
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

	result, err := extractPDF(path)
	if err != nil {
		t.Fatalf("extractPDF returned error: %v", err)
	}
	if result.CoverBytes != nil {
		t.Errorf("CoverBytes = %v, want nil", result.CoverBytes)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/metadata/... -run TestExtractPDF_ -v`
Expected: FAIL — `result.CoverBytes` stays `nil` for `TestExtractPDF_FindsCoverImage` since nothing sets it yet.

- [ ] **Step 3: Write the implementation**

In `internal/metadata/pdf.go`, add `"bytes"` to the import block (currently `os`, `regexp`, `strings`, `unicode/utf16`), add these package-level vars and a new function, and call it from `extractPDF`:

```go
var pdfImageStreamRe = regexp.MustCompile(`(?s)<<([^>]*?)>>\s*stream\r?\n(.*?)endstream`)
var pdfSubtypeImageRe = regexp.MustCompile(`/Subtype\s*/Image`)
var pdfDCTDecodeRe = regexp.MustCompile(`/Filter\s*/DCTDecode|/Filter\s*\[[^\]]*/DCTDecode`)

// findPDFCover scans data for the first image XObject stream (a "<<...>>
// stream ... endstream" block whose dictionary declares both /Subtype
// /Image and a /Filter of /DCTDecode) and returns its raw stream bytes --
// already a complete, valid JPEG, since that's what DCTDecode means per the
// PDF spec. See this task's plan entry for why FlateDecode-filtered images
// are out of scope. This is a textual scan, not a real PDF parser: it does
// not handle a dictionary containing its own nested "<<...>>" (e.g. a
// /DecodeParms sub-dictionary), matching the rest of this file's
// deliberately best-effort approach.
func findPDFCover(data []byte) ([]byte, string, bool) {
	for _, m := range pdfImageStreamRe.FindAllSubmatch(data, -1) {
		dict := m[1]
		if !pdfSubtypeImageRe.Match(dict) || !pdfDCTDecodeRe.Match(dict) {
			continue
		}
		stream := bytes.TrimRight(m[2], "\r\n")
		if len(stream) == 0 {
			continue
		}
		return stream, "image/jpeg", true
	}
	return nil, "", false
}
```

In `extractPDF`, add the cover lookup right before the final `return result, nil`:

```go
	if coverData, coverContentType, ok := findPDFCover(data); ok {
		result.CoverBytes = coverData
		result.CoverContentType = coverContentType
	}
	return result, nil
}
```

(Only the new vars/function and the four added lines before `extractPDF`'s final return are additions — everything else in `pdf.go` is unchanged.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/metadata/... -run TestExtractPDF -v`
Expected: PASS (all `TestExtractPDF*` tests, including the pre-existing ones)

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_test.go
git commit -m "Extract cover images from PDF DCTDecode image XObject streams"
```

---

## Task 4: `internal/covercache` package

**Files:**
- Create: `internal/covercache/covercache.go`
- Test: `internal/covercache/covercache_test.go`

**Interfaces:**
- Produces: `covercache.Dir(logFolder string) string`, `func Ensure(logFolder, sourcePath string, sourceModTime time.Time, coverBytes []byte, contentType string) (urlPath string, err error)`. Consumed by Task 5.

- [ ] **Step 1: Write the failing tests**

```go
// internal/covercache/covercache_test.go
package covercache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnsure_EmptyCoverBytesReturnsEmptyPath(t *testing.T) {
	logFolder := t.TempDir()
	url, err := Ensure(logFolder, "/library/Fiction/book.epub", time.Now(), nil, "image/jpeg")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if url != "" {
		t.Errorf("url = %q, want empty", url)
	}
}

func TestEnsure_WritesCoverAndReturnsURL(t *testing.T) {
	logFolder := t.TempDir()
	data := []byte("fake-jpeg-bytes")

	url, err := Ensure(logFolder, "/library/Fiction/book.epub", time.Now(), data, "image/jpeg")
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if filepath.Ext(url) != ".jpg" {
		t.Errorf("url = %q, want a .jpg extension", url)
	}

	cachePath := filepath.Join(Dir(logFolder), filepath.Base(url))
	written, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(written) != string(data) {
		t.Errorf("cached content = %q, want %q", written, data)
	}
}

func TestEnsure_SkipsRewriteWhenCacheIsFresh(t *testing.T) {
	logFolder := t.TempDir()
	sourceModTime := time.Now().Add(-time.Hour)

	if _, err := Ensure(logFolder, "/library/Fiction/book.epub", sourceModTime, []byte("original"), "image/jpeg"); err != nil {
		t.Fatalf("first Ensure returned error: %v", err)
	}
	url, err := Ensure(logFolder, "/library/Fiction/book.epub", sourceModTime, []byte("changed-but-should-be-ignored"), "image/jpeg")
	if err != nil {
		t.Fatalf("second Ensure returned error: %v", err)
	}

	cachePath := filepath.Join(Dir(logFolder), filepath.Base(url))
	written, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(written) != "original" {
		t.Errorf("cached content = %q, want unchanged %q", written, "original")
	}
}

func TestEnsure_RewritesWhenSourceIsNewerThanCache(t *testing.T) {
	logFolder := t.TempDir()

	if _, err := Ensure(logFolder, "/library/Fiction/book.epub", time.Now().Add(-time.Hour), []byte("original"), "image/jpeg"); err != nil {
		t.Fatalf("first Ensure returned error: %v", err)
	}
	url, err := Ensure(logFolder, "/library/Fiction/book.epub", time.Now().Add(time.Hour), []byte("updated"), "image/jpeg")
	if err != nil {
		t.Fatalf("second Ensure returned error: %v", err)
	}

	cachePath := filepath.Join(Dir(logFolder), filepath.Base(url))
	written, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(written) != "updated" {
		t.Errorf("cached content = %q, want %q", written, "updated")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/covercache/... -v`
Expected: FAIL to compile — package `covercache` doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```go
// internal/covercache/covercache.go

// Package covercache writes extracted book-cover images to a stable,
// content-addressed location on disk so the frontend can load them via a
// plain same-origin URL. Wails 2.13 blocks file:// URLs in its webview (see
// desktop/app.go's OpenFile doc comment), so covers can't be passed to the
// frontend as raw filesystem paths; caching them under the app's existing
// LogFolder and serving them at a fixed URL prefix (see desktop/covers.go,
// Task 7) sidesteps that instead of re-sending base64 image data on every
// library listing.
package covercache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"
)

var extByContentType = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
}

// Dir returns the cache directory under logFolder that cover files live in.
func Dir(logFolder string) string {
	return filepath.Join(logFolder, "covers")
}

// fileName returns the cache filename for sourcePath: a hash of the path
// itself (not its bytes -- one book, one stable name across calls, so the
// served URL doesn't change on every relist) plus an extension inferred
// from contentType.
func fileName(sourcePath, contentType string) string {
	sum := sha256.Sum256([]byte(sourcePath))
	ext := extByContentType[contentType]
	if ext == "" {
		ext = ".img"
	}
	return hex.EncodeToString(sum[:]) + ext
}

// Ensure writes coverBytes to the cache under logFolder if no fresher cache
// entry already exists for sourcePath, and returns the URL path the
// frontend should use as an <img src> (served by desktop/covers.go's
// coverHandler). Returns "" if coverBytes is empty -- the caller found no
// extractable cover for this book. sourceModTime is the book file's own
// mtime; an existing cache file at least that new is reused as-is rather
// than rewritten, so a re-scan of an unchanged library doesn't redo image
// writes on every call.
func Ensure(logFolder, sourcePath string, sourceModTime time.Time, coverBytes []byte, contentType string) (string, error) {
	if len(coverBytes) == 0 {
		return "", nil
	}
	dir := Dir(logFolder)
	name := fileName(sourcePath, contentType)
	cachePath := filepath.Join(dir, name)

	if info, err := os.Stat(cachePath); err == nil && !info.ModTime().Before(sourceModTime) {
		return "/covers/" + name, nil
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(cachePath, coverBytes, 0644); err != nil {
		return "", err
	}
	return "/covers/" + name, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/covercache/... -v`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add internal/covercache/covercache.go internal/covercache/covercache_test.go
git commit -m "Add internal/covercache: disk-cached, URL-addressable cover images"
```

---

## Task 5: `internal/librarian` package

**Files:**
- Create: `internal/librarian/librarian.go`
- Test: `internal/librarian/librarian_test.go`

**Interfaces:**
- Consumes: `scanner.Scan(root string) ([]string, error)` (existing, `internal/scanner/scanner.go`), `metadata.Extract(path string, hyphenExceptions []string) (metadata.Result, error)` (existing, extended by Tasks 1-3), `covercache.Ensure` (Task 4), `config.Config.General.LibraryFolder`/`LogFolder`/`TitleFormatting.HyphenExceptions` (existing).
- Produces: `librarian.Book{SourcePath, Format, Title, Author, Year, Category, Subcategory, CoverPath string}`, `func Scan(cfg config.Config) ([]Book, error)`. Consumed by Task 6.

- [ ] **Step 1: Write the failing tests**

```go
// internal/librarian/librarian_test.go
package librarian

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/librarian/... -v`
Expected: FAIL to compile — package `librarian` doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```go
// internal/librarian/librarian.go

// Package librarian walks the already-organized library folder
// (cfg.General.LibraryFolder) and reports what's in it, grouped by the
// Category/Subcategory folder structure rename.BuildPath already produces.
// Unlike internal/pipeline, it never computes a destination or moves
// anything -- it only reads back what's already there. Title/Author/Year
// and cover art are re-derived on every call via metadata.Extract, the same
// "never persisted" convention pipeline.Run uses for the working folder.
package librarian

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
	"github.com/FrancisChung/book-organiser/internal/metadata"
	"github.com/FrancisChung/book-organiser/internal/scanner"
)

// Book is one already-organized library file, with Category/Subcategory
// read directly from its folder location rather than recomputed.
type Book struct {
	SourcePath  string
	Format      string
	Title       string
	Author      string
	Year        string
	Category    string
	Subcategory string
	CoverPath   string // "" if no cover was found; otherwise a /covers/... URL path
}

// Scan walks cfg.General.LibraryFolder for every supported ebook file,
// deriving each book's Category/Subcategory from its position in the
// <library>/<Category>/<Subcategory>/<file> layout rename.BuildPath
// produces, and Title/Author/Year/cover art via metadata.Extract. A file
// sitting directly in <library>/ (no Category folder) or in
// <library>/<Category>/ with no Subcategory folder gets an empty
// Subcategory (and, for the former, an empty Category too) rather than
// being skipped -- Scan reports what it finds, it doesn't enforce layout.
// A file metadata.Extract fails on (e.g. corrupt) still gets a Book entry
// with empty Title/Author/Year/CoverPath rather than being dropped, so it's
// still visible on its shelf.
func Scan(cfg config.Config) ([]Book, error) {
	paths, err := scanner.Scan(cfg.General.LibraryFolder)
	if err != nil {
		return nil, err
	}

	books := make([]Book, 0, len(paths))
	for _, path := range paths {
		rel, err := filepath.Rel(cfg.General.LibraryFolder, path)
		if err != nil {
			continue
		}
		parts := strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/")

		b := Book{
			SourcePath: path,
			Format:     strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		}
		if len(parts) >= 1 && parts[0] != "." {
			b.Category = parts[0]
		}
		if len(parts) >= 2 {
			b.Subcategory = parts[1]
		}

		if res, err := metadata.Extract(path, cfg.TitleFormatting.HyphenExceptions); err == nil {
			b.Title = res.Title
			b.Author = res.Author
			b.Year = res.Year

			if len(res.CoverBytes) > 0 {
				if coverURL, err := covercache.Ensure(cfg.General.LogFolder, path, statModTime(path), res.CoverBytes, res.CoverContentType); err == nil {
					b.CoverPath = coverURL
				}
			}
		}

		books = append(books, b)
	}
	return books, nil
}

func statModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/librarian/... -v`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add internal/librarian/librarian.go internal/librarian/librarian_test.go
git commit -m "Add internal/librarian: walk and report the organized library folder"
```

---

## Task 6: `appapi.ListLibrary`

**Files:**
- Create: `internal/appapi/library.go`
- Test: `internal/appapi/library_test.go`

**Interfaces:**
- Consumes: `librarian.Scan(cfg config.Config) ([]librarian.Book, error)` (Task 5), `(a *App).loadConfig() (config.Config, error)` (existing, `internal/appapi/app.go`).
- Produces: `appapi.LibraryBookView{SourcePath, Format, Title, Author, Year, Category, Subcategory, CoverPath string}`, `appapi.LibraryView{Books []LibraryBookView; Categories []string}`, `func (a *App) ListLibrary() (LibraryView, error)`. Consumed by Task 7 (desktop wiring) and the frontend.

- [ ] **Step 1: Write the failing tests**

```go
// internal/appapi/library_test.go
package appapi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
)

func writeTestConfigForLibrary(t *testing.T, libraryFolder, logFolder string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Config{
		General:    config.General{LibraryFolder: libraryFolder, LogFolder: logFolder},
		Categories: map[string]config.Category{"Uncategorized": {}},
	}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return path
}

func TestListLibrary_ReturnsBooksGroupedByCategory(t *testing.T) {
	libDir := t.TempDir()
	fictionSciFi := filepath.Join(libDir, "Fiction", "Sci-Fi", "Foundation.epub")
	if err := os.MkdirAll(filepath.Dir(fictionSciFi), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(fictionSciFi, []byte("not a real epub"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	configPath := writeTestConfigForLibrary(t, libDir, t.TempDir())
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	view, err := app.ListLibrary()
	if err != nil {
		t.Fatalf("ListLibrary returned error: %v", err)
	}
	if len(view.Books) != 1 {
		t.Fatalf("len(Books) = %d, want 1", len(view.Books))
	}
	if view.Books[0].Category != "Fiction" || view.Books[0].Subcategory != "Sci-Fi" {
		t.Errorf("Category/Subcategory = %q/%q, want Fiction/Sci-Fi", view.Books[0].Category, view.Books[0].Subcategory)
	}
	if len(view.Categories) != 1 || view.Categories[0] != "Fiction" {
		t.Errorf("Categories = %v, want [Fiction]", view.Categories)
	}
}

func TestListLibrary_EmptyLibraryReturnsEmptyView(t *testing.T) {
	configPath := writeTestConfigForLibrary(t, t.TempDir(), t.TempDir())
	app := NewApp()
	app.configPath = func() (string, error) { return configPath, nil }

	view, err := app.ListLibrary()
	if err != nil {
		t.Fatalf("ListLibrary returned error: %v", err)
	}
	if len(view.Books) != 0 {
		t.Errorf("len(Books) = %d, want 0", len(view.Books))
	}
	if len(view.Categories) != 0 {
		t.Errorf("len(Categories) = %d, want 0", len(view.Categories))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/appapi/... -run TestListLibrary -v`
Expected: FAIL to compile — `(*App).ListLibrary` doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```go
// internal/appapi/library.go
package appapi

import (
	"sort"

	"github.com/FrancisChung/book-organiser/internal/librarian"
)

// LibraryBookView is the JSON view of a librarian.Book -- one
// already-organized book shown in the frontend's Library/Bookshelf view.
type LibraryBookView struct {
	SourcePath  string `json:"sourcePath"`
	Format      string `json:"format"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Year        string `json:"year"`
	Category    string `json:"category"`
	Subcategory string `json:"subcategory"`
	CoverPath   string `json:"coverPath"`
}

// LibraryView is the full result of ListLibrary: every already-organized
// book, plus the sorted list of distinct category names actually present
// (used for the frontend's Library submenu; unlike Categories(), this
// includes "Uncategorized" if any organized book genuinely lives there).
type LibraryView struct {
	Books      []LibraryBookView `json:"books"`
	Categories []string          `json:"categories"`
}

func libraryBookToView(b librarian.Book) LibraryBookView {
	return LibraryBookView{
		SourcePath:  b.SourcePath,
		Format:      b.Format,
		Title:       b.Title,
		Author:      b.Author,
		Year:        b.Year,
		Category:    b.Category,
		Subcategory: b.Subcategory,
		CoverPath:   b.CoverPath,
	}
}

// ListLibrary walks the configured library folder and returns every
// already-organized book found there, for the frontend's Library/Bookshelf
// view. It never touches the filesystem beyond reading -- no moves, no
// categorization, no destination-path computation (those are Scan/Apply's
// job for the *working* folder; this reads back what's already organized).
func (a *App) ListLibrary() (LibraryView, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return LibraryView{}, err
	}

	books, err := librarian.Scan(cfg)
	if err != nil {
		return LibraryView{}, err
	}

	views := make([]LibraryBookView, 0, len(books))
	categorySet := map[string]bool{}
	for _, b := range books {
		views = append(views, libraryBookToView(b))
		if b.Category != "" {
			categorySet[b.Category] = true
		}
	}
	categories := make([]string, 0, len(categorySet))
	for c := range categorySet {
		categories = append(categories, c)
	}
	sort.Strings(categories)

	return LibraryView{Books: views, Categories: categories}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/appapi/... -v`
Expected: PASS (all tests, including pre-existing `scan_test.go`/`apply_test.go`/etc.)

- [ ] **Step 5: Commit**

```bash
git add internal/appapi/library.go internal/appapi/library_test.go
git commit -m "Expose appapi.ListLibrary for the frontend's Library view"
```

---

## Task 7: Desktop wiring — cover HTTP handler + Wails bindings

**Files:**
- Create: `desktop/covers.go`
- Test: `desktop/covers_test.go`
- Modify: `desktop/main.go`
- Modify: `desktop/app.go`
- Modify: `desktop/frontend/wailsjs/go/main/App.d.ts`
- Modify: `desktop/frontend/wailsjs/go/main/App.js`
- Modify: `desktop/frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: `appapi.App.ListLibrary()` / `appapi.LibraryView` (Task 6), `appapi.DefaultConfigPath` (existing), `config.Load` (existing), `covercache.Dir` (Task 4).
- Produces: `(a *App) ListLibrary() (appapi.LibraryView, error)` (Wails-bound), a `/covers/<file>` HTTP route serving cached cover images, and the frontend TypeScript bindings the Library components (Tasks 8-11) call.

**Note on the three `wailsjs/` files:** they're normally regenerated by `wails build`/`wails generate module` and carry a "DO NOT EDIT" header. This task hand-edits them directly (mechanically mirroring the existing entries) so this task's diff is self-contained and testable without depending on a full frontend build being run mid-task; a later real `wails build` will regenerate them to the same content.

- [ ] **Step 1: Write the failing tests**

```go
// desktop/covers_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/FrancisChung/book-organiser/internal/config"
)

func writeTestConfigForCovers(t *testing.T, logFolder string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Config{General: config.General{LogFolder: logFolder}}
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return path
}

func TestCoverHandler_ServesExistingCoverFile(t *testing.T) {
	logFolder := t.TempDir()
	coversDir := filepath.Join(logFolder, "covers")
	if err := os.MkdirAll(coversDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coversDir, "abc123.jpg"), []byte("fake-jpeg"), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	configPath := writeTestConfigForCovers(t, logFolder)

	handler := coverHandler(func() (string, error) { return configPath, nil })
	req := httptest.NewRequest(http.MethodGet, "/covers/abc123.jpg", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "fake-jpeg" {
		t.Errorf("body = %q, want fake-jpeg", rec.Body.String())
	}
}

func TestCoverHandler_MissingFileReturns404(t *testing.T) {
	configPath := writeTestConfigForCovers(t, t.TempDir())

	handler := coverHandler(func() (string, error) { return configPath, nil })
	req := httptest.NewRequest(http.MethodGet, "/covers/does-not-exist.jpg", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestCoverHandler_RejectsPathTraversal(t *testing.T) {
	configPath := writeTestConfigForCovers(t, t.TempDir())

	handler := coverHandler(func() (string, error) { return configPath, nil })
	req := httptest.NewRequest(http.MethodGet, "/covers/..%2F..%2Fetc%2Fpasswd", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./desktop/... -run TestCoverHandler -v`
Expected: FAIL to compile — `coverHandler` doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```go
// desktop/covers.go
package main

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/FrancisChung/book-organiser/internal/config"
	"github.com/FrancisChung/book-organiser/internal/covercache"
)

// coverHandler serves cached cover images at /covers/<file> for the Library
// view's <img> tags. Wails 2.13 blocks file:// URLs in the webview (see
// App.OpenFile's doc comment), so covers can't be loaded as raw filesystem
// paths -- this same-origin route is the workaround. configPath is injected
// (rather than calling appapi.DefaultConfigPath directly) so tests can point
// it at a temp config, matching appapi.App's own configPath field. The log
// folder is resolved fresh on every request rather than cached at startup,
// matching the rest of the app's "always reload config" convention.
func coverHandler(configPath func() (string, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/covers/")
		if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
			http.NotFound(w, r)
			return
		}

		cfgPath, err := configPath()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cfg, err := config.Load(cfgPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		http.ServeFile(w, r, filepath.Join(covercache.Dir(cfg.General.LogFolder), name))
	})
}
```

In `desktop/main.go`, add the `appapi` import and wire `Handler` into `assetserver.Options`:

```go
package main

import (
	"embed"

	"github.com/FrancisChung/book-organiser/internal/appapi"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "Book Organiser",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: coverHandler(appapi.DefaultConfigPath),
		},
		BackgroundColour: &options.RGBA{R: 246, G: 248, B: 251, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
		Linux: &linux.Options{
			Icon: icon,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
```

In `desktop/app.go`, add the thin wrapper method (next to the other one-liners like `Categories`):

```go
func (a *App) ListLibrary() (appapi.LibraryView, error) {
	return a.api.ListLibrary()
}
```

In `desktop/frontend/wailsjs/go/main/App.d.ts`, insert alphabetically between `ConfirmUndo` and `ListCategoryWarnings`:

```typescript
export function ListLibrary():Promise<appapi.LibraryView>;
```

In `desktop/frontend/wailsjs/go/main/App.js`, insert in the same position:

```js
export function ListLibrary() {
  return window['go']['main']['App']['ListLibrary']();
}
```

In `desktop/frontend/wailsjs/go/models.ts`, add these two classes inside the `export namespace appapi { ... }` block, right before its closing `}`:

```typescript
	export class LibraryBookView {
	    sourcePath: string;
	    format: string;
	    title: string;
	    author: string;
	    year: string;
	    category: string;
	    subcategory: string;
	    coverPath: string;

	    static createFrom(source: any = {}) {
	        return new LibraryBookView(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourcePath = source["sourcePath"];
	        this.format = source["format"];
	        this.title = source["title"];
	        this.author = source["author"];
	        this.year = source["year"];
	        this.category = source["category"];
	        this.subcategory = source["subcategory"];
	        this.coverPath = source["coverPath"];
	    }
	}
	export class LibraryView {
	    books: LibraryBookView[];
	    categories: string[];

	    static createFrom(source: any = {}) {
	        return new LibraryView(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.books = this.convertValues(source["books"], LibraryBookView);
	        this.categories = source["categories"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./desktop/... ./internal/... -v`
Expected: build succeeds, all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/covers.go desktop/covers_test.go desktop/main.go desktop/app.go desktop/frontend/wailsjs/go/main/App.d.ts desktop/frontend/wailsjs/go/main/App.js desktop/frontend/wailsjs/go/models.ts
git commit -m "Serve cached covers at /covers/ and bind ListLibrary to the frontend"
```

---

## Task 8: Frontend `types.ts` additions

**Files:**
- Modify: `desktop/frontend/src/lib/types.ts`
- Test: `desktop/frontend/src/lib/types.test.ts` (new file)

**Interfaces:**
- Produces: `LibraryBookView`, `LibraryViewData`, `LibrarySortMode`, `LibraryShelf` types, `groupIntoShelves(books, category, sortMode): LibraryShelf[]`, and `SidebarView` extended with `'library'`. Consumed by Tasks 9-11.

- [ ] **Step 1: Write the failing tests**

```typescript
// desktop/frontend/src/lib/types.test.ts
import { describe, it, expect } from 'vitest';
import { groupIntoShelves, type LibraryBookView } from './types';

function makeBook(overrides: Partial<LibraryBookView> = {}): LibraryBookView {
  return {
    sourcePath: '/library/Fiction/Sci-Fi/book.epub',
    format: 'epub',
    title: 'Foundation',
    author: 'Isaac Asimov',
    year: '1951',
    category: 'Fiction',
    subcategory: 'Sci-Fi',
    coverPath: '',
    ...overrides,
  };
}

describe('groupIntoShelves', () => {
  it('groups books into one shelf per subcategory, sorted by subcategory name', () => {
    const books = [
      makeBook({ sourcePath: '/a', subcategory: 'Fantasy', title: 'Mistborn' }),
      makeBook({ sourcePath: '/b', subcategory: 'Sci-Fi', title: 'Foundation' }),
    ];

    const shelves = groupIntoShelves(books, '', 'title');

    expect(shelves.map((s) => s.subcategory)).toEqual(['Fantasy', 'Sci-Fi']);
  });

  it('filters to the given category', () => {
    const books = [
      makeBook({ sourcePath: '/a', category: 'Fiction', subcategory: 'Sci-Fi' }),
      makeBook({ sourcePath: '/b', category: 'Non-Fiction', subcategory: 'Science' }),
    ];

    const shelves = groupIntoShelves(books, 'Fiction', 'title');

    expect(shelves).toHaveLength(1);
    expect(shelves[0].subcategory).toBe('Sci-Fi');
  });

  it('shows every category when the filter is empty', () => {
    const books = [
      makeBook({ sourcePath: '/a', category: 'Fiction', subcategory: 'Sci-Fi' }),
      makeBook({ sourcePath: '/b', category: 'Non-Fiction', subcategory: 'Science' }),
    ];

    const shelves = groupIntoShelves(books, '', 'title');

    expect(shelves).toHaveLength(2);
  });

  it('sorts books within a shelf by title', () => {
    const books = [
      makeBook({ sourcePath: '/a', title: 'Zebra' }),
      makeBook({ sourcePath: '/b', title: 'Alpha' }),
    ];

    const shelves = groupIntoShelves(books, '', 'title');

    expect(shelves[0].books.map((b) => b.title)).toEqual(['Alpha', 'Zebra']);
  });

  it('sorts books within a shelf by author', () => {
    const books = [
      makeBook({ sourcePath: '/a', author: 'Zed' }),
      makeBook({ sourcePath: '/b', author: 'Amy' }),
    ];

    const shelves = groupIntoShelves(books, '', 'author');

    expect(shelves[0].books.map((b) => b.author)).toEqual(['Amy', 'Zed']);
  });

  it('sorts books within a shelf by year', () => {
    const books = [
      makeBook({ sourcePath: '/a', year: '2020' }),
      makeBook({ sourcePath: '/b', year: '1951' }),
    ];

    const shelves = groupIntoShelves(books, '', 'year');

    expect(shelves[0].books.map((b) => b.year)).toEqual(['1951', '2020']);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/types.test.ts`
Expected: FAIL — `groupIntoShelves` and `LibraryBookView` don't exist yet.

- [ ] **Step 3: Write the implementation**

Append to `desktop/frontend/src/lib/types.ts` (everything already in the file stays unchanged):

```typescript
export interface LibraryBookView {
  sourcePath: string;
  format: string;
  title: string;
  author: string;
  year: string;
  category: string;
  subcategory: string;
  coverPath: string;
}

export interface LibraryViewData {
  books: LibraryBookView[];
  categories: string[];
}

export type LibrarySortMode = 'title' | 'author' | 'year';

export interface LibraryShelf {
  subcategory: string;
  books: LibraryBookView[];
}

// groupIntoShelves filters books to category (or keeps all when category is
// ""), groups the result into one shelf per subcategory (subcategories
// sorted alphabetically; an empty subcategory groups under
// "(No subcategory)"), and sorts each shelf's books by sortMode -- the
// single global sort control applies to every shelf at once, per the design
// spec.
export function groupIntoShelves(
  books: LibraryBookView[],
  category: string,
  sortMode: LibrarySortMode,
): LibraryShelf[] {
  const filtered = category ? books.filter((b) => b.category === category) : books;

  const bySubcategory = new Map<string, LibraryBookView[]>();
  for (const b of filtered) {
    const key = b.subcategory || '(No subcategory)';
    const list = bySubcategory.get(key) ?? [];
    list.push(b);
    bySubcategory.set(key, list);
  }

  const compare = (a: LibraryBookView, b: LibraryBookView): number => {
    if (sortMode === 'year') return a.year.localeCompare(b.year);
    if (sortMode === 'author') return a.author.localeCompare(b.author);
    return a.title.localeCompare(b.title);
  };

  return Array.from(bySubcategory.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([subcategory, shelfBooks]) => ({
      subcategory,
      books: [...shelfBooks].sort(compare),
    }));
}
```

Change the last line of `types.ts` (the `SidebarView` type) from:

```typescript
export type SidebarView = 'scan' | 'operations' | 'warnings';
```

to:

```typescript
export type SidebarView = 'scan' | 'library' | 'operations' | 'warnings';
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/types.test.ts`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/types.ts desktop/frontend/src/lib/types.test.ts
git commit -m "Add Library types and groupIntoShelves helper"
```

---

## Task 9: `LibraryBookCard.svelte`

**Files:**
- Create: `desktop/frontend/src/lib/LibraryBookCard.svelte`
- Test: `desktop/frontend/src/lib/LibraryBookCard.test.ts`

**Interfaces:**
- Consumes: `LibraryBookView` (Task 8), `OpenFile(path: string): Promise<void>` (existing, `../../wailsjs/go/main/App`).
- Produces: `<LibraryBookCard book={LibraryBookView} />`. Consumed by Task 10.

- [ ] **Step 1: Write the failing tests**

```typescript
// desktop/frontend/src/lib/LibraryBookCard.test.ts
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen } from '@testing-library/svelte';
import LibraryBookCard from './LibraryBookCard.svelte';
import type { LibraryBookView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  OpenFile: vi.fn(),
}));

import { OpenFile } from '../../wailsjs/go/main/App';

function makeBook(overrides: Partial<LibraryBookView> = {}): LibraryBookView {
  return {
    sourcePath: '/library/Fiction/Sci-Fi/Foundation (1951) - Isaac Asimov.epub',
    format: 'epub',
    title: 'Foundation',
    author: 'Isaac Asimov',
    year: '1951',
    category: 'Fiction',
    subcategory: 'Sci-Fi',
    coverPath: '',
    ...overrides,
  };
}

describe('LibraryBookCard', () => {
  it('shows the cover image when coverPath is set', () => {
    render(LibraryBookCard, { book: makeBook({ coverPath: '/covers/abc123.jpg' }) });
    const img = screen.getByRole('img') as HTMLImageElement;
    expect(img.src).toContain('/covers/abc123.jpg');
  });

  it('shows a placeholder with the title when coverPath is empty', () => {
    render(LibraryBookCard, { book: makeBook({ coverPath: '' }) });
    expect(screen.queryByRole('img')).toBeNull();
    expect(screen.getByText('Foundation')).toBeInTheDocument();
  });

  it('reveals the filename minus extension via the hover title attribute', () => {
    render(LibraryBookCard, { book: makeBook() });
    const cover = document.querySelector('.cover') as HTMLElement;
    expect(cover.title).toBe('Foundation (1951) - Isaac Asimov');
  });

  it('calls OpenFile with sourcePath when clicked', async () => {
    const book = makeBook();
    render(LibraryBookCard, { book });
    const cover = document.querySelector('.cover') as HTMLElement;

    await fireEvent.click(cover);

    expect(OpenFile).toHaveBeenCalledWith(book.sourcePath);
  });

  it('shows an error banner when OpenFile rejects', async () => {
    vi.mocked(OpenFile).mockRejectedValueOnce(new Error('file moved'));
    render(LibraryBookCard, { book: makeBook() });
    const cover = document.querySelector('.cover') as HTMLElement;

    await fireEvent.click(cover);
    await screen.findByText('Error: file moved');
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/LibraryBookCard.test.ts`
Expected: FAIL — `./LibraryBookCard.svelte` doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```svelte
<!-- desktop/frontend/src/lib/LibraryBookCard.svelte -->
<script lang="ts">
  import type { LibraryBookView } from './types';
  import { OpenFile } from '../../wailsjs/go/main/App';

  export let book: LibraryBookView;

  let openError = '';

  function filenameNoExt(sourcePath: string): string {
    const base = sourcePath.split(/[\\/]+/).pop() ?? '';
    const dot = base.lastIndexOf('.');
    return dot > 0 ? base.slice(0, dot) : base;
  }

  async function open() {
    openError = '';
    try {
      await OpenFile(book.sourcePath);
    } catch (e) {
      openError = String(e);
    }
  }
</script>

<div class="tile">
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <!-- svelte-ignore a11y_click_events_have_key_events -- click-to-open is a
       supplementary affordance (like a file manager icon), matching
       BookCard.svelte's openOriginal pattern -->
  <div class="cover" on:click={open} title={filenameNoExt(book.sourcePath)}>
    {#if book.coverPath}
      <img src={book.coverPath} alt={book.title || filenameNoExt(book.sourcePath)} />
    {:else}
      <div class="placeholder">{book.title || filenameNoExt(book.sourcePath)}</div>
    {/if}
  </div>
  {#if openError}
    <div class="banner error">{openError}</div>
  {/if}
</div>

<style>
  .tile {
    width: 90px;
    flex-shrink: 0;
  }
  .cover {
    width: 90px;
    height: 130px;
    border-radius: 4px;
    overflow: hidden;
    cursor: pointer;
    box-shadow: 0 2px 6px rgba(0, 0, 0, 0.3);
    background: var(--bf-surface);
    border: 1px solid var(--bf-border);
  }
  .cover img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .placeholder {
    width: 100%;
    height: 100%;
    display: flex;
    align-items: flex-end;
    padding: 6px;
    font-size: 11px;
    line-height: 1.2;
    color: var(--bf-text-muted);
    background: repeating-linear-gradient(
      45deg,
      var(--bf-surface),
      var(--bf-surface) 8px,
      var(--bf-border) 8px,
      var(--bf-border) 16px
    );
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 4px 6px;
    border-radius: 6px;
    font-size: 10px;
    margin-top: 4px;
  }
</style>
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/LibraryBookCard.test.ts`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/LibraryBookCard.svelte desktop/frontend/src/lib/LibraryBookCard.test.ts
git commit -m "Add LibraryBookCard: cover-forward book tile with click-to-open"
```

---

## Task 10: `LibraryView.svelte`

**Files:**
- Create: `desktop/frontend/src/lib/LibraryView.svelte`
- Test: `desktop/frontend/src/lib/LibraryView.test.ts`

**Interfaces:**
- Consumes: `LibraryBookCard` (Task 9), `groupIntoShelves`/`LibraryBookView`/`LibrarySortMode` (Task 8), `ListLibrary(): Promise<appapi.LibraryView>` (Task 7, `../../wailsjs/go/main/App`).
- Produces: `<LibraryView category={string} on:categoriesLoaded={(e: CustomEvent<string[]>) => void} />`. Consumed by Task 11.

- [ ] **Step 1: Write the failing tests**

```typescript
// desktop/frontend/src/lib/LibraryView.test.ts
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, screen, waitFor } from '@testing-library/svelte';
import LibraryView from './LibraryView.svelte';
import type { LibraryBookView } from './types';

vi.mock('../../wailsjs/go/main/App', () => ({
  ListLibrary: vi.fn(),
  OpenFile: vi.fn(),
}));

import { ListLibrary } from '../../wailsjs/go/main/App';

function makeBook(overrides: Partial<LibraryBookView> = {}): LibraryBookView {
  return {
    sourcePath: '/library/Fiction/Sci-Fi/Foundation.epub',
    format: 'epub',
    title: 'Foundation',
    author: 'Isaac Asimov',
    year: '1951',
    category: 'Fiction',
    subcategory: 'Sci-Fi',
    coverPath: '',
    ...overrides,
  };
}

describe('LibraryView', () => {
  it('groups books into one shelf per subcategory', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({
      books: [
        makeBook({ sourcePath: '/a', title: 'Foundation', subcategory: 'Sci-Fi' }),
        makeBook({ sourcePath: '/b', title: 'Mistborn', subcategory: 'Fantasy' }),
      ],
      categories: ['Fiction'],
    });

    render(LibraryView, { category: '' });

    await screen.findByText('Sci-Fi');
    expect(screen.getByText('Fantasy')).toBeInTheDocument();
  });

  it('filters to the given category', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({
      books: [
        makeBook({ sourcePath: '/a', category: 'Fiction', subcategory: 'Sci-Fi' }),
        makeBook({ sourcePath: '/b', category: 'Non-Fiction', subcategory: 'Science' }),
      ],
      categories: ['Fiction', 'Non-Fiction'],
    });

    render(LibraryView, { category: 'Fiction' });

    await screen.findByText('Sci-Fi');
    expect(screen.queryByText('Science')).toBeNull();
  });

  it('emits categoriesLoaded with the categories from the response', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({ books: [], categories: ['Fiction', 'Non-Fiction'] });
    const { component } = render(LibraryView, { category: '' });
    const handler = vi.fn();
    component.$on('categoriesLoaded', handler);

    await waitFor(() => expect(handler).toHaveBeenCalledTimes(1));
    expect(handler.mock.calls[0][0].detail).toEqual(['Fiction', 'Non-Fiction']);
  });

  it('re-sorts shelves when a sort button is clicked', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({
      books: [
        makeBook({ sourcePath: '/a', title: 'Zebra', year: '1990', subcategory: 'Sci-Fi' }),
        makeBook({ sourcePath: '/b', title: 'Alpha', year: '2020', subcategory: 'Sci-Fi' }),
      ],
      categories: ['Fiction'],
    });

    render(LibraryView, { category: '' });
    await screen.findByText('Sci-Fi');

    const shelfRow = document.querySelector('.shelf-row') as HTMLElement;
    expect(shelfRow.textContent?.indexOf('Alpha')).toBeLessThan(shelfRow.textContent?.indexOf('Zebra') ?? -1);

    await fireEvent.click(screen.getByRole('button', { name: 'Year' }));

    expect(shelfRow.textContent?.indexOf('Zebra')).toBeLessThan(shelfRow.textContent?.indexOf('Alpha') ?? -1);
  });

  it('shows an error banner when ListLibrary rejects', async () => {
    vi.mocked(ListLibrary).mockRejectedValue(new Error('no config'));
    render(LibraryView, { category: '' });
    await screen.findByText('Error: no config');
  });

  it('shows an empty-state message when there are no books', async () => {
    vi.mocked(ListLibrary).mockResolvedValue({ books: [], categories: [] });
    render(LibraryView, { category: '' });
    await screen.findByText('No books found in the library folder yet.');
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/LibraryView.test.ts`
Expected: FAIL — `./LibraryView.svelte` doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```svelte
<!-- desktop/frontend/src/lib/LibraryView.svelte -->
<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte';
  import LibraryBookCard from './LibraryBookCard.svelte';
  import { groupIntoShelves, type LibraryBookView, type LibrarySortMode } from './types';
  import { ListLibrary } from '../../wailsjs/go/main/App';

  export let category: string = '';

  const dispatch = createEventDispatcher<{ categoriesLoaded: string[] }>();

  let books: LibraryBookView[] = [];
  let loadError = '';
  let loading = false;
  let sortMode: LibrarySortMode = 'title';

  onMount(load);

  async function load() {
    loading = true;
    loadError = '';
    try {
      const view = await ListLibrary();
      books = view.books ?? [];
      dispatch('categoriesLoaded', view.categories ?? []);
    } catch (e) {
      loadError = String(e);
      books = [];
    } finally {
      loading = false;
    }
  }

  $: shelves = groupIntoShelves(books, category, sortMode);
</script>

<div class="library">
  <div class="topbar">
    <h2>{category ? `Library — ${category}` : 'Library — All categories'}</h2>
    <div class="sort-toggle" role="group" aria-label="Sort by">
      <button type="button" class:active={sortMode === 'title'} on:click={() => (sortMode = 'title')}>Title</button>
      <button type="button" class:active={sortMode === 'author'} on:click={() => (sortMode = 'author')}>Author</button>
      <button type="button" class:active={sortMode === 'year'} on:click={() => (sortMode = 'year')}>Year</button>
    </div>
  </div>

  {#if loadError}
    <div class="banner error">Error: {loadError}</div>
  {/if}
  {#if loading}
    <p>Loading library…</p>
  {:else if shelves.length === 0}
    <p class="empty">No books found in the library folder yet.</p>
  {:else}
    {#each shelves as shelf (shelf.subcategory)}
      <div class="shelf-section">
        <div class="shelf-heading">{shelf.subcategory}</div>
        <div class="shelf-row">
          {#each shelf.books as book (book.sourcePath)}
            <LibraryBookCard {book} />
          {/each}
        </div>
      </div>
    {/each}
  {/if}
</div>

<style>
  .library {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  .topbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .sort-toggle {
    display: inline-flex;
    border: 1px solid var(--bf-border);
    border-radius: 6px;
    overflow: hidden;
  }
  .sort-toggle button {
    border: none;
    background: none;
    font-family: inherit;
    padding: 6px 12px;
    font-size: 12.5px;
    cursor: pointer;
    color: var(--bf-text);
  }
  .sort-toggle button.active {
    background: var(--bf-blue);
    color: white;
  }
  .shelf-heading {
    font-size: 13px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--bf-text-muted);
    margin-bottom: 8px;
  }
  .shelf-row {
    display: flex;
    gap: 12px;
    padding-bottom: 14px;
    border-bottom: 8px solid var(--bf-border);
    overflow-x: auto;
  }
  .banner.error {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 13px;
  }
  .empty {
    color: var(--bf-text-muted);
    font-size: 14px;
  }
</style>
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/LibraryView.test.ts`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/LibraryView.svelte desktop/frontend/src/lib/LibraryView.test.ts
git commit -m "Add LibraryView: bookshelf grouping, global sort, category filter"
```

---

## Task 11: `Sidebar.svelte` + `App.svelte` wiring

**Files:**
- Modify: `desktop/frontend/src/lib/Sidebar.svelte`
- Modify: `desktop/frontend/src/lib/Sidebar.test.ts`
- Modify: `desktop/frontend/src/App.svelte`
- Modify: `desktop/frontend/src/App.test.ts`

**Interfaces:**
- Consumes: `LibraryView` (Task 10), `SidebarView` extended with `'library'` (Task 8).
- Produces: a "Library" top-level sidebar item above "Scan & Review", with a category submenu that filters the Library view; the full navigation wiring in `App.svelte`.

- [ ] **Step 1: Write the failing tests**

Add these tests to `desktop/frontend/src/lib/Sidebar.test.ts` (leave the existing tests untouched):

```typescript
it('highlights Library as active and not the others', () => {
  render(Sidebar, { active: 'library' });
  expect(screen.getByRole('button', { name: 'Library' }).className).toContain('active');
  expect(screen.getByRole('button', { name: 'Scan & Review' }).className).not.toContain('active');
});

it('emits navigate with "library" when Library is clicked', async () => {
  const { component } = render(Sidebar, { active: 'scan' });
  const handler = vi.fn();
  component.$on('navigate', handler);

  await fireEvent.click(screen.getByRole('button', { name: 'Library' }));

  expect(handler).toHaveBeenCalledTimes(1);
  expect(handler.mock.calls[0][0].detail).toBe('library');
});

it('shows no category submenu when libraryCategories is empty', () => {
  render(Sidebar, { active: 'library', libraryCategories: [] });
  expect(screen.queryByRole('button', { name: 'All' })).toBeNull();
});

it('shows "All" plus each category when libraryCategories is set', () => {
  render(Sidebar, { active: 'library', libraryCategories: ['Fiction', 'Non-Fiction'] });
  expect(screen.getByRole('button', { name: 'All' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Fiction' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Non-Fiction' })).toBeInTheDocument();
});

it('emits navigate("library") and selectCategory when a category is clicked', async () => {
  const { component } = render(Sidebar, { active: 'scan', libraryCategories: ['Fiction'] });
  const navHandler = vi.fn();
  const categoryHandler = vi.fn();
  component.$on('navigate', navHandler);
  component.$on('selectCategory', categoryHandler);

  await fireEvent.click(screen.getByRole('button', { name: 'Fiction' }));

  expect(navHandler.mock.calls[0][0].detail).toBe('library');
  expect(categoryHandler.mock.calls[0][0].detail).toBe('Fiction');
});

it('highlights "All" when active is library and activeLibraryCategory is empty', () => {
  render(Sidebar, { active: 'library', libraryCategories: ['Fiction'], activeLibraryCategory: '' });
  expect(screen.getByRole('button', { name: 'All' }).className).toContain('active');
  expect(screen.getByRole('button', { name: 'Fiction' }).className).not.toContain('active');
});
```

Add `ListLibrary: vi.fn()` to the `vi.mock('../wailsjs/go/main/App', ...)` factory near the top of `desktop/frontend/src/App.test.ts`, add `ListLibrary` to its import line, and add this test (leave the existing tests untouched):

```typescript
it('switches to the Library view when its sidebar item is clicked', async () => {
  vi.mocked(ListLibrary).mockResolvedValue({ books: [], categories: [] });
  render(App);
  await waitFor(() => {
    expect(screen.getByRole('button', { name: 'Scan & Review' })).toBeInTheDocument();
  });

  await fireEvent.click(screen.getByRole('button', { name: 'Library' }));

  await waitFor(() => {
    expect(screen.getByText('No books found in the library folder yet.')).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/Sidebar.test.ts src/App.test.ts`
Expected: FAIL — no "Library" button exists yet, `libraryCategories`/`activeLibraryCategory` props and `selectCategory` event don't exist yet.

- [ ] **Step 3: Write the implementation**

Replace `desktop/frontend/src/lib/Sidebar.svelte` in full:

```svelte
<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import type { SidebarView } from './types';

  export let active: SidebarView;
  export let libraryCategories: string[] = [];
  export let activeLibraryCategory: string = '';

  const dispatch = createEventDispatcher<{ navigate: SidebarView; selectCategory: string }>();

  // Array-driven (not hardcoded markup) so a future top-level item is a
  // one-line addition here, not a rework.
  const topLevelItems: { view: SidebarView; label: string }[] = [
    { view: 'library', label: 'Library' },
    { view: 'scan', label: 'Scan & Review' },
  ];

  const logItems: { view: SidebarView; label: string }[] = [
    { view: 'operations', label: 'Operations' },
    { view: 'warnings', label: 'Warnings' },
  ];

  function go(view: SidebarView) {
    dispatch('navigate', view);
  }

  function selectCategory(category: string) {
    dispatch('navigate', 'library');
    dispatch('selectCategory', category);
  }
</script>

<nav class="sidebar">
  {#each topLevelItems as item (item.view)}
    <button
      type="button"
      class="nav-item"
      class:active={active === item.view}
      on:click={() => go(item.view)}
    >
      {item.label}
    </button>
    {#if item.view === 'library' && libraryCategories.length > 0}
      <button
        type="button"
        class="nav-sub"
        class:active={active === 'library' && activeLibraryCategory === ''}
        on:click={() => selectCategory('')}
      >
        All
      </button>
      {#each libraryCategories as category (category)}
        <button
          type="button"
          class="nav-sub"
          class:active={active === 'library' && activeLibraryCategory === category}
          on:click={() => selectCategory(category)}
        >
          {category}
        </button>
      {/each}
    {/if}
  {/each}

  <div class="nav-section">Logs</div>
  {#each logItems as item (item.view)}
    <button
      type="button"
      class="nav-sub"
      class:active={active === item.view}
      on:click={() => go(item.view)}
    >
      {item.label}
    </button>
  {/each}
</nav>

<style>
  .sidebar {
    width: 220px;
    flex-shrink: 0;
    background: var(--bf-surface);
    border-right: 1px solid var(--bf-border);
    padding: 28px 16px 20px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .nav-item,
  .nav-sub {
    display: block;
    width: 100%;
    text-align: left;
    border: none;
    background: none;
    font-family: inherit;
    padding: 10px 12px;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 600;
    color: var(--bf-text-muted);
    cursor: pointer;
  }
  .nav-sub {
    padding-left: 30px;
    font-size: 13.5px;
  }
  .nav-item.active,
  .nav-sub.active {
    background: var(--bf-blue-soft);
    color: var(--bf-blue);
  }
  .nav-section {
    padding: 10px 12px 4px;
    font-size: 12px;
    font-weight: 800;
    letter-spacing: 0.03em;
    text-transform: uppercase;
    color: var(--bf-text-muted);
    margin-top: 10px;
  }
</style>
```

Replace `desktop/frontend/src/App.svelte` in full:

```svelte
<script lang="ts">
  import { onMount } from 'svelte';
  import Sidebar from './lib/Sidebar.svelte';
  import ScanReviewView from './lib/ScanReviewView.svelte';
  import LibraryView from './lib/LibraryView.svelte';
  import OperationsLogView from './lib/OperationsLogView.svelte';
  import WarningsLogView from './lib/WarningsLogView.svelte';
  import type { SidebarView } from './lib/types';
  import { ConfigStatus } from '../wailsjs/go/main/App';

  let activeView: SidebarView = 'scan';
  let configError = '';
  let configWarnings: string[] = [];
  let libraryCategories: string[] = [];
  let activeLibraryCategory = '';

  onMount(async () => {
    const status = await ConfigStatus();
    if (status.error) {
      configError = `No usable config at ${status.path}: ${status.error}`;
    }
    configWarnings = status.warnings ?? [];
  });

  function onNavigate(e: CustomEvent<SidebarView>) {
    activeView = e.detail;
  }

  function onSelectCategory(e: CustomEvent<string>) {
    activeLibraryCategory = e.detail;
  }

  function onCategoriesLoaded(e: CustomEvent<string[]>) {
    libraryCategories = e.detail;
  }
</script>

<div class="shell">
  <Sidebar
    active={activeView}
    {libraryCategories}
    {activeLibraryCategory}
    on:navigate={onNavigate}
    on:selectCategory={onSelectCategory}
  />
  <main>
    {#if configError}
      <div class="banner error">{configError}</div>
    {/if}
    {#each configWarnings as warning}
      <div class="banner warning">Config: {warning}</div>
    {/each}
    {#if activeView === 'scan'}
      <ScanReviewView />
    {:else if activeView === 'library'}
      <LibraryView category={activeLibraryCategory} on:categoriesLoaded={onCategoriesLoaded} />
    {:else if activeView === 'operations'}
      <OperationsLogView />
    {:else if activeView === 'warnings'}
      <WarningsLogView />
    {/if}
  </main>
</div>

<style>
  .shell {
    display: flex;
    min-height: 100vh;
  }
  main {
    flex: 1;
    padding: 24px 28px;
    text-align: left;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .banner.error,
  .banner.warning {
    background: var(--bf-amber-soft);
    color: var(--bf-amber);
    padding: 10px 14px;
    border-radius: 8px;
    font-size: 13px;
  }
</style>
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run`
Expected: PASS (the entire frontend suite, including every pre-existing test file)

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/Sidebar.svelte desktop/frontend/src/lib/Sidebar.test.ts desktop/frontend/src/App.svelte desktop/frontend/src/App.test.ts
git commit -m "Wire Library into the sidebar nav with a category submenu"
```

---

## Final verification

- [ ] **Run the full backend and frontend test suites**

Run: `go build ./... && go test ./... && (cd desktop/frontend && npx vitest run)`
Expected: Go build succeeds, all Go packages PASS, all Vitest files PASS.

- [ ] **Manual smoke test**

Run the app (`cd desktop && wails dev`), point `library_folder` in config.yaml at a folder containing a few real EPUB/MOBI/PDF files organized into `<Category>/<Subcategory>/` subfolders, click "Library" in the sidebar, and confirm: shelves appear grouped by subcategory, covers render for files that have them (placeholder otherwise), the Title/Author/Year sort control re-orders every shelf, clicking a category in the submenu filters the view, and clicking a book tile opens it in the OS default app.

This plan covers the Go backend and the Svelte frontend end-to-end per the design spec's full scope — nothing is deferred except what the spec's own Non-goals section already excludes (pagination/virtualization, persisted sort/filter choice, cover art beyond best-effort EPUB/MOBI/PDF extraction, prev/next shelf-overflow controls).
