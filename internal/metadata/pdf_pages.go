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
var pdfKidsIndirectRe = regexp.MustCompile(`/Kids\s+(\d+)\s+\d+\s+R`)
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

	kidsContent, ok := resolvePDFKidsContent(idx, dict)
	if !ok {
		return
	}
	for _, ref := range pdfKidRefRe.FindAllSubmatch(kidsContent, -1) {
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

// resolvePDFKidsContent returns the byte range holding a Pages node's
// Kids array entries -- either the inline "[...]" array's own content
// (pdfKidsRe's usual case), or, when /Kids is instead an indirect
// reference to a separate object holding the array (e.g. "/Kids 6 0 R",
// rather than "/Kids [...]" inlined in this dict -- a pattern some PDF
// producers use, apparently to keep large Pages dicts smaller), that
// object's own body resolved via idx.lookup. Either shape's result feeds
// pdfKidRefRe.FindAllSubmatch the same way: pdfKidRefRe matches "N G R"
// patterns anywhere in its input, so the resolved object's raw body --
// brackets and all -- works without needing to re-extract just the
// bracketed interior. ok is false if neither shape matches, or the
// indirect object can't be resolved.
func resolvePDFKidsContent(idx *pdfObjIndex, dict []byte) ([]byte, bool) {
	if m := pdfKidsRe.FindSubmatch(dict); m != nil {
		return m[1], true
	}
	m := pdfKidsIndirectRe.FindSubmatch(dict)
	if m == nil {
		return nil, false
	}
	objNum, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return nil, false
	}
	return idx.lookup(objNum)
}
