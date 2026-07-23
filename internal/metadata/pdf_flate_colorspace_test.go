// internal/metadata/pdf_flate_colorspace_test.go
package metadata

import (
	"bytes"
	"image/color"
	"image/png"
	"testing"
)

func colorRGBABlack() color.RGBA { return color.RGBA{A: 0xFF} }

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
