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
// convention.
func renderPDFPageAsCover(data []byte, pageNum int) (imageBytes []byte, contentType string, ok bool) {
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
	var objNums []int
	if m := pdfContentsArrayRe.FindSubmatch(page.dict); m != nil {
		for _, ref := range pdfKidRefRe.FindAllSubmatch(m[1], -1) {
			if n, err := strconv.Atoi(string(ref[1])); err == nil {
				objNums = append(objNums, n)
			}
		}
	} else if m := pdfContentsRefRe.FindSubmatch(page.dict); m != nil {
		if n, err := strconv.Atoi(string(m[1])); err == nil {
			objNums = append(objNums, n)
		}
	}

	for _, n := range objNums {
		body, ok := idx.lookup(n)
		if !ok {
			continue
		}
		_, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream {
			continue
		}
		// Content streams aren't required to declare a /Filter -- when a
		// stream isn't (or doesn't parse as) zlib/Flate-compressed, fall
		// back to treating it as literal, already-plain content-operator
		// bytes rather than skipping it outright.
		content := stream
		if r, err := zlib.NewReader(bytes.NewReader(stream)); err == nil {
			if decompressed, err := io.ReadAll(r); err == nil {
				content = decompressed
			}
			r.Close()
		}
		if matchesTextShowOperator(content) {
			return true
		}
	}
	return false
}
