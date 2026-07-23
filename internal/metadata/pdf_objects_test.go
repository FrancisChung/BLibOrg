// internal/metadata/pdf_objects_test.go
package metadata

import (
	"bytes"
	"testing"
)

func TestBuildPDFObjIndex_LiteralLookup(t *testing.T) {
	data := []byte("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	idx := buildPDFObjIndex(data)

	body, ok := idx.lookup(1)
	if !ok {
		t.Fatal("lookup(1) not found")
	}
	if !bytes.Contains(body, []byte("/Type /Catalog")) {
		t.Errorf("lookup(1) = %q, want it to contain /Type /Catalog", body)
	}

	if _, ok := idx.lookup(99); ok {
		t.Error("lookup(99) found, want not found")
	}
}

func TestBuildPDFObjIndex_LastIncrementalUpdateWins(t *testing.T) {
	data := []byte("1 0 obj\n<< /Title (Old) >>\nendobj\n" +
		"1 0 obj\n<< /Title (New) >>\nendobj\n")
	idx := buildPDFObjIndex(data)

	body, ok := idx.lookup(1)
	if !ok {
		t.Fatal("lookup(1) not found")
	}
	if !bytes.Contains(body, []byte("(New)")) {
		t.Errorf("lookup(1) = %q, want it to contain the later revision's (New)", body)
	}
}

func TestSplitPDFObjectBody_WithStream(t *testing.T) {
	body := []byte(" << /Type /XObject /Length 5 >>\nstream\nhello\nendstream")
	dict, stream, hasStream := splitPDFObjectBody(body)
	if !hasStream {
		t.Fatal("hasStream = false, want true")
	}
	if !bytes.Contains(dict, []byte("/Type /XObject")) {
		t.Errorf("dict = %q, want it to contain /Type /XObject", dict)
	}
	if string(stream) != "hello" {
		t.Errorf("stream = %q, want %q", stream, "hello")
	}
}

func TestSplitPDFObjectBody_NoStream(t *testing.T) {
	body := []byte(" << /Type /Page /Parent 2 0 R >>")
	dict, _, hasStream := splitPDFObjectBody(body)
	if hasStream {
		t.Fatal("hasStream = true, want false")
	}
	if string(dict) != string(body) {
		t.Errorf("dict = %q, want whole body %q", dict, body)
	}
}

func TestPDFSubDictValue_NestedDict(t *testing.T) {
	// Reproduces a real library file that broke the old
	// `<<([^>]*?)>>` regex: /DecodeParms is a dictionary nested inside
	// the outer image dict, and the old regex stopped at the FIRST ">>"
	// (the inner one), never matching the outer dict at all.
	dict := []byte(`<</Type/XObject/Subtype/Image/Width 1410/Height 2000/ColorSpace/DeviceGray/BitsPerComponent 8/DecodeParms<</BitsPerComponent 8/Colors 1/Columns 1410/Predictor 2>>/Filter/FlateDecode/Length 5306>>`)
	value, ok := pdfSubDictValue(dict, "DecodeParms")
	if !ok {
		t.Fatal("pdfSubDictValue not found")
	}
	want := "/BitsPerComponent 8/Colors 1/Columns 1410/Predictor 2"
	if string(value) != want {
		t.Errorf("value = %q, want %q", value, want)
	}
}

func TestPDFSubDictValue_DoubleNested(t *testing.T) {
	// Tests that the depth-aware bracket-balancing logic correctly handles
	// a value that itself contains a nested dictionary. The value for
	// /DecodeParms is /A<</X 1>>/B 2, which contains a << >> pair.
	// A naive implementation that stops at the FIRST >> after /DecodeParms<<
	// would incorrectly return /A<< (missing the closing >> of the inner dict
	// and everything after). This test ensures depth correctly goes 1→2→1→0.
	dict := []byte(`<</Type/XObject/DecodeParms<</A<</X 1>>/B 2>>/Filter/FlateDecode>>`)
	value, ok := pdfSubDictValue(dict, "DecodeParms")
	if !ok {
		t.Fatal("pdfSubDictValue not found")
	}
	want := "/A<</X 1>>/B 2"
	if string(value) != want {
		t.Errorf("value = %q, want %q", value, want)
	}
}

func TestPDFSubDictValue_KeyAbsent(t *testing.T) {
	dict := []byte(`<</Type/XObject/Subtype/Image/Filter/DCTDecode>>`)
	if _, ok := pdfSubDictValue(dict, "DecodeParms"); ok {
		t.Error("pdfSubDictValue found a value, want not found")
	}
}
