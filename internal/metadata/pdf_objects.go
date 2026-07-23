// Package metadata's PDF support is a dependency-free, best-effort textual
// scanner, not a real PDF parser -- see pdf.go's package doc. This file
// adds a minimal indirect-object index on top of that same convention,
// used by the page-tree walker (pdf_pages.go) so cover selection can
// prefer true page order over incidental byte order in the file.
package metadata

import (
	"bytes"
	"compress/zlib"
	"io"
	"regexp"
	"sort"
	"strconv"
)

// pdfObjIndex resolves a PDF indirect object number to its raw body bytes
// (everything between the "obj" and "endobj" keywords).
type pdfObjIndex struct {
	literal        map[int][]byte
	literalOrder   map[int]int
	objStm         map[int][]byte
	objStmResolved bool
}

var pdfIndirectObjRe = regexp.MustCompile(`(?s)(\d+)\s+\d+\s+obj(.*?)endobj`)

// buildPDFObjIndex indexes every literal "N G obj...endobj" block in data.
// When the same object number appears more than once (a PDF edited via
// incremental update, appending a new revision rather than rewriting the
// file), the LAST occurrence wins -- consistent with findInfoDictBody's
// existing "most recent update wins" convention elsewhere in this package.
func buildPDFObjIndex(data []byte) *pdfObjIndex {
	idx := &pdfObjIndex{
		literal:      map[int][]byte{},
		literalOrder: map[int]int{},
	}
	matches := pdfIndirectObjRe.FindAllSubmatch(data, -1)
	for i, m := range matches {
		n, err := strconv.Atoi(string(m[1]))
		if err != nil {
			continue
		}
		idx.literal[n] = m[2]
		idx.literalOrder[n] = i
	}
	return idx
}

// lookup returns the body bytes for objNum, or ok=false if it isn't in the
// literal index or any ObjStm.
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

var pdfObjStmTypeRe = regexp.MustCompile(`/Type\s*/ObjStm\b`)
var pdfObjStmNRe = regexp.MustCompile(`/N\s+(\d+)`)
var pdfObjStmFirstRe = regexp.MustCompile(`/First\s+(\d+)`)

// resolveObjStms decompresses every /Type /ObjStm object in the literal
// index and indexes the objects compressed inside it, so lookup can
// resolve objects that never appear as literal text -- PDF producers
// increasingly compress non-stream objects like Page/Pages nodes into
// ObjStm for space savings (confirmed present in ~30% of a real 192-PDF
// library sample during this feature's design). Runs at most once per
// index. Processing happens in file order (by literalOrder) to ensure
// later ObjStm containers deterministically override earlier ones for
// any colliding compressed object numbers.
func (idx *pdfObjIndex) resolveObjStms() {
	if idx.objStmResolved {
		return
	}
	idx.objStmResolved = true
	idx.objStm = map[int][]byte{}

	// Collect all object numbers and sort by their file order.
	objNums := make([]int, 0, len(idx.literal))
	for n := range idx.literal {
		objNums = append(objNums, n)
	}
	sort.Slice(objNums, func(i, j int) bool {
		return idx.literalOrder[objNums[i]] < idx.literalOrder[objNums[j]]
	})

	// Process ObjStm containers in file order.
	for _, objNum := range objNums {
		body := idx.literal[objNum]
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
// by a fixed, predictable object number. Scanning is deterministic: literal
// objects are scanned in file order (by literalOrder), and ObjStm objects
// are scanned in numeric order.
func (idx *pdfObjIndex) find(typeRe *regexp.Regexp) (dict []byte, ok bool) {
	// Scan literals in file order.
	objNums := make([]int, 0, len(idx.literal))
	for n := range idx.literal {
		objNums = append(objNums, n)
	}
	sort.Slice(objNums, func(i, j int) bool {
		return idx.literalOrder[objNums[i]] < idx.literalOrder[objNums[j]]
	})
	for _, n := range objNums {
		body := idx.literal[n]
		d, _, _ := splitPDFObjectBody(body)
		if typeRe.Match(d) {
			return d, true
		}
	}

	// Scan ObjStm in numeric order.
	idx.resolveObjStms()
	objStmNums := make([]int, 0, len(idx.objStm))
	for n := range idx.objStm {
		objStmNums = append(objStmNums, n)
	}
	sort.Ints(objStmNums)
	for _, n := range objStmNums {
		body := idx.objStm[n]
		if typeRe.Match(body) {
			return body, true
		}
	}
	return nil, false
}
