// This file backs the manual cover-override picker with page-level
// granularity Extract's single combined Result can't expose: listing
// every candidate image across a PDF's first pageLimit pages (for the
// picker's thumbnail grid), and re-extracting one specific page's image
// (once the user has chosen it, and on every later scan while that
// override is in effect).
package metadata

import "os"

// PDFCoverCandidate is one image found on a specific page during
// ListPDFCoverCandidates' collect-all walk.
type PDFCoverCandidate struct {
	Page        int
	Bytes       []byte
	ContentType string
}

// ListPDFCoverCandidates walks up to pageLimit pages of the PDF at path
// and returns every qualifying image found (not just the first, unlike
// Extract's auto-detect path), for the cover-override picker's thumbnail
// grid. Returns an empty (not nil-error) slice if the page tree can't be
// resolved at all -- matching this package's convention of degrading
// gracefully for atypical PDFs rather than erroring.
func ListPDFCoverCandidates(path string, pageLimit int) ([]PDFCoverCandidate, error) {
	idx, pages, ok, err := loadPDFPages(path, pageLimit)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	images := findPDFPageImages(idx, pages, false)
	candidates := make([]PDFCoverCandidate, len(images))
	for i, img := range images {
		candidates[i] = PDFCoverCandidate{Page: img.page, Bytes: img.bytes, ContentType: img.contentType}
	}
	return candidates, nil
}

// ExtractPDFPageCover re-extracts the qualifying image found on exactly
// page (1-based), for a manual override that pins a specific page rather
// than auto-detecting the first qualifying one. ok is false (not an
// error) if the page tree can't be resolved, page is out of range, or no
// qualifying image is found on that exact page.
func ExtractPDFPageCover(path string, page int) (data []byte, contentType string, ok bool, err error) {
	idx, pages, treeOK, err := loadPDFPages(path, page)
	if err != nil {
		return nil, "", false, err
	}
	if !treeOK || page < 1 || page > len(pages) {
		return nil, "", false, nil
	}
	images := findPDFPageImages(idx, []pdfPage{pages[page-1]}, true)
	if len(images) == 0 {
		return nil, "", false, nil
	}
	return images[0].bytes, images[0].contentType, true, nil
}

// loadPDFPages reads path, builds its object index, and walks its page
// tree up to limit pages -- the shared first half of both
// ListPDFCoverCandidates and ExtractPDFPageCover before they diverge on
// page selection.
func loadPDFPages(path string, limit int) (idx *pdfObjIndex, pages []pdfPage, ok bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false, err
	}
	idx = buildPDFObjIndex(data)
	pages, ok = walkPDFPageTree(idx, limit)
	return idx, pages, ok, nil
}
