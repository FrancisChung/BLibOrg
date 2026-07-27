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

func TestFindPDFPageImages_FindsImageNestedInsideFormXObject(t *testing.T) {
	// Reproduces the real "Programming with Types" bug: the page's own
	// XObject entry is a Form (/Subtype /Form), and the actual image is
	// nested inside THAT form's own Resources/XObject, not directly on
	// the page.
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Fm1 10 0 R >> >> >>\nendobj\n" +
			"10 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Im0 11 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"11 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, true)
	if len(images) != 1 {
		t.Fatalf("len(images) = %d, want 1 (image nested inside the page's Form XObject)", len(images))
	}
	if string(images[0].bytes) != string(jpegData) {
		t.Errorf("images[0].bytes = %q, want %q", images[0].bytes, jpegData)
	}
}

func TestFindPDFPageImages_FindsImageFourFormsDeep(t *testing.T) {
	// Four chained Form XObjects (Fm1 -> Fm2 -> Fm3 -> Fm4), with the
	// image directly inside Fm4's own Resources -- exactly at the 4-level
	// depth cap, must still be found.
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Fm1 10 0 R >> >> >>\nendobj\n" +
			"10 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm2 11 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"11 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm3 12 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"12 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm4 13 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"13 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Im0 14 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"14 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, true)
	if len(images) != 1 {
		t.Fatalf("len(images) = %d, want 1 (image exactly 4 forms deep, at the depth cap)", len(images))
	}
}

func TestFindPDFPageImages_DoesNotFindImageFiveFormsDeep(t *testing.T) {
	// Same shape as the four-forms-deep test, but with one more Form
	// (Fm5) in the chain -- the image is now 5 forms deep and must NOT
	// be found (depth cap enforced, protecting against pathological
	// nesting).
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Fm1 10 0 R >> >> >>\nendobj\n" +
			"10 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm2 11 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"11 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm3 12 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"12 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm4 13 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"13 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm5 14 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"14 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Im0 15 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"15 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" +
			string(jpegData) + "\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, true)
	if len(images) != 0 {
		t.Fatalf("len(images) = %d, want 0 (image 5 forms deep, past the depth cap)", len(images))
	}
}

func TestFindPDFPageImages_FormCycleDoesNotHang(t *testing.T) {
	// Fm1 references Fm2, which references Fm1 back -- a malformed
	// cyclic PDF. The visited-set guard must stop the recursion; test
	// completing at all (without hanging) is proof it worked.
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Fm1 10 0 R >> >> >>\nendobj\n" +
			"10 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm2 11 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n" +
			"11 0 obj\n<< /Type /XObject /Subtype /Form /Resources << /XObject << /Fm1 10 0 R >> >> >>\nstream\nq Q\nendstream\nendobj\n")

	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	images := findPDFPageImages(idx, pages, false)
	if len(images) != 0 {
		t.Fatalf("len(images) = %d, want 0 (pure cycle, no images anywhere)", len(images))
	}
}

func TestFindFirstPageWithUndecodableImage_FindsPageWithUnsupportedFilter(t *testing.T) {
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 4 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /Font << /F1 9 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 5 0 R >> >> >>\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /JPXDecode /Length 10 >>\nstream\njpxbytes12\nendstream\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	pageNum, ok := findFirstPageWithUndecodableImage(idx, pages)
	if !ok {
		t.Fatal("findFirstPageWithUndecodableImage ok=false, want true")
	}
	if pageNum != 2 {
		t.Errorf("pageNum = %d, want 2 (page 1 has no image at all, page 2 has the undecodable one)", pageNum)
	}
}

func TestFindFirstPageWithUndecodableImage_NoImagesReturnsNotOK(t *testing.T) {
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /Font << /F1 9 0 R >> >> >>\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok {
		t.Fatal("walkPDFPageTree not ok")
	}
	if _, ok := findFirstPageWithUndecodableImage(idx, pages); ok {
		t.Error("findFirstPageWithUndecodableImage ok=true, want false (no image XObjects anywhere)")
	}
}

func TestFindFirstPageWithUndecodableImage_SkipsDecodableImage(t *testing.T) {
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
	if _, ok := findFirstPageWithUndecodableImage(idx, pages); ok {
		t.Error("findFirstPageWithUndecodableImage ok=true, want false (the only image is a decodable DCTDecode one)")
	}
}

func TestDecodePDFImageStream_UnresolvableFlateDecodeSkipped(t *testing.T) {
	// No /Width or /Height at all, so geometry parsing fails regardless of
	// filter -- confirms decodePDFImageStream's FlateDecode fallback still
	// degrades to ok=false for a dict it genuinely can't reconstruct,
	// same as any other malformed/unsupported image.
	dict := []byte(`<</Type/XObject/Subtype/Image/Filter/FlateDecode>>`)
	if _, _, ok := decodePDFImageStream(buildPDFObjIndex(nil), nil, dict, []byte("rawbytes")); ok {
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

	data, contentType, ok := decodePDFImageStream(buildPDFObjIndex(nil), nil, dict, compressed.Bytes())
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

func TestPageHasMultipleImages_TrueWhenPageHasTwoOrMoreImages(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 4 0 R /Im1 5 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" + string(jpegData) + "\nendstream\nendobj\n" +
			"5 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" + string(jpegData) + "\nendstream\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok || len(pages) != 1 {
		t.Fatalf("walkPDFPageTree ok=%v pages=%d, want ok=true pages=1", ok, len(pages))
	}
	if !pageHasMultipleImages(idx, pages[0]) {
		t.Error("pageHasMultipleImages = false, want true (page has 2 image XObjects)")
	}
}

func TestPageHasMultipleImages_FalseWhenPageHasOneImage(t *testing.T) {
	jpegData := []byte("\xFF\xD8\xFFfakejpegbytes")
	data := []byte(
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Resources << /XObject << /Im0 4 0 R >> >> >>\nendobj\n" +
			"4 0 obj\n<< /Type /XObject /Subtype /Image /Filter /DCTDecode /Length 16 >>\nstream\n" + string(jpegData) + "\nendstream\nendobj\n")
	idx := buildPDFObjIndex(data)
	pages, ok := walkPDFPageTree(idx, 10)
	if !ok || len(pages) != 1 {
		t.Fatalf("walkPDFPageTree ok=%v pages=%d, want ok=true pages=1", ok, len(pages))
	}
	if pageHasMultipleImages(idx, pages[0]) {
		t.Error("pageHasMultipleImages = true, want false (page has only 1 image XObject)")
	}
}
