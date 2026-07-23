package metadata

import (
	"bytes"
	"compress/zlib"
	"testing"
)

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

func TestUndoPDFPredictor_ZeroRowBytesNotOKNoHang(t *testing.T) {
	// A malformed /DecodeParms<</Columns 0>> (or a colorsOrDefault that
	// resolves to 0) makes rowBytes == columns*colors == 0. Before the fix,
	// dispatching this to undoTIFFPredictor's `for row := 0; row+rowBytes <=
	// len(out); row += rowBytes` loop never advances row, hanging forever.
	// undoPDFPredictor must reject rowBytes <= 0 before dispatching to
	// either predictor function.
	geo := pdfImageGeometry{width: 2, height: 1, bitsPerComponent: 8, predictor: 2, columns: 0}
	if _, ok := undoPDFPredictor([]byte{1, 2, 3, 4}, geo, 1); ok {
		t.Error("ok = true, want false (rowBytes == 0)")
	}
}

func TestUndoTIFFPredictor_TruncatedStreamNotOK(t *testing.T) {
	// rowBytes=3 (one RGB pixel/row), but data length 4 is not a multiple of
	// 3: a truncated/malformed stream. Should be reported as a decode
	// failure, mirroring undoPNGPredictor's len(data)%stride != 0 check,
	// rather than silently processing only the leading whole row(s) and
	// dropping the trailing partial bytes.
	data := []byte{10, 20, 30, 40}
	if _, ok := undoTIFFPredictor(data, 3, 3); ok {
		t.Error("ok = true, want false (len(data) not a multiple of rowBytes)")
	}
}

func TestUndoPDFPredictor_PNGAverageFilterUsesLeftAndUpAverage(t *testing.T) {
	// Two-pixel single-component rows (rowBytes=2), PNG filter type 3
	// (Average): dst[i] = src[i] + floor((a+b)/2), where a is the left
	// neighbor (0 if none) and b is the same-column byte in the previous
	// row (0 if none).
	//
	// Row1 (filter None): pixel0=10, pixel1=20 -> dst = [10, 20]
	// Row2 (filter Average):
	//   i=0: a=0 (no left), b=10 (row1 col0) -> avg=(0+10)/2=5; src=7 -> dst=12
	//   i=1: a=12 (row2 col0, just computed), b=20 (row1 col1) -> avg=(12+20)/2=16; src=9 -> dst=25
	geo := pdfImageGeometry{width: 2, height: 2, bitsPerComponent: 8, predictor: 10, columns: 2}
	data := []byte{
		0 /* None */, 10, 20,
		3 /* Average */, 7, 9,
	}
	out, ok := undoPDFPredictor(data, geo, 1)
	if !ok {
		t.Fatal("undoPDFPredictor not ok")
	}
	want := []byte{10, 20, 12, 25}
	if string(out) != string(want) {
		t.Errorf("out = %v, want %v", out, want)
	}
}

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
	data, contentType, ok := decodeFlatePDFImage(buildPDFObjIndex(nil), nil, dict, compressed.Bytes())
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
	if _, _, ok := decodeFlatePDFImage(buildPDFObjIndex(nil), nil, dict, []byte("irrelevant")); ok {
		t.Error("ok = true, want false (not a FlateDecode dict)")
	}
}

func TestDecodeFlatePDFImage_NoColorSpaceNotOK(t *testing.T) {
	dict := []byte(`<</Width 1/Height 1/Filter/FlateDecode>>`)
	if _, _, ok := decodeFlatePDFImage(buildPDFObjIndex(nil), nil, dict, []byte("irrelevant")); ok {
		t.Error("ok = true, want false (no /ColorSpace at all)")
	}
}

func TestUndoPDFPredictor_PNGPaethFilterNonDegenerate(t *testing.T) {
	// Two-pixel single-component rows (rowBytes=2), PNG filter type 4
	// (Paeth), chosen so a, b, c are non-zero and distinct and the
	// "closest predictor" choice is non-trivial.
	//
	// Row1 (filter None): pixel0=10, pixel1=40 -> dst = [10, 40]
	// Row2 (filter Paeth):
	//   i=0: a=0 (no left), b=10 (row1 col0), c=0 (no upper-left)
	//        p = a+b-c = 10; pa=|10-0|=10, pb=|10-10|=0, pc=|10-0|=10
	//        pb smallest -> predictor = b = 10; src=2 -> dst=12
	//   i=1: a=12 (row2 col0, just computed), b=40 (row1 col1), c=10 (row1 col0)
	//        p = a+b-c = 12+40-10 = 42
	//        pa=|42-12|=30, pb=|42-40|=2, pc=|42-10|=32
	//        pb smallest -> predictor = b = 40; src=3 -> dst=43
	geo := pdfImageGeometry{width: 2, height: 2, bitsPerComponent: 8, predictor: 10, columns: 2}
	data := []byte{
		0 /* None */, 10, 40,
		4 /* Paeth */, 2, 3,
	}
	out, ok := undoPDFPredictor(data, geo, 1)
	if !ok {
		t.Fatal("undoPDFPredictor not ok")
	}
	want := []byte{10, 40, 12, 43}
	if string(out) != string(want) {
		t.Errorf("out = %v, want %v", out, want)
	}
}
