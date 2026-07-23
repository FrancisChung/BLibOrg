// Package metadata's FlateDecode support reconstructs raw raster image
// XObjects: undoing whatever predictor (TIFF or PNG-style) was applied
// before the raster data was deflated, then (pdf_flate_colorspace.go)
// mapping the resulting samples through the image's colorspace to RGB and
// re-encoding as PNG. Unlike DCTDecode (already a complete JPEG file),
// FlateDecode raster data has no container of its own, so this package
// must assemble one -- see the design doc at
// docs/superpowers/specs/2026-07-23-pdf-page-aware-cover-extraction-design.md
// for the real-world filter/colorspace distribution that scoped this.
package metadata

import (
	"bytes"
	"compress/zlib"
	"io"
	"regexp"
	"strconv"
)

var pdfWidthRe = regexp.MustCompile(`/Width\s+(\d+)`)
var pdfHeightRe = regexp.MustCompile(`/Height\s+(\d+)`)
var pdfBitsPerComponentRe = regexp.MustCompile(`/BitsPerComponent\s+(\d+)`)
var pdfPredictorRe = regexp.MustCompile(`/Predictor\s+(\d+)`)
var pdfColorsRe = regexp.MustCompile(`/Colors\s+(\d+)`)
var pdfColumnsRe = regexp.MustCompile(`/Columns\s+(\d+)`)

// pdfImageGeometry is the subset of an image XObject dict's numeric
// parameters needed to reconstruct FlateDecode raster data: dimensions,
// sample depth, and (if a /DecodeParms sub-dictionary is present) the
// predictor applied before the data was deflated.
type pdfImageGeometry struct {
	width, height, bitsPerComponent int
	predictor, colors, columns      int
}

// parsePDFImageGeometry reads /Width, /Height, /BitsPerComponent (default
// 8), and, from an inline /DecodeParms dict if present, /Predictor
// (default 1: no prediction), /Colors (0 here means "unset" -- see
// colorsOrDefault), and /Columns (defaults to /Width, the only sane value
// for an image XObject specifically -- see Global Constraints). ok is
// false if /Width or /Height is missing or non-numeric.
func parsePDFImageGeometry(dict []byte) (pdfImageGeometry, bool) {
	w := pdfWidthRe.FindSubmatch(dict)
	h := pdfHeightRe.FindSubmatch(dict)
	if w == nil || h == nil {
		return pdfImageGeometry{}, false
	}
	width, err1 := strconv.Atoi(string(w[1]))
	height, err2 := strconv.Atoi(string(h[1]))
	if err1 != nil || err2 != nil || width <= 0 || height <= 0 {
		return pdfImageGeometry{}, false
	}

	geo := pdfImageGeometry{width: width, height: height, bitsPerComponent: 8, predictor: 1, columns: width}
	if m := pdfBitsPerComponentRe.FindSubmatch(dict); m != nil {
		if bpc, err := strconv.Atoi(string(m[1])); err == nil {
			geo.bitsPerComponent = bpc
		}
	}
	if parms, ok := pdfSubDictValue(dict, "DecodeParms"); ok {
		if m := pdfPredictorRe.FindSubmatch(parms); m != nil {
			if p, err := strconv.Atoi(string(m[1])); err == nil {
				geo.predictor = p
			}
		}
		if m := pdfColorsRe.FindSubmatch(parms); m != nil {
			if c, err := strconv.Atoi(string(m[1])); err == nil {
				geo.colors = c
			}
		}
		if m := pdfColumnsRe.FindSubmatch(parms); m != nil {
			if c, err := strconv.Atoi(string(m[1])); err == nil {
				geo.columns = c
			}
		}
	}
	return geo, true
}

// colorsOrDefault returns the predictor's per-pixel component count: the
// explicit /Colors value if /DecodeParms set one, otherwise the resolved
// colorspace's own component count (csComponents) -- a deliberate
// deviation from the PDF spec's generic stream default of 1, justified
// because this geometry is always for an image XObject, whose real
// component count is already known by the time predictor undo runs.
func (geo pdfImageGeometry) colorsOrDefault(csComponents int) int {
	if geo.colors > 0 {
		return geo.colors
	}
	return csComponents
}

// undoPDFPredictor reverses the TIFF (predictor 2) or PNG (predictor >=
// 10) filter applied to inflated FlateDecode raster data before it was
// compressed, per PDF spec Table 8. ok is false for a bit depth other
// than 8 (out of scope, see Global Constraints) or an unrecognized
// predictor value.
func undoPDFPredictor(data []byte, geo pdfImageGeometry, csComponents int) ([]byte, bool) {
	if geo.bitsPerComponent != 8 {
		return nil, false
	}
	colors := geo.colorsOrDefault(csComponents)
	rowBytes := geo.columns * colors

	if geo.predictor > 1 && rowBytes <= 0 {
		// A malformed /DecodeParms (e.g. /Columns 0) or an unresolved
		// colorsOrDefault can make rowBytes <= 0. Reject here, before
		// dispatching to either predictor function: undoTIFFPredictor's
		// row-advancing loop would never terminate with rowBytes == 0,
		// and a zero/negative stride is meaningless for PNG row framing
		// too.
		return nil, false
	}

	switch {
	case geo.predictor <= 1:
		return data, true
	case geo.predictor == 2:
		return undoTIFFPredictor(data, rowBytes, colors)
	case geo.predictor >= 10:
		return undoPNGPredictor(data, rowBytes, colors)
	default:
		return nil, false
	}
}

// undoTIFFPredictor reverses predictor 2: each component was stored as
// the difference from the same component in the pixel immediately to its
// left (restarting at each row's first pixel), so undoing it is a
// per-component running sum across each row. ok is false if rowBytes is
// non-positive (which would make the row-advancing loop never terminate)
// or if len(data) isn't a whole multiple of rowBytes (a truncated or
// otherwise malformed stream), mirroring undoPNGPredictor's own length
// validation.
func undoTIFFPredictor(data []byte, rowBytes, colors int) ([]byte, bool) {
	if rowBytes <= 0 || len(data)%rowBytes != 0 {
		return nil, false
	}
	out := make([]byte, len(data))
	copy(out, data)
	for row := 0; row+rowBytes <= len(out); row += rowBytes {
		r := out[row : row+rowBytes]
		for i := colors; i < len(r); i++ {
			r[i] += r[i-colors]
		}
	}
	return out, true
}

// undoPNGPredictor reverses PNG-style per-row filtering (predictor >=
// 10): each row of the inflated stream is prefixed with a one-byte filter
// type (0 None, 1 Sub, 2 Up, 3 Average, 4 Paeth) -- PDF's Predictor 10-15
// values borrow this scheme directly from the PNG spec, and the filter
// type is read live per row (it can vary row to row even under one
// Predictor value) rather than assumed constant. bpp is the per-pixel
// byte distance (== colors, since bit depth is always 8 here) filters use
// to find each row's "left" and "upper-left" reference bytes.
func undoPNGPredictor(data []byte, rowBytes, bpp int) ([]byte, bool) {
	stride := rowBytes + 1
	if stride <= 1 || bpp <= 0 || len(data)%stride != 0 {
		return nil, false
	}
	rows := len(data) / stride
	out := make([]byte, rows*rowBytes)
	prev := make([]byte, rowBytes)
	for row := 0; row < rows; row++ {
		filterType := data[row*stride]
		src := data[row*stride+1 : row*stride+stride]
		dst := out[row*rowBytes : (row+1)*rowBytes]
		for i := 0; i < rowBytes; i++ {
			var a, b, c byte
			if i >= bpp {
				a = dst[i-bpp]
				c = prev[i-bpp]
			}
			b = prev[i]
			switch filterType {
			case 0:
				dst[i] = src[i]
			case 1:
				dst[i] = src[i] + a
			case 2:
				dst[i] = src[i] + b
			case 3:
				dst[i] = src[i] + byte((int(a)+int(b))/2)
			case 4:
				dst[i] = src[i] + paethPredictor(a, b, c)
			default:
				return nil, false
			}
		}
		copy(prev, dst)
	}
	return out, true
}

// paethPredictor is the PNG spec's Paeth filter predictor function: picks
// whichever of a (left), b (up), c (upper-left) is numerically closest to
// a+b-c.
func paethPredictor(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa, pb, pc := absInt(p-int(a)), absInt(p-int(b)), absInt(p-int(c))
	if pa <= pb && pa <= pc {
		return a
	}
	if pb <= pc {
		return b
	}
	return c
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

var pdfFlateDecodeRe = regexp.MustCompile(`/Filter\s*/FlateDecode|/Filter\s*\[[^\]]*/FlateDecode`)

// decodeFlatePDFImage reconstructs a FlateDecode image XObject into a
// display-ready PNG: resolves its colorspace (resolvePDFColorSpaceValue +
// parsePDFColorSpace, pdf_flate_colorspace.go, which may need idx to
// chase an indirect reference and resources to look up a named one),
// inflates the stream, undoes whatever predictor was applied, then maps
// samples to RGB and PNG-encodes.
func decodeFlatePDFImage(idx *pdfObjIndex, resources, dict, stream []byte) (data []byte, contentType string, ok bool) {
	if !pdfFlateDecodeRe.Match(dict) {
		return nil, "", false
	}
	geo, ok := parsePDFImageGeometry(dict)
	if !ok {
		return nil, "", false
	}
	csValue, ok := resolvePDFColorSpaceValue(idx, resources, dict)
	if !ok {
		return nil, "", false
	}
	cs, ok := parsePDFColorSpace(idx, csValue)
	if !ok {
		return nil, "", false
	}

	r, err := zlib.NewReader(bytes.NewReader(stream))
	if err != nil {
		return nil, "", false
	}
	inflated, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		return nil, "", false
	}

	samples, ok := undoPDFPredictor(inflated, geo, cs.components)
	if !ok {
		return nil, "", false
	}
	pngBytes, ok := buildRGBAFromSamples(samples, geo, cs)
	if !ok {
		return nil, "", false
	}
	return pngBytes, "image/png", true
}
