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
