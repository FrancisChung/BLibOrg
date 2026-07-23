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
