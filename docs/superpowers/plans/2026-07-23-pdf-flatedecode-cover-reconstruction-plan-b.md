# PDF FlateDecode Cover Reconstruction (Plan B) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `internal/metadata`'s page-aware PDF cover selection (Plan A) to reconstruct `FlateDecode` raster images -- undoing TIFF/PNG predictors and mapping DeviceGray/DeviceRGB/DeviceCMYK/Indexed/ICCBased colorspaces to RGB -- and re-encode them as PNG, so the ~30% of library PDFs whose first page image is a raw raster (not a pre-encoded JPEG) get a real cover instead of none.

**Architecture:** Two new files in `internal/metadata` sit alongside the existing Plan A parser: `pdf_flate.go` (image-XObject geometry parsing + predictor reconstruction + top-level orchestration) and `pdf_flate_colorspace.go` (colorspace resolution/parsing + sample-to-RGBA mapping + PNG encoding). `decodePDFImageStream` (pdf_images.go, Plan A) gains a FlateDecode fallback path after its existing DCTDecode check, so callers (`findPDFPageImages`, and transitively `findPDFCoverPageAware`) get FlateDecode support for free with no change to their own logic.

**Tech Stack:** Go stdlib only (`compress/zlib`, `image`, `image/color`, `image/png`, `regexp`, `bytes`, `strconv`) -- no new dependencies, consistent with this package's existing convention.

## Global Constraints

- No new external/cgo dependencies -- stdlib only (`image/png` for encoding, no decoder needed).
- Never regress existing behavior: `TestExtractPDF_FindsCoverImage`, `TestExtractPDF_NoCoverLeavesFieldEmpty`, and every Plan A test must keep passing unmodified in assertions (only `TestDecodePDFImageStream_SkipsNonDCTDecode`'s fixture/rationale changes, per Task 3 -- FlateDecode is no longer categorically unsupported).
- `JPXDecode` remains explicitly out of scope (see the design doc's Non-goals) -- this plan touches `FlateDecode` only.
- Scope is deliberately capped to what's needed for real-world cover images, matching this package's "dependency-free, best-effort" convention:
  - `BitsPerComponent` other than 8 is out of scope (`ok=false`, falls back to the next candidate/legacy scan) -- 1/2/4/16-bit raster covers are rare in practice and add real complexity (bit-packing) for negligible additional coverage.
  - `DecodeParms` must be an inline `<<...>>` dictionary (via `pdfSubDictValue`, already built in Plan A) -- an indirect `/DecodeParms N G R` reference is not resolved, matching Plan A's own choice to keep `Resources`/`XObject` resolution the only indirect-dict case this package chases.
  - Predictor `Colors` defaults to the resolved colorspace's component count (not the PDF spec's generic default of 1), and `Columns` defaults to the image's `Width` (not the spec's generic default of 1) -- both are intentional deviations from the spec's *generic*-stream defaults, justified because this parsing is always for an image XObject specifically, where those are the only sane values.
- Every new regex that matches an indirect object reference must allow any generation number (`\s+\d+\s+R` / `\s+\d+\s+obj`), matching Plan A's convention.

---

## Task 1: Image geometry parsing + predictor reconstruction

**Files:**
- Create: `internal/metadata/pdf_flate.go`
- Test: `internal/metadata/pdf_flate_test.go`

**Interfaces:**
- Consumes: `pdfSubDictValue` (pdf_objects.go, Plan A).
- Produces: `type pdfImageGeometry struct { width, height, bitsPerComponent, predictor, colors, columns int }`, `func parsePDFImageGeometry(dict []byte) (pdfImageGeometry, bool)`, `func (geo pdfImageGeometry) colorsOrDefault(csComponents int) int`, `func undoPDFPredictor(data []byte, geo pdfImageGeometry, csComponents int) ([]byte, bool)`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/metadata/pdf_flate_test.go
package metadata

import "testing"

func TestParsePDFImageGeometry_DefaultsWhenNoDecodeParms(t *testing.T) {
	dict := []byte(`<</Type/XObject/Subtype/Image/Width 10/Height 20/Filter/FlateDecode>>`)
	geo, ok := parsePDFImageGeometry(dict)
	if !ok {
		t.Fatal("parsePDFImageGeometry not ok")
	}
	if geo.width != 10 || geo.height != 20 {
		t.Errorf("width/height = %d/%d, want 10/20", geo.width, geo.height)
	}
	if geo.bitsPerComponent != 8 {
		t.Errorf("bitsPerComponent = %d, want 8 (default)", geo.bitsPerComponent)
	}
	if geo.predictor != 1 {
		t.Errorf("predictor = %d, want 1 (default: no prediction)", geo.predictor)
	}
	if geo.columns != 10 {
		t.Errorf("columns = %d, want 10 (defaults to width)", geo.columns)
	}
}

func TestParsePDFImageGeometry_ReadsDecodeParms(t *testing.T) {
	dict := []byte(`<</Type/XObject/Subtype/Image/Width 4/Height 4/BitsPerComponent 8` +
		`/DecodeParms<</Predictor 15/Colors 3/Columns 4>>/Filter/FlateDecode>>`)
	geo, ok := parsePDFImageGeometry(dict)
	if !ok {
		t.Fatal("parsePDFImageGeometry not ok")
	}
	if geo.predictor != 15 || geo.colors != 3 || geo.columns != 4 {
		t.Errorf("predictor/colors/columns = %d/%d/%d, want 15/3/4", geo.predictor, geo.colors, geo.columns)
	}
}

func TestParsePDFImageGeometry_MissingWidthOrHeightNotOK(t *testing.T) {
	if _, ok := parsePDFImageGeometry([]byte(`<</Height 20>>`)); ok {
		t.Error("ok = true, want false (no /Width)")
	}
	if _, ok := parsePDFImageGeometry([]byte(`<</Width 20>>`)); ok {
		t.Error("ok = true, want false (no /Height)")
	}
}

func TestColorsOrDefault_UsesExplicitColorsOverColorSpaceComponents(t *testing.T) {
	geo := pdfImageGeometry{colors: 3}
	if got := geo.colorsOrDefault(1); got != 3 {
		t.Errorf("colorsOrDefault = %d, want 3 (explicit /Colors wins)", got)
	}
}

func TestColorsOrDefault_FallsBackToColorSpaceComponents(t *testing.T) {
	geo := pdfImageGeometry{colors: 0}
	if got := geo.colorsOrDefault(4); got != 4 {
		t.Errorf("colorsOrDefault = %d, want 4 (falls back to colorspace component count)", got)
	}
}

func TestUndoPDFPredictor_NoPredictorPassesThrough(t *testing.T) {
	geo := pdfImageGeometry{width: 2, height: 1, bitsPerComponent: 8, predictor: 1, columns: 2}
	data := []byte{10, 20, 30, 40}
	out, ok := undoPDFPredictor(data, geo, 2)
	if !ok {
		t.Fatal("undoPDFPredictor not ok")
	}
	if string(out) != string(data) {
		t.Errorf("out = %v, want unchanged %v", out, data)
	}
}

func TestUndoPDFPredictor_TIFFPredictorUndoesHorizontalDiff(t *testing.T) {
	// Two RGB pixels/row (rowBytes=6), predictor 2 (TIFF): each component is
	// stored as the difference from the same component in the pixel to its
	// left, so undoing it means running cumulative sum per component across
	// the row, restarting at the row's first pixel.
	geo := pdfImageGeometry{width: 2, height: 1, bitsPerComponent: 8, predictor: 2, columns: 2}
	// Row: pixel0 = (10,20,30) stored as-is (no left neighbor); pixel1 stored
	// as pixel1-pixel0 = (5,5,5), so real pixel1 = (15,25,35).
	data := []byte{10, 20, 30, 5, 5, 5}
	out, ok := undoPDFPredictor(data, geo, 3)
	if !ok {
		t.Fatal("undoPDFPredictor not ok")
	}
	want := []byte{10, 20, 30, 15, 25, 35}
	if string(out) != string(want) {
		t.Errorf("out = %v, want %v", out, want)
	}
}

func TestUndoPDFPredictor_PNGSubFilterUndoesLeftNeighborDiff(t *testing.T) {
	// One row, 2 grayscale pixels (rowBytes=2), PNG filter type 1 (Sub)
	// prefixed to the row: byte 0 is the filter type, then the row data.
	geo := pdfImageGeometry{width: 2, height: 1, bitsPerComponent: 8, predictor: 10, columns: 2}
	data := []byte{1 /* Sub */, 10, 5} // pixel0=10, pixel1 = 10+5 = 15
	out, ok := undoPDFPredictor(data, geo, 1)
	if !ok {
		t.Fatal("undoPDFPredictor not ok")
	}
	want := []byte{10, 15}
	if string(out) != string(want) {
		t.Errorf("out = %v, want %v", out, want)
	}
}

func TestUndoPDFPredictor_PNGUpFilterUsesPreviousRow(t *testing.T) {
	geo := pdfImageGeometry{width: 2, height: 2, bitsPerComponent: 8, predictor: 10, columns: 2}
	data := []byte{
		0 /* None */, 10, 20,
		2 /* Up */, 5, 5, // row2 = row1 + (5,5) = (15,25)
	}
	out, ok := undoPDFPredictor(data, geo, 1)
	if !ok {
		t.Fatal("undoPDFPredictor not ok")
	}
	want := []byte{10, 20, 15, 25}
	if string(out) != string(want) {
		t.Errorf("out = %v, want %v", out, want)
	}
}

func TestUndoPDFPredictor_PNGPaethFilter(t *testing.T) {
	// Single-row, single-component so Paeth degenerates to: a=left(0 since
	// no left neighbor), b=up(0, first row), c=upper-left(0) -> predictor=0,
	// so the raw byte passes through unchanged. Confirms the plumbing (filter
	// type 4 dispatches to paethPredictor) without needing a multi-row fixture.
	geo := pdfImageGeometry{width: 1, height: 1, bitsPerComponent: 8, predictor: 15, columns: 1}
	data := []byte{4 /* Paeth */, 42}
	out, ok := undoPDFPredictor(data, geo, 1)
	if !ok {
		t.Fatal("undoPDFPredictor not ok")
	}
	if string(out) != string([]byte{42}) {
		t.Errorf("out = %v, want [42]", out)
	}
}

func TestUndoPDFPredictor_NonByteAlignedBitsPerComponentNotOK(t *testing.T) {
	geo := pdfImageGeometry{width: 2, height: 1, bitsPerComponent: 4, predictor: 1, columns: 2}
	if _, ok := undoPDFPredictor([]byte{0xFF}, geo, 1); ok {
		t.Error("ok = true, want false (4-bit depth out of scope)")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run 'TestParsePDFImageGeometry|TestColorsOrDefault|TestUndoPDFPredictor' -v`
Expected: FAIL with `undefined: parsePDFImageGeometry` (compile error).

- [ ] **Step 3: Implement `pdf_flate.go`**

```go
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

	switch {
	case geo.predictor <= 1:
		return data, true
	case geo.predictor == 2:
		return undoTIFFPredictor(data, rowBytes, colors), true
	case geo.predictor >= 10:
		return undoPNGPredictor(data, rowBytes, colors)
	default:
		return nil, false
	}
}

// undoTIFFPredictor reverses predictor 2: each component was stored as
// the difference from the same component in the pixel immediately to its
// left (restarting at each row's first pixel), so undoing it is a
// per-component running sum across each row.
func undoTIFFPredictor(data []byte, rowBytes, colors int) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	for row := 0; row+rowBytes <= len(out); row += rowBytes {
		r := out[row : row+rowBytes]
		for i := colors; i < len(r); i++ {
			r[i] += r[i-colors]
		}
	}
	return out
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run 'TestParsePDFImageGeometry|TestColorsOrDefault|TestUndoPDFPredictor' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_flate.go internal/metadata/pdf_flate_test.go
git commit -m "Add PDF image geometry parsing and predictor reconstruction"
```

---

## Task 2: Device colorspace mapping + RGBA/PNG assembly

**Files:**
- Create: `internal/metadata/pdf_flate_colorspace.go`
- Test: `internal/metadata/pdf_flate_colorspace_test.go`

**Interfaces:**
- Consumes: nothing new from Task 1.
- Produces: `type pdfColorSpace struct { components int; toRGBA func([]byte) color.RGBA }`, package vars `pdfDeviceGrayCS`, `pdfDeviceRGBCS`, `pdfDeviceCMYKCS`, `pdfDeviceColorSpaceByName map[string]pdfColorSpace`, `func buildRGBAFromSamples(samples []byte, geo pdfImageGeometry, cs pdfColorSpace) ([]byte, bool)`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/metadata/pdf_flate_colorspace_test.go
package metadata

import (
	"bytes"
	"image/png"
	"testing"
)

func decodePNGPixel(t *testing.T, pngBytes []byte, x, y int) (r, g, b, a uint32) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	return img.At(x, y).RGBA()
}

func TestBuildRGBAFromSamples_DeviceGray(t *testing.T) {
	geo := pdfImageGeometry{width: 2, height: 1}
	samples := []byte{0x00, 0xFF} // black, white
	pngBytes, ok := buildRGBAFromSamples(samples, geo, pdfDeviceGrayCS)
	if !ok {
		t.Fatal("buildRGBAFromSamples not ok")
	}
	r, g, b, _ := decodePNGPixel(t, pngBytes, 1, 0)
	if r>>8 != 0xFF || g>>8 != 0xFF || b>>8 != 0xFF {
		t.Errorf("pixel(1,0) = (%d,%d,%d), want white", r>>8, g>>8, b>>8)
	}
}

func TestBuildRGBAFromSamples_DeviceRGB(t *testing.T) {
	geo := pdfImageGeometry{width: 1, height: 1}
	samples := []byte{0x10, 0x20, 0x30}
	pngBytes, ok := buildRGBAFromSamples(samples, geo, pdfDeviceRGBCS)
	if !ok {
		t.Fatal("buildRGBAFromSamples not ok")
	}
	r, g, b, _ := decodePNGPixel(t, pngBytes, 0, 0)
	if r>>8 != 0x10 || g>>8 != 0x20 || b>>8 != 0x30 {
		t.Errorf("pixel = (%d,%d,%d), want (16,32,48)", r>>8, g>>8, b>>8)
	}
}

func TestBuildRGBAFromSamples_DeviceCMYKAllZeroIsWhite(t *testing.T) {
	geo := pdfImageGeometry{width: 1, height: 1}
	samples := []byte{0, 0, 0, 0} // no ink at all
	pngBytes, ok := buildRGBAFromSamples(samples, geo, pdfDeviceCMYKCS)
	if !ok {
		t.Fatal("buildRGBAFromSamples not ok")
	}
	r, g, b, _ := decodePNGPixel(t, pngBytes, 0, 0)
	if r>>8 != 0xFF || g>>8 != 0xFF || b>>8 != 0xFF {
		t.Errorf("pixel = (%d,%d,%d), want white (0,0,0,0 CMYK)", r>>8, g>>8, b>>8)
	}
}

func TestBuildRGBAFromSamples_DeviceCMYKFullKIsBlack(t *testing.T) {
	geo := pdfImageGeometry{width: 1, height: 1}
	samples := []byte{0, 0, 0, 255} // full black ink
	pngBytes, ok := buildRGBAFromSamples(samples, geo, pdfDeviceCMYKCS)
	if !ok {
		t.Fatal("buildRGBAFromSamples not ok")
	}
	r, g, b, _ := decodePNGPixel(t, pngBytes, 0, 0)
	if r>>8 != 0 || g>>8 != 0 || b>>8 != 0 {
		t.Errorf("pixel = (%d,%d,%d), want black", r>>8, g>>8, b>>8)
	}
}

func TestBuildRGBAFromSamples_TooFewSamplesNotOK(t *testing.T) {
	geo := pdfImageGeometry{width: 2, height: 2}
	if _, ok := buildRGBAFromSamples([]byte{1, 2, 3}, geo, pdfDeviceGrayCS); ok {
		t.Error("ok = true, want false (fewer samples than width*height*components)")
	}
}

func TestPDFDeviceColorSpaceByName_ResolvesStandardNames(t *testing.T) {
	for _, name := range []string{"DeviceGray", "DeviceRGB", "DeviceCMYK"} {
		if _, ok := pdfDeviceColorSpaceByName[name]; !ok {
			t.Errorf("pdfDeviceColorSpaceByName[%q] not found", name)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run 'TestBuildRGBAFromSamples|TestPDFDeviceColorSpaceByName' -v`
Expected: FAIL with `undefined: pdfDeviceGrayCS` (compile error).

- [ ] **Step 3: Implement `pdf_flate_colorspace.go`**

```go
// This file resolves a PDF image XObject's colorspace and maps its
// predictor-reconstructed sample bytes to RGB, so pdf_flate.go's
// FlateDecode reconstruction can hand the frontend webview a real PNG.
// Indirect/named colorspace resolution (via Resources/ColorSpace) and
// ICCBased/Indexed support are added in later tasks; this task covers
// only the three Device* colorspaces, which alone account for the large
// majority of library covers per the design doc's sampling.
package metadata

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// pdfColorSpace describes how to map one pixel's raw sample bytes
// (already predictor-reconstructed, 8 bits per component) to an RGBA
// color. components is the number of sample bytes per pixel this
// colorspace consumes.
type pdfColorSpace struct {
	components int
	toRGBA     func(samples []byte) color.RGBA
}

var pdfDeviceGrayCS = pdfColorSpace{components: 1, toRGBA: func(s []byte) color.RGBA {
	return color.RGBA{R: s[0], G: s[0], B: s[0], A: 0xFF}
}}

var pdfDeviceRGBCS = pdfColorSpace{components: 3, toRGBA: func(s []byte) color.RGBA {
	return color.RGBA{R: s[0], G: s[1], B: s[2], A: 0xFF}
}}

// pdfDeviceCMYKCS converts via the standard naive CMYK->RGB formula
// (R=255*(1-C)*(1-K), etc.) -- not color-managed, but correct for the
// common case of a cover image with no embedded ICC profile.
var pdfDeviceCMYKCS = pdfColorSpace{components: 4, toRGBA: func(s []byte) color.RGBA {
	c, m, y, k := float64(s[0])/255, float64(s[1])/255, float64(s[2])/255, float64(s[3])/255
	r := 255 * (1 - c) * (1 - k)
	g := 255 * (1 - m) * (1 - k)
	b := 255 * (1 - y) * (1 - k)
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xFF}
}}

// pdfDeviceColorSpaceByName maps a PDF colorspace name to its pdfColorSpace.
// CalGray/CalRGB (calibrated variants) are treated identically to their
// Device counterparts -- this package doesn't attempt real color
// calibration, matching its "pragmatic approximation" convention.
var pdfDeviceColorSpaceByName = map[string]pdfColorSpace{
	"DeviceGray": pdfDeviceGrayCS,
	"DeviceRGB":  pdfDeviceRGBCS,
	"DeviceCMYK": pdfDeviceCMYKCS,
	"CalGray":    pdfDeviceGrayCS,
	"CalRGB":     pdfDeviceRGBCS,
}

// buildRGBAFromSamples maps predictor-reconstructed sample bytes
// (row-major, geo.width*geo.height pixels, cs.components bytes per pixel)
// into an image.RGBA and PNG-encodes it. ok is false if samples is
// shorter than the image's dimensions require (a truncated/malformed
// stream).
func buildRGBAFromSamples(samples []byte, geo pdfImageGeometry, cs pdfColorSpace) ([]byte, bool) {
	rowBytes := geo.width * cs.components
	if rowBytes <= 0 || len(samples) < rowBytes*geo.height {
		return nil, false
	}
	img := image.NewRGBA(image.Rect(0, 0, geo.width, geo.height))
	for y := 0; y < geo.height; y++ {
		row := samples[y*rowBytes : (y+1)*rowBytes]
		for x := 0; x < geo.width; x++ {
			px := row[x*cs.components : (x+1)*cs.components]
			img.SetRGBA(x, y, cs.toRGBA(px))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run 'TestBuildRGBAFromSamples|TestPDFDeviceColorSpaceByName' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_flate_colorspace.go internal/metadata/pdf_flate_colorspace_test.go
git commit -m "Add PDF device colorspace mapping and RGBA/PNG assembly"
```

---

## Task 3: Top-level FlateDecode decode + wire into decodePDFImageStream

**Files:**
- Modify: `internal/metadata/pdf_flate.go`
- Modify: `internal/metadata/pdf_images.go`
- Modify: `internal/metadata/pdf_images_test.go`
- Test: `internal/metadata/pdf_flate_test.go`

**Interfaces:**
- Consumes: `parsePDFImageGeometry`, `undoPDFPredictor` (Task 1), `pdfDeviceColorSpaceByName`, `buildRGBAFromSamples` (Task 2).
- Produces: `func decodeFlatePDFImage(dict, stream []byte) (data []byte, contentType string, ok bool)`. Modifies `decodePDFImageStream`'s body (signature unchanged: still `func decodePDFImageStream(dict, stream []byte) (data []byte, contentType string, ok bool)`).

This task only resolves a colorspace written as a bare name directly in the image dict (e.g. `/ColorSpace /DeviceRGB`); Task 4 adds resolution through `Resources/ColorSpace` and indirect references.

- [ ] **Step 1: Write the failing tests**

```go
// Append to internal/metadata/pdf_flate_test.go

func TestDecodeFlatePDFImage_DeviceRGBNoPredictor(t *testing.T) {
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write([]byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60}); err != nil { // 2x1 RGB
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	dict := []byte(`<</Type/XObject/Subtype/Image/Width 2/Height 1/ColorSpace/DeviceRGB/Filter/FlateDecode>>`)
	data, contentType, ok := decodeFlatePDFImage(dict, compressed.Bytes())
	if !ok {
		t.Fatal("decodeFlatePDFImage not ok")
	}
	if contentType != "image/png" {
		t.Errorf("contentType = %q, want image/png", contentType)
	}
	r, g, b, _ := decodePNGPixel(t, data, 0, 0)
	if r>>8 != 0x10 || g>>8 != 0x20 || b>>8 != 0x30 {
		t.Errorf("pixel(0,0) = (%d,%d,%d), want (16,32,48)", r>>8, g>>8, b>>8)
	}
}

func TestDecodeFlatePDFImage_NotFlateDecodeNotOK(t *testing.T) {
	dict := []byte(`<</Filter/DCTDecode>>`)
	if _, _, ok := decodeFlatePDFImage(dict, []byte("irrelevant")); ok {
		t.Error("ok = true, want false (not a FlateDecode dict)")
	}
}

func TestDecodeFlatePDFImage_NoColorSpaceNotOK(t *testing.T) {
	dict := []byte(`<</Width 1/Height 1/Filter/FlateDecode>>`)
	if _, _, ok := decodeFlatePDFImage(dict, []byte("irrelevant")); ok {
		t.Error("ok = true, want false (no bare colorspace name to resolve yet -- Task 4 adds indirect/named resolution)")
	}
}
```

```go
// Replace internal/metadata/pdf_images_test.go's
// TestDecodePDFImageStream_SkipsNonDCTDecode with:

func TestDecodePDFImageStream_UnresolvableFlateDecodeSkipped(t *testing.T) {
	// No /Width or /Height at all, so geometry parsing fails regardless of
	// filter -- confirms decodePDFImageStream's FlateDecode fallback still
	// degrades to ok=false for a dict it genuinely can't reconstruct,
	// same as any other malformed/unsupported image.
	dict := []byte(`<</Type/XObject/Subtype/Image/Filter/FlateDecode>>`)
	if _, _, ok := decodePDFImageStream(dict, []byte("rawbytes")); ok {
		t.Error("decodePDFImageStream ok = true, want false (no geometry to reconstruct)")
	}
}

func TestDecodePDFImageStream_DecodesFlateDecodeDeviceRGB(t *testing.T) {
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write([]byte{0x01, 0x02, 0x03}); err != nil { // 1x1 RGB
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	dict := []byte(`<</Type/XObject/Subtype/Image/Width 1/Height 1/ColorSpace/DeviceRGB/Filter/FlateDecode>>`)

	data, contentType, ok := decodePDFImageStream(dict, compressed.Bytes())
	if !ok {
		t.Fatal("decodePDFImageStream not ok")
	}
	if contentType != "image/png" {
		t.Errorf("contentType = %q, want image/png", contentType)
	}
	if len(data) == 0 {
		t.Error("data is empty")
	}
}
```

Add `"bytes"` and `"compress/zlib"` to `pdf_images_test.go`'s imports, and the same two (`"bytes"`, `"compress/zlib"`) to `pdf_flate_test.go`'s imports (currently just `"testing"`) for the `TestDecodeFlatePDFImage_DeviceRGBNoPredictor` fixture appended above.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run 'TestDecodeFlatePDFImage|TestDecodePDFImageStream' -v`
Expected: FAIL with `undefined: decodeFlatePDFImage` (compile error).

- [ ] **Step 3: Implement**

Append to `internal/metadata/pdf_flate.go`:

```go
var pdfFlateDecodeRe = regexp.MustCompile(`/Filter\s*/FlateDecode|/Filter\s*\[[^\]]*/FlateDecode`)
var pdfColorSpaceNameRe = regexp.MustCompile(`/ColorSpace\s*/(\w+)`)

// decodeFlatePDFImage reconstructs a FlateDecode image XObject into a
// display-ready PNG. This task resolves only a bare device colorspace
// name written directly as the image dict's /ColorSpace value (e.g.
// "/ColorSpace /DeviceRGB"); Task 4 extends colorspace resolution to
// indirect references and Resources/ColorSpace-scoped names.
func decodeFlatePDFImage(dict, stream []byte) (data []byte, contentType string, ok bool) {
	if !pdfFlateDecodeRe.Match(dict) {
		return nil, "", false
	}
	geo, ok := parsePDFImageGeometry(dict)
	if !ok {
		return nil, "", false
	}
	nameMatch := pdfColorSpaceNameRe.FindSubmatch(dict)
	if nameMatch == nil {
		return nil, "", false
	}
	cs, ok := pdfDeviceColorSpaceByName[string(nameMatch[1])]
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
```

Add `"bytes"`, `"compress/zlib"`, and `"io"` to `pdf_flate.go`'s imports.

Modify `decodePDFImageStream` in `internal/metadata/pdf_images.go` (replacing its current body, which only handled DCTDecode):

```go
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run 'TestDecodeFlatePDFImage|TestDecodePDFImageStream' -v`
Expected: PASS

Then run the full package to confirm no regressions: `go test ./internal/metadata/...`
Expected: PASS (all tests, including Plan A's)

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_flate.go internal/metadata/pdf_flate_test.go internal/metadata/pdf_images.go internal/metadata/pdf_images_test.go
git commit -m "Reconstruct FlateDecode images with a bare device colorspace name"
```

---

## Task 4: Indirect/named colorspace resolution via Resources/ColorSpace

**Files:**
- Modify: `internal/metadata/pdf_flate.go`
- Modify: `internal/metadata/pdf_flate_colorspace.go`
- Modify: `internal/metadata/pdf_images.go`
- Test: `internal/metadata/pdf_flate_colorspace_test.go`

**Interfaces:**
- Consumes: `resolveDictValue`, `pdfObjIndex`, `splitPDFObjectBody` (pdf_objects.go, Plan A).
- Produces: `func resolvePDFColorSpaceValue(idx *pdfObjIndex, resources, dict []byte) (value []byte, ok bool)`, `func parsePDFColorSpace(idx *pdfObjIndex, raw []byte) (pdfColorSpace, bool)`. Changes `decodeFlatePDFImage` and `decodePDFImageStream` signatures to `(idx *pdfObjIndex, resources, dict, stream []byte) (data []byte, contentType string, ok bool)`.

Real-world PDFs commonly write an image's `/ColorSpace` as a resource name (e.g. `/CS0`) that must be looked up in the page's `Resources/ColorSpace` subdictionary, or as an indirect reference to an array (e.g. `[/ICCBased 8 0 R]`) -- this task resolves both, leaving the actual array *interpretation* (ICCBased, Indexed) to Tasks 5-6, which extend `parsePDFColorSpace` further.

- [ ] **Step 1: Write the failing tests**

```go
// Append to internal/metadata/pdf_flate_colorspace_test.go

func TestResolvePDFColorSpaceValue_BareDeviceNameNeedsNoResources(t *testing.T) {
	dict := []byte(`<</ColorSpace/DeviceRGB>>`)
	idx := buildPDFObjIndex(nil)
	value, ok := resolvePDFColorSpaceValue(idx, nil, dict)
	if !ok {
		t.Fatal("resolvePDFColorSpaceValue not ok")
	}
	if string(value) != "/DeviceRGB" {
		t.Errorf("value = %q, want /DeviceRGB", value)
	}
}

func TestResolvePDFColorSpaceValue_NamedResourceLooksUpInResourcesColorSpace(t *testing.T) {
	data := []byte("5 0 obj\n<< /ICCBased 6 0 R >>\nendobj\n")
	idx := buildPDFObjIndex(data)
	resources := []byte(`<</ColorSpace<</CS0 5 0 R>>>>`)
	dict := []byte(`<</ColorSpace/CS0>>`)

	value, ok := resolvePDFColorSpaceValue(idx, resources, dict)
	if !ok {
		t.Fatal("resolvePDFColorSpaceValue not ok")
	}
	if !bytes.Contains(value, []byte("/ICCBased")) {
		t.Errorf("value = %q, want it to resolve through to the ICCBased dict", value)
	}
}

func TestResolvePDFColorSpaceValue_InlineArrayReturnedAsIs(t *testing.T) {
	dict := []byte(`<</ColorSpace[/Indexed/DeviceRGB 255 6 0 R]>>`)
	idx := buildPDFObjIndex(nil)
	value, ok := resolvePDFColorSpaceValue(idx, nil, dict)
	if !ok {
		t.Fatal("resolvePDFColorSpaceValue not ok")
	}
	if !bytes.Contains(value, []byte("/Indexed")) {
		t.Errorf("value = %q, want the inline array as-is", value)
	}
}

func TestResolvePDFColorSpaceValue_MissingColorSpaceNotOK(t *testing.T) {
	idx := buildPDFObjIndex(nil)
	if _, ok := resolvePDFColorSpaceValue(idx, nil, []byte(`<</Width 1>>`)); ok {
		t.Error("ok = true, want false (no /ColorSpace at all)")
	}
}

func TestParsePDFColorSpace_BareDeviceName(t *testing.T) {
	idx := buildPDFObjIndex(nil)
	cs, ok := parsePDFColorSpace(idx, []byte("/DeviceGray"))
	if !ok || cs.components != 1 {
		t.Errorf("parsePDFColorSpace(/DeviceGray) = %+v, %v, want components=1, ok=true", cs, ok)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run 'TestResolvePDFColorSpaceValue|TestParsePDFColorSpace' -v`
Expected: FAIL with `undefined: resolvePDFColorSpaceValue` (compile error).

- [ ] **Step 3: Implement**

Append to `internal/metadata/pdf_flate_colorspace.go`:

```go
var pdfColorSpaceValueRe = regexp.MustCompile(`/ColorSpace\s*(/\w+|\[[^\]]*\]|\d+\s+\d+\s+R)`)
var pdfIndirectRefRe = regexp.MustCompile(`^(\d+)\s+\d+\s+R$`)

// resolvePDFColorSpaceValue returns the raw bytes describing an image's
// colorspace: a bare device name or an inline array is returned as-is; a
// name that isn't a recognized device colorspace (e.g. "/CS0") is looked
// up in resources' /ColorSpace subdictionary; an indirect reference ("N G
// R"), whether written directly or found via that lookup, is resolved
// through idx. ok is false if /ColorSpace is absent from dict or nothing
// above resolves it.
func resolvePDFColorSpaceValue(idx *pdfObjIndex, resources, dict []byte) (value []byte, ok bool) {
	m := pdfColorSpaceValueRe.FindSubmatch(dict)
	if m == nil {
		return nil, false
	}
	raw := m[1]

	if raw[0] == '/' {
		name := string(raw[1:])
		if _, isDevice := pdfDeviceColorSpaceByName[name]; isDevice {
			return raw, true
		}
		if resources == nil {
			return nil, false
		}
		csDict, ok := resolveDictValue(idx, resources, "ColorSpace")
		if !ok {
			return nil, false
		}
		entryRe := regexp.MustCompile(`/` + regexp.QuoteMeta(name) + `\s+(\[[^\]]*\]|\d+\s+\d+\s+R|/\w+)`)
		entry := entryRe.FindSubmatch(csDict)
		if entry == nil {
			return nil, false
		}
		raw = entry[1]
		if raw[0] == '/' {
			// A named resource pointing at another bare name (rare); resolve
			// once more against the device map only -- chasing further
			// indirection here is out of scope.
			if _, isDevice := pdfDeviceColorSpaceByName[string(raw[1:])]; isDevice {
				return raw, true
			}
			return nil, false
		}
	}

	if refMatch := pdfIndirectRefRe.FindSubmatch(raw); refMatch != nil {
		objNum, err := strconv.Atoi(string(refMatch[1]))
		if err != nil {
			return nil, false
		}
		body, ok := idx.lookup(objNum)
		if !ok {
			return nil, false
		}
		bodyDict, _, _ := splitPDFObjectBody(body)
		return bodyDict, true
	}
	return raw, true
}

// parsePDFColorSpace interprets colorspace bytes already resolved by
// resolvePDFColorSpaceValue: a bare device name maps directly via
// pdfDeviceColorSpaceByName. ICCBased and Indexed array forms are added
// in Tasks 5 and 6.
func parsePDFColorSpace(idx *pdfObjIndex, raw []byte) (pdfColorSpace, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return pdfColorSpace{}, false
	}
	if trimmed[0] == '/' {
		cs, ok := pdfDeviceColorSpaceByName[string(trimmed[1:])]
		return cs, ok
	}
	return pdfColorSpace{}, false
}
```

Add `"regexp"` and `"strconv"` to `pdf_flate_colorspace.go`'s imports.

Replace `decodeFlatePDFImage` in `internal/metadata/pdf_flate.go` with the idx/resources-aware version:

```go
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
```

Remove the now-unused `pdfColorSpaceNameRe` var and `pdfDeviceColorSpaceByName` lookup from `pdf_flate.go` (the map itself stays in `pdf_flate_colorspace.go`; only the direct-lookup call site in `decodeFlatePDFImage` is superseded).

Update `decodePDFImageStream` in `internal/metadata/pdf_images.go`:

```go
func decodePDFImageStream(idx *pdfObjIndex, resources, dict, stream []byte) (data []byte, contentType string, ok bool) {
	if pdfDCTDecodeRe.Match(dict) {
		trimmed := bytes.TrimSuffix(stream, []byte("endstream"))
		trimmed = bytes.TrimRight(trimmed, "\r\n")
		if len(trimmed) == 0 {
			return nil, "", false
		}
		return trimmed, "image/jpeg", true
	}
	return decodeFlatePDFImage(idx, resources, dict, stream)
}
```

Update its one call site in `findPDFPageImages` (same file):

```go
data, contentType, ok := decodePDFImageStream(idx, resources, imgDict, imgStream)
```

(`resources` is already in scope there as the page's resolved `Resources` dict.)

Update `pdf_images_test.go`'s and `pdf_flate_test.go`'s Task 3 tests to pass `idx` and `resources`: every `decodePDFImageStream(dict, stream)` call becomes `decodePDFImageStream(buildPDFObjIndex(nil), nil, dict, stream)`, and every `decodeFlatePDFImage(dict, stream)` call becomes `decodeFlatePDFImage(buildPDFObjIndex(nil), nil, dict, stream)`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/...`
Expected: PASS (all tests)

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_flate.go internal/metadata/pdf_flate_colorspace.go internal/metadata/pdf_flate_colorspace_test.go internal/metadata/pdf_images.go internal/metadata/pdf_images_test.go internal/metadata/pdf_flate_test.go
git commit -m "Resolve indirect and Resources-scoped named PDF colorspaces"
```

---

## Task 5: ICCBased colorspace approximation

**Files:**
- Modify: `internal/metadata/pdf_flate_colorspace.go`
- Test: `internal/metadata/pdf_flate_colorspace_test.go`

**Interfaces:**
- Consumes: `idx.lookup`, `splitPDFObjectBody` (Plan A); extends `parsePDFColorSpace` (Task 4).
- Produces: no new exported names; `parsePDFColorSpace` gains ICCBased handling.

- [ ] **Step 1: Write the failing tests**

```go
// Append to internal/metadata/pdf_flate_colorspace_test.go

func TestParsePDFColorSpace_ICCBasedApproximatesByComponentCount(t *testing.T) {
	tests := []struct {
		n              string
		wantComponents int
	}{
		{"1", 1}, // Gray
		{"3", 3}, // RGB
		{"4", 4}, // CMYK
	}
	for _, tt := range tests {
		data := []byte("6 0 obj\n<< /N " + tt.n + " /Length 0 >>\nstream\n\nendstream\nendobj\n")
		idx := buildPDFObjIndex(data)
		raw := []byte("[/ICCBased 6 0 R]")

		cs, ok := parsePDFColorSpace(idx, raw)
		if !ok {
			t.Fatalf("N=%s: parsePDFColorSpace not ok", tt.n)
		}
		if cs.components != tt.wantComponents {
			t.Errorf("N=%s: components = %d, want %d", tt.n, cs.components, tt.wantComponents)
		}
	}
}

func TestParsePDFColorSpace_ICCBasedUnsupportedComponentCountNotOK(t *testing.T) {
	data := []byte("6 0 obj\n<< /N 2 /Length 0 >>\nstream\n\nendstream\nendobj\n")
	idx := buildPDFObjIndex(data)
	if _, ok := parsePDFColorSpace(idx, []byte("[/ICCBased 6 0 R]")); ok {
		t.Error("ok = true, want false (N=2 doesn't map to Gray/RGB/CMYK)")
	}
}

func TestParsePDFColorSpace_ICCBasedUnresolvableRefNotOK(t *testing.T) {
	idx := buildPDFObjIndex(nil)
	if _, ok := parsePDFColorSpace(idx, []byte("[/ICCBased 99 0 R]")); ok {
		t.Error("ok = true, want false (object 99 doesn't exist)")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run TestParsePDFColorSpace_ICCBased -v`
Expected: FAIL (all cases report `ok = true` incorrectly, or rather `parsePDFColorSpace` returns `ok=false` for everything since ICCBased isn't handled yet -- confirm the first two sub-tests fail with "not ok").

- [ ] **Step 3: Implement**

Replace `parsePDFColorSpace` in `internal/metadata/pdf_flate_colorspace.go`:

```go
var pdfICCBasedRe = regexp.MustCompile(`(?s)\[\s*/ICCBased\s+(\d+)\s+\d+\s+R`)
var pdfStreamNRe = regexp.MustCompile(`/N\s+(\d+)`)

// parsePDFColorSpace interprets colorspace bytes already resolved by
// resolvePDFColorSpaceValue: a bare device name maps directly; an
// ICCBased array is approximated by its stream dict's /N (component
// count) -- 1/3/4 -> Gray/RGB/CMYK, a pragmatic approximation rather than
// real ICC profile support (matching this package's convention, see the
// design doc). Indexed array support is added in Task 6.
func parsePDFColorSpace(idx *pdfObjIndex, raw []byte) (pdfColorSpace, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return pdfColorSpace{}, false
	}
	if trimmed[0] == '/' {
		cs, ok := pdfDeviceColorSpaceByName[string(trimmed[1:])]
		return cs, ok
	}
	if m := pdfICCBasedRe.FindSubmatch(trimmed); m != nil {
		objNum, err := strconv.Atoi(string(m[1]))
		if err != nil {
			return pdfColorSpace{}, false
		}
		body, ok := idx.lookup(objNum)
		if !ok {
			return pdfColorSpace{}, false
		}
		streamDict, _, _ := splitPDFObjectBody(body)
		nMatch := pdfStreamNRe.FindSubmatch(streamDict)
		if nMatch == nil {
			return pdfColorSpace{}, false
		}
		n, err := strconv.Atoi(string(nMatch[1]))
		if err != nil {
			return pdfColorSpace{}, false
		}
		switch n {
		case 1:
			return pdfDeviceGrayCS, true
		case 3:
			return pdfDeviceRGBCS, true
		case 4:
			return pdfDeviceCMYKCS, true
		default:
			return pdfColorSpace{}, false
		}
	}
	return pdfColorSpace{}, false
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run 'TestParsePDFColorSpace' -v`
Expected: PASS

Then run the full package: `go test ./internal/metadata/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_flate_colorspace.go internal/metadata/pdf_flate_colorspace_test.go
git commit -m "Approximate ICCBased colorspaces by component count"
```

---

## Task 6: Indexed colorspace (palette + base colorspace)

**Files:**
- Modify: `internal/metadata/pdf_flate_colorspace.go`
- Test: `internal/metadata/pdf_flate_colorspace_test.go`

**Interfaces:**
- Consumes: `parsePDFColorSpace` (Task 4/5, recursive call for the base colorspace), `idx.lookup`, `splitPDFObjectBody`, `unescapePDFBytes` (pdf.go, Plan A).
- Produces: no new exported names; `parsePDFColorSpace` gains Indexed handling via a new unexported `parsePDFIndexedColorSpace`.

- [ ] **Step 1: Write the failing tests**

```go
// Append to internal/metadata/pdf_flate_colorspace_test.go

func TestParsePDFColorSpace_IndexedWithLiteralStringPalette(t *testing.T) {
	// 2-entry palette, base DeviceRGB: index 0 -> (255,0,0), index 1 -> (0,255,0).
	raw := []byte("[/Indexed/DeviceRGB 1(\xFF\x00\x00\x00\xFF\x00)]")
	idx := buildPDFObjIndex(nil)

	cs, ok := parsePDFColorSpace(idx, raw)
	if !ok {
		t.Fatal("parsePDFColorSpace not ok")
	}
	if cs.components != 1 {
		t.Errorf("components = %d, want 1 (Indexed images sample one palette index per pixel)", cs.components)
	}
	rgba := cs.toRGBA([]byte{1})
	if rgba.R != 0 || rgba.G != 0xFF || rgba.B != 0 {
		t.Errorf("toRGBA([1]) = %+v, want green (index 1)", rgba)
	}
}

func TestParsePDFColorSpace_IndexedWithStreamPalette(t *testing.T) {
	data := []byte("7 0 obj\n<< /Length 3 >>\nstream\n\x00\x00\xFF\nendstream\nendobj\n")
	idx := buildPDFObjIndex(data)
	raw := []byte("[/Indexed/DeviceRGB 0 7 0 R]")

	cs, ok := parsePDFColorSpace(idx, raw)
	if !ok {
		t.Fatal("parsePDFColorSpace not ok")
	}
	rgba := cs.toRGBA([]byte{0})
	if rgba.R != 0 || rgba.G != 0 || rgba.B != 0xFF {
		t.Errorf("toRGBA([0]) = %+v, want blue", rgba)
	}
}

func TestParsePDFColorSpace_IndexedOutOfRangeIndexIsBlack(t *testing.T) {
	raw := []byte("[/Indexed/DeviceRGB 0(\xFF\x00\x00)]")
	idx := buildPDFObjIndex(nil)
	cs, ok := parsePDFColorSpace(idx, raw)
	if !ok {
		t.Fatal("parsePDFColorSpace not ok")
	}
	rgba := cs.toRGBA([]byte{5}) // hival is 0, so only index 0 is valid
	if rgba != (colorRGBABlack()) {
		t.Errorf("toRGBA([5]) = %+v, want black (out-of-range palette index)", rgba)
	}
}
```

Add this small test helper alongside the others in `pdf_flate_colorspace_test.go` (avoids importing `image/color` into the test file just for one literal):

```go
func colorRGBABlack() color.RGBA { return color.RGBA{A: 0xFF} }
```

Add `"image/color"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metadata/... -run TestParsePDFColorSpace_Indexed -v`
Expected: FAIL with `ok = false` (Indexed not yet recognized) or a compile error if `color` isn't imported correctly.

- [ ] **Step 3: Implement**

Add to `internal/metadata/pdf_flate_colorspace.go`, and extend `parsePDFColorSpace`'s dispatch:

```go
// parsePDFColorSpace's Indexed branch, inserted after the ICCBased check
// and before the final `return pdfColorSpace{}, false`:
	if bytes.Contains(trimmed, []byte("/Indexed")) {
		return parsePDFIndexedColorSpace(idx, trimmed)
	}
```

```go
var pdfIndexedBaseRe = regexp.MustCompile(`(?s)^\s*(/\w+|\[[^\]]*\])`)
var pdfIndexedHivalRe = regexp.MustCompile(`(?s)^(\d+)\s*(.*)$`)
var pdfIndexedStreamRefRe = regexp.MustCompile(`^(\d+)\s+\d+\s+R`)

// parsePDFIndexedColorSpace parses "[/Indexed base hival lookup]": base is
// another colorspace value (recursively parsed via parsePDFColorSpace),
// hival is the highest valid palette index, and lookup is either a
// literal PDF string "(...)" of raw palette bytes or a reference to a
// stream object containing them (both forms are real-world-common; a
// hex string "<...>" lookup table is not handled -- out of scope). The
// returned pdfColorSpace's toRGBA looks up base.components bytes at
// sample[0]*base.components within the palette, returning black for an
// out-of-range index rather than erroring, matching this package's
// best-effort convention.
func parsePDFIndexedColorSpace(idx *pdfObjIndex, arr []byte) (pdfColorSpace, bool) {
	inner := bytes.TrimSuffix(bytes.TrimPrefix(bytes.TrimSpace(arr), []byte("[")), []byte("]"))
	inner = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(inner), []byte("/Indexed")))

	baseMatch := pdfIndexedBaseRe.FindSubmatch(inner)
	if baseMatch == nil {
		return pdfColorSpace{}, false
	}
	base, ok := parsePDFColorSpace(idx, baseMatch[1])
	if !ok {
		return pdfColorSpace{}, false
	}
	rest := bytes.TrimSpace(inner[len(baseMatch[0]):])

	hivalMatch := pdfIndexedHivalRe.FindSubmatch(rest)
	if hivalMatch == nil {
		return pdfColorSpace{}, false
	}
	rest = bytes.TrimSpace(hivalMatch[2])

	var palette []byte
	switch {
	case len(rest) > 0 && rest[0] == '(':
		end := bytes.LastIndexByte(rest, ')')
		if end < 0 {
			return pdfColorSpace{}, false
		}
		palette = unescapePDFBytes(string(rest[1:end]))
	case pdfIndexedStreamRefRe.Match(rest):
		refMatch := pdfIndexedStreamRefRe.FindSubmatch(rest)
		objNum, err := strconv.Atoi(string(refMatch[1]))
		if err != nil {
			return pdfColorSpace{}, false
		}
		body, ok := idx.lookup(objNum)
		if !ok {
			return pdfColorSpace{}, false
		}
		_, stream, hasStream := splitPDFObjectBody(body)
		if !hasStream {
			return pdfColorSpace{}, false
		}
		palette = stream
	default:
		return pdfColorSpace{}, false
	}

	return pdfColorSpace{
		components: 1,
		toRGBA: func(s []byte) color.RGBA {
			off := int(s[0]) * base.components
			if off+base.components > len(palette) {
				return color.RGBA{A: 0xFF}
			}
			return base.toRGBA(palette[off : off+base.components])
		},
	}, true
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/metadata/... -run 'TestParsePDFColorSpace' -v`
Expected: PASS

Then run the full package: `go test ./internal/metadata/...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/metadata/pdf_flate_colorspace.go internal/metadata/pdf_flate_colorspace_test.go
git commit -m "Add Indexed colorspace support (palette + base colorspace)"
```

---

## Task 7: Manual verification against the real library

- [ ] **Step 1: Build and run the desktop app**

Run: `cd desktop && wails build` (or `wails dev` for a faster inner loop), then launch the built app pointed at the existing `config.yaml` for `/media/francis/Data1/Books/Library`.

- [ ] **Step 2: Spot-check the previously-FlateDecode-only books**

Open the Library view and find `Residues - Time, Uncertainty, and Change in Software Architecture` and `Atomic Kotlin` (both called out in Plan A's own verification task as FlateDecode-covered, out of scope until this plan). Confirm both now show a real cover image instead of the placeholder tile.

- [ ] **Step 3: Spot-check a broader sample**

Browse several more shelves and confirm no cover regressed (a book that had a cover before this plan should still have one, and it should look the same for DCTDecode books) and that previously-blank tiles across a range of categories now show plausible-looking cover art (not corrupted/garbled images -- a strong signal of a colorspace or predictor bug).

- [ ] **Step 4: Report results**

Note how many of the previously-cover-less books now show a cover, and flag any book whose cover looks visibly wrong (inverted colors, garbled/striped image) for follow-up -- likely an edge case in predictor or colorspace handling worth a targeted fixture/fix.
