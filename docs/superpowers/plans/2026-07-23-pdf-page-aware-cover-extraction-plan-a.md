# PDF Page-Aware Cover Extraction (Plan A: parsing engine) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `internal/metadata/pdf.go`'s byte-order-first cover scan with a real page-tree walk (Catalog → Pages → Kids, including ObjStm-compressed page trees) that picks the first `DCTDecode` (JPEG) image found in true page order within a configurable page limit, falling back to today's behavior when the page tree can't be resolved.

**Architecture:** A small, dependency-free PDF object model (`pdfObjIndex`) is built on top of the existing regex-scanning style already used in this package: an object-number → byte-offset index (resolving both literal objects and ones compressed inside `/Type /ObjStm` streams via stdlib `compress/zlib`), a nesting-aware sub-dictionary extractor (fixes the existing `/DecodeParms<<...>>` blind spot), a page-tree walker built on that index, and a per-page image finder built on the walker. `extractPDF` calls this new path first and only falls back to the legacy whole-file scan if the page tree can't be resolved or no image is found within the page limit.

**Tech Stack:** Go stdlib only (`regexp`, `compress/zlib`, `strconv`, `bytes`) — no new dependencies, consistent with this package's existing convention (see `pdf.go`'s package doc: "dependency-free, best-effort").

## Global Constraints

- No new external/cgo dependencies — stdlib only.
- Never regress existing behavior: if the new page-tree path can't resolve a cover, fall back to the pre-existing `findPDFCover` whole-file byte-order scan (already covered by `TestExtractPDF_FindsCoverImage` and `TestExtractPDF_NoCoverLeavesFieldEmpty`, which must keep passing unmodified in assertions).
- `FlateDecode` and `JPXDecode` images are explicitly out of scope for this plan (`decodePDFImageStream` returns `ok=false` for them here) — `FlateDecode` support is Plan B.
- Default page limit is 10 when unset (config value `<= 0`).
- Every new regex that matches an indirect object reference must allow any generation number (`\s+\d+\s+R` / `\s+\d+\s+obj`), not hardcode generation `0` — matching the existing convention in `pdfInfoRefRe` (`pdf.go:16`).

---

## Task 1: PDF object index (literal objects)

**Files:**
- Create: `internal/metadata/pdf_objects.go`
- Test: `internal/metadata/pdf_objects_test.go`

**Interfaces:**
- Produces: `type pdfObjIndex struct { literal map[int][]byte }`, `func buildPDFObjIndex(data []byte) *pdfObjIndex`, `func (idx *pdfObjIndex) lookup(objNum int) ([]byte, bool)`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/metadata/pdf_objects_test.go
package metadata

import (
	"bytes"
	"testing"
)

func TestBuildPDFObjIndex_LiteralLookup(t *testing.T) {
	data := []byte("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	idx := buildPDFObjIndex(data)

	body, ok := idx.lookup(1)
	if !ok {
		t.Fatal("lookup(1) not found")
	}
	if !bytes.Contains(body, []byte("/Type /Catalog")) {
		t.Errorf("lookup(1) = %q, want it to contain /Type /Catalog", body)
	}

	if _, ok := idx.lookup(99); ok {
		t.Error("lookup(99) found, want not found")
	}
}

func TestBuildPDFObjIndex_LastIncrementalUpdateWins(t *testing.T) {
	data := []byte("1 0 obj\n<< /Title (Old) >>\nendobj\n" +
		"1 0 obj\n<< /Title (New) >>\nendobj\n")
	idx := buildPDFObjIndex(data)

	body, ok := idx.lookup(1)
	if !ok {
		t.Fatal("lookup(1) not found")
	}
	if !bytes.Contains(body, []byte("(New)")) {
		t.Errorf("lookup(1) = %q, want it to contain the later revision's (New)", body)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run TestBuildPDFObjIndex -v`
Expected: FAIL with `undefined: buildPDFObjIndex` (compile error).

- [ ] **Step 3: Implement `pdf_objects.go`**

```go
// Package metadata's PDF support is a dependency-free, best-effort textual
// scanner, not a real PDF parser -- see pdf.go's package doc. This file
// adds a minimal indirect-object index on top of that same convention,
// used by the page-tree walker (pdf_pages.go) so cover selection can
// prefer true page order over incidental byte order in the file.
package metadata

import (
	"regexp"
	"strconv"
)

// pdfObjIndex resolves a PDF indirect object number to its raw body bytes
// (everything between the "obj" and "endobj" keywords).
type pdfObjIndex struct {
	literal map[int][]byte
}

var pdfIndirectObjRe = regexp.MustCompile(`(?s)(\d+)\s+\d+\s+obj(.*?)endobj`)

// buildPDFObjIndex indexes every literal "N G obj...endobj" block in data.
// When the same object number appears more than once (a PDF edited via
// incremental update, appending a new revision rather than rewriting the
// file), the LAST occurrence wins -- consistent with findInfoDictBody's
// existing "most recent update wins" convention elsewhere in this package.
func buildPDFObjIndex(data []byte) *pdfObjIndex {
	idx := &pdfObjIndex{literal: map[int][]byte{}}
	for _, m := range pdfIndirectObjRe.FindAllSubmatch(data, -1) {
		n, err := strconv.Atoi(string(m[1]))
		if err != nil {
			continue
		}
		idx.literal[n] = m[2]
	}
	return idx
}

// lookup returns the body bytes for objNum, or ok=false if it isn't in the
// literal index.
func (idx *pdfObjIndex) lookup(objNum int) ([]byte, bool) {
	body, ok := idx.literal[objNum]
	return body, ok
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run TestBuildPDFObjIndex -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_objects.go internal/metadata/pdf_objects_test.go
git commit -m "Add PDF indirect-object index for literal objects"
```

---

## Task 2: Object body splitting + nesting-aware sub-dictionary extraction

**Files:**
- Modify: `internal/metadata/pdf_objects.go`
- Test: `internal/metadata/pdf_objects_test.go`

**Interfaces:**
- Consumes: nothing new from Task 1 beyond package-local helpers.
- Produces: `func splitPDFObjectBody(body []byte) (dict []byte, stream []byte, hasStream bool)`, `func pdfSubDictValue(dict []byte, key string) (value []byte, ok bool)`.

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/metadata/pdf_objects_test.go

func TestSplitPDFObjectBody_WithStream(t *testing.T) {
	body := []byte(" << /Type /XObject /Length 5 >>\nstream\nhello\nendstream")
	dict, stream, hasStream := splitPDFObjectBody(body)
	if !hasStream {
		t.Fatal("hasStream = false, want true")
	}
	if !bytes.Contains(dict, []byte("/Type /XObject")) {
		t.Errorf("dict = %q, want it to contain /Type /XObject", dict)
	}
	if string(stream) != "hello" {
		t.Errorf("stream = %q, want %q", stream, "hello")
	}
}

func TestSplitPDFObjectBody_NoStream(t *testing.T) {
	body := []byte(" << /Type /Page /Parent 2 0 R >>")
	dict, _, hasStream := splitPDFObjectBody(body)
	if hasStream {
		t.Fatal("hasStream = true, want false")
	}
	if string(dict) != string(body) {
		t.Errorf("dict = %q, want whole body %q", dict, body)
	}
}

func TestPDFSubDictValue_NestedDict(t *testing.T) {
	// Reproduces a real library file that broke the old
	// `<<([^>]*?)>>` regex: /DecodeParms is a dictionary nested inside
	// the outer image dict, and the old regex stopped at the FIRST ">>"
	// (the inner one), never matching the outer dict at all.
	dict := []byte(`<</Type/XObject/Subtype/Image/Width 1410/Height 2000/ColorSpace/DeviceGray/BitsPerComponent 8/DecodeParms<</BitsPerComponent 8/Colors 1/Columns 1410/Predictor 2>>/Filter/FlateDecode/Length 5306>>`)
	value, ok := pdfSubDictValue(dict, "DecodeParms")
	if !ok {
		t.Fatal("pdfSubDictValue not found")
	}
	want := "/BitsPerComponent 8/Colors 1/Columns 1410/Predictor 2"
	if string(value) != want {
		t.Errorf("value = %q, want %q", value, want)
	}
}

func TestPDFSubDictValue_KeyAbsent(t *testing.T) {
	dict := []byte(`<</Type/XObject/Subtype/Image/Filter/DCTDecode>>`)
	if _, ok := pdfSubDictValue(dict, "DecodeParms"); ok {
		t.Error("pdfSubDictValue found a value, want not found")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run "TestSplitPDFObjectBody|TestPDFSubDictValue" -v`
Expected: FAIL with `undefined: splitPDFObjectBody` / `undefined: pdfSubDictValue`.

- [ ] **Step 3: Implement**

Add to `internal/metadata/pdf_objects.go` (add `"bytes"` to the import block):

```go
var pdfStreamMarkerRe = regexp.MustCompile(`(?s)^(.*?)stream\r?\n(.*)$`)

// splitPDFObjectBody splits a literal object body (as returned by
// pdfObjIndex.lookup) into its dictionary portion and, if present, its
// stream bytes. hasStream is false for a body with no "stream" keyword (a
// plain dictionary object, e.g. a Page or Pages node), in which case dict
// is the whole body and stream is nil.
func splitPDFObjectBody(body []byte) (dict []byte, stream []byte, hasStream bool) {
	m := pdfStreamMarkerRe.FindSubmatch(body)
	if m == nil {
		return body, nil, false
	}
	streamBytes := bytes.TrimSuffix(m[2], []byte("endstream"))
	streamBytes = bytes.TrimRight(streamBytes, "\r\n")
	return m[1], streamBytes, true
}

// pdfSubDictValue extracts the value of /key within dict when that value
// is itself written as an inline "<<...>>" dictionary (e.g.
// "/DecodeParms<<...>>"), correctly balancing nested "<<"/">>" pairs --
// unlike a plain regex, which stops at the first ">>" regardless of
// nesting depth. ok is false if /key isn't present or its value isn't a
// "<<...>>" dictionary.
func pdfSubDictValue(dict []byte, key string) (value []byte, ok bool) {
	re := regexp.MustCompile(`/` + regexp.QuoteMeta(key) + `\s*<<`)
	loc := re.FindIndex(dict)
	if loc == nil {
		return nil, false
	}
	depth := 1
	start := loc[1]
	i := start
	for i < len(dict)-1 {
		switch {
		case dict[i] == '<' && dict[i+1] == '<':
			depth++
			i += 2
		case dict[i] == '>' && dict[i+1] == '>':
			depth--
			i += 2
			if depth == 0 {
				return dict[start : i-2], true
			}
		default:
			i++
		}
	}
	return nil, false
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run "TestSplitPDFObjectBody|TestPDFSubDictValue" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_objects.go internal/metadata/pdf_objects_test.go
git commit -m "Add nesting-aware PDF sub-dictionary extraction"
```

---

## Task 3: ObjStm decompression + lookup fallback

**Files:**
- Modify: `internal/metadata/pdf_objects.go`
- Test: `internal/metadata/pdf_objects_test.go`

**Interfaces:**
- Consumes: `splitPDFObjectBody` (Task 2).
- Produces: `idx.lookup` now also resolves objects compressed inside `/Type /ObjStm` streams. `func (idx *pdfObjIndex) resolveObjStms()` (internal, called lazily by `lookup`).

- [ ] **Step 1: Write the failing test**

```go
// append to internal/metadata/pdf_objects_test.go
// add "compress/zlib" and "fmt" to this test file's imports

func TestPDFObjIndex_ResolvesObjectInsideObjStm(t *testing.T) {
	// Builds an ObjStm containing two compressed objects, per the PDF
	// spec's layout: header is N whitespace-separated "objNum offset"
	// pairs, then object bodies concatenated starting at byte offset
	// /First. Offsets are computed from the actual fixture strings rather
	// than hardcoded, so the test can't silently rot if either object's
	// text changes length.
	obj5 := "<</Type/Page/Parent 1 0 R>>"
	obj6 := "<</Foo/Bar>>"
	header := fmt.Sprintf("5 0 6 %d", len(obj5))
	content := header + obj5 + obj6
	first := len(header)

	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	var data bytes.Buffer
	fmt.Fprintf(&data, "9 0 obj\n<< /Type /ObjStm /N 2 /First %d /Filter /FlateDecode /Length %d >>\nstream\n", first, compressed.Len())
	data.Write(compressed.Bytes())
	data.WriteString("\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data.Bytes())

	body5, ok := idx.lookup(5)
	if !ok {
		t.Fatal("lookup(5) not found")
	}
	if !bytes.Contains(body5, []byte("/Type/Page")) {
		t.Errorf("lookup(5) = %q, want it to contain /Type/Page", body5)
	}

	body6, ok := idx.lookup(6)
	if !ok {
		t.Fatal("lookup(6) not found")
	}
	if string(body6) != obj6 {
		t.Errorf("lookup(6) = %q, want %q", body6, obj6)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/metadata/... -run TestPDFObjIndex_ResolvesObjectInsideObjStm -v`
Expected: FAIL (lookup(5) not found — ObjStm not yet resolved).

- [ ] **Step 3: Implement**

Update the `pdfObjIndex` struct and add ObjStm resolution to `internal/metadata/pdf_objects.go` (add `"bytes"` (already added), `"compress/zlib"`, `"io"` to imports):

```go
type pdfObjIndex struct {
	literal        map[int][]byte
	objStm         map[int][]byte
	objStmResolved bool
}

var pdfObjStmTypeRe = regexp.MustCompile(`/Type\s*/ObjStm\b`)
var pdfObjStmNRe = regexp.MustCompile(`/N\s+(\d+)`)
var pdfObjStmFirstRe = regexp.MustCompile(`/First\s+(\d+)`)

// resolveObjStms decompresses every /Type /ObjStm object in the literal
// index and indexes the objects compressed inside it, so lookup can
// resolve objects that never appear as literal text -- PDF producers
// increasingly compress non-stream objects like Page/Pages nodes into
// ObjStm for space savings (confirmed present in ~30% of a real 192-PDF
// library sample during this feature's design). Runs at most once per
// index.
func (idx *pdfObjIndex) resolveObjStms() {
	if idx.objStmResolved {
		return
	}
	idx.objStmResolved = true
	idx.objStm = map[int][]byte{}

	for _, body := range idx.literal {
		dict, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream || !pdfObjStmTypeRe.Match(dict) {
			continue
		}
		nMatch := pdfObjStmNRe.FindSubmatch(dict)
		firstMatch := pdfObjStmFirstRe.FindSubmatch(dict)
		if nMatch == nil || firstMatch == nil {
			continue
		}
		n, err1 := strconv.Atoi(string(nMatch[1]))
		first, err2 := strconv.Atoi(string(firstMatch[1]))
		if err1 != nil || err2 != nil || n <= 0 || first < 0 {
			continue
		}

		r, err := zlib.NewReader(bytes.NewReader(stream))
		if err != nil {
			continue
		}
		inflated, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			continue
		}
		idx.indexObjStmContents(inflated, n, first)
	}
}

// indexObjStmContents parses an inflated ObjStm's header (N whitespace-
// separated "objNum offset" pairs, per the PDF spec) and slices out each
// compressed object's body, indexing it under idx.objStm.
func (idx *pdfObjIndex) indexObjStmContents(inflated []byte, n, first int) {
	if first > len(inflated) {
		return
	}
	fields := bytes.Fields(inflated[:first])
	if len(fields) < 2*n {
		return
	}
	type pair struct{ num, offset int }
	pairs := make([]pair, n)
	for i := 0; i < n; i++ {
		num, err1 := strconv.Atoi(string(fields[2*i]))
		off, err2 := strconv.Atoi(string(fields[2*i+1]))
		if err1 != nil || err2 != nil {
			return
		}
		pairs[i] = pair{num, off}
	}
	for i, p := range pairs {
		start := first + p.offset
		end := len(inflated)
		if i+1 < n {
			end = first + pairs[i+1].offset
		}
		if start < 0 || end > len(inflated) || start > end {
			continue
		}
		idx.objStm[p.num] = inflated[start:end]
	}
}
```

Update `lookup` to fall back to ObjStm resolution:

```go
func (idx *pdfObjIndex) lookup(objNum int) ([]byte, bool) {
	if body, ok := idx.literal[objNum]; ok {
		return body, true
	}
	if !idx.objStmResolved {
		idx.resolveObjStms()
	}
	body, ok := idx.objStm[objNum]
	return body, ok
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/metadata/... -run TestPDFObjIndex_ResolvesObjectInsideObjStm -v`
Expected: PASS

Also run the full package to confirm no regressions:
Run: `go test ./internal/metadata/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_objects.go internal/metadata/pdf_objects_test.go
git commit -m "Resolve PDF objects compressed inside ObjStm streams"
```

---

## Task 4: Indirect-or-inline dict value resolution + find-by-type

**Files:**
- Modify: `internal/metadata/pdf_objects.go`
- Test: `internal/metadata/pdf_objects_test.go`

**Interfaces:**
- Consumes: `pdfSubDictValue` (Task 2), `idx.lookup`/`idx.resolveObjStms` (Tasks 1, 3).
- Produces: `func resolveDictValue(idx *pdfObjIndex, dict []byte, key string) (value []byte, ok bool)`, `func (idx *pdfObjIndex) find(typeRe *regexp.Regexp) (dict []byte, ok bool)`.

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/metadata/pdf_objects_test.go
// add "regexp" to this test file's imports

func TestResolveDictValue_InlineDict(t *testing.T) {
	dict := []byte(`<</Type/Page/Resources<</Font<</F1 3 0 R>>>>>>`)
	value, ok := resolveDictValue(nil, dict, "Resources")
	if !ok {
		t.Fatal("resolveDictValue not found")
	}
	if string(value) != "/Font<</F1 3 0 R>>" {
		t.Errorf("value = %q, want %q", value, "/Font<</F1 3 0 R>>")
	}
}

func TestResolveDictValue_IndirectRef(t *testing.T) {
	data := []byte("1 0 obj\n<< /Type /Page /Resources 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Font << /F1 3 0 R >> >>\nendobj\n")
	idx := buildPDFObjIndex(data)
	pageBody, _ := idx.lookup(1)
	dict, _, _ := splitPDFObjectBody(pageBody)

	value, ok := resolveDictValue(idx, dict, "Resources")
	if !ok {
		t.Fatal("resolveDictValue not found")
	}
	if !bytes.Contains(value, []byte("/F1 3 0 R")) {
		t.Errorf("value = %q, want it to contain /F1 3 0 R", value)
	}
}

func TestPDFObjIndex_Find(t *testing.T) {
	data := []byte("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n")
	idx := buildPDFObjIndex(data)

	dict, ok := idx.find(regexp.MustCompile(`/Type\s*/Catalog\b`))
	if !ok {
		t.Fatal("find(Catalog) not found")
	}
	if !bytes.Contains(dict, []byte("/Pages 2 0 R")) {
		t.Errorf("dict = %q, want it to contain /Pages 2 0 R", dict)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run "TestResolveDictValue|TestPDFObjIndex_Find" -v`
Expected: FAIL with `undefined: resolveDictValue` / `idx.find undefined`.

- [ ] **Step 3: Implement**

Add to `internal/metadata/pdf_objects.go`:

```go
// resolveDictValue returns the raw dict bytes for /key within dict,
// whether its value is written inline ("/key<<...>>", via
// pdfSubDictValue) or as an indirect reference ("/key N G R", resolved
// through idx). Real-world PDFs commonly write /Resources (and, within
// it, /XObject) as an indirect reference rather than an inline
// dictionary, as a producer optimization for Resources shared across
// pages. ok is false if /key isn't present, or its reference can't be
// resolved via idx.
func resolveDictValue(idx *pdfObjIndex, dict []byte, key string) (value []byte, ok bool) {
	if sub, ok := pdfSubDictValue(dict, key); ok {
		return sub, true
	}
	refRe := regexp.MustCompile(`/` + regexp.QuoteMeta(key) + `\s+(\d+)\s+\d+\s+R`)
	m := refRe.FindSubmatch(dict)
	if m == nil {
		return nil, false
	}
	objNum, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return nil, false
	}
	body, ok := idx.lookup(objNum)
	if !ok {
		return nil, false
	}
	resolvedDict, _, _ := splitPDFObjectBody(body)
	return resolvedDict, true
}

// find scans every indexed object (literal first, then ObjStm-compressed
// ones) for one whose dict matches typeRe, returning its dict bytes. Used
// to locate singleton objects like /Type /Catalog that aren't referenced
// by a fixed, predictable object number.
func (idx *pdfObjIndex) find(typeRe *regexp.Regexp) (dict []byte, ok bool) {
	for _, body := range idx.literal {
		d, _, _ := splitPDFObjectBody(body)
		if typeRe.Match(d) {
			return d, true
		}
	}
	idx.resolveObjStms()
	for _, body := range idx.objStm {
		if typeRe.Match(body) {
			return body, true
		}
	}
	return nil, false
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run "TestResolveDictValue|TestPDFObjIndex_Find" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_objects.go internal/metadata/pdf_objects_test.go
git commit -m "Add indirect-or-inline dict value resolution and find-by-type"
```

---

## Task 5: Page tree walker

**Files:**
- Create: `internal/metadata/pdf_pages.go`
- Test: `internal/metadata/pdf_pages_test.go`

**Interfaces:**
- Consumes: `pdfObjIndex`, `idx.lookup`, `idx.find`, `resolveDictValue`, `splitPDFObjectBody` (Tasks 1-4).
- Produces: `type pdfPage struct { number int; dict []byte }`, `func walkPDFPageTree(idx *pdfObjIndex, limit int) (pages []pdfPage, ok bool)`, `const defaultPDFCoverPageLimit = 10`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/metadata/pdf_pages_test.go
package metadata

import "testing"

func pagesTreeFixture() []byte {
	return []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n" +
			"5 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")
}

func TestWalkPDFPageTree_OrderedByKids(t *testing.T) {
	idx := buildPDFObjIndex(pagesTreeFixture())
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	if len(pages) != 3 {
		t.Fatalf("len(pages) = %d, want 3", len(pages))
	}
	for i, p := range pages {
		if p.number != i+1 {
			t.Errorf("pages[%d].number = %d, want %d", i, p.number, i+1)
		}
	}
}

func TestWalkPDFPageTree_RespectsLimit(t *testing.T) {
	idx := buildPDFObjIndex(pagesTreeFixture())
	pages, ok := walkPDFPageTree(idx, 2)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	if len(pages) != 2 {
		t.Fatalf("len(pages) = %d, want 2 (limit)", len(pages))
	}
}

func TestWalkPDFPageTree_ZeroLimitUsesDefault(t *testing.T) {
	idx := buildPDFObjIndex(pagesTreeFixture())
	pages, ok := walkPDFPageTree(idx, 0)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	if len(pages) != 3 {
		t.Fatalf("len(pages) = %d, want 3 (fixture has fewer pages than the default limit)", len(pages))
	}
}

func TestWalkPDFPageTree_NestedKids(t *testing.T) {
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [6 0 R 4 0 R] >>\nendobj\n" +
			"6 0 obj\n<< /Type /Pages /Parent 2 0 R /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 6 0 R >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Parent 2 0 R >>\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	if len(pages) != 2 {
		t.Fatalf("len(pages) = %d, want 2", len(pages))
	}
}

func TestWalkPDFPageTree_NoCatalogReturnsNotOK(t *testing.T) {
	idx := buildPDFObjIndex([]byte("1 0 obj\n<< /Title (No page tree here) >>\nendobj\n"))
	if _, ok := walkPDFPageTree(idx, 10); ok {
		t.Error("walkPDFPageTree ok = true, want false (no /Type /Catalog present)")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run TestWalkPDFPageTree -v`
Expected: FAIL with `undefined: walkPDFPageTree` (compile error).

- [ ] **Step 3: Implement `pdf_pages.go`**

```go
// Page-tree resolution for PDF cover selection: walks Catalog -> Pages ->
// Kids (via pdf_objects.go's pdfObjIndex) to produce pages in true
// document reading order, so cover selection (pdf_images.go, pdf.go) can
// prefer "page 1's image" over "whichever image object happens to appear
// first in file byte order" -- these are not the same thing in general,
// since PDF object order has no required relationship to page order.
package metadata

import (
	"regexp"
	"strconv"
)

const defaultPDFCoverPageLimit = 10

var pdfCatalogTypeRe = regexp.MustCompile(`/Type\s*/Catalog\b`)
var pdfPagesRefRe = regexp.MustCompile(`/Pages\s+(\d+)\s+\d+\s+R`)
var pdfTypePageLeafRe = regexp.MustCompile(`/Type\s*/Page\b`)
var pdfKidsRe = regexp.MustCompile(`/Kids\s*\[([^\]]*)\]`)
var pdfKidRefRe = regexp.MustCompile(`(\d+)\s+\d+\s+R`)

// pdfPage is one page's resolved dict, plus its 1-based position in
// document reading order.
type pdfPage struct {
	number int
	dict   []byte
}

// walkPDFPageTree resolves the document's page tree into an ordered list
// of page dicts, stopping once limit pages have been collected. limit <=
// 0 uses defaultPDFCoverPageLimit. ok is false if the Catalog, its Pages
// root, or every Kids reference can't be resolved -- callers treat that
// as "fall back to legacy whole-file behavior" rather than an error,
// since malformed/atypical PDFs are expected for this best-effort
// package.
func walkPDFPageTree(idx *pdfObjIndex, limit int) (pages []pdfPage, ok bool) {
	if limit <= 0 {
		limit = defaultPDFCoverPageLimit
	}
	catalog, ok := idx.find(pdfCatalogTypeRe)
	if !ok {
		return nil, false
	}
	rootMatch := pdfPagesRefRe.FindSubmatch(catalog)
	if rootMatch == nil {
		return nil, false
	}
	rootNum, err := strconv.Atoi(string(rootMatch[1]))
	if err != nil {
		return nil, false
	}

	visited := map[int]bool{}
	collectPDFPages(idx, rootNum, &pages, visited, limit)
	return pages, len(pages) > 0
}

// collectPDFPages recursively walks the Pages tree node at objNum,
// appending every /Type /Page leaf found (in Kids order) to pages, until
// limit have been collected. visited guards against a malformed/cyclic
// Kids reference looping forever; one unresolvable Kids entry is skipped
// rather than aborting the whole walk.
func collectPDFPages(idx *pdfObjIndex, objNum int, pages *[]pdfPage, visited map[int]bool, limit int) {
	if len(*pages) >= limit || visited[objNum] {
		return
	}
	visited[objNum] = true

	body, ok := idx.lookup(objNum)
	if !ok {
		return
	}
	dict, _, _ := splitPDFObjectBody(body)

	if pdfTypePageLeafRe.Match(dict) {
		*pages = append(*pages, pdfPage{number: len(*pages) + 1, dict: dict})
		return
	}

	kidsMatch := pdfKidsRe.FindSubmatch(dict)
	if kidsMatch == nil {
		return
	}
	for _, ref := range pdfKidRefRe.FindAllSubmatch(kidsMatch[1], -1) {
		if len(*pages) >= limit {
			return
		}
		kidNum, err := strconv.Atoi(string(ref[1]))
		if err != nil {
			continue
		}
		collectPDFPages(idx, kidNum, pages, visited, limit)
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run TestWalkPDFPageTree -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_pages.go internal/metadata/pdf_pages_test.go
git commit -m "Add PDF page-tree walker (Catalog -> Pages -> Kids)"
```

---

## Task 6: Per-page image enumeration + DCTDecode extraction

**Files:**
- Create: `internal/metadata/pdf_images.go`
- Test: `internal/metadata/pdf_images_test.go`

**Interfaces:**
- Consumes: `pdfPage`, `walkPDFPageTree` (Task 5); `resolveDictValue`, `idx.lookup`, `splitPDFObjectBody` (Tasks 1-4); existing `pdfSubtypeImageRe`, `pdfDCTDecodeRe` (`pdf.go:18-19`).
- Produces: `type pdfPageImage struct { page int; bytes []byte; contentType string }`, `func findPDFPageImages(idx *pdfObjIndex, pages []pdfPage, stopAtFirst bool) []pdfPageImage`, `func decodePDFImageStream(dict, stream []byte) (data []byte, contentType string, ok bool)`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/metadata/pdf_images_test.go
package metadata

import "testing"

func TestFindPDFPageImages_FindsDCTDecodeOnFirstPage(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 4 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, true)
	if len(images) != 1 {
		t.Fatalf("len(images) = %d, want 1", len(images))
	}
	if images[0].page != 1 {
		t.Errorf("images[0].page = %d, want 1", images[0].page)
	}
	if string(images[0].bytes) != string(jpegData) {
		t.Errorf("images[0].bytes = %q, want %q", images[0].bytes, jpegData)
	}
	if images[0].contentType != "image/jpeg" {
		t.Errorf("images[0].contentType = %q, want image/jpeg", images[0].contentType)
	}
}

func TestFindPDFPageImages_SkipsPagesWithNoImageUntilOneHasOne(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /Font << /F1 9 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 5 0 R >> >> >>\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, true)
	if len(images) != 1 {
		t.Fatalf("len(images) = %d, want 1", len(images))
	}
	if images[0].page != 2 {
		t.Errorf("images[0].page = %d, want 2 (page 1 has no image)", images[0].page)
	}
}

func TestFindPDFPageImages_StopAtFirstFalseCollectsAll(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 5 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Resources << /XObject << /Im1 6 0 R >> >> >>\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n" +
			"6 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, false)
	if len(images) != 2 {
		t.Fatalf("len(images) = %d, want 2", len(images))
	}
}

func TestDecodePDFImageStream_SkipsNonDCTDecode(t *testing.T) {
	dict := []byte(`<</Type/XObject/Subtype/Image/Filter/FlateDecode>>`)
	if _, _, ok := decodePDFImageStream(dict, []byte("rawbytes")); ok {
		t.Error("decodePDFImageStream ok = true, want false (FlateDecode not supported by this plan)")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run "TestFindPDFPageImages|TestDecodePDFImageStream" -v`
Expected: FAIL with `undefined: findPDFPageImages` (compile error).

- [ ] **Step 3: Implement `pdf_images.go`**

```go
// Per-page image enumeration for PDF cover selection: given an ordered
// page list from pdf_pages.go, finds qualifying image XObjects on each
// page in turn.
package metadata

import (
	"bytes"
	"regexp"
	"strconv"
)

// pdfPageImage is one candidate cover image found while walking the page
// tree: which page (1-based, matching pdfPage.number) it came from, plus
// its already-decoded, display-ready bytes.
type pdfPageImage struct {
	page        int
	bytes       []byte
	contentType string
}

var pdfXObjectEntryRe = regexp.MustCompile(`/\w+\s+(\d+)\s+\d+\s+R`)

// findPDFPageImages returns every qualifying image found across pages (in
// page, then XObject, order). When stopAtFirst is true, the walk returns
// as soon as one qualifying image is found -- the normal auto-detect
// path used by findPDFCoverPageAware (pdf.go). A later plan's override
// picker calls this with stopAtFirst=false to collect every candidate for
// its thumbnail grid.
func findPDFPageImages(idx *pdfObjIndex, pages []pdfPage, stopAtFirst bool) []pdfPageImage {
	var found []pdfPageImage
	for _, p := range pages {
		resources, ok := resolveDictValue(idx, p.dict, "Resources")
		if !ok {
			continue
		}
		xobjects, ok := resolveDictValue(idx, resources, "XObject")
		if !ok {
			continue
		}
		for _, ref := range pdfXObjectEntryRe.FindAllSubmatch(xobjects, -1) {
			objNum, err := strconv.Atoi(string(ref[1]))
			if err != nil {
				continue
			}
			body, ok := idx.lookup(objNum)
			if !ok {
				continue
			}
			imgDict, imgStream, hasStream := splitPDFObjectBody(body)
			if !hasStream || !pdfSubtypeImageRe.Match(imgDict) {
				continue
			}
			data, contentType, ok := decodePDFImageStream(imgDict, imgStream)
			if !ok {
				continue
			}
			found = append(found, pdfPageImage{page: p.number, bytes: data, contentType: contentType})
			if stopAtFirst {
				return found
			}
		}
	}
	return found
}

// decodePDFImageStream turns an image XObject's raw stream bytes into
// display-ready image bytes. DCTDecode streams are already a complete
// JPEG file and pass through unchanged (matching the pre-existing
// findPDFCover behavior in pdf.go). FlateDecode raster reconstruction is
// added in a later plan; for now those images -- and JPXDecode, and any
// other filter -- are skipped.
func decodePDFImageStream(dict, stream []byte) (data []byte, contentType string, ok bool) {
	if !pdfDCTDecodeRe.Match(dict) {
		return nil, "", false
	}
	trimmed := bytes.TrimRight(stream, "\r\n")
	if len(trimmed) == 0 {
		return nil, "", false
	}
	return trimmed, "image/jpeg", true
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run "TestFindPDFPageImages|TestDecodePDFImageStream" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_images.go internal/metadata/pdf_images_test.go
git commit -m "Add per-page PDF image enumeration with DCTDecode extraction"
```

---

## Task 7: Wire page-aware selection into extractPDF, with legacy fallback

**Files:**
- Modify: `internal/metadata/pdf.go`
- Modify: `internal/metadata/pdf_test.go`
- Modify: `internal/metadata/extractor.go` (temporary call-site fix, replaced properly in Task 8)

**Interfaces:**
- Consumes: `walkPDFPageTree` (Task 5), `findPDFPageImages` (Task 6), existing `findPDFCover` (`pdf.go:136-149`, unchanged).
- Produces: `func findPDFCoverPageAware(data []byte, pageLimit int) ([]byte, string, bool)`; `extractPDF` signature becomes `func extractPDF(path string, pageLimit int) (Result, error)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/metadata/pdf_test.go`:

```go
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
```

Then update every existing `extractPDF(path)` call in `internal/metadata/pdf_test.go` to `extractPDF(path, 10)`:

Run: `sed -i 's/extractPDF(path)/extractPDF(path, 10)/g' internal/metadata/pdf_test.go`

And fix the one remaining call site so the package still compiles for this test run, `internal/metadata/extractor.go:29`:

Run: `sed -i 's/extractPDF(path)/extractPDF(path, 0)/' internal/metadata/extractor.go`

- [ ] **Step 2: Run the tests to verify the new test fails**

Run: `go test ./internal/metadata/... -run TestExtractPDF -v`
Expected: `TestExtractPDF_PageAwareCoverPrefersPageOrderOverByteOrder` FAILs (CoverBytes will be page 2's image — today's `findPDFCover` picks whichever image object appears first in the file, which is object `5 0 obj`, page 2's). All other `TestExtractPDF_*` tests PASS unchanged (signature-only edit).

- [ ] **Step 3: Implement**

Add to `internal/metadata/pdf.go`, near `findPDFCover`:

```go
// findPDFCoverPageAware is the primary cover-selection entry point: it
// walks the page tree (walkPDFPageTree, pdf_pages.go) and returns the
// first qualifying image found within the first pageLimit pages, in page
// order. If the page tree can't be resolved at all, or no qualifying
// image turns up within the page limit, it falls back to findPDFCover's
// whole-file byte-order scan -- so this is never worse than the
// pre-page-aware behavior, only better when a real page tree is present.
func findPDFCoverPageAware(data []byte, pageLimit int) ([]byte, string, bool) {
	idx := buildPDFObjIndex(data)
	if pages, ok := walkPDFPageTree(idx, pageLimit); ok {
		if images := findPDFPageImages(idx, pages, true); len(images) > 0 {
			return images[0].bytes, images[0].contentType, true
		}
	}
	return findPDFCover(data)
}
```

Change `extractPDF`'s signature and its cover-lookup call (`pdf.go:170`, `pdf.go:208`):

```go
func extractPDF(path string, pageLimit int) (Result, error) {
	// ... unchanged body ...
	if coverData, coverContentType, ok := findPDFCoverPageAware(data, pageLimit); ok {
		result.CoverBytes = coverData
		result.CoverContentType = coverContentType
	}
	return result, nil
}
```

(Only the signature and the final `if coverData, ...` line change; everything else in `extractPDF`'s body is untouched.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/...`
Expected: PASS (including `TestExtractPDF_FindsCoverImage` and `TestExtractPDF_NoCoverLeavesFieldEmpty`, both exercising the legacy-fallback path since their fixtures have no Catalog/Pages tree).

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf.go internal/metadata/pdf_test.go internal/metadata/extractor.go
git commit -m "Select PDF covers by page order, falling back to legacy byte-order scan"
```

---

## Task 8: Config wiring (`pdf_cover_page_limit`)

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `config.yaml.example`
- Modify: `internal/metadata/extractor.go`
- Modify: `internal/metadata/extractor_test.go`
- Modify: `internal/pipeline/pipeline.go`
- Modify: `internal/librarian/librarian.go`

**Interfaces:**
- Consumes: `extractPDF(path string, pageLimit int)` (Task 7).
- Produces: `config.General.PDFCoverPageLimit int`; `metadata.Extract(path string, hyphenExceptions []string, pdfCoverPageLimit int) (Result, error)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go` (extend `sampleYAML`'s `general:` block and add an assertion in `TestLoad`):

```yaml
# add this line inside sampleYAML's "general:" block, alongside the existing keys:
  pdf_cover_page_limit: 10
```

```go
// add inside TestLoad, alongside its other General-field assertions:
if cfg.General.PDFCoverPageLimit != 10 {
    t.Errorf("General.PDFCoverPageLimit = %d, want 10", cfg.General.PDFCoverPageLimit)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/... -run TestLoad -v`
Expected: FAIL with `cfg.General.PDFCoverPageLimit undefined` (compile error).

- [ ] **Step 3: Implement**

In `internal/config/config.go`, add the field to `General`:

```go
type General struct {
	WorkingFolder      string `yaml:"working_folder"`
	LibraryFolder      string `yaml:"library_folder"`
	LogFolder          string `yaml:"log_folder"`
	FilenameFormat     string `yaml:"filename_format"`
	PDFCoverPageLimit  int    `yaml:"pdf_cover_page_limit"`
}
```

In `config.yaml.example`, add the key to the `general:` block with a comment:

```yaml
general:
  working_folder: ""
  library_folder: ""
  log_folder: ""
  filename_format: "{title} ({year}) - {author}"
  # How many pages (from the start of the document) to search for a cover
  # image before giving up on page-aware detection. Defaults to 10 if
  # unset or 0.
  pdf_cover_page_limit: 10
```

In `internal/metadata/extractor.go`, thread the new parameter through:

```go
func Extract(path string, hyphenExceptions []string, pdfCoverPageLimit int) (Result, error) {
	ext := strings.ToLower(filepath.Ext(path))
	var result Result
	var err error
	switch ext {
	case ".epub":
		result, err = extractEpub(path)
	case ".pdf":
		result, err = extractPDF(path, pdfCoverPageLimit)
	case ".mobi", ".azw3":
		result, err = extractMobi(path)
	default:
		return Result{}, fmt.Errorf("unsupported extension: %s", ext)
	}
	// ... rest unchanged ...
}
```

In `internal/pipeline/pipeline.go:43`, update the call:

```go
if res, err := metadata.Extract(path, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit); err == nil {
```

In `internal/librarian/librarian.go:71`, update the call:

```go
if res, err := metadata.Extract(path, cfg.TitleFormatting.HyphenExceptions, cfg.General.PDFCoverPageLimit); err == nil {
```

- [ ] **Step 4: Fix the remaining call sites and run the full test suite**

Update every `Extract(path, nil)` / `Extract(epubPath, nil)` / `Extract(pdfPath, nil)` / `Extract(mobiPath, nil)` / `Extract(azw3Path, nil)` call in `internal/metadata/extractor_test.go` to pass a third argument of `0`:

Run: `sed -i -E 's/Extract\(([a-zA-Z0-9]+Path), nil\)/Extract(\1, nil, 0)/g' internal/metadata/extractor_test.go`

Also update the one call in `TestExtract_FormatsTitle` that passes a non-nil slice:

Run: `sed -i 's/Extract(epubPath, \[\]string{"High-Performance"})/Extract(epubPath, []string{"High-Performance"}, 0)/' internal/metadata/extractor_test.go`

Then run everything:

Run: `go build ./... && go test ./...`
Expected: PASS across the whole module (`internal/config`, `internal/metadata`, `internal/pipeline`, `internal/librarian`, `internal/appapi`, `desktop`).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go config.yaml.example \
        internal/metadata/extractor.go internal/metadata/extractor_test.go \
        internal/pipeline/pipeline.go internal/librarian/librarian.go
git commit -m "Add pdf_cover_page_limit config option and thread it through Extract"
```

---

## Manual verification (after all tasks)

- [ ] Set `pdf_cover_page_limit: 10` (or leave unset to confirm the default) in the real `~/.config/BLibOrg/config.yaml`.
- [ ] Run the desktop app, open the Library view, and spot-check a handful of the books identified during this feature's design as previously mis-covered or cover-less due to the nested-`/DecodeParms` blind spot (e.g. `Residues - Time, Uncertainty, and Change in Software Architecture`, `Atomic Kotlin`) — confirm they still show no cover (their real cover images are `FlateDecode`, out of scope until Plan B) but no longer error or regress anything else.
- [ ] Spot-check a few JPEG-covered books whose first-in-file-byte-order image previously happened to differ from their true page-1 image, if any are known, to confirm the displayed cover is now the actual front cover.
- [ ] Confirm `go vet ./...` is clean.
