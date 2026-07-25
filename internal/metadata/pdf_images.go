// Per-page image enumeration for PDF cover selection: given an ordered
// page list from pdf_pages.go, finds qualifying image XObjects on each
// page in turn -- recursing into Form XObjects (/Subtype /Form) a page
// references, since real-world PDFs (particularly prepress/OPI-workflow
// output) commonly nest the actual cover image one or more levels inside
// a Form rather than directly in the page's own Resources.
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
var pdfSubtypeFormRe = regexp.MustCompile(`/Subtype\s*/Form`)

// maxFormXObjectDepth caps how many levels of Form XObject nesting
// findImagesInXObjects will recurse into. A real cover image is 1 level
// deep (a single Form wrapping the image, common in prepress/OPI
// workflows); 4 is generous headroom without risking runaway recursion
// on a malformed/pathologically-nested PDF. Combined with the visited
// set (also passed to findImagesInXObjects), a cyclic Form reference
// terminates immediately regardless of this cap.
const maxFormXObjectDepth = 4

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
		visited := map[int]bool{}
		if findImagesInXObjects(idx, p.number, resources, xobjects, 0, visited, stopAtFirst, &found) {
			return found
		}
	}
	return found
}

// findImagesInXObjects scans xobjects (a page's or a Form XObject's own
// /XObject dict) for qualifying images, recursing into any /Subtype
// /Form entries found up to maxFormXObjectDepth levels. visited guards
// against a malformed/cyclic Form reference (shared across the whole
// recursion tree for one page, the same way collectPDFPages' visited map
// guards Kids cycles in pdf_pages.go). Found images are appended to
// *found, tagged with pageNumber -- the page they were ultimately
// reached from, regardless of how many Form levels deep they were
// nested, since pdfPageImage's contract is "which page to show this
// cover for," not "which object declared it." Returns true once
// stopAtFirst is satisfied, signalling the caller to stop walking
// further pages too.
func findImagesInXObjects(idx *pdfObjIndex, pageNumber int, resources, xobjects []byte, depth int, visited map[int]bool, stopAtFirst bool, found *[]pdfPageImage) bool {
	for _, ref := range pdfXObjectEntryRe.FindAllSubmatch(xobjects, -1) {
		objNum, err := strconv.Atoi(string(ref[1]))
		if err != nil {
			continue
		}
		if visited[objNum] {
			continue
		}
		body, ok := idx.lookup(objNum)
		if !ok {
			continue
		}
		dict, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream {
			continue
		}

		if pdfSubtypeImageRe.Match(dict) {
			data, contentType, ok := decodePDFImageStream(idx, resources, dict, stream)
			if !ok {
				continue
			}
			*found = append(*found, pdfPageImage{page: pageNumber, bytes: data, contentType: contentType})
			if stopAtFirst {
				return true
			}
			continue
		}

		if pdfSubtypeFormRe.Match(dict) && depth < maxFormXObjectDepth {
			formResources, ok := resolveDictValue(idx, dict, "Resources")
			if !ok {
				continue
			}
			formXObjects, ok := resolveDictValue(idx, formResources, "XObject")
			if !ok {
				continue
			}
			visited[objNum] = true
			if findImagesInXObjects(idx, pageNumber, formResources, formXObjects, depth+1, visited, stopAtFirst, found) {
				return true
			}
		}
	}
	return false
}

// decodePDFImageStream turns an image XObject's raw stream bytes into
// display-ready image bytes. DCTDecode streams are already a complete
// JPEG file and pass through unchanged. FlateDecode streams are
// reconstructed via decodeFlatePDFImage (pdf_flate.go) -- predictor undo,
// colorspace mapping, and PNG re-encoding. Any other filter (or a
// FlateDecode image this package can't fully resolve) returns ok=false.
func decodePDFImageStream(idx *pdfObjIndex, resources, dict, stream []byte) (data []byte, contentType string, ok bool) {
	if pdfDCTDecodeRe.Match(dict) {
		// splitPDFObjectBody may leave "endstream" in the stream if there's a
		// trailing newline before it, so we trim it here and then any remaining
		// whitespace.
		trimmed := bytes.TrimSuffix(stream, []byte("endstream"))
		trimmed = bytes.TrimRight(trimmed, "\r\n")
		if len(trimmed) == 0 {
			return nil, "", false
		}
		return trimmed, "image/jpeg", true
	}
	return decodeFlatePDFImage(idx, resources, dict, stream)
}
