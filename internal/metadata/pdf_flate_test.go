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
