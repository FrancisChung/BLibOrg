// Package metadata's PDF support is a dependency-free, best-effort textual
// scanner, not a real PDF parser -- see pdf.go's package doc. This file
// adds a minimal indirect-object index on top of that same convention,
// used by the page-tree walker (pdf_pages.go) so cover selection can
// prefer true page order over incidental byte order in the file.
package metadata

import (
	"bytes"
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
