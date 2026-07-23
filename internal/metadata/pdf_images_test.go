// internal/metadata/pdf_images_test.go
package metadata

import (
	"bytes"
	"compress/zlib"
	"testing"
)

func TestFindPDFPageImages_FindsDCTDecodeOnFirstPage(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 4 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, true)
	if len(images) != 1 {
		t.Fatalf("len(images) = %d, want 1", len(images))
	}
	if images[0].page != 1 {
		t.Errorf("images[0].page = %d, want 1", images[0].page)
	}
	if string(images[0].bytes) != string(jpegData) {
		t.Errorf("images[0].bytes = %q, want %q", images[0].bytes, jpegData)
	}
	if images[0].contentType != "image/jpeg" {
		t.Errorf("images[0].contentType = %q, want image/jpeg", images[0].contentType)
	}
}

func TestFindPDFPageImages_SkipsPagesWithNoImageUntilOneHasOne(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /Font << /F1 9 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 5 0 R >> >> >>\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, true)
	if len(images) != 1 {
		t.Fatalf("len(images) = %d, want 1", len(images))
	}
	if images[0].page != 2 {
		t.Errorf("images[0].page = %d, want 2 (page 1 has no image)", images[0].page)
	}
}

func TestFindPDFPageImages_StopAtFirstFalseCollectsAll(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 5 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Resources << /XObject << /Im1 6 0 R >> >> >>\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n" +
			"6 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, false)
	if len(images) != 2 {
		t.Fatalf("len(images) = %d, want 2", len(images))
	}
}

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
