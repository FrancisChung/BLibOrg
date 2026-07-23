// internal/metadata/pdf_objects_test.go
package metadata

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"regexp"
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

func TestPDFObjIndex_ResolvesObjectInsideObjStm(t *testing.T) {
	// Builds an ObjStm containing two compressed objects, per the PDF
	// spec's layout: header is N whitespace-separated "objNum offset"
	// pairs, then object bodies concatenated starting at byte offset
	// /First. Offsets are computed from the actual fixture strings rather
	// than hardcoded, so the test can't silently rot if either object's
	// text changes length.
	obj5 := "<</Type/Page/Parent 1 0 R>>"
	obj6 := "<</Foo/Bar>>"
	header := fmt.Sprintf("5 0 6 %d", len(obj5))
	content := header + obj5 + obj6
	first := len(header)

	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	var data bytes.Buffer
	fmt.Fprintf(&data, "9 0 obj\n<< /Type /ObjStm /N 2 /First %d /Filter /FlateDecode /Length %d >>\nstream\n", first, compressed.Len())
	data.Write(compressed.Bytes())
	data.WriteString("\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data.Bytes())

	body5, ok := idx.lookup(5)
	if !ok {
		t.Fatal("lookup(5) not found")
	}
	if !bytes.Contains(body5, []byte("/Type/Page")) {
		t.Errorf("lookup(5) = %q, want it to contain /Type/Page", body5)
	}

	body6, ok := idx.lookup(6)
	if !ok {
		t.Fatal("lookup(6) not found")
	}
	if string(body6) != obj6 {
		t.Errorf("lookup(6) = %q, want %q", body6, obj6)
	}
}

func TestPDFObjIndex_LaterObjStmOverridesEarlier(t *testing.T) {
	// Tests that when two separate ObjStm containers at different object
	// numbers both contain a compressed object with the same target number
	// but different content, the LATER ObjStm (by file position) wins.
	// This verifies deterministic file-order processing, not random map
	// iteration order.

	// First ObjStm (object 9) compresses object 5 as V1.
	obj5_v1 := "<</V 1>>"
	header1 := fmt.Sprintf("5 0")
	content1 := header1 + obj5_v1
	first1 := len(header1)

	var compressed1 bytes.Buffer
	w1 := zlib.NewWriter(&compressed1)
	if _, err := w1.Write([]byte(content1)); err != nil {
		t.Fatalf("zlib write (ObjStm 9): %v", err)
	}
	if err := w1.Close(); err != nil {
		t.Fatalf("zlib close (ObjStm 9): %v", err)
	}

	// Second ObjStm (object 20) compresses object 5 as V2 -- should override.
	obj5_v2 := "<</V 2>>"
	header2 := fmt.Sprintf("5 0")
	content2 := header2 + obj5_v2
	first2 := len(header2)

	var compressed2 bytes.Buffer
	w2 := zlib.NewWriter(&compressed2)
	if _, err := w2.Write([]byte(content2)); err != nil {
		t.Fatalf("zlib write (ObjStm 20): %v", err)
	}
	if err := w2.Close(); err != nil {
		t.Fatalf("zlib close (ObjStm 20): %v", err)
	}

	var data bytes.Buffer
	fmt.Fprintf(&data, "9 0 obj\n<< /Type /ObjStm /N 1 /First %d /Filter /FlateDecode /Length %d >>\nstream\n", first1, compressed1.Len())
	data.Write(compressed1.Bytes())
	data.WriteString("\nendstream\nendobj\n")

	fmt.Fprintf(&data, "20 0 obj\n<< /Type /ObjStm /N 1 /First %d /Filter /FlateDecode /Length %d >>\nstream\n", first2, compressed2.Len())
	data.Write(compressed2.Bytes())
	data.WriteString("\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data.Bytes())

	body5, ok := idx.lookup(5)
	if !ok {
		t.Fatal("lookup(5) not found")
	}
	if !bytes.Contains(body5, []byte("/V 2")) {
		t.Errorf("lookup(5) = %q, want it to contain /V 2 (from later ObjStm 20)", body5)
	}
	if bytes.Contains(body5, []byte("/V 1")) {
		t.Errorf("lookup(5) = %q, want it to NOT contain /V 1 (from earlier ObjStm 9)", body5)
	}
}

func TestResolveDictValue_InlineDict(t *testing.T) {
	dict := []byte(`<</Type/Page/Resources<</Font<</F1 3 0 R>>>>>>`)
	value, ok := resolveDictValue(nil, dict, "Resources")
	if !ok {
		t.Fatal("resolveDictValue not found")
	}
	if string(value) != "/Font<</F1 3 0 R>>" {
		t.Errorf("value = %q, want %q", value, "/Font<</F1 3 0 R>>")
	}
}

func TestResolveDictValue_IndirectRef(t *testing.T) {
	data := []byte("1 0 obj\n<< /Type /Page /Resources 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Font << /F1 3 0 R >> >>\nendobj\n")
	idx := buildPDFObjIndex(data)
	pageBody, _ := idx.lookup(1)
	dict, _, _ := splitPDFObjectBody(pageBody)

	value, ok := resolveDictValue(idx, dict, "Resources")
	if !ok {
		t.Fatal("resolveDictValue not found")
	}
	if !bytes.Contains(value, []byte("/F1 3 0 R")) {
		t.Errorf("value = %q, want it to contain /F1 3 0 R", value)
	}
}

func TestPDFObjIndex_Find(t *testing.T) {
	data := []byte("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n")
	idx := buildPDFObjIndex(data)

	dict, ok := idx.find(regexp.MustCompile(`/Type\s*/Catalog\b`))
	if !ok {
		t.Fatal("find(Catalog) not found")
	}
	if !bytes.Contains(dict, []byte("/Pages 2 0 R")) {
		t.Errorf("dict = %q, want it to contain /Pages 2 0 R", dict)
	}
}
