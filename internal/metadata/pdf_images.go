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
// JPEG file and pass through unchanged. FlateDecode streams are
// reconstructed via decodeFlatePDFImage (pdf_flate.go) -- predictor undo,
// colorspace mapping, and PNG re-encoding. Any other filter (or a
// FlateDecode image this package can't fully resolve) returns ok=false.
func decodePDFImageStream(dict, stream []byte) (data []byte, contentType string, ok bool) {
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
	return decodeFlatePDFImage(dict, stream)
}
