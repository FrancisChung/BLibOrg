// This file adds a fallback capability for PDF covers this package's
// otherwise dependency-free textual scanner structurally cannot provide:
// a page whose visual cover is a composite of an embedded raster image
// plus separately-drawn vector text/graphics (title, author, publisher
// logo) layered on top -- common in professionally-designed technical
// book covers (confirmed on a real O'Reilly title). No amount of
// image-XObject extraction can recover text that was never part of any
// raster image. renderPDFPageAsCover renders the whole page instead, via
// PDFium's WebAssembly build (github.com/klippa-app/go-pdfium) -- no CGo,
// no external binary the end user needs to install, the WASM blob is
// embedded in this Go binary by that library itself.
package metadata

import (
	"bytes"
	"compress/zlib"
	"image/png"
	"io"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// pdfRenderDPI is the resolution renderPDFPageAsCover renders at. 150 DPI
// on this feature's motivating real-world example (a 504x661.5pt page)
// produces a 1050x1379px PNG -- large enough to be a genuinely usable
// cover image, not so large that it's wasteful to cache.
const pdfRenderDPI = 150

var (
	pdfiumInstance pdfium.Pdfium
	pdfiumInitOnce sync.Once
	pdfiumInitErr  error
	// pdfiumMu serializes every call into the single shared PDFium WASM
	// instance above (webassembly.Init is configured with MaxTotal: 1 --
	// exactly one instance for the process's whole life, reused by every
	// render call). PDFium is not safe for concurrent use from multiple
	// goroutines against the same instance, so renderPDFPageAsCover holds
	// this for its entire body -- callers (metadata.Extract, and
	// therefore internal/librarian.Scan's parallel per-book workers) can
	// call it concurrently without knowing this constraint exists; only
	// the minority of PDFs that actually reach this render path
	// (composite covers, or an image filter this package's other
	// decoders can't handle) ever contend on this lock.
	pdfiumMu sync.Mutex
)

// getPdfiumInstance lazily starts exactly one PDFium WASM instance for the
// life of the process and reuses it for every render call -- WASM runtime
// startup has real cost, so this must not happen per book.
func getPdfiumInstance() (pdfium.Pdfium, error) {
	pdfiumInitOnce.Do(func() {
		pool, err := webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: 1, MaxTotal: 1})
		if err != nil {
			pdfiumInitErr = err
			return
		}
		pdfiumInstance, pdfiumInitErr = pool.GetInstance(30 * time.Second)
	})
	return pdfiumInstance, pdfiumInitErr
}

// renderPDFPageAsCover renders pageNum (1-based, matching this package's
// pdfPage.number convention) of the PDF in data to a full-page PNG image,
// compositing embedded images with any vector text/graphics drawn on top
// of them -- unlike this package's usual image-XObject extraction, which
// can only ever recover an embedded raster image on its own. ok is false
// (never an error) on any failure -- an unopenable/corrupt PDF, an
// out-of-range page, or a PDFium rendering failure -- matching this
// package's pervasive "one book's failure never fails the whole scan"
// convention. Safe to call concurrently from multiple goroutines: it
// holds pdfiumMu for its entire body, serializing access to the single
// shared PDFium instance internally, so callers never need their own
// synchronization around it.
func renderPDFPageAsCover(data []byte, pageNum int) (imageBytes []byte, contentType string, ok bool) {
	pdfiumMu.Lock()
	defer pdfiumMu.Unlock()

	instance, err := getPdfiumInstance()
	if err != nil {
		return nil, "", false
	}

	doc, err := instance.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return nil, "", false
	}
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})

	resp, err := instance.RenderPageInDPI(&requests.RenderPageInDPI{
		DPI: pdfRenderDPI,
		Page: requests.Page{
			ByIndex: &requests.PageByIndex{
				Document: doc.Document,
				Index:    pageNum - 1, // PDFium pages are 0-indexed.
			},
		},
	})
	if err != nil {
		return nil, "", false
	}
	defer resp.Cleanup()

	var buf bytes.Buffer
	if err := png.Encode(&buf, resp.Result.Image); err != nil {
		return nil, "", false
	}
	return buf.Bytes(), "image/png", true
}

var pdfContentsRefRe = regexp.MustCompile(`/Contents\s+(\d+)\s+\d+\s+R`)
var pdfContentsArrayRe = regexp.MustCompile(`/Contents\s*\[([^\]]*)\]`)
var pdfTextShowOperatorRe = regexp.MustCompile(`\b(Tj|TJ)\b`)

// matchesTextShowOperator reports whether stream contains a PDF text-show
// operator (Tj, the single-string form; or TJ, the array-with-kerning
// form) as a standalone token. Word-boundary matched rather than requiring
// a preceding space: real-world PDFs (InDesign's output, confirmed on
// this feature's motivating example) commonly write the array form as
// "]TJ" with no space between the closing bracket and the operator.
func matchesTextShowOperator(stream []byte) bool {
	return pdfTextShowOperatorRe.Match(stream)
}

// pageContentSuggestsCompositeCover reports whether page's content
// stream(s) contain a text-show operator, alongside whatever image is
// drawn there -- the signal that the page's true visual cover is a
// composite of that image plus separately-drawn text (a title, author
// name, or similar), which plain image-XObject extraction can never
// recover. A page's /Contents may be a single indirect reference or an
// array of them (multiple content-stream objects concatenated in order,
// as PDF producers commonly emit); every one of them is checked.
func pageContentSuggestsCompositeCover(idx *pdfObjIndex, page pdfPage) bool {
	for _, n := range pageContentObjNums(page.dict) {
		body, ok := idx.lookup(n)
		if !ok {
			continue
		}
		dict, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream {
			continue
		}
		if matchesTextShowOperator(decodePDFContentStream(dict, stream)) {
			return true
		}
	}

	// The page's own /Contents stream(s) may draw nothing but "/Fm0 Do"
	// -- a single top-level Form XObject wrapping everything (image and
	// text alike), the shape real prepress/print-registration output
	// commonly produces. In that case the loop above finds no text-show
	// operator at all, even though the page visually has one, because
	// it's nested inside the Form's own content stream instead.
	if resources, ok := resolveDictValue(idx, page.dict, "Resources"); ok {
		if xobjects, ok := resolveDictValue(idx, resources, "XObject"); ok {
			return formXObjectContainsTextShowOperator(idx, xobjects, 0, map[int]bool{})
		}
	}
	return false
}

// pageContentObjNums returns the object numbers of page's own /Contents
// stream(s) -- a single indirect reference, or an array of them (multiple
// content-stream objects concatenated in order, as PDF producers commonly
// emit). Shared by pageContentSuggestsCompositeCover and pageContentIsEmpty, both
// of which need every /Contents object a page declares, in order, before
// deciding whether to decode any of them.
func pageContentObjNums(dict []byte) []int {
	var objNums []int
	if m := pdfContentsArrayRe.FindSubmatch(dict); m != nil {
		for _, ref := range pdfKidRefRe.FindAllSubmatch(m[1], -1) {
			if n, err := strconv.Atoi(string(ref[1])); err == nil {
				objNums = append(objNums, n)
			}
		}
	} else if m := pdfContentsRefRe.FindSubmatch(dict); m != nil {
		if n, err := strconv.Atoi(string(m[1])); err == nil {
			objNums = append(objNums, n)
		}
	}
	return objNums
}

// pdfSoleFlateFilterRe matches a /Filter entry that is FlateDecode ALONE
// -- either the bare literal form ("/Filter/FlateDecode") or an array
// containing exactly that one name ("/Filter[/FlateDecode]") -- as
// opposed to a filter chain that also includes another filter (e.g.
// "/Filter[/ASCII85Decode/FlateDecode]", common in Distiller/InDesign
// output). decodePDFContentStream's plain zlib attempt can only correctly
// invert the former: pdfFlateDecodeRe (pdf_flate.go) deliberately matches
// FlateDecode anywhere in a filter array so image decoding doesn't miss
// it, but that means a chained filter's raw (still-ASCII85-armored) bytes
// fail zlib decompression the same way genuinely corrupt data does --
// decodePDFContentStream already treats that failure as "return nil,"
// which is the right call for its existing callers (a missed text-show
// operator is a minor degradation), but pageContentIsEmpty needs to tell
// the two apart: "confirmed zero bytes" from "couldn't decode this at
// all," since only the former is real evidence of a blank page.
var pdfSoleFlateFilterRe = regexp.MustCompile(`/Filter\s*/FlateDecode\b|/Filter\s*\[\s*/FlateDecode\s*\]`)

// pageContentIsEmpty reports whether page's own /Contents stream(s), once
// decoded, contain zero bytes total -- a genuinely blank page (some real
// books have an intentional blank leading page before their actual cover,
// e.g. for print-binding reasons), as opposed to a page that merely has
// no raster image but still draws real vector content (text, gradients,
// icons as vector paths). findPDFCoverPageAware (pdf.go) uses this to
// avoid treating a truly blank page 1 as if it were an all-vector cover:
// without this check, a blank leading page would be rendered and returned
// as "the cover" instead of falling through to the real cover on a later
// page. Returns true (blank) if page declares no /Contents at all.
//
// A stream whose filter isn't either "no filter" or FlateDecode alone
// (see pdfSoleFlateFilterRe) is treated as "assume non-empty" rather than
// blank: this package's best-effort decoder can't decode it with
// confidence, and getting that uncertainty wrong in the "assume
// non-empty" direction only costs one extra, harmless PDFium render
// attempt, while getting it wrong the other way would silently discard a
// real cover -- exactly the regression this function exists to prevent.
func pageContentIsEmpty(idx *pdfObjIndex, page pdfPage) bool {
	objNums := pageContentObjNums(page.dict)
	if len(objNums) == 0 {
		return true
	}
	for _, n := range objNums {
		body, ok := idx.lookup(n)
		if !ok {
			return false
		}
		dict, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream {
			return false
		}
		if pdfFlateDecodeRe.Match(dict) && !pdfSoleFlateFilterRe.Match(dict) {
			return false
		}
		if len(decodePDFContentStream(dict, stream)) > 0 {
			return false
		}
	}
	return true
}

// decodePDFContentStream returns stream's plain content-operator bytes,
// inflating it first if dict declares FlateDecode -- the shared
// decompression step pageContentSuggestsCompositeCover needs for both a
// page's own /Contents stream(s) and a Form XObject's own stream.
// Content streams aren't required to declare a /Filter -- only attempt
// zlib decompression when one is explicitly present (the same check
// decodeFlatePDFImage uses for image streams, pdf_flate.go), otherwise
// treat the stream as already-plain bytes. This avoids ever running the
// text-operator regex over corrupted/truncated compressed binary, which
// a blind try/fallback could do. Returns nil if inflation fails, so the
// caller's regex check against the still-compressed bytes safely reports
// no match instead of scanning raw compressed binary.
func decodePDFContentStream(dict, stream []byte) []byte {
	if !pdfFlateDecodeRe.Match(dict) {
		return stream
	}
	r, err := zlib.NewReader(bytes.NewReader(stream))
	if err != nil {
		return nil
	}
	decompressed, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		return nil
	}
	return decompressed
}

// formXObjectContainsTextShowOperator recurses into xobjects (a page's or
// nested Form's own /XObject dict) looking for a /Subtype /Form entry
// whose own content stream contains a text-show operator -- the
// composite-cover signal pageContentSuggestsCompositeCover's own
// /Contents check can't see when a PDF producer wraps a whole page's
// content inside a single top-level Form XObject invoked via "/Fm0 Do".
// Mirrors findImagesInXObjects' traversal (pdf_images.go): same
// maxFormXObjectDepth cap and cycle-guarding visited set, but checking
// for a text-show operator instead of collecting images.
func formXObjectContainsTextShowOperator(idx *pdfObjIndex, xobjects []byte, depth int, visited map[int]bool) bool {
	if depth >= maxFormXObjectDepth {
		return false
	}
	for _, ref := range pdfXObjectEntryRe.FindAllSubmatch(xobjects, -1) {
		objNum, err := strconv.Atoi(string(ref[1]))
		if err != nil || visited[objNum] {
			continue
		}
		body, ok := idx.lookup(objNum)
		if !ok {
			continue
		}
		dict, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream || !pdfSubtypeFormRe.Match(dict) {
			continue
		}
		visited[objNum] = true
		if matchesTextShowOperator(decodePDFContentStream(dict, stream)) {
			return true
		}
		if formResources, ok := resolveDictValue(idx, dict, "Resources"); ok {
			if formXObjects, ok := resolveDictValue(idx, formResources, "XObject"); ok {
				if formXObjectContainsTextShowOperator(idx, formXObjects, depth+1, visited) {
					return true
				}
			}
		}
	}
	return false
}
